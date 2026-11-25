// SPDX-License-Identifier: AGPL-3.0-or-later

package lsp

import (
	"context"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestSymbolQuickFixVersionedUTF16(t *testing.T) {
	uri := "file:///workspace/main.cl"
	text := `class Main {main():Object {{"😀"; (new Helper).lenght();}};};`
	server := testServer(map[string]documentSnapshot{
		uri:                           {URI: uri, Text: text, Open: true, Version: 7},
		"file:///workspace/helper.cl": {URI: "file:///workspace/helper.cl", Text: `class Helper {length():Int {1};};`},
	})
	server.documentChanges = true
	rangeAt := Range{Start: Position{Line: 0}, End: Position{Line: 1}}
	actions := decodeAs[[]codeAction](t, callFeature(t, server, server.requestCodeActions, codeActionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Range: rangeAt}))
	if len(actions) != 1 || len(actions[0].Edit.DocumentChanges) != 1 || actions[0].Edit.Changes != nil {
		t.Fatalf("actions %+v", actions)
	}
	change := actions[0].Edit.DocumentChanges[0]
	start := len(utf16.Encode([]rune(text[:strings.Index(text, "lenght")])))
	if change.TextDocument.URI != uri || change.TextDocument.Version == nil || *change.TextDocument.Version != 7 || len(change.Edits) != 1 || change.Edits[0].Range != (Range{Start: Position{Character: start}, End: Position{Character: start + 6}}) {
		t.Fatalf("version/range %+v", change)
	}
	units := utf16.Encode([]rune(text))
	edit := change.Edits[0]
	fixed := string(utf16.Decode(append(append(append([]uint16{}, units[:edit.Range.Start.Character]...), utf16.Encode([]rune(edit.NewText))...), units[edit.Range.End.Character:]...)))
	server.mu.Lock()
	server.documents[uri].Text = fixed
	server.documents[uri].Version = 8
	server.invalidateLocked()
	server.mu.Unlock()
	value, err := server.analyze(context.Background())
	if err != nil || value.Result.HasErrors() {
		t.Fatalf("applied fix: %v %+v", err, value)
	}
}
