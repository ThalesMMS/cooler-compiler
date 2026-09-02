// SPDX-License-Identifier: AGPL-3.0-or-later

package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ThalesMMS/cooler-compiler/internal/compiler"
)

func Run(root, repoRoot string) (Report, error) {
	matrix, fixtures, err := Load(root, repoRoot)
	if err != nil {
		return Report{}, err
	}

	fixtureResults := make([]FixtureResult, 0, len(fixtures))
	fixtureByID := map[string]FixtureResult{}
	for _, fixture := range fixtures {
		result := runFixture(fixture)
		fixtureResults = append(fixtureResults, result)
		fixtureByID[result.ID] = result
	}

	report := Report{
		SchemaVersion:       schemaVersion,
		MatrixSchemaVersion: matrix.SchemaVersion,
		Fixtures:            fixtureResults,
	}
	for _, rule := range matrix.Rules {
		ids := append(append([]string{}, rule.PositiveFixtures...), rule.NegativeFixtures...)
		sort.Strings(ids)
		result := RuleResult{ID: rule.ID, DeclaredStatus: rule.Status, FixtureIDs: ids}
		for _, id := range ids {
			if fixture := fixtureByID[id]; !fixture.Passed {
				result.Failures = append(result.Failures, fmt.Sprintf("%s: %s", id, fixture.Failure))
			}
		}
		if len(result.Failures) > 0 {
			result.Outcome = "failed"
		} else {
			switch rule.Status {
			case "supported":
				result.Outcome = "passed"
			case "partial":
				result.Outcome = "partial"
			case "intentional-divergence":
				result.Outcome = "divergent"
			case "unsupported", "not-yet-tested":
				result.Outcome = "untested"
			}
		}
		report.Rules = append(report.Rules, result)
	}

	report.Summary.Rules = len(report.Rules)
	report.Summary.Fixtures = len(report.Fixtures)
	for _, rule := range report.Rules {
		switch rule.Outcome {
		case "passed":
			report.Summary.Passed++
		case "failed":
			report.Summary.Failed++
		case "partial":
			report.Summary.Partial++
		case "divergent":
			report.Summary.Divergent++
		case "untested":
			report.Summary.Untested++
		}
	}
	if report.Summary.Failed > 0 {
		return report, fmt.Errorf("%d conformance rule(s) failed", report.Summary.Failed)
	}
	return report, nil
}

