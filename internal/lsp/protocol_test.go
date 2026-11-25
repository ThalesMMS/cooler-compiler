// SPDX-License-Identifier: AGPL-3.0-or-later

package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestInitializeShutdownExitLifecycle(t *testing.T) {
	input := framedMessages(t,
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}},
		map[string]any{"jsonrpc": "2.0", "id": 2, "method": "shutdown"},
		map[string]any{"jsonrpc": "2.0", "method": "exit"},
	)
	var output, logs bytes.Buffer
	config := testConfig()
	config.Version = "test-version"
	if code := Run(context.Background(), input, &output, &logs, config); code != 0 {
		t.Fatalf("exit code %d; logs=%s", code, logs.String())
	}
	messages := decodeOutput(t, output.Bytes())
	if len(messages) != 2 || numberID(messages[0]) != 1 || numberID(messages[1]) != 2 {
		t.Fatalf("responses %+v", messages)
	}
	result := messages[0]["result"].(map[string]any)
	capabilities := result["capabilities"].(map[string]any)
	if capabilities["positionEncoding"] != "utf-16" || capabilities["hoverProvider"] != true || capabilities["semanticTokensProvider"] == nil {
		t.Fatalf("capabilities %+v", capabilities)
	}
	serverInfo := result["serverInfo"].(map[string]any)
	if serverInfo["name"] != "cooler-lsp" || serverInfo["version"] != "test-version" {
		t.Fatalf("server info %+v", serverInfo)
	}
	if bytes.Contains(output.Bytes(), []byte("cooler-lsp:")) {
		t.Fatal("logs leaked into protocol stdout")
	}
}

func TestInitializeRejectsTooManyRootsAndCanRetry(t *testing.T) {
	input := framedMessages(t,
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{
			"rootUri": "file:///workspace/one", "workspaceFolders": []map[string]any{{"uri": "file:///workspace/two"}},
		}},
		map[string]any{"jsonrpc": "2.0", "id": 2, "method": "initialize", "params": map[string]any{}},
		map[string]any{"jsonrpc": "2.0", "id": 3, "method": "shutdown"},
		map[string]any{"jsonrpc": "2.0", "method": "exit"},
	)
	var output bytes.Buffer
	config := testConfig()
	config.MaxWorkspaceRoots = 1
	if code := Run(context.Background(), input, &output, io.Discard, config); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	messages := decodeOutput(t, output.Bytes())
	if len(messages) != 3 || numberID(messages[0]) != 1 || int(messages[0]["error"].(map[string]any)["code"].(float64)) != jsonRPCInvalidParams || numberID(messages[1]) != 2 || numberID(messages[2]) != 3 {
		t.Fatalf("responses %+v", messages)
	}
}

func TestMalformedJSONRPCIsContained(t *testing.T) {
	var input bytes.Buffer
	input.WriteString("Content-Length: 1\r\n\r\n{")
	for _, message := range []any{
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}},
		map[string]any{"jsonrpc": "2.0", "id": 2, "method": "shutdown"},
		map[string]any{"jsonrpc": "2.0", "method": "exit"},
	} {
		if err := writeFrame(&input, message); err != nil {
			t.Fatal(err)
		}
	}
	var output bytes.Buffer
	if code := Run(context.Background(), &input, &output, io.Discard, testConfig()); code != 0 {
		t.Fatalf("exit %d", code)
	}
	messages := decodeOutput(t, output.Bytes())
	if len(messages) != 3 || int(messages[0]["error"].(map[string]any)["code"].(float64)) != jsonRPCParseError {
		t.Fatalf("responses %+v", messages)
	}
}

