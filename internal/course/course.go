// SPDX-License-Identifier: AGPL-3.0-or-later

// Package course composes the opt-in course-assignment artifacts (PA2–PA5)
// that the GUI and CLI expose beside Cooler's default educational artifacts.
// Everything here runs in-process: no external toolchain is invoked.
package course

import (
	"context"
	"fmt"
	"strings"

	"github.com/ThalesMMS/cooler-compiler/internal/compiler"
	"github.com/ThalesMMS/cooler-compiler/internal/mipssim"
)

// Artifact names accepted in Request.Artifacts.
const (
	ArtifactTokens = "tokens"
	ArtifactAST    = "ast"
	// ArtifactSemant is the typed AST printed by semant (PA4).
	ArtifactSemant = "semant"
	// ArtifactDiagnostics is the course error report of the first halted
	// phase (lexer/parser or semant), empty when the program is accepted.
	ArtifactDiagnostics = "diagnostics"
	// ArtifactMIPS is the SPIM assembly from the optional MIPS backend (PA5).
	ArtifactMIPS = "mips"
	// ArtifactRun runs the generated MIPS on the embedded simulator (PA5).
	ArtifactRun = "run"
)

// Request selects source units and the course artifacts to build.
type Request struct {
	Sources   []compiler.SourceUnit `json:"sources"`
	Input     string                `json:"input,omitempty"`
	Artifacts []string              `json:"artifacts"`
	// Frontend selects the lexer for the reference token stream:
	// "handwritten" (default) or "generated".
	Frontend string `json:"frontend,omitempty"`
	// GC selects the Cool memory manager for generated MIPS (-g, -t, -T).
	GC compiler.MIPSGCOptions `json:"gc,omitempty"`
	// Interactive stops a simulator run that reads past Input with status
	// waiting-input; the client re-runs with the new line appended.
	Interactive bool `json:"interactive,omitempty"`
}

// TextArtifact is one reference-format text view. OK is false when the
// course tool would have halted (the text then holds its error output).
type TextArtifact struct {
	Text     string `json:"text"`
	OK       bool   `json:"ok"`
	ExitCode int    `json:"exitCode"`
}

// Response holds the requested artifacts; absent ones were not requested.
type Response struct {
	Tokens *TextArtifact `json:"tokens,omitempty"`
	AST    *TextArtifact `json:"ast,omitempty"`
	Semant *TextArtifact `json:"semant,omitempty"`
	// Diagnostics.Text is the report; OK is true when no phase halted.
	Diagnostics *TextArtifact        `json:"diagnostics,omitempty"`
	MIPS        *compiler.MIPSResult `json:"mips,omitempty"`
	Run         *RunArtifact         `json:"run,omitempty"`
}

// RunArtifact is a simulator run of the generated MIPS. When generation
// fails, Status is "compile-error" and Diagnostics explain why.
type RunArtifact struct {
	mipssim.Result
	Diagnostics []compiler.Diagnostic `json:"diagnostics,omitempty"`
}

// StatusCompileError marks a run whose program could not be compiled.
const StatusCompileError = "compile-error"

// InputLines splits program input into lines the way the Go VM does: one
// line per read, without the trailing newline.
func InputLines(input string) []string {
	if input == "" {
		return nil
	}
	input = strings.ReplaceAll(input, "\r\n", "\n")
	return strings.Split(strings.TrimSuffix(input, "\n"), "\n")
}

// RunMIPS generates MIPS for the sources and runs it on the simulator.
func RunMIPS(ctx context.Context, sources []compiler.SourceUnit, options compiler.MIPSOptions, input string, interactive bool, limits mipssim.Limits) RunArtifact {
	generated := compiler.GenerateMIPS(sources, options)
	if !generated.OK {
		return RunArtifact{Result: mipssim.Result{Status: StatusCompileError, Error: "the program has compile errors"}, Diagnostics: generated.Diagnostics}
	}
	return RunArtifact{Result: mipssim.Run(ctx, generated.Assembly, mipssim.Options{Input: InputLines(input), Interactive: interactive, Limits: limits})}
}

// MaxSources bounds the number of units in one request.
const MaxSources = 64

// Validate rejects malformed requests before any work happens.
func (r Request) Validate() error {
	if len(r.Sources) == 0 {
		return fmt.Errorf("course request needs at least one source unit")
	}
	if len(r.Sources) > MaxSources {
		return fmt.Errorf("course request accepts at most %d source units", MaxSources)
	}
	if _, err := compiler.NormalizeFrontend(r.Frontend); err != nil {
		return fmt.Errorf("frontend must be handwritten or generated")
	}
	for _, artifact := range r.Artifacts {
		switch artifact {
		case ArtifactTokens, ArtifactAST, ArtifactSemant, ArtifactDiagnostics, ArtifactMIPS, ArtifactRun:
		default:
			return fmt.Errorf("unknown course artifact %q", artifact)
		}
	}
	return nil
}

// Build computes the requested artifacts.
func Build(r Request) (Response, error) {
	return BuildContext(context.Background(), r)
}

// BuildContext is Build with cancellation for simulator runs.
func BuildContext(ctx context.Context, r Request) (Response, error) {
	if err := r.Validate(); err != nil {
		return Response{}, err
	}
	var response Response
	frontend, _ := compiler.NormalizeFrontend(r.Frontend)
	pipeline := compiler.ReferencePipeline{Frontend: frontend}
	for _, artifact := range r.Artifacts {
		switch artifact {
		case ArtifactTokens:
			response.Tokens = &TextArtifact{Text: pipeline.Tokens(r.Sources), OK: true}
		case ArtifactAST:
			response.AST = textArtifact(pipeline.Parser(r.Sources))
		case ArtifactSemant:
			response.Semant = textArtifact(pipeline.SemantDump(r.Sources))
		case ArtifactDiagnostics:
			result := pipeline.Semant(r.Sources)
			response.Diagnostics = textArtifact(result.Report, result.Stage == "")
		case ArtifactMIPS:
			result := compiler.GenerateMIPS(r.Sources, compiler.MIPSOptions{Frontend: frontend, GC: r.GC})
			response.MIPS = &result
		case ArtifactRun:
			run := RunMIPS(ctx, r.Sources, compiler.MIPSOptions{Frontend: frontend, GC: r.GC}, r.Input, r.Interactive, mipssim.Limits{})
			response.Run = &run
		}
	}
	return response, nil
}

func textArtifact(text string, ok bool) *TextArtifact {
	artifact := &TextArtifact{Text: text, OK: ok}
	if !ok {
		artifact.ExitCode = 1
	}
	return artifact
}