func WriteReport(path string, report Report) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func runFixture(fixture Fixture) FixtureResult {
	result := FixtureResult{ID: fixture.ID, Passed: true}
	sourceUnits, err := readSources(fixture)
	if err != nil {
		result.Passed = false
		result.Failure = err.Error()
		return result
	}
	if fixture.DebuggerScenario != nil {
		return runDebuggerFixture(fixture, sourceUnits)
	}
	input := strings.Join(fixture.InputLines, "\n")
	if len(fixture.InputLines) > 0 {
		input += "\n"
	}
	policy := compiler.VMPolicy{}
	if fixture.VMPolicy != nil {
		policy = *fixture.VMPolicy
	}
	maxSteps := 0
	if len(fixture.RunSteps) > 0 {
		maxSteps = fixture.RunSteps[0]
	}
	compiled, completedStage := compileProjectThroughConfigured(fixture.RunThrough, sourceUnits, input, compiler.ResultContract{
		Options: compiler.CompilationOptions{Execute: true, InputProvided: input != "", VMMaxSteps: maxSteps, VMPolicy: policy},
	}, fixture.RunSteps, fixture.Breakpoints)
	diagnostics := append([]compiler.Diagnostic{}, compiled.Diagnostics...)
	sourceNames := map[string]string{}
	for _, unit := range compiled.SourceUnits {
		sourceNames[unit.ID] = unit.Name
	}
	actualDiagnostics := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		actualDiagnostics = append(actualDiagnostics, diagnosticKey(diagnostic.Stage, string(diagnostic.Severity), diagnostic.Code))
		result.DiagnosticDetails = append(result.DiagnosticDetails, observedDiagnostic(diagnostic, sourceNames))
	}
	result.Diagnostics = actualDiagnostics
	result.Output = strings.Join(compiled.Runtime.Output, "")
	if completedStage == "runtime" {
		result.RuntimeStatus = runtimeStatus(compiled)
	}
	for _, unit := range compiled.SourceUnits {
		result.SourceUnits = append(result.SourceUnits, unit.Name)
	}
	result.LastSuccessfulStage = lastSuccessfulStage(diagnostics, completedStage)
	for _, token := range compiled.Tokens {
		if token.Type == compiler.TokenEOF {
			continue
		}
		span := expectedSpan(token.Span)
		result.Tokens = append(result.Tokens, ExpectedToken{Type: string(token.Type), Lexeme: token.Lexeme, Literal: token.Literal, Span: &span})
	}

	expectedDiagnostics := make([]string, 0, len(fixture.ExpectedDiagnostics))
	for _, diagnostic := range fixture.ExpectedDiagnostics {
		expectedDiagnostics = append(expectedDiagnostics, diagnosticKey(diagnostic.Stage, diagnostic.Severity, diagnostic.Code))
	}

	failures := []string{}
	if strings.Join(actualDiagnostics, "\n") != strings.Join(expectedDiagnostics, "\n") {
		failures = append(failures, fmt.Sprintf("diagnostics %v, want %v", actualDiagnostics, expectedDiagnostics))
	}
	if failure := compareDiagnosticDetails(result.DiagnosticDetails, fixture.ExpectedDiagnostics); failure != "" {
		failures = append(failures, failure)
	}
	if failure := compareTokens(result.Tokens, fixture.ExpectedTokens); failure != "" {
		failures = append(failures, failure)
	}
	result.AST, failures = observeExpectedAST(compiled.AST, fixture.ExpectedAST, failures)
	if result.LastSuccessfulStage != fixture.ExpectedLastSuccessfulStage {
		failures = append(failures, fmt.Sprintf("last successful stage %q, want %q", result.LastSuccessfulStage, fixture.ExpectedLastSuccessfulStage))
	}
	if result.Output != fixture.ExpectedOutput {
		failures = append(failures, fmt.Sprintf("output %q, want %q", result.Output, fixture.ExpectedOutput))
	}
	if fixture.ExpectedRuntimeStatus != "" && result.RuntimeStatus != fixture.ExpectedRuntimeStatus {
		failures = append(failures, fmt.Sprintf("runtime status %q, want %q", result.RuntimeStatus, fixture.ExpectedRuntimeStatus))
	}
	if compiled.Runtime.TraceOffset < fixture.ExpectedTraceOffsetMinimum {
		failures = append(failures, fmt.Sprintf("trace offset %d, want at least %d", compiled.Runtime.TraceOffset, fixture.ExpectedTraceOffsetMinimum))
	}
	if maximum := fixture.ExpectedRetainedTraceMaximum; maximum != nil && len(compiled.Runtime.Trace) > *maximum {
		failures = append(failures, fmt.Sprintf("retained trace entries %d, want at most %d", len(compiled.Runtime.Trace), *maximum))
	}
	if strings.Join(result.SourceUnits, "\n") != strings.Join(fixture.ExpectedSourceUnits, "\n") && len(fixture.ExpectedSourceUnits) > 0 {
		failures = append(failures, fmt.Sprintf("source units %v, want %v", result.SourceUnits, fixture.ExpectedSourceUnits))
	}
	for _, artifact := range fixture.ExpectedArtifacts {
		if !artifactPresent(compiled, artifact) {
			failures = append(failures, "missing artifact "+artifact)
		}
	}
	if fixture.Validity == "valid" && hasError(diagnostics) {
		failures = append(failures, "valid fixture produced an error")
	}
	if fixture.Validity == "invalid" && !hasError(diagnostics) {
		failures = append(failures, "invalid fixture produced no error")
	}
	if len(failures) > 0 {
		result.Passed = false
		result.Failure = strings.Join(failures, "; ")
	}
	return result
}

