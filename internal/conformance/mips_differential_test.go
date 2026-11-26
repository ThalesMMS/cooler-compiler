// SPDX-License-Identifier: AGPL-3.0-or-later

package conformance

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ThalesMMS/cooler-compiler/internal/compiler"
	"github.com/ThalesMMS/cooler-compiler/internal/mipssim"
)

// The test binary doubles as a fake spim and a fake coolc so the harness can
// be exercised without the real course tools.
func TestMain(m *testing.M) {
	switch os.Getenv("COOLER_FAKE_TOOL") {
	case "spim":
		os.Exit(fakeSPIM(os.Args[1:]))
	case "coolc":
		os.Exit(fakeCoolc(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func fakeSPIM(args []string) int {
	program := ""
	for index, arg := range args {
		if arg == "-file" && index+1 < len(args) {
			program = args[index+1]
		}
	}
	source, err := os.ReadFile(program)
	if err != nil {
		return 1
	}
	fmt.Println("SPIM Version 6.5 of January 4, 2003")
	fmt.Println("Loaded: /fake/trap.handler")
	fmt.Print(mipssim.Run(context.Background(), string(source), mipssim.Options{}).Output)
	return 0
}

func fakeCoolc(args []string) int {
	var gc compiler.MIPSGCOptions
	file := ""
	for _, arg := range args {
		switch arg {
		case "-g":
			gc.Generational = true
		case "-t":
			gc.Test = true
		default:
			file = arg
		}
	}
	content, err := os.ReadFile(file)
	if err != nil {
		return 1
	}
	result := compiler.GenerateMIPS([]compiler.SourceUnit{{ID: "u", Name: file, Content: string(content)}}, compiler.MIPSOptions{GC: gc})
	assembly := result.Assembly
	if os.Getenv("COOLER_FAKE_COOLC_SKEW") == "1" {
		assembly = strings.Replace(assembly, "\"Hello", "\"Howdy", 1)
	}
	return map[bool]int{true: 0, false: 1}[os.WriteFile(strings.TrimSuffix(file, ".cl")+".s", []byte(assembly), 0o600) == nil]
}

func fakeTool(t *testing.T, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell wrappers require a POSIX shell")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), name)
	script := fmt.Sprintf("#!/bin/sh\nCOOLER_FAKE_TOOL=%s exec %q \"$@\"\n", name, executable)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMIPSDifferentialSkipsWithoutSPIM(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	config := MIPSDifferentialConfig{CorpusDirs: []string{filepath.Join("..", "course", "testdata", "pa5")}, CoolRoot: t.TempDir()}
	report, err := RunMIPSDifferential(config)
	if err != nil || report.SkippedReason == "" || report.Summary.Skipped == 0 {
		t.Fatalf("report=%+v err=%v", report.Summary, err)
	}
	config.Strict = true
	if _, err := RunMIPSDifferential(config); !errors.Is(err, ErrReferenceUnavailable) {
		t.Fatalf("strict err=%v", err)
	}
}

func TestMIPSDifferentialComparesWithCoolcOnSPIM(t *testing.T) {
	config := MIPSDifferentialConfig{
		CorpusDirs: []string{filepath.Join("..", "course", "testdata", "pa5")},
		SPIMPath:   fakeTool(t, "spim"), CoolcPath: fakeTool(t, "coolc"), TrapHandler: "/fake/trap.handler",
	}
	report, err := RunMIPSDifferential(config)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Cases < 8 || report.Summary.Comparisons != 3*report.Summary.Cases || report.HasFailures() {
		for _, result := range report.Results {
			if result.Outcome != "match" {
				t.Logf("%s %q %s %v %s", result.Case, result.GC, result.Outcome, result.Differences, result.Error)
			}
		}
		t.Fatalf("summary %+v", report.Summary)
	}
	categories := map[string]bool{}
	for _, result := range report.Results {
		categories[result.CoolerCategory] = true
	}
	for _, want := range []string{"ok", "dispatch-void", "case-void", "case-no-match"} {
		if !categories[want] {
			t.Fatalf("corpus does not cover %s: %v", want, categories)
		}
	}

	t.Setenv("COOLER_FAKE_COOLC_SKEW", "1")
	report, err = RunMIPSDifferential(config)
	if err != nil || report.Summary.Mismatches == 0 {
		t.Fatalf("a differing reference must be reported: %+v %v", report.Summary, err)
	}
}

func TestMIPSOutputCategory(t *testing.T) {
	for output, want := range map[string]string{
		"x\n" + mipssim.SuccessMessage:               "ok",
		"a.cl:3: Dispatch to void.\n":                "dispatch-void",
		"a.cl:3: Match on void in case statement.\n": "case-void",
		"No match in case statement for Class A\n":   "case-no-match",
		"Abort called from class Main\n":             "abort",
		"Exception occurred at PC=0x00400024\n":      "error",
	} {
		if got := MIPSOutputCategory(output); got != want {
			t.Fatalf("%q: %s, want %s", output, got, want)
		}
	}
}
