// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	api "github.com/ThalesMMS/cooler-compiler/compiler"
)

type programManifest struct {
	SchemaVersion string            `json:"schemaVersion"`
	Cases         []programTestCase `json:"cases"`
}

type manifestReadError struct{ cause error }

func (e manifestReadError) Error() string { return e.cause.Error() }
func (e manifestReadError) Unwrap() error { return e.cause }

type programTestCase struct {
	Name    string   `json:"name"`
	Sources []string `json:"sources"`
	Input   string   `json:"input"`
	Expect  struct {
		Outcome     string                      `json:"outcome"`
		Output      *string                     `json:"output"`
		Diagnostics []expectedProgramDiagnostic `json:"diagnostics,omitempty"`
	} `json:"expect"`
	Limits struct {
		MaxSteps             int `json:"maxSteps,omitempty"`
		TimeoutMS            int `json:"timeoutMs,omitempty"`
		MaxOutputBytes       int `json:"maxOutputBytes,omitempty"`
		MaxObjectAllocations int `json:"maxObjectAllocations,omitempty"`
	} `json:"limits,omitempty"`
}

type expectedProgramDiagnostic struct {
	Code  string `json:"code"`
	Stage string `json:"stage"`
}

type programTestResult struct {
	Name                string                      `json:"name"`
	Passed              bool                        `json:"passed"`
	Outcome             string                      `json:"outcome"`
	Output              string                      `json:"output"`
	ExpectedOutcome     string                      `json:"expectedOutcome"`
	ExpectedOutput      string                      `json:"expectedOutput"`
	Diagnostics         []expectedProgramDiagnostic `json:"diagnostics"`
	ExpectedDiagnostics []expectedProgramDiagnostic `json:"expectedDiagnostics"`
	ExecutedSteps       int                         `json:"executedSteps"`
	Failures            []string                    `json:"failures"`
}

type programTestReport struct {
	SchemaVersion   string              `json:"schemaVersion"`
	CompilerVersion string              `json:"compilerVersion"`
	Passed          int                 `json:"passed"`
	Failed          int                 `json:"failed"`
	Cases           []programTestResult `json:"cases"`
}

func parseProgramManifest(reader io.Reader) (programManifest, error) {
	var manifest programManifest
	data, err := io.ReadAll(io.LimitReader(reader, (1<<20)+1))
	if err != nil {
		return manifest, manifestReadError{cause: err}
	}
	if len(data) > 1<<20 {
		return manifest, errors.New("manifest exceeds 1 MiB")
	}
	if !utf8.Valid(data) {
		return manifest, errors.New("manifest must be valid UTF-8")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return manifest, errors.New("manifest must contain exactly one JSON value")
	}
	if manifest.SchemaVersion != "1.0.0" {
		return manifest, errors.New("schemaVersion must be 1.0.0")
	}
	if len(manifest.Cases) < 1 || len(manifest.Cases) > 100 {
		return manifest, errors.New("manifest requires 1 through 100 cases")
	}
	names := map[string]bool{}
	codePattern := regexp.MustCompile(`^COL[0-9]{4}$`)
	for i := range manifest.Cases {
		item := &manifest.Cases[i]
		if strings.TrimSpace(item.Name) == "" || len(item.Name) > 128 || names[item.Name] {
			return manifest, fmt.Errorf("case %d: names must be unique, nonempty and at most 128 bytes", i+1)
		}
		names[item.Name] = true
		if len(item.Sources) < 1 || len(item.Sources) > 32 {
			return manifest, fmt.Errorf("case %q: requires 1 through 32 ordered sources", item.Name)
		}
		seen := map[string]bool{}
		for _, source := range item.Sources {
			clean := filepath.Clean(filepath.FromSlash(source))
			if !filepath.IsLocal(clean) || strings.Contains(source, "\\") || len(source) > 512 || filepath.Ext(source) != ".cl" || seen[clean] {
				return manifest, fmt.Errorf("case %q: invalid or duplicate relative .cl source %q", item.Name, source)
			}
			seen[clean] = true
		}
		if len(item.Input) > 64<<10 || item.Expect.Output == nil || len(*item.Expect.Output) > 64<<10 {
			return manifest, fmt.Errorf("case %q: input/output must fit 64 KiB; expect.output is required", item.Name)
		}
		switch item.Expect.Outcome {
		case "success", "input-required", "step-limit":
			if len(item.Expect.Diagnostics) != 0 {
				return manifest, fmt.Errorf("case %q: error diagnostics require outcome error", item.Name)
			}
		case "error":
			if len(item.Expect.Diagnostics) == 0 || len(item.Expect.Diagnostics) > 100 {
				return manifest, fmt.Errorf("case %q: error requires 1 through 100 expected diagnostics", item.Name)
			}
		default:
			return manifest, fmt.Errorf("case %q: unsupported expected outcome", item.Name)
		}
		for _, diagnostic := range item.Expect.Diagnostics {
			if !codePattern.MatchString(diagnostic.Code) {
				return manifest, fmt.Errorf("case %q: invalid diagnostic code", item.Name)
			}
			switch diagnostic.Stage {
			case "input", "lexer", "parser", "semantic", "ir", "assembly", "runtime":
			default:
				return manifest, fmt.Errorf("case %q: invalid diagnostic stage", item.Name)
			}
		}
		for _, bound := range []struct {
			name          string
			value         *int
			fallback, max int
		}{
			{"maxSteps", &item.Limits.MaxSteps, 1000, 100000},
			{"timeoutMs", &item.Limits.TimeoutMS, 2000, 5000},
			{"maxOutputBytes", &item.Limits.MaxOutputBytes, 64 << 10, 64 << 10},
			{"maxObjectAllocations", &item.Limits.MaxObjectAllocations, 10000, 10000},
		} {
			if *bound.value < 0 || *bound.value > bound.max {
				return manifest, fmt.Errorf("case %q: %s must be 0 through %d", item.Name, bound.name, bound.max)
			}
			if *bound.value == 0 {
				*bound.value = bound.fallback
			}
		}
	}
	return manifest, nil
}

