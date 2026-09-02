// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/ThalesMMS/cooler-compiler/internal/benchwork"
)

var (
	benchmarkTokenSink       []Token
	benchmarkProgramSink     *Program
	benchmarkAnalysisSink    Analysis
	benchmarkExecutableSink  *executable
	benchmarkDiagnosticsSink []Diagnostic
	benchmarkBytesSink       []byte
	benchmarkSnapshotSink    DebugSnapshot
)

func BenchmarkLex(b *testing.B) {
	workloads := append(benchwork.StandardProjects(), benchwork.LongText(500))
	for _, workload := range workloads {
		b.Run(workload.Name, func(b *testing.B) {
			reportBenchmarkSource(b, workload.Source)
			b.ReportAllocs()
			for b.Loop() {
				benchmarkTokenSink, benchmarkDiagnosticsSink = Lex(workload.Source)
			}
		})
	}
}

func BenchmarkParse(b *testing.B) {
	workloads := append(benchwork.StandardProjects(), benchwork.NestedExpression(400))
	for _, workload := range workloads {
		b.Run(workload.Name, func(b *testing.B) {
			tokens, diagnostics := Lex(workload.Source)
			if hasErrors(diagnostics) {
				b.Fatalf("benchmark lex failed: %+v", diagnostics)
			}
			reportBenchmarkSource(b, workload.Source)
			b.ReportMetric(float64(len(tokens)), "tokens/op")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				benchmarkProgramSink, benchmarkDiagnosticsSink = Parse(tokens)
			}
		})
	}
}

func BenchmarkClassTable(b *testing.B) {
	for _, workload := range benchwork.StandardProjects() {
		b.Run(workload.Name, func(b *testing.B) {
			program := benchmarkParseProgram(b, workload.Source)
			reportBenchmarkSource(b, workload.Source)
			b.ReportMetric(float64(len(program.Classes)), "classes/op")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				analyzer := benchmarkBuildClassTable(program)
				benchmarkAnalysisSink = benchmarkAnalysisResult(analyzer)
			}
		})
	}
}

func BenchmarkSemanticBodies(b *testing.B) {
	for _, workload := range benchwork.StandardProjects() {
		b.Run(workload.Name, func(b *testing.B) {
			program := benchmarkParseProgram(b, workload.Source)
			classAnalyzer := benchmarkBuildClassTable(program)
			reportBenchmarkSource(b, workload.Source)
			b.ReportMetric(float64(len(program.Classes)), "classes/op")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				analyzer := &analyzer{program: program, table: classAnalyzer.table}
				analyzer.installBuiltinSymbols()
				analyzer.checkBodies()
				benchmarkAnalysisSink = benchmarkAnalysisResult(analyzer)
			}
		})
	}
}

func BenchmarkTypeQueries(b *testing.B) {
	workload := benchwork.DeepInheritance(400)
	program := benchmarkParseProgram(b, workload.Source)
	analysis := Analyze(program)
	analyzer := &analyzer{program: program, table: analysis.Table, currentClass: "Main"}
	reportBenchmarkSource(b, workload.Source)
	b.ReportMetric(200, "queries/op")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for range 100 {
			if !analyzer.conforms("DeepClass399", "DeepClass100") {
				b.Fatal("benchmark conformance query changed")
			}
			if analyzer.join("DeepClass399", "DeepClass200") != "DeepClass200" {
				b.Fatal("benchmark LUB query changed")
			}
		}
	}
}

func BenchmarkIRLowering(b *testing.B) {
	for _, workload := range benchwork.StandardProjects() {
		b.Run(workload.Name, func(b *testing.B) {
			program, analysis := benchmarkAnalyzedProgram(b, workload.Source)
			reportBenchmarkSource(b, workload.Source)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				benchmarkExecutableSink = generateExecutable(program, analysis)
			}
		})
	}
}

func BenchmarkCFGGeneration(b *testing.B) {
	for _, workload := range benchwork.StandardProjects() {
		b.Run(workload.Name, func(b *testing.B) {
			program, analysis := benchmarkAnalyzedProgram(b, workload.Source)
			exec := generateExecutable(program, analysis)
			reportBenchmarkSource(b, workload.Source)
			b.ReportMetric(float64(len(exec.MethodOrder)), "methods/op")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				blocks, edges := 0, 0
				start := 0
				for _, method := range exec.MethodOrder {
					methodBlocks, methodEdges := controlFlowForMethod(method, start)
					blocks += len(methodBlocks)
					edges += len(methodEdges)
					start += len(method.Code)
				}
				if blocks == 0 || edges < 0 {
					b.Fatal("empty benchmark CFG")
				}
			}
		})
	}
}

