// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	publiccompiler "github.com/ThalesMMS/cooler-compiler/compiler"
	"github.com/ThalesMMS/cooler-compiler/examples"
	"github.com/ThalesMMS/cooler-compiler/internal/compiler"
	"github.com/ThalesMMS/cooler-compiler/internal/web"
)

const (
	maxDebugSessions      = 32
	debugSessionTTL       = 30 * time.Minute
	maxRequestBodyBytes   = 1 << 20
	maxBreakpointLine     = 10_000_000
	maxAPIRunSteps        = 100_000
	maxTraceOffset        = 10_000_000
	maxHeaderBytes        = 16 << 10
	maxLiveInputLineBytes = 64 << 10
	maxDebuggerTextBytes  = 1_024
	maxWatchCount         = 64
	browserOpenTimeout    = 10 * time.Second
	capabilityHeader      = "X-Cooler-Capability"
)

var (
	version                    = "1.0.0"
	commit                     = ""
	buildDate                  = ""
	errNativeWindowUnavailable = errors.New("native window unavailable on this platform")
	maxAPIRequestedRunSteps    = compiler.MaximumVMPolicy().MaxInstructionsPerRun
	maxBreakpointCount         = compiler.MaximumVMPolicy().MaxBreakpoints
)

type debugSessionRequest struct {
	ID               string                     `json:"id,omitempty"`
	Source           string                     `json:"source,omitempty"`
	Sources          []compiler.SourceUnit      `json:"sources,omitempty"`
	Input            *string                    `json:"input,omitempty"`
	MaxSteps         int                        `json:"maxSteps,omitempty"`
	Breakpoints      []compiler.Breakpoint      `json:"breakpoints,omitempty"`
	Watches          []compiler.WatchExpression `json:"watches,omitempty"`
	Light            bool                       `json:"light,omitempty"`
	TraceFrom        int                        `json:"traceFrom,omitempty"`
	VMPolicy         *compiler.VMPolicy         `json:"vmPolicy,omitempty"`
	Interactive      bool                       `json:"interactive,omitempty"`
	Line             *string                    `json:"line,omitempty"`
	InputSequence    int                        `json:"inputSequence,omitempty"`
	Cursor           *compiler.SourcePosition   `json:"cursor,omitempty"`
	Heap             *compiler.HeapQuery        `json:"heap,omitempty"`
	ReverseMode      compiler.ReverseMode       `json:"reverseMode,omitempty"`
	TimelineRevision uint64                     `json:"timelineRevision,omitempty"`
	Optimization     string                     `json:"optimization,omitempty"`
	WarningRules     string                     `json:"warningRules,omitempty"`
}

type debugSessionResponse struct {
	ID       string `json:"id"`
	Snapshot any    `json:"snapshot"`
}

type heapSessionResponse struct {
	ID   string                `json:"id"`
	Heap compiler.HeapSnapshot `json:"heap"`
}

type lightDebugSnapshot struct {
	Runtime     compiler.VMResult              `json:"runtime"`
	Status      string                         `json:"status"`
	Location    *compiler.DebugLocation        `json:"location,omitempty"`
	StopReason  compiler.DebugStopReason       `json:"stopReason,omitempty"`
	Breakpoints []compiler.BreakpointState     `json:"breakpoints,omitempty"`
	Watches     []compiler.WatchResult         `json:"watches,omitempty"`
	DebugLog    []compiler.DebugLogEntry       `json:"debugLog,omitempty"`
	Timeline    *compiler.ReverseTimelineState `json:"timeline,omitempty"`
}

type pipelineResponse struct {
	ID       string                    `json:"id"`
	Snapshot compiler.PipelineSnapshot `json:"snapshot"`
}

type compileRequest struct {
	Source       string                `json:"source"`
	Sources      []compiler.SourceUnit `json:"sources,omitempty"`
	Input        string                `json:"input,omitempty"`
	MaxSteps     int                   `json:"maxSteps,omitempty"`
	VMPolicy     *compiler.VMPolicy    `json:"vmPolicy,omitempty"`
	Interactive  bool                  `json:"interactive,omitempty"`
	Optimization string                `json:"optimization,omitempty"`
	WarningRules string                `json:"warningRules,omitempty"`
}

