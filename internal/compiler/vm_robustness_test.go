// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestAdversarialVMLimitsHaltCleanly(t *testing.T) {
	tests := []struct {
		name   string
		source string
		input  string
		policy func() VMPolicy
		code   string
	}{
		{
			name: "deep recursion",
			source: `class Main {
  recurse() : Object { recurse() };
  main() : Object { recurse() };
};`,
			policy: func() VMPolicy { p := DefaultVMPolicy(); p.MaxCallFrameDepth = 4; return p },
			code:   "COL6024",
		},
		{
			name: "rapid allocation",
			source: `class Main {
  main() : Object {
    let i : Int <- 0 in
      while i < 10 loop { new Object; i <- i + 1; } pool
  };
};`,
			policy: func() VMPolicy { p := DefaultVMPolicy(); p.MaxObjectAllocations = 3; return p },
			code:   "COL6019",
		},
		{
			name:   "repeated concat",
			source: `class Main { main() : Object { "aa".concat("bb").concat("c") }; };`,
			policy: func() VMPolicy { p := DefaultVMPolicy(); p.MaxStringBytes = 4; return p },
			code:   "COL6051",
		},
		{
			name: "large output",
			source: `class Main inherits IO {
  main() : Object { { out_string("ab"); out_string("c"); } };
};`,
			policy: func() VMPolicy { p := DefaultVMPolicy(); p.MaxOutputBytes = 2; return p },
			code:   "COL6052",
		},
		{
			name: "nested scopes",
			source: `class Main {
  main() : Object { let a : Int <- 1 in let b : Int <- 2 in a + b };
};`,
			policy: func() VMPolicy { p := DefaultVMPolicy(); p.MaxScopeDepth = 2; return p },
			code:   "COL6050",
		},
		{
			name:   "large input line",
			source: `class Main inherits IO { main() : Object { in_string() }; };`,
			input:  "abc",
			policy: func() VMPolicy { p := DefaultVMPolicy(); p.MaxStringBytes = 2; return p },
			code:   "COL6051",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			session := robustnessDebugSession(t, test.source, test.input, test.policy())
			snapshot := session.Run(1_000, nil)
			if !snapshot.Runtime.Halted || snapshot.Runtime.Paused || !hasDiagnosticCode(snapshot.Runtime.Diagnostics, test.code) {
				t.Fatalf("limit did not halt cleanly: %+v", snapshot.Runtime)
			}
			before := snapshot.Runtime.ExecutedSteps
			after := session.Run(10, nil)
			if after.Runtime.ExecutedSteps != before || !reflect.DeepEqual(snapshot.Runtime.Diagnostics, after.Runtime.Diagnostics) {
				t.Fatalf("failed VM resumed or changed diagnostics: before=%+v after=%+v", snapshot.Runtime, after.Runtime)
			}
			assertVMInspectable(t, session.vm)
		})
	}
}

func TestInfiniteLoopUsesPerRunAndTotalInstructionLimits(t *testing.T) {
	policy := DefaultVMPolicy()
	policy.MaxInstructionsPerRun = 10
	policy.MaxInstructionsTotal = 20
	session := robustnessDebugSession(t, `class Main { main() : Object { while true loop 0 pool }; };`, "", policy)
	first := session.Run(100, nil)
	if !first.Runtime.Paused || first.Runtime.Halted || first.Runtime.ExecutedSteps != 10 {
		t.Fatalf("first bounded run = %+v", first.Runtime)
	}
	second := session.Run(100, nil)
	if !second.Runtime.Paused || second.Runtime.Halted || second.Runtime.ExecutedSteps != 20 {
		t.Fatalf("second bounded run = %+v", second.Runtime)
	}
	third := session.Run(1, nil)
	if !third.Runtime.Halted || !hasDiagnosticCode(third.Runtime.Diagnostics, "COL6002") || third.Runtime.ExecutedSteps != 20 {
		t.Fatalf("total instruction exhaustion = %+v", third.Runtime)
	}
	assertVMInspectable(t, session.vm)
}

