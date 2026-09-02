// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"errors"
	"strings"
	"testing"
)

const interactiveInputProgram = `class Main inherits IO {
  main() : Object {{
    out_string("name> ");
    out_string(in_string());
    out_string(" number> ");
    out_int(in_int());
  }};
};`

func TestInteractiveInputWaitsWithoutConsumingOpcodeAndResumesOnce(t *testing.T) {
	session := NewDebugSession(interactiveInputProgram)
	session.SetInteractiveInput(true)
	waiting := session.Run(100_000, nil)
	request := waiting.Runtime.WaitingInput
	if waiting.Status != "waiting-input" || request == nil || request.Builtin != "in_string" {
		t.Fatalf("first wait = %+v", waiting)
	}
	if strings.Join(waiting.Runtime.Output, "") != "name> " || strings.Join(request.Output, "") != "name> " {
		t.Fatalf("prompt metadata = output=%q request=%q", strings.Join(waiting.Runtime.Output, ""), strings.Join(request.Output, ""))
	}
	executed, ip := waiting.Runtime.ExecutedSteps, waiting.Runtime.Frames[len(waiting.Runtime.Frames)-1].IP
	repeated := session.Run(100_000, nil)
	if repeated.Runtime.ExecutedSteps != executed || repeated.Runtime.Frames[len(repeated.Runtime.Frames)-1].IP != ip {
		t.Fatalf("waiting opcode advanced: before=%d/%d after=%d/%d", executed, ip, repeated.Runtime.ExecutedSteps, repeated.Runtime.Frames[len(repeated.Runtime.Frames)-1].IP)
	}
	if _, err := session.ProvideInputLine("stale", request.Sequence+1); !errors.Is(err, ErrInputSequence) {
		t.Fatalf("stale sequence error = %v", err)
	}
	accepted, err := session.ProvideInputLine("Ada", request.Sequence)
	if err != nil || accepted.Runtime.WaitingInput != nil || accepted.Runtime.ExecutedSteps != executed+1 {
		t.Fatalf("accepted input = %+v, %v", accepted, err)
	}
	if _, err := session.ProvideInputLine("duplicate", request.Sequence); !errors.Is(err, ErrInputNotWaiting) {
		t.Fatalf("duplicate input error = %v", err)
	}

	second := session.Run(100_000, nil)
	if second.Runtime.WaitingInput == nil || second.Runtime.WaitingInput.Builtin != "in_int" || strings.Join(second.Runtime.Output, "") != "name> Ada number> " {
		t.Fatalf("second wait = %+v", second)
	}
	if _, err := session.ProvideInputLine("not-an-int", second.Runtime.WaitingInput.Sequence); err != nil {
		t.Fatal(err)
	}
	completed := session.Run(100_000, nil)
	if !completed.Runtime.Halted || strings.Join(completed.Runtime.Output, "") != "name> Ada number> 0" {
		t.Fatalf("completed runtime = %+v", completed.Runtime)
	}
	events := session.InputEvents(true)
	if len(events) != 2 || events[0].Builtin != "in_string" || events[0].Line != "Ada" || !events[0].Live || events[1].Builtin != "in_int" {
		t.Fatalf("input events = %+v", events)
	}
	redacted := session.InputEvents(false)
	if redacted[0].Line != "" || redacted[1].Line != "" {
		t.Fatalf("redacted events leaked content: %+v", redacted)
	}
}

func TestInteractiveInputConsumesPreloadedLinesBeforeWaitingAndResetRestoresOnlyInitialInput(t *testing.T) {
	session := NewDebugSession(interactiveInputProgram)
	session.SetInput("preloaded\n")
	session.SetInteractiveInput(true)
	waiting := session.Run(100_000, nil)
	if waiting.Runtime.WaitingInput == nil || waiting.Runtime.WaitingInput.Builtin != "in_int" || waiting.Runtime.WaitingInput.InputPosition != 1 {
		t.Fatalf("wait after preloaded input = %+v", waiting.Runtime)
	}
	if _, err := session.ProvideInputLine("7", waiting.Runtime.WaitingInput.Sequence); err != nil {
		t.Fatal(err)
	}
	if completed := session.Run(100_000, nil); !completed.Runtime.Halted {
		t.Fatalf("live run did not halt: %+v", completed.Runtime)
	}
	reset := session.Reset()
	if reset.Runtime.InputEvents != 0 || reset.Runtime.WaitingInput != nil {
		t.Fatalf("reset retained live history: %+v", reset.Runtime)
	}
	waiting = session.Run(100_000, nil)
	if waiting.Runtime.WaitingInput == nil || strings.Join(waiting.Runtime.Output, "") != "name> preloaded number> " {
		t.Fatalf("reset did not restore configured input: %+v", waiting.Runtime)
	}
}

func TestInteractiveEOFFallsBackAndLiveInputIsBounded(t *testing.T) {
	policy := DefaultVMPolicy()
	policy.MaxInputBytes = 3
	contract := ResultContract{Options: CompilationOptions{Execute: true, VMMaxSteps: 100_000, VMPolicy: policy}}
	pipeline := NewCompilationSessionWithContract(interactiveInputProgram, contract)
	pipeline.Complete()
	session, ok := pipeline.DebugSession()
	if !ok {
		t.Fatal("missing debug session")
	}
	session.SetInteractiveInput(true)
	waiting := session.Run(100_000, nil)
	if _, err := session.ProvideInputLine("four", waiting.Runtime.WaitingInput.Sequence); !errors.Is(err, ErrInputLimit) {
		t.Fatalf("oversized line error = %v", err)
	}
	if snapshot := session.Snapshot(false, 0); snapshot.Runtime.WaitingInput == nil {
		t.Fatal("rejected line consumed the pending request")
	}
	session.FinishInteractiveInput()
	completed := session.Run(100_000, nil)
	if !completed.Runtime.Halted || strings.Join(completed.Runtime.Output, "") != "name>  number> 0" {
		t.Fatalf("EOF fallback = %+v", completed.Runtime)
	}
}
