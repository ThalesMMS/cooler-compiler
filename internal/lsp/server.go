// SPDX-License-Identifier: AGPL-3.0-or-later

package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"runtime/debug"
	"sync"
	"time"
)

const (
	maxPendingCancellations   = 256
	defaultConcurrentRequests = 64
	defaultWorkspaceRoots     = 32
	defaultWorkspaceEntries   = 100_000
	defaultContentChanges     = 4_096
)

type Config struct {
	Version               string
	MaxMessageBytes       int
	MaxOpenDocuments      int
	MaxWorkspaceFiles     int
	MaxWorkspaceRoots     int
	MaxWorkspaceEntries   int
	MaxContentChanges     int
	MaxFileBytes          int
	MaxWorkspaceBytes     int
	Debounce              time.Duration
	AnalysisTimeout       time.Duration
	WorkspaceScanTimeout  time.Duration
	MaxConcurrentRequests int
}

func DefaultConfig() Config {
	return Config{
		Version:         "1.0.0",
		MaxMessageBytes: 4 << 20, MaxOpenDocuments: 256, MaxWorkspaceFiles: 1_024,
		MaxWorkspaceRoots: defaultWorkspaceRoots, MaxWorkspaceEntries: defaultWorkspaceEntries,
		MaxContentChanges: defaultContentChanges,
		MaxFileBytes:      4 << 20, MaxWorkspaceBytes: 32 << 20,
		Debounce: 75 * time.Millisecond, AnalysisTimeout: 5 * time.Second, WorkspaceScanTimeout: 5 * time.Second,
		MaxConcurrentRequests: defaultConcurrentRequests,
	}
}

type Server struct {
	config Config
	reader *bufio.Reader
	writer io.Writer
	logger *log.Logger

	writeMu           sync.Mutex
	mu                sync.Mutex
	documents         map[string]*document
	roots             []string
	generation        uint64
	cache             *analysis
	diagnosticTimer   *time.Timer
	diagnosticCancel  context.CancelFunc
	initialized       bool
	clientInitialized bool
	shutdown          bool
	exited            bool
	requests          map[string]context.CancelFunc
	cancelled         map[string]struct{}
	cancelledOrder    []string

	analysisToken chan struct{}
	requestSlots  chan struct{}
	wait          sync.WaitGroup
}

// Run serves one stdio LSP session. It returns the process exit code required
// by the LSP shutdown/exit lifecycle.
func Run(ctx context.Context, input io.Reader, output, logs io.Writer, config Config) int {
	config = normalizeConfig(config)
	server := &Server{
		config: config, reader: bufio.NewReader(input), writer: output,
		logger: log.New(logs, "cooler-lsp: ", log.LstdFlags), documents: map[string]*document{},
		requests: map[string]context.CancelFunc{}, cancelled: map[string]struct{}{}, analysisToken: make(chan struct{}, 1),
		requestSlots: make(chan struct{}, config.MaxConcurrentRequests),
	}
	server.analysisToken <- struct{}{}
	return server.serve(ctx)
}

func normalizeConfig(config Config) Config {
	defaults := DefaultConfig()
	if config.MaxMessageBytes <= 0 {
		config.MaxMessageBytes = defaults.MaxMessageBytes
	}
	if config.Version == "" {
		config.Version = defaults.Version
	}
	if config.MaxOpenDocuments <= 0 {
		config.MaxOpenDocuments = defaults.MaxOpenDocuments
	}
	if config.MaxWorkspaceFiles <= 0 {
		config.MaxWorkspaceFiles = defaults.MaxWorkspaceFiles
	}
	if config.MaxWorkspaceRoots <= 0 {
		config.MaxWorkspaceRoots = defaults.MaxWorkspaceRoots
	}
	if config.MaxWorkspaceEntries <= 0 {
		config.MaxWorkspaceEntries = defaults.MaxWorkspaceEntries
	}
	if config.MaxContentChanges <= 0 {
		config.MaxContentChanges = defaults.MaxContentChanges
	}
	if config.MaxFileBytes <= 0 {
		config.MaxFileBytes = defaults.MaxFileBytes
	}
	if config.MaxWorkspaceBytes <= 0 {
		config.MaxWorkspaceBytes = defaults.MaxWorkspaceBytes
	}
	if config.Debounce <= 0 {
		config.Debounce = defaults.Debounce
	}
	if config.AnalysisTimeout <= 0 {
		config.AnalysisTimeout = defaults.AnalysisTimeout
	}
	if config.WorkspaceScanTimeout <= 0 {
		config.WorkspaceScanTimeout = defaults.WorkspaceScanTimeout
	}
	if config.MaxConcurrentRequests <= 0 {
		config.MaxConcurrentRequests = defaults.MaxConcurrentRequests
	}
	return config
}