type securityPolicy struct {
	capability   string
	listenerPort string
	allowRemote  bool
	allowedHosts map[string]struct{}
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	addr := flag.String("addr", "127.0.0.1:0", "internal GUI HTTP address")
	browser := flag.Bool("browser", false, "open in the default browser instead of a native window")
	open := flag.Bool("open", true, "open the browser automatically in browser mode")
	debugWebview := flag.Bool("debug-webview", false, "enable WebView debugging tools")
	allowRemote := flag.Bool("allow-remote", false, "allow the GUI HTTP server to listen beyond loopback (unsafe)")
	flag.Parse()

	if err := validateListenAddress(*addr, *allowRemote); err != nil {
		return err
	}
	if *allowRemote {
		log.Printf("WARNING: --allow-remote exposes the local GUI service beyond loopback; Cooler Compiler is not a hosted multi-user service")
	}
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", *addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", *addr, err)
	}
	guiURL := "http://" + listener.Addr().String()
	fmt.Printf("Cooler Compiler at %s\n", guiURL)
	capability, err := newCapability()
	if err != nil {
		_ = listener.Close()
		return fmt.Errorf("create process capability: %w", err)
	}
	_, listenerPort, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		_ = listener.Close()
		return fmt.Errorf("resolve listener address: %w", err)
	}
	policy := securityPolicy{
		capability:   capability,
		listenerPort: listenerPort,
		allowRemote:  *allowRemote,
		allowedHosts: allowedHostsForListener(*addr, listener.Addr(), *allowRemote),
	}

	sessions := newSessionStore()
	defer sessions.close()
	server := newHTTPServer(buildMux(sessions, policy))
	serverErr := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serverErr <- err
	}()

	if *browser {
		if *open {
			if err := openBrowser(guiURL); err != nil {
				return fmt.Errorf("open browser: %w", err)
			}
		}
		return <-serverErr
	}

	windowErr := runNativeWindow(guiURL, *debugWebview)
	if errors.Is(windowErr, errNativeWindowUnavailable) {
		log.Printf("%v; using the default browser", windowErr)
		if *open {
			if err := openBrowser(guiURL); err != nil {
				log.Printf("could not open browser automatically: %v; open %s manually", err, guiURL)
			}
		}
		return <-serverErr
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		_ = server.Close()
	}
	sessions.close()
	if err := <-serverErr; err != nil {
		return err
	}
	return windowErr
}

func buildMux(sessions *sessionStore, policy securityPolicy) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", web.Handler(policy.capability))
	mux.HandleFunc("/api/compile", apiGuard(policy, http.MethodPost, true, compileHandler))
	mux.HandleFunc("/api/pipeline/start", apiGuard(policy, http.MethodPost, true, sessions.pipelineStartHandler))
	mux.HandleFunc("/api/pipeline/next", apiGuard(policy, http.MethodPost, true, sessions.pipelineNextHandler))
	mux.HandleFunc("/api/pipeline/complete", apiGuard(policy, http.MethodPost, true, sessions.pipelineCompleteHandler))
	mux.HandleFunc("/api/pipeline/dispose", apiGuard(policy, http.MethodPost, true, sessions.disposeHandler))
	mux.HandleFunc("/api/vm/start", apiGuard(policy, http.MethodPost, true, sessions.startHandler))
	mux.HandleFunc("/api/vm/step", apiGuard(policy, http.MethodPost, true, sessions.stepHandler))
	mux.HandleFunc("/api/vm/step-into", apiGuard(policy, http.MethodPost, true, sessions.stepIntoHandler))
	mux.HandleFunc("/api/vm/step-over", apiGuard(policy, http.MethodPost, true, sessions.stepOverHandler))
	mux.HandleFunc("/api/vm/step-out", apiGuard(policy, http.MethodPost, true, sessions.stepOutHandler))
	mux.HandleFunc("/api/vm/run-to-cursor", apiGuard(policy, http.MethodPost, true, sessions.runToCursorHandler))
	mux.HandleFunc("/api/vm/run", apiGuard(policy, http.MethodPost, true, sessions.runHandler))
	mux.HandleFunc("/api/vm/reset", apiGuard(policy, http.MethodPost, true, sessions.resetHandler))
	mux.HandleFunc("/api/vm/input", apiGuard(policy, http.MethodPost, true, sessions.inputHandler))
	mux.HandleFunc("/api/vm/heap", apiGuard(policy, http.MethodPost, true, sessions.heapHandler))
	mux.HandleFunc("/api/vm/step-back", apiGuard(policy, http.MethodPost, true, sessions.stepBackHandler))
	mux.HandleFunc("/api/vm/reverse-continue", apiGuard(policy, http.MethodPost, true, sessions.reverseContinueHandler))
	mux.HandleFunc("/api/vm/dispose", apiGuard(policy, http.MethodPost, true, sessions.disposeHandler))
	mux.HandleFunc("/api/examples", apiGuard(policy, http.MethodGet, false, examplesHandler))
	mux.HandleFunc("/api/health", apiGuard(policy, http.MethodGet, false, healthHandler))
	return recoverHTTP(securityHeaders(hostGuard(policy, mux)))
}

func recoverHTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("recovered local request panic: %v", recovered)
				writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal request failure"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func newHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    maxHeaderBytes,
	}
}

func apiGuard(policy securityPolicy, method string, requireCapability bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != method {
			w.Header().Set("Allow", method)
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		if !originAllowed(policy, r) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "origin not allowed"})
			return
		}
		if requireCapability && !capabilityAllowed(policy.capability, r.Header.Get(capabilityHeader)) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "invalid process capability"})
			return
		}
		if method == http.MethodPost {
			mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mediaType != "application/json" {
				writeJSON(w, http.StatusUnsupportedMediaType, map[string]any{"error": "Content-Type must be application/json"})
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		}
		next(w, r)
	}
}

func hostGuard(policy securityPolicy, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !policy.hostAllowed(r.Host) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "host not allowed"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func originAllowed(policy securityPolicy, r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	return policy.hostAllowed(parsed.Host) && sameAuthority(parsed.Host, r.Host)
}

func capabilityAllowed(expected, provided string) bool {
	if len(expected) == 0 || len(provided) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) == 1
}

