// SPDX-License-Identifier: AGPL-3.0-or-later

package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ThalesMMS/cooler-compiler/internal/compiler"
)

func runDifferentialFixture(config DifferentialConfig, fixture Fixture, coolcPath, runnerPath string) DifferentialFixtureResult {
	result := DifferentialFixtureResult{
		FixtureID: fixture.ID,
		RuleIDs:   append([]string(nil), fixture.RuleIDs...),
		Sources:   append([]string(nil), fixture.Sources...),
	}
	result.Cooler = observeCoolerFixture(fixture)
	result.Reference = observeReferenceFixture(config, fixture, coolcPath, runnerPath)
	result.Differences, result.RuntimeCompared = compareDifferentialObservations(result.Cooler, result.Reference)
	return result
}

func observeCoolerFixture(fixture Fixture) DifferentialObservation {
	observation := DifferentialObservation{CompileAttempted: true, ArtifactGenerated: false, Stdout: "", Stderr: ""}
	sourceUnits, err := readSources(fixture)
	if err != nil {
		observation.InfrastructureError = err.Error()
		return observation
	}
	input := fixtureInput(fixture)
	result, _ := compileProjectThrough("runtime", sourceUnits, input, compiler.ResultContract{
		Options: compiler.CompilationOptions{Execute: true, InputProvided: input != ""},
	})
	observation.CompileAccepted = true
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Severity != compiler.SeverityError || diagnostic.Stage == "runtime" {
			continue
		}
		observation.CompileAccepted = false
		if observation.FirstFailingStage == "" || stageRank(diagnostic.Stage) < stageRank(observation.FirstFailingStage) {
			observation.FirstFailingStage = normalizeCompilerStage(diagnostic.Stage)
		}
	}
	observation.DiagnosticCategory = coolerDiagnosticCategory(result.Diagnostics, observation.CompileAccepted)
	observation.ArtifactGenerated = observation.CompileAccepted && len(result.IR) > 0
	if !observation.CompileAccepted {
		observation.Stderr = normalizedSemanticDiagnostic(observation.FirstFailingStage, observation.DiagnosticCategory)
		return observation
	}

	observation.RuntimeAttempted = true
	observation.Stdout = normalizeProgramOutput(strings.Join(result.Runtime.Output, ""), "")
	observation.RuntimeExit = "success"
	if result.Runtime.Paused {
		observation.RuntimeExit = "paused"
	}
	for _, diagnostic := range result.Runtime.Diagnostics {
		if diagnostic.Severity == compiler.SeverityError {
			observation.RuntimeExit = "runtime-error"
			observation.RuntimeError = classifyCoolerRuntimeError(diagnostic.Code)
			observation.Stderr = normalizedSemanticDiagnostic("runtime", observation.RuntimeError)
			break
		}
	}
	return observation
}