func (s *Server) serve(ctx context.Context) int {
	defer s.stopBackground()
	exitReceived := false
	for {
		payload, err := readFrame(s.reader, s.config.MaxMessageBytes)
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || ctx.Err() != nil {
			break
		}
		if err != nil {
			s.logger.Printf("protocol framing error: %v", err)
			break
		}
		var message rpcMessage
		if err := json.Unmarshal(payload, &message); err != nil {
			s.respondError(json.RawMessage("null"), jsonRPCParseError, "Parse error", nil)
			continue
		}
		if message.JSONRPC != "2.0" || message.Method == "" {
			s.respondError(validResponseID(message.ID), jsonRPCInvalidRequest, "Invalid Request", nil)
			continue
		}
		if !hasRequestID(message.ID) {
			if s.handleNotification(ctx, message) {
				exitReceived = true
				break
			}
			continue
		}
		if !validRequestID(message.ID) {
			s.respondError(json.RawMessage("null"), jsonRPCInvalidRequest, "Invalid Request", nil)
			continue
		}
		if message.Method == "initialize" || message.Method == "shutdown" {
			s.handleRequestSafely(ctx, message)
			continue
		}
		s.dispatchRequest(ctx, message)
	}
	if exitReceived || ctx.Err() != nil {
		s.cancelRequests()
	}
	s.wait.Wait()
	s.mu.Lock()
	clean := s.shutdown
	s.mu.Unlock()
	if clean {
		return 0
	}
	return 1
}

func (s *Server) handleNotification(parent context.Context, message rpcMessage) bool {
	defer func() {
		if recovered := recover(); recovered != nil {
			s.logger.Printf("notification %s panic: %v\n%s", message.Method, recovered, debug.Stack())
		}
	}()
	s.mu.Lock()
	initialized, shutdown := s.initialized, s.shutdown
	s.mu.Unlock()
	if shutdown && message.Method != "exit" {
		return false
	}
	if !initialized && message.Method != "exit" && message.Method != "$/cancelRequest" {
		return false
	}
	switch message.Method {
	case "initialized":
		s.mu.Lock()
		if s.clientInitialized {
			s.mu.Unlock()
			return false
		}
		s.clientInitialized = true
		s.scheduleDiagnosticsLockedContext(parent)
		s.mu.Unlock()
		return false
	case "exit":
		s.mu.Lock()
		s.exited = true
		s.mu.Unlock()
		return true
	case "$/cancelRequest":
		var params cancelParams
		if decodeParams(message.Params, &params) == nil {
			if !validRequestID(params.ID) {
				return false
			}
			s.mu.Lock()
			key := requestKey(params.ID)
			cancel := s.requests[key]
			if cancel == nil {
				if _, exists := s.cancelled[key]; !exists {
					s.cancelled[key] = struct{}{}
					s.cancelledOrder = append(s.cancelledOrder, key)
				}
				for len(s.cancelledOrder) > maxPendingCancellations {
					oldest := s.cancelledOrder[0]
					s.cancelledOrder = s.cancelledOrder[1:]
					delete(s.cancelled, oldest)
				}
			}
			s.mu.Unlock()
			if cancel != nil {
				cancel()
			}
		}
	case "textDocument/didOpen":
		var params didOpenParams
		if decodeParams(message.Params, &params) == nil {
			s.didOpenContext(parent, params)
		}
	case "textDocument/didChange":
		var params didChangeParams
		if decodeParams(message.Params, &params) == nil {
			s.didChangeContext(parent, params)
		}
	case "textDocument/didClose":
		var params didCloseParams
		if decodeParams(message.Params, &params) == nil {
			s.didCloseContext(parent, params)
		}
	case "workspace/didChangeWatchedFiles":
		var params jsonWatchedFiles
		if decodeParams(message.Params, &params) == nil {
			s.didChangeWatchedFilesContext(parent, params)
		}
	case "workspace/didChangeConfiguration":
		s.mu.Lock()
		s.invalidateLocked()
		s.scheduleDiagnosticsLockedContext(parent)
		s.mu.Unlock()
	}
	return false
}