func (policy securityPolicy) hostAllowed(authority string) bool {
	host, port, err := splitAuthority(authority)
	if err != nil || host == "" {
		return false
	}
	if policy.listenerPort != "" && port != policy.listenerPort {
		return false
	}
	if len(policy.allowedHosts) > 0 {
		if _, found := policy.allowedHosts[host]; !found {
			return false
		}
	}
	return policy.allowRemote || isLoopbackHost(host)
}

func allowedHostsForListener(configuredAddress string, listenerAddress net.Addr, allowRemote bool) map[string]struct{} {
	hosts := map[string]struct{}{}
	addHost := func(host string) {
		host = strings.Trim(strings.ToLower(host), "[]")
		if host != "" {
			hosts[host] = struct{}{}
		}
	}
	addHost("localhost")
	addHost("127.0.0.1")
	addHost("::1")

	configuredHost, _, _ := net.SplitHostPort(configuredAddress)
	configuredIP := net.ParseIP(strings.Trim(configuredHost, "[]"))
	if configuredHost != "" && (configuredIP == nil || !configuredIP.IsUnspecified()) {
		addHost(configuredHost)
	}
	if actualHost, _, err := net.SplitHostPort(listenerAddress.String()); err == nil {
		actualIP := net.ParseIP(strings.Trim(actualHost, "[]"))
		if actualHost != "" && (actualIP == nil || !actualIP.IsUnspecified()) {
			addHost(actualHost)
		}
	}
	if allowRemote && (configuredIP == nil || configuredIP.IsUnspecified()) {
		if addresses, err := net.InterfaceAddrs(); err == nil {
			for _, address := range addresses {
				if network, ok := address.(*net.IPNet); ok {
					addHost(network.IP.String())
				}
			}
		}
	}
	return hosts
}

func splitAuthority(authority string) (string, string, error) {
	if strings.Contains(authority, ":") {
		host, port, err := net.SplitHostPort(authority)
		return strings.Trim(strings.ToLower(host), "[]"), port, err
	}
	return strings.ToLower(authority), "", nil
}

func sameAuthority(left, right string) bool {
	leftHost, leftPort, leftErr := splitAuthority(left)
	rightHost, rightPort, rightErr := splitAuthority(right)
	return leftErr == nil && rightErr == nil && leftHost == rightHost && leftPort == rightPort
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(strings.Trim(host, "[]"), "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func validateListenAddress(address string, allowRemote bool) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid listen address %s: %w", address, err)
	}
	if allowRemote || isLoopbackHost(host) {
		return nil
	}
	return fmt.Errorf("refusing non-loopback listen address %s without --allow-remote", address)
}

func compileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "use POST"})
		return
	}
	var request compileRequest
	if err := decodeJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if request.Interactive {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "interactive execution requires a pipeline or VM session"})
		return
	}
	if err := compiler.ValidateWarningRules(request.WarningRules); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := validateOptimizationMode(request.Optimization); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	steps, err := validateCompileExecution(request.MaxSteps, request.VMPolicy)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	options := publicCompileOptions(request.Input, steps, request.Optimization, request.VMPolicy)
	options.WarningRules = request.WarningRules
	var result publiccompiler.Result
	if len(request.Sources) > 0 {
		project := publiccompiler.Project{Sources: make([]publiccompiler.SourceUnit, len(request.Sources))}
		for index, source := range request.Sources {
			project.Sources[index] = publiccompiler.SourceUnit{ID: source.ID, Name: source.Name, Content: source.Content}
		}
		result, err = publiccompiler.CompileProject(r.Context(), project, options)
	} else {
		result, err = publiccompiler.Compile(r.Context(), request.Source, options)
	}
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, publiccompiler.ErrInvalidOptions) || errors.Is(err, publiccompiler.ErrInvalidProject) {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func publicCompileOptions(input string, steps int, optimization string, requested *compiler.VMPolicy) publiccompiler.Options {
	policy := valueOrDefaultPolicy(requested)
	return publiccompiler.Options{
		Mode: publiccompiler.ModeRun, Input: input, MaxSteps: steps, Optimization: optimization,
		CompilerVersion: version, CompilerCommit: commit,
		Limits: publiccompiler.Limits{
			MaxInstructionsPerRun: policy.MaxInstructionsPerRun, MaxInstructionsTotal: policy.MaxInstructionsTotal,
			MaxOperandStackDepth: policy.MaxOperandStackDepth, MaxCallFrameDepth: policy.MaxCallFrameDepth,
			MaxScopeDepth: policy.MaxScopeDepth, MaxObjectAllocations: policy.MaxObjectAllocations,
			MaxTraceEntries: policy.MaxTraceEntries, MaxTraceBytes: policy.MaxTraceBytes,
			MaxStringBytes: policy.MaxStringBytes, MaxOutputBytes: policy.MaxOutputBytes,
			MaxInputBytes: policy.MaxInputBytes, MaxInputLines: policy.MaxInputLines, MaxBreakpoints: policy.MaxBreakpoints,
		},
	}
}

func apiResultContract(source, input string, execute bool, requested ...*compiler.VMPolicy) compiler.ResultContract {
	var policy *compiler.VMPolicy
	if len(requested) > 0 {
		policy = requested[0]
	}
	return compiler.ResultContract{
		CompilerVersion: version,
		CompilerCommit:  commit,
		Options: compiler.CompilationOptions{
			Execute:       execute,
			VMMaxSteps:    maxAPIRunSteps,
			InputProvided: input != "",
			VMPolicy:      valueOrDefaultPolicy(policy),
		},
	}
}

func valueOrDefaultPolicy(policy *compiler.VMPolicy) compiler.VMPolicy {
	if policy == nil {
		return compiler.VMPolicy{}
	}
	return *policy
}

func validateRequestedVMPolicy(policy *compiler.VMPolicy) error {
	if policy == nil {
		return nil
	}
	_, err := compiler.NormalizeVMPolicy(*policy)
	return err
}

func validateOptimizationMode(mode string) error {
	_, err := compiler.NormalizeOptimizationMode(mode)
	return err
}

func validateCompileExecution(requested int, policy *compiler.VMPolicy) (int, error) {
	if err := validateRequestedVMPolicy(policy); err != nil {
		return 0, err
	}
	if requested < 0 || requested > maxAPIRequestedRunSteps {
		return 0, fmt.Errorf("maxSteps must be between 0 and %d", maxAPIRequestedRunSteps)
	}
	steps := requested
	if steps == 0 {
		steps = maxAPIRunSteps
	}
	normalized, _ := compiler.NormalizeVMPolicy(valueOrDefaultPolicy(policy))
	if steps > normalized.MaxInstructionsPerRun {
		return 0, fmt.Errorf("maxSteps must not exceed vmPolicy.maxInstructionsPerRun (%d)", normalized.MaxInstructionsPerRun)
	}
	return steps, nil
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "use GET"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "name": "cooler-compiler", "version": version,
		"commit": commit, "buildDate": buildDate,
	})
}

func examplesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "use GET"})
		return
	}
	catalog, err := examples.Catalog()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "example catalog unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"examples": catalog})
}

func (s *sessionStore) startHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "use POST"})
		return
	}
	var request debugSessionRequest
	if err := decodeDebugSessionRequest(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := validateRequestedVMPolicy(request.VMPolicy); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := validateOptimizationMode(request.Optimization); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	contract := apiResultContract(request.Source, valueOrEmpty(request.Input), true, request.VMPolicy)
	contract.Options.Optimization = request.Optimization
	contract.Options.WarningRules = request.WarningRules
	pipeline := projectPipeline(request.Source, request.Sources, contract)
	pipeline.Complete()
	session, ok := pipeline.DebugSession()
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "could not complete compilation session"})
		return
	}
	entry := &sessionEntry{pipeline: pipeline, session: session}
	applyOptionalInput(entry, request.Input)
	entry.interactive = request.Interactive
	session.SetInteractiveInput(entry.interactive)
	snapshot := session.Snapshot(request.Light, request.TraceFrom)
	entry.updateDebugState(snapshot)
	id, pin, err := s.createPinned(entry)
	if err != nil {
		writeSessionCreateError(w, err)
		return
	}
	defer pin.release()
	writeJSON(w, http.StatusOK, debugSessionResponse{ID: id, Snapshot: debugSnapshotPayload(snapshot, request.Light)})
}

func (s *sessionStore) pipelineStartHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "use POST"})
		return
	}
	var request compileRequest
	if err := decodeJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := validateRequestedVMPolicy(request.VMPolicy); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := compiler.ValidateWarningRules(request.WarningRules); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := validateOptimizationMode(request.Optimization); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	contract := apiResultContract(request.Source, request.Input, true, request.VMPolicy)
	contract.Options.Optimization = request.Optimization
	contract.Options.WarningRules = request.WarningRules
	pipeline := projectPipeline(request.Source, request.Sources, contract)
	snapshot := pipeline.NextPhase()
	entry := &sessionEntry{pipeline: pipeline, input: request.Input, interactive: request.Interactive}
	if session, ok := pipeline.DebugSession(); ok {
		entry.session = session
		session.SetInput(entry.input)
		session.SetInteractiveInput(entry.interactive)
	}
	entry.updatePipelineState(snapshot)
	id, pin, err := s.createPinned(entry)
	if err != nil {
		writeSessionCreateError(w, err)
		return
	}
	defer pin.release()
	writeJSON(w, http.StatusOK, pipelineResponse{ID: id, Snapshot: snapshot})
}

func writeSessionCreateError(w http.ResponseWriter, err error) {
	if errors.Is(err, errSessionCapacity) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "could not create session"})
}

func (s *sessionStore) pipelineNextHandler(w http.ResponseWriter, r *http.Request) {
	s.withPipeline(w, r, sessionOperationPipelineNext, func(pipeline *compiler.CompilationSession) compiler.PipelineSnapshot {
		return pipeline.NextPhase()
	})
}

func (s *sessionStore) pipelineCompleteHandler(w http.ResponseWriter, r *http.Request) {
	s.withPipeline(w, r, sessionOperationPipelineComplete, func(pipeline *compiler.CompilationSession) compiler.PipelineSnapshot {
		return pipeline.Complete()
	})
}