func observeReferenceFixture(config DifferentialConfig, fixture Fixture, coolcPath, runnerPath string) DifferentialObservation {
	observation := DifferentialObservation{CompileAttempted: true, Stdout: "", Stderr: ""}
	workDir, err := os.MkdirTemp("", "cooler-differential-"+fixture.ID+"-*")
	if err != nil {
		observation.InfrastructureError = "create temporary directory: " + err.Error()
		return observation
	}
	if !config.KeepTemporary {
		defer func() { _ = os.RemoveAll(workDir) }()
	} else {
		observation.TemporaryDirectory = workDir
	}

	sourceArgs, err := copyDifferentialSources(fixture, workDir)
	if err != nil {
		observation.InfrastructureError = err.Error()
		return observation
	}
	compileArgs := append(append([]string(nil), config.CoolcArgs...), sourceArgs...)
	compileResult := executeDifferentialCommand(coolcPath, compileArgs, workDir, "", config.Timeout, config.OutputLimit)
	compileOutput := compileResult.Stdout + "\n" + compileResult.Stderr
	normalizeDifferentialCommand(&compileResult, workDir, false)
	observation.CompileCommand = &compileResult
	observation.Stderr = normalizeDiagnosticOutput(compileResult.Stderr, workDir)
	if compileResult.ExitCategory != "success" {
		observation.FirstFailingStage, observation.DiagnosticCategory = classifyReferenceCompileFailure(compileOutput)
		if isInfrastructureExit(compileResult.ExitCategory) {
			observation.InfrastructureError = "reference compiler " + compileResult.ExitCategory
		}
		return observation
	}

	artifacts, err := findReferenceArtifacts(workDir)
	if err != nil {
		observation.InfrastructureError = err.Error()
		return observation
	}
	if len(artifacts) != 1 {
		observation.InfrastructureError = fmt.Sprintf("reference compiler malformed output: produced %d assembly artifacts, want 1", len(artifacts))
		observation.CompileCommand.ExitCategory = "malformed-output"
		return observation
	}
	observation.CompileAccepted = true
	observation.ArtifactGenerated = true
	if runnerPath == "" {
		return observation
	}

	runtimeArgs := append(append([]string(nil), config.RunnerArgs...), artifacts[0])
	runtimeResult := executeDifferentialCommand(runnerPath, runtimeArgs, workDir, fixtureInput(fixture), config.Timeout, config.OutputLimit)
	runtimeOutput := runtimeResult.Stdout + "\n" + runtimeResult.Stderr
	normalizeDifferentialCommand(&runtimeResult, workDir, true)
	observation.RuntimeCommand = &runtimeResult
	observation.RuntimeAttempted = true
	observation.Stdout = runtimeResult.Stdout
	runtimeCategory := classifyReferenceRuntimeError(runtimeOutput)
	observation.RuntimeError = runtimeCategory
	observation.RuntimeExit = runtimeResult.ExitCategory
	if runtimeCategory != "" {
		observation.RuntimeExit = "runtime-error"
		observation.Stdout = stripReferenceRuntimeDiagnostic(observation.Stdout, runtimeCategory)
		observation.Stderr = normalizedSemanticDiagnostic("runtime", runtimeCategory)
	} else {
		observation.Stderr = normalizeDiagnosticOutput(runtimeResult.Stderr, workDir)
	}
	if isInfrastructureExit(runtimeResult.ExitCategory) {
		observation.InfrastructureError = "reference runtime " + runtimeResult.ExitCategory
	}
	return observation
}

func fixtureInput(fixture Fixture) string {
	if len(fixture.InputLines) == 0 {
		return ""
	}
	return strings.Join(fixture.InputLines, "\n") + "\n"
}

func copyDifferentialSources(fixture Fixture, workDir string) ([]string, error) {
	base := filepath.Dir(fixture.metadataPath)
	args := make([]string, 0, len(fixture.Sources))
	seen := map[string]bool{}
	for _, sourceName := range fixture.Sources {
		clean := filepath.Clean(sourceName)
		if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("unsafe differential source %q", sourceName)
		}
		if seen[clean] {
			return nil, fmt.Errorf("duplicate differential source %q", sourceName)
		}
		seen[clean] = true
		data, err := os.ReadFile(filepath.Join(base, clean))
		if err != nil {
			return nil, fmt.Errorf("read differential source %s: %w", sourceName, err)
		}
		destination := filepath.Join(workDir, clean)
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return nil, fmt.Errorf("create differential source directory: %w", err)
		}
		if err := os.WriteFile(destination, data, 0o600); err != nil {
			return nil, fmt.Errorf("write differential source %s: %w", sourceName, err)
		}
		args = append(args, clean)
	}
	return args, nil
}

