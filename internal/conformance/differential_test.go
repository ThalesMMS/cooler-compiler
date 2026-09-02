// SPDX-License-Identifier: AGPL-3.0-or-later

package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDifferentialCommandContainment(t *testing.T) {
	tests := []struct {
		name     string
		mode     string
		timeout  time.Duration
		limit    int64
		category string
	}{
		{name: "pass", mode: "pass", timeout: 5 * time.Second, limit: 1024, category: "success"},
		{name: "fail", mode: "fail", timeout: 5 * time.Second, limit: 1024, category: "nonzero"},
		{name: "timeout", mode: "timeout", timeout: 100 * time.Millisecond, limit: 1024, category: "timeout"},
		{name: "child timeout", mode: "child-timeout", timeout: 2 * time.Second, limit: 1024, category: "timeout"},
		{name: "output overflow", mode: "overflow", timeout: 5 * time.Second, limit: 64, category: "output-limit"},
	}
	if runtime.GOOS != "windows" {
		tests = append(tests, struct {
			name     string
			mode     string
			timeout  time.Duration
			limit    int64
			category string
		}{name: "crash", mode: "crash", timeout: 5 * time.Second, limit: 1024, category: "crash"})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := executeDifferentialCommand(os.Args[0], helperArgs(test.mode), t.TempDir(), "", test.timeout, test.limit)
			if result.ExitCategory != test.category {
				t.Fatalf("exit category = %q, want %q; result=%+v", result.ExitCategory, test.category, result)
			}
			if test.category == "output-limit" && int64(len(result.Stdout)) > test.limit {
				t.Fatalf("captured %d bytes with limit %d", len(result.Stdout), test.limit)
			}
		})
	}
}

func TestDifferentialNormalizationPreservesProgramOutput(t *testing.T) {
	program := "\n  user output  \r\n"
	if got := normalizeReferenceRuntimeOutput(program, ""); got != "\n  user output  \n" {
		t.Fatalf("program output normalization = %q", got)
	}
	banner := "SPIM Version 8.0\nCopyright 1990\nLoaded: trap.handler\nprogram\n"
	if got := normalizeReferenceRuntimeOutput(banner, ""); got != "program\n" {
		t.Fatalf("SPIM banner normalization = %q", got)
	}
	classic := "SPIM Version 6.5\nCopyright 1990-2003\nAll Rights Reserved.\nSee the file README for a full copyright notice.\nLoaded: ../lib/trap.handler\nB3COOL program successfully executed\n"
	if got := normalizeReferenceRuntimeOutput(classic, ""); got != "B3" {
		t.Fatalf("classic SPIM footer normalization = %q", got)
	}
	programFooter := "SPIM Version 6.5\nLoaded: trap.handler\nCOOL program successfully executed\nCOOL program successfully executed\n"
	if got := normalizeReferenceRuntimeOutput(programFooter, ""); got != "COOL program successfully executed\n" {
		t.Fatalf("program output matching footer was removed = %q", got)
	}
	if got := normalizeProgramOutput("<path>\r\n", "<path>"); got != "<TMP>\n" {
		t.Fatalf("path/newline normalization = %q", got)
	}
}

