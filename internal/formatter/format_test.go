// SPDX-License-Identifier: AGPL-3.0-or-later
package formatter

import (
	compiler "github.com/ThalesMMS/cooler-compiler/internal/compiler"
	"strings"
	"testing"
)

func TestFormatPreservesTokensCommentsAndIdempotence(t *testing.T) {
	for _, source := range []string{
		`class Main{main():Object{{out_string("hello  😀\\\"-- (* x *)");-- tail
 (* outer (* nested *) done *)let x:Int<-1,y:Int<-2 in (x+y);case 1 of z:Int=>z;esac;}};};`,
		"-- comentário\r\nclass Main { main():Object {\r\n0\r\n}; };\r\n",
		`class Other { f():String {"a\nb"}; };`,
	} {
		formatted, err := Format(source)
		if err != nil {
			t.Fatal(err)
		}
		twice, err := Format(formatted)
		if err != nil || twice != formatted {
			t.Fatalf("not idempotent %q / %q: %v", formatted, twice, err)
		}
		a, _ := compiler.Lex(source)
		b, _ := compiler.Lex(formatted)
		if len(a) != len(b) {
			t.Fatal("token count")
		}
		for i := range a {
			if a[i].Type != b[i].Type || a[i].Lexeme != b[i].Lexeme {
				t.Fatal("token changed")
			}
		}
		for _, comment := range []string{"-- tail", "(* outer (* nested *) done *)", "-- comentário"} {
			if strings.Contains(source, comment) && !strings.Contains(formatted, comment) {
				t.Fatal("comment lost")
			}
		}
	}
	for _, source := range []string{`class Main {`, `class Main {main():Object {"bad};};`, `(* unterminated`, string([]byte{0xff})} {
		if _, err := Format(source); err == nil {
			t.Fatalf("accepted invalid %q", source)
		}
	}
}

func TestFormatExpansionBound(t *testing.T) {
	source := "class Main {main():Object {" + strings.Repeat("{", 100) + strings.Repeat("0;", 10000) + strings.Repeat("};", 99) + "}" + "};};"
	if _, err := Format(source); err == nil {
		t.Fatal("accepted expanded source over limit")
	}
}