func findReferenceArtifacts(workDir string) ([]string, error) {
	artifacts := []string{}
	err := filepath.WalkDir(workDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".s") {
			artifacts = append(artifacts, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan reference artifacts: %w", err)
	}
	sort.Strings(artifacts)
	return artifacts, nil
}

func compareDifferentialObservations(cooler, reference DifferentialObservation) ([]string, bool) {
	differences := []string{}
	if cooler.InfrastructureError != "" {
		return []string{"Cooler infrastructure: " + cooler.InfrastructureError}, false
	}
	if reference.InfrastructureError != "" {
		return []string{"reference infrastructure: " + reference.InfrastructureError}, false
	}
	if cooler.CompileAccepted != reference.CompileAccepted {
		differences = append(differences, fmt.Sprintf("compile acceptance Cooler=%t reference=%t", cooler.CompileAccepted, reference.CompileAccepted))
		return differences, false
	}
	if !cooler.CompileAccepted {
		if reference.FirstFailingStage != "" && reference.FirstFailingStage != "compile" && cooler.FirstFailingStage != reference.FirstFailingStage {
			differences = append(differences, fmt.Sprintf("first failing stage Cooler=%s reference=%s", cooler.FirstFailingStage, reference.FirstFailingStage))
		}
		if reference.DiagnosticCategory != "" && reference.DiagnosticCategory != "compile-rejected" && cooler.DiagnosticCategory != reference.DiagnosticCategory {
			differences = append(differences, fmt.Sprintf("diagnostic category Cooler=%s reference=%s", cooler.DiagnosticCategory, reference.DiagnosticCategory))
		}
		return differences, false
	}
	if !reference.ArtifactGenerated {
		differences = append(differences, "reference compiler did not produce an assembly artifact")
	}
	if !reference.RuntimeAttempted {
		return differences, false
	}
	runtimeCompared := true
	if cooler.Stdout != reference.Stdout {
		differences = append(differences, fmt.Sprintf("runtime stdout Cooler=%q reference=%q", cooler.Stdout, reference.Stdout))
	}
	if cooler.RuntimeExit != reference.RuntimeExit {
		differences = append(differences, fmt.Sprintf("runtime exit Cooler=%s reference=%s", cooler.RuntimeExit, reference.RuntimeExit))
	}
	if cooler.RuntimeError != reference.RuntimeError && (cooler.RuntimeError != "" || reference.RuntimeError != "") {
		differences = append(differences, fmt.Sprintf("runtime error Cooler=%s reference=%s", cooler.RuntimeError, reference.RuntimeError))
	}
	if cooler.Stderr != reference.Stderr {
		differences = append(differences, fmt.Sprintf("semantic stderr Cooler=%q reference=%q", cooler.Stderr, reference.Stderr))
	}
	return differences, runtimeCompared
}

func applyDifferentialOutcome(result *DifferentialFixtureResult, allowlist DifferentialAllowlist) {
	var matching *DifferentialAllowlistEntry
	for index := range allowlist.Entries {
		entry := &allowlist.Entries[index]
		if entry.FixtureID == result.FixtureID && containsString(result.RuleIDs, entry.RuleID) {
			matching = entry
			break
		}
	}
	if result.Reference.InfrastructureError != "" || result.Cooler.InfrastructureError != "" {
		if matching != nil {
			result.Outcome = "expected-divergence"
			result.AllowlistID = matching.ID
			return
		}
		result.Outcome = "reference-error"
		return
	}
	if len(result.Differences) == 0 {
		if matching != nil {
			result.Outcome = "unexpected-pass"
			result.AllowlistID = matching.ID
			return
		}
		result.Outcome = "matched"
		return
	}
	if matching != nil {
		result.Outcome = "expected-divergence"
		result.AllowlistID = matching.ID
		return
	}
	result.Outcome = "unexpected-mismatch"
}

func accumulateDifferentialSummary(summary *DifferentialSummary, result DifferentialFixtureResult) {
	if !result.RuntimeCompared && result.Cooler.CompileAccepted && result.Reference.CompileAccepted {
		summary.RuntimeNotCompared++
	}
	switch result.Outcome {
	case "matched":
		summary.Matched++
	case "expected-divergence":
		summary.ExpectedDivergences++
	case "unexpected-mismatch":
		summary.UnexpectedMismatches++
	case "unexpected-pass":
		summary.UnexpectedPasses++
	case "reference-error":
		summary.ReferenceErrors++
	case "skipped":
		summary.Skipped++
	}
}

func (report DifferentialReport) HasFailures() bool {
	return report.Summary.UnexpectedMismatches > 0 || report.Summary.UnexpectedPasses > 0 || report.Summary.ReferenceErrors > 0
}

func WriteDifferentialReport(path string, report DifferentialReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal differential report: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create differential report directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write differential report: %w", err)
	}
	return nil
}

