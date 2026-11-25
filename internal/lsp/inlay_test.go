// SPDX-License-Identifier: AGPL-3.0-or-later
package lsp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestInlayHintsResolutionRangeAndConfiguration(t *testing.T) {
	const uri = "file:///workspace/main.cl"
	source := `class Base { f(count:Int):SELF_TYPE {self}; }; class Child inherits Base {f(count:Int):SELF_TYPE {self};}; class Main { main():Object {{ "😀"; (new Child).f(7); (new Child)@Base.f(8); }};};`
	s := testServer(map[string]documentSnapshot{uri: {URI: uri, Text: source, Open: true, Version: 1}})
	s.inlayHints = inlayHintOptions{ParameterNames: true, SelfType: true}
	params := map[string]any{"textDocument": map[string]any{"uri": uri}, "range": Range{End: Position{Line: 1}}}
	hints := decodeAs[[]inlayHint](t, callFeature(t, s, s.requestInlayHints, params))
	labels := []string{}
	for _, hint := range hints {
		labels = append(labels, hint.Label)
	}
	if strings.Join(labels, "|") != "count:|: Child|count:|: Base" {
		t.Fatalf("hints %+v", hints)
	}
	if hints[0].Position != nthPosition(source, "7", 1) {
		t.Fatalf("UTF16 position %+v", hints[0])
	}
	params["range"] = Range{Start: nthPosition(source, "8", 1), End: Position{Line: 1}}
	ranged := decodeAs[[]inlayHint](t, callFeature(t, s, s.requestInlayHints, params))
	if len(ranged) != 2 {
		t.Fatalf("range %+v", ranged)
	}
	s.handleNotification(context.Background(), rpcMessage{Method: "workspace/didChangeConfiguration", Params: json.RawMessage(`{"settings":{"cooler":{"inlayHints":{"parameterNames":false,"selfType":false}}}}`)})
	disabled := decodeAs[[]inlayHint](t, callFeature(t, s, s.requestInlayHints, params))
	if len(disabled) != 0 {
		t.Fatalf("disabled hints %+v", disabled)
	}
}

func TestInlayHintsNestedAndVersion(t *testing.T) {
	mustJSON := func(value any) json.RawMessage {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	const uri = "file:///workspace/main.cl"
	source := `class Main { f(count:Int):Int {count}; main():Object {let count:Int <- 1 in f(f(count))};};`
	s := testServer(map[string]documentSnapshot{uri: {URI: uri, Text: source, Open: true, Version: 1}})
	params := map[string]any{"textDocument": map[string]any{"uri": uri}, "range": Range{End: Position{Line: 1}}}
	get := func() []inlayHint { return decodeAs[[]inlayHint](t, callFeature(t, s, s.requestInlayHints, params)) }
	if len(get()) != 0 {
		t.Fatal("hints must be opt-in")
	}
	s.sourceSet = &sourceSet{Exclude: []string{"ignored/"}}
	s.warningRules = "none"
	if err := s.updateClientConfiguration(context.Background(), json.RawMessage(`{"settings":{"cooler":{"inlayHints":{"parameterNames":true}}}}`)); err != nil {
		t.Fatal(err)
	}
	if s.sourceSet == nil || s.warningRules != "none" || s.inlayHints.SelfType {
		t.Fatal("unrelated configuration changed")
	}
	s.mu.Lock()
	s.sourceSet = nil
	s.invalidateLocked()
	s.mu.Unlock()
	hints := get()
	if len(hints) != 1 || hints[0].Label != "count:" || hints[0].Position != nthPosition(source, "f(count)", 1) {
		t.Fatalf("nested/suppressed hints %+v", hints)
	}
	s.handleNotification(context.Background(), rpcMessage{Method: "textDocument/didChange", Params: mustJSON(map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2}, "contentChanges": []map[string]string{{"text": strings.Replace(source, "f(f(count))", "f(unknown)", 1)}}})})
	if len(get()) != 0 {
		t.Fatal("unresolved new version retained hints")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, rpcErr := s.requestInlayHints(ctx, mustJSON(params)); rpcErr == nil || rpcErr.Code != jsonRPCRequestCancel {
		t.Fatalf("cancel: %+v", rpcErr)
	}
}

func TestInlayConfigurationTransactionalAndDiagnostics(t *testing.T) {
	s := testServer(nil)
	s.config.Debounce = time.Minute
	defer func() { s.mu.Lock(); s.invalidateLocked(); s.mu.Unlock(); s.wait.Wait() }()
	s.sourceSet = &sourceSet{Include: []string{"main.cl"}}
	if err := s.updateClientConfiguration(context.Background(), json.RawMessage(`{"settings":{"cooler":{"sourceSet":{"include":["../bad.cl"]}}}}`)); err == nil {
		t.Fatal("accepted invalid selection")
	}
	if s.sourceSet.Include[0] != "main.cl" {
		t.Fatal("rejected selection mutated active configuration")
	}
	s.mu.Lock()
	s.scheduleDiagnosticsLocked()
	old := s.diagnosticTimer
	s.mu.Unlock()
	if err := s.updateClientConfiguration(context.Background(), json.RawMessage(`{"settings":{"cooler":{"inlayHints":{"selfType":true}}}}`)); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	replacement := s.diagnosticTimer
	s.mu.Unlock()
	if replacement == nil || replacement == old {
		t.Fatal("hint toggle did not reschedule pending diagnostics")
	}
}

func TestUnrelatedClientConfigurationPreservesAnalysis(t *testing.T) {
	s := testServer(nil)
	defer s.stopBackground()
	value, err := s.analyze(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.updateClientConfiguration(context.Background(), json.RawMessage(`{"settings":{"cooler":{"compilerPath":"elsewhere"}}}`)); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	unchanged := s.cache == value && s.generation == value.Generation && s.diagnosticTimer == nil
	s.mu.Unlock()
	if !unchanged {
		t.Fatal("unrelated settings invalidated analysis")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.updateClientConfiguration(ctx, json.RawMessage(`{"settings":{}}`)); err == nil {
		t.Fatal("cancelled configuration accepted")
	}
}