func TestProtocolPublishesVersionedDiagnostics(t *testing.T) {
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	done := make(chan int, 1)
	config := testConfig()
	config.Debounce = time.Millisecond
	go func() {
		done <- Run(context.Background(), inputReader, outputWriter, io.Discard, config)
		_ = outputWriter.Close()
	}()
	reader := bufio.NewReader(outputReader)
	writeProtocol := func(value any) {
		t.Helper()
		if err := writeFrame(inputWriter, value); err != nil {
			t.Fatal(err)
		}
	}
	readProtocol := func() map[string]any {
		t.Helper()
		result := make(chan map[string]any, 1)
		go func() {
			payload, err := readFrame(reader, config.MaxMessageBytes)
			if err != nil {
				result <- map[string]any{"readError": err.Error()}
				return
			}
			var decoded map[string]any
			_ = json.Unmarshal(payload, &decoded)
			result <- decoded
		}()
		select {
		case decoded := <-result:
			return decoded
		case <-time.After(5 * time.Second):
			t.Fatal("timed out reading protocol message")
			return nil
		}
	}

	writeProtocol(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	_ = readProtocol()
	uri := "file:///workspace/main.cl"
	writeProtocol(map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "cool", "version": 7, "text": `class Main { main() : Int { missing }; };`,
	}}})
	diagnostics := readProtocol()
	if diagnostics["method"] != "textDocument/publishDiagnostics" {
		t.Fatalf("message %+v", diagnostics)
	}
	params := diagnostics["params"].(map[string]any)
	if int(params["version"].(float64)) != 7 || len(params["diagnostics"].([]any)) == 0 {
		t.Fatalf("diagnostics %+v", params)
	}
	writeProtocol(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "shutdown"})
	_ = readProtocol()
	writeProtocol(map[string]any{"jsonrpc": "2.0", "method": "exit"})
	_ = inputWriter.Close()
	if code := <-done; code != 0 {
		t.Fatalf("exit %d", code)
	}
}

func TestCancellationNotificationProducesRequestCancelled(t *testing.T) {
	server := testServer(map[string]documentSnapshot{
		"file:///workspace/main.cl": {URI: "file:///workspace/main.cl", Version: 1, Text: mainSource, Open: true},
	})
	var output bytes.Buffer
	server.writer = &output
	id := json.RawMessage("99")
	server.handleNotification(context.Background(), rpcMessage{JSONRPC: "2.0", Method: "$/cancelRequest", Params: json.RawMessage(`{"id":99}`)})
	server.handleRequestSafely(context.Background(), rpcMessage{JSONRPC: "2.0", ID: id, Method: "workspace/symbol", Params: json.RawMessage(`{"query":""}`)})
	messages := decodeOutput(t, output.Bytes())
	if len(messages) != 1 || int(messages[0]["error"].(map[string]any)["code"].(float64)) != jsonRPCRequestCancel {
		t.Fatalf("responses %+v", messages)
	}
}

func TestFrameSizeLimit(t *testing.T) {
	reader := bufio.NewReader(bytes.NewBufferString("Content-Length: 9\r\n\r\n123456789"))
	if _, err := readFrame(reader, 8); !errors.Is(err, errMessageTooLarge) {
		t.Fatalf("error %v", err)
	}
}

type countingReader struct {
	reader io.Reader
	read   int
}

func (r *countingReader) Read(buffer []byte) (int, error) {
	count, err := r.reader.Read(buffer)
	r.read += count
	return count, err
}

func TestHeaderLimitStopsBeforeReadingUnboundedLine(t *testing.T) {
	source := &countingReader{reader: io.MultiReader(
		strings.NewReader("X-Long: "),
		io.LimitReader(zeroReader{}, 8<<20),
	)}
	reader := bufio.NewReaderSize(source, 4<<10)
	if _, err := readFrame(reader, 4<<20); !errors.Is(err, errHeadersTooLarge) {
		t.Fatalf("error %v", err)
	}
	if source.read > maxLSPHeaderBytes+reader.Size() {
		t.Fatalf("read %d bytes for a %d-byte header limit", source.read, maxLSPHeaderBytes)
	}
}

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = 'x'
	}
	return len(buffer), nil
}

func TestDuplicateContentLengthIsRejected(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("Content-Length: 2\r\nContent-Length: 2\r\n\r\n{}"))
	if _, err := readFrame(reader, 32); err == nil || !strings.Contains(err.Error(), "duplicate Content-Length") {
		t.Fatalf("error %v", err)
	}
}

func TestRequestIDValidationBoundsCancellationStorage(t *testing.T) {
	server := testServer(nil)
	oversized := json.RawMessage(`"` + strings.Repeat("x", maxRequestIDBytes) + `"`)
	params, err := json.Marshal(cancelParams{ID: oversized})
	if err != nil {
		t.Fatal(err)
	}
	server.handleNotification(context.Background(), rpcMessage{JSONRPC: "2.0", Method: "$/cancelRequest", Params: params})
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.cancelled) != 0 || len(server.cancelledOrder) != 0 {
		t.Fatalf("oversized cancellation retained: map=%d order=%d", len(server.cancelled), len(server.cancelledOrder))
	}
}

