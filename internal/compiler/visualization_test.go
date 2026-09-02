// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestVisualizationGraphAndTablesGolden(t *testing.T) {
	result := CompileProject([]SourceUnit{
		{ID: "base", Name: "base.cl", Content: `class A { x : Object; f() : SELF_TYPE { self }; };`},
		{ID: "main", Name: "main.cl", Content: `class B inherits A { y : Int; f() : SELF_TYPE { self }; }; class Main inherits B { main() : Object { self }; };`},
	}, ProjectOptions{})
	if hasErrors(result.Diagnostics) || result.Visualizations == nil {
		t.Fatalf("visualization unavailable: %+v", result.Diagnostics)
	}
	model := findLogicalClass(result.Visualizations.Classes, "B")
	if model == nil {
		t.Fatal("class B model is missing")
	}
	lines := []string{"INHERITANCE"}
	for _, node := range result.Visualizations.Inheritance {
		lines = append(lines, fmt.Sprintf("%s <- %s [%s/%s]", node.Name, node.Parent, node.Kind, node.Status))
	}
	lines = append(lines, "", "B ATTRIBUTES")
	for _, attr := range model.Attributes {
		lines = append(lines, fmt.Sprintf("%d %s.%s : %s inherited=%t", attr.Slot, attr.Owner, attr.Name, attr.Type, attr.Inherited))
	}
	lines = append(lines, "", "B DISPATCH")
	for _, method := range model.Dispatch {
		lines = append(lines, fmt.Sprintf("%d %s.%s self=%t override=%s", method.Slot, method.Owner, method.Name, method.ReturnsSelfType, method.Overrides))
	}
	lines = append(lines, "", "LOOKUP "+strings.Join(model.LookupOrder, " -> "))
	assertGolden(t, "golden/visualization-model.txt", strings.Join(lines, "\n"))

	if result.Visualizations.SchemaVersion != visualizationSchemaVersion || strings.Join(result.Visualizations.SourceUnitIDs, ",") != "base,main" {
		t.Fatalf("export identity is incomplete: %+v", result.Visualizations)
	}
	if len(result.Visualizations.Groups) == 0 || len(result.Visualizations.Artifacts) == 0 {
		t.Fatal("cross-phase provenance was not materialized")
	}
	for _, instruction := range result.IR {
		if strings.Contains(instruction.Method, "$init") && !instruction.Synthetic {
			t.Fatalf("generated instruction is not marked synthetic: %+v", instruction)
		}
	}
}

func TestVisualizationPreservesInvalidInheritanceCycle(t *testing.T) {
	result := Compile(`class A inherits B {}; class B inherits A {}; class Main { main() : Object { 0 }; };`)
	if result.Visualizations == nil || !hasDiagnosticCode(result.Diagnostics, "COL3012") {
		t.Fatalf("cycle setup failed: %+v", result.Diagnostics)
	}
	cycleEdges := 0
	for _, edge := range result.Visualizations.Edges {
		if (edge.From == "A" || edge.From == "B") && edge.Cycle && edge.Recovered {
			cycleEdges++
		}
	}
	if cycleEdges != 2 {
		t.Fatalf("original recovered cycle is not explicit: %+v", result.Visualizations.Edges)
	}
}

func TestVisualizationResultCloneIsIndependent(t *testing.T) {
	result := Compile(`class Main { main() : Object { 0 }; };`)
	clone := cloneResult(result)
	clone.Visualizations.Classes[0].LookupOrder[0] = "changed"
	if result.Visualizations.Classes[0].LookupOrder[0] == "changed" {
		t.Fatal("visualization snapshot shares mutable slices")
	}
	data, err := json.Marshal(clone)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"attributes":null`) || strings.Contains(string(data), `"dispatch":null`) {
		t.Fatalf("empty visualization collections must serialize as arrays: %s", data)
	}
}

func BenchmarkBuildVisualizationsLargeProject(b *testing.B) {
	var source strings.Builder
	for index := 0; index < 300; index++ {
		parent := "Object"
		if index > 0 {
			parent = fmt.Sprintf("C%d", index-1)
		}
		fmt.Fprintf(&source, "class C%d inherits %s { value%d : Int; method%d() : Int { %d }; };\n", index, parent, index, index, index)
	}
	source.WriteString("class Main inherits C299 { main() : Object { self }; };\n")
	program, diagnostics := Parse(mustLexForVisualizationBenchmark(b, source.String()))
	if hasErrors(diagnostics) {
		b.Fatal(diagnostics)
	}
	analysis := Analyze(program)
	result := newResult(source.String(), ResultContract{})
	result.AST = program.ToAST()
	result.Symbols = analysis.Symbols
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		bundle := BuildVisualizations(program, analysis, result)
		if len(bundle.Classes) != 306 || len(bundle.Groups) == 0 || len(bundle.Artifacts) > bundle.Limits.MaxArtifacts {
			b.Fatalf("unexpected bounded model: classes=%d truncated=%t", len(bundle.Classes), bundle.Truncated)
		}
	}
}

func mustLexForVisualizationBenchmark(b *testing.B, source string) []Token {
	b.Helper()
	tokens, diagnostics := LexSource("source-1", source)
	if hasErrors(diagnostics) {
		b.Fatal(diagnostics)
	}
	return tokens
}

func findLogicalClass(classes []LogicalClassModel, name string) *LogicalClassModel {
	for index := range classes {
		if classes[index].Class == name {
			return &classes[index]
		}
	}
	return nil
}
