// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"reflect"
	"testing"
)

const pipelineProgram = `class Main inherits IO {
  main(): Object { out_int(1) };
};`

func TestCompilationSessionAdvancesOnePhaseAtATime(t *testing.T) {
	session := NewCompilationSession(pipelineProgram)
	initial := session.Snapshot()
	if initial.CurrentPhase != PhaseInitial || initial.NextPhase != PhaseLexer || initial.Complete || initial.Blocked {
		t.Fatalf("unexpected initial snapshot: %+v", initial)
	}
	assertNoPipelineArtifactsAfter(t, initial, PhaseInitial)
	assertPipelineStage(t, initial, "lexer", "ready")
	assertPipelineStage(t, initial, "parser", "waiting")

	want := []CompilationPhase{
		PhaseLexer,
		PhaseParser,
		PhaseSemantic,
		PhaseIR,
		PhaseCodegen,
		PhaseRuntime,
	}
	for index, phase := range want {
		snapshot := session.NextPhase()
		if snapshot.CurrentPhase != phase {
			t.Fatalf("click %d: got %q, want %q", index+1, snapshot.CurrentPhase, phase)
		}
		assertNoPipelineArtifactsAfter(t, snapshot, phase)
	}

	finished := session.Snapshot()
	if !finished.Complete || finished.Blocked || finished.NextPhase != PhaseInitial {
		t.Fatalf("unexpected terminal snapshot: %+v", finished)
	}
	if finished.Result.Runtime.ExecutedSteps != 0 || len(finished.Result.Runtime.Frames) == 0 {
		t.Fatalf("runtime was not prepared and stopped: %+v", finished.Result.Runtime)
	}
	assertPipelineStage(t, finished, "runtime", "ok")

	repeated := session.NextPhase()
	if !reflect.DeepEqual(finished, repeated) {
		t.Fatalf("terminal NextPhase mutated the snapshot\nfinished=%+v\nrepeated=%+v", finished, repeated)
	}
}

func TestCompilationSessionCompleteMatchesSixClicks(t *testing.T) {
	incremental := NewCompilationSession(pipelineProgram)
	for range 6 {
		incremental.NextPhase()
	}

	complete := NewCompilationSession(pipelineProgram)
	complete.NextPhase()
	completed := complete.Complete()
	if !completed.Complete || completed.CurrentPhase != PhaseRuntime {
		t.Fatalf("complete did not reach runtime: %+v", completed)
	}
	if !reflect.DeepEqual(incremental.Snapshot().Result, completed.Result) {
		t.Fatal("Complete and six NextPhase calls produced different results")
	}
}

func TestNewDebugSessionPreservesCompletedPipelineStages(t *testing.T) {
	session := NewDebugSession(pipelineProgram)
	snapshot := session.Snapshot(false, 0)
	if snapshot.Runtime.ExecutedSteps != 0 {
		t.Fatalf("new debug session executed %d instructions, want 0", snapshot.Runtime.ExecutedSteps)
	}
	for _, stage := range snapshot.Result.Stages {
		if stage.Status != "ok" {
			t.Fatalf("completed stage %s has status %q, want ok", stage.ID, stage.Status)
		}
	}
}

func TestCompileRetainsCompletedPipelineStaticArtifacts(t *testing.T) {
	pipeline := NewCompilationSession(pipelineProgram)
	want := pipeline.Complete().Result
	got := Compile(pipelineProgram)

	if !reflect.DeepEqual(got.Tokens, want.Tokens) ||
		!reflect.DeepEqual(got.AST, want.AST) ||
		!reflect.DeepEqual(got.Symbols, want.Symbols) ||
		!reflect.DeepEqual(got.IR, want.IR) ||
		!reflect.DeepEqual(got.Assembly, want.Assembly) ||
		!reflect.DeepEqual(got.ControlFlow, want.ControlFlow) ||
		!reflect.DeepEqual(got.Metrics, want.Metrics) {
		t.Fatal("Compile and CompilationSession produced different static artifacts")
	}
}

