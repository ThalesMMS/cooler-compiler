// SPDX-License-Identifier: AGPL-3.0-or-later

package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type latencySamples struct {
	mu           sync.Mutex
	values       map[string][]float64
	queueEntered chan struct{}
}

func (r *latencySamples) record(stage string, elapsed time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if stage == "queue-enter" {
		select {
		case r.queueEntered <- struct{}{}:
		default:
		}
		return
	}
	// Fixed-operation baselines stay below this bound. Refuse unbounded storage
	// in arbitrarily long user-invoked benchmarks; percentiles use first samples.
	if len(r.values[stage]) < 8192 {
		r.values[stage] = append(r.values[stage], float64(elapsed))
	}
}

type benchmarkMessage struct {
	ID     json.RawMessage          `json:"id"`
	Method string                   `json:"method"`
	Params publishDiagnosticsParams `json:"params"`
	Error  *rpcError                `json:"error"`
}

// The production server serializes writes. Diagnostic writes additionally hold
// s.mu, allowing this transport witness to compare emitted and current versions
// at publication time, rather than incorrectly rejecting earlier valid frames
// merely because a later edit reached the client first.
type benchmarkTransport struct {
	server    *Server
	cancelled <-chan struct{}
	buffer    []byte
	messages  chan benchmarkMessage
	stale     atomic.Int64
}

func (w *benchmarkTransport) Write(data []byte) (int, error) {
	w.buffer = append(w.buffer, data...)
	for {
		end := bytes.Index(w.buffer, []byte("\r\n\r\n"))
		if end < 0 {
			break
		}
		var length int
		if _, err := fmt.Sscanf(string(w.buffer[:end]), "Content-Length: %d", &length); err != nil {
			return 0, err
		}
		if len(w.buffer) < end+4+length {
			break
		}
		var message benchmarkMessage
		if err := json.Unmarshal(w.buffer[end+4:end+4+length], &message); err != nil {
			return 0, err
		}
		w.buffer = w.buffer[end+4+length:]
		if message.Method == "textDocument/publishDiagnostics" {
			doc := w.server.documents[message.Params.URI]
			if doc == nil || message.Params.Version == nil || *message.Params.Version != doc.Version {
				w.stale.Add(1)
			}
		}
		select {
		case w.messages <- message:
		case <-w.cancelled:
			return 0, context.Canceled
		}
	}
	return len(data), nil
}

type measuredLSPSession struct {
	t         testing.TB
	s         *Server
	cancelled <-chan struct{}
	input     *io.PipeWriter
	transport *benchmarkTransport
	done      chan int
	samples   *latencySamples
}

func newMeasuredLSPSession(t testing.TB) *measuredLSPSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	reader, writer := io.Pipe()
	s := testServer(nil)
	s.initialized = false
	s.config = DefaultConfig()
	s.config.Debounce = 2 * time.Millisecond
	s.reader = bufio.NewReader(reader)
	records := &latencySamples{values: map[string][]float64{}, queueEntered: make(chan struct{}, 512)}
	s.config.measurement = &measurementProbe{record: records.record, clock: newBenchmarkClock(t)}
	transport := &benchmarkTransport{server: s, cancelled: ctx.Done(), messages: make(chan benchmarkMessage, 512)}
	s.writer = transport
	session := &measuredLSPSession{t: t, s: s, cancelled: ctx.Done(), input: writer, transport: transport, done: make(chan int, 1), samples: records}
	stop := context.AfterFunc(ctx, func() { _ = reader.Close(); _ = writer.Close() })
	go func() { session.done <- s.serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = writer.Close()
		_ = reader.Close()
		stop()
		select {
		case <-session.done:
		case <-time.After(5 * time.Second):
			t.Error("LSP benchmark did not stop")
		}
	})
	session.send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	session.next(func(m benchmarkMessage) bool { return string(m.ID) == "1" })
	session.send(map[string]any{"jsonrpc": "2.0", "method": "initialized", "params": map[string]any{}})
	return session
}

func (s *measuredLSPSession) send(message any) {
	s.t.Helper()
	if err := writeFrame(s.input, message); err != nil {
		s.t.Fatal(err)
	}
}