func TestDifferentialClassifiesClassicReferenceDiagnostics(t *testing.T) {
	compileCases := []string{
		`"tokens.cl", line 1: syntax error at or near ERROR = _`,
		`"tokens.cl", line 1: syntax error at or near INT_CONST = 2147483648`,
	}
	for _, diagnostic := range compileCases {
		if stage, category := classifyReferenceCompileFailure(diagnostic); stage != "lexer" || category != "lexer" {
			t.Fatalf("compile diagnostic %q classified as %s/%s", diagnostic, stage, category)
		}
	}
	if stage, category := classifyReferenceCompileFailure(`"main.cl", line 1: syntax error at or near INT_CONST = 42`); stage != "parser" || category != "parser" {
		t.Fatalf("ordinary parser diagnostic classified as %s/%s", stage, category)
	}

	runtimeCases := []struct {
		output   string
		category string
		program  string
	}{
		{"prefixAbort called from class Main\n", "abort", "prefix"},
		{"  Exception 9  [Breakpoint/Division by 0]  Execution aborted\n", "division-by-zero", ""},
		{"main.cl:4Match on void in case statement.\n", "case-on-void", ""},
		{"Length to substr too long\nExecution aborted.\n", "substring-range", ""},
		{"  Exception 12  [Arithmetic overflow]  Execution aborted\n", "arithmetic-overflow", ""},
	}
	for _, test := range runtimeCases {
		if got := classifyReferenceRuntimeError(test.output); got != test.category {
			t.Fatalf("runtime diagnostic %q classified as %q, want %q", test.output, got, test.category)
		}
		if got := stripReferenceRuntimeDiagnostic(test.output, test.category); got != test.program {
			t.Fatalf("runtime diagnostic %q left output %q, want %q", test.output, got, test.program)
		}
	}
}

func TestDifferentialHarnessPassMismatchEvidenceAndStaleAllowlist(t *testing.T) {
	repoRoot, corpusRoot := writeDifferentialTestCorpus(t, nil, false)
	config := differentialTestConfig(repoRoot, corpusRoot, "runner-match")
	report, err := RunDifferential(config)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Matched != 1 || report.HasFailures() || !report.Results[0].RuntimeCompared {
		t.Fatalf("matching report = %+v", report)
	}

	config = differentialTestConfig(repoRoot, corpusRoot, "runner-mismatch")
	report, err = RunDifferential(config)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.UnexpectedMismatches != 1 || !report.HasFailures() {
		t.Fatalf("mismatch report = %+v", report)
	}
	evidence := report.Results[0].EvidenceBundle
	for _, path := range []string{
		filepath.Join(evidence, "evidence.json"),
		filepath.Join(evidence, "input.txt"),
		filepath.Join(evidence, "sources", "main.cl"),
	} {
		if info, statErr := os.Stat(path); statErr != nil || !info.Mode().IsRegular() {
			t.Fatalf("missing evidence file %s: %v", path, statErr)
		}
	}

	allowlist := []DifferentialAllowlistEntry{testAllowlistEntry()}
	repoRoot, corpusRoot = writeDifferentialTestCorpus(t, allowlist, false)
	config = differentialTestConfig(repoRoot, corpusRoot, "runner-mismatch")
	report, err = RunDifferential(config)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.ExpectedDivergences != 1 || report.HasFailures() {
		t.Fatalf("allowed divergence report = %+v", report)
	}

	config = differentialTestConfig(repoRoot, corpusRoot, "runner-match")
	report, err = RunDifferential(config)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.UnexpectedPasses != 1 || !report.HasFailures() {
		t.Fatalf("stale allowlist report = %+v", report)
	}

	repoRoot, corpusRoot = writeDifferentialTestCorpus(t, nil, false)
	config = differentialTestConfig(repoRoot, corpusRoot, "timeout")
	config.Timeout = 2 * time.Second
	report, err = RunDifferential(config)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.ReferenceErrors != 1 || !report.HasFailures() {
		t.Fatalf("unlisted reference timeout report = %+v", report)
	}
	repoRoot, corpusRoot = writeDifferentialTestCorpus(t, allowlist, false)
	config = differentialTestConfig(repoRoot, corpusRoot, "timeout")
	config.Timeout = 2 * time.Second
	report, err = RunDifferential(config)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.ExpectedDivergences != 1 || report.HasFailures() || report.Results[0].Reference.InfrastructureError != "reference runtime timeout" {
		t.Fatalf("allowlisted reference timeout report = %+v", report)
	}
}