func BenchmarkAssemblyFormatting(b *testing.B) {
	for _, workload := range benchwork.StandardProjects() {
		b.Run(workload.Name, func(b *testing.B) {
			program, analysis := benchmarkAnalyzedProgram(b, workload.Source)
			exec := generateExecutable(program, analysis)
			reportBenchmarkSource(b, workload.Source)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				assembly, spans := assemblyForExecutable(exec)
				if len(assembly) != len(spans) {
					b.Fatal("assembly/span count changed")
				}
			}
		})
	}
}

func BenchmarkBytecodeVerification(b *testing.B) {
	for _, workload := range benchwork.StandardProjects() {
		b.Run(workload.Name, func(b *testing.B) {
			program, analysis := benchmarkAnalyzedProgram(b, workload.Source)
			exec := generateExecutable(program, analysis)
			materializeIR(exec)
			materializeAssembly(exec)
			reportBenchmarkSource(b, workload.Source)
			b.ReportMetric(float64(len(exec.IR)), "ir-instructions/op")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				benchmarkDiagnosticsSink = verifyExecutable(exec)
			}
		})
	}
}

func BenchmarkCompileComplete(b *testing.B) {
	for _, workload := range benchwork.StandardProjects() {
		b.Run(workload.Name, func(b *testing.B) {
			reportBenchmarkSource(b, workload.Source)
			b.ReportAllocs()
			for b.Loop() {
				result := Compile(workload.Source)
				if hasErrors(result.Diagnostics) {
					b.Fatalf("benchmark compilation failed: %+v", result.Diagnostics)
				}
			}
		})
	}
}

func BenchmarkCompileProject(b *testing.B) {
	for _, workload := range benchwork.StandardProjects() {
		b.Run(workload.Name, func(b *testing.B) {
			units := benchmarkSourceUnits(workload.Units)
			reportBenchmarkSource(b, workload.Source)
			b.ReportMetric(float64(len(units)), "source-units/op")
			b.ReportAllocs()
			for b.Loop() {
				result := CompileProject(units, ProjectOptions{VMMaxSteps: defaultVMMaxSteps})
				if hasErrors(result.Diagnostics) {
					b.Fatalf("benchmark project failed: %+v", result.Diagnostics)
				}
			}
		})
	}
}