func (s *sessionStore) stepHandler(w http.ResponseWriter, r *http.Request) {
	s.withSessionResult(r.Context(), w, r, sessionOperationStep, func(_ context.Context, entry *sessionEntry, request debugSessionRequest) (compiler.DebugSnapshot, error) {
		entry.session.Step()
		return entry.session.Snapshot(request.Light, request.TraceFrom), nil
	})
}

func (s *sessionStore) stepIntoHandler(w http.ResponseWriter, r *http.Request) {
	s.withSessionResult(r.Context(), w, r, sessionOperationStepInto, func(ctx context.Context, entry *sessionEntry, request debugSessionRequest) (compiler.DebugSnapshot, error) {
		return entry.session.StepIntoContext(ctx, request.MaxSteps, request.Breakpoints), nil
	})
}

func (s *sessionStore) stepOverHandler(w http.ResponseWriter, r *http.Request) {
	s.withSessionResult(r.Context(), w, r, sessionOperationStepOver, func(ctx context.Context, entry *sessionEntry, request debugSessionRequest) (compiler.DebugSnapshot, error) {
		return entry.session.StepOverContext(ctx, request.MaxSteps, request.Breakpoints), nil
	})
}

func (s *sessionStore) stepOutHandler(w http.ResponseWriter, r *http.Request) {
	s.withSessionResult(r.Context(), w, r, sessionOperationStepOut, func(ctx context.Context, entry *sessionEntry, request debugSessionRequest) (compiler.DebugSnapshot, error) {
		return entry.session.StepOutContext(ctx, request.MaxSteps, request.Breakpoints), nil
	})
}

func (s *sessionStore) runToCursorHandler(w http.ResponseWriter, r *http.Request) {
	s.withSessionResult(r.Context(), w, r, sessionOperationRunToCursor, func(ctx context.Context, entry *sessionEntry, request debugSessionRequest) (compiler.DebugSnapshot, error) {
		return entry.session.RunToCursorContext(ctx, request.MaxSteps, request.Breakpoints, *request.Cursor)
	})
}

func (s *sessionStore) runHandler(w http.ResponseWriter, r *http.Request) {
	s.withSessionResult(r.Context(), w, r, sessionOperationRun, func(ctx context.Context, entry *sessionEntry, request debugSessionRequest) (compiler.DebugSnapshot, error) {
		entry.session.RunWithBreakpointsContext(ctx, request.MaxSteps, request.Breakpoints)
		return entry.session.Snapshot(request.Light, request.TraceFrom), nil
	})
}

func (s *sessionStore) resetHandler(w http.ResponseWriter, r *http.Request) {
	s.withSessionResult(r.Context(), w, r, sessionOperationReset, func(_ context.Context, entry *sessionEntry, request debugSessionRequest) (compiler.DebugSnapshot, error) {
		entry.session.Reset()
		return entry.session.Snapshot(request.Light, request.TraceFrom), nil
	})
}

func (s *sessionStore) inputHandler(w http.ResponseWriter, r *http.Request) {
	s.withSessionResult(r.Context(), w, r, sessionOperationInput, func(_ context.Context, entry *sessionEntry, request debugSessionRequest) (compiler.DebugSnapshot, error) {
		_, err := entry.session.ProvideInputLine(*request.Line, request.InputSequence)
		return entry.session.Snapshot(request.Light, request.TraceFrom), err
	})
}

func (s *sessionStore) heapHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "use POST"})
		return
	}
	var request debugSessionRequest
	if err := decodeDebugSessionRequest(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := validateSessionOperationRequest(sessionOperationHeap, request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if request.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing session id"})
		return
	}
	lease, found := s.acquire(request.ID)
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "debug session not found"})
		return
	}
	entry := lease.entry
	defer lease.release()
	if !entry.allows(sessionOperationHeap) {
		state := entry.state
		s.reject()
		lease.release()
		writeInvalidTransition(w, sessionOperationHeap, state)
		return
	}
	if entry.session == nil {
		lease.release()
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "debug session unavailable"})
		return
	}
	heap, err := entry.session.Heap(*request.Heap)
	if err != nil {
		s.reject()
		status := http.StatusBadRequest
		if errors.Is(err, compiler.ErrHeapObjectNotFound) {
			status = http.StatusNotFound
		}
		lease.release()
		writeJSON(w, status, map[string]any{"error": err.Error(), "id": request.ID})
		return
	}
	lease.release()
	writeJSON(w, http.StatusOK, heapSessionResponse{ID: request.ID, Heap: heap})
}

func (s *sessionStore) stepBackHandler(w http.ResponseWriter, r *http.Request) {
	s.withSessionResult(r.Context(), w, r, sessionOperationStepBack, func(_ context.Context, entry *sessionEntry, request debugSessionRequest) (compiler.DebugSnapshot, error) {
		if entry.session.ReverseTimeline().Revision != request.TimelineRevision {
			return entry.session.Snapshot(request.Light, request.TraceFrom), compiler.ErrTimelineRevision
		}
		snapshot, err := entry.session.StepBack(request.ReverseMode)
		if request.Light && err == nil {
			snapshot = entry.session.Snapshot(true, request.TraceFrom)
		}
		return snapshot, err
	})
}

