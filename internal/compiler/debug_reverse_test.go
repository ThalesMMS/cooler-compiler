// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"context"
	"errors"
	"math/rand"
	"reflect"
	"testing"
)

func TestSeekRetainedHistoryRoundTripAndCancellation(t *testing.T) {
	s := NewDebugSession(reverseObjectProgram)
	original := map[int]string{}
	for i := 0; i < 90; i++ {
		snap := s.StepInstruction()
		original[snap.Timeline.CurrentStep] = snap.Timeline.StateHash
		if snap.Runtime.Halted {
			break
		}
	}
	latest := s.ReverseTimeline()
	// Populate the subject in one run, so navigation must replay between sparse
	// checkpoints instead of merely cloning our per-instruction reference stops.
	s = NewDebugSession(reverseObjectProgram)
	s.RunWithBreakpoints(latest.CurrentStep, nil)
	latest = s.ReverseTimeline()
	for _, target := range []int{latest.CurrentStep - 5, latest.CurrentStep, latest.CurrentStep - 17, latest.CurrentStep - 2} {
		snap, err := s.SeekRetainedStep(context.Background(), target, latest.Revision)
		if err != nil {
			t.Fatal(err)
		}
		if snap.Timeline.StateHash != original[target] || snap.Timeline.LatestStep != latest.LatestStep || snap.Timeline.EventCount != latest.EventCount || snap.Timeline.Revision != latest.Revision {
			t.Fatalf("history changed at %d: %+v", target, snap.Timeline)
		}
	}
	before := s.ReverseTimeline()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.SeekRetainedStep(ctx, latest.CurrentStep, latest.Revision); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	if s.ReverseTimeline() != before {
		t.Fatal("cancel mutated history")
	}
	if _, err := s.SeekRetainedStep(context.Background(), 0, latest.Revision+1); !errors.Is(err, ErrTimelineRevision) {
		t.Fatalf("revision: %v", err)
	}
	if _, err := s.SeekRetainedStep(context.Background(), latest.LatestStep+1, latest.Revision); err == nil {
		t.Fatal("accepted unrecorded future")
	}
	s.StepInstruction()
	if !s.ReverseTimeline().FutureDiscarded || s.ReverseTimeline().Revision == latest.Revision {
		t.Fatal("execution did not discard future")
	}
}

func TestSeekRetainedInputAndError(t *testing.T) {
	s := NewDebugSession(`class Main inherits IO {main():Object {{out_string(in_string()); 1/0;}};};`)
	s.SetInteractiveInput(true)
	waiting := s.RunWithBreakpoints(1000, nil)
	if _, err := s.ProvideInputLine("retained", waiting.Runtime.WaitingInput.Sequence); err != nil {
		t.Fatal(err)
	}
	final := s.RunWithBreakpoints(1000, nil)
	if _, err := s.SeekRetainedStep(context.Background(), waiting.Timeline.CurrentStep, final.Timeline.Revision); err != nil {
		t.Fatal(err)
	}
	got, err := s.SeekRetainedStep(context.Background(), final.Timeline.CurrentStep, final.Timeline.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Runtime.Output, final.Runtime.Output) || got.Runtime.WaitingInput != nil || got.Timeline.StateHash != final.Timeline.StateHash {
		t.Fatalf("input/error roundtrip changed state: %+v", got.Runtime)
	}
}

func TestSeekRetainedEviction(t *testing.T) {
	s := NewDebugSession(reverseObjectProgram)
	if err := s.SetReverseHistoryLimits(ReverseHistoryLimits{CheckpointInterval: 2, MaxCheckpoints: 2}); err != nil {
		t.Fatal(err)
	}
	s.RunWithBreakpoints(80, nil)
	timeline := s.ReverseTimeline()
	if timeline.EarliestReversibleStep <= 0 {
		t.Fatal("history was not evicted")
	}
	if _, err := s.SeekRetainedStep(context.Background(), timeline.EarliestReversibleStep-1, timeline.Revision); !errors.Is(err, ErrReverseTargetEvicted) {
		t.Fatalf("evicted target: %v", err)
	}
	if _, err := s.SeekRetainedStep(context.Background(), timeline.EarliestReversibleStep, timeline.Revision); err != nil {
		t.Fatal(err)
	}
}

type replayCancellationContext struct {
	context.Context //nolint:containedctx // Test decorator injects deterministic cancellation during replay.
	checks          int
	cancel          context.CancelFunc
}