func TestResetRerunAndBreakpointSequenceAreDeterministic(t *testing.T) {
	policy := DefaultVMPolicy()
	policy.MaxTraceEntries = 128
	source := `class Box { value : Int <- 7; get() : Int { value }; };
class Main inherits IO {
  main() : Object { let box : Box <- new Box in { out_int(box.get()); out_string(in_string()); } };
};`
	session := robustnessDebugSession(t, source, "same\n", policy)
	first := session.Run(1_000, nil)
	if !first.Runtime.Halted || len(first.Runtime.Diagnostics) != 0 {
		t.Fatalf("first run failed: %+v", first.Runtime)
	}
	for cycle := 0; cycle < 25; cycle++ {
		reset := session.Reset()
		if reset.Runtime.ExecutedSteps != 0 || reset.Runtime.Halted || reset.Runtime.Paused || len(reset.Runtime.Diagnostics) != 0 || session.vm.inputPos != 0 || session.vm.nextObjectID != 1 || session.vm.objectCount != 0 || session.vm.skipBreakpoint {
			t.Fatalf("cycle %d reset retained stale state: vm=%+v runtime=%+v", cycle, session.vm, reset.Runtime)
		}
		rerun := session.Run(1_000, nil)
		if !reflect.DeepEqual(first.Runtime, rerun.Runtime) {
			t.Fatalf("cycle %d rerun changed state\nfirst=%+v\nrerun=%+v", cycle, first.Runtime, rerun.Runtime)
		}
	}

	loop := robustnessDebugSession(t, `class Main {
  main() : Object {
    let i : Int <- 0 in
      while i < 2 loop
        i <- i + 1
      pool
  };
};`, "", policy)
	paused := loop.Run(100, []int{5})
	if !paused.Runtime.Paused || paused.Runtime.Halted {
		t.Fatalf("breakpoint did not pause: %+v", paused.Runtime)
	}
	loop.Reset()
	again := loop.Run(100, []int{5})
	if !reflect.DeepEqual(paused.Runtime, again.Runtime) {
		t.Fatalf("breakpoint sequence changed after reset: first=%+v second=%+v", paused.Runtime, again.Runtime)
	}
}

func TestTraceEvictionOffsetsAndLightDeltasAtEveryBoundary(t *testing.T) {
	policy := DefaultVMPolicy()
	policy.MaxInstructionsPerRun = 200
	policy.MaxInstructionsTotal = 200
	policy.MaxTraceEntries = 4
	policy.MaxTraceBytes = 1 << 20
	session := robustnessDebugSession(t, `class Main {
  main() : Object { let i : Int <- 0 in while i < 6 loop i <- i + 1 pool };
};`, "", policy)
	finished := session.Run(200, nil)
	if !finished.Runtime.Halted || session.vm.traceDropped == 0 || len(session.vm.trace) > policy.MaxTraceEntries {
		t.Fatalf("trace did not evict as expected: %+v", finished.Runtime)
	}
	dropped := session.vm.traceDropped
	end := dropped + len(session.vm.trace)
	boundaries := []int{dropped - 1, dropped, dropped + 1, end - 1, end, end + 1}
	seen := map[int]bool{}
	for _, from := range boundaries {
		if from < 0 || seen[from] {
			continue
		}
		seen[from] = true
		full := session.Snapshot(false, from).Runtime
		light := session.Snapshot(true, from).Runtime
		if !reflect.DeepEqual(full, light) {
			t.Fatalf("light snapshot diverged at traceFrom=%d", from)
		}
		wantOffset, wantLength := dropped, end-dropped
		if from > dropped && from < end {
			wantOffset, wantLength = from, end-from
		} else if from >= end {
			wantOffset, wantLength = end, 0
		}
		if full.TraceOffset != wantOffset || len(full.Trace) != wantLength {
			t.Fatalf("traceFrom=%d offset=%d len=%d want offset=%d len=%d", from, full.TraceOffset, len(full.Trace), wantOffset, wantLength)
		}
	}
}

func TestInputExhaustionAndRuntimeErrorResetRemainDeterministic(t *testing.T) {
	policy := DefaultVMPolicy()
	session := robustnessDebugSession(t, `class Main inherits IO {
  main() : Object { { out_string(in_string()); out_string("|"); out_string(in_string()); } };
};`, "only\n", policy)
	first := session.Run(1_000, nil)
	if outputText(first.Runtime.Output) != "only|" || len(first.Runtime.Diagnostics) != 0 {
		t.Fatalf("input exhaustion changed: %+v", first.Runtime)
	}
	session.Reset()
	second := session.Run(1_000, nil)
	if !reflect.DeepEqual(first.Runtime, second.Runtime) {
		t.Fatalf("input exhaustion changed after reset: first=%+v second=%+v", first.Runtime, second.Runtime)
	}

	failing := robustnessDebugSession(t, `class Main { main() : Object { 1 / 0 }; };`, "", policy)
	errorRun := failing.Run(1_000, nil)
	if !hasDiagnosticCode(errorRun.Runtime.Diagnostics, "COL6015") {
		t.Fatalf("expected division failure: %+v", errorRun.Runtime)
	}
	failing.Reset()
	errorAgain := failing.Run(1_000, nil)
	if !reflect.DeepEqual(errorRun.Runtime, errorAgain.Runtime) {
		t.Fatalf("runtime error changed after reset: first=%+v second=%+v", errorRun.Runtime, errorAgain.Runtime)
	}
}