func compileProjectThrough(target string, units []compiler.SourceUnit, input string, contract compiler.ResultContract) (compiler.Result, string) {
	return compileProjectThroughConfigured(target, units, input, contract, nil, nil)
}

func compileProjectThroughConfigured(target string, units []compiler.SourceUnit, input string, contract compiler.ResultContract, runSteps, breakpoints []int) (compiler.Result, string) {
	if target == "" {
		target = "runtime"
	}
	session := compiler.NewProjectCompilationSession(units, contract)
	snapshot := session.Snapshot()
	for string(snapshot.CurrentPhase) != target && !snapshot.Blocked && !snapshot.Complete {
		snapshot = session.NextPhase()
	}
	if target == "runtime" && snapshot.CurrentPhase == compiler.PhaseRuntime {
		if debug, ok := session.DebugSession(); ok {
			debug.SetInput(input)
			if len(runSteps) == 0 {
				runSteps = []int{snapshot.Result.Options.VMMaxSteps}
			}
			for _, steps := range runSteps {
				snapshot.Result = debug.Run(steps, breakpoints).Result
			}
		}
	}
	return snapshot.Result, string(snapshot.CurrentPhase)
}

func expectedSpan(span compiler.Span) ExpectedSpan {
	return ExpectedSpan{
		StartLine: span.StartLine, StartColumn: span.StartColumn,
		EndLine: span.EndLine, EndColumn: span.EndColumn,
		StartOffset: span.StartOffset, EndOffset: span.EndOffset,
	}
}

func observedDiagnostic(diagnostic compiler.Diagnostic, sourceNames map[string]string) ExpectedDiagnostic {
	span := expectedSpan(diagnostic.Span)
	observed := ExpectedDiagnostic{
		Code: diagnostic.Code, Stage: diagnostic.Stage, Severity: string(diagnostic.Severity),
		Source: sourceName(sourceNames, diagnostic.Span.SourceID), Span: &span,
	}
	for _, related := range diagnostic.Related {
		relatedSpan := expectedSpan(related.Span)
		observed.Related = append(observed.Related, ExpectedRelatedSpan{
			Label: related.Label, Source: sourceName(sourceNames, related.Span.SourceID), Span: &relatedSpan,
		})
	}
	return observed
}

func sourceName(names map[string]string, id string) string {
	if name := names[id]; name != "" {
		return name
	}
	return id
}

func compareDiagnosticDetails(actual, expected []ExpectedDiagnostic) string {
	if len(actual) != len(expected) {
		return ""
	}
	for index, want := range expected {
		got := actual[index]
		if want.Source != "" && got.Source != want.Source {
			return fmt.Sprintf("diagnostic[%d] source %q, want %q", index, got.Source, want.Source)
		}
		if want.Span != nil && (got.Span == nil || *got.Span != *want.Span) {
			return fmt.Sprintf("diagnostic[%d] span %+v, want %+v", index, got.Span, want.Span)
		}
		if len(want.Related) > 0 {
			if len(got.Related) != len(want.Related) {
				return fmt.Sprintf("diagnostic[%d] related spans %d, want %d", index, len(got.Related), len(want.Related))
			}
			for relatedIndex, relatedWant := range want.Related {
				relatedGot := got.Related[relatedIndex]
				if relatedGot.Label != relatedWant.Label || (relatedWant.Source != "" && relatedGot.Source != relatedWant.Source) || (relatedWant.Span != nil && (relatedGot.Span == nil || *relatedGot.Span != *relatedWant.Span)) {
					return fmt.Sprintf("diagnostic[%d] related[%d] %+v, want %+v", index, relatedIndex, relatedGot, relatedWant)
				}
			}
		}
	}
	return ""
}

