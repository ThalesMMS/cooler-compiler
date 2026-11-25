// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler_test

import (
	"context"
	api "github.com/ThalesMMS/cooler-compiler/compiler"
	"reflect"
	"strings"
	"testing"
)

func TestWarningSelectionPreservesEffectsAndErrors(t *testing.T) {
	source := `class Main inherits IO { main():Object { let x:Object <- out_string("effect") in 0 }; };`
	for _, rules := range []string{"", "all", "none", "COL3180", "COL3161"} {
		options := api.DefaultOptions()
		options.WarningRules = rules
		result, err := api.Compile(context.Background(), source, options)
		if err != nil || result.HasErrors() {
			t.Fatalf("%s: %v %+v", rules, err, result.Diagnostics())
		}
		if strings.Join(result.Runtime().Output, "") != "effect" {
			t.Fatal("initializer effect changed")
		}
		want := rules == "" || rules == "all" || rules == "COL3180"
		if (len(result.Diagnostics()) == 1) != want {
			t.Fatalf("rules=%s diagnostics=%+v", rules, result.Diagnostics())
		}
		if result.Report().Options.WarningRules != rules {
			t.Fatal("warning provenance lost")
		}
	}
	options := api.DefaultOptions()
	options.WarningRules = "none"
	result, err := api.Compile(context.Background(), `class Main {main():Object {let x:Int in missing};};`, options)
	if err != nil || !result.HasErrors() {
		t.Fatal("warning suppression hid semantic error")
	}
	options.WarningRules = "COL9999"
	if _, err := api.NormalizeOptions(options); err == nil {
		t.Fatal("unknown warning accepted")
	}
}

func TestUnreachableWarningInCheckAndRun(t *testing.T) {
	for _, mode := range []api.Mode{api.ModeCheck, api.ModeRun} {
		for _, optimization := range []string{api.OptimizationNone, api.OptimizationBasic} {
			options := api.DefaultOptions()
			options.Mode = mode
			options.Optimization = optimization
			options.WarningRules = "COL3181"
			result, err := api.Compile(context.Background(), `class Main inherits IO {main():Object {if true then out_string("chosen") else out_string("dead") fi};};`, options)
			if err != nil || result.HasErrors() || len(result.Diagnostics()) != 1 || result.Diagnostics()[0].Code != "COL3181" {
				t.Fatalf("mode=%s opt=%s err=%v diagnostics=%+v", mode, optimization, err, result.Diagnostics())
			}
			if mode == api.ModeRun && strings.Join(result.Runtime().Output, "") != "chosen" {
				t.Fatal("unreachable warning changed execution")
			}
			options.WarningRules = "none"
			result, err = api.Compile(context.Background(), `class Main {main():Object {if true then 1 else missing fi};};`, options)
			if err != nil || !result.HasErrors() {
				t.Fatal("dead branch errors suppressed")
			}
		}
	}
}

func TestSelfAssignmentFixPreservesRuntime(t *testing.T) {
	for _, source := range []string{
		`class Main inherits IO {x:Int <- 7; main():Object {out_int(x <- x)};};`,
		`class Main inherits IO {f(x:Int):Int {{0;x <- x;}}; main():Object {out_int(f(7))};};`,
		`class Main inherits IO {main():Object {let x:Int <- 7 in out_int(x <- x)};};`,
	} {
		options := api.DefaultOptions()
		options.WarningRules = "COL3182"
		result, err := api.Compile(context.Background(), source, options)
		if err != nil || result.HasErrors() || len(result.Diagnostics()) != 1 {
			t.Fatalf("original: %v %+v", err, result.Diagnostics())
		}
		fix := result.Diagnostics()[0].QuickFix[0]
		fixed := source[:fix.Span.StartOffset] + fix.Replacement + source[fix.Span.EndOffset:]
		updated, err := api.Compile(context.Background(), fixed, options)
		if err != nil || updated.HasErrors() || len(updated.Diagnostics()) != 0 || strings.Join(updated.Runtime().Output, "") != "7" || strings.Join(result.Runtime().Output, "") != "7" {
			t.Fatalf("fix changed behavior: %v %+v", err, updated.Diagnostics())
		}
		options.WarningRules = "none"
		suppressed, err := api.Compile(context.Background(), source, options)
		if err != nil || len(suppressed.Diagnostics()) != 0 {
			t.Fatal("self assignment suppression failed")
		}
	}
}

func TestIsvoidWarningPreservesConstructorEffectsAndFailures(t *testing.T) {
	for _, tc := range []struct {
		initializer string
		allocations int
		fails       bool
	}{
		{`out_string("initialized")`, 0, false},
		{`1 / 0`, 0, true},
		{`out_string("initialized")`, 1, true},
	} {
		source := `class T inherits IO {value:Object <- ` + tc.initializer + `;}; class Main {main():Object {isvoid new T};};`
		options := api.DefaultOptions()
		options.WarningRules = "COL3183"
		if tc.allocations != 0 {
			options.Limits.MaxObjectAllocations = tc.allocations
		}
		result, err := api.Compile(context.Background(), source, options)
		if err != nil || result.HasErrors() != tc.fails {
			t.Fatalf("effects/failure: %v %+v", err, result.Diagnostics())
		}
		found := false
		for _, d := range result.Diagnostics() {
			if d.Code == "COL3183" {
				found = true
				if len(d.QuickFix) != 0 {
					t.Fatal("allocation received unsafe fix")
				}
			}
		}
		if !found {
			t.Fatal("missing allocation warning")
		}
		options.WarningRules = "none"
		suppressed, err := api.Compile(context.Background(), source, options)
		if err != nil || !reflect.DeepEqual(result.Runtime(), suppressed.Runtime()) {
			t.Fatal("warning altered runtime")
		}
		if !tc.fails && strings.Join(result.Runtime().Output, "") != "initialized" {
			t.Fatal("constructor side effect was removed")
		}
	}
}
