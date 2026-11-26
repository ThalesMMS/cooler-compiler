// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"fmt"
	"io"

	"github.com/ThalesMMS/cooler-compiler/internal/compiler"
	"github.com/ThalesMMS/cooler-compiler/internal/course"
	"github.com/ThalesMMS/cooler-compiler/internal/mipssim"
)

// Course-assignment modes are opt-in secondary/test surfaces. The GUI exposes
// the same artifacts; see docs/course-assignments.md.

func courseModeRequested(options cliOptions) bool {
	return options.format == "reference" || options.diagnosticFormat == "reference" || options.target == "mips"
}

func validateCourseOptions(options cliOptions) error {
	if options.format != "" && options.format != "cooler" && options.format != "reference" {
		return usageError{"--format must be cooler or reference"}
	}
	if options.stage != "" && options.stage != "parser" && options.stage != "semant" {
		return usageError{"--stage must be parser or semant"}
	}
	if options.target != "" && options.target != "vm" && options.target != "mips" {
		return usageError{"--target must be vm or mips"}
	}
	if (options.gc.Generational || options.gc.Test || options.gc.Debug) && options.target != "mips" {
		return usageError{"-g, -t, and -T select the MIPS collector and require --target=mips"}
	}
	if options.stage == "semant" && options.format != "reference" {
		return usageError{"--stage=semant requires --format=reference"}
	}
	if options.diagnosticFormat == "reference" && options.command != "check" && !(options.command == "emit" && options.emit == "ast" && options.stage == "semant") {
		return usageError{"--diagnostic-format=reference is available with check and 'emit ast --format=reference --stage=semant'"}
	}
	return nil
}

// courseUnits names units the way the course tools do: by the path given on
// the command line, or <stdin> when the source came from standard input.
func courseUnits(name, source string, files []sourceFile) []compiler.SourceUnit {
	if len(files) == 0 {
		return []compiler.SourceUnit{{ID: "source-1", Name: name, Content: source}}
	}
	units := make([]compiler.SourceUnit, len(files))
	for index, file := range files {
		units[index] = compiler.SourceUnit{ID: file.id, Name: file.name, Content: file.source}
	}
	return units
}

func runCourseMode(options cliOptions, name, source string, files []sourceFile, programInput string, stdout, stderr io.Writer) int {
	units := courseUnits(name, source, files)
	frontend, _ := compiler.NormalizeFrontend(options.frontend)
	pipeline := compiler.ReferencePipeline{Frontend: frontend}
	switch {
	case options.command == "emit" && options.emit == "tokens":
		// The course lexer prints error tokens and exits successfully; the
		// parser is the phase that halts.
		if _, err := io.WriteString(stdout, pipeline.Tokens(units)); err != nil {
			return exitTool
		}
		return exitOK
	case options.command == "emit" && options.emit == "ast" && options.stage != "semant":
		text, ok := pipeline.Parser(units)
		return writeCourseResult(stdout, stderr, text, ok)
	case options.command == "emit" && options.emit == "ast":
		text, ok := pipeline.SemantDump(units)
		return writeCourseResult(stdout, stderr, text, ok)
	case options.target == "mips" && options.command == "run":
		// The embedded simulator runs the generated MIPS with the Cool
		// runtime contract; no host spim is involved.
		run := course.RunMIPS(context.Background(), units, compiler.MIPSOptions{Frontend: frontend, GC: options.gc}, programInput, false, mipssim.Limits{MaxSteps: options.maxSteps * 100})
		if run.Status == course.StatusCompileError {
			if err := writeDiagnostics(stderr, name, files, run.Diagnostics, options.diagnosticFormat); err != nil {
				return exitTool
			}
			return exitError
		}
		if _, err := io.WriteString(stdout, run.Output); err != nil {
			return exitTool
		}
		if run.Status != mipssim.StatusHalted {
			if _, err := fmt.Fprintf(stderr, "coolerc: MIPS simulator: %s: %s\n", run.Status, run.Error); err != nil {
				return exitTool
			}
			return exitError
		}
		return exitOK
	case options.target == "mips" && options.command == "emit":
		result := compiler.GenerateMIPS(units, compiler.MIPSOptions{Frontend: frontend, GC: options.gc})
		if !result.OK {
			if err := writeDiagnostics(stderr, name, files, result.Diagnostics, options.diagnosticFormat); err != nil {
				return exitTool
			}
			return exitError
		}
		if _, err := io.WriteString(stdout, result.Assembly); err != nil {
			return exitTool
		}
		return exitOK
	case options.command == "check":
		// Reference diagnostics: the halted course phase's report, or no
		// output when lexer, parser, and semant all accept the program.
		result := pipeline.Semant(units)
		if result.Stage == "" {
			return exitOK
		}
		return writeCourseResult(stdout, stderr, result.Report, false)
	}
	if _, err := fmt.Fprintln(stderr, "coolerc: --format=reference is not available for this command"); err != nil {
		return exitTool
	}
	return exitUsage
}

// writeCourseResult follows the course tools' streams: a successful dump goes
// to stdout; a halted phase writes its report to stderr and exits 1.
func writeCourseResult(stdout, stderr io.Writer, text string, ok bool) int {
	target := stdout
	if !ok {
		target = stderr
	}
	if _, err := io.WriteString(target, text); err != nil {
		return exitTool
	}
	if !ok {
		return exitError
	}
	return exitOK
}
