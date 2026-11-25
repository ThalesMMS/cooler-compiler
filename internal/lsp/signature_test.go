// SPDX-License-Identifier: AGPL-3.0-or-later
package lsp

import (
	"context"
	"strings"
	"testing"
)

func TestSignatureIncompleteSyntax(t *testing.T) {
	const uri = "file:///workspace/main.cl"
	for _, tc := range []struct {
		name, body, want string
		active           int
	}{
		{"implicit open", "f(|", "Main.f(x: Int, y: String): Main", 0},
		{"zero parameters", "copy(|", "Object.copy(): Main", -1},
		{"comma", "f(1, |", "Main.f(x: Int, y: String): Main", 1},
		{"explicit let", "let target:Main<-self in target.f(1, |", "Main.f(x: Int, y: String): Main", 1},
		{"shadowed let", "let target:String<-\"a\" in {let target:Main<-self in target.f(|", "Main.f(x: Int, y: String): Main", 0},
		{"let initializer scope", "let target:String<-\"a\", target:Main<-target.concat(|", "String.concat(s: String): String", 0},
		{"explicit new", "(new Main).f(|", "Main.f(x: Int, y: String): Main", 0},
		{"nested", "f(1, \"hi\".substr(0, |", "String.substr(i: Int, l: Int): String", 1},
		{"nested closed", "f(1, \"hi\".substr(0, 1)|", "Main.f(x: Int, y: String): Main", 1},
		{"string commas", "f(1, \"(,😀)\"|", "Main.f(x: Int, y: String): Main", 1},
		{"comment commas", "f(1, (* (, (* nested *) ) *) |", "Main.f(x: Int, y: String): Main", 1},
		{"block commas", "f({let x:Int<-1,y:Int<-2 in x+y;}, |", "Main.f(x: Int, y: String): Main", 1},
		{"let commas", "f(let x:Int<-1,y:Int<-2 in x+y, |", "Main.f(x: Int, y: String): Main", 1},
		{"static self", "self@IO.out_int(|", "IO.out_int(x: Int): IO", 0},
		{"static chain", "(self@IO.out_int(1)).f(|", "", 0},
		{"unrelated static", "self@String.substr(|", "", 0},
		{"chained self type", "self.copy().f(|", "Main.f(x: Int, y: String): Main", 0},
		{"closed", "f(1, \"done\")|", "", 0},
		{"unresolved receiver", "missing.f(|", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			marked := `class Main inherits IO { f(x:Int,y:String):SELF_TYPE {self}; main():Object {` + tc.body + `};};`
			position := nthPosition(marked, "|", 1)
			source := strings.Replace(marked, "|", "", 1)
			s := testServer(map[string]documentSnapshot{uri: {URI: uri, Text: source, Open: true, Version: 1}})
			response := callFeature(t, s, s.requestSignatureHelp, TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: position})
			if tc.want == "" {
				if response != nil {
					t.Fatalf("unexpected signature %+v", response)
				}
				return
			}
			got := decodeAs[signatureHelp](t, response)
			activeOK := tc.active < 0 && got.ActiveParameter == nil || tc.active >= 0 && got.ActiveParameter != nil && *got.ActiveParameter == tc.active
			if len(got.Signatures) != 1 || got.Signatures[0].Label != tc.want || !activeOK {
				t.Fatalf("got %+v want %s active%d", got, tc.want, tc.active)
			}
		})
	}
}

func TestSignatureCancellation(t *testing.T) {
	s := testServer(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.requestSignatureHelp(ctx, []byte(`{"textDocument":{"uri":"file:///none.cl"},"position":{"line":0,"character":0}}`))
	if err == nil || err.Code != jsonRPCRequestCancel {
		t.Fatalf("cancelled signature: %+v", err)
	}
}

func TestSignatureMultifileVersionsAndBuiltinCatalog(t *testing.T) {
	const uri = "file:///workspace/main.cl"
	const typesURI = "file:///workspace/types.cl"
	marked := `class Main {main():Object {(new Child)@Base.f(|};};`
	source := strings.Replace(marked, "|", "", 1)
	types := `class Base {f(n:Int):SELF_TYPE {self};}; class Child inherits Base {};`
	s := testServer(map[string]documentSnapshot{uri: {URI: uri, Text: source, Open: true, Version: 1}, typesURI: {URI: typesURI, Text: types, Open: true, Version: 1}})
	params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: nthPosition(marked, "|", 1)}
	got := decodeAs[signatureHelp](t, callFeature(t, s, s.requestSignatureHelp, params))
	if got.Signatures[0].Label != "Base.f(n: Int): Base" {
		t.Fatalf("static SELF_TYPE %+v", got)
	}
	s.didChange(didChangeParams{TextDocument: VersionedTextDocumentIdentifier{URI: typesURI, Version: 2}, ContentChanges: []contentChange{{Text: strings.Replace(types, "n:Int", "n:String", 1)}}})
	got = decodeAs[signatureHelp](t, callFeature(t, s, s.requestSignatureHelp, params))
	if got.Signatures[0].Label != "Base.f(n: String): Base" {
		t.Fatalf("stale signature %+v", got)
	}
	valid := testServer(map[string]documentSnapshot{uri: {URI: uri, Text: `class Main {main():Object {0};};`, Open: true, Version: 1}})
	a, err := valid.analyze(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	m := newSignatureModel(a)
	for _, decl := range a.Index.Declarations {
		if decl.Kind != "builtin method" {
			continue
		}
		sig := m.lookup(decl.Owner, decl.Name)
		if sig == nil {
			t.Fatalf("missing builtin %s.%s", decl.Owner, decl.Name)
		}
		params := []string{}
		for _, p := range sig.params {
			params = append(params, strings.Replace(p.Label, ": ", " : ", 1))
		}
		want := sig.name + "(" + strings.Join(params, ", ") + ") : " + sig.result
		if want != decl.Type {
			t.Fatalf("builtin drift %s vs %s", want, decl.Type)
		}
	}
}

func TestSignatureAttributeInitializerScope(t *testing.T) {
	const uri = "file:///workspace/main.cl"
	marked := `class Main { value:Object <- let text:String in text.substr(|; main():Object {0};};`
	s := testServer(map[string]documentSnapshot{uri: {URI: uri, Text: strings.Replace(marked, "|", "", 1), Open: true, Version: 1}})
	got := decodeAs[signatureHelp](t, callFeature(t, s, s.requestSignatureHelp, TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: nthPosition(marked, "|", 1)}))
	if len(got.Signatures) != 1 || got.Signatures[0].Label != "String.substr(i: Int, l: Int): String" {
		t.Fatalf("attribute scope: %+v", got)
	}
}
