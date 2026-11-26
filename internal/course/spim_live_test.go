// SPDX-License-Identifier: AGPL-3.0-or-later

package course

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ThalesMMS/cooler-compiler/examples"
	"github.com/ThalesMMS/cooler-compiler/internal/compiler"
	"github.com/ThalesMMS/cooler-compiler/internal/mipssim"
	"github.com/ThalesMMS/cooler-compiler/internal/reftools"
)

func requireSPIM(t *testing.T) reftools.SPIMTools {
	t.Helper()
	tools := reftools.FindSPIM()
	if tools.SPIM == "" {
		if reftools.StrictReferenceTools() {
			t.Fatal("reference spim not found (strict mode); set COOLER_REFERENCE_RUNNER and COOLER_TRAP_HANDLER")
		}
		t.Skip("reference spim not found; real-SPIM comparison skipped")
	}
	return tools
}

// spimCorpus is the course-style corpus: the examples plus programs for
// every runtime error the Cool runtime reports.
func spimCorpus(t *testing.T) map[string][]compiler.SourceUnit {
	t.Helper()
	corpus := map[string][]compiler.SourceUnit{}
	catalog, err := examples.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, example := range catalog {
		if example.IntentionalError || len(example.Files) > 0 {
			continue
		}
		corpus[example.ID] = []compiler.SourceUnit{{ID: example.ID, Name: example.Path, Content: example.Source}}
	}
	for name, body := range map[string]string{
		"dispatch_void": "let a : A in a.f()",
		"case_void":     "let a : A in case a of x : Object => x; esac",
		"case_no_match": "case new A of x : Int => x; esac",
		"abort":         "abort()",
		"io":            "{ out_string(\"x=\"); out_int(~42); out_string(\"\\n\".concat(type_name())); }",
	} {
		source := "class A { f() : Int { 1 }; };\nclass Main inherits IO {\n  main() : Object { " + body + " };\n};\n"
		corpus[name] = []compiler.SourceUnit{{ID: name, Name: name + ".cl", Content: source}}
	}
	return corpus
}

// TestLiveSimulatorMatchesSPIM compares the embedded simulator with a
// user-supplied real SPIM and trap.handler on the corpus, under each
// collector configuration. It skips when spim is absent.
func TestLiveSimulatorMatchesSPIM(t *testing.T) {
	tools := requireSPIM(t)
	for name, units := range spimCorpus(t) {
		for label, gc := range map[string]compiler.MIPSGCOptions{"nogc": {}, "-g": {Generational: true}, "-g -t": {Generational: true, Test: true}} {
			t.Run(name+"/"+label, func(t *testing.T) {
				generated := compiler.GenerateMIPS(units, compiler.MIPSOptions{GC: gc})
				if !generated.OK {
					t.Fatalf("generation failed: %+v", generated.Diagnostics)
				}
				dir := t.TempDir()
				program := filepath.Join(dir, "program.s")
				if err := os.WriteFile(program, []byte(generated.Assembly), 0o600); err != nil {
					t.Fatal(err)
				}
				stdout, _ := runReferenceTool(t, tools.SPIM, dir, nil, tools.Args(program)...)
				want := reftools.NormalizeSPIMOutput(stdout)
				got := mipssim.Run(context.Background(), generated.Assembly, mipssim.Options{}).Output
				if got != want {
					t.Fatalf("simulator output differs from spim\n--- simulator\n%s\n--- spim\n%s", got, want)
				}
			})
		}
	}
}

func TestNormalizeSPIMOutput(t *testing.T) {
	raw := "SPIM Version 6.5 of January 4, 2003\nCopyright 1990-2003 by James R. Larus.\nAll Rights Reserved.\nSee the file README for a full copyright notice.\nLoaded: /var/tmp/cool/lib/trap.handler\nhello\nCOOL program successfully executed\n"
	if got := reftools.NormalizeSPIMOutput(raw); got != "hello\nCOOL program successfully executed\n" {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(strings.Join(reftools.SPIMTools{SPIM: "spim", TrapHandler: "/x/trap.handler"}.Args("p.s"), " "), "-exception_file /x/trap.handler -file p.s") {
		t.Fatal("trap.handler must be passed to spim")
	}
}

func TestBuildRunArtifact(t *testing.T) {
	sources := []compiler.SourceUnit{{ID: "a", Name: "a.cl", Content: "class Main inherits IO { main() : Object { out_string(in_string()) }; };"}}
	response, err := Build(Request{Sources: sources, Artifacts: []string{ArtifactRun}, Input: "hi\n", GC: compiler.MIPSGCOptions{Generational: true, Test: true}})
	if err != nil || response.Run == nil || response.Run.Status != mipssim.StatusHalted || response.Run.Output != "hi"+mipssim.SuccessMessage || response.Run.GC.Collections == 0 {
		t.Fatalf("run=%+v err=%v", response.Run, err)
	}
	waiting, _ := Build(Request{Sources: sources, Artifacts: []string{ArtifactRun}, Interactive: true})
	if waiting.Run.Status != mipssim.StatusWaitingInput {
		t.Fatalf("waiting=%+v", waiting.Run)
	}
	bad, _ := Build(Request{Sources: []compiler.SourceUnit{{ID: "b", Name: "b.cl", Content: "class Main { main() : Int { x }; };"}}, Artifacts: []string{ArtifactRun}})
	if bad.Run.Status != StatusCompileError || len(bad.Run.Diagnostics) == 0 {
		t.Fatalf("bad=%+v", bad.Run)
	}
}
