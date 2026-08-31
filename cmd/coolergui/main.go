// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ThalesMMS/cooler-compiler/examples"
	"github.com/ThalesMMS/cooler-compiler/internal/compiler"
	"github.com/ThalesMMS/cooler-compiler/internal/web"
)

const (
	maxDebugSessions    = 32
	debugSessionTTL     = 30 * time.Minute
	maxRequestBodyBytes = 1 << 20
)

var (
	version                    = "dev"
	errNativeWindowUnavailable = errors.New("native window unavailable on this platform")
)

type debugSessionRequest struct {
	ID          string  `json:"id,omitempty"`
	Source      string  `json:"source,omitempty"`
	Input       *string `json:"input,omitempty"`
	MaxSteps    int     `json:"maxSteps,omitempty"`
	Breakpoints []int   `json:"breakpoints,omitempty"`
	Light       bool    `json:"light,omitempty"`
	TraceFrom   int     `json:"traceFrom,omitempty"`
}

type debugSessionResponse struct {
	ID       string `json:"id"`
	Snapshot any    `json:"snapshot"`
}

type lightDebugSnapshot struct {
	Runtime compiler.VMResult `json:"runtime"`
	Status  string            `json:"status"`
}

type pipelineResponse struct {
	ID       string                    `json:"id"`
	Snapshot compiler.PipelineSnapshot `json:"snapshot"`
}

type compileRequest struct {
	Source string `json:"source"`
	Input  string `json:"input,omitempty"`
}

type sessionEntry struct {
	mu       sync.Mutex
	pipeline *compiler.CompilationSession
	session  *compiler.DebugSession
	input    string
	lastUsed time.Time
}

type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]*sessionEntry
	max      int
	ttl      time.Duration
}

func newSessionStore() *sessionStore {
	return &sessionStore{sessions: map[string]*sessionEntry{}, max: maxDebugSessions, ttl: debugSessionTTL}
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
	flag.Parse()

	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", *addr, err)
	}
	guiURL := "http://" + listener.Addr().String()
	fmt.Printf("Cooler Compiler Lab at %s\n", guiURL)

	server := &http.Server{Handler: buildMux(newSessionStore()), ReadHeaderTimeout: 5 * time.Second}
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
	if err := <-serverErr; err != nil {
		return err
	}
	return windowErr
}

func buildMux(sessions *sessionStore) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", web.Handler())
	mux.HandleFunc("/api/compile", apiGuard(compileHandler))
	mux.HandleFunc("/api/pipeline/start", apiGuard(sessions.pipelineStartHandler))
	mux.HandleFunc("/api/pipeline/next", apiGuard(sessions.pipelineNextHandler))
	mux.HandleFunc("/api/pipeline/complete", apiGuard(sessions.pipelineCompleteHandler))
	mux.HandleFunc("/api/pipeline/dispose", apiGuard(sessions.disposeHandler))
	mux.HandleFunc("/api/vm/start", apiGuard(sessions.startHandler))
	mux.HandleFunc("/api/vm/step", apiGuard(sessions.stepHandler))
	mux.HandleFunc("/api/vm/run", apiGuard(sessions.runHandler))
	mux.HandleFunc("/api/vm/reset", apiGuard(sessions.resetHandler))
	mux.HandleFunc("/api/vm/dispose", apiGuard(sessions.disposeHandler))
	mux.HandleFunc("/api/examples", apiGuard(examplesHandler))
	mux.HandleFunc("/api/health", apiGuard(healthHandler))
	return mux
}

func apiGuard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !originAllowed(r) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "origin not allowed"})
			return
		}
		if r.Method == http.MethodPost {
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				writeJSON(w, http.StatusUnsupportedMediaType, map[string]any{"error": "Content-Type must be application/json"})
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		}
		next(w, r)
	}
}

func originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	switch parsed.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
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
	writeJSON(w, http.StatusOK, compiler.CompileWithInput(request.Source, request.Input))
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "use GET"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": "cooler-compiler", "version": version})
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
	if err := decodeJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	id, err := newSessionID()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "could not create session"})
		return
	}
	pipeline := compiler.NewCompilationSession(request.Source)
	pipeline.Complete()
	session, ok := pipeline.DebugSession()
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "could not complete compilation session"})
		return
	}
	entry := &sessionEntry{pipeline: pipeline, session: session, lastUsed: time.Now()}
	applyOptionalInput(entry, request.Input)
	s.insert(id, entry)

	snapshot := session.Snapshot(request.Light, request.TraceFrom)
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
	id, err := newSessionID()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "could not create session"})
		return
	}
	pipeline := compiler.NewCompilationSession(request.Source)
	snapshot := pipeline.NextPhase()
	entry := &sessionEntry{pipeline: pipeline, input: request.Input, lastUsed: time.Now()}
	if session, ok := pipeline.DebugSession(); ok {
		entry.session = session
		session.SetInput(entry.input)
	}
	s.insert(id, entry)
	writeJSON(w, http.StatusOK, pipelineResponse{ID: id, Snapshot: snapshot})
}

func (s *sessionStore) pipelineNextHandler(w http.ResponseWriter, r *http.Request) {
	s.withPipeline(w, r, func(pipeline *compiler.CompilationSession) compiler.PipelineSnapshot {
		return pipeline.NextPhase()
	})
}

