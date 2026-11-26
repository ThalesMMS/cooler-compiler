// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import "testing"

func TestReferenceParserDumpValid(t *testing.T) {
	source := "class Main inherits IO {\n  x : Int;\n  main() : Object {\n    out_string(\"hi\\n\")\n  };\n};\n"
	got, ok := ReferenceParserDump([]SourceUnit{{ID: "u", Name: "hello.cl", Content: source}})
	if !ok {
		t.Fatalf("parse failed:\n%s", got)
	}
	want := `#1
_program
  #1
  _class
    Main
    IO
    "hello.cl"
    (
    #2
    _attr
      x
      Int
      #2
      _no_expr
      : _no_type
    #3
    _method
      main
      Object
      #4
      _dispatch
        #4
        _object
          self
        : _no_type
        out_string
        (
        #4
        _string
          "hi\n"
        : _no_type
        )
      : _no_type
    )
`
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestReferenceParserDumpExpressionForms(t *testing.T) {
	source := `class A inherits B {
  f(a : Int, b : Bool) : Object {
    {
      a <- ~a * 2 + 1;
      (new A)@B.g(isvoid a, not b);
      while a < 3 loop a <- a - 1 pool;
      case a of i : Int => i; o : Object => 0; esac;
      if a = 1 then true else false fi;
      (a
        + 1) <= 2;
    }
  };
};
`
	got, ok := ReferenceParserDump([]SourceUnit{{ID: "u", Name: "forms.cl", Content: source}})
	if !ok {
		t.Fatalf("parse failed:\n%s", got)
	}
	for _, fragment := range []string{
		"_assign\n        a\n        #4\n        _plus\n          #4\n          _mul\n            #4\n            _neg",
		"_static_dispatch\n        #5\n        _new\n          A\n        : _no_type\n        B\n        g\n        (\n        #5\n        _isvoid",
		"_comp", "_loop", "_lt", "_sub", "_typcase", "_branch\n          i\n          Int",
		"_cond", "_eq", "_bool\n          1",
		// A parenthesized left operand gives the comparison the '(' line,
		// while the inner node keeps the line of its first token.
		"      #9\n      _leq\n        #9\n        _plus\n          #9\n          _object\n            a",
	} {
		if !contains(trimLines(got), trimLines(fragment)) {
			t.Fatalf("dump lacks %q:\n%s", fragment, got)
		}
	}
}

func TestReferenceParserErrors(t *testing.T) {
	cases := []struct{ name, source, want string }{
		{"missing operand", "class Main {\n main() : Int { 1 + };\n};\n",
			"\"e.cl\", line 2: syntax error at or near '}'\nCompilation halted due to lex and parse errors\n"},
		{"objectid", "class Main {\n main() : Int { x y };\n};\n",
			"\"e.cl\", line 2: syntax error at or near OBJECTID = y\nCompilation halted due to lex and parse errors\n"},
		{"lexer error token", "class Main {\n x : Int <- # ;\n};\n",
			"\"e.cl\", line 2: syntax error at or near ERROR = \"#\"\nCompilation halted due to lex and parse errors\n"},
		{"eof uses last token line", "class Main {\n x : Int;\n\n\n",
			"\"e.cl\", line 2: syntax error at or near EOF\nCompilation halted due to lex and parse errors\n"},
		{"recovery continues with the next feature", "class Main {\n a : Int <- ;\n b : Int;\n c : ;\n};\n",
			"\"e.cl\", line 2: syntax error at or near ';'\n\"e.cl\", line 4: syntax error at or near ';'\nCompilation halted due to lex and parse errors\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ReferenceParserDump([]SourceUnit{{ID: "u", Name: "e.cl", Content: tc.source}})
			if ok || got != tc.want {
				t.Fatalf("ok=%v got:\n%s\nwant:\n%s", ok, got, tc.want)
			}
		})
	}
}

func TestReferenceParserMultifileNames(t *testing.T) {
	got, ok := ReferenceParserDump([]SourceUnit{{ID: "a", Name: "a.cl", Content: "class A {};"}, {ID: "b", Name: "b.cl", Content: "class B {\n};"}})
	if !ok || !contains(got, "  _class\n    A\n    Object\n    \"a.cl\"") || !contains(got, "  #1\n  _class\n    B\n    Object\n    \"b.cl\"") {
		t.Fatalf("got:\n%s", got)
	}
	got, ok = ReferenceParserDump([]SourceUnit{{ID: "a", Name: "a.cl", Content: "class A {};"}, {ID: "b", Name: "b.cl", Content: "class B {\n x };"}})
	if ok || got != "\"b.cl\", line 2: syntax error at or near '}'\nCompilation halted due to lex and parse errors\n" {
		t.Fatalf("got:\n%s", got)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if haystack[index:index+len(needle)] == needle {
			return index
		}
	}
	return -1
}

func trimLines(text string) string {
	lines := []byte{}
	start := 0
	for index := 0; index <= len(text); index++ {
		if index == len(text) || text[index] == '\n' {
			line := text[start:index]
			for len(line) > 0 && line[0] == ' ' {
				line = line[1:]
			}
			lines = append(lines, line...)
			if index < len(text) {
				lines = append(lines, '\n')
			}
			start = index + 1
		}
	}
	return string(lines)
}
