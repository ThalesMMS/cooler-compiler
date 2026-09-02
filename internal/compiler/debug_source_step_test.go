// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"context"
	"errors"
	"testing"
)

const sourceStepProgram = `class Helper {
  twice(v : Int) : Int { v + v };
};

class Main inherits IO {
  value : Int <- 1;
  main() : Object {{
    out_int((new Helper).twice(value));
    out_string("\n");
  }};
};`

func TestSourceStepIntoOverOutAndBuiltinAtomicity(t *testing.T) {
	t.Run("into enters initializers and user calls", func(t *testing.T) {
		session := NewDebugSession(sourceStepProgram)
		initializer := session.StepInto(100_000, nil)
		if initializer.StopReason != StopReasonStepInto || initializer.Location == nil || initializer.Location.Method != "Main.$init" || initializer.Location.Synthetic {
			t.Fatalf("initializer stop = %+v", initializer)
		}
		call := advanceToLocation(t, session, "Main.main", 2)
		if call.Location.FrameDepth != 1 {
			t.Fatalf("call location = %+v", call.Location)
		}
		inside := session.StepInto(100_000, nil)
		if inside.Location == nil || inside.Location.Method != "Helper.twice" || inside.Location.FrameDepth != 2 {
			t.Fatalf("step into call = %+v", inside)
		}
	})

	t.Run("over skips deeper frame and builtins stay atomic", func(t *testing.T) {
		session := NewDebugSession(sourceStepProgram)
		advanceToLocation(t, session, "Main.main", 2)
		over := session.StepOver(100_000, nil)
		if over.StopReason != StopReasonStepOver || over.Location == nil || over.Location.Method != "Main.main" || over.Location.Instruction != 3 || over.Location.FrameDepth != 1 {
			t.Fatalf("step over call = %+v", over)
		}
		builtin := session.StepInto(100_000, nil)
		if builtin.Location == nil || builtin.Location.Method != "Main.main" || builtin.Location.Span.StartLine != 9 {
			t.Fatalf("builtin was not atomic = %+v", builtin)
		}
	})

	t.Run("out returns to caller", func(t *testing.T) {
		session := NewDebugSession(sourceStepProgram)
		advanceToLocation(t, session, "Helper.twice", 0)
		out := session.StepOut(100_000, nil)
		if out.StopReason != StopReasonStepOut || out.Location == nil || out.Location.Method != "Main.main" || out.Location.FrameDepth != 1 {
			t.Fatalf("step out = %+v", out)
		}
	})
}

func TestSourceCommandsRespectBreakpointInputErrorAndLimitPrecedence(t *testing.T) {
	session := NewDebugSession(sourceStepProgram)
	advanceToLocation(t, session, "Main.main", 3)
	stopped := session.StepOver(100_000, []Breakpoint{{SourceID: "source-1", Line: 8}})
	if stopped.StopReason != StopReasonBreakpoint || stopped.Location == nil || stopped.Location.Span.StartLine != 8 {
		t.Fatalf("breakpoint precedence = %+v", stopped)
	}

	input := NewDebugSession(interactiveInputProgram)
	input.SetInteractiveInput(true)
	for index := 0; index < 20; index++ {
		waiting := input.StepInto(100_000, nil)
		if waiting.Runtime.WaitingInput != nil {
			if waiting.StopReason != StopReasonWaitingInput {
				t.Fatalf("input stop reason = %+v", waiting)
			}
			break
		}
		if index == 19 {
			t.Fatal("source stepping never reached input wait")
		}
	}

	runtimeError := NewDebugSession(`class Main { main() : Object { 1 / 0 }; };`)
	for !runtimeError.Snapshot(false, 0).Runtime.Halted {
		runtimeError.StepInto(100_000, nil)
	}
	if got := runtimeError.Snapshot(false, 0).StopReason; got != StopReasonRuntimeError {
		t.Fatalf("runtime error stop reason = %q", got)
	}

	limited := NewDebugSession(sourceStepProgram).StepInto(1, nil)
	if limited.StopReason != StopReasonInstructionLimit || !limited.Runtime.Paused {
		t.Fatalf("instruction limit = %+v", limited)
	}
}