func (s *sessionStore) reverseContinueHandler(w http.ResponseWriter, r *http.Request) {
	s.withSessionResult(r.Context(), w, r, sessionOperationReverseContinue, func(_ context.Context, entry *sessionEntry, request debugSessionRequest) (compiler.DebugSnapshot, error) {
		if entry.session.ReverseTimeline().Revision != request.TimelineRevision {
			return entry.session.Snapshot(request.Light, request.TraceFrom), compiler.ErrTimelineRevision
		}
		snapshot, err := entry.session.ReverseContinue()
		if request.Light && err == nil {
			snapshot = entry.session.Snapshot(true, request.TraceFrom)
		}
		return snapshot, err
	})
}

func (s *sessionStore) disposeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "use POST"})
		return
	}
	var request debugSessionRequest
	if err := decodeDebugSessionRequest(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if request.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing session id"})
		return
	}
	found := s.remove(request.ID, sessionStateDisposed)
	if !found {
		s.reject()
	}
	writeJSON(w, http.StatusOK, map[string]any{"disposed": found})
}

func (s *sessionStore) withSession(w http.ResponseWriter, r *http.Request, operation sessionOperation, action func(*sessionEntry, debugSessionRequest) compiler.DebugSnapshot) {
	s.withSessionResult(r.Context(), w, r, operation, func(_ context.Context, entry *sessionEntry, request debugSessionRequest) (compiler.DebugSnapshot, error) {
		return action(entry, request), nil
	})
}

func (s *sessionStore) withSessionResult(ctx context.Context, w http.ResponseWriter, r *http.Request, operation sessionOperation, action func(context.Context, *sessionEntry, debugSessionRequest) (compiler.DebugSnapshot, error)) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "use POST"})
		return
	}
	var request debugSessionRequest
	if err := decodeDebugSessionRequest(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := validateSessionOperationRequest(operation, request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if request.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing session id"})
		return
	}

	lease, found := s.acquire(request.ID)
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "debug session not found"})
		return
	}
	entry := lease.entry

	var snapshot compiler.DebugSnapshot
	var actionErr error
	var recovered any
	available := false
	invalidState := sessionState("")
	func() {
		defer lease.release()
		defer func() {
			if value := recover(); value != nil {
				recovered = value
				if entry.session != nil {
					entry.session.AbortInternal(fmt.Sprint(value))
					snapshot = entry.session.Snapshot(request.Light, request.TraceFrom)
					entry.updateDebugState(snapshot)
				}
			}
		}()
		if !entry.allows(operation) {
			invalidState = entry.state
			s.reject()
			return
		}
		available = s.promoteDebugSession(entry)
		if !available {
			return
		}
		if !entry.allows(operation) {
			invalidState = entry.state
			s.reject()
			return
		}
		entry.state = sessionStateRunning
		applyOptionalInput(entry, request.Input)
		entry.session.SetWatches(request.Watches)
		snapshot, actionErr = action(ctx, entry, request)
		entry.updateDebugState(snapshot)
	}()
	if recovered != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "internal VM failure", "id": request.ID, "snapshot": debugSnapshotPayload(snapshot, request.Light),
		})
		return
	}
	if actionErr != nil {
		s.reject()
		status := http.StatusConflict
		if errors.Is(actionErr, compiler.ErrInputLimit) {
			status = http.StatusRequestEntityTooLarge
		}
		writeJSON(w, status, map[string]any{"error": actionErr.Error(), "id": request.ID, "snapshot": debugSnapshotPayload(snapshot, request.Light)})
		return
	}
	if invalidState != "" {
		writeInvalidTransition(w, operation, invalidState)
		return
	}
	if !available {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "debug session unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, debugSessionResponse{ID: request.ID, Snapshot: debugSnapshotPayload(snapshot, request.Light)})
}

func writeInvalidTransition(w http.ResponseWriter, operation sessionOperation, state sessionState) {
	writeJSON(w, http.StatusConflict, map[string]any{
		"error": "invalid session transition", "operation": operation, "state": state,
	})
}

func debugSnapshotPayload(snapshot compiler.DebugSnapshot, light bool) any {
	if light {
		return lightDebugSnapshot{
			Runtime: snapshot.Runtime, Status: snapshot.Status, Location: snapshot.Location, StopReason: snapshot.StopReason,
			Breakpoints: snapshot.Breakpoints, Watches: snapshot.Watches, DebugLog: snapshot.DebugLog, Timeline: snapshot.Timeline,
		}
	}
	return snapshot
}

func (s *sessionStore) withPipeline(w http.ResponseWriter, r *http.Request, operation sessionOperation, action func(*compiler.CompilationSession) compiler.PipelineSnapshot) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "use POST"})
		return
	}
	var request debugSessionRequest
	if err := decodeDebugSessionRequest(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if request.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing session id"})
		return
	}
	lease, found := s.acquire(request.ID)
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "compilation session not found"})
		return
	}
	entry := lease.entry

	var snapshot compiler.PipelineSnapshot
	var recovered any
	invalidState := sessionState("")
	available := true
	func() {
		defer lease.release()
		defer func() { recovered = recover() }()
		if entry.pipeline == nil {
			available = false
			return
		}
		if !entry.allows(operation) {
			invalidState = entry.state
			s.reject()
			return
		}
		entry.state = sessionStateCompiling
		applyOptionalInput(entry, request.Input)
		snapshot = action(entry.pipeline)
		entry.updatePipelineState(snapshot)
		if session, ok := entry.pipeline.DebugSession(); ok {
			entry.session = session
			session.SetInput(entry.input)
			session.SetInteractiveInput(entry.interactive)
			entry.updateDebugState(session.Snapshot(false, 0))
		}
	}()
	if recovered != nil {
		s.remove(request.ID, sessionStateDisposed)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal compilation failure"})
		return
	}
	if invalidState != "" {
		writeInvalidTransition(w, operation, invalidState)
		return
	}
	if !available {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "compilation session not found"})
		return
	}
	writeJSON(w, http.StatusOK, pipelineResponse{ID: request.ID, Snapshot: snapshot})
}