func (s *Server) dispatchRequest(parent context.Context, message rpcMessage) {
	select {
	case s.requestSlots <- struct{}{}:
		s.wait.Add(1)
		go func() {
			defer s.wait.Done()
			defer func() { <-s.requestSlots }()
			s.handleRequestSafely(parent, message)
		}()
	default:
		s.respondError(message.ID, jsonRPCServerBusy, "Concurrent request limit reached", nil)
	}
}

func (s *Server) handleRequestSafely(parent context.Context, message rpcMessage) {
	ctx, cancel := context.WithCancel(parent)
	key := requestKey(message.ID)
	s.mu.Lock()
	if _, exists := s.requests[key]; exists {
		s.mu.Unlock()
		cancel()
		s.respondError(message.ID, jsonRPCInvalidRequest, "Duplicate active request ID", nil)
		return
	}
	s.requests[key] = cancel
	if _, wasCancelled := s.cancelled[key]; wasCancelled {
		delete(s.cancelled, key)
		for index, cancelledKey := range s.cancelledOrder {
			if cancelledKey == key {
				s.cancelledOrder = append(s.cancelledOrder[:index], s.cancelledOrder[index+1:]...)
				break
			}
		}
		cancel()
	}
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		delete(s.requests, key)
		s.mu.Unlock()
		if recovered := recover(); recovered != nil {
			s.logger.Printf("request %s panic: %v\n%s", message.Method, recovered, debug.Stack())
			s.respondError(message.ID, jsonRPCInternalError, "Internal error", nil)
		}
	}()
	result, rpcErr := s.handleRequest(ctx, message)
	if rpcErr != nil {
		s.respondError(message.ID, rpcErr.Code, rpcErr.Message, rpcErr.Data)
		return
	}
	s.respondResult(message.ID, result)
}

func (s *Server) handleRequest(ctx context.Context, message rpcMessage) (any, *rpcError) {
	if message.Method == "initialize" {
		var params initializeParams
		if err := decodeParams(message.Params, &params); err != nil {
			return nil, &rpcError{Code: jsonRPCInvalidParams, Message: err.Error()}
		}
		s.mu.Lock()
		if s.initialized {
			s.mu.Unlock()
			return nil, &rpcError{Code: jsonRPCInvalidRequest, Message: "initialize may only be sent once"}
		}
		s.mu.Unlock()
		if err := s.initializeWorkspace(ctx, params); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, &rpcError{Code: jsonRPCRequestCancel, Message: "Request cancelled", Data: err.Error()}
			}
			return nil, &rpcError{Code: jsonRPCInvalidParams, Message: err.Error()}
		}
		s.mu.Lock()
		s.initialized = true
		s.mu.Unlock()
		return initializeResult(s.config.Version), nil
	}
	s.mu.Lock()
	initialized, shutdown := s.initialized, s.shutdown
	s.mu.Unlock()
	if !initialized {
		return nil, &rpcError{Code: jsonRPCServerNotInit, Message: "Server not initialized"}
	}
	if shutdown && message.Method != "shutdown" {
		return nil, &rpcError{Code: jsonRPCInvalidRequest, Message: "server is shutting down"}
	}
	if ctx.Err() != nil {
		return nil, &rpcError{Code: jsonRPCRequestCancel, Message: "Request cancelled"}
	}

	switch message.Method {
	case "shutdown":
		s.mu.Lock()
		s.shutdown = true
		s.invalidateLocked()
		s.mu.Unlock()
		s.cancelRequests()
		s.wait.Wait()
		return nil, nil
	case "textDocument/documentSymbol":
		return s.requestDocumentSymbols(ctx, message.Params)
	case "workspace/symbol":
		return s.requestWorkspaceSymbols(ctx, message.Params)
	case "textDocument/hover":
		return s.requestHover(ctx, message.Params)
	case "textDocument/definition":
		return s.requestDefinition(ctx, message.Params)
	case "textDocument/references":
		return s.requestReferences(ctx, message.Params)
	case "textDocument/completion":
		return s.requestCompletion(ctx, message.Params)
	case "textDocument/semanticTokens/full":
		return s.requestSemanticTokens(ctx, message.Params)
	case "textDocument/codeAction":
		return s.requestCodeActions(ctx, message.Params)
	default:
		return nil, &rpcError{Code: jsonRPCMethodNotFound, Message: fmt.Sprintf("method %q not found", message.Method)}
	}
}