func observeExpectedAST(root *compiler.ASTNode, expected []ExpectedASTNode, failures []string) ([]ExpectedASTNode, []string) {
	observed := make([]ExpectedASTNode, 0, len(expected))
	for index, want := range expected {
		node := astNodeAt(root, want.Path)
		if node == nil {
			failures = append(failures, fmt.Sprintf("expectedAst[%d] path %v does not exist", index, want.Path))
			continue
		}
		span := expectedSpan(node.Span)
		got := ExpectedASTNode{
			Path: append([]int{}, want.Path...), Kind: node.Kind, Text: node.Text,
			Name: node.Name, Type: node.Type, Value: node.Value, Span: &span,
		}
		observed = append(observed, got)
		if got.Kind != want.Kind || (want.Text != "" && got.Text != want.Text) || (want.Name != "" && got.Name != want.Name) || (want.Type != "" && got.Type != want.Type) || (want.Value != "" && got.Value != want.Value) || (want.Span != nil && *got.Span != *want.Span) {
			failures = append(failures, fmt.Sprintf("expectedAst[%d] %+v, want %+v", index, got, want))
		}
	}
	return observed, failures
}

func astNodeAt(root *compiler.ASTNode, path []int) *compiler.ASTNode {
	node := root
	for _, child := range path {
		if node == nil || child < 0 || child >= len(node.Children) {
			return nil
		}
		node = &node.Children[child]
	}
	return node
}

func compareTokens(actual, expected []ExpectedToken) string {
	if len(expected) == 0 {
		return ""
	}
	if len(actual) != len(expected) {
		return fmt.Sprintf("tokens count %d, want %d", len(actual), len(expected))
	}
	for index := range expected {
		got, want := actual[index], expected[index]
		if got.Type != want.Type || got.Lexeme != want.Lexeme || got.Literal != want.Literal {
			return fmt.Sprintf("token[%d] %s/%q/%q, want %s/%q/%q", index, got.Type, got.Lexeme, got.Literal, want.Type, want.Lexeme, want.Literal)
		}
		if want.Span != nil && (got.Span == nil || *got.Span != *want.Span) {
			return fmt.Sprintf("token[%d] span %+v, want %+v", index, got.Span, want.Span)
		}
	}
	return ""
}

func runtimeStatus(result compiler.Result) string {
	if result.Runtime.Paused {
		return "paused"
	}
	if result.Runtime.Halted {
		return "halted"
	}
	return "ready"
}

func readSources(fixture Fixture) ([]compiler.SourceUnit, error) {
	base := filepath.Dir(fixture.metadataPath)
	units := make([]compiler.SourceUnit, 0, len(fixture.Sources))
	for index, source := range fixture.Sources {
		data, err := os.ReadFile(filepath.Join(base, filepath.FromSlash(source)))
		if err != nil {
			return nil, fmt.Errorf("read fixture %s source %s: %w", fixture.ID, source, err)
		}
		units = append(units, compiler.SourceUnit{ID: fmt.Sprintf("%s-source-%d", fixture.ID, index+1), Name: source, Content: string(data)})
	}
	return units, nil
}

func diagnosticKey(stage, severity, code string) string {
	return stage + ":" + severity + ":" + code
}

func hasError(diagnostics []compiler.Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == compiler.SeverityError {
			return true
		}
	}
	return false
}

func lastSuccessfulStage(diagnostics []compiler.Diagnostic, completedStage string) string {
	order := []string{"lexer", "parser", "semantic", "ir", "codegen", "runtime"}
	completed := -1
	for index, stage := range order {
		if stage == completedStage {
			completed = index
			break
		}
	}
	firstError := completed + 1
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity != compiler.SeverityError {
			continue
		}
		for index, stage := range order {
			if diagnostic.Stage == stage && index < firstError {
				firstError = index
			}
		}
	}
	if firstError == 0 {
		return "none"
	}
	if firstError <= completed {
		return order[firstError-1]
	}
	if completed >= 0 {
		return order[completed]
	}
	return "none"
}

func artifactPresent(result compiler.Result, artifact string) bool {
	switch artifact {
	case "tokens":
		return len(result.Tokens) > 0
	case "ast":
		return result.AST != nil
	case "symbols":
		return len(result.Symbols) > 0
	case "ir":
		return len(result.IR) > 0
	case "assembly":
		return len(result.Assembly) > 0
	case "controlFlow":
		return len(result.ControlFlow.Blocks) > 0
	case "runtimeTrace":
		return len(result.Runtime.Trace) > 0
	default:
		return false
	}
}