func TestDifferentialHarnessMultifileInputAndMalformedArtifact(t *testing.T) {
	repoRoot, corpusRoot := writeDifferentialTestCorpus(t, nil, true)
	config := differentialTestConfig(repoRoot, corpusRoot, "runner-input")
	report, err := RunDifferential(config)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Matched != 1 || report.Results[0].Reference.Stdout != "hello" {
		t.Fatalf("multifile/input report = %+v", report)
	}
	args := report.Results[0].Reference.CompileCommand.Args
	if !containsString(args, "helper.cl") || !containsString(args, "main.cl") {
		t.Fatalf("reference compile args omit multifile sources: %v", args)
	}

	config.CoolcArgs = helperArgs("compile-no-artifact")
	report, err = RunDifferential(config)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.ReferenceErrors != 1 || !strings.Contains(report.Results[0].Reference.InfrastructureError, "malformed output") {
		t.Fatalf("malformed artifact report = %+v", report)
	}

	config.CoolcArgs = helperArgs("compile-reject")
	report, err = RunDifferential(config)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.UnexpectedMismatches != 1 || report.Summary.ReferenceErrors != 0 || report.Results[0].Reference.FirstFailingStage != "parser" {
		t.Fatalf("ordinary compiler rejection report = %+v", report)
	}
}

func TestDifferentialUnavailableSkipsUnlessStrict(t *testing.T) {
	repoRoot, corpusRoot := writeDifferentialTestCorpus(t, nil, false)
	config := differentialTestConfig(repoRoot, corpusRoot, "runner-match")
	config.CoolcPath = filepath.Join(t.TempDir(), "missing-coolc")
	report, err := RunDifferential(config)
	if err != nil || report.Summary.Skipped != 1 {
		t.Fatalf("optional unavailable report=%+v err=%v", report, err)
	}
	config.Strict = true
	if _, err := RunDifferential(config); err == nil || !strings.Contains(err.Error(), ErrReferenceUnavailable.Error()) {
		t.Fatalf("strict unavailable error = %v", err)
	}
	config.CoolcPath = os.Args[0]
	config.CoolcArgs = helperArgs("compile-pass")
	config.VersionArgs = helperArgs("version")
	config.RunnerPath = filepath.Join(t.TempDir(), "missing-runner")
	if report, err := RunDifferential(config); err == nil || report.Summary.Skipped != 1 || !strings.Contains(report.SkippedReason, "runtime runner") {
		t.Fatalf("strict missing runner report=%+v err=%v", report, err)
	}
}

func TestDifferentialHelperProcess(t *testing.T) {
	separator := -1
	for index, arg := range os.Args {
		if arg == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		return
	}
	mode := os.Args[separator+1]
	switch mode {
	case "pass":
		fmt.Print("ok")
	case "fail":
		os.Exit(7)
	case "timeout":
		time.Sleep(10 * time.Second)
	case "child-timeout":
		child := exec.CommandContext(context.Background(), os.Args[0], helperArgs("timeout")...)
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(10)
		}
		time.Sleep(10 * time.Second)
	case "overflow":
		fmt.Print(strings.Repeat("x", 4096))
	case "crash":
		process, _ := os.FindProcess(os.Getpid())
		_ = process.Kill()
		select {}
	case "version":
		fmt.Print("fake-cool-tool 1.2.3")
	case "compile-pass":
		if err := os.WriteFile("program.s", []byte("fake assembly\n"), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(8)
		}
	case "compile-no-artifact":
		fmt.Print("accepted without artifact")
	case "compile-reject":
		fmt.Fprintln(os.Stderr, "syntax error at main.cl:1")
		os.Exit(1)
	case "runner-match":
		fmt.Print("ok")
	case "runner-mismatch":
		fmt.Print("wrong")
	case "runner-input":
		data, _ := io.ReadAll(os.Stdin)
		fmt.Print(strings.TrimSuffix(string(data), "\n"))
	default:
		fmt.Fprintf(os.Stderr, "unknown helper mode %q", mode)
		os.Exit(9)
	}
	os.Exit(0)
}

func helperArgs(mode string) []string {
	return []string{"-test.run=^TestDifferentialHelperProcess$", "--", mode}
}

