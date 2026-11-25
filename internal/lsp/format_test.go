// SPDX-License-Identifier: AGPL-3.0-or-later
package lsp

import (
	"context"
	"encoding/json"
	"testing"
)

func TestFormattingUTF16AndInvalid(t *testing.T) {
	const uri = "file:///workspace/main.cl"
	const source = `class Main{main():Object{"😀"};};`
	s := testServer(map[string]documentSnapshot{uri: {URI: uri, Text: source, Version: 1, Open: true}})
	params := map[string]any{"textDocument": map[string]string{"uri": uri}, "options": map[string]any{"tabSize": 2, "insertSpaces": true}}
	edits := decodeAs[[]textEdit](t, callFeature(t, s, s.requestFormatting, params))
	if len(edits) != 1 || edits[0].Range.End.Character != len([]rune(source))+1 {
		t.Fatalf("UTF16 %+v", edits)
	}
	s.documents[uri].Text = edits[0].NewText
	if edits := decodeAs[[]textEdit](t, callFeature(t, s, s.requestFormatting, params)); len(edits) != 0 {
		t.Fatal("not idempotent")
	}
	s.documents[uri].Text = "class Main {"
	raw, _ := json.Marshal(params)
	if _, err := s.requestFormatting(context.Background(), raw); err == nil {
		t.Fatal("invalid formatted")
	}
}
