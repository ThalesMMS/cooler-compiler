// SPDX-License-Identifier: AGPL-3.0-or-later

package lsp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"testing"

	publiccompiler "github.com/ThalesMMS/cooler-compiler/compiler"
)

const baseSource = `class Base {
  value : Int <- 1;
  get() : Int { value };
  fluent() : SELF_TYPE { self };
};

class Child inherits Base {
  get() : Int { value + 1 };
  again() : Int { get() };
  shadow(value : Int) : Int { value };
};
`

const mainSource = `class Main inherits IO {
  main() : Object {
    let x : Base <- new Child in {
      out_int(x.get());
      x.fluent();
      0;
    }
  };
};
`

func TestMultifileLanguageFeatures(t *testing.T) {
	server := testServer(map[string]documentSnapshot{
		"file:///workspace/base.cl": {URI: "file:///workspace/base.cl", Path: "/workspace/base.cl", Version: 1, Text: baseSource, Open: true},
		"file:///workspace/main.cl": {URI: "file:///workspace/main.cl", Path: "/workspace/main.cl", Version: 1, Text: mainSource, Open: true},
	})
	value, err := server.analyze(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if value.Result.HasErrors() {
		t.Fatalf("diagnostics %+v", value.Result.Diagnostics())
	}

	baseURI, mainURI := "file:///workspace/base.cl", "file:///workspace/main.cl"
	valueRef := nthPosition(baseSource, "value", 2)
	definition := callFeature(t, server, server.requestDefinition, TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: baseURI}, Position: valueRef})
	location := decodeAs[Location](t, definition)
	if location.URI != baseURI || location.Range.Start != (Position{Line: 1, Character: 2}) {
		t.Fatalf("attribute definition %+v", location)
	}

	getCall := nthPosition(mainSource, "get", 1)
	definition = callFeature(t, server, server.requestDefinition, TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: mainURI}, Position: getCall})
	location = decodeAs[Location](t, definition)
	if location.URI != baseURI || location.Range.Start.Line != 2 {
		t.Fatalf("method definition %+v; occurrences=%+v", location, value.Index.Occurrences[mainURI])
	}

	hoverResult := callFeature(t, server, server.requestHover, TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: mainURI}, Position: nthPosition(mainSource, "fluent", 1)})
	hoverValue := decodeAs[hover](t, hoverResult)
	if !strings.Contains(hoverValue.Contents.Value, "SELF_TYPE") || !strings.Contains(hoverValue.Contents.Value, "Base.fluent") {
		t.Fatalf("hover %s", hoverValue.Contents.Value)
	}

	references := callFeature(t, server, server.requestReferences, map[string]any{
		"textDocument": map[string]any{"uri": baseURI}, "position": valueRef, "context": map[string]any{"includeDeclaration": true},
	})
	locations := decodeAs[[]Location](t, references)
	if len(locations) != 3 {
		t.Fatalf("attribute references %+v", locations)
	}

	shadowReference := nthPosition(baseSource, "value", 5)
	definition = callFeature(t, server, server.requestDefinition, TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: baseURI}, Position: shadowReference})
	location = decodeAs[Location](t, definition)
	if location.Range.Start != nthRange(baseSource, "value", 4).Start {
		t.Fatalf("shadow definition %+v", location)
	}

	symbols := callFeature(t, server, server.requestDocumentSymbols, map[string]any{"textDocument": map[string]any{"uri": baseURI}})
	documentSymbols := decodeAs[[]documentSymbol](t, symbols)
	if len(documentSymbols) != 2 || documentSymbols[0].Name != "Base" || len(documentSymbols[1].Children) < 3 {
		t.Fatalf("symbols %+v", documentSymbols)
	}

	workspace := callFeature(t, server, server.requestWorkspaceSymbols, map[string]any{"query": "Child"})
	workspaceSymbols := decodeAs[[]symbolInformation](t, workspace)
	if len(workspaceSymbols) != 1 || workspaceSymbols[0].Name != "Child" {
		t.Fatalf("workspace symbols %+v", workspaceSymbols)
	}

	dot := nthPosition(mainSource, "x.get", 1)
	dot.Character += 2
	completion := callFeature(t, server, server.requestCompletion, TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: mainURI}, Position: dot})
	items := decodeAs[[]completionItem](t, completion)
	if !hasCompletion(items, "get") || !hasCompletion(items, "fluent") || hasCompletion(items, "out_string") {
		t.Fatalf("completion %+v", items)
	}

	semantic := callFeature(t, server, server.requestSemanticTokens, map[string]any{"textDocument": map[string]any{"uri": baseURI}})
	tokens := decodeAs[semanticTokens](t, semantic)
	if len(tokens.Data) < 25 {
		t.Fatalf("semantic tokens too small: %v", tokens.Data)
	}

	childGetCall := nthPosition(baseSource, "get", 3)
	hoverResult = callFeature(t, server, server.requestHover, TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: baseURI}, Position: childGetCall})
	hoverValue = decodeAs[hover](t, hoverResult)
	if !strings.Contains(hoverValue.Contents.Value, "Child.get") {
		t.Fatalf("override hover %s", hoverValue.Contents.Value)
	}
}

