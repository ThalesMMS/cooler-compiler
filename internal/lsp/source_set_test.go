// SPDX-License-Identifier: AGPL-3.0-or-later

package lsp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSourceSetSeparatesExercisesAndPreservesBuffers(t *testing.T) {
	root := t.TempDir()
	for name, text := range map[string]string{
		"one/main.cl":    "class Main inherits Shared { main():Object { self }; };",
		"two/main.cl":    "class Main inherits Shared { main():Object { self }; };",
		"shared/base.cl": "class Shared {};",
		"one/ignored.cl": "class Main {};",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	server := testServer(nil)
	server.config.Debounce = time.Hour
	var output bytes.Buffer
	server.writer = &output
	defer server.stopBackground()
	var params initializeParams
	raw, _ := json.Marshal(map[string]any{"rootUri": uriFromPath(root), "initializationOptions": map[string]any{"sourceSet": map[string]any{"include": []string{"one/", "shared/"}, "exclude": []string{"one/ignored.cl"}}}})
	if err := json.Unmarshal(raw, &params); err != nil {
		t.Fatal(err)
	}
	if err := server.initializeWorkspace(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	first, err := server.analyze(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.Result.HasErrors() || len(first.Documents) != 2 {
		t.Fatalf("selected documents=%d diagnostics=%+v", len(first.Documents), first.Report.Diagnostics)
	}
	uri := uriFromPath(filepath.Join(root, "two/main.cl"))
	edited := "class Main inherits Shared { main():Object { 123 }; };"
	server.didOpen(didOpenParams{TextDocument: TextDocumentItem{URI: uri, Version: 7, Text: edited}})
	stillFirst, err := server.analyze(context.Background())
	if err != nil || len(stillFirst.Documents) != 2 || stillFirst.Result.HasErrors() {
		t.Fatalf("inactive buffer contaminated analysis: %+v %v", stillFirst, err)
	}
	server.initialized = true
	server.clientInitialized = true
	server.handleNotification(context.Background(), rpcMessage{Method: "workspace/didChangeConfiguration", Params: json.RawMessage(`{"settings":{"cooler":{"sourceSet":{"include":["two/","shared/"]}}}}`)})
	second, err := server.analyze(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.Generation == first.Generation || second.Result.HasErrors() || len(second.Documents) != 2 || second.Documents[uri].Text != edited || second.Documents[uri].Version != 7 {
		t.Fatalf("switch lost selection or buffer: %+v", second)
	}
	oldURI := uriFromPath(filepath.Join(root, "one/main.cl"))
	cleared := false
	for _, message := range decodeOutput(t, output.Bytes()) {
		params := message["params"].(map[string]any)
		if params["uri"] == oldURI && len(params["diagnostics"].([]any)) == 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("old exercise diagnostics were not cleared")
	}
	before := output.Len()
	server.publishDiagnostics(first)
	if output.Len() != before {
		t.Fatal("stale analysis republished old diagnostics")
	}
	server.didClose(didCloseParams{TextDocument: TextDocumentIdentifier{URI: uri}})
	if server.documents[uri].Open || server.documents[uri].Text == edited {
		t.Fatal("active close did not reload disk")
	}
}

func TestChangeSourceSetPreservesWarningRules(t *testing.T) {
	s := testServer(nil)
	defer s.stopBackground()
	s.warningRules = "COL3180"
	if err := s.changeSourceSet(context.Background(), &sourceSet{Include: []string{"main.cl"}}); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.warningRules != "COL3180" {
		t.Fatalf("warning rules reset: %q", s.warningRules)
	}
}

func TestSourceSetPathsAndLimits(t *testing.T) {
	root := t.TempDir()
	set := &sourceSet{Include: []string{"exercise/", "shared.cl"}, Exclude: []string{"exercise/skip/"}}
	for name, want := range map[string]bool{"exercise/main.cl": true, "exercise/sub/a.cool": true, "exercise/skip/a.cl": false, "exercises/main.cl": false, "shared.cl": true, "other.cl": false, "../outside.cl": false} {
		if got := set.matches(filepath.Join(root, filepath.FromSlash(name)), []string{root}); got != want {
			t.Errorf("%s: got %v want %v", name, got, want)
		}
	}
	for _, entry := range []string{"", "../a.cl", "a/../b.cl", "/a.cl", "C:/a.cl", `a\b.cl`, "*.cl", ".", "./a.cl"} {
		if err := (&sourceSet{Include: []string{entry}}).validate(); err == nil {
			t.Errorf("accepted %q", entry)
		}
	}
	if err := (&sourceSet{Include: make([]string, 257)}).validate(); err == nil {
		t.Fatal("accepted excess entries")
	}
}

func TestSourceSetWatchSwitchFailureAndUnsavedFile(t *testing.T) {
	root := t.TempDir()
	server := testServer(nil)
	defer server.stopBackground()
	server.roots = []string{root}
	server.sourceSet = &sourceSet{Include: []string{"active/"}}
	server.config.Debounce = time.Hour
	write := func(name string) string {
		t.Helper()
		file := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("class Example {};"), 0600); err != nil {
			t.Fatal(err)
		}
		return uriFromPath(file)
	}
	watch := func(uri string, kind int) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"changes": []any{map[string]any{"uri": uri, "type": kind}}})
		var params jsonWatchedFiles
		if err := json.Unmarshal(raw, &params); err != nil {
			t.Fatal(err)
		}
		server.didChangeWatchedFiles(params)
	}
	active, inactive := write("active/new.cl"), write("inactive/new.cl")
	watch(inactive, 1)
	watch(active, 1)
	if len(server.documents) != 1 || server.documents[active] == nil {
		t.Fatal("watch ignored selection")
	}
	watch(active, 3)
	if len(server.documents) != 0 {
		t.Fatal("watch deletion retained source")
	}
	unsaved := uriFromPath(filepath.Join(root, "active/unsaved.cl"))
	server.didOpen(didOpenParams{TextDocument: TextDocumentItem{URI: unsaved, Version: 1, Text: "class Main { main():Object { 0 }; };"}})
	_, snapshots := server.snapshotDocuments()
	if len(snapshots) != 1 || snapshots[0].URI != unsaved {
		t.Fatal("unsaved selected file absent")
	}
	generation := server.generation
	server.config.MaxWorkspaceFiles = 1
	if err := server.changeSourceSet(context.Background(), nil); err == nil {
		t.Fatal("over-limit configuration accepted")
	}
	if server.generation != generation || server.documents[unsaved] == nil {
		t.Fatal("failed switch mutated workspace")
	}
	server.config.MaxWorkspaceFiles = 10
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := server.changeSourceSet(ctx, nil); err == nil {
		t.Fatal("cancelled switch accepted")
	}
	if server.generation != generation {
		t.Fatal("cancelled switch mutated generation")
	}
	if err := server.changeSourceSet(context.Background(), &sourceSet{Include: []string{"inactive/"}}); err != nil {
		t.Fatal(err)
	}
	_, snapshots = server.snapshotDocuments()
	if len(snapshots) != 1 || snapshots[0].URI != inactive || server.documents[unsaved] == nil {
		t.Fatal("switch failed to retain excluded unsaved buffer")
	}
	server.didClose(didCloseParams{TextDocument: TextDocumentIdentifier{URI: unsaved}})
	if server.documents[unsaved] != nil {
		t.Fatal("closed excluded buffer retained")
	}
}

func TestSourceSetCancelledEmptyWorkspace(t *testing.T) {
	server := testServer(nil)
	defer server.stopBackground()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := server.changeSourceSet(ctx, nil); err == nil {
		t.Fatal("cancelled empty-workspace reconfiguration succeeded")
	}
}

func TestSourceSetScanUsesAuthoritativeOpenBytes(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.cl")
	if err := os.WriteFile(a, bytes.Repeat([]byte("x"), 80), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.cl"), bytes.Repeat([]byte("x"), 40), 0600); err != nil {
		t.Fatal(err)
	}
	server := testServer(nil)
	defer server.stopBackground()
	server.roots = []string{root}
	server.config.Debounce = time.Hour
	server.config.MaxWorkspaceBytes = 100
	server.didOpen(didOpenParams{TextDocument: TextDocumentItem{URI: uriFromPath(a), Version: 1, Text: string(bytes.Repeat([]byte("x"), 20))}})
	if err := server.changeSourceSet(context.Background(), &sourceSet{}); err != nil {
		t.Fatalf("effective 60-byte project rejected: %v", err)
	}
	if server.workspaceTextBytesLocked("") != 60 {
		t.Fatal("disk content replaced open buffer")
	}
}