func differentialTestConfig(repoRoot, corpusRoot, runnerMode string) DifferentialConfig {
	return DifferentialConfig{
		Root: corpusRoot, RepoRoot: repoRoot,
		CoolcPath: os.Args[0], RunnerPath: os.Args[0],
		CoolcArgs: helperArgs("compile-pass"), RunnerArgs: helperArgs(runnerMode), VersionArgs: helperArgs("version"),
		Timeout: 5 * time.Second, OutputLimit: 4096,
		EvidenceDir: filepath.Join(repoRoot, "evidence"),
	}
}

func writeDifferentialTestCorpus(t *testing.T, entries []DifferentialAllowlistEntry, multifile bool) (string, string) {
	t.Helper()
	repoRoot := t.TempDir()
	corpusRoot := filepath.Join(repoRoot, "corpus")
	writeTestFile(t, filepath.Join(repoRoot, "README.md"), "differential test claim\n")
	rule := Rule{
		ID: "RUN-DIFFERENTIAL", Name: "differential", Citation: Citation{Source: "manual", Section: "runtime"},
		ExpectedStage: "runtime", Behavior: "same observable output", Status: "supported",
		PositiveFixtures: []string{"differential-valid"}, ReferenceComparison: "same",
	}
	matrix := Matrix{
		SchemaVersion: schemaVersion, Project: "differential-test",
		NormativeHierarchy: []NormativeSource{{Rank: 1, ID: "manual", Title: "Manual", Locator: "manual", Role: "normative"}},
		StatusDefinitions: map[string]string{
			"supported": "yes", "partial": "some", "intentional-divergence": "different",
			"unsupported": "no", "not-yet-tested": "unknown",
		},
		Claims: []DocumentClaim{{ID: "claim", Document: "README.md", Contains: "differential test claim", RuleIDs: []string{rule.ID}}},
		Rules:  []Rule{rule},
	}
	writeJSONTestFile(t, filepath.Join(corpusRoot, "matrix.json"), matrix)
	fixtureDir := filepath.Join(corpusRoot, "runtime", "differential-valid")
	fixture := Fixture{
		SchemaVersion: schemaVersion, ID: "differential-valid", Title: "differential",
		RuleIDs: []string{rule.ID}, Validity: "valid", ExpectedLastSuccessfulStage: "runtime",
		ExpectedDiagnostics: []ExpectedDiagnostic{}, ExpectedOutput: "ok", Sources: []string{"main.cl"},
		Provenance: "original differential self-test",
	}
	mainSource := `class Main inherits IO { main() : Object { out_string("ok") }; };`
	if multifile {
		fixture.Sources = []string{"helper.cl", "main.cl"}
		fixture.InputLines = []string{"hello"}
		fixture.ExpectedOutput = "hello"
		writeTestFile(t, filepath.Join(fixtureDir, "helper.cl"), "class Helper { value() : Int { 1 }; };\n")
		mainSource = `class Main inherits IO { main() : Object { out_string(in_string()) }; };`
	}
	writeTestFile(t, filepath.Join(fixtureDir, "main.cl"), mainSource+"\n")
	writeJSONTestFile(t, filepath.Join(fixtureDir, "fixture.json"), fixture)
	if entries == nil {
		entries = []DifferentialAllowlistEntry{}
	}
	writeJSONTestFile(t, filepath.Join(corpusRoot, "differential-allowlist.json"), DifferentialAllowlist{SchemaVersion: differentialSchemaVersion, Entries: entries})
	return repoRoot, corpusRoot
}

func testAllowlistEntry() DifferentialAllowlistEntry {
	return DifferentialAllowlistEntry{
		ID: "known-output", RuleID: "RUN-DIFFERENTIAL", FixtureID: "differential-valid",
		Reason: "self-test", Evidence: "fake reference output", Issue: "https://example.invalid/issues/1",
		ExpectedCooler: "ok", ExpectedReference: "wrong", RemovalCondition: "fake runner matches",
	}
}

func writeJSONTestFile(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, path, string(data)+"\n")
}