func TestCodeActionsAndRelatedCrossFileDiagnostic(t *testing.T) {
	first := `class Duplicate { value() : Int { 1 }; }; class Main { main() : Int { 0 }; };`
	second := `class Duplicate { value() : Int { 2 }; };`
	firstURI, secondURI := "file:///workspace/first.cl", "file:///workspace/second.cl"
	server := testServer(map[string]documentSnapshot{
		firstURI:  {URI: firstURI, Text: first, Version: 1, Open: true},
		secondURI: {URI: secondURI, Text: second, Version: 2, Open: true},
	})
	value, err := server.analyze(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var duplicateRange Range
	foundRelated := false
	for _, item := range value.Report.Diagnostics {
		if item.Code == "COL3002" {
			duplicateRange = rangeFromSpan(item.Span)
			foundRelated = len(item.Related) == 1 && item.Related[0].Span.SourceID == firstURI
		}
	}
	if !foundRelated {
		t.Fatalf("diagnostics %+v", value.Report.Diagnostics)
	}
	actionsResult := callFeature(t, server, server.requestCodeActions, codeActionParams{TextDocument: TextDocumentIdentifier{URI: secondURI}, Range: duplicateRange})
	actions := decodeAs[[]codeAction](t, actionsResult)
	if len(actions) != 1 || actions[0].Edit.Changes[secondURI][0].NewText != "Duplicate2" {
		t.Fatalf("actions %+v", actions)
	}
}

func TestStaleVersionsCancellationAndStaleDiagnostics(t *testing.T) {
	server := testServer(nil)
	uri := "file:///workspace/main.cl"
	server.didOpen(didOpenParams{TextDocument: TextDocumentItem{URI: uri, Version: 2, Text: mainSource}})
	server.didChange(didChangeParams{TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: 1}, ContentChanges: []contentChange{{Text: "stale"}}})
	server.mu.Lock()
	if server.documents[uri].Text != mainSource || server.documents[uri].Version != 2 {
		t.Fatal("stale version replaced current document")
	}
	server.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := server.analyze(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation %v", err)
	}

	old, err := server.analyze(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	server.didChange(didChangeParams{TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: 3}, ContentChanges: []contentChange{{Text: `class Main { main() : Int { missing }; };`}}})
	var output bytes.Buffer
	server.writer = &output
	server.publishDiagnostics(old)
	if output.Len() != 0 {
		t.Fatal("stale analysis published diagnostics")
	}
	current, err := server.analyze(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	server.publishDiagnostics(current)
	if output.Len() == 0 {
		t.Fatal("current analysis did not publish")
	}
}

func TestIncrementalUTF16ChangeAndWorkspaceStress(t *testing.T) {
	server := testServer(nil)
	uri := "file:///workspace/unicode.cl"
	text := "-- 😀\n" + mainSource
	server.didOpen(didOpenParams{TextDocument: TextDocumentItem{URI: uri, Version: 1, Text: text}})
	server.didChange(didChangeParams{TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: 2}, ContentChanges: []contentChange{{
		Range: &Range{Start: Position{Line: 0, Character: 3}, End: Position{Line: 0, Character: 5}}, Text: "ok",
	}}})
	server.mu.Lock()
	if !strings.HasPrefix(server.documents[uri].Text, "-- ok") {
		t.Fatalf("text %q", server.documents[uri].Text[:8])
	}
	server.mu.Unlock()

	documents := map[string]documentSnapshot{}
	for index := range 50 {
		name := fmt.Sprintf("Class%d", index)
		docURI := fmt.Sprintf("file:///workspace/%03d.cl", index)
		documents[docURI] = documentSnapshot{URI: docURI, Version: 1, Text: fmt.Sprintf("class %s { value() : Int { %d }; };", name, index), Open: true}
	}
	documents["file:///workspace/main.cl"] = documentSnapshot{URI: "file:///workspace/main.cl", Version: 1, Text: `class Main { main() : Int { 0 }; };`, Open: true}
	stress := testServer(documents)
	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := stress.analyze(context.Background())
			if err != nil {
				t.Errorf("analysis error: %v", err)
				return
			}
			if result == nil {
				t.Error("analysis returned nil without an error")
				return
			}
			if result.Result.HasErrors() {
				t.Errorf("diagnostics=%+v", result.Result.Diagnostics())
			}
		}()
	}
	wait.Wait()
}