func TestInvalidRequestIDsAreRejected(t *testing.T) {
	for _, id := range []json.RawMessage{json.RawMessage("null"), json.RawMessage("1.5"), json.RawMessage(`{"nested":1}`), json.RawMessage(`[]`)} {
		if validRequestID(id) {
			t.Errorf("accepted invalid LSP request ID %s", id)
		}
	}
	for _, id := range []json.RawMessage{json.RawMessage("0"), json.RawMessage("-7"), json.RawMessage(`"request-7"`)} {
		if !validRequestID(id) {
			t.Errorf("rejected valid LSP request ID %s", id)
		}
	}
	if got := string(validResponseID(json.RawMessage(`{"nested":1}`))); got != "null" {
		t.Fatalf("invalid response ID = %s", got)
	}
}

func TestNullIDCannotActAsExitNotification(t *testing.T) {
	input := framedMessages(t,
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}},
		map[string]any{"jsonrpc": "2.0", "id": nil, "method": "exit"},
		map[string]any{"jsonrpc": "2.0", "id": 2, "method": "shutdown"},
		map[string]any{"jsonrpc": "2.0", "method": "exit"},
	)
	var output bytes.Buffer
	if code := Run(context.Background(), input, &output, io.Discard, testConfig()); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	messages := decodeOutput(t, output.Bytes())
	if len(messages) != 3 || int(messages[1]["error"].(map[string]any)["code"].(float64)) != jsonRPCInvalidRequest || numberID(messages[2]) != 2 {
		t.Fatalf("responses %+v", messages)
	}
}

func TestOversizedRequestIDGetsBoundedInvalidRequestResponse(t *testing.T) {
	oversized := strings.Repeat("x", maxRequestIDBytes)
	input := framedMessages(t,
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}},
		map[string]any{"jsonrpc": "2.0", "id": oversized, "method": "workspace/symbol", "params": map[string]any{"query": ""}},
		map[string]any{"jsonrpc": "2.0", "id": 2, "method": "shutdown"},
		map[string]any{"jsonrpc": "2.0", "method": "exit"},
	)
	var output bytes.Buffer
	if code := Run(context.Background(), input, &output, io.Discard, testConfig()); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	messages := decodeOutput(t, output.Bytes())
	if len(messages) != 3 || messages[1]["id"] != nil || int(messages[1]["error"].(map[string]any)["code"].(float64)) != jsonRPCInvalidRequest {
		t.Fatalf("responses %+v", messages)
	}
}

func TestDuplicateActiveRequestIDIsRejected(t *testing.T) {
	server := testServer(nil)
	server.requests["7"] = func() {}
	var output bytes.Buffer
	server.writer = &output
	server.handleRequestSafely(context.Background(), rpcMessage{JSONRPC: "2.0", ID: json.RawMessage("7"), Method: "workspace/symbol"})
	messages := decodeOutput(t, output.Bytes())
	if len(messages) != 1 || int(messages[0]["error"].(map[string]any)["code"].(float64)) != jsonRPCInvalidRequest {
		t.Fatalf("responses %+v", messages)
	}
	if len(server.requests) != 1 || server.requests["7"] == nil {
		t.Fatalf("duplicate request disturbed active request map: %+v", server.requests)
	}
}

type errorWriter struct{ err error }

func (writer errorWriter) Write([]byte) (int, error) { return 0, writer.err }

func TestProtocolContainsOutputWriterFailure(t *testing.T) {
	input := framedMessages(t,
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}},
		map[string]any{"jsonrpc": "2.0", "id": 2, "method": "shutdown"},
		map[string]any{"jsonrpc": "2.0", "method": "exit"},
	)
	var logs bytes.Buffer
	if code := Run(context.Background(), input, errorWriter{err: io.ErrClosedPipe}, &logs, testConfig()); code != 0 {
		t.Fatalf("exit code %d; logs=%s", code, logs.String())
	}
	if !strings.Contains(logs.String(), io.ErrClosedPipe.Error()) {
		t.Fatalf("writer failure was not contained and logged: %q", logs.String())
	}
}