func writeDifferentialEvidence(config DifferentialConfig, fixture Fixture, result DifferentialFixtureResult, host DifferentialHost, tools DifferentialTools) (string, error) {
	directory := filepath.Join(config.EvidenceDir, fixture.ID)
	if err := os.MkdirAll(filepath.Join(directory, "sources"), 0o755); err != nil {
		return "", fmt.Errorf("create evidence directory: %w", err)
	}
	base := filepath.Dir(fixture.metadataPath)
	for _, sourceName := range fixture.Sources {
		data, err := os.ReadFile(filepath.Join(base, sourceName))
		if err != nil {
			return "", fmt.Errorf("read evidence source: %w", err)
		}
		destination := filepath.Join(directory, "sources", filepath.Clean(sourceName))
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(destination, data, 0o644); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "input.txt"), []byte(fixtureInput(fixture)), 0o644); err != nil {
		return "", err
	}
	bundle := struct {
		SchemaVersion string                    `json:"schemaVersion"`
		Host          DifferentialHost          `json:"host"`
		Tools         DifferentialTools         `json:"tools"`
		Result        DifferentialFixtureResult `json:"result"`
	}{SchemaVersion: differentialSchemaVersion, Host: host, Tools: tools, Result: result}
	data, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(directory, "evidence.json"), append(data, '\n'), 0o644); err != nil {
		return "", err
	}
	return directory, nil
}

func isInfrastructureExit(category string) bool {
	switch category {
	case "timeout", "output-limit", "launch-error", "crash", "malformed-output":
		return true
	default:
		return false
	}
}

func stageRank(stage string) int {
	switch normalizeCompilerStage(stage) {
	case "lexer":
		return 1
	case "parser":
		return 2
	case "semantic":
		return 3
	case "codegen":
		return 4
	case "runtime":
		return 5
	default:
		return 99
	}
}

func normalizeCompilerStage(stage string) string {
	switch stage {
	case "lexer", "parser", "semantic", "runtime":
		return stage
	case "ir", "codegen":
		return "codegen"
	default:
		return stage
	}
}

func coolerDiagnosticCategory(diagnostics []compiler.Diagnostic, compileAccepted bool) string {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity != compiler.SeverityError {
			continue
		}
		if !compileAccepted && diagnostic.Stage != "runtime" {
			return normalizeCompilerStage(diagnostic.Stage)
		}
		if compileAccepted && diagnostic.Stage == "runtime" {
			return classifyCoolerRuntimeError(diagnostic.Code)
		}
	}
	return ""
}

func classifyCoolerRuntimeError(code string) string {
	switch code {
	case "COL6015":
		return "division-by-zero"
	case "COL6020":
		return "dispatch-on-void"
	case "COL6030":
		return "case-on-void"
	case "COL6031":
		return "case-no-match"
	case "COL6040":
		return "abort"
	case "COL6046":
		return "substring-range"
	default:
		if strings.HasPrefix(code, "COL60") {
			return "runtime-error"
		}
		return ""
	}
}

func classifyReferenceCompileFailure(output string) (string, string) {
	lower := strings.ToLower(output)
	switch {
	case containsAny(lower, "lexical error", "invalid character", "eof in string", "eof in comment", "unterminated string", "at or near error =") || referenceIntConstantOutOfRange(lower):
		return "lexer", "lexer"
	case containsAny(lower, "syntax error", "parse error"):
		return "parser", "parser"
	case containsAny(lower, "semantic error", "static semantic", "inheritance", "undefined method", "undeclared identifier", "does not conform"):
		return "semantic", "semantic"
	default:
		return "compile", "compile-rejected"
	}
}