func BenchmarkJSONSerialization(b *testing.B) {
	for _, workload := range benchwork.StandardProjects() {
		b.Run(workload.Name, func(b *testing.B) {
			result := Compile(workload.Source)
			encoded, err := json.Marshal(result)
			if err != nil {
				b.Fatal(err)
			}
			reportBenchmarkSource(b, workload.Source)
			b.ReportMetric(float64(len(encoded)), "report-bytes/op")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				benchmarkBytesSink, err = json.Marshal(result)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkScalingLex(b *testing.B) {
	for _, classes := range []int{8, 32, 128, 256} {
		workload := benchwork.Project("scaling", benchwork.ProjectSize{Classes: classes, MethodsPerClass: 2, ExpressionTerms: 4, ClassesPerUnit: classes + 1})
		b.Run(benchmarkSizeName(classes), func(b *testing.B) {
			reportBenchmarkSource(b, workload.Source)
			b.ReportMetric(float64(classes), "classes/op")
			b.ReportAllocs()
			for b.Loop() {
				benchmarkTokenSink, benchmarkDiagnosticsSink = Lex(workload.Source)
			}
		})
	}
}

func BenchmarkScalingProject(b *testing.B) {
	for _, classes := range []int{8, 32, 128, 256} {
		workload := benchwork.Project("scaling", benchwork.ProjectSize{Classes: classes, MethodsPerClass: 2, ExpressionTerms: 4, ClassesPerUnit: 8})
		units := benchmarkSourceUnits(workload.Units)
		b.Run(benchmarkSizeName(classes), func(b *testing.B) {
			reportBenchmarkSource(b, workload.Source)
			b.ReportMetric(float64(classes), "classes/op")
			b.ReportAllocs()
			for b.Loop() {
				result := CompileProject(units, ProjectOptions{VMMaxSteps: defaultVMMaxSteps})
				if hasErrors(result.Diagnostics) {
					b.Fatalf("scaling project failed: %+v", result.Diagnostics)
				}
			}
		})
	}
}

func BenchmarkProjectMetadataVariants(b *testing.B) {
	workload := benchwork.Project("metadata", benchwork.ProjectSize{Classes: 64, MethodsPerClass: 3, ExpressionTerms: 8, ClassesPerUnit: 8})
	stable := benchmarkSourceUnits(workload.Units)
	renamed := append([]SourceUnit(nil), stable...)
	for index := range renamed {
		renamed[index].ID = "renamed-" + strconv.Itoa(index)
		renamed[index].Name = "renamed-" + strconv.Itoa(index) + ".cl"
	}
	reordered := append([]SourceUnit(nil), stable...)
	for left, right := 0, len(reordered)-1; left < right; left, right = left+1, right-1 {
		reordered[left], reordered[right] = reordered[right], reordered[left]
	}
	for _, variant := range []struct {
		name  string
		units []SourceUnit
	}{{"stable", stable}, {"renamed", renamed}, {"reordered", reordered}} {
		b.Run(variant.name, func(b *testing.B) {
			b.ReportMetric(float64(len(variant.units)), "source-units/op")
			b.ReportAllocs()
			for b.Loop() {
				result := CompileProject(variant.units, ProjectOptions{VMMaxSteps: defaultVMMaxSteps})
				if hasErrors(result.Diagnostics) {
					b.Fatalf("metadata variant failed: %+v", result.Diagnostics)
				}
			}
		})
	}
}

func BenchmarkVMInstructionDispatch(b *testing.B) {
	code := make([]byteInstruction, 0, 513)
	for range 256 {
		code = append(code, byteInstruction{Op: opPushInt, Span: emptySpan()}, byteInstruction{Op: opPop, Span: emptySpan()})
	}
	code = append(code, byteInstruction{Op: opPushInt, Span: emptySpan()}, byteInstruction{Op: opHalt, Span: emptySpan()})
	policy := DefaultVMPolicy()
	policy.MaxTraceEntries = 1
	policy.MaxTraceBytes = 1
	b.ReportMetric(float64(len(code)), "instructions/op")
	b.ReportAllocs()
	for b.Loop() {
		vm := policyVM(policy, code)
		vm.Run(len(code), nil)
		if !vm.halted {
			b.Fatal("dispatch benchmark did not halt")
		}
	}
}

func BenchmarkVMArithmeticLoop(b *testing.B) {
	benchmarkVMWorkload(b, benchwork.ArithmeticLoop(1_000))
}

func BenchmarkVMCallsAndRecursion(b *testing.B) {
	benchmarkVMWorkload(b, benchwork.RecursiveCalls(100))
}

func BenchmarkVMDispatch(b *testing.B) {
	b.Run("dynamic", func(b *testing.B) { benchmarkVMWorkload(b, benchwork.Dispatch(500, false)) })
	b.Run("static", func(b *testing.B) { benchmarkVMWorkload(b, benchwork.Dispatch(500, true)) })
}

func BenchmarkVMAllocationAndInitialization(b *testing.B) {
	benchmarkVMWorkload(b, benchwork.AllocationLoop(500))
}

func BenchmarkVMDeepCase(b *testing.B) {
	benchmarkVMWorkload(b, benchwork.DeepCase(200))
}

func BenchmarkVMStringBuiltins(b *testing.B) {
	benchmarkVMWorkload(b, benchwork.StringOperations(100))
}

func BenchmarkVMInputOutput(b *testing.B) {
	benchmarkVMWorkload(b, benchwork.IOOperations(100))
}

func BenchmarkVMTraceModes(b *testing.B) {
	workload := benchwork.ArithmeticLoop(200)
	for _, test := range []struct {
		name   string
		policy VMPolicy
	}{
		{"minimized", benchmarkTracePolicy(1, 1)},
		{"retained", benchmarkTracePolicy(5_000, 16<<20)},
	} {
		b.Run(test.name, func(b *testing.B) {
			benchmarkVMWorkloadWithPolicy(b, workload, test.policy)
		})
	}
}

func BenchmarkVMSnapshot(b *testing.B) {
	workload := benchwork.Project("snapshot", benchwork.ProjectSize{Classes: 32, MethodsPerClass: 4, ExpressionTerms: 8, ClassesPerUnit: 32})
	session := benchmarkDebugSession(b, workload, DefaultVMPolicy())
	session.Run(10, nil)
	for _, light := range []bool{false, true} {
		name := "full"
		if light {
			name = "light"
		}
		b.Run(name, func(b *testing.B) {
			snapshot := session.Snapshot(light, 0)
			encoded, err := json.Marshal(snapshot)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportMetric(float64(len(encoded)), "snapshot-bytes/op")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				benchmarkSnapshotSink = session.Snapshot(light, 0)
			}
		})
	}
}

func BenchmarkVMResetReplay(b *testing.B) {
	workload := benchwork.ArithmeticLoop(100)
	session := benchmarkDebugSession(b, workload, DefaultVMPolicy())
	session.Run(defaultVMMaxSteps, nil)
	b.Run("reset", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			benchmarkSnapshotSink = session.Reset()
		}
	})
	b.Run("reset-and-replay", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			session.Reset()
			benchmarkSnapshotSink = session.Run(defaultVMMaxSteps, nil)
		}
	})
}

