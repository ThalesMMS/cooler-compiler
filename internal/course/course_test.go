// SPDX-License-Identifier: AGPL-3.0-or-later

package course

import (
	"strings"
	"testing"

	"github.com/ThalesMMS/cooler-compiler/internal/compiler"
)

func TestBuildTokens(t *testing.T) {
	response, err := Build(Request{
		Sources:   []compiler.SourceUnit{{ID: "a", Name: "a.cl", Content: "class A {};"}},
		Artifacts: []string{ArtifactTokens},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Tokens == nil || !strings.HasPrefix(response.Tokens.Text, "#name \"a.cl\"\n#1 CLASS\n") {
		t.Fatalf("unexpected tokens: %+v", response.Tokens)
	}
}

func TestBuildRejectsUnknownArtifacts(t *testing.T) {
	if _, err := Build(Request{Sources: []compiler.SourceUnit{{ID: "a", Name: "a.cl"}}, Artifacts: []string{"nope"}}); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := Build(Request{}); err == nil {
		t.Fatal("expected an error for an empty request")
	}
}

func TestBuildHonorsFrontend(t *testing.T) {
	sources := []compiler.SourceUnit{{ID: "a", Name: "a.cl", Content: "class Main { s : String <- \"x\ny\"; main() : Int { _z }; };"}}
	all := []string{ArtifactTokens, ArtifactAST, ArtifactSemant, ArtifactDiagnostics}
	handwritten, err := Build(Request{Sources: sources, Artifacts: all})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := Build(Request{Sources: sources, Artifacts: all, Frontend: "generated"})
	if err != nil {
		t.Fatal(err)
	}
	if *handwritten.Tokens != *generated.Tokens || *handwritten.AST != *generated.AST || *handwritten.Diagnostics != *generated.Diagnostics {
		t.Fatalf("front-ends disagree:\n%+v\n%+v", handwritten, generated)
	}
	if _, err := Build(Request{Sources: sources, Artifacts: all, Frontend: "flex"}); err == nil {
		t.Fatal("expected an invalid front-end error")
	}
}

func TestBuildMIPS(t *testing.T) {
	sources := []compiler.SourceUnit{{ID: "a", Name: "a.cl", Content: "class Main { x : Int <- 1; main() : Object { x <- 2 }; };"}}
	response, err := Build(Request{Sources: sources, Artifacts: []string{ArtifactMIPS}, GC: compiler.MIPSGCOptions{Generational: true}})
	if err != nil || response.MIPS == nil || !response.MIPS.OK || !strings.Contains(response.MIPS.Assembly, "_GenGC_Assign") {
		t.Fatalf("response=%+v err=%v", response.MIPS, err)
	}
}