func referenceIntConstantOutOfRange(output string) bool {
	const marker = "int_const = "
	for search := output; ; {
		index := strings.Index(search, marker)
		if index < 0 {
			return false
		}
		digits := search[index+len(marker):]
		end := 0
		for end < len(digits) && digits[end] >= '0' && digits[end] <= '9' {
			end++
		}
		digits = strings.TrimLeft(digits[:end], "0")
		if len(digits) > 10 || len(digits) == 10 && digits > "2147483647" {
			return true
		}
		search = search[index+len(marker):]
	}
}

func classifyReferenceRuntimeError(output string) string {
	lower := strings.ToLower(output)
	switch {
	case containsAny(lower, "division by zero", "divide by zero", "division by 0", "divide by 0"):
		return "division-by-zero"
	case containsAny(lower, "arithmetic overflow"):
		return "arithmetic-overflow"
	case containsAny(lower, "dispatch on void", "dispatch to void"):
		return "dispatch-on-void"
	case containsAny(lower, "case on void", "match on void in case"):
		return "case-on-void"
	case containsAny(lower, "no match in case", "case without matching branch"):
		return "case-no-match"
	case containsAny(lower, "abort called"):
		return "abort"
	case containsAny(lower, "substring out of range", "substr out of range", "length to substr too long", "substr too long"):
		return "substring-range"
	case containsAny(lower, "execution aborted"):
		return "runtime-error"
	default:
		return ""
	}
}

func stripReferenceRuntimeDiagnostic(output, category string) string {
	markers := map[string][]string{
		"abort":               {"abort called from class"},
		"arithmetic-overflow": {"exception 12", "arithmetic overflow"},
		"case-no-match":       {"no match in case statement", "case without matching branch"},
		"case-on-void":        {"match on void in case statement", "case on void"},
		"dispatch-on-void":    {"dispatch to void", "dispatch on void"},
		"division-by-zero":    {"exception 9", "division by zero", "division by 0", "divide by zero", "divide by 0"},
		"runtime-error":       {"execution aborted"},
		"substring-range":     {"length to substr too long", "substring out of range", "substr out of range", "substr too long"},
	}
	lower := strings.ToLower(output)
	start := -1
	for _, marker := range markers[category] {
		if index := strings.Index(lower, marker); index >= 0 && (start < 0 || index < start) {
			start = index
		}
	}
	if start < 0 {
		return output
	}
	lineStart := strings.LastIndex(output[:start], "\n") + 1
	prefix := strings.ToLower(output[lineStart:start])
	if strings.Contains(prefix, ".cl:") || strings.TrimSpace(prefix) == "" {
		start = lineStart
	}
	return output[:start]
}

func containsAny(text string, fragments ...string) bool {
	for _, fragment := range fragments {
		if strings.Contains(text, fragment) {
			return true
		}
	}
	return false
}

func normalizedSemanticDiagnostic(stage, category string) string {
	if stage == "" && category == "" {
		return ""
	}
	return stage + ":" + category
}

func normalizeProgramOutput(text, workDir string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if workDir != "" {
		text = strings.ReplaceAll(text, workDir, "<TMP>")
	}
	return text
}