func (s *sessionStore) promoteDebugSession(entry *sessionEntry) bool {
	if entry.session != nil {
		return true
	}
	if entry.pipeline == nil {
		return false
	}
	pipelineSnapshot := entry.pipeline.Complete()
	entry.updatePipelineState(pipelineSnapshot)
	session, ok := entry.pipeline.DebugSession()
	if !ok {
		return false
	}
	entry.session = session
	entry.session.SetInput(entry.input)
	entry.session.SetInteractiveInput(entry.interactive)
	entry.updateDebugState(entry.session.Snapshot(false, 0))
	return true
}

func applyOptionalInput(entry *sessionEntry, input *string) {
	if input == nil {
		return
	}
	entry.input = *input
	if entry.session != nil {
		entry.session.SetInput(entry.input)
	}
}

func decodeJSON(r *http.Request, destination any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("invalid JSON: multiple values are not allowed")
		}
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

func decodeDebugSessionRequest(r *http.Request, destination *debugSessionRequest) error {
	if err := decodeJSON(r, destination); err != nil {
		return err
	}
	return validateDebugSessionRequest(*destination)
}

func validateDebugSessionRequest(request debugSessionRequest) error {
	if err := compiler.ValidateWarningRules(request.WarningRules); err != nil {
		return err
	}
	if request.ID != "" && !validSessionID(request.ID) {
		return errors.New("invalid session id")
	}
	if request.MaxSteps < 0 || request.MaxSteps > maxAPIRequestedRunSteps {
		return fmt.Errorf("maxSteps must be between 0 and %d", maxAPIRequestedRunSteps)
	}
	if request.TraceFrom < 0 || request.TraceFrom > maxTraceOffset {
		return fmt.Errorf("traceFrom must be between 0 and %d", maxTraceOffset)
	}
	if len(request.Breakpoints) > maxBreakpointCount {
		return fmt.Errorf("breakpoints must contain at most %d entries", maxBreakpointCount)
	}
	if len(request.Watches) > maxWatchCount {
		return fmt.Errorf("watches must contain at most %d entries", maxWatchCount)
	}
	if request.ID != "" && request.VMPolicy != nil {
		return errors.New("vmPolicy can only be set when starting a session")
	}
	if request.ID != "" && request.Interactive {
		return errors.New("interactive can only be set when starting a session")
	}
	if request.Line != nil && len(*request.Line) > maxLiveInputLineBytes {
		return fmt.Errorf("line must contain at most %d bytes", maxLiveInputLineBytes)
	}
	if err := validateRequestedVMPolicy(request.VMPolicy); err != nil {
		return err
	}
	seenBreakpoints := map[string]bool{}
	for _, breakpoint := range request.Breakpoints {
		if breakpoint.Line <= 0 || breakpoint.Line > maxBreakpointLine {
			return fmt.Errorf("breakpoint lines must be between 1 and %d", maxBreakpointLine)
		}
		if breakpoint.Column < 0 || breakpoint.Column > maxBreakpointLine {
			return fmt.Errorf("breakpoint columns must be between 0 and %d", maxBreakpointLine)
		}
		if len(breakpoint.ID) > maxDebuggerTextBytes || len(breakpoint.Condition) > maxDebuggerTextBytes || len(breakpoint.LogMessage) > maxDebuggerTextBytes {
			return fmt.Errorf("breakpoint id, condition, and log message must each contain at most %d bytes", maxDebuggerTextBytes)
		}
		if breakpoint.HitCount < 0 || breakpoint.HitCount > maxTraceOffset {
			return fmt.Errorf("breakpoint hitCount must be between 0 and %d", maxTraceOffset)
		}
		identity := breakpoint.ID
		if identity == "" {
			identity = fmt.Sprintf("%s:%d:%d", breakpoint.SourceID, breakpoint.Line, breakpoint.Column)
		}
		if seenBreakpoints[identity] {
			return errors.New("breakpoint identities must be unique")
		}
		seenBreakpoints[identity] = true
	}
	seenWatches := map[string]bool{}
	for _, watch := range request.Watches {
		if watch.ID == "" || len(watch.ID) > maxDebuggerTextBytes || len(watch.Expression) > maxDebuggerTextBytes {
			return fmt.Errorf("watches require an id and expression of at most %d bytes", maxDebuggerTextBytes)
		}
		if seenWatches[watch.ID] {
			return fmt.Errorf("watch ids must be unique")
		}
		seenWatches[watch.ID] = true
	}
	return nil
}

