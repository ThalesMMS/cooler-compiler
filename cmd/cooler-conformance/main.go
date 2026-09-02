// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ThalesMMS/cooler-compiler/internal/conformance"
)

func main() {
	root := flag.String("root", "internal/compiler/testdata/conformance", "conformance corpus root")
	repo := flag.String("repo", ".", "repository root used for claim auditing")
	reportPath := flag.String("report", "dist/conformance-report.json", "machine-readable report path")
	summaryPath := flag.String("summary", "docs/conformance-summary.md", "generated human-readable matrix summary")
	flag.Parse()

	report, runErr := conformance.Run(*root, *repo)
	if report.SchemaVersion != "" {
		if err := conformance.WriteReport(*reportPath, report); err != nil {
			fmt.Fprintf(os.Stderr, "write conformance report: %v\n", err)
			os.Exit(1)
		}
		for _, fixture := range report.Fixtures {
			status := "PASS"
			if !fixture.Passed {
				status = "FAIL"
			}
			fmt.Printf("[%s] fixture %s", status, fixture.ID)
			if fixture.Failure != "" {
				fmt.Printf(": %s", fixture.Failure)
			}
			fmt.Println()
		}
		fmt.Printf(
			"Conformance: %d rules, %d fixtures — passed=%d failed=%d partial=%d divergent=%d untested=%d\n",
			report.Summary.Rules, report.Summary.Fixtures, report.Summary.Passed,
			report.Summary.Failed, report.Summary.Partial, report.Summary.Divergent,
			report.Summary.Untested,
		)
		fmt.Printf("Machine report: %s\n", *reportPath)
	}
	if err := conformance.WriteSummary(*root, *repo, *summaryPath); err != nil {
		fmt.Fprintf(os.Stderr, "write conformance summary: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Human summary: %s\n", *summaryPath)
	if runErr != nil {
		fmt.Fprintln(os.Stderr, runErr)
		os.Exit(1)
	}
}