func TestProtocolEmitsNoBackgroundWritesAfterShutdown(t *testing.T) {
	input := framedMessages(t,
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}},
		map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{"textDocument": map[string]any{"uri": "file:///workspace/before.cl", "version": 1, "text": "class Before {};"}}},
		map[string]any{"jsonrpc": "2.0", "id": 2, "method": "shutdown"},
		map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{"textDocument": map[string]any{"uri": "file:///workspace/after.cl", "version": 1, "text": "class After {};"}}},
		map[string]any{"jsonrpc": "2.0", "method": "exit"},
	)
	var output bytes.Buffer
	config := testConfig()
	config.Debounce = time.Hour
	if code := Run(context.Background(), input, &output, io.Discard, config); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	messages := decodeOutput(t, output.Bytes())
	if len(messages) != 2 || numberID(messages[0]) != 1 || numberID(messages[1]) != 2 {
		t.Fatalf("post-shutdown output %+v", messages)
	}
}

func TestShutdownResponseFollowsActiveRequestDrain(t *testing.T) {
	server := testServer(nil)
	<-server.analysisToken
	var output bytes.Buffer
	server.writer = &output
	server.dispatchRequest(context.Background(), rpcMessage{JSONRPC: "2.0", ID: json.RawMessage("7"), Method: "workspace/symbol", Params: json.RawMessage(`{"query":""}`)})
	deadline := time.Now().Add(5 * time.Second)
	for {
		server.mu.Lock()
		active := server.requests["7"] != nil
		server.mu.Unlock()
		if active {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("request did not become active")
		}
		time.Sleep(time.Millisecond)
	}
	server.handleRequestSafely(context.Background(), rpcMessage{JSONRPC: "2.0", ID: json.RawMessage("8"), Method: "shutdown"})
	messages := decodeOutput(t, output.Bytes())
	if len(messages) != 2 || numberID(messages[0]) != 7 || numberID(messages[1]) != 8 {
		t.Fatalf("shutdown ordering %+v", messages)
	}
	// Registration precedes the request's shutdown check. Depending on when
	// shutdown runs, the tracked request is either cancelled during analysis
	// or rejected before analysis; both must be drained before response 8.
	requestError, ok := messages[0]["error"].(map[string]any)
	if !ok {
		t.Fatalf("missing error object in response: %+v", messages[0])
	}
	codeValue, ok := requestError["code"].(float64)
	if !ok {
		t.Fatalf("missing numeric error code in response: %+v", messages[0])
	}
	code := int(codeValue)
	if code != jsonRPCRequestCancel && !(code == jsonRPCInvalidRequest && requestError["message"] == "server is shutting down") {
		t.Fatalf("unexpected drained request response %+v", messages[0])
	}
}

func TestConcurrentRequestLimitRejectsWithoutSpawning(t *testing.T) {
	server := testServer(nil)
	for index := 0; index < cap(server.requestSlots); index++ {
		server.requestSlots <- struct{}{}
	}
	var output bytes.Buffer
	server.writer = &output
	server.dispatchRequest(context.Background(), rpcMessage{JSONRPC: "2.0", ID: json.RawMessage("7"), Method: "workspace/symbol"})
	messages := decodeOutput(t, output.Bytes())
	if len(messages) != 1 || int(messages[0]["error"].(map[string]any)["code"].(float64)) != jsonRPCServerBusy {
		t.Fatalf("responses %+v", messages)
	}
	if len(server.requests) != 0 {
		t.Fatalf("request was spawned despite full limit: %+v", server.requests)
	}
}

func TestShutdownInvalidatesAndIgnoresFurtherNotifications(t *testing.T) {
	server := testServer(nil)
	server.config.Debounce = time.Hour
	server.mu.Lock()
	server.scheduleDiagnosticsLocked()
	generation := server.generation
	server.mu.Unlock()
	result, rpcErr := server.handleRequest(context.Background(), rpcMessage{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "shutdown"})
	if rpcErr != nil || result != nil {
		t.Fatalf("shutdown result=%v error=%+v", result, rpcErr)
	}
	server.handleNotification(context.Background(), rpcMessage{JSONRPC: "2.0", Method: "textDocument/didOpen", Params: json.RawMessage(`{"textDocument":{"uri":"file:///workspace/late.cl","version":1,"text":"class Late {};"}}`)})
	server.mu.Lock()
	defer server.mu.Unlock()
	if !server.shutdown || server.generation != generation+1 || server.diagnosticTimer != nil || server.documents["file:///workspace/late.cl"] != nil {
		t.Fatalf("post-shutdown state shutdown=%v generation=%d timer=%v documents=%+v", server.shutdown, server.generation, server.diagnosticTimer, server.documents)
	}
}

