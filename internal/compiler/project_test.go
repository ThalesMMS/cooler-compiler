// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import "testing"

const projectHelperSource = `class Helper {
  value() : Int { 41 };
};`

const projectMainSource = `class Main inherits IO {
  main() : Object { out_int((new Helper).value() + 1) };
};`

func TestCompileProjectPreservesOrderedNativeSourceUnits(t *testing.T) {
	units := []SourceUnit{
		{ID: "helper-id", Name: "same.cl", Content: projectHelperSource},
		{ID: "main-id", Name: "same.cl", Content: projectMainSource},
	}
	result := CompileProject(units, ProjectOptions{})
	if hasErrors(result.Diagnostics) {
		t.Fatalf("unexpected diagnostics: %+v", result.Diagnostics)
	}
	if result.Source != "" {
		t.Fatalf("multifile result exposed a synthetic aggregate: %q", result.Source)
	}
	if len(result.SourceUnits) != 2 || result.SourceUnits[0].ID != "helper-id" || result.SourceUnits[1].ID != "main-id" {
		t.Fatalf("source unit order/identity changed: %+v", result.SourceUnits)
	}
	if result.SourceUnits[0].Name != result.SourceUnits[1].Name {
		t.Fatalf("duplicate display names should be accepted: %+v", result.SourceUnits)
	}
	if err := validateReportContract(result); err != nil {
		t.Fatalf("valid project report was rejected: %v", err)
	}
	assertArtifactSourceIDs(t, result)
}

func TestCompileProjectReportsCrossFileDiagnosticsWithoutRemapping(t *testing.T) {
	result := CompileProject([]SourceUnit{
		{ID: "first", Name: "duplicate.cl", Content: "class Duplicate {};"},
		{ID: "second", Name: "duplicate.cl", Content: "class Duplicate {};"},
	}, ProjectOptions{})
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code != "COL3002" {
			continue
		}
		if diagnostic.Span.SourceID != "second" || diagnostic.Line != 1 || diagnostic.Span.StartLine != 1 {
			t.Fatalf("duplicate diagnostic was remapped: %+v", diagnostic)
		}
		if len(diagnostic.Related) != 1 || diagnostic.Related[0].Span.SourceID != "first" || diagnostic.Related[0].Line != 1 {
			t.Fatalf("related location lost its source identity: %+v", diagnostic.Related)
		}
		if len(diagnostic.QuickFix) != 1 || diagnostic.QuickFix[0].Span.SourceID != "second" {
			t.Fatalf("quick fix lost its target source identity: %+v", diagnostic.QuickFix)
		}
		return
	}
	t.Fatalf("missing duplicate-class diagnostic: %+v", result.Diagnostics)
}

func TestCompileProjectValidatesUnitIdentityAndPreservesText(t *testing.T) {
	units := []SourceUnit{
		{ID: "same", Name: "first.cl", Content: "-- comment only\r\n"},
		{ID: "same", Name: "second.cl", Content: projectMainSource},
	}
	result := CompileProject(units, ProjectOptions{})
	if !hasDiagnosticCode(result.Diagnostics, "COL0003") {
		t.Fatalf("duplicate source ID was accepted: %+v", result.Diagnostics)
	}
	if len(result.SourceUnits) != 2 || result.SourceUnits[0].ByteSize != len(units[0].Content) {
		t.Fatalf("source metadata did not preserve exact CRLF bytes: %+v", result.SourceUnits)
	}
}

func TestCompileProjectAcceptsEmptyAndCommentOnlyUnits(t *testing.T) {
	result := CompileProject([]SourceUnit{
		{ID: "empty", Name: "empty.cl", Content: ""},
		{ID: "comment", Name: "comment.cl", Content: "-- retained\r\n"},
		{ID: "main", Name: "main.cl", Content: projectMainSource},
		{ID: "helper", Name: "helper.cl", Content: projectHelperSource},
	}, ProjectOptions{})
	if hasErrors(result.Diagnostics) {
		t.Fatalf("empty organizational units blocked a valid program: %+v", result.Diagnostics)
	}
	if len(result.SourceUnits) != 4 || result.SourceUnits[1].ByteSize != len("-- retained\r\n") {
		t.Fatalf("empty/comment source metadata changed: %+v", result.SourceUnits)
	}
}

