// SPDX-License-Identifier: AGPL-3.0-or-later

package conformance

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/ThalesMMS/cooler-compiler/internal/compiler"
	"github.com/ThalesMMS/cooler-compiler/internal/mipssim"
	"github.com/ThalesMMS/cooler-compiler/internal/reftools"
)

// The MIPS differential (PA5, CI/dev only) runs Cooler-generated MIPS on a
// user-supplied real SPIM with trap.handler and compares program stdout and
// the runtime-error category with the reference coolc's code on the same
// SPIM, under each collector configuration. It also checks the embedded
// simulator against real SPIM. The shipped app never uses these tools.

const mipsDifferentialSchemaVersion = "1.0.0"

type MIPSDifferentialConfig struct {
	CorpusDirs  []string
	SPIMPath    string
	TrapHandler string
	CoolcPath   string
	CoolRoot    string
	// GCModes are coolc collector flag sets, e.g. "", "-g", "-g -t".
	GCModes     []string
	Timeout     time.Duration
	OutputLimit int64
	Strict      bool
}

type MIPSDifferentialReport struct {
	SchemaVersion string                   `json:"schemaVersion"`
	GeneratedAt   string                   `json:"generatedAt"`
	Host          DifferentialHost         `json:"host"`
	Tools         MIPSDifferentialTools    `json:"tools"`
	Summary       MIPSDifferentialSummary  `json:"summary"`
	SkippedReason string                   `json:"skippedReason,omitempty"`
	Results       []MIPSDifferentialResult `json:"results"`
}

type MIPSDifferentialTools struct {
	SPIM        DifferentialTool `json:"spim"`
	TrapHandler string           `json:"trapHandler,omitempty"`
	Coolc       DifferentialTool `json:"coolc"`
}

type MIPSDifferentialSummary struct {
	Cases           int `json:"cases"`
	Comparisons     int `json:"comparisons"`
	Matched         int `json:"matched"`
	Mismatches      int `json:"mismatches"`
	ReferenceErrors int `json:"referenceErrors"`
	Skipped         int `json:"skipped"`
}

type MIPSDifferentialResult struct {
	Case              string   `json:"case"`
	GC                string   `json:"gc"`
	Outcome           string   `json:"outcome"`
	Differences       []string `json:"differences,omitempty"`
	CoolerOnSPIM      string   `json:"coolerOnSpim"`
	Simulator         string   `json:"simulator"`
	ReferenceOnSPIM   string   `json:"referenceOnSpim,omitempty"`
	CoolerCategory    string   `json:"coolerCategory"`
	ReferenceCategory string   `json:"referenceCategory,omitempty"`
	Error             string   `json:"error,omitempty"`
}

// MIPSOutputCategory classifies a Cool program's SPIM output.
func MIPSOutputCategory(output string) string {
	switch {
	case strings.Contains(output, "Dispatch to void."):
		return "dispatch-void"
	case strings.Contains(output, "Match on void in case statement."):
		return "case-void"
	case strings.Contains(output, "No match in case statement for Class"):
		return "case-no-match"
	case strings.Contains(output, "Abort called from class"):
		return "abort"
	case strings.Contains(output, "substr"):
		return "substr"
	case strings.HasSuffix(output, mipssim.SuccessMessage):
		return "ok"
	}
	return "error"
}

// RunMIPSDifferential runs the comparison; it skips when spim is absent.
func RunMIPSDifferential(config MIPSDifferentialConfig) (MIPSDifferentialReport, error) {
	if config.Timeout <= 0 {
		config.Timeout = 10 * time.Second
	}
	if config.OutputLimit <= 0 {
		config.OutputLimit = 1 << 20
	}
	if len(config.GCModes) == 0 {
		config.GCModes = []string{"", "-g", "-g -t"}
	}
	report := MIPSDifferentialReport{
		SchemaVersion: mipsDifferentialSchemaVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		Host:          DifferentialHost{OS: runtime.GOOS, Arch: runtime.GOARCH, GoVersion: runtime.Version()},
		Results:       []MIPSDifferentialResult{},
	}
	cases, err := mipsCases(config)
	if err != nil {
		return report, err
	}
	report.Summary.Cases = len(cases)
	spim := reftools.LocateReferenceTool(config.SPIMPath, "spim", config.CoolRoot)
	coolc := reftools.LocateReferenceTool(config.CoolcPath, "coolc", config.CoolRoot)
	limits := DifferentialConfig{Timeout: config.Timeout, OutputLimit: config.OutputLimit}
	report.Tools = MIPSDifferentialTools{
		SPIM:  inspectDifferentialTool(spim, nil, nil, limits),
		Coolc: inspectDifferentialTool(coolc, nil, nil, limits),
	}
	trap := config.TrapHandler
	if trap == "" && config.CoolRoot != "" {
		if candidate := filepath.Join(config.CoolRoot, "lib", "trap.handler"); fileExists(candidate) {
			trap = candidate
		}
	}
	report.Tools.TrapHandler = trap
	if spim == "" {
		report.SkippedReason = "reference spim was not found; set -runner, COOLER_REFERENCE_RUNNER, or -cool-root"
		report.Summary.Skipped = len(cases)
		if config.Strict {
			return report, fmt.Errorf("%w: %s", ErrReferenceUnavailable, report.SkippedReason)
		}
		return report, nil
	}
	if config.Strict && coolc == "" {
		report.SkippedReason = "reference coolc was not found"
		return report, fmt.Errorf("%w: %s", ErrReferenceUnavailable, report.SkippedReason)
	}
	tools := reftools.SPIMTools{SPIM: spim, TrapHandler: trap}
	for _, path := range cases {
		for _, mode := range config.GCModes {
			result, err := compareMIPSCase(config, tools, coolc, path, mode)
			if err != nil {
				return report, err
			}
			report.Summary.Comparisons++
			switch result.Outcome {
			case "match":
				report.Summary.Matched++
			case "mismatch":
				report.Summary.Mismatches++
			default:
				report.Summary.ReferenceErrors++
			}
			report.Results = append(report.Results, result)
		}
	}
	return report, nil
}

