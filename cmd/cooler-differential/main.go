// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ThalesMMS/cooler-compiler/internal/conformance"
)

type repeatedFlag []string

func (values *repeatedFlag) String() string { return fmt.Sprint([]string(*values)) }
func (values *repeatedFlag) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func main() {
	var coolcArgs repeatedFlag
	var runnerArgs repeatedFlag
	var versionArgs repeatedFlag
	root := flag.String("root", "internal/compiler/testdata/conformance", "conformance corpus root")
	repo := flag.String("repo", ".", "repository root")
	reportPath := flag.String("report", "dist/differential-report.json", "machine-readable report path")
	evidenceDir := flag.String("evidence-dir", "dist/differential-evidence", "mismatch evidence directory")
	allowlistPath := flag.String("allowlist", "", "versioned divergence allowlist path")
	coolcPath := flag.String("coolc", os.Getenv("COOLER_REFERENCE_COOLC"), "reference coolc executable or path")
	runnerPath := flag.String("runner", os.Getenv("COOLER_REFERENCE_RUNNER"), "SPIM-compatible reference runtime executable or path")
	rule := flag.String("rule", "", "run fixtures linked to this conformance rule")
	fixture := flag.String("fixture", "", "run one fixture ID")
	strict := flag.Bool("strict", false, "fail when the complete reference toolchain is unavailable")
	timeout := flag.Duration("timeout", 5*time.Second, "timeout for each external invocation")
	outputLimit := flag.Int64("output-limit", 1<<20, "maximum bytes captured from each stdout/stderr stream")
	keepTemporary := flag.Bool("keep-temporary", false, "retain isolated reference work directories for manual debugging")
	phases := flag.Bool("phases", true, "also compare reference lexer/parser/semant output (PA2-PA4)")
	phaseReportPath := flag.String("phase-report", "dist/differential-phases-report.json", "machine-readable phase report path")
	phaseAllowlist := flag.String("phase-allowlist", "", "versioned phase divergence allowlist path")
	lexerPath := flag.String("lexer", os.Getenv("COOLER_REFERENCE_LEXER"), "reference course lexer executable or path")
	parserPath := flag.String("parser", os.Getenv("COOLER_REFERENCE_PARSER"), "reference course parser executable or path")
	semantPath := flag.String("semant", os.Getenv("COOLER_REFERENCE_SEMANT"), "reference course semant executable or path")
	coolRoot := flag.String("cool-root", os.Getenv("COOLER_COOL_ROOT"), "Cool installation root searched for lexer/parser/semant (default /var/tmp/cool)")
	battery := flag.String("phase-battery", "internal/course/testdata/phases", "PA-style fixture directory for phase comparisons")
	mips := flag.Bool("mips", true, "also run Cooler-generated MIPS on the reference SPIM and compare with coolc (PA5)")
	mipsReportPath := flag.String("mips-report", "dist/differential-mips-report.json", "machine-readable MIPS report path")
	trapHandler := flag.String("trap-handler", os.Getenv("COOLER_TRAP_HANDLER"), "Cool runtime trap.handler passed to spim")
	mipsCorpus := flag.String("mips-corpus", "internal/course/testdata/pa5", "PA5 corpus directory; the Cool root's examples/ is added when present")
	gcModes := flag.String("mips-gc", ",-g,-g -t", "comma-separated coolc collector flag sets for the MIPS comparison")
	flag.Var(&coolcArgs, "coolc-arg", "argument placed before source paths; repeat for multiple arguments")
	flag.Var(&runnerArgs, "runner-arg", "argument placed before the generated assembly path; repeat to replace the default -file")
	flag.Var(&versionArgs, "version-arg", "argument used to query tool versions; repeat to replace the default -version")
	flag.Parse()

	config := conformance.DifferentialConfig{
		Root: *root, RepoRoot: *repo, CoolcPath: *coolcPath, RunnerPath: *runnerPath,
		CoolcArgs: coolcArgs, RunnerArgs: runnerArgs, VersionArgs: versionArgs,
		Timeout: *timeout, OutputLimit: *outputLimit, Rule: *rule, Fixture: *fixture,
		Strict: *strict, EvidenceDir: *evidenceDir, AllowlistPath: *allowlistPath,
		KeepTemporary: *keepTemporary,
	}
	if runnerArgs == nil {
		config.RunnerArgs = nil
	}
	if versionArgs == nil {
		config.VersionArgs = nil
	}

	code := runRuntimeDifferential(config, *reportPath)
	if *phases {
		phaseConfig := conformance.PhaseDifferentialConfig{
			Root: *root, RepoRoot: *repo, LexerPath: *lexerPath, ParserPath: *parserPath, SemantPath: *semantPath,
			CoolRoot: *coolRoot, Timeout: *timeout, OutputLimit: *outputLimit, Rule: *rule, Fixture: *fixture,
			Strict: *strict, AllowlistPath: *phaseAllowlist,
		}
		if *battery != "" {
			phaseConfig.BatteryDirs = []string{*battery}
		}
		if phaseCode := runPhaseDifferential(phaseConfig, *phaseReportPath); phaseCode > code {
			code = phaseCode
		}
	}
	if *mips {
		mipsConfig := conformance.MIPSDifferentialConfig{
			SPIMPath: *runnerPath, TrapHandler: *trapHandler, CoolcPath: *coolcPath, CoolRoot: *coolRoot,
			GCModes: strings.Split(*gcModes, ","), Timeout: *timeout, OutputLimit: *outputLimit, Strict: *strict,
		}
		if *mipsCorpus != "" {
			mipsConfig.CorpusDirs = append(mipsConfig.CorpusDirs, *mipsCorpus)
		}
		root := *coolRoot
		if root == "" {
			root = "/var/tmp/cool"
		}
		if info, err := os.Stat(filepath.Join(root, "examples")); err == nil && info.IsDir() {
			mipsConfig.CorpusDirs = append(mipsConfig.CorpusDirs, filepath.Join(root, "examples"))
		}
		if mipsCode := runMIPSDifferential(mipsConfig, *mipsReportPath); mipsCode > code {
			code = mipsCode
		}
	}
	os.Exit(code)
}