func TestCompileProjectWithoutUnitsReportsOnlyProjectInputError(t *testing.T) {
	result := CompileProject(nil, ProjectOptions{})
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "COL0001" {
		t.Fatalf("unexpected empty-project diagnostics: %+v", result.Diagnostics)
	}
}

func TestSourceAwareBreakpointSurvivesUnitReordering(t *testing.T) {
	for _, units := range [][]SourceUnit{
		{{ID: "helper-id", Name: "helper.cl", Content: projectHelperSource}, {ID: "main-id", Name: "main.cl", Content: projectMainSource}},
		{{ID: "main-id", Name: "main.cl", Content: projectMainSource}, {ID: "helper-id", Name: "helper.cl", Content: projectHelperSource}},
	} {
		pipeline := NewProjectCompilationSession(units, ResultContract{})
		snapshot := pipeline.Complete()
		if snapshot.Blocked {
			t.Fatalf("project blocked after reorder: %+v", snapshot.Result.Diagnostics)
		}
		debug, ok := pipeline.DebugSession()
		if !ok {
			t.Fatal("missing debug session")
		}
		runtime := debug.RunWithBreakpoints(100, []Breakpoint{{SourceID: "main-id", Line: 2}}).Runtime
		if !runtime.Paused || len(runtime.Trace) == 0 || runtime.Trace[len(runtime.Trace)-1].Op != "BREAKPOINT" {
			t.Fatalf("source breakpoint did not pause: %+v", runtime)
		}
		if runtime.Trace[len(runtime.Trace)-1].Span.SourceID != "main-id" || len(runtime.Frames) == 0 || runtime.Frames[len(runtime.Frames)-1].Span.SourceID != "main-id" {
			t.Fatalf("runtime source identity was lost: %+v", runtime)
		}
	}
}

func assertArtifactSourceIDs(t *testing.T, result Result) {
	t.Helper()
	assertBoth := func(label string, spans []Span) {
		seen := map[string]bool{}
		for _, span := range spans {
			if span.SourceID != "" {
				seen[span.SourceID] = true
			}
		}
		if !seen["helper-id"] || !seen["main-id"] {
			t.Fatalf("%s source identities = %v, want helper-id and main-id", label, seen)
		}
	}
	tokenSpans := []Span{}
	for _, token := range result.Tokens {
		tokenSpans = append(tokenSpans, token.Span)
	}
	assertBoth("tokens", tokenSpans)
	astSpans := []Span{}
	var collectAST func(*ASTNode)
	collectAST = func(node *ASTNode) {
		if node == nil {
			return
		}
		astSpans = append(astSpans, node.Span)
		for index := range node.Children {
			collectAST(&node.Children[index])
		}
	}
	collectAST(result.AST)
	assertBoth("AST", astSpans)
	symbolSpans := []Span{}
	for _, symbol := range result.Symbols {
		symbolSpans = append(symbolSpans, symbol.Span)
	}
	assertBoth("symbols", symbolSpans)
	irSpans := []Span{}
	for _, instruction := range result.IR {
		irSpans = append(irSpans, instruction.Span)
	}
	assertBoth("IR", irSpans)
	assertBoth("assembly", result.AssemblySpans)
	blockSpans := []Span{}
	for _, block := range result.ControlFlow.Blocks {
		blockSpans = append(blockSpans, block.Span)
	}
	assertBoth("CFG blocks", blockSpans)
	for _, edge := range result.ControlFlow.Edges {
		if edge.Span.SourceID == "" {
			t.Fatalf("source-linked CFG edge has no source ID: %+v", edge)
		}
	}
	traceSpans := []Span{}
	for _, state := range result.Runtime.Trace {
		traceSpans = append(traceSpans, state.Span)
	}
	assertBoth("runtime trace", traceSpans)
	if len(result.Assembly) != len(result.AssemblySpans) {
		t.Fatalf("assembly source map is not line-aligned: %d != %d", len(result.Assembly), len(result.AssemblySpans))
	}
}

func hasDiagnosticCode(diagnostics []Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}