func (s *measuredLSPSession) next(match func(benchmarkMessage) bool) benchmarkMessage {
	s.t.Helper()
	for {
		select {
		case message := <-s.transport.messages:
			if match(message) {
				return message
			}
		case <-s.cancelled:
			s.t.Fatal("LSP benchmark deadline exceeded")
			return benchmarkMessage{}
		}
	}
}

func (s *measuredLSPSession) change(uri, text string, version int) {
	s.send(map[string]any{"jsonrpc": "2.0", "method": "textDocument/didChange", "params": didChangeParams{
		TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: version}, ContentChanges: []contentChange{{Text: text}},
	}})
}

func (s *measuredLSPSession) hover(id int, uri string) {
	s.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": "textDocument/hover", "params": TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri}, Position: Position{Character: 6},
	}})
}

func measureLSPSession(t testing.TB, b *testing.B, files int, scenario string, iterations int) *latencySamples {
	t.Helper()
	session := newMeasuredLSPSession(t)
	mainURI := "file:///benchmark/zmain.cl"
	mainText := "class Main {main():Object {(new Source0).value()};};"
	helper := func(i int) string {
		return fmt.Sprintf("class Source%d {value():Int {{%s1;}};};", i, strings.Repeat("1;", 32))
	}
	sourceBytes := len(mainText)
	for i := 0; i < files; i++ {
		text := helper(i)
		sourceBytes += len(text)
		session.send(map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": didOpenParams{TextDocument: TextDocumentItem{URI: fmt.Sprintf("file:///benchmark/source%d.cl", i), Version: 1, Text: text}}})
	}
	session.send(map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": didOpenParams{TextDocument: TextDocumentItem{URI: mainURI, Version: 1, Text: mainText}}})
	session.next(func(m benchmarkMessage) bool {
		return m.Method == "textDocument/publishDiagnostics" && m.Params.URI == mainURI && m.Params.Version != nil && *m.Params.Version == 1
	})
	// Synchronize with completion of the write probe before discarding warmup.
	session.s.mu.Lock()
	session.s.writeMu.Lock()
	session.samples.mu.Lock()
	session.samples.values = map[string][]float64{}
	for len(session.samples.queueEntered) > 0 {
		<-session.samples.queueEntered
	}
	session.samples.mu.Unlock()
	session.s.writeMu.Unlock()
	session.s.mu.Unlock()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	if b != nil {
		b.ReportAllocs()
		b.ResetTimer()
	}
	version := 1
	for iteration := 0; iteration < iterations; iteration++ {
		start := session.s.config.measurement.clock()
		requestID := iteration + 10
		switch scenario {
		case "cache":
			session.hover(requestID, mainURI)
			response := session.next(func(m benchmarkMessage) bool { return string(m.ID) == fmt.Sprint(requestID) })
			if response.Error != nil {
				t.Fatalf("cached hover: %+v", response.Error)
			}
		case "cancel":
			// An occupied analysis slot deterministically puts the request in
			// the queue; cancellation must finish without releasing that slot.
			select {
			case <-session.s.analysisToken:
			case <-session.cancelled:
				t.Fatal("LSP benchmark deadline exceeded")
			}
			session.hover(requestID, mainURI)
			select {
			case <-session.samples.queueEntered:
			case <-session.cancelled:
				t.Fatal("request did not enter analysis queue")
			}
			session.send(map[string]any{"jsonrpc": "2.0", "method": "$/cancelRequest", "params": map[string]any{"id": requestID}})
			response := session.next(func(m benchmarkMessage) bool { return string(m.ID) == fmt.Sprint(requestID) })
			session.s.analysisToken <- struct{}{}
			if response.Error == nil || response.Error.Code != jsonRPCRequestCancel {
				t.Fatalf("cancel response: %+v", response)
			}
		default:
			session.s.mu.Lock()
			previous := session.s.cache
			session.s.mu.Unlock()
			if scenario == "multifile" {
				session.change("file:///benchmark/source0.cl", helper(0)+fmt.Sprintf("\n-- %d", iteration), iteration+2)
			}
			for burst := 0; burst < 4; burst++ {
				version++
				session.change(mainURI, mainText+fmt.Sprintf("\n-- %d", version), version)
			}
			// Exercise rejection of an analysis captured before the burst while
			// fresh analysis/publication and subsequent requests compete.
			session.s.publishDiagnostics(previous)
			requests := 0
			if scenario == "hover" {
				for j := 0; j < 3; j++ {
					session.hover(requestID*10+j, mainURI)
					requests++
				}
			}
			diagnostics := false
			session.next(func(m benchmarkMessage) bool {
				if len(m.ID) > 0 {
					if m.Error != nil {
						t.Errorf("concurrent hover: %+v", m.Error)
					}
					requests--
				}
				if m.Method == "textDocument/publishDiagnostics" && m.Params.URI == mainURI && m.Params.Version != nil && *m.Params.Version == version {
					diagnostics = true
				}
				return diagnostics && requests == 0
			})
		}
		session.samples.record("end-to-end", session.s.config.measurement.clock()-start)
	}
	if b != nil {
		b.StopTimer()
	}
	runtime.ReadMemStats(&after)
	// Ensure all publication callbacks have completed before reporting.
	session.s.mu.Lock()
	session.s.writeMu.Lock()
	stale := session.transport.stale.Load()
	session.s.writeMu.Unlock()
	session.s.mu.Unlock()
	if stale != 0 {
		t.Fatalf("published %d obsolete diagnostics", stale)
	}
	if b != nil {
		b.ReportMetric(float64(sourceBytes), "source-bytes/op")
		b.ReportMetric(float64(files+1), "source-units/op")
		b.ReportMetric(float64(before.HeapAlloc), "heap-start-bytes")
		b.ReportMetric(float64(after.HeapAlloc), "heap-end-bytes")
		b.ReportMetric(float64(after.TotalAlloc-before.TotalAlloc)/float64(iterations), "session-allocated-bytes/op")
		b.ReportMetric(0, "stale-publications/op")
		session.samples.mu.Lock()
		for stage, values := range session.samples.values {
			sort.Float64s(values)
			mean := 0.0
			for _, value := range values {
				mean += value
			}
			mean /= float64(len(values))
			variance := 0.0
			for _, value := range values {
				variance += (value - mean) * (value - mean)
			}
			variance /= float64(len(values))
			b.ReportMetric(float64(len(values))/float64(iterations), stage+"-samples/op")
			if stage == "cache-hit" {
				continue
			}
			b.ReportMetric(mean, stage+"-mean-ns")
			b.ReportMetric(math.Sqrt(variance), stage+"-stddev-ns")
			b.ReportMetric(values[(len(values)-1)/2], stage+"-p50-ns")
			b.ReportMetric(values[int(math.Ceil(float64(len(values))*.95))-1], stage+"-p95-ns")
		}
		session.samples.mu.Unlock()
	}
	return session.samples
}

func BenchmarkLSPEditSession(b *testing.B) {
	for _, files := range []int{1, 32} {
		for _, scenario := range []string{"typing", "multifile", "hover", "cancel", "cache"} {
			b.Run(fmt.Sprintf("%d-files/%s", files+1, scenario), func(b *testing.B) { measureLSPSession(b, b, files, scenario, b.N) })
		}
	}
}

func TestLSPMeasurementScenarios(t *testing.T) {
	for _, scenario := range []string{"typing", "multifile", "hover", "cancel", "cache"} {
		t.Run(scenario, func(t *testing.T) {
			samples := measureLSPSession(t, nil, 2, scenario, 2)
			samples.mu.Lock()
			defer samples.mu.Unlock()
			if len(samples.values["end-to-end"]) != 2 {
				t.Fatal("missing end-to-end samples")
			}
			if scenario == "cache" && (len(samples.values["compile"]) != 0 || len(samples.values["cache-hit"]) != 2) {
				t.Fatal("cached hover recompiled")
			}
			if scenario == "cancel" && len(samples.values["queue"]) != 2 {
				t.Fatal("cancellation did not traverse queue")
			}
			if scenario == "typing" || scenario == "multifile" {
				for _, stage := range []string{"debounce", "queue", "compile", "report", "index", "diagnostic-conversion", "serialization", "publication-write"} {
					if len(samples.values[stage]) == 0 {
						t.Errorf("missing %s", stage)
					}
				}
			}
		})
	}
}
