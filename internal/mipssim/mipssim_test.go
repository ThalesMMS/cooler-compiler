// SPDX-License-Identifier: AGPL-3.0-or-later

package mipssim_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ThalesMMS/cooler-compiler/examples"
	"github.com/ThalesMMS/cooler-compiler/internal/compiler"
	"github.com/ThalesMMS/cooler-compiler/internal/mipssim"
)

func compileMIPS(t *testing.T, source string, gc compiler.MIPSGCOptions) string {
	t.Helper()
	result := compiler.GenerateMIPS([]compiler.SourceUnit{{ID: "u", Name: "t.cl", Content: source}}, compiler.MIPSOptions{GC: gc})
	if !result.OK {
		t.Fatalf("generation failed: %+v", result.Diagnostics)
	}
	return result.Assembly
}

func runSource(t *testing.T, source string, options mipssim.Options, gc compiler.MIPSGCOptions) mipssim.Result {
	t.Helper()
	return mipssim.Run(context.Background(), compileMIPS(t, source, gc), options)
}

func inputLines(input string) []string {
	if input == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(input, "\n"), "\n")
}

var gcModes = map[string]compiler.MIPSGCOptions{
	"nogc":     {},
	"gengc -g": {Generational: true},
	"-g -t":    {Generational: true, Test: true},
	"-g -t -T": {Generational: true, Test: true, Debug: true},
	"-t -T":    {Test: true, Debug: true},
}

// TestExamplesMatchGoVM runs every example on the simulator under every
// collector configuration and compares stdout with the Go VM's output.
func TestExamplesMatchGoVM(t *testing.T) {
	catalog, err := examples.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, example := range catalog {
		if example.IntentionalError {
			continue
		}
		units := []compiler.SourceUnit{{ID: "u", Name: example.Path, Content: example.Source}}
		if len(example.Files) > 0 {
			units = units[:0]
			for index, file := range example.Files {
				units = append(units, compiler.SourceUnit{ID: string(rune('a' + index)), Name: file.Name, Content: file.Source})
			}
		}
		for name, gc := range gcModes {
			t.Run(example.ID+"/"+name, func(t *testing.T) {
				generated := compiler.GenerateMIPS(units, compiler.MIPSOptions{GC: gc})
				if !generated.OK {
					t.Fatalf("generation failed: %+v", generated.Diagnostics)
				}
				result := mipssim.Run(context.Background(), generated.Assembly, mipssim.Options{Input: inputLines(example.Input)})
				switch example.ID {
				case "10_runtime_division_by_zero":
					if result.Status != mipssim.StatusRuntimeError || result.ErrorCategory != "division-by-zero" {
						t.Fatalf("result %+v", result)
					}
				case "11_dispatch_on_void":
					if result.ErrorCategory != "dispatch-void" || !strings.HasSuffix(result.Output, "11_dispatch_on_void.cl:8: Dispatch to void.\n") {
						t.Fatalf("result %+v", result)
					}
				default:
					if result.Status != mipssim.StatusHalted {
						t.Fatalf("status %s: %s", result.Status, result.Error)
					}
					if got := strings.TrimSuffix(result.Output, mipssim.SuccessMessage); got != example.Expected || !strings.HasSuffix(result.Output, mipssim.SuccessMessage) {
						t.Fatalf("output %q, want %q + success line", result.Output, example.Expected)
					}
				}
				if (gc.Test) && result.GC.Collections < result.GC.Allocations {
					t.Fatalf("-t must collect at every allocation: %+v", result.GC)
				}
			})
		}
	}
}

func TestRuntimeErrors(t *testing.T) {
	cases := []struct {
		name, body, category, output string
	}{
		{"dispatch on void", "let a : A in a.f()", "dispatch-void", "t.cl:3: Dispatch to void.\n"},
		{"case on void", "let a : A in case a of x : Object => x; esac", "case-void", "t.cl:3: Match on void in case statement.\n"},
		{"case without match", "case new A of x : Int => x; esac", "case-no-match", "No match in case statement for Class A\n"},
		{"abort", "{ out_string(\"bye\\n\"); abort(); }", "abort", "bye\nAbort called from class Main\n"},
		{"substr", "\"abc\".substr(2, 5)", "substr", "Length to substr too long\n"},
		{"division", "1 / 0", "division-by-zero", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := "class A { f() : Int { 1 }; };\nclass Main inherits IO {\n  main() : Object { " + tc.body + " };\n};\n"
			result := runSource(t, source, mipssim.Options{}, compiler.MIPSGCOptions{})
			if result.Status != mipssim.StatusRuntimeError || result.ErrorCategory != tc.category || result.Output != tc.output {
				t.Fatalf("result %+v", result)
			}
		})
	}
}

func TestStringsIOAndEquality(t *testing.T) {
	source := `class Main inherits IO {
  main() : Object { {
    out_string(in_string().concat("!").concat("\n"));
    out_int(in_int() + 1);
    out_string("\n");
    out_int("hello".substr(1, 3).length());
    if "ab" = "a".concat("b") then out_string(" eq") else out_string(" ne") fi;
    if 3 = 3 then out_string(" eq") else out_string(" ne") fi;
    if new Main = new Main then out_string(" eq") else out_string(" ne") fi;
    out_string(" ".concat(type_name()).concat("\n"));
    out_int(in_int());
  } };
};
`
	result := runSource(t, source, mipssim.Options{Input: []string{"hi", " 41 "}}, compiler.MIPSGCOptions{Generational: true, Test: true})
	want := "hi!\n42\n3 eq eq ne Main\n0" + mipssim.SuccessMessage
	if result.Status != mipssim.StatusHalted || result.Output != want || result.InputConsumed != 2 {
		t.Fatalf("result %+v", result)
	}
}