func normalizeDiagnosticOutput(text, workDir string) string {
	text = normalizeProgramOutput(text, workDir)
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		lines[index] = strings.Join(strings.Fields(line), " ")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func normalizeReferenceRuntimeOutput(text, workDir string) string {
	text = normalizeProgramOutput(text, workDir)
	lines := strings.Split(text, "\n")
	candidate := 0
	seenVersion := false
	seenLoaded := false
	for candidate < len(lines) {
		line := strings.TrimSpace(lines[candidate])
		version := strings.HasPrefix(line, "SPIM Version") || strings.HasPrefix(line, "QtSpim Version")
		loaded := strings.HasPrefix(line, "Loaded:")
		banner := version || loaded || strings.HasPrefix(line, "Copyright") ||
			strings.HasPrefix(line, "All Rights Reserved") || strings.HasPrefix(line, "See the file")
		if banner {
			seenVersion = seenVersion || version
			seenLoaded = seenLoaded || loaded
			candidate++
			continue
		}
		if line == "" && seenVersion {
			candidate++
			continue
		}
		break
	}
	if !seenVersion || !seenLoaded {
		return text
	}
	programOutput := strings.Join(lines[candidate:], "\n")
	const successFooter = "COOL program successfully executed"
	if strings.HasSuffix(programOutput, successFooter+"\n") {
		return strings.TrimSuffix(programOutput, successFooter+"\n")
	}
	return strings.TrimSuffix(programOutput, successFooter)
}

func normalizeDifferentialCommand(command *DifferentialCommand, workDir string, runtimeOutput bool) {
	for index, arg := range command.Args {
		command.Args[index] = strings.ReplaceAll(arg, workDir, "<TMP>")
	}
	if runtimeOutput {
		command.Stdout = normalizeReferenceRuntimeOutput(command.Stdout, workDir)
	} else {
		command.Stdout = normalizeProgramOutput(command.Stdout, workDir)
	}
	command.Stderr = normalizeDiagnosticOutput(command.Stderr, workDir)
	command.Error = strings.ReplaceAll(command.Error, workDir, "<TMP>")
}

type cappedCommandWriter struct {
	mu       sync.Mutex
	buffer   bytes.Buffer
	limit    int64
	written  int64
	overflow bool
	cancel   context.CancelFunc
}

func (writer *cappedCommandWriter) Write(data []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	remaining := writer.limit - writer.written
	if remaining > 0 {
		keep := int64(len(data))
		if keep > remaining {
			keep = remaining
		}
		_, _ = writer.buffer.Write(data[:int(keep)])
		writer.written += keep
	}
	if int64(len(data)) > remaining {
		writer.overflow = true
		writer.cancel()
	}
	return len(data), nil
}

func (writer *cappedCommandWriter) String() string {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.buffer.String()
}

func (writer *cappedCommandWriter) Overflowed() bool {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.overflow
}

func executeDifferentialCommand(executable string, args []string, workDir, input string, timeout time.Duration, outputLimit int64) DifferentialCommand {
	started := time.Now()
	commandArgs := append([]string{executable}, args...)
	result := DifferentialCommand{Args: commandArgs, ExitCode: -1}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	command := exec.CommandContext(ctx, executable, args...)
	configureDifferentialProcess(command)
	command.Cancel = func() error { return terminateDifferentialProcess(command) }
	command.WaitDelay = time.Second
	command.Dir = workDir
	command.Stdin = strings.NewReader(input)
	stdout := &cappedCommandWriter{limit: outputLimit, cancel: cancel}
	stderr := &cappedCommandWriter{limit: outputLimit, cancel: cancel}
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		result.ExitCategory = "launch-error"
		result.Error = err.Error()
		result.DurationMillis = time.Since(started).Milliseconds()
		return result
	}
	waitErr := command.Wait()
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	result.DurationMillis = time.Since(started).Milliseconds()
	if command.ProcessState != nil {
		result.ExitCode = command.ProcessState.ExitCode()
	}
	switch {
	case stdout.Overflowed() || stderr.Overflowed():
		result.ExitCategory = "output-limit"
		result.Error = "captured output exceeded limit"
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		result.ExitCategory = "timeout"
		result.Error = ctx.Err().Error()
	case waitErr == nil:
		result.ExitCategory = "success"
	case result.ExitCode < 0:
		result.ExitCategory = "crash"
		result.Error = waitErr.Error()
	default:
		result.ExitCategory = "nonzero"
		result.Error = waitErr.Error()
	}
	return result
}