func benchmarkVMWorkload(b *testing.B, workload benchwork.Workload) {
	benchmarkVMWorkloadWithPolicy(b, workload, DefaultVMPolicy())
}

func benchmarkVMWorkloadWithPolicy(b *testing.B, workload benchwork.Workload, policy VMPolicy) {
	session := benchmarkDebugSession(b, workload, policy)
	reportBenchmarkSource(b, workload.Source)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		session.Reset()
		snapshot := session.Run(policy.MaxInstructionsPerRun, nil)
		if !snapshot.Runtime.Halted || hasErrors(snapshot.Runtime.Diagnostics) {
			b.Fatalf("VM benchmark failed: %+v", snapshot.Runtime)
		}
	}
}

func benchmarkDebugSession(b testing.TB, workload benchwork.Workload, policy VMPolicy) *DebugSession {
	b.Helper()
	contract := ResultContract{CompilerVersion: "benchmark", Options: CompilationOptions{Execute: true, VMMaxSteps: policy.MaxInstructionsPerRun, InputProvided: workload.Input != "", VMPolicy: policy}}
	pipeline := NewCompilationSessionWithContract(workload.Source, contract)
	snapshot := pipeline.Complete()
	if hasErrors(snapshot.Result.Diagnostics) {
		b.Fatalf("benchmark workload failed compilation: %+v", snapshot.Result.Diagnostics)
	}
	session, ok := pipeline.DebugSession()
	if !ok {
		b.Fatal("benchmark debug session unavailable")
	}
	session.SetInput(workload.Input)
	return session
}

func benchmarkTracePolicy(entries, bytes int) VMPolicy {
	policy := DefaultVMPolicy()
	policy.MaxTraceEntries = entries
	policy.MaxTraceBytes = bytes
	return policy
}

func benchmarkParseProgram(b testing.TB, source string) *Program {
	b.Helper()
	tokens, diagnostics := Lex(source)
	if hasErrors(diagnostics) {
		b.Fatalf("benchmark lex failed: %+v", diagnostics)
	}
	program, diagnostics := Parse(tokens)
	if program == nil || hasErrors(diagnostics) {
		b.Fatalf("benchmark parse failed: %+v", diagnostics)
	}
	return program
}

func benchmarkAnalyzedProgram(b testing.TB, source string) (*Program, Analysis) {
	b.Helper()
	program := benchmarkParseProgram(b, source)
	analysis := Analyze(program)
	if hasErrors(analysis.Diagnostics) {
		b.Fatalf("benchmark analysis failed: %+v", analysis.Diagnostics)
	}
	return program, analysis
}

func benchmarkBuildClassTable(program *Program) *analyzer {
	analyzer := &analyzer{program: program, table: newClassTable()}
	analyzer.installBuiltinSymbols()
	analyzer.collectClasses()
	analyzer.validateInheritance()
	analyzer.collectFeatures()
	analyzer.validateFeatureInheritance()
	analyzer.validateMain()
	return analyzer
}

func benchmarkAnalysisResult(analyzer *analyzer) Analysis {
	return Analysis{Table: analyzer.table, Symbols: analyzer.symbols, Diagnostics: analyzer.diagnostics}
}

func benchmarkSourceUnits(units []benchwork.Unit) []SourceUnit {
	result := make([]SourceUnit, 0, len(units))
	for _, unit := range units {
		result = append(result, SourceUnit{ID: unit.ID, Name: unit.Name, Content: unit.Content})
	}
	return result
}

func reportBenchmarkSource(b *testing.B, source string) {
	b.Helper()
	b.SetBytes(int64(len(source)))
	b.ReportMetric(float64(len(source)), "source-bytes/op")
}

func benchmarkSizeName(size int) string {
	return "classes-" + strconv.Itoa(size)
}