// HasFailures reports unexplained differences or reference failures.
func (r MIPSDifferentialReport) HasFailures() bool {
	return r.Summary.Mismatches > 0 || r.Summary.ReferenceErrors > 0
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func mipsCases(config MIPSDifferentialConfig) ([]string, error) {
	var cases []string
	for _, dir := range config.CorpusDirs {
		matches, err := filepath.Glob(filepath.Join(dir, "*.cl"))
		if err != nil {
			return nil, err
		}
		sort.Strings(matches)
		cases = append(cases, matches...)
	}
	return cases, nil
}

func gcOptions(mode string) compiler.MIPSGCOptions {
	var options compiler.MIPSGCOptions
	for _, flag := range strings.Fields(mode) {
		switch flag {
		case "-g":
			options.Generational = true
		case "-t":
			options.Test = true
		case "-T":
			options.Debug = true
		}
	}
	return options
}

func compareMIPSCase(config MIPSDifferentialConfig, tools reftools.SPIMTools, coolc, path, mode string) (MIPSDifferentialResult, error) {
	result := MIPSDifferentialResult{Case: path, GC: mode}
	content, err := os.ReadFile(path)
	if err != nil {
		return result, err
	}
	name := filepath.Base(path)
	generated := compiler.GenerateMIPS([]compiler.SourceUnit{{ID: name, Name: name, Content: string(content)}}, compiler.MIPSOptions{GC: gcOptions(mode)})
	if !generated.OK {
		result.Outcome, result.Error = "reference-error", "Cooler could not compile the program"
		return result, nil
	}
	dir, err := os.MkdirTemp("", "cooler-mips-differential-*")
	if err != nil {
		return result, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
		return result, err
	}
	coolerProgram := filepath.Join(dir, "cooler.s")
	if err := os.WriteFile(coolerProgram, []byte(generated.Assembly), 0o600); err != nil {
		return result, err
	}
	run := func(executable string, args []string) DifferentialCommand {
		return executeDifferentialCommand(executable, args, dir, "", config.Timeout, config.OutputLimit)
	}
	spimRun := run(tools.SPIM, tools.Args(coolerProgram))
	if spimRun.ExitCategory == "timeout" || spimRun.ExitCategory == "launch-error" {
		result.Outcome, result.Error = "reference-error", "spim: "+spimRun.ExitCategory
		return result, nil
	}
	result.CoolerOnSPIM = reftools.NormalizeSPIMOutput(spimRun.Stdout)
	result.CoolerCategory = MIPSOutputCategory(result.CoolerOnSPIM)
	simulated := mipssim.Run(context.Background(), generated.Assembly, mipssim.Options{})
	result.Simulator = simulated.Output
	if result.Simulator != result.CoolerOnSPIM {
		result.Differences = append(result.Differences, "embedded simulator output differs from real SPIM")
	}
	if coolc != "" {
		compile := run(coolc, append(strings.Fields(mode), name))
		referenceProgram := filepath.Join(dir, strings.TrimSuffix(name, ".cl")+".s")
		if compile.ExitCode != 0 || !fileExists(referenceProgram) {
			result.Outcome, result.Error = "reference-error", "coolc did not produce an assembly file"
			return result, nil
		}
		referenceRun := run(tools.SPIM, tools.Args(referenceProgram))
		result.ReferenceOnSPIM = reftools.NormalizeSPIMOutput(referenceRun.Stdout)
		result.ReferenceCategory = MIPSOutputCategory(result.ReferenceOnSPIM)
		if result.ReferenceOnSPIM != result.CoolerOnSPIM {
			result.Differences = append(result.Differences, "program output differs from reference coolc on SPIM")
		}
		if result.ReferenceCategory != result.CoolerCategory {
			result.Differences = append(result.Differences, fmt.Sprintf("runtime category %s, reference %s", result.CoolerCategory, result.ReferenceCategory))
		}
	}
	result.Outcome = "match"
	if len(result.Differences) > 0 {
		result.Outcome = "mismatch"
	}
	return result, nil
}

// WriteMIPSDifferentialReport writes the machine-readable report.
func WriteMIPSDifferentialReport(path string, report MIPSDifferentialReport) error {
	return writeIndentedJSON(path, report)
}