func (c *replayCancellationContext) Err() error {
	c.checks++
	if c.checks == 6 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestSeekRetainedCancellationDuringReplayIsAtomic(t *testing.T) {
	s := NewDebugSession(reverseObjectProgram)
	s.RunWithBreakpoints(90, nil)
	before := s.ReverseTimeline()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	probe := &replayCancellationContext{Context: ctx, cancel: cancel}
	if _, err := s.SeekRetainedStep(probe, 85, before.Revision); !errors.Is(err, context.Canceled) {
		t.Fatalf("mid replay cancellation: %v", err)
	}
	if s.ReverseTimeline() != before || s.reverse.replaying {
		t.Fatal("cancelled replay changed live history")
	}
	if _, err := s.SeekRetainedStep(context.Background(), 85, before.Revision); err != nil {
		t.Fatal(err)
	}
}

const reverseObjectProgram = `class Box { value : Int <- 1; next : Box; set(n : Box) : Box {{ next <- n; self; }}; };
class Main {
  first : Box;
  second : Box;
  i : Int <- 0;
  main() : Object {{
    first <- new Box;
    second <- first.copy();
    first.set(second);
    second.set(first);
    while i < 5 loop i <- i + 1 pool;
    self;
  }};
};`

func TestStepBackReplaysExactInstructionStateAndBranches(t *testing.T) {
	session := NewDebugSession(reverseObjectProgram)
	hashes := map[int]string{}
	for step := 0; step < 70; step++ {
		snapshot := session.Snapshot(false, 0)
		hashes[snapshot.Runtime.ExecutedSteps] = snapshot.Timeline.StateHash
		if snapshot.Runtime.Halted {
			break
		}
		session.StepInstruction()
	}
	latest := session.ReverseTimeline()
	if latest.CurrentStep < 20 || latest.CheckpointCount < 2 || latest.CheckpointBytes <= 0 || latest.EventCount <= 0 || len(latest.ExecutableHash) != 64 {
		t.Fatalf("reverse history was not populated: %+v", latest)
	}
	if another := NewDebugSession(reverseObjectProgram).ReverseTimeline().ExecutableHash; another != latest.ExecutableHash {
		t.Fatalf("executable identity changed: %s != %s", another, latest.ExecutableHash)
	}
	for count := 0; count < 10; count++ {
		reversed, err := session.StepBack(ReverseModeInstruction)
		if err != nil {
			t.Fatal(err)
		}
		step := reversed.Runtime.ExecutedSteps
		if reversed.StopReason != StopReasonReverseStep || reversed.Timeline.StateHash != hashes[step] {
			t.Fatalf("step %d replay hash/reason = %s/%s want %s/%s", step, reversed.Timeline.StateHash, reversed.StopReason, hashes[step], StopReasonReverseStep)
		}
	}
	beforeBranch := session.ReverseTimeline()
	forward := session.StepInstruction()
	if forward.Timeline.Revision <= beforeBranch.Revision || !forward.Timeline.FutureDiscarded || forward.Timeline.LatestStep != forward.Runtime.ExecutedSteps {
		t.Fatalf("forward execution did not branch explicitly: before=%+v after=%+v", beforeBranch, forward.Timeline)
	}
}

func TestReverseContinueRestoresInputBoundaryAndAcceptsDifferentFuture(t *testing.T) {
	const source = `class Main inherits IO { main() : Object {{ out_string(in_string()); out_string("!"); }}; };`
	session := NewDebugSession(source)
	session.SetInteractiveInput(true)
	waiting := session.RunWithBreakpoints(100_000, nil)
	if waiting.Runtime.WaitingInput == nil {
		t.Fatalf("expected input boundary: %+v", waiting)
	}
	if _, err := session.ProvideInputLine("first", waiting.Runtime.WaitingInput.Sequence); err != nil {
		t.Fatal(err)
	}
	finished := session.RunWithBreakpoints(100_000, nil)
	if !finished.Runtime.Halted || !reflect.DeepEqual(finished.Runtime.Output, []string{"first", "!"}) {
		t.Fatalf("first timeline = %+v", finished.Runtime)
	}
	beforeHalt, err := session.StepBack(ReverseModeInstruction)
	if err != nil {
		t.Fatal(err)
	}
	if beforeHalt.Runtime.WaitingInput != nil || len(beforeHalt.Runtime.Output) == 0 || beforeHalt.Runtime.Output[0] != "first" {
		t.Fatalf("live input was not replayed automatically: %+v", beforeHalt.Runtime)
	}
	reversed, err := session.ReverseContinue()
	if err != nil {
		t.Fatal(err)
	}
	if reversed.Runtime.WaitingInput == nil || reversed.StopReason != StopReasonReverseContinue {
		t.Fatalf("reverse continue did not restore input boundary: %+v", reversed)
	}
	if _, err := session.ProvideInputLine("second", reversed.Runtime.WaitingInput.Sequence); err != nil {
		t.Fatal(err)
	}
	branched := session.RunWithBreakpoints(100_000, nil)
	if !reflect.DeepEqual(branched.Runtime.Output, []string{"second", "!"}) || !branched.Timeline.FutureDiscarded || branched.Timeline.Revision <= finished.Timeline.Revision {
		t.Fatalf("changed input did not create an explicit future: %+v timeline=%+v", branched.Runtime, branched.Timeline)
	}
}

func TestReverseReplayRecordsInteractiveEOFFinish(t *testing.T) {
	const source = `class Main inherits IO { main() : Object {{ out_string(in_string()); out_string("eof"); }}; };`
	session := NewDebugSession(source)
	session.SetInteractiveInput(true)
	if waiting := session.RunWithBreakpoints(100_000, nil); waiting.Runtime.WaitingInput == nil {
		t.Fatalf("expected interactive boundary: %+v", waiting)
	}
	session.FinishInteractiveInput()
	finished := session.RunWithBreakpoints(100_000, nil)
	if !reflect.DeepEqual(finished.Runtime.Output, []string{"", "eof"}) {
		t.Fatalf("EOF timeline output = %+v", finished.Runtime.Output)
	}
	replayed, err := session.StepBack(ReverseModeInstruction)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Runtime.WaitingInput != nil || len(replayed.Runtime.Output) == 0 {
		t.Fatalf("EOF completion was not replayed: %+v", replayed.Runtime)
	}
}

func TestReplayPreservesLogpointsRuntimeErrorsAndRecursiveDispatch(t *testing.T) {
	enabled := true
	logpoint := Breakpoint{ID: "loop-log", SourceID: "source-1", Line: 11, LogMessage: "i={i}", Enabled: &enabled}
	finished := NewDebugSession(reverseObjectProgram)
	end := finished.RunWithBreakpoints(100_000, []Breakpoint{logpoint})
	if !end.Runtime.Halted || end.Runtime.ExecutedSteps < 2 {
		t.Fatalf("logpoint fixture did not halt: %+v", end.Runtime)
	}
	target := end.Runtime.ExecutedSteps - 1
	reference := NewDebugSession(reverseObjectProgram)
	referenceAtTarget := reference.RunWithBreakpoints(target, []Breakpoint{logpoint})
	replayed, err := finished.StepBack(ReverseModeInstruction)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Timeline.StateHash != referenceAtTarget.Timeline.StateHash || !reflect.DeepEqual(replayed.DebugLog, referenceAtTarget.DebugLog) {
		t.Fatalf("logpoint replay diverged:\nreplayed=%+v\nreference=%+v", replayed, referenceAtTarget)
	}

	const failing = `class Main { main() : Object { 1 / 0 }; };`
	errorSession := NewDebugSession(failing)
	failed := errorSession.RunWithBreakpoints(100_000, nil)
	if failed.StopReason != StopReasonRuntimeError {
		t.Fatalf("expected runtime error: %+v", failed)
	}
	if _, err := errorSession.StepBack(ReverseModeInstruction); err != nil {
		t.Fatal(err)
	}
	repeated := errorSession.RunWithBreakpoints(100_000, nil)
	if repeated.StopReason != StopReasonRuntimeError || len(repeated.Runtime.Diagnostics) == 0 || repeated.Runtime.Diagnostics[0].Code != failed.Runtime.Diagnostics[0].Code {
		t.Fatalf("runtime error did not reproduce: first=%+v repeated=%+v", failed.Runtime.Diagnostics, repeated.Runtime.Diagnostics)
	}

	const recursive = `class Counter { value : Int <- 1; go(n : Int) : Int { if n = 0 then value else go(n - 1) fi }; };
class Main { main() : Object { (new Counter).go(8) }; };`
	recursiveSession := NewDebugSession(recursive)
	recursiveEnd := recursiveSession.RunWithBreakpoints(100_000, nil)
	if !recursiveEnd.Runtime.Halted {
		t.Fatalf("recursive dispatch fixture failed: %+v", recursiveEnd.Runtime)
	}
	for range 20 {
		if _, err := recursiveSession.StepBack(ReverseModeInstruction); err != nil {
			t.Fatal(err)
		}
	}
	if recursiveSession.ReverseTimeline().CurrentStep != recursiveEnd.Runtime.ExecutedSteps-20 {
		t.Fatalf("recursive replay position = %+v", recursiveSession.ReverseTimeline())
	}
}

func TestSourceStepBackAndRandomizedForwardBackModel(t *testing.T) {
	session := NewDebugSession(sourceStepProgram)
	positions := []int{0}
	for index := 0; index < 8 && !session.Snapshot(false, 0).Runtime.Halted; index++ {
		snapshot := session.StepInto(100_000, nil)
		positions = append(positions, snapshot.Runtime.ExecutedSteps)
	}
	current := session.ReverseTimeline().CurrentStep
	reversed, err := session.StepBack(ReverseModeSource)
	if err != nil {
		t.Fatal(err)
	}
	if reversed.Runtime.ExecutedSteps >= current || reversed.StopReason != StopReasonReverseStep {
		t.Fatalf("source step back = %+v", reversed)
	}

	random := rand.New(rand.NewSource(39))
	model := reversed.Runtime.ExecutedSteps
	for index := 0; index < 100; index++ {
		if model > reversed.Timeline.EarliestReversibleStep && random.Intn(3) == 0 {
			reversed, err = session.StepBack(ReverseModeInstruction)
			if err != nil {
				t.Fatal(err)
			}
			model--
		} else if !reversed.Runtime.Halted {
			reversed = session.StepInstruction()
			model++
		}
		if reversed.Runtime.ExecutedSteps != model {
			t.Fatalf("random operation %d model=%d runtime=%d", index, model, reversed.Runtime.ExecutedSteps)
		}
	}
	if len(positions) == 0 || positions[0] >= positions[len(positions)-1] {
		t.Fatalf("forward source-step positions = %v", positions)
	}
}

func TestReverseHistoryBoundsExposeEarliestStep(t *testing.T) {
	session := NewDebugSession(reverseObjectProgram)
	if err := session.SetReverseHistoryLimits(ReverseHistoryLimits{
		CheckpointInterval: 2, MaxCheckpoints: 3, MaxCheckpointBytes: 1 << 20,
		MaxEvents: 12, MaxEventBytes: 4 << 10,
	}); err != nil {
		t.Fatal(err)
	}
	finished := session.RunWithBreakpoints(100_000, nil)
	timeline := finished.Timeline
	if timeline.CheckpointCount > 3 || timeline.CheckpointBytes > timeline.Limits.MaxCheckpointBytes || timeline.EventCount > timeline.Limits.MaxEvents || timeline.EventBytes > timeline.Limits.MaxEventBytes {
		t.Fatalf("reverse bounds escaped: %+v", timeline)
	}
	if timeline.EarliestReversibleStep <= 0 || timeline.CanStepBack != (timeline.CurrentStep > timeline.EarliestReversibleStep) {
		t.Fatalf("earliest reverse boundary = %+v", timeline)
	}
	for session.ReverseTimeline().CanStepBack {
		if _, err := session.StepBack(ReverseModeInstruction); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := session.StepBack(ReverseModeInstruction); !errors.Is(err, ErrReverseHistoryUnavailable) {
		t.Fatalf("step before earliest error = %v", err)
	}
}

func BenchmarkStepBackCheckpointReplay(b *testing.B) {
	for iteration := 0; iteration < b.N; iteration++ {
		session := NewDebugSession(reverseObjectProgram)
		session.RunWithBreakpoints(100_000, nil)
		b.StopTimer()
		timeline := session.ReverseTimeline()
		b.ReportMetric(float64(timeline.CheckpointBytes), "checkpoint-bytes")
		b.StartTimer()
		for count := 0; count < 16; count++ {
			if _, err := session.StepBack(ReverseModeInstruction); err != nil {
				b.Fatal(err)
			}
		}
	}
}