func TestLongTraceStaysWithinEntryAndByteBounds(t *testing.T) {
	policy := DefaultVMPolicy()
	policy.MaxTraceEntries = 8
	policy.MaxTraceBytes = 2_048
	vm := &VM{policy: policy}
	for index := 0; index < 10_000; index++ {
		vm.executed = index
		vm.record("LONG", emptySpan(), "bounded")
		if len(vm.trace) > policy.MaxTraceEntries || vm.traceBytes > policy.MaxTraceBytes {
			t.Fatalf("trace escaped bounds at %d: entries=%d bytes=%d", index, len(vm.trace), vm.traceBytes)
		}
	}
	if vm.traceDropped == 0 || vm.traceDropped+len(vm.trace) != 10_000 {
		t.Fatalf("absolute trace accounting changed: dropped=%d retained=%d", vm.traceDropped, len(vm.trace))
	}
}

func BenchmarkVMTraceRetention(b *testing.B) {
	policy := DefaultVMPolicy()
	policy.MaxTraceEntries = 64
	policy.MaxTraceBytes = 64 << 10
	vm := &VM{policy: policy}
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		vm.executed = index
		vm.record("BENCH", emptySpan(), "bounded trace retention")
	}
}

func BenchmarkVMObjectAllocation(b *testing.B) {
	policy := MaximumVMPolicy()
	vm := &VM{
		policy: policy,
		exec:   &executable{Classes: map[string]*runtimeClass{"Box": {Name: "Box"}}},
	}
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		if vm.objectCount == policy.MaxObjectAllocations {
			vm.objectCount = 0
			vm.nextObjectID = 1
		}
		value, ok := vm.allocate("Box", emptySpan())
		if !ok || value.o == nil {
			b.Fatalf("bounded allocation failed at %d: %+v", index, vm.diagnostics)
		}
	}
}

func robustnessDebugSession(t testing.TB, source, input string, policy VMPolicy) *DebugSession {
	t.Helper()
	normalized, err := NormalizeVMPolicy(policy)
	if err != nil {
		t.Fatalf("invalid test policy: %v", err)
	}
	pipeline := NewCompilationSessionWithContract(source, ResultContract{
		CompilerVersion: "robustness-test",
		Options:         CompilationOptions{Execute: true, VMMaxSteps: normalized.MaxInstructionsPerRun, InputProvided: input != "", VMPolicy: normalized},
	})
	snapshot := pipeline.Complete()
	if hasErrors(snapshot.Result.Diagnostics) {
		t.Fatalf("robustness source failed compilation: %+v", snapshot.Result.Diagnostics)
	}
	session, ok := pipeline.DebugSession()
	if !ok || session.vm == nil {
		t.Fatal("robustness source did not produce a debug VM")
	}
	session.SetInput(input)
	return session
}

func assertVMInspectable(t testing.TB, vm *VM) {
	t.Helper()
	if vm == nil {
		t.Fatal("VM is nil")
	}
	if len(vm.trace) != len(vm.traceSizes) || len(vm.trace) > vm.policy.MaxTraceEntries || vm.traceBytes > vm.policy.MaxTraceBytes {
		t.Fatalf("invalid retained trace accounting: entries=%d sizes=%d bytes=%d", len(vm.trace), len(vm.traceSizes), vm.traceBytes)
	}
	traceBytes := 0
	for _, size := range vm.traceSizes {
		if size < 0 {
			t.Fatalf("negative retained trace size: %d", size)
		}
		traceBytes = addApproximateBytes(traceBytes, size)
	}
	if traceBytes != vm.traceBytes {
		t.Fatalf("trace byte accounting mismatch: got=%d want=%d", vm.traceBytes, traceBytes)
	}
	if vm.outputBytes != len(strings.Join(vm.output, "")) || vm.outputBytes > vm.policy.MaxOutputBytes {
		t.Fatalf("invalid output accounting: fragments=%q bytes=%d", vm.output, vm.outputBytes)
	}
	if len(vm.stack) > vm.policy.MaxOperandStackDepth || len(vm.frames) > vm.policy.MaxCallFrameDepth || vm.objectCount > vm.policy.MaxObjectAllocations {
		t.Fatalf("VM escaped policy: stack=%d frames=%d objects=%d", len(vm.stack), len(vm.frames), vm.objectCount)
	}
	for index, frame := range vm.frames {
		if frame == nil || frame.method == nil || frame.stackBase < 0 || frame.stackBase > len(vm.stack) || len(frame.scopes) > vm.policy.MaxScopeDepth {
			t.Fatalf("invalid frame %d after terminal failure: %+v", index, frame)
		}
	}
	if _, err := json.Marshal(vm.result(0)); err != nil {
		t.Fatalf("terminal VM snapshot is not JSON-exportable: %v", err)
	}
}
