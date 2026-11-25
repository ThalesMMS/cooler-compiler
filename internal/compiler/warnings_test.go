// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"strings"
	"testing"
)

func TestUnusedBindingWarnings(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		unused     int
	}{
		{"unused", "let x:Int <- 1 in 0", 1},
		{"read", "let x:Int <- 1 in x", 0},
		{"write only", "let x:Int <- 1 in x <- 2", 1},
		{"read in assignment", "let x:Int <- 1 in x <- x + 1", 0},
		{"previous initializer", "let x:Int <- 1, y:Int <- x in y", 0},
		{"same list shadow", "let x:Int <- 1, x:Int <- 2 in x", 1},
		{"nested shadow", "let x:Int <- 1 in let x:Int <- 2 in x", 1},
		{"case unused", "case 1 of x:Int => 0; esac", 1},
		{"case read", "case 1 of x:Int => x; esac", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, analysis := analyzeTypedProgram(t, "class Main { main():Object { "+tc.body+" }; };")
			found := 0
			for _, d := range analysis.Diagnostics {
				if d.Code == "COL3180" {
					found++
					if d.Severity != SeverityWarning || !strings.Contains(d.Message, "never read") || d.Span.EndOffset-d.Span.StartOffset != 1 {
						t.Fatalf("bad warning: %+v", d)
					}
				}
			}
			if found != tc.unused {
				t.Fatalf("unused=%d want=%d diagnostics=%+v", found, tc.unused, analysis.Diagnostics)
			}
		})
	}
}

func TestRedundantIsvoidWarnings(t *testing.T) {
	for _, tc := range []struct {
		body  string
		count int
	}{
		{"isvoid 1", 1}, {`isvoid "text"`, 1}, {"isvoid true", 1},
		{"isvoid new Object", 1}, {"isvoid new SELF_TYPE", 1},
		{"let x:Object in isvoid x", 0}, {"isvoid self", 0},
		{"isvoid new Missing", 0}, {"isvoid (if true then 1 else 2 fi)", 0},
	} {
		source := "class Main {main():Object {" + tc.body + "};};"
		_, a := analyzeTypedProgram(t, source)
		found := 0
		for _, d := range a.Diagnostics {
			if d.Code == "COL3183" {
				found++
				if d.Severity != SeverityWarning || len(d.QuickFix) != 0 || source[d.Span.StartOffset:d.Span.EndOffset] != tc.body {
					t.Fatalf("unsafe isvoid warning: %+v", d)
				}
			}
		}
		if found != tc.count {
			t.Fatalf("%s: found=%d want=%d", tc.body, found, tc.count)
		}
	}
}

func TestSelfAssignmentWarningAndFix(t *testing.T) {
	for _, source := range []string{
		`class Main {x:Int; main():Object {x <- x};};`,
		`class Main {f(x:Int):Int {x <- x};main():Object {0};};`,
		`class Main {main():Object {let x:Int <- 3 in {0; x <- x;}};};`,
		`class Main {x:Int; main():Object {let x:Int <- 3 in x <- x};};`,
		`class Main inherits IO {main():Object {let x:Int <- 3 in out_int(x <- x)};};`,
	} {
		_, a := analyzeTypedProgram(t, source)
		found := 0
		for _, d := range a.Diagnostics {
			if d.Code == "COL3182" {
				found++
				if len(d.QuickFix) != 1 || d.QuickFix[0].Replacement != "x" {
					t.Fatalf("fix: %+v", d)
				}
				fix := d.QuickFix[0]
				if source[fix.Span.StartOffset:fix.Span.EndOffset] != "x <- x" {
					t.Fatalf("span: %+v", fix)
				}
				fixed := source[:fix.Span.StartOffset] + fix.Replacement + source[fix.Span.EndOffset:]
				_, updated := analyzeTypedProgram(t, fixed)
				assertNoErrors(t, updated.Diagnostics)
			}
		}
		if found != 1 {
			t.Fatalf("expected self assignment: %s %+v", source, a.Diagnostics)
		}
	}
	for _, body := range []string{"let x:Int in x <- (let x:Int in x)", "missing <- missing", "self <- self"} {
		_, a := analyzeTypedProgram(t, "class Main {main():Object {"+body+"};};")
		for _, d := range a.Diagnostics {
			if d.Code == "COL3182" {
				t.Fatalf("unsafe self assignment warning: %s", body)
			}
		}
	}
}

func TestLiteralUnreachableWarnings(t *testing.T) {
	for _, tc := range []struct {
		body, dead string
		count      int
	}{
		{"if true then 1 else 22 fi", "22", 1},
		{"if false then 11 else 2 fi", "11", 1},
		{"while false loop 33 pool", "33", 1},
		{"while true loop 1 pool", "", 0},
		{"if 1 < 2 then 1 else 2 fi", "", 0},
		{"if { true; } then 1 else 2 fi", "", 0},
	} {
		source := "class Main {main():Object {" + tc.body + "};};"
		_, a := analyzeTypedProgram(t, source)
		count := 0
		for _, d := range a.Diagnostics {
			if d.Code == "COL3181" {
				count++
				if source[d.Span.StartOffset:d.Span.EndOffset] != tc.dead {
					t.Fatalf("incorrect unreachable span %+v", d)
				}
			}
		}
		if count != tc.count {
			t.Fatalf("%s: count=%d want=%d", tc.body, count, tc.count)
		}
	}
	_, a := analyzeTypedProgram(t, "class Main {main():Object {if true then 1 else missing fi};};")
	foundError, foundWarning := false, false
	for _, d := range a.Diagnostics {
		foundError = foundError || d.Code == "COL3100"
		foundWarning = foundWarning || d.Code == "COL3181"
	}
	if !foundError || !foundWarning {
		t.Fatalf("dead branch must still be checked: %+v", a.Diagnostics)
	}
}