func initializeResult(version string) map[string]any {
	return map[string]any{
		"serverInfo": map[string]any{"name": "cooler-lsp", "version": version},
		"capabilities": map[string]any{
			"positionEncoding":       "utf-16",
			"textDocumentSync":       map[string]any{"openClose": true, "change": 2, "save": map[string]any{"includeText": false}},
			"documentSymbolProvider": true, "workspaceSymbolProvider": true, "hoverProvider": true,
			"definitionProvider": true, "referencesProvider": true,
			"completionProvider": map[string]any{"triggerCharacters": []string{".", "@", ":"}},
			"semanticTokensProvider": map[string]any{
				"legend": map[string]any{"tokenTypes": semanticTokenTypes, "tokenModifiers": semanticTokenModifiers},
				"full":   true,
			},
			"codeActionProvider": map[string]any{"codeActionKinds": []string{"quickfix"}},
			"workspace":          map[string]any{"workspaceFolders": map[string]any{"supported": true, "changeNotifications": false}},
		},
	}
}

func (s *Server) respondResult(id json.RawMessage, result any) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := writeFrame(s.writer, map[string]any{"jsonrpc": "2.0", "id": id, "result": result}); err != nil {
		s.logger.Printf("write response: %v", err)
	}
}

func (s *Server) respondError(id json.RawMessage, code int, message string, data any) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	response := map[string]any{"jsonrpc": "2.0", "id": id, "error": &rpcError{Code: code, Message: message, Data: data}}
	if err := writeFrame(s.writer, response); err != nil {
		s.logger.Printf("write error response: %v", err)
	}
}

func (s *Server) notify(method string, params any) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := writeFrame(s.writer, rpcNotification{JSONRPC: "2.0", Method: method, Params: params}); err != nil {
		s.logger.Printf("write notification: %v", err)
	}
}

func validResponseID(id json.RawMessage) json.RawMessage {
	if !validRequestID(id) {
		return json.RawMessage("null")
	}
	return id
}

func (s *Server) invalidateLocked() {
	s.generation++
	s.cache = nil
	if s.diagnosticTimer != nil {
		if s.diagnosticTimer.Stop() {
			s.wait.Done()
		}
		s.diagnosticTimer = nil
	}
	if s.diagnosticCancel != nil {
		s.diagnosticCancel()
		s.diagnosticCancel = nil
	}
}

func (s *Server) scheduleDiagnosticsLocked() {
	s.scheduleDiagnosticsLockedContext(context.Background())
}

// scheduleDiagnosticsLockedContext derives the debounced analysis lifetime
// from the server's Run context. The lock must be held by the caller so timer
// replacement and generation capture stay atomic with workspace mutation.
func (s *Server) scheduleDiagnosticsLockedContext(parent context.Context) {
	generation := s.generation
	s.wait.Add(1)
	s.diagnosticTimer = time.AfterFunc(s.config.Debounce, func() {
		defer s.wait.Done()
		ctx, cancel := context.WithTimeout(parent, s.config.AnalysisTimeout)
		s.mu.Lock()
		if generation != s.generation {
			s.mu.Unlock()
			cancel()
			return
		}
		s.diagnosticCancel = cancel
		s.mu.Unlock()
		defer cancel()
		analysis, err := s.analyze(ctx)
		if err != nil {
			if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				s.logger.Printf("analysis: %v", err)
			}
			return
		}
		s.publishDiagnostics(analysis)
	})
}

func (s *Server) stopBackground() {
	s.mu.Lock()
	s.invalidateLocked()
	s.mu.Unlock()
	s.cancelRequests()
	s.wait.Wait()
}

func (s *Server) cancelRequests() {
	s.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(s.requests))
	for _, cancel := range s.requests {
		cancels = append(cancels, cancel)
	}
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

var semanticTokenTypes = []string{"class", "method", "property", "parameter", "variable", "type", "keyword", "string", "number", "comment", "operator"}
var semanticTokenModifiers = []string{"declaration", "definition", "readonly", "static", "defaultLibrary"}
