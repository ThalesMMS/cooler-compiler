// SPDX-License-Identifier: AGPL-3.0-or-later

package lsp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDocumentAndWorkspaceResourceLimits(t *testing.T) {
	server := testServer(nil)
	server.config.MaxOpenDocuments = 1
	server.config.MaxFileBytes = 64
	server.didOpen(didOpenParams{TextDocument: TextDocumentItem{URI: "file:///workspace/one.cl", Version: 1, Text: "class One {};"}})
	server.didOpen(didOpenParams{TextDocument: TextDocumentItem{URI: "file:///workspace/two.cl", Version: 1, Text: "class Two {};"}})
	server.didOpen(didOpenParams{TextDocument: TextDocumentItem{URI: "file:///workspace/large.cl", Version: 1, Text: string(bytes.Repeat([]byte{'x'}, 65))}})
	server.mu.Lock()
	if len(server.documents) != 1 || server.documents["file:///workspace/one.cl"] == nil {
		t.Fatalf("documents %+v", server.documents)
	}
	server.mu.Unlock()

	server = testServer(map[string]documentSnapshot{
		"file:///workspace/existing.cl": {URI: "file:///workspace/existing.cl", Text: "class Existing {};"},
	})
	server.config.MaxWorkspaceFiles = 1
	server.didOpen(didOpenParams{TextDocument: TextDocumentItem{URI: "file:///workspace/extra.cl", Version: 1, Text: "class Extra {};"}})
	server.mu.Lock()
	if len(server.documents) != 1 || server.documents["file:///workspace/extra.cl"] != nil {
		t.Fatalf("didOpen exceeded workspace limit: %+v", server.documents)
	}
	server.mu.Unlock()

	directory := t.TempDir()
	for _, name := range []string{"a.cl", "b.cool"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("class "+name[:1]+" {};"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	server.config.MaxWorkspaceFiles = 1
	documents, err := server.scanWorkspace([]string{directory})
	if err == nil || len(documents) != 1 {
		t.Fatalf("scan files=%d err=%v", len(documents), err)
	}
}

func TestRetainedDocumentsEnforceCombinedSourceLimit(t *testing.T) {
	server := testServer(nil)
	server.config.MaxWorkspaceBytes = 6
	server.didOpen(didOpenParams{TextDocument: TextDocumentItem{URI: "file:///workspace/a.cl", Version: 1, Text: "abc"}})
	server.didOpen(didOpenParams{TextDocument: TextDocumentItem{URI: "file:///workspace/b.cl", Version: 1, Text: "defg"}})
	server.mu.Lock()
	if len(server.documents) != 1 || server.documents["file:///workspace/b.cl"] != nil {
		t.Fatalf("didOpen exceeded combined source limit: %+v", server.documents)
	}
	server.mu.Unlock()
	server.didOpen(didOpenParams{TextDocument: TextDocumentItem{URI: "file:///workspace/b.cl", Version: 1, Text: "def"}})
	server.didChange(didChangeParams{
		TextDocument:   VersionedTextDocumentIdentifier{URI: "file:///workspace/a.cl", Version: 2},
		ContentChanges: []contentChange{{Text: "abcd"}},
	})
	server.mu.Lock()
	doc := server.documents["file:///workspace/a.cl"]
	if doc.Text != "abc" || !doc.Desynchronized || doc.Version != 2 || server.workspaceTextBytesLocked("") != 6 {
		t.Fatalf("didChange exceeded combined source limit: document=%+v bytes=%d", doc, server.workspaceTextBytesLocked(""))
	}
	server.mu.Unlock()
	server.stopBackground()
}

func TestWatchedBatchUsesFinalEventsAndCombinedByteLimit(t *testing.T) {
	directory := t.TempDir()
	paths := map[string]string{}
	for _, name := range []string{"a.cl", "b.cl", "c.cl"} {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte("xx"), 0o600); err != nil {
			t.Fatal(err)
		}
		paths[name] = uriFromPath(path)
	}
	server := testServer(map[string]documentSnapshot{paths["a.cl"]: {URI: paths["a.cl"], Text: "old"}})
	server.config.MaxWorkspaceBytes = 2
	server.didChangeWatchedFiles(jsonWatchedFiles{Changes: []struct {
		URI  string `json:"uri"`
		Type int    `json:"type"`
	}{
		{URI: paths["a.cl"], Type: 1},
		{URI: paths["a.cl"], Type: 3},
		{URI: paths["b.cl"], Type: 1},
		{URI: paths["c.cl"], Type: 1},
	}})
	server.mu.Lock()
	if server.documents[paths["a.cl"]] != nil || server.documents[paths["b.cl"]] != nil || server.documents[paths["c.cl"]] == nil || server.workspaceTextBytesLocked("") != 2 {
		t.Fatalf("watched batch documents=%+v bytes=%d", server.documents, server.workspaceTextBytesLocked(""))
	}
	server.mu.Unlock()
	server.stopBackground()
}

func TestWorkspaceScanEntryLimitAndCancellation(t *testing.T) {
	directory := t.TempDir()
	for index := 0; index < 4; index++ {
		path := filepath.Join(directory, fmt.Sprintf("entry-%d.txt", index))
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	server := testServer(nil)
	server.config.MaxWorkspaceEntries = 2
	if _, err := server.scanWorkspaceContext(context.Background(), []string{directory}); !errors.Is(err, errWorkspaceScanLimit) {
		t.Fatalf("entry limit error = %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := server.scanWorkspaceContext(cancelled, []string{directory}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled scan error = %v", err)
	}
}

func TestBoundedSourceReadStopsAtLimit(t *testing.T) {
	source := &countingReader{reader: zeroReader{}}
	if _, err := readBounded(source, 16); !errors.Is(err, errSourceFileTooLarge) {
		t.Fatalf("bounded read error = %v", err)
	}
	if source.read != 17 {
		t.Fatalf("bounded read consumed %d bytes, want 17", source.read)
	}
	path := filepath.Join(t.TempDir(), "large.cl")
	if err := os.WriteFile(path, []byte("1234"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readSourceFile(path, 3); !errors.Is(err, errSourceFileTooLarge) {
		t.Fatalf("bounded file error = %v", err)
	}
	if _, err := readSourceFile(t.TempDir(), 3); !errors.Is(err, errSourceFileType) {
		t.Fatalf("non-regular source error = %v", err)
	}
}

func TestWatchedFilesAndCloseClearDiagnostics(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "watched.cl")
	uri := uriFromPath(path)
	if err := os.WriteFile(path, []byte(`class Main { main() : Int { 0 }; };`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := testServer(nil)
	var output bytes.Buffer
	server.writer = &output
	server.didChangeWatchedFiles(jsonWatchedFiles{Changes: []struct {
		URI  string `json:"uri"`
		Type int    `json:"type"`
	}{{URI: uri, Type: 1}}})
	server.mu.Lock()
	if server.documents[uri] == nil {
		t.Fatal("created file was not loaded")
	}
	server.mu.Unlock()
	server.didChangeWatchedFiles(jsonWatchedFiles{Changes: []struct {
		URI  string `json:"uri"`
		Type int    `json:"type"`
	}{{URI: uri, Type: 3}}})
	server.mu.Lock()
	if server.documents[uri] != nil {
		t.Fatal("deleted file remained in workspace")
	}
	server.mu.Unlock()
	if output.Len() == 0 {
		t.Fatal("delete did not clear diagnostics")
	}

	unsaved := uriFromPath(filepath.Join(t.TempDir(), "unsaved.cl"))
	server.didOpen(didOpenParams{TextDocument: TextDocumentItem{URI: unsaved, Version: 1, Text: mainSource}})
	before := output.Len()
	server.didClose(didCloseParams{TextDocument: TextDocumentIdentifier{URI: unsaved}})
	if output.Len() <= before {
		t.Fatal("closing removed unsaved file did not clear diagnostics")
	}
}

func TestPathFromURIDecodesExactlyOnce(t *testing.T) {
	path, err := pathFromURI("file:///workspace/a%2520b.cl")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.ToSlash(path) != "/workspace/a%20b.cl" {
		t.Fatalf("decoded path = %q", path)
	}
}

func TestFileURIRoundTripPreservesSpacesUnicodeAndPercent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "space ü 100%.cl")
	uri := uriFromPath(path)
	decoded, err := pathFromURI(uri)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(decoded) != filepath.Clean(path) {
		t.Fatalf("round trip path=%q uri=%q decoded=%q", path, uri, decoded)
	}
}

func TestFileURIAuthorityHandling(t *testing.T) {
	path, err := pathFromURIForOS("file://server/share/space%20%C3%BC%25.cl", "windows")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.FromSlash("//server/share/space ü%.cl")
	uri := uriFromSlashedPath("//server/share/space ü%.cl", "windows")
	if path != want || uri != "file://server/share/space%20%C3%BC%25.cl" {
		t.Fatalf("UNC path=%q uri=%q", path, uri)
	}
	if _, err := pathFromURIForOS("file://server/share/main.cl", "linux"); err == nil {
		t.Fatal("remote file authority was silently mapped to a local Unix path")
	}
	if isCoolURI("file:///workspace/main.cl?revision=1") {
		t.Fatal("file URI with query was accepted as a local document identity")
	}
}

func TestAnalysisTimeoutAndCombinedSourceLimit(t *testing.T) {
	server := testServer(map[string]documentSnapshot{
		"file:///workspace/a.cl": {URI: "file:///workspace/a.cl", Text: mainSource, Open: true},
		"file:///workspace/b.cl": {URI: "file:///workspace/b.cl", Text: baseSource, Open: true},
	})
	server.config.MaxWorkspaceBytes = 8
	if _, err := server.analyze(context.Background()); err == nil {
		t.Fatal("combined source limit was not enforced")
	}
	server.config.MaxWorkspaceBytes = 32 << 20
	server.config.AnalysisTimeout = time.Millisecond
	<-server.analysisToken
	_, rpcErr := server.analysisForRequest(context.Background())
	server.analysisToken <- struct{}{}
	if rpcErr == nil || rpcErr.Code != jsonRPCRequestCancel {
		t.Fatalf("timeout error %+v", rpcErr)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := server.analyze(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error %v", err)
	}
}

func TestIncrementalChangeDesynchronizationRequiresFullText(t *testing.T) {
	server := testServer(nil)
	uri := "file:///workspace/desync.cl"
	server.didOpen(didOpenParams{TextDocument: TextDocumentItem{URI: uri, Version: 1, Text: "abc"}})
	server.didChange(didChangeParams{
		TextDocument:   VersionedTextDocumentIdentifier{URI: uri, Version: 2},
		ContentChanges: []contentChange{{Range: &Range{Start: Position{Line: 9}, End: Position{Line: 9}}, Text: "x"}},
	})
	server.didChange(didChangeParams{
		TextDocument:   VersionedTextDocumentIdentifier{URI: uri, Version: 3},
		ContentChanges: []contentChange{{Range: &Range{Start: Position{}, End: Position{Character: 1}}, Text: "z"}},
	})
	server.mu.Lock()
	doc := server.documents[uri]
	if doc.Text != "abc" || !doc.Desynchronized || doc.Version != 3 {
		t.Fatalf("desynchronized document = %+v", doc)
	}
	server.mu.Unlock()
	server.didChange(didChangeParams{
		TextDocument:   VersionedTextDocumentIdentifier{URI: uri, Version: 4},
		ContentChanges: []contentChange{{Text: "class Main { main() : Int { 0 }; };"}},
	})
	server.mu.Lock()
	doc = server.documents[uri]
	if doc.Desynchronized || doc.Version != 4 || !strings.HasPrefix(doc.Text, "class Main") {
		t.Fatalf("full-text recovery = %+v", doc)
	}
	server.mu.Unlock()
	server.stopBackground()
}

func TestOversizedFullTextChangeRequiresBoundedRecovery(t *testing.T) {
	server := testServer(nil)
	server.config.MaxFileBytes = 3
	uri := "file:///workspace/bounded.cl"
	server.didOpen(didOpenParams{TextDocument: TextDocumentItem{URI: uri, Version: 1, Text: "abc"}})
	server.didChange(didChangeParams{
		TextDocument:   VersionedTextDocumentIdentifier{URI: uri, Version: 2},
		ContentChanges: []contentChange{{Text: "abcd"}},
	})
	server.mu.Lock()
	doc := server.documents[uri]
	if doc.Text != "abc" || !doc.Desynchronized || doc.Version != 2 {
		t.Fatalf("oversized full-text change = %+v", doc)
	}
	server.mu.Unlock()
	server.didChange(didChangeParams{
		TextDocument:   VersionedTextDocumentIdentifier{URI: uri, Version: 3},
		ContentChanges: []contentChange{{Text: "ok"}},
	})
	server.mu.Lock()
	doc = server.documents[uri]
	if doc.Text != "ok" || doc.Desynchronized || doc.Version != 3 {
		t.Fatalf("bounded full-text recovery = %+v", doc)
	}
	server.mu.Unlock()
	server.stopBackground()
}

func TestIncrementalChangeCountRequiresFullTextRecovery(t *testing.T) {
	server := testServer(nil)
	server.config.MaxContentChanges = 1
	uri := "file:///workspace/change-count.cl"
	server.didOpen(didOpenParams{TextDocument: TextDocumentItem{URI: uri, Version: 1, Text: "abc"}})
	server.didChange(didChangeParams{
		TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: 2},
		ContentChanges: []contentChange{
			{Range: &Range{Start: Position{}, End: Position{}}, Text: "x"},
			{Range: &Range{Start: Position{}, End: Position{}}, Text: "y"},
		},
	})
	server.mu.Lock()
	doc := server.documents[uri]
	if doc.Text != "abc" || !doc.Desynchronized || doc.Version != 2 {
		t.Fatalf("over-count incremental change = %+v", doc)
	}
	server.mu.Unlock()
	server.stopBackground()
}

func TestDidCloseClearsDesynchronizationAfterDiskReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reload.cl")
	diskText := "class Main { main() : Int { 0 }; };"
	if err := os.WriteFile(path, []byte(diskText), 0o600); err != nil {
		t.Fatal(err)
	}
	uri := uriFromPath(path)
	server := testServer(nil)
	server.didOpen(didOpenParams{TextDocument: TextDocumentItem{URI: uri, Version: 1, Text: "abc"}})
	server.didChange(didChangeParams{
		TextDocument:   VersionedTextDocumentIdentifier{URI: uri, Version: 2},
		ContentChanges: []contentChange{{Range: &Range{Start: Position{Line: 9}, End: Position{Line: 9}}, Text: "x"}},
	})
	server.didClose(didCloseParams{TextDocument: TextDocumentIdentifier{URI: uri}})
	server.mu.Lock()
	doc := server.documents[uri]
	if doc == nil || doc.Open || doc.Desynchronized || doc.Text != diskText {
		t.Fatalf("reloaded document = %+v", doc)
	}
	server.mu.Unlock()
	_, snapshots := server.snapshotDocuments()
	if len(snapshots) != 1 || snapshots[0].URI != uri {
		t.Fatalf("reloaded snapshots = %+v", snapshots)
	}
	server.stopBackground()
}
