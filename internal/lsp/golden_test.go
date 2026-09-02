// SPDX-License-Identifier: AGPL-3.0-or-later

package lsp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestGoldenLanguageResponses(t *testing.T) {
	baseURI, mainURI := "file:///workspace/base.cl", "file:///workspace/main.cl"
	server := testServer(map[string]documentSnapshot{
		baseURI: {URI: baseURI, Path: "/workspace/base.cl", Version: 1, Text: baseSource, Open: true},
		mainURI: {URI: mainURI, Path: "/workspace/main.cl", Version: 1, Text: mainSource, Open: true},
	})
	_, err := server.analyze(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	hoverResult := callFeature(t, server, server.requestHover, TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: mainURI}, Position: nthPosition(mainSource, "fluent", 1)})
	symbolsResult := callFeature(t, server, server.requestDocumentSymbols, map[string]any{"textDocument": map[string]any{"uri": baseURI}})
	definitionResult := callFeature(t, server, server.requestDefinition, TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: mainURI}, Position: nthPosition(mainSource, "get", 1)})
	referencesResult := callFeature(t, server, server.requestReferences, map[string]any{
		"textDocument": map[string]any{"uri": baseURI}, "position": nthPosition(baseSource, "value", 2), "context": map[string]any{"includeDeclaration": true},
	})
	dot := nthPosition(mainSource, "x.get", 1)
	dot.Character += 2
	completionResult := callFeature(t, server, server.requestCompletion, TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: mainURI}, Position: dot})
	completionItems := decodeAs[[]completionItem](t, completionResult)
	completionLabels := make([]string, 0, len(completionItems))
	for _, item := range completionItems {
		completionLabels = append(completionLabels, item.Label)
	}
	sort.Strings(completionLabels)
	semanticResult := callFeature(t, server, server.requestSemanticTokens, map[string]any{"textDocument": map[string]any{"uri": mainURI}})

	duplicateURI := "file:///workspace/duplicate.cl"
	duplicate := testServer(map[string]documentSnapshot{
		baseURI:      {URI: baseURI, Text: `class Duplicate {}; class Main { main() : Int { 0 }; };`, Version: 1, Open: true},
		duplicateURI: {URI: duplicateURI, Text: `class Duplicate {};`, Version: 1, Open: true},
	})
	duplicateAnalysis, err := duplicate.analyze(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var diagnostics []diagnostic
	var duplicateRange Range
	for _, item := range duplicateAnalysis.Report.Diagnostics {
		if item.Code == "COL3002" {
			diagnostics = append(diagnostics, lspDiagnostic(duplicateAnalysis.Documents, duplicateURI, item))
			duplicateRange = rangeForSpan(duplicateAnalysis.Documents, duplicateURI, item.Span)
		}
	}
	actionsResult := callFeature(t, duplicate, duplicate.requestCodeActions, codeActionParams{TextDocument: TextDocumentIdentifier{URI: duplicateURI}, Range: duplicateRange})

	digests := map[string]string{
		"diagnostics":    goldenDigest(t, diagnostics),
		"hover":          goldenDigest(t, decodeAs[hover](t, hoverResult)),
		"symbols":        goldenDigest(t, decodeAs[[]documentSymbol](t, symbolsResult)),
		"definition":     goldenDigest(t, decodeAs[Location](t, definitionResult)),
		"references":     goldenDigest(t, decodeAs[[]Location](t, referencesResult)),
		"completions":    goldenDigest(t, completionLabels),
		"semanticTokens": goldenDigest(t, decodeAs[semanticTokens](t, semanticResult)),
		"codeActions":    goldenDigest(t, decodeAs[[]codeAction](t, actionsResult)),
	}
	encoded, err := json.MarshalIndent(digests, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, '\n')
	path := filepath.Join("testdata", "language-features.golden.json")
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v\n--- generated ---\n%s", err, encoded)
	}
	if string(encoded) != string(want) {
		t.Fatalf("language feature golden changed\n--- generated ---\n%s", encoded)
	}
}

func goldenDigest(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("sha256:%x", digest)
}