func TestDocumentNotificationsAreIgnoredBeforeInitialize(t *testing.T) {
	server := testServer(nil)
	server.initialized = false
	server.handleNotification(context.Background(), rpcMessage{JSONRPC: "2.0", Method: "textDocument/didOpen", Params: json.RawMessage(`{"textDocument":{"uri":"file:///workspace/early.cl","version":1,"text":"class Early {};"}}`)})
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.documents) != 0 || server.diagnosticTimer != nil {
		t.Fatalf("pre-initialize notification mutated state: documents=%+v timer=%v", server.documents, server.diagnosticTimer)
	}
}

func TestDuplicateInitializedNotificationDoesNotRetainAnotherCallback(t *testing.T) {
	server := testServer(nil)
	server.config.Debounce = time.Hour
	message := rpcMessage{JSONRPC: "2.0", Method: "initialized"}
	server.handleNotification(context.Background(), message)
	server.mu.Lock()
	first := server.diagnosticTimer
	server.mu.Unlock()
	server.handleNotification(context.Background(), message)
	server.mu.Lock()
	duplicate := server.diagnosticTimer
	ready := server.clientInitialized
	server.mu.Unlock()
	if !ready || first == nil || duplicate != first {
		t.Fatalf("duplicate initialized callback first=%p duplicate=%p ready=%v", first, duplicate, ready)
	}
	server.stopBackground()
}

func framedMessages(t *testing.T, messages ...any) *bytes.Reader {
	t.Helper()
	var framed bytes.Buffer
	for _, message := range messages {
		if err := writeFrame(&framed, message); err != nil {
			t.Fatal(err)
		}
	}
	return bytes.NewReader(framed.Bytes())
}

func decodeOutput(t *testing.T, output []byte) []map[string]any {
	t.Helper()
	reader := bufio.NewReader(bytes.NewReader(output))
	result := []map[string]any{}
	for {
		payload, err := readFrame(reader, 4<<20)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		var message map[string]any
		if err := json.Unmarshal(payload, &message); err != nil {
			t.Fatal(err)
		}
		result = append(result, message)
	}
	return result
}

func numberID(message map[string]any) int { return int(message["id"].(float64)) }

func testConfig() Config {
	config := DefaultConfig()
	config.Debounce = time.Hour
	config.AnalysisTimeout = 5 * time.Second
	return config
}

func TestDefaultConfigUsesReleaseVersion(t *testing.T) {
	if version := DefaultConfig().Version; version != "1.0.0" {
		t.Fatalf("version = %q", version)
	}
}

func TestLateCancellationSetIsBounded(t *testing.T) {
	server := testServer(nil)
	for id := 0; id < maxPendingCancellations*2; id++ {
		params, err := json.Marshal(cancelParams{ID: json.RawMessage(strconv.Itoa(id))})
		if err != nil {
			t.Fatal(err)
		}
		server.handleNotification(context.Background(), rpcMessage{JSONRPC: "2.0", Method: "$/cancelRequest", Params: params})
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.cancelled) != maxPendingCancellations || len(server.cancelledOrder) != maxPendingCancellations {
		t.Fatalf("late cancellations map=%d order=%d", len(server.cancelled), len(server.cancelledOrder))
	}
}

func TestStopBackgroundInvalidatesDiagnostics(t *testing.T) {
	server := testServer(nil)
	server.mu.Lock()
	generation := server.generation
	server.scheduleDiagnosticsLocked()
	server.mu.Unlock()

	server.stopBackground()
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.generation != generation+1 || server.diagnosticTimer != nil || server.diagnosticCancel != nil {
		t.Fatalf("shutdown state generation=%d timer=%v cancel=%v", server.generation, server.diagnosticTimer, server.diagnosticCancel)
	}
}