func runMIPSDifferential(config conformance.MIPSDifferentialConfig, reportPath string) int {
	report, runErr := conformance.RunMIPSDifferential(config)
	if report.SchemaVersion != "" {
		if err := conformance.WriteMIPSDifferentialReport(reportPath, report); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	if report.SkippedReason != "" {
		fmt.Printf("[SKIP] mips: %s\n", report.SkippedReason)
	}
	for _, result := range report.Results {
		if result.Outcome == "match" {
			continue
		}
		fmt.Printf("[%s] mips %s %q: %v %s\n", result.Outcome, result.Case, result.GC, result.Differences, result.Error)
	}
	fmt.Printf("MIPS differential: cases=%d comparisons=%d matched=%d mismatches=%d reference-errors=%d skipped=%d\n",
		report.Summary.Cases, report.Summary.Comparisons, report.Summary.Matched, report.Summary.Mismatches,
		report.Summary.ReferenceErrors, report.Summary.Skipped)
	fmt.Printf("MIPS report: %s\n", reportPath)
	if runErr != nil {
		fmt.Fprintln(os.Stderr, runErr)
		if errors.Is(runErr, conformance.ErrReferenceUnavailable) {
			return 2
		}
		return 1
	}
	if report.HasFailures() {
		return 1
	}
	return 0
}

func runRuntimeDifferential(config conformance.DifferentialConfig, reportPath string) int {
	report, runErr := conformance.RunDifferential(config)
	if report.SchemaVersion != "" {
		if err := conformance.WriteDifferentialReport(reportPath, report); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	if report.SkippedReason != "" {
		fmt.Printf("[SKIP] %s\n", report.SkippedReason)
	}
	for _, result := range report.Results {
		fmt.Printf("[%s] fixture %s", result.Outcome, result.FixtureID)
		if len(result.Differences) > 0 {
			fmt.Printf(": %v", result.Differences)
		}
		if result.EvidenceBundle != "" {
			fmt.Printf(" (evidence: %s)", result.EvidenceBundle)
		}
		fmt.Println()
	}
	fmt.Printf(
		"Differential: selected=%d matched=%d expected-divergences=%d mismatches=%d unexpected-passes=%d reference-errors=%d runtime-not-compared=%d skipped=%d\n",
		report.Summary.Selected, report.Summary.Matched, report.Summary.ExpectedDivergences,
		report.Summary.UnexpectedMismatches, report.Summary.UnexpectedPasses,
		report.Summary.ReferenceErrors, report.Summary.RuntimeNotCompared, report.Summary.Skipped,
	)
	fmt.Printf("Machine report: %s\n", reportPath)
	if runErr != nil {
		fmt.Fprintln(os.Stderr, runErr)
		if errors.Is(runErr, conformance.ErrReferenceUnavailable) {
			return 2
		}
		return 1
	}
	if report.HasFailures() {
		return 1
	}
	return 0
}

func runPhaseDifferential(config conformance.PhaseDifferentialConfig, reportPath string) int {
	report, runErr := conformance.RunPhaseDifferential(config)
	if report.SchemaVersion != "" {
		if err := conformance.WritePhaseDifferentialReport(reportPath, report); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	if report.SkippedReason != "" {
		fmt.Printf("[SKIP] phases: %s\n", report.SkippedReason)
	}
	for _, result := range report.Results {
		if result.Outcome == "match" {
			continue
		}
		fmt.Printf("[%s] %s %s", result.Outcome, result.Phase, result.Case)
		if result.FirstDiff != "" {
			fmt.Printf(": %s", result.FirstDiff)
		}
		if result.Error != "" {
			fmt.Printf(": %s", result.Error)
		}
		fmt.Println()
	}
	fmt.Printf("Phase differential: cases=%d comparisons=%d matched=%d expected-divergences=%d mismatches=%d reference-errors=%d skipped=%d\n",
		report.Summary.Cases, report.Summary.Comparisons, report.Summary.Matched, report.Summary.ExpectedDivergences,
		report.Summary.Mismatches, report.Summary.ReferenceErrors, report.Summary.Skipped)
	fmt.Printf("Phase report: %s\n", reportPath)
	if runErr != nil {
		fmt.Fprintln(os.Stderr, runErr)
		if errors.Is(runErr, conformance.ErrReferenceUnavailable) {
			return 2
		}
		return 1
	}
	if report.HasFailures() {
		return 1
	}
	return 0
}