func TestMalformedFilesRetainIndependentFrontendDiagnostics(t *testing.T) {
	firstURI, secondURI := "file:///workspace/first.cl", "file:///workspace/second.cl"
	server := testServer(map[string]documentSnapshot{
		firstURI:  {URI: firstURI, Version: 1, Text: `class First { broken( : Int { 0 }; };`, Open: true},
		secondURI: {URI: secondURI, Version: 1, Text: "class Second { value() : Int { # }; };", Open: true},
	})
	value, err := server.analyze(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, item := range value.Report.Diagnostics {
		seen[item.Span.SourceID] = true
	}
	if !seen[firstURI] || !seen[secondURI] {
		t.Fatalf("diagnostics did not retain both source identities: %+v", value.Report.Diagnostics)
	}
}

func TestSemanticTokenCategoriesAndCaseResolution(t *testing.T) {
	source := `-- leading comment
class Helper {
  attribute : String <- "hi";
  method(parameter : Int) : Object {
    let local : Int <- 1 in
      case parameter of branch : Int => branch + local; esac
  };
};
class Main { main() : Int { 0 }; };
`
	uri := "file:///workspace/categories.cl"
	server := testServer(map[string]documentSnapshot{uri: {URI: uri, Text: source, Version: 1, Open: true}})
	result := callFeature(t, server, server.requestSemanticTokens, map[string]any{"textDocument": map[string]any{"uri": uri}})
	tokens := decodeAs[semanticTokens](t, result)
	types := map[uint32]bool{}
	hasDeclaration := false
	for index := 0; index+4 < len(tokens.Data); index += 5 {
		types[tokens.Data[index+3]] = true
		if tokens.Data[index+4]&(1<<0) != 0 {
			hasDeclaration = true
		}
	}
	for _, tokenType := range []uint32{0, 1, 2, 3, 4, 6, 7, 8, 9, 10} {
		if !types[tokenType] {
			t.Errorf("semantic token type %d missing from %v", tokenType, types)
		}
	}
	if !hasDeclaration {
		t.Error("declaration modifier missing")
	}
	branchReference := nthPosition(source, "branch", 2)
	definition := callFeature(t, server, server.requestDefinition, TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: branchReference})
	location := decodeAs[Location](t, definition)
	if location.Range.Start != nthRange(source, "branch", 1).Start {
		t.Fatalf("case variable definition %+v", location)
	}
}

func testServer(documents map[string]documentSnapshot) *Server {
	config := testConfig()
	server := &Server{
		config: config, writer: io.Discard, logger: log.New(io.Discard, "", 0), documents: map[string]*document{},
		requests: map[string]context.CancelFunc{}, cancelled: map[string]struct{}{}, analysisToken: make(chan struct{}, 1), initialized: true,
		requestSlots: make(chan struct{}, config.MaxConcurrentRequests),
	}
	server.analysisToken <- struct{}{}
	for uri, snapshot := range documents {
		copy := snapshot
		server.documents[uri] = &document{URI: copy.URI, Path: copy.Path, Version: copy.Version, Text: copy.Text, Open: copy.Open}
	}
	server.generation = 1
	return server
}

type featureCall func(context.Context, json.RawMessage) (any, *rpcError)

func callFeature(t *testing.T, server *Server, call featureCall, params any) any {
	t.Helper()
	encoded, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	result, rpcErr := call(context.Background(), encoded)
	if rpcErr != nil {
		t.Fatalf("rpc error %+v", rpcErr)
	}
	return result
}

func decodeAs[T any](t *testing.T, value any) T {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result T
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func nthPosition(text, needle string, occurrence int) Position {
	return nthRange(text, needle, occurrence).Start
}

func nthRange(text, needle string, occurrence int) Range {
	offset := -1
	searchFrom := 0
	for count := 0; count < occurrence; count++ {
		found := strings.Index(text[searchFrom:], needle)
		if found < 0 {
			panic("needle not found: " + needle)
		}
		offset = searchFrom + found
		searchFrom = offset + len(needle)
	}
	position := Position{}
	for _, r := range text[:offset] {
		if r == '\n' {
			position.Line++
			position.Character = 0
		} else if r > 0xFFFF {
			position.Character += 2
		} else {
			position.Character++
		}
	}
	end := position
	end.Character += utf16Length(needle)
	return Range{Start: position, End: end}
}

func TestUTF16OutputRangesAndComments(t *testing.T) {
	text := "😀value"
	span := publiccompiler.Span{StartLine: 1, StartColumn: 2, EndLine: 1, EndColumn: 7, StartOffset: 2, EndOffset: 7}
	got := rangeFromSpanInText(span, text)
	want := Range{Start: Position{Character: 2}, End: Position{Character: 7}}
	if got != want {
		t.Fatalf("UTF-16 range %+v, want %+v", got, want)
	}
	indexed := rangeFromSpanInIndex(span, newTextPositionIndex(text))
	if indexed != want {
		t.Fatalf("indexed UTF-16 range %+v, want %+v", indexed, want)
	}
	if _, ok := newTextPositionIndex(text).position(1); ok {
		t.Fatal("indexed UTF-16 position accepted a surrogate midpoint")
	}

	comments := commentSemanticItems("-- 😀")
	if len(comments) != 1 || comments[0].character != 0 || comments[0].length != 5 {
		t.Fatalf("UTF-16 comment tokens %+v", comments)
	}
}

func hasCompletion(items []completionItem, label string) bool {
	for _, item := range items {
		if item.Label == label {
			return true
		}
	}
	return false
}