func runProgramTests(parent context.Context, args []string, stdout, stderr io.Writer) int {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	flags := flag.NewFlagSet("coolerc test", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	structured := flags.Bool("json", false, "write a structured test result")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			if _, err := fmt.Fprintln(stdout, "usage: coolerc test [--json] exercise.json"); err != nil {
				return exitTool
			}
			return exitOK
		}
		if _, err := fmt.Fprintln(stderr, "coolerc test:", err); err != nil {
			return exitTool
		}
		return exitUsage
	}
	if flags.NArg() != 1 {
		if _, err := fmt.Fprintln(stderr, "usage: coolerc test [--json] exercise.json"); err != nil {
			return exitTool
		}
		return exitUsage
	}
	if err := ctx.Err(); err != nil {
		return writeProgramTestError(stderr, err, exitTool)
	}
	manifestPath, err := filepath.Abs(flags.Arg(0))
	if err != nil {
		return writeProgramTestError(stderr, err, exitTool)
	}
	info, err := os.Stat(manifestPath)
	if err != nil {
		return writeProgramTestError(stderr, err, exitTool)
	}
	if !info.Mode().IsRegular() {
		return writeProgramTestError(stderr, errors.New("manifest must be a regular file"), exitTool)
	}
	file, err := os.Open(manifestPath)
	if err != nil {
		return writeProgramTestError(stderr, err, exitTool)
	}
	manifest, parseErr := parseProgramManifest(file)
	closeErr := file.Close()
	if parseErr != nil {
		var readFailure manifestReadError
		if errors.As(parseErr, &readFailure) {
			return writeProgramTestError(stderr, parseErr, exitTool)
		}
		return writeProgramTestError(stderr, parseErr, exitUsage)
	}
	if closeErr != nil {
		return writeProgramTestError(stderr, closeErr, exitTool)
	}
	root, err := filepath.EvalSymlinks(filepath.Dir(manifestPath))
	if err != nil {
		return writeProgramTestError(stderr, err, exitTool)
	}
	report := programTestReport{SchemaVersion: "1.0.0", CompilerVersion: version, Cases: []programTestResult{}}
	code := exitOK
	for _, item := range manifest.Cases {
		result := executeProgramCase(ctx, root, item)
		report.Cases = append(report.Cases, result)
		if result.Passed {
			report.Passed++
		} else {
			report.Failed++
			if code == exitOK {
				code = exitError
			}
		}
		if result.Outcome == "tool-error" {
			code = exitTool
		}
	}
	if *structured {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			return writeProgramTestError(stderr, err, exitTool)
		}
	} else {
		for _, result := range report.Cases {
			status := "PASS"
			if !result.Passed {
				status = "FAIL"
			}
			if _, err := fmt.Fprintf(stdout, "%s %q (%s)\n", status, result.Name, result.Outcome); err != nil {
				return exitTool
			}
			for _, failure := range result.Failures {
				if _, err := fmt.Fprintf(stdout, "  %s\n", failure); err != nil {
					return exitTool
				}
			}
		}
		if _, err := fmt.Fprintf(stdout, "%d passed, %d failed\n", report.Passed, report.Failed); err != nil {
			return exitTool
		}
	}
	return code
}

func writeProgramTestError(writer io.Writer, err error, code int) int {
	if _, writeErr := fmt.Fprintln(writer, "coolerc test:", err); writeErr != nil {
		return exitTool
	}
	return code
}

