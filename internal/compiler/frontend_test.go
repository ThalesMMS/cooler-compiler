// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGeneratedFrontendCompilesAndRunsLikeDefault(t *testing.T) {
	source := "class Main inherits IO {\n  main() : Object { let x : Int <- 6 in out_int(x * 7) };\n};\n"
	run := func(frontend string) Result {
		units := []SourceUnit{{ID: "u", Name: "m.cl", Content: source}}
		pipeline := NewProjectCompilationSession(units, ResultContract{Options: CompilationOptions{Execute: true, VMMaxSteps: 10000, Frontend: frontend}})
		pipeline.Complete()
		debug, ok := pipeline.DebugSession()
		if !ok {
			t.Fatal("no debug session")
		}
		return debug.Run(10000, nil).Result
	}
	handwritten, generated := run(""), run(FrontendGenerated)
	if strings.Join(generated.Runtime.Output, "") != "42" || hasErrors(generated.Diagnostics) {
		t.Fatalf("generated front-end output %q diagnostics %+v", generated.Runtime.Output, generated.Diagnostics)
	}
	a, _ := json.Marshal(handwritten.AST)
	b, _ := json.Marshal(generated.AST)
	if string(a) != string(b) {
		t.Fatal("ASTs differ between front-ends")
	}
	if generated.Options.Frontend != FrontendGenerated || handwritten.Options.Frontend != "" {
		t.Fatalf("report options: %q / %q", handwritten.Options.Frontend, generated.Options.Frontend)
	}
}

func TestGeneratedFrontendReportsSyntaxErrors(t *testing.T) {
	units := []SourceUnit{{ID: "u", Name: "m.cl", Content: "class Main { main() : Int { 1 + }; };"}}
	pipeline := NewProjectCompilationSession(units, ResultContract{Options: CompilationOptions{Frontend: FrontendGenerated}})
	snapshot := pipeline.Complete()
	if !snapshot.Blocked || len(snapshot.Result.Diagnostics) == 0 || snapshot.Result.Diagnostics[0].Code != "COL2200" {
		t.Fatalf("diagnostics %+v", snapshot.Result.Diagnostics)
	}
	if !strings.Contains(snapshot.Result.Diagnostics[0].Message, "syntax error at or near '}'") {
		t.Fatalf("message %q", snapshot.Result.Diagnostics[0].Message)
	}
}

func TestInvalidFrontendIsRejected(t *testing.T) {
	pipeline := NewProjectCompilationSession([]SourceUnit{{ID: "u", Name: "m.cl", Content: "class Main {};"}}, ResultContract{Options: CompilationOptions{Frontend: "flex"}})
	snapshot := pipeline.Complete()
	if !snapshot.Blocked || snapshot.Result.Diagnostics[0].Code != "COL0008" {
		t.Fatalf("diagnostics %+v", snapshot.Result.Diagnostics)
	}
	if value, err := NormalizeFrontend(" Handwritten "); err != nil || value != "" {
		t.Fatalf("normalize: %q %v", value, err)
	}
}