func TestSourceCommandCancellationIsTerminalAndExplicit(t *testing.T) {
	session := NewDebugSession(sourceStepProgram)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	snapshot := session.StepOutContext(ctx, 100_000, nil)
	if snapshot.StopReason != StopReasonCancelled || !snapshot.Runtime.Halted || len(snapshot.Runtime.Diagnostics) == 0 || snapshot.Runtime.Diagnostics[0].Code != "COL6056" {
		t.Fatalf("cancelled source command = %+v", snapshot)
	}
}

func TestRunToCursorIsSourceUnitAwareAndRejectsNonExecutableLines(t *testing.T) {
	units := []SourceUnit{
		{ID: "helper", Name: "helper.cl", Content: `class Helper { value() : Int { 7 }; };`},
		{ID: "main", Name: "main.cl", Content: "class Main inherits IO {\n  main() : Object {{\n    out_int((new Helper).value());\n    out_string(\"done\\n\");\n  }};\n};"},
	}
	pipeline := NewProjectCompilationSession(units, ResultContract{Options: CompilationOptions{Execute: true, VMMaxSteps: 100_000}})
	pipeline.Complete()
	session, ok := pipeline.DebugSession()
	if !ok {
		t.Fatal("missing project debug session")
	}
	stopped, err := session.RunToCursor(100_000, nil, SourcePosition{SourceID: "main", Line: 4})
	if err != nil || stopped.StopReason != StopReasonRunToCursor || stopped.Location == nil || stopped.Location.SourceID != "main" || stopped.Location.Span.StartLine != 4 {
		t.Fatalf("run to cursor = %+v, %v", stopped, err)
	}
	if _, err := session.RunToCursor(100_000, nil, SourcePosition{SourceID: "main", Line: 6}); !errors.Is(err, ErrNoExecutableSourcePosition) {
		t.Fatalf("non-executable cursor error = %v", err)
	}
	if _, err := session.RunToCursor(100_000, nil, SourcePosition{SourceID: "missing", Line: 1}); !errors.Is(err, ErrNoExecutableSourcePosition) {
		t.Fatalf("unknown source cursor error = %v", err)
	}
}

func TestSourceSteppingCoversRecursionCaseAndLoopWithoutSyntheticStops(t *testing.T) {
	program := `class Main inherits IO {
  descend(n : Int) : Int { if n = 0 then 0 else descend(n - 1) fi };
  main() : Object {
    let x : Int <- case descend(2) of value : Int => value; esac in {
      while x < 2 loop x <- x + 1 pool;
      out_int(x);
    }
  };
};`
	session := NewDebugSession(program)
	firstRecursiveDepth := -1
	sawDeeperRecursion := false
	sawCase := false
	sawLoop := false
	for index := 0; index < 200; index++ {
		snapshot := session.StepInto(100_000, nil)
		if snapshot.Location != nil {
			if snapshot.Location.Synthetic {
				t.Fatalf("source command stopped at synthetic location: %+v", snapshot.Location)
			}
			if snapshot.Location.Method == "Main.descend" {
				if firstRecursiveDepth < 0 {
					firstRecursiveDepth = snapshot.Location.FrameDepth
				} else if snapshot.Location.FrameDepth > firstRecursiveDepth {
					sawDeeperRecursion = true
				}
			}
			if snapshot.Location.Span.StartLine == 4 {
				sawCase = true
			}
			if snapshot.Location.Span.StartLine == 5 {
				sawLoop = true
			}
		}
		if snapshot.Runtime.Halted {
			if snapshot.StopReason != StopReasonHalt {
				t.Fatalf("terminal stop reason = %q", snapshot.StopReason)
			}
			break
		}
		if index == 199 {
			t.Fatal("source stepping did not halt")
		}
	}
	if !sawDeeperRecursion || !sawCase || !sawLoop {
		t.Fatalf("coverage recursion=%v case=%v loop=%v", sawDeeperRecursion, sawCase, sawLoop)
	}
}

func advanceToLocation(t *testing.T, session *DebugSession, method string, instruction int) DebugSnapshot {
	t.Helper()
	for index := 0; index < 100; index++ {
		snapshot := session.StepInto(100_000, nil)
		if snapshot.Location != nil && snapshot.Location.Method == method && snapshot.Location.Instruction == instruction {
			return snapshot
		}
		if snapshot.Runtime.Halted {
			break
		}
	}
	t.Fatalf("did not reach %s instruction %d", method, instruction)
	return DebugSnapshot{}
}