func validateSessionOperationRequest(operation sessionOperation, request debugSessionRequest) error {
	if operation == sessionOperationStepBack || operation == sessionOperationReverseContinue {
		if request.TimelineRevision == 0 {
			return errors.New("missing timelineRevision")
		}
		if operation == sessionOperationStepBack {
			if request.ReverseMode != "" && request.ReverseMode != compiler.ReverseModeInstruction && request.ReverseMode != compiler.ReverseModeSource {
				return errors.New("reverseMode must be instruction or source")
			}
		} else if request.ReverseMode != "" {
			return errors.New("reverseMode is only valid for step-back")
		}
		if request.Cursor != nil || request.Line != nil || request.InputSequence != 0 || request.Input != nil || request.MaxSteps != 0 || len(request.Breakpoints) != 0 || request.Heap != nil {
			return errors.New("reverse operations do not accept forward execution inputs")
		}
		return nil
	}
	if request.ReverseMode != "" || request.TimelineRevision != 0 {
		return errors.New("reverseMode and timelineRevision are only valid for reverse operations")
	}
	if operation == sessionOperationHeap {
		if request.Heap == nil {
			return errors.New("missing heap query")
		}
		mode := strings.ToLower(strings.TrimSpace(request.Heap.Mode))
		if mode != "" && mode != "summary" && mode != "roots" && mode != "object" && mode != "graph" && mode != "search" {
			return errors.New("heap mode must be summary, roots, object, graph, or search")
		}
		if request.Heap.Offset < 0 || len(request.Heap.ID) > maxDebuggerTextBytes || len(request.Heap.Class) > maxDebuggerTextBytes {
			return fmt.Errorf("heap offset cannot be negative and id/class must each contain at most %d bytes", maxDebuggerTextBytes)
		}
		if _, err := compiler.NormalizeHeapLimits(request.Heap.Limits); err != nil {
			return err
		}
		if request.Cursor != nil || request.Line != nil || request.InputSequence != 0 || request.Input != nil || request.MaxSteps != 0 || len(request.Breakpoints) != 0 || len(request.Watches) != 0 || request.Light || request.TraceFrom != 0 {
			return errors.New("heap queries only accept id and heap")
		}
		return nil
	}
	if request.Heap != nil {
		return errors.New("heap is only valid for the heap operation")
	}
	if operation == sessionOperationRunToCursor {
		if request.Cursor == nil {
			return errors.New("missing cursor source position")
		}
		if request.Cursor.SourceID == "" || request.Cursor.Line < 1 || request.Cursor.Line > maxBreakpointLine || request.Cursor.Column < 0 || request.Cursor.Column > maxBreakpointLine {
			return fmt.Errorf("cursor requires a sourceId, line between 1 and %d, and optional column between 0 and %d", maxBreakpointLine, maxBreakpointLine)
		}
	} else if request.Cursor != nil {
		return errors.New("cursor is only valid for run-to-cursor")
	}
	if operation == sessionOperationInput {
		if request.Line == nil {
			return errors.New("missing input line")
		}
		if request.Input != nil {
			return errors.New("pre-supplied input cannot be replaced by the input operation")
		}
		if request.InputSequence < 1 || request.InputSequence > maxTraceOffset {
			return fmt.Errorf("inputSequence must be between 1 and %d", maxTraceOffset)
		}
		return nil
	}
	if request.Line != nil || request.InputSequence != 0 {
		return errors.New("line and inputSequence are only valid for the input operation")
	}
	return nil
}

func projectPipeline(source string, sources []compiler.SourceUnit, contract compiler.ResultContract) *compiler.CompilationSession {
	if len(sources) > 0 {
		return compiler.NewProjectCompilationSession(sources, contract)
	}
	return compiler.NewCompilationSessionWithContract(source, contract)
}

func validSessionID(id string) bool {
	if len(id) != 32 {
		return false
	}
	decoded, err := hex.DecodeString(id)
	return err == nil && len(decoded) == 16 && id == strings.ToLower(id)
}

func newSessionID() (string, error) {
	return newRandomHex(16)
}

func newCapability() (string, error) {
	return newRandomHex(32)
}

func newRandomHex(size int) (string, error) {
	bytes := make([]byte, size)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func openBrowser(target string) error {
	ctx, cancel := context.WithTimeout(context.Background(), browserOpenTimeout)
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.CommandContext(ctx, "open", target)
	case "windows":
		command = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", target)
	default:
		command = exec.CommandContext(ctx, "xdg-open", target)
	}
	done, err := startAndReap(command, cancel)
	if err != nil {
		return err
	}
	go func() {
		if err := <-done; err != nil && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			log.Printf("browser opener exited unsuccessfully: %v", err)
		}
	}()
	return nil
}

// startAndReap starts a short-lived helper and guarantees that Wait is called.
// Without Wait, Unix launchers can remain as zombies and Go retains process
// resources until the GUI exits.
func startAndReap(command *exec.Cmd, cancel context.CancelFunc) (<-chan error, error) {
	if err := command.Start(); err != nil {
		cancel()
		return nil, err
	}
	done := make(chan error, 1)
	go func() {
		err := command.Wait()
		cancel()
		done <- err
		close(done)
	}()
	return done, nil
}
