// SPDX-License-Identifier: AGPL-3.0-or-later

package conformance

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// fakeCourseTools builds coolerc and writes lexer/parser/semant shell
// wrappers that behave like the course tools on the files named in their
// input. The real course binaries are never required by this test.
func fakeCourseTools(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell wrappers require a POSIX shell")
	}
	dir := t.TempDir()
	coolerc := filepath.Join(dir, "coolerc")
	build := exec.Command("go", "build", "-o", coolerc, "./cmd/coolerc")
	build.Dir = filepath.Join("..", "..")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build coolerc: %v\n%s", err, output)
	}
	scripts := map[string]string{
		"lexer": `exec "` + coolerc + `" emit tokens --format=reference "$@"`,
		// The parser and semant learn the file names from their input.
		"parser": `set -- $(sed -n 's/^#name "\(.*\)"$/\1/p'); exec "` + coolerc + `" emit ast --format=reference "$@"`,
		"semant": `set -- $(grep -E '^    "' | tr -d '" ' | awk '!seen[$0]++'); exec "` + coolerc + `" emit ast --format=reference --stage=semant "$@"`,
	}
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func writePhaseBattery(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestPhaseDifferentialSkipsWithoutTools(t *testing.T) {
	empty := t.TempDir()
	t.Setenv("PATH", empty)
	config := PhaseDifferentialConfig{BatteryDirs: []string{writePhaseBattery(t, map[string]string{"a.cl": "class A {};"})}, CoolRoot: empty}
	report, err := RunPhaseDifferential(config)
	if err != nil || report.SkippedReason == "" || report.Summary.Skipped != 1 {
		t.Fatalf("report=%+v err=%v", report.Summary, err)
	}
	config.Strict = true
	if _, err := RunPhaseDifferential(config); !errors.Is(err, ErrReferenceUnavailable) {
		t.Fatalf("strict err=%v", err)
	}
}

func TestPhaseDifferentialComparesEveryPhase(t *testing.T) {
	tools := fakeCourseTools(t)
	battery := writePhaseBattery(t, map[string]string{
		"ok.cl":     "class Main inherits IO {\n  main() : Object { out_string(\"hi\") };\n};\n",
		"semant.cl": "class Main {\n  main() : Int { x };\n};\n",
		"parse.cl":  "class Main {\n  main() : Int { 1 + };\n};\n",
	})
	config := PhaseDifferentialConfig{
		BatteryDirs: []string{battery},
		LexerPath:   filepath.Join(tools, "lexer"), ParserPath: filepath.Join(tools, "parser"), SemantPath: filepath.Join(tools, "semant"),
	}
	report, err := RunPhaseDifferential(config)
	if err != nil {
		t.Fatal(err)
	}
	// ok.cl and semant.cl reach semant; parse.cl stops after the parser.
	if report.Summary.Comparisons != 8 || report.Summary.Matched != 8 || report.HasFailures() {
		for _, result := range report.Results {
			t.Logf("%s %s %s %s\ncooler:\n%s\nreference:\n%s", result.Case, result.Phase, result.Outcome, result.FirstDiff, result.Cooler, result.Reference)
		}
		t.Fatalf("summary %+v", report.Summary)
	}
	if !report.Tools.Lexer.Available || report.Tools.Lexer.SHA256 == "" {
		t.Fatalf("tool fingerprint missing: %+v", report.Tools.Lexer)
	}
}

func TestPhaseDifferentialMismatchAndAllowlist(t *testing.T) {
	tools := t.TempDir()
	lexer := filepath.Join(tools, "lexer")
	if err := os.WriteFile(lexer, []byte("#!/bin/sh\necho '#name \"a.cl\"'\necho '#1 CLASS'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	battery := writePhaseBattery(t, map[string]string{"a.cl": "class A {};"})
	config := PhaseDifferentialConfig{BatteryDirs: []string{battery}, LexerPath: lexer, CoolRoot: t.TempDir()}
	t.Setenv("PATH", t.TempDir())
	report, err := RunPhaseDifferential(config)
	if err != nil || report.Summary.Mismatches != 1 || report.Results[0].FirstDiff == "" {
		t.Fatalf("report=%+v err=%v", report.Summary, err)
	}
	allowlist := filepath.Join(t.TempDir(), "allow.json")
	if err := os.WriteFile(allowlist, []byte(`{"schemaVersion":"1.0.0","entries":[{"id":"a","case":"battery/a.cl","phase":"tokens","reason":"test","evidence":"fake lexer","removalCondition":"never"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	config.AllowlistPath = allowlist
	report, err = RunPhaseDifferential(config)
	if err != nil || report.Summary.ExpectedDivergences != 1 || report.HasFailures() {
		t.Fatalf("report=%+v err=%v", report.Summary, err)
	}
}
