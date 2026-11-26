// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import "testing"

func TestReferenceTokenDump(t *testing.T) {
	cases := []struct {
		name, file, source, want string
	}{
		{
			name: "hello", file: "hello.cl",
			source: "class Main inherits IO {\n  main() : Object { out_string(\"hi\\n\") };\n};\n",
			want: `#name "hello.cl"
#1 CLASS
#1 TYPEID Main
#1 INHERITS
#1 TYPEID IO
#1 '{'
#2 OBJECTID main
#2 '('
#2 ')'
#2 ':'
#2 TYPEID Object
#2 '{'
#2 OBJECTID out_string
#2 '('
#2 STR_CONST "hi\n"
#2 ')'
#2 '}'
#2 ';'
#3 '}'
#3 ';'
`,
		},
		{
			name: "operators and keywords", file: "ops.cl",
			source: "x <- y <= 3 => isvoid NOT true fALSE False 007 2147483648 ~a.b@C",
			want: `#name "ops.cl"
#1 OBJECTID x
#1 ASSIGN
#1 OBJECTID y
#1 LE
#1 INT_CONST 3
#1 DARROW
#1 ISVOID
#1 NOT
#1 BOOL_CONST true
#1 BOOL_CONST false
#1 TYPEID False
#1 INT_CONST 007
#1 INT_CONST 2147483648
#1 '~'
#1 OBJECTID a
#1 '.'
#1 OBJECTID b
#1 '@'
#1 TYPEID C
`,
		},
		{
			name: "errors", file: "err.cl",
			source: "\"abc\nx [ _foo *) \"a\\\nb\" \"n\x00l\" (* open",
			want: `#name "err.cl"
#2 ERROR "Unterminated string constant"
#2 OBJECTID x
#2 ERROR "["
#2 ERROR "_"
#2 OBJECTID foo
#2 ERROR "Unmatched *)"
#3 STR_CONST "a\nb"
#3 ERROR "String contains null character."
#3 ERROR "EOF in comment"
`,
		},
		{
			name: "eof in string", file: "eof.cl",
			source: "\"abc",
			want:   "#name \"eof.cl\"\n#1 ERROR \"EOF in string constant\"\n",
		},
		{
			name: "escapes and bytes", file: "esc.cl",
			source: "\"tab\\there\\q\\\"\" \xc3\xa9",
			want:   "#name \"esc.cl\"\n#1 STR_CONST \"tab\\thereq\\\"\"\n#1 ERROR \"\\303\"\n#1 ERROR \"\\251\"\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ReferenceTokenDump([]SourceUnit{{ID: "u", Name: tc.file, Content: tc.source}})
			if got != tc.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

func TestReferenceTokenDumpMultifileAndDefaultUnchanged(t *testing.T) {
	units := []SourceUnit{{ID: "a", Name: "a.cl", Content: "class A {};"}, {ID: "b", Name: "dir/b.cl", Content: "class B {};"}}
	want := "#name \"a.cl\"\n#1 CLASS\n#1 TYPEID A\n#1 '{'\n#1 '}'\n#1 ';'\n#name \"dir/b.cl\"\n#1 CLASS\n#1 TYPEID B\n#1 '{'\n#1 '}'\n#1 ';'\n"
	if got := ReferenceTokenDump(units); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	// The default lexer still reports errors as diagnostics, not tokens.
	tokens, diagnostics := LexSource("x", "_a 99999999999")
	for _, token := range tokens {
		if token.Type == TokenError {
			t.Fatal("default lexer emitted an ERROR token")
		}
	}
	if len(diagnostics) != 2 {
		t.Fatalf("default lexer diagnostics changed: %+v", diagnostics)
	}
}

func TestReferenceTokenDumpCarriageReturns(t *testing.T) {
	// A lone CR is whitespace that does not advance the line, and an escaped
	// CR before LF leaves an unescaped newline: the classic CRLF pitfall.
	got := ReferenceTokenDump([]SourceUnit{{ID: "u", Name: "crlf.cl", Content: "a\rb\r\nc \"x\\\r\ny\""}})
	want := "#name \"crlf.cl\"\n#1 OBJECTID a\n#1 OBJECTID b\n#2 OBJECTID c\n#3 ERROR \"Unterminated string constant\"\n#3 OBJECTID y\n#3 ERROR \"EOF in string constant\"\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
