// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const optimizationProgram = `
class Main inherits IO {
  main() : Object {
    if (1 + 2) = 3 then out_int((6 * 7) + (~0)) else out_int(1 / 0) fi
  };
};
`

func TestBasicOptimizationGoldenAndVerifierEvidence(t *testing.T) {
	baseline := compileWithOptimization(optimizationProgram, "", OptimizationNone)
	optimized := compileWithOptimization(optimizationProgram, "", OptimizationBasic)
	if hasErrors(baseline.Diagnostics) || hasErrors(optimized.Diagnostics) {
		t.Fatalf("compilation failed: baseline=%+v optimized=%+v", baseline.Diagnostics, optimized.Diagnostics)
	}
	if strings.Join(baseline.Runtime.Output, "") != "42" || strings.Join(optimized.Runtime.Output, "") != "42" {
		t.Fatalf("observable output changed: baseline=%q optimized=%q", baseline.Runtime.Output, optimized.Runtime.Output)
	}
	report := optimized.Optimization
	if !report.Enabled || report.ExecutionIR != "optimized" || !report.BaselineVerified || !report.OptimizedVerified || len(report.Passes) != 3 {
		t.Fatalf("incomplete optimization report: %+v", report)
	}
	if len(report.AfterIR) >= len(report.BeforeIR) {
		t.Fatalf("basic passes had no effect: before=%d after=%d", len(report.BeforeIR), len(report.AfterIR))
	}
	for _, instruction := range report.AfterIR {
		if len(instruction.OriginalIndexes) == 0 {
			t.Fatalf("optimized instruction lost origin mapping: %+v", instruction)
		}
	}
	lines := []string{"BEFORE"}
	lines = append(lines, optimizationIRLines(report.BeforeIR, "Main.main")...)
	lines = append(lines, "", "AFTER")
	lines = append(lines, optimizationIRLines(report.AfterIR, "Main.main")...)
	lines = append(lines, "", "PASSES")
	for _, pass := range report.Passes {
		lines = append(lines, fmt.Sprintf("%s %d->%d changes=%d rejected=%d verified=%t", pass.Contract.Name, pass.BeforeInstructions, pass.AfterInstructions, pass.Transformations, pass.Rejected, pass.Verified))
	}
	lines = append(lines, "", "BEFORE CFG")
	lines = append(lines, optimizationCFGLines(report.BeforeCFG)...)
	lines = append(lines, "", "AFTER CFG")
	lines = append(lines, optimizationCFGLines(report.AfterCFG)...)
	assertGolden(t, "golden/optimization-basic.txt", strings.Join(lines, "\n"))
}

func TestOptimizationRefusesRuntimeErrorAndPreservesSpan(t *testing.T) {
	source := `class Main { main() : Object { 10 / 0 }; };`
	baseline := compileWithOptimization(source, "", OptimizationNone)
	optimized := compileWithOptimization(source, "", OptimizationBasic)
	baseError := firstRuntimeError(baseline.Diagnostics)
	optimizedError := firstRuntimeError(optimized.Diagnostics)
	if baseError == nil || optimizedError == nil || baseError.Code != optimizedError.Code || baseError.Span != optimizedError.Span {
		t.Fatalf("runtime error changed: baseline=%+v optimized=%+v", baseError, optimizedError)
	}
	foundRefusal := false
	for _, pass := range optimized.Optimization.Passes {
		for _, rejection := range pass.Rejections {
			foundRefusal = foundRefusal || strings.Contains(rejection.Reason, "division by zero")
		}
	}
	if !foundRefusal {
		t.Fatal("division-by-zero fold was not explicitly rejected")
	}
}

func TestOptimizationPreservesInputAndObjectCopyBehavior(t *testing.T) {
	sources := []struct {
		program string
		input   string
	}{
		{`class Main inherits IO { main() : Object { { out_string(in_string()); out_int(in_int()); } }; };`, "hello\n42\n"},
		{`class Box { value : Object; set(x : Object) : SELF_TYPE { { value <- x; self; } }; get() : Object { value }; }; class Main inherits IO { main() : Object { let child : Box <- new Box, original : Box <- (new Box).set(child), clone : Box <- original.copy() in { if original = clone then out_string("same") else out_string("distinct") fi; if original.get() = clone.get() then out_string(" shared") else out_string(" copied") fi; } }; };`, ""},
	}
	for index, test := range sources {
		baseline := compileWithOptimization(test.program, test.input, OptimizationNone)
		optimized := compileWithOptimization(test.program, test.input, OptimizationBasic)
		if strings.Join(baseline.Runtime.Output, "") != strings.Join(optimized.Runtime.Output, "") || baseline.Runtime.InputEvents != optimized.Runtime.InputEvents || baseline.Runtime.Halted != optimized.Runtime.Halted || baseline.Runtime.WaitingInput != nil || optimized.Runtime.WaitingInput != nil {
			t.Fatalf("case %d observable behavior differs: baseline=%+v optimized=%+v", index, baseline.Runtime, optimized.Runtime)
		}
		if index == 1 {
			baselineHeap := heapAfterOptimization(t, test.program, OptimizationNone)
			optimizedHeap := heapAfterOptimization(t, test.program, OptimizationBasic)
			if !reflect.DeepEqual(baselineHeap, optimizedHeap) {
				t.Fatalf("object/copy heap differs: baseline=%+v optimized=%+v", baselineHeap, optimizedHeap)
			}
		}
	}
}

func TestGeneratedConstantProgramsRemainEquivalent(t *testing.T) {
	for left := 0; left < 20; left++ {
		for right := 1; right < 10; right++ {
			source := fmt.Sprintf(`class Main inherits IO { main() : Object { out_int(((%d + %d) * 3) / %d) }; };`, left, right, right)
			baseline := compileWithOptimization(source, "", OptimizationNone)
			optimized := compileWithOptimization(source, "", OptimizationBasic)
			if strings.Join(baseline.Runtime.Output, "") != strings.Join(optimized.Runtime.Output, "") || hasErrors(baseline.Diagnostics) != hasErrors(optimized.Diagnostics) {
				t.Fatalf("generated case %d/%d differs: baseline=%q optimized=%q", left, right, baseline.Runtime.Output, optimized.Runtime.Output)
			}
		}
	}
}

func TestTerminatingValidConformanceFixturesRemainEquivalent(t *testing.T) {
	type fixtureContract struct {
		ID                          string          `json:"id"`
		Validity                    string          `json:"validity"`
		ExpectedLastSuccessfulStage string          `json:"expectedLastSuccessfulStage"`
		ExpectedRuntimeStatus       string          `json:"expectedRuntimeStatus"`
		DebuggerScenario            json.RawMessage `json:"debuggerScenario"`
		Sources                     []string        `json:"sources"`
		InputLines                  []string        `json:"inputLines"`
	}
	root := filepath.Join("testdata", "conformance")
	fixtures := []string{}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && entry.Name() == "fixture.json" {
			fixtures = append(fixtures, path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(fixtures)
	compared := 0
	for _, fixturePath := range fixtures {
		data, err := os.ReadFile(fixturePath)
		if err != nil {
			t.Fatal(err)
		}
		var fixture fixtureContract
		if err := json.Unmarshal(data, &fixture); err != nil {
			t.Fatal(err)
		}
		if fixture.Validity != "valid" || fixture.ExpectedLastSuccessfulStage != "runtime" || fixture.ExpectedRuntimeStatus == "paused" || len(fixture.DebuggerScenario) > 0 {
			continue
		}
		units := make([]SourceUnit, 0, len(fixture.Sources))
		for index, name := range fixture.Sources {
			source, err := os.ReadFile(filepath.Join(filepath.Dir(fixturePath), name))
			if err != nil {
				t.Fatal(err)
			}
			units = append(units, SourceUnit{ID: fmt.Sprintf("source-%d", index+1), Name: name, Content: string(source)})
		}
		input := strings.Join(fixture.InputLines, "\n")
		if len(fixture.InputLines) > 0 {
			input += "\n"
		}
		baseline := CompileProject(units, ProjectOptions{Input: input, VMMaxSteps: defaultVMMaxSteps, Optimization: OptimizationNone})
		optimized := CompileProject(units, ProjectOptions{Input: input, VMMaxSteps: defaultVMMaxSteps, Optimization: OptimizationBasic})
		if optimizationFullObservable(baseline) != optimizationFullObservable(optimized) {
			t.Fatalf("fixture %s differs:\nbaseline: %s\noptimized: %s", fixture.ID, optimizationFullObservable(baseline), optimizationFullObservable(optimized))
		}
		compared++
	}
	if compared < 15 {
		t.Fatalf("equivalence corpus unexpectedly small: %d fixtures", compared)
	}
}

func FuzzOptimizationEquivalence(f *testing.F) {
	f.Add(int32(1), int32(2), uint8(0))
	f.Add(int32(10), int32(0), uint8(3))
	f.Add(int32(42), int32(42), uint8(5))
	f.Fuzz(func(t *testing.T, rawLeft, rawRight int32, rawOperation uint8) {
		left := uint32(rawLeft) % 10_000
		right := uint32(rawRight) % 10_000
		operation := int(rawOperation % 6)
		expression := fmt.Sprintf("%d + %d", left, right)
		body := fmt.Sprintf("out_int(%s)", expression)
		switch operation {
		case 1:
			expression = fmt.Sprintf("%d - %d", left, right)
			body = fmt.Sprintf("out_int(%s)", expression)
		case 2:
			expression = fmt.Sprintf("%d * %d", left, right)
			body = fmt.Sprintf("out_int(%s)", expression)
		case 3:
			expression = fmt.Sprintf("%d / %d", left, right)
			body = fmt.Sprintf("out_int(%s)", expression)
		case 4:
			expression = fmt.Sprintf("%d < %d", left, right)
			body = fmt.Sprintf(`if %s then out_string("true") else out_string("false") fi`, expression)
		case 5:
			expression = fmt.Sprintf("%d = %d", left, right)
			body = fmt.Sprintf(`if %s then out_string("true") else out_string("false") fi`, expression)
		}
		source := fmt.Sprintf("class Main inherits IO { main() : Object { %s }; };", body)
		baseline := compileWithOptimization(source, "", OptimizationNone)
		optimized := compileWithOptimization(source, "", OptimizationBasic)
		if optimizationObservable(baseline) != optimizationObservable(optimized) {
			t.Fatalf("observable behavior differs for %s: baseline=%s optimized=%s", expression, optimizationObservable(baseline), optimizationObservable(optimized))
		}
	})
}

func TestInvalidOptimizationModeIsAnInputBarrier(t *testing.T) {
	snapshot := NewCompilationSessionWithContract(`class Main { main() : Object { 0 }; };`, ResultContract{Options: CompilationOptions{Optimization: "aggressive"}}).Complete()
	if !snapshot.Blocked || !hasDiagnosticCode(snapshot.Result.Diagnostics, "COL0006") {
		t.Fatalf("invalid optimization mode was accepted: %+v", snapshot)
	}
}

func TestCFGCleanupReachesFixedPoint(t *testing.T) {
	method := &compiledMethod{DisplayName: "Main.main", Code: []byteInstruction{
		{Op: opJump, Target: 2, OriginalIndexes: []int{0}},
		{Op: opPushInt, Int: 9, OriginalIndexes: []int{1}},
		{Op: opJump, Target: 4, OriginalIndexes: []int{2}},
		{Op: opPushInt, Int: 8, OriginalIndexes: []int{3}},
		{Op: opPushInt, Int: 1, OriginalIndexes: []int{4}},
		{Op: opReturn, OriginalIndexes: []int{5}},
	}}
	report := cleanupControlFlowPass(&executable{MethodOrder: []*compiledMethod{method}})
	if len(method.Code) != 2 || method.Code[0].Op != opPushInt || method.Code[1].Op != opReturn {
		t.Fatalf("cleanup did not reach its fixed point: %+v", method.Code)
	}
	foundThread, foundRedundant := false, false
	for _, change := range report.Changes {
		foundThread = foundThread || change.Kind == "jump-thread"
		foundRedundant = foundRedundant || change.Kind == "redundant-jump"
	}
	if !foundThread || !foundRedundant || report.Transformations < 5 {
		t.Fatalf("cleanup evidence incomplete: %+v", report)
	}
}

func BenchmarkBasicOptimizationEffect(b *testing.B) {
	for _, mode := range []string{OptimizationNone, OptimizationBasic} {
		b.Run(mode, func(b *testing.B) {
			b.ReportAllocs()
			for index := 0; index < b.N; index++ {
				result := compileWithOptimization(optimizationProgram, "", mode)
				reduction := len(result.Optimization.BeforeIR) - len(result.Optimization.AfterIR)
				b.ReportMetric(float64(reduction), "IR-insns-reduced")
				if mode == OptimizationBasic && reduction <= 0 {
					b.Fatal("optimization did not reduce this benchmark fixture")
				}
			}
		})
	}
}

func compileWithOptimization(source, input, mode string) Result {
	return CompileWithContract(source, input, ResultContract{Options: CompilationOptions{
		Execute: true, VMMaxSteps: defaultVMMaxSteps, Optimization: mode,
	}})
}

func optimizationIRLines(instructions []IRInstruction, method string) []string {
	result := []string{}
	for _, instruction := range instructions {
		if instruction.Method != method {
			continue
		}
		result = append(result, fmt.Sprintf("%s %-12s %-12s origins=%v", instruction.Method, instruction.Op, strings.Join(instruction.Args, ","), instruction.OriginalIndexes))
	}
	return result
}

func optimizationCFGLines(graph ControlFlowGraph) []string {
	result := []string{}
	for _, block := range graph.Blocks {
		edges := []string{}
		for _, edge := range graph.Edges {
			if edge.From == block.Name {
				edges = append(edges, edge.Kind+"->"+edge.To)
			}
		}
		line := fmt.Sprintf("%s %s", block.Name, block.Range)
		if len(edges) > 0 {
			line += " " + strings.Join(edges, ",")
		}
		result = append(result, line)
	}
	return result
}

func firstRuntimeError(diagnostics []Diagnostic) *Diagnostic {
	for index := range diagnostics {
		if diagnostics[index].Stage == "runtime" && diagnostics[index].Severity == SeverityError {
			return &diagnostics[index]
		}
	}
	return nil
}

func optimizationObservable(result Result) string {
	errorIdentity := ""
	if diagnostic := firstRuntimeError(result.Diagnostics); diagnostic != nil {
		errorIdentity = fmt.Sprintf("%s@%s:%d:%d-%d:%d", diagnostic.Code, diagnostic.Span.SourceID, diagnostic.Span.StartLine, diagnostic.Span.StartColumn, diagnostic.Span.EndLine, diagnostic.Span.EndColumn)
	}
	return fmt.Sprintf("output=%q input=%d halted=%t paused=%t waiting=%t error=%s", strings.Join(result.Runtime.Output, ""), result.Runtime.InputEvents, result.Runtime.Halted, result.Runtime.Paused, result.Runtime.WaitingInput != nil, errorIdentity)
}

func optimizationFullObservable(result Result) string {
	diagnostics := make([]string, 0, len(result.Diagnostics))
	for _, diagnostic := range result.Diagnostics {
		diagnostics = append(diagnostics, fmt.Sprintf("%s/%s/%s@%s:%d:%d-%d:%d", diagnostic.Stage, diagnostic.Severity, diagnostic.Code, diagnostic.Span.SourceID, diagnostic.Span.StartLine, diagnostic.Span.StartColumn, diagnostic.Span.EndLine, diagnostic.Span.EndColumn))
	}
	return fmt.Sprintf("%s diagnostics=%q stack=%v", optimizationObservable(result), diagnostics, result.Runtime.Stack)
}

func heapAfterOptimization(t *testing.T, source, mode string) HeapSnapshot {
	t.Helper()
	pipeline := NewCompilationSessionWithContract(source, ResultContract{Options: CompilationOptions{
		Execute: true, VMMaxSteps: defaultVMMaxSteps, Optimization: mode,
	}})
	snapshot := pipeline.Complete()
	if snapshot.Blocked {
		t.Fatalf("heap fixture compilation blocked: %+v", snapshot.Result.Diagnostics)
	}
	session, ok := pipeline.DebugSession()
	if !ok {
		t.Fatal("heap fixture did not prepare a debug session")
	}
	session.Run(defaultVMMaxSteps, nil)
	heap, err := session.Heap(HeapQuery{Mode: "search"})
	if err != nil {
		t.Fatal(err)
	}
	return heap
}