func TestInteractiveInputStopsAndReplays(t *testing.T) {
	source := "class Main inherits IO { main() : Object { { out_string(\"name? \"); out_string(in_string()); } }; };"
	first := runSource(t, source, mipssim.Options{Interactive: true}, compiler.MIPSGCOptions{})
	if first.Status != mipssim.StatusWaitingInput || first.Output != "name? " {
		t.Fatalf("first %+v", first)
	}
	second := runSource(t, source, mipssim.Options{Interactive: true, Input: []string{"Ada"}}, compiler.MIPSGCOptions{})
	if second.Status != mipssim.StatusHalted || second.Output != "name? Ada"+mipssim.SuccessMessage {
		t.Fatalf("second %+v", second)
	}
}

func TestLimitsAreDeterministic(t *testing.T) {
	loop := "class Main { main() : Object { while true loop 0 pool }; };"
	result := runSource(t, loop, mipssim.Options{Limits: mipssim.Limits{MaxSteps: 5000}}, compiler.MIPSGCOptions{})
	if result.Status != mipssim.StatusStepLimit || result.Steps != 5001 {
		t.Fatalf("step limit %+v", result)
	}
	recursion := "class Main { f(n : Int) : Int { f(n + 1) }; main() : Object { f(0) }; };"
	result = runSource(t, recursion, mipssim.Options{Limits: mipssim.Limits{StackBytes: 64 << 10}}, compiler.MIPSGCOptions{})
	if result.Status != mipssim.StatusRuntimeError || result.ErrorCategory != "stack-overflow" {
		t.Fatalf("stack %+v", result)
	}
	allocate := "class Main { l : Main; main() : Object { while true loop l <- let m : Main <- new Main in { m.set(l); m; } pool }; set(x : Main) : Main { l <- x }; };"
	result = runSource(t, allocate, mipssim.Options{Limits: mipssim.Limits{MaxHeapBytes: 1 << 20}}, compiler.MIPSGCOptions{Generational: true})
	if result.Status != mipssim.StatusRuntimeError || result.ErrorCategory != "heap-exhausted" || result.GC.Collections == 0 {
		t.Fatalf("heap %+v", result)
	}
	printer := "class Main inherits IO { main() : Object { while true loop out_string(\"xxxxxxxx\") pool }; };"
	result = runSource(t, printer, mipssim.Options{Limits: mipssim.Limits{MaxOutputBytes: 1000}}, compiler.MIPSGCOptions{})
	if result.ErrorCategory != "output-limit" || len(result.Output) > 1000 {
		t.Fatalf("output %+v", result.ErrorCategory)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	result = mipssim.Run(ctx, compileMIPS(t, loop, compiler.MIPSGCOptions{}), mipssim.Options{Limits: mipssim.Limits{MaxSteps: 1 << 30}})
	if result.Status != mipssim.StatusCancelled {
		t.Fatalf("cancel %+v", result.Status)
	}
}

func TestGarbageCollectorReclaimsAndChecksHeaders(t *testing.T) {
	churn := `class Main inherits IO {
  keep : String <- "k";
  main() : Object { let i : Int <- 0 in {
    while i < 3000 loop { keep <- keep.concat("").substr(0, 1); i <- i + 1; } pool;
    out_string(keep);
  } };
};`
	nogc := runSource(t, churn, mipssim.Options{}, compiler.MIPSGCOptions{})
	gen := runSource(t, churn, mipssim.Options{}, compiler.MIPSGCOptions{Generational: true})
	checked := runSource(t, churn, mipssim.Options{}, compiler.MIPSGCOptions{Generational: true, Debug: true})
	for _, result := range []mipssim.Result{nogc, gen, checked} {
		if result.Status != mipssim.StatusHalted || result.Output != "k"+mipssim.SuccessMessage {
			t.Fatalf("result %+v", result)
		}
	}
	if gen.GC.Collector != "GenGC" || gen.GC.Collections == 0 || gen.GC.FreedObjects == 0 || gen.GC.HeapBytes >= nogc.GC.HeapBytes {
		t.Fatalf("collector did not reclaim: gen=%+v nogc=%+v", gen.GC, nogc.GC)
	}
	if checked.GC.HeaderChecks == 0 || gen.GC.AssignmentLogs == 0 {
		t.Fatalf("hooks not exercised: %+v", checked.GC)
	}
}

func TestAssemblyErrorsAreContained(t *testing.T) {
	for _, source := range []string{"\tfrobnicate $t1", "\t.text\n\tjal nowhere", "x:\nx:"} {
		result := mipssim.Run(context.Background(), source, mipssim.Options{})
		if result.Status != mipssim.StatusAssemblyError || result.Error == "" {
			t.Fatalf("%q: %+v", source, result)
		}
	}
	result := mipssim.Run(context.Background(), "\t.data\n\t.text\nmain:\n\tjr $ra\n", mipssim.Options{})
	if result.Status != mipssim.StatusRuntimeError || result.ErrorCategory != "link" {
		t.Fatalf("missing Cool runtime symbols must be a contained error: %+v", result)
	}
}