func (s *sessionStore) pipelineCompleteHandler(w http.ResponseWriter, r *http.Request) {
	s.withPipeline(w, r, func(pipeline *compiler.CompilationSession) compiler.PipelineSnapshot {
		return pipeline.Complete()
	})
}

func (s *sessionStore) stepHandler(w http.ResponseWriter, r *http.Request) {
	s.withSession(w, r, func(entry *sessionEntry, request debugSessionRequest) compiler.DebugSnapshot {
		entry.session.Step()
		return entry.session.Snapshot(request.Light, request.TraceFrom)
	})
}

func (s *sessionStore) runHandler(w http.ResponseWriter, r *http.Request) {
	s.withSession(w, r, func(entry *sessionEntry, request debugSessionRequest) compiler.DebugSnapshot {
		entry.session.Run(request.MaxSteps, request.Breakpoints)
		return entry.session.Snapshot(request.Light, request.TraceFrom)
	})
}

func (s *sessionStore) resetHandler(w http.ResponseWriter, r *http.Request) {
	s.withSession(w, r, func(entry *sessionEntry, request debugSessionRequest) compiler.DebugSnapshot {
		entry.session.Reset()
		return entry.session.Snapshot(request.Light, request.TraceFrom)
	})
}

func (s *sessionStore) disposeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "use POST"})
		return
	}
	var request debugSessionRequest
	if err := decodeJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if request.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing session id"})
		return
	}
	s.mu.Lock()
	_, found := s.sessions[request.ID]
	delete(s.sessions, request.ID)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"disposed": found})
}

func (s *sessionStore) withSession(w http.ResponseWriter, r *http.Request, action func(*sessionEntry, debugSessionRequest) compiler.DebugSnapshot) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "use POST"})
		return
	}
	var request debugSessionRequest
	if err := decodeJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if request.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing session id"})
		return
	}

	entry, found := s.lookup(request.ID)
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "debug session not found"})
		return
	}

	entry.mu.Lock()
	if !s.promoteDebugSession(entry) {
		entry.mu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "debug session unavailable"})
		return
	}
	applyOptionalInput(entry, request.Input)
	snapshot := action(entry, request)
	entry.mu.Unlock()
	writeJSON(w, http.StatusOK, debugSessionResponse{ID: request.ID, Snapshot: debugSnapshotPayload(snapshot, request.Light)})
}

func debugSnapshotPayload(snapshot compiler.DebugSnapshot, light bool) any {
	if light {
		return lightDebugSnapshot{Runtime: snapshot.Runtime, Status: snapshot.Status}
	}
	return snapshot
}

func (s *sessionStore) withPipeline(w http.ResponseWriter, r *http.Request, action func(*compiler.CompilationSession) compiler.PipelineSnapshot) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "use POST"})
		return
	}
	var request debugSessionRequest
	if err := decodeJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if request.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing session id"})
		return
	}
	entry, found := s.lookup(request.ID)
	if !found || entry.pipeline == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "compilation session not found"})
		return
	}

	entry.mu.Lock()
	applyOptionalInput(entry, request.Input)
	snapshot := action(entry.pipeline)
	if session, ok := entry.pipeline.DebugSession(); ok {
		entry.session = session
		session.SetInput(entry.input)
	}
	entry.mu.Unlock()
	writeJSON(w, http.StatusOK, pipelineResponse{ID: request.ID, Snapshot: snapshot})
}

func (s *sessionStore) promoteDebugSession(entry *sessionEntry) bool {
	if entry.session != nil {
		return true
	}
	if entry.pipeline == nil {
		return false
	}
	entry.pipeline.Complete()
	session, ok := entry.pipeline.DebugSession()
	if !ok {
		return false
	}
	entry.session = session
	entry.session.SetInput(entry.input)
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

func (s *sessionStore) insert(id string, entry *sessionEntry) {
	s.mu.Lock()
	s.pruneLocked(entry.lastUsed)
	s.sessions[id] = entry
	s.mu.Unlock()
}

func (s *sessionStore) lookup(id string) (*sessionEntry, bool) {
	now := time.Now()
	s.mu.Lock()
	entry, found := s.sessions[id]
	if found && now.Sub(entry.lastUsed) > s.ttl {
		delete(s.sessions, id)
		entry = nil
		found = false
	}
	if found {
		entry.lastUsed = now
	}
	s.mu.Unlock()
	return entry, found
}

func (s *sessionStore) pruneLocked(now time.Time) {
	for id, entry := range s.sessions {
		if now.Sub(entry.lastUsed) > s.ttl {
			delete(s.sessions, id)
		}
	}
	for len(s.sessions) >= s.max {
		oldestID := ""
		var oldest time.Time
		for id, entry := range s.sessions {
			if oldestID == "" || entry.lastUsed.Before(oldest) {
				oldestID, oldest = id, entry.lastUsed
			}
		}
		if oldestID == "" {
			break
		}
		delete(s.sessions, oldestID)
	}
}

func decodeJSON(r *http.Request, destination any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

func newSessionID() (string, error) {
	bytes := make([]byte, 16)
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
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", target)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		command = exec.Command("xdg-open", target)
	}
	return command.Start()
}
