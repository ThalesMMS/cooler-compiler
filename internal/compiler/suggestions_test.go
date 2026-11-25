// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"strings"
	"testing"
)

func TestSymbolSuggestions(t *testing.T) {
	for _, tc := range []struct{ name, source, code, token, replacement string }{
		{"parenthesized variable", `class Main {main():Object {let count:Int <- 1 in (cuont)};};`, "COL3100", "cuont", "count"},
		{"assignment", `class Main {count:Int; main():Object {cuont <- 1};};`, "COL3102", "cuont", "count"},
		{"inherited attribute", `class Base {count:Int;}; class Main inherits Base {main():Object {cuont};};`, "COL3100", "cuont", "count"},
		{"shadow", `class Main {count:Int; main():Object {let count:Int <- 2 in cuont};};`, "COL3100", "cuont", "count"},
		{"method transpose", `class Main {main():Object {"x".lenght()};};`, "COL3173", "lenght", "length"},
		{"inherited method", `class Base {count():Int {1};}; class Main inherits Base {main():Object {cuont()};};`, "COL3173", "cuont", "count"},
		{"allocation", `class Main {main():Object {new Strign};};`, "COL3140", "Strign", "String"},
		{"attribute type", `class Main {value:Strign; main():Object {0};};`, "COL3021", "Strign", "String"},
		{"return type", `class Main {main():Strign {""};};`, "COL3031", "Strign", "String"},
		{"formal type", `class Main {f(value:Strign):Object {value}; main():Object {0};};`, "COL3034", "Strign", "String"},
		{"let type", `class Main {main():Object {let value:Strign in value};};`, "COL3121", "Strign", "String"},
		{"case type", `class Main {main():Object {case "" of value:Strign => value; esac};};`, "COL3132", "Strign", "String"},
		{"parent", `class Parent {}; class Main inherits Paren {main():Object {0};};`, "COL3011", "Paren", "Parent"},
		{"static type", `class Parent {count():Int {1};}; class Main inherits Parent {main():Object {self@Paren.count()};};`, "COL3171", "Paren", "Parent"},
		{"ambiguous", `class Main {main():Object {let count:Int, court:Int in cout};};`, "COL3100", "cout", ""},
		{"unavailable method", `class Child {length():Int {1};}; class Main {main():Object {(new Object).lenght()};};`, "COL3173", "lenght", ""},
		{"unavailable static type", `class Other {}; class Main {main():Object {self@Othe.copy()};};`, "COL3171", "Othe", ""},
		{"unavailable variable", `class Other {count:Int;}; class Main {main():Object {cuont};};`, "COL3100", "cuont", ""},
		{"short name", `class Main {main():Object {let x:Int in y};};`, "COL3100", "y", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, a := analyzeTypedProgram(t, tc.source)
			for _, d := range a.Diagnostics {
				if d.Code != tc.code {
					continue
				}
				if tc.replacement == "" {
					if len(d.QuickFix) != 0 {
						t.Fatalf("unsafe suggestion: %+v", d)
					}
					return
				}
				if len(d.QuickFix) != 1 {
					t.Fatalf("missing unique suggestion: %+v", d)
				}
				fix := d.QuickFix[0]
				if fix.Replacement != tc.replacement || tc.source[fix.Span.StartOffset:fix.Span.EndOffset] != tc.token {
					t.Fatalf("wrong token/replacement: %+v", fix)
				}
				fixed := tc.source[:fix.Span.StartOffset] + fix.Replacement + tc.source[fix.Span.EndOffset:]
				_, corrected := analyzeTypedProgram(t, fixed)
				if hasErrors(corrected.Diagnostics) {
					t.Fatalf("fix did not compile: %+v", corrected.Diagnostics)
				}
				return
			}
			t.Fatalf("missing %s in %+v", tc.code, a.Diagnostics)
		})
	}
}

func TestSuggestionBounds(t *testing.T) {
	source := `class Main {main():Object {let ` + strings.Repeat("a", 65) + `:Int in ` + strings.Repeat("a", 64) + `b};};`
	_, a := analyzeTypedProgram(t, source)
	for _, d := range a.Diagnostics {
		if len(d.QuickFix) != 0 {
			t.Fatalf("long name suggestion: %+v", d)
		}
	}
	search := (&analyzer{suggestionBudget: 16384}).newSuggestionSearch()
	search.add("length", true)
	for i := 0; i < 2048; i++ {
		search.add("other", true)
	}
	if search.candidate("lenght") != "" {
		t.Fatal("partial search produced a fix")
	}
	search = (&analyzer{suggestionBudget: 1}).newSuggestionSearch()
	search.add("length", true)
	search.add("other", true)
	if search.candidate("lenght") != "" {
		t.Fatal("analysis budget exhaustion produced a fix")
	}
}

func TestOneEditApart(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"length", "lenght", true}, {"length", "lengt", true}, {"length", "lengthx", true},
		{"length", "xength", true}, {"length", "length", false}, {"length", "lentghx", false},
		{"", "", false}, {"", "a", true}, {"a", "", true}, {"a", "xyz", false},
	} {
		if oneEditApart(tc.a, tc.b) != tc.want || oneEditApart(tc.b, tc.a) != tc.want {
			t.Fatalf("%q / %q", tc.a, tc.b)
		}
	}
}
