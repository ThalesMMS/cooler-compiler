// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
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

	report, runErr := conformance.RunDifferential(config)
	if report.SchemaVersion != "" {
		if err := conformance.WriteDifferentialReport(*reportPath, report); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
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
	fmt.Printf("Machine report: %s\n", *reportPath)
	if runErr != nil {
		if errors.Is(runErr, conformance.ErrReferenceUnavailable) {
			fmt.Fprintln(os.Stderr, runErr)
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, runErr)
		os.Exit(1)
	}
	if report.HasFailures() {
		os.Exit(1)
	}
}