func TestLexerDiagnosticsStillAllowParser(t *testing.T) {
	session := NewCompilationSession(`class Main {
  main(): Object { # 0 };
};`)
	lexed := session.NextPhase()
	if !hasStageErrors(lexed.Result.Diagnostics, "lexer") {
		t.Fatal("expected lexer diagnostic")
	}
	if lexed.Blocked || lexed.NextPhase != PhaseParser {
		t.Fatalf("lexer diagnostics blocked parser early: %+v", lexed)
	}

	parsed := session.NextPhase()
	if parsed.CurrentPhase != PhaseParser {
		t.Fatalf("got %q, want parser", parsed.CurrentPhase)
	}
	if !parsed.Blocked || parsed.NextPhase != PhaseInitial {
		t.Fatalf("frontend diagnostics should block semantic: %+v", parsed)
	}
	assertPipelineStage(t, parsed, "lexer", "error")
	assertPipelineStage(t, parsed, "semantic", "blocked")

	repeated := session.NextPhase()
	if !reflect.DeepEqual(parsed, repeated) {
		t.Fatal("blocked pipeline changed after another NextPhase")
	}
}

func TestParserRetainsPartialASTBeforeBlocking(t *testing.T) {
	session := NewCompilationSession(`class Good {};
class Main {
  main(): Object { 0 };
  broken: ;
};`)
	session.NextPhase()
	parsed := session.NextPhase()
	if !hasStageErrors(parsed.Result.Diagnostics, "parser") || !parsed.Blocked {
		t.Fatalf("expected parser barrier: %+v", parsed)
	}
	if parsed.Result.AST == nil {
		t.Fatal("parser discarded its partial AST")
	}
}

func TestSemanticDiagnosticsBlockBackend(t *testing.T) {
	session := NewCompilationSession(`class Main {
  main(): MissingType { 0 };
};`)
	snapshot := session.Complete()
	if snapshot.CurrentPhase != PhaseSemantic || !snapshot.Blocked || snapshot.Complete {
		t.Fatalf("semantic barrier not enforced: %+v", snapshot)
	}
	if len(snapshot.Result.IR) != 0 || len(snapshot.Result.Assembly) != 0 || len(snapshot.Result.ControlFlow.Blocks) != 0 {
		t.Fatal("backend artifacts appeared after semantic failure")
	}
	assertPipelineStage(t, snapshot, "semantic", "error")
	assertPipelineStage(t, snapshot, "ir", "blocked")
	assertPipelineStage(t, snapshot, "runtime", "blocked")

	debug, ok := session.DebugSession()
	if !ok || debug == nil || debug.Snapshot(false, 0).Status != "blocked" {
		t.Fatal("blocked terminal pipeline did not expose its diagnostic debug snapshot")
	}
}

func assertNoPipelineArtifactsAfter(t *testing.T, snapshot PipelineSnapshot, phase CompilationPhase) {
	t.Helper()
	order := map[CompilationPhase]int{
		PhaseInitial:  0,
		PhaseLexer:    1,
		PhaseParser:   2,
		PhaseSemantic: 3,
		PhaseIR:       4,
		PhaseCodegen:  5,
		PhaseRuntime:  6,
	}
	current := order[phase]
	if current < 1 && len(snapshot.Result.Tokens) != 0 {
		t.Fatal("tokens appeared before Lexer")
	}
	if current < 2 && snapshot.Result.AST != nil {
		t.Fatal("AST appeared before Parser")
	}
	if current < 3 && len(snapshot.Result.Symbols) != 0 {
		t.Fatal("symbols appeared before Semantic")
	}
	if current < 4 && (len(snapshot.Result.IR) != 0 || len(snapshot.Result.ControlFlow.Blocks) != 0) {
		t.Fatal("IR appeared before Stack IR")
	}
	if current < 5 && len(snapshot.Result.Assembly) != 0 {
		t.Fatal("assembly appeared before Pseudo-assembly")
	}
	if current < 6 && len(snapshot.Result.Runtime.Frames) != 0 {
		t.Fatal("VM appeared before Educational VM")
	}
}

func assertPipelineStage(t *testing.T, snapshot PipelineSnapshot, id, wantStatus string) {
	t.Helper()
	for _, stage := range snapshot.Result.Stages {
		if stage.ID == id {
			if stage.Status != wantStatus {
				t.Fatalf("stage %s status = %q, want %q", id, stage.Status, wantStatus)
			}
			return
		}
	}
	t.Fatalf("missing stage %s", id)
}
