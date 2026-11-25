// SPDX-License-Identifier: AGPL-3.0-or-later
package lsp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestRenameSafety(t *testing.T) {
	for _, tc := range []struct {
		name, source, target, replacement string
		ok                                bool
		edits                             int
	}{
		{"parameter", `class Main { f(count:Int):Int {count}; main():Object {0};};`, "count", "amount", true, 2},
		{"self context", `class Main {x:Int; f():SELF_TYPE {self}; main():Object {{self;x;}};};`, "x:Int", "count", true, 2},
		{"let", `class Main {main():Object {let x:Int <- 1 in x};};`, "x:Int", "number", true, 2},
		{"case", `class Main {main():Object {case 1 of x:Int => x; esac};};`, "x:Int", "number", true, 2},
		{"capture", `class Main {main():Object {let x:Int <- 1 in let y:Int <- 2 in x+y};};`, "x:Int", "y", false, 0},
		{"attribute capture", `class Main {x:Int; main():Object {let y:Int <- 2 in x+y};};`, "x:Int", "y", false, 0},
		{"keyword", `class Main {x:Int; main():Object {x};};`, "x:Int", "while", false, 0},
		{"entry", `class Main {main():Object {0};};`, "Main", "Other", false, 0},
		{"builtin", `class Main {main():Object {new Int};};`, "Int", "Integer", false, 0},
		{"family", `class Base {f():Int {1};}; class Child inherits Base {f():Int {2};}; class Main {main():Object {{(new Child).f(); (new Child)@Base.f();}};};`, "f():Int", "longer", true, 4},
		{"family collision", `class Base {f():Int {1};}; class Child inherits Base {g():Int {2};}; class Main {main():Object {(new Child).f()};};`, "f():Int", "g", false, 0},
		{"class", `class Base {}; class Main {main():Object {let x:Base <- new Base in x};};`, "Base", "Renamed", true, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const uri = "file:///workspace/main.cl"
			s := testServer(map[string]documentSnapshot{uri: {URI: uri, Text: tc.source, Open: true, Version: 7}})
			s.documentChanges = true
			raw, _ := json.Marshal(map[string]any{"textDocument": map[string]string{"uri": uri}, "position": nthPosition(tc.source, tc.target, 1), "newName": tc.replacement})
			result, rpcErr := s.requestRename(context.Background(), raw)
			if !tc.ok {
				if rpcErr == nil {
					t.Fatalf("unsafe rename accepted: %+v", result)
				}
				return
			}
			if rpcErr != nil {
				t.Fatal(rpcErr)
			}
			edit := decodeAs[workspaceEdit](t, result)
			if len(edit.DocumentChanges) != 1 || len(edit.DocumentChanges[0].Edits) != tc.edits || *edit.DocumentChanges[0].TextDocument.Version != 7 {
				t.Fatalf("edits %+v", edit)
			}
		})
	}
}

func TestRenamePreservesParenthesesAndLimits(t *testing.T) {
	const uri = "file:///workspace/main.cl"
	const source = `class Main {main():Object {let x:Int <- 1 in ( (*keep this*) x )};};`
	s := testServer(map[string]documentSnapshot{uri: {URI: uri, Text: source, Open: true, Version: 1}})
	s.documentChanges = true
	params := map[string]any{"textDocument": map[string]string{"uri": uri}, "position": nthPosition(source, "x:Int", 1), "newName": "number"}
	edit := decodeAs[workspaceEdit](t, callFeature(t, s, s.requestRename, params))
	text, err := applyRenameText(context.Background(), source, edit.DocumentChanges[0].Edits, len(source)+10)
	if err != nil || text != strings.ReplaceAll(strings.ReplaceAll(source, "x:Int", "number:Int"), " x )", " number )") {
		t.Fatalf("source lost trivia %q: %+v", text, err)
	}
	s.config.MaxWorkspaceBytes = len(source)
	raw, _ := json.Marshal(params)
	if _, err := s.requestRename(context.Background(), raw); err == nil {
		t.Fatal("accepted expansion beyond workspace limit")
	}
}

func TestRenameMultifileUTF16AndPrepare(t *testing.T) {
	const a = "file:///workspace/base.cl"
	const b = "file:///workspace/main.cl"
	const source = `class Main {main():Object {{"😀"; (new Base).f();}};};`
	s := testServer(map[string]documentSnapshot{a: {URI: a, Text: `class Base {f():Int {1};};`, Version: 3, Open: true}, b: {URI: b, Text: source, Version: 9, Open: true}})
	s.documentChanges = true
	params := map[string]any{"textDocument": map[string]string{"uri": b}, "position": nthPosition(source, "f()", 1), "newName": "calculate"}
	prepared := decodeAs[struct {
		Range       Range
		Placeholder string
	}](t, callFeature(t, s, s.requestPrepareRename, params))
	if prepared.Placeholder != "f" || prepared.Range.Start != nthPosition(source, "f()", 1) {
		t.Fatalf("prepare %+v", prepared)
	}
	edit := decodeAs[workspaceEdit](t, callFeature(t, s, s.requestRename, params))
	if len(edit.DocumentChanges) != 2 || edit.DocumentChanges[1].TextDocument.URI != b || *edit.DocumentChanges[1].TextDocument.Version != 9 || edit.DocumentChanges[1].Edits[0].Range.Start != prepared.Range.Start {
		t.Fatalf("multifile %+v", edit)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	raw, _ := json.Marshal(params)
	if _, err := s.requestRename(ctx, raw); err == nil || err.Code != jsonRPCRequestCancel {
		t.Fatalf("cancel %+v", err)
	}
	if s.documents[b].Text != source {
		t.Fatal("rename mutated server document")
	}
}