func loadProgramCase(ctx context.Context, root string, item programTestCase) (api.Project, error) {
	project := api.Project{Sources: []api.SourceUnit{}}
	total := 0
	for _, source := range item.Sources {
		if err := ctx.Err(); err != nil {
			return project, err
		}
		path, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(source)))
		if err != nil {
			return project, err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil || !filepath.IsLocal(relative) {
			return project, fmt.Errorf("source %q escapes manifest directory", source)
		}
		info, err := os.Stat(path)
		if err != nil {
			return project, err
		}
		if !info.Mode().IsRegular() {
			return project, fmt.Errorf("source %q is not a regular file", source)
		}
		file, err := os.Open(path)
		if err != nil {
			return project, err
		}
		data, readErr := io.ReadAll(io.LimitReader(file, int64((2<<20)-total+1)))
		closeErr := file.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return project, err
		}
		total += len(data)
		if total > 2<<20 {
			return project, errors.New("case sources exceed 2 MiB")
		}
		project.Sources = append(project.Sources, api.SourceUnit{ID: source, Name: source, Content: string(data)})
	}
	return project, nil
}

func executeProgramCase(parent context.Context, root string, item programTestCase) programTestResult {
	result := programTestResult{Name: item.Name, ExpectedOutcome: item.Expect.Outcome, ExpectedOutput: *item.Expect.Output, ExpectedDiagnostics: append([]expectedProgramDiagnostic{}, item.Expect.Diagnostics...), Diagnostics: []expectedProgramDiagnostic{}, Failures: []string{}}
	ctx, cancel := context.WithTimeout(parent, time.Duration(item.Limits.TimeoutMS)*time.Millisecond)
	defer cancel()
	compiled, outcome, err := runProgramCase(ctx, root, item)
	result.Outcome = outcome
	if err != nil {
		result.Outcome = "tool-error"
		result.Failures = append(result.Failures, err.Error())
		return result
	}
	runtime := compiled.Runtime()
	result.Output = strings.Join(runtime.Output, "")
	result.ExecutedSteps = runtime.ExecutedSteps
	for _, diagnostic := range compiled.Diagnostics() {
		if diagnostic.Severity == api.SeverityError {
			result.Diagnostics = append(result.Diagnostics, expectedProgramDiagnostic{Code: diagnostic.Code, Stage: diagnostic.Stage})
		}
	}
	if result.Outcome != result.ExpectedOutcome {
		result.Failures = append(result.Failures, fmt.Sprintf("expected outcome %s, got %s", result.ExpectedOutcome, result.Outcome))
	}
	if result.Output != result.ExpectedOutput {
		offset := 0
		for offset < len(result.Output) && offset < len(result.ExpectedOutput) && result.Output[offset] == result.ExpectedOutput[offset] {
			offset++
		}
		result.Failures = append(result.Failures, fmt.Sprintf("output differs at UTF-8 byte %d: expected %q, got %q", offset, result.ExpectedOutput, result.Output))
	}
	if !reflect.DeepEqual(result.Diagnostics, result.ExpectedDiagnostics) {
		result.Failures = append(result.Failures, fmt.Sprintf("expected error diagnostics %v, got %v", result.ExpectedDiagnostics, result.Diagnostics))
	}
	result.Passed = len(result.Failures) == 0
	return result
}

func runProgramCase(ctx context.Context, root string, item programTestCase) (result api.Result, outcome string, err error) {
	project, err := loadProgramCase(ctx, root, item)
	if err != nil {
		return result, "", err
	}
	options := api.DefaultOptions()
	options.MaxSteps = item.Limits.MaxSteps
	options.CompilerVersion = version
	options.CompilerCommit = commit
	options.Limits.MaxOutputBytes = item.Limits.MaxOutputBytes
	options.Limits.MaxObjectAllocations = item.Limits.MaxObjectAllocations
	options.Limits.MaxInputBytes = 64 << 10
	options.Limits.MaxTraceEntries = 128
	options.Limits.MaxTraceBytes = 1 << 20
	session, err := api.NewSession(ctx, project, options)
	if err != nil {
		return result, "", err
	}
	defer func() { err = errors.Join(err, session.Close()) }()
	snapshot, err := session.Complete(ctx)
	if err != nil {
		return result, "", err
	}
	if snapshot.Blocked {
		return snapshot.Result, "error", nil
	}
	debugger, err := session.Debugger(ctx)
	if err != nil {
		return result, "", err
	}
	defer func() { err = errors.Join(err, debugger.Close()) }()
	if err := debugger.SetInput(ctx, item.Input); err != nil {
		return result, "", err
	}
	if err := debugger.SetInteractiveInput(ctx, true); err != nil {
		return result, "", err
	}
	stopped, err := debugger.Run(ctx, item.Limits.MaxSteps, nil)
	if err != nil {
		return stopped.Result, "", err
	}
	if stopped.Result.HasErrors() {
		return stopped.Result, "error", nil
	}
	switch stopped.StopReason {
	case api.StopHalt:
		return stopped.Result, "success", nil
	case api.StopWaitingInput:
		return stopped.Result, "input-required", nil
	case api.StopInstructionLimit:
		return stopped.Result, "step-limit", nil
	default:
		return stopped.Result, "", fmt.Errorf("unexpected VM stop %q", stopped.StopReason)
	}
}
