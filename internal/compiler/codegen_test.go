// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"reflect"
	"testing"
)

const codegenTestProgram = `class Main inherits IO {
  main(): Object { out_int(1) };
};`

func parseAndAnalyzeForCodegenTest(t *testing.T, source string) (*Program, Analysis) {
	t.Helper()
	tokens, lexerDiagnostics := Lex(source)
	program, parserDiagnostics := Parse(tokens)
	diagnostics := append(append([]Diagnostic(nil), lexerDiagnostics...), parserDiagnostics...)
	if program == nil || hasErrors(diagnostics) {
		t.Fatalf("test source did not parse: %v", diagnostics)
	}
	analysis := Analyze(program)
	if hasErrors(analysis.Diagnostics) {
		t.Fatalf("test source did not analyze: %v", analysis.Diagnostics)
	}
	return program, analysis
}

func TestMaterializeIRDoesNotCreateAssembly(t *testing.T) {
	program, analysis := parseAndAnalyzeForCodegenTest(t, codegenTestProgram)

	exec := generateExecutable(program, analysis)
	if len(exec.IR) != 0 || len(exec.Assembly) != 0 || len(exec.ControlFlow.Blocks) != 0 || len(exec.ControlFlow.Edges) != 0 {
		t.Fatal("executable contains formatted artifacts before materialization")
	}

	materializeIR(exec)
	if len(exec.IR) == 0 || len(exec.ControlFlow.Blocks) == 0 {
		t.Fatal("Stack IR phase did not materialize IR and control flow")
	}
	if len(exec.Assembly) != 0 {
		t.Fatal("Stack IR phase materialized assembly early")
	}
}

func TestMaterializeAssemblyReusesExistingBytecode(t *testing.T) {
	program, analysis := parseAndAnalyzeForCodegenTest(t, codegenTestProgram)
	exec := generateExecutable(program, analysis)
	materializeIR(exec)
	wantIR := append([]IRInstruction(nil), exec.IR...)

	materializeAssembly(exec)
	if len(exec.Assembly) == 0 {
		t.Fatal("pseudo-assembly phase produced no assembly")
	}
	if !reflect.DeepEqual(wantIR, exec.IR) {
		t.Fatal("pseudo-assembly changed IR")
	}
}
