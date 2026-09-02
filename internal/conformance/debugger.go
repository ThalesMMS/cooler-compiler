// SPDX-License-Identifier: AGPL-3.0-or-later

package conformance

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	publiccompiler "github.com/ThalesMMS/cooler-compiler/compiler"
	"github.com/ThalesMMS/cooler-compiler/internal/compiler"
)

type debuggerScenarioState struct {
	pipeline       *compiler.CompilationSession
	pipelineState  compiler.PipelineSnapshot
	debugger       *compiler.DebugSession
	snapshot       compiler.DebugSnapshot
	heap           *compiler.HeapSnapshot
	lastErr        error
	recordedHashes map[int]string
	seenMethods    map[string]bool
	seenLines      map[int]bool
	maxFrameDepth  int
	sawSynthetic   bool
	publicDebugger *publiccompiler.DebugSession
	publicOptions  publiccompiler.Options
}

func runDebuggerFixture(fixture Fixture, units []compiler.SourceUnit) FixtureResult {
	result := FixtureResult{ID: fixture.ID, Passed: true}
	policy := compiler.VMPolicy{}
	if fixture.VMPolicy != nil {
		policy = *fixture.VMPolicy
	}
	state := debuggerScenarioState{
		recordedHashes: map[int]string{}, seenMethods: map[string]bool{}, seenLines: map[int]bool{},
	}
	state.pipeline = compiler.NewProjectCompilationSession(units, compiler.ResultContract{
		CompilerVersion: "conformance-debugger",
		Options:         compiler.CompilationOptions{Execute: true, VMMaxSteps: fixture.DebuggerScenario.MaxInstructions, VMPolicy: policy},
	})
	state.pipelineState = state.pipeline.Snapshot()

	for index, action := range fixture.DebuggerScenario.Actions {
		state.lastErr = nil
		state.heap = nil
		state.seenMethods = map[string]bool{}
		state.seenLines = map[int]bool{}
		state.maxFrameDepth = 0
		state.sawSynthetic = false
		if err := executeDebuggerAction(&state, units, *fixture.DebuggerScenario, action); err != nil {
			result.Passed = false
			result.Failure = fmt.Sprintf("action %d %s: %v", index+1, action.Op, err)
			result.Replay = debuggerReplayEvidence(state, fixture.DebuggerScenario.Actions[:index+1], index+1)
			break
		}
		if err := compareDebuggerExpectation(state, action.Expect); err != nil {
			result.Passed = false
			result.Failure = fmt.Sprintf("action %d %s: %v", index+1, action.Op, err)
			result.Replay = debuggerReplayEvidence(state, fixture.DebuggerScenario.Actions[:index+1], index+1)
			break
		}
	}
	if state.publicDebugger != nil {
		_ = state.publicDebugger.Close()
	}

	finalResult := state.pipelineState.Result
	if state.snapshot.Result.SchemaVersion != "" {
		finalResult = state.snapshot.Result
	}
	result.LastSuccessfulStage = lastSuccessfulStage(finalResult.Diagnostics, string(state.pipelineState.CurrentPhase))
	if state.pipelineState.CurrentPhase == compiler.PhaseRuntime && !state.pipelineState.Blocked && !hasError(finalResult.Diagnostics) {
		result.LastSuccessfulStage = "runtime"
	}
	result.Output = strings.Join(state.snapshot.Runtime.Output, "")
	result.RuntimeStatus = runtimeStatus(finalResult)
	for _, unit := range finalResult.SourceUnits {
		result.SourceUnits = append(result.SourceUnits, unit.Name)
	}
	diagnosticKeys := map[string]bool{}
	for _, diagnostic := range append(append([]compiler.Diagnostic{}, finalResult.Diagnostics...), state.snapshot.Runtime.Diagnostics...) {
		diagnosticKeys[diagnosticKey(diagnostic.Stage, string(diagnostic.Severity), diagnostic.Code)] = true
	}
	for key := range diagnosticKeys {
		result.Diagnostics = append(result.Diagnostics, key)
	}
	sort.Strings(result.Diagnostics)
	if result.Passed {
		expectedDiagnostics := make([]string, 0, len(fixture.ExpectedDiagnostics))
		for _, diagnostic := range fixture.ExpectedDiagnostics {
			expectedDiagnostics = append(expectedDiagnostics, diagnosticKey(diagnostic.Stage, diagnostic.Severity, diagnostic.Code))
		}
		sort.Strings(expectedDiagnostics)
		failures := []string{}
		if !equalStringSlices(result.Diagnostics, expectedDiagnostics) {
			failures = append(failures, fmt.Sprintf("diagnostics %v, want %v", result.Diagnostics, expectedDiagnostics))
		}
		if result.LastSuccessfulStage != fixture.ExpectedLastSuccessfulStage {
			failures = append(failures, fmt.Sprintf("last successful stage %q, want %q", result.LastSuccessfulStage, fixture.ExpectedLastSuccessfulStage))
		}
		if result.Output != fixture.ExpectedOutput {
			failures = append(failures, fmt.Sprintf("output %q, want %q", result.Output, fixture.ExpectedOutput))
		}
		if fixture.ExpectedRuntimeStatus != "" && result.RuntimeStatus != fixture.ExpectedRuntimeStatus {
			failures = append(failures, fmt.Sprintf("runtime status %q, want %q", result.RuntimeStatus, fixture.ExpectedRuntimeStatus))
		}
		if len(fixture.ExpectedSourceUnits) > 0 && !reflect.DeepEqual(result.SourceUnits, fixture.ExpectedSourceUnits) {
			failures = append(failures, fmt.Sprintf("source units %v, want %v", result.SourceUnits, fixture.ExpectedSourceUnits))
		}
		if fixture.Validity == "valid" && hasError(finalResult.Diagnostics) {
			failures = append(failures, "valid fixture produced an error")
		}
		if fixture.Validity == "invalid" && !hasError(finalResult.Diagnostics) {
			failures = append(failures, "invalid fixture produced no error")
		}
		if len(failures) > 0 {
			result.Passed = false
			result.Failure = strings.Join(failures, "; ")
			if len(fixture.DebuggerScenario.Actions) > 0 {
				result.Replay = debuggerReplayEvidence(state, fixture.DebuggerScenario.Actions, len(fixture.DebuggerScenario.Actions))
			}
		}
	}
	return result
}

func debuggerReplayEvidence(state debuggerScenarioState, actions []DebuggerAction, actionIndex int) *DebuggerReplayEvidence {
	actual := DebuggerActualEvidence{
		PipelinePhase: string(state.pipelineState.CurrentPhase), Complete: state.pipelineState.Complete,
		Blocked: state.pipelineState.Blocked, StopReason: string(state.snapshot.StopReason), Status: state.snapshot.Status,
		ExecutedSteps: state.snapshot.Runtime.ExecutedSteps, Halted: state.snapshot.Runtime.Halted,
		Paused: state.snapshot.Runtime.Paused, WaitingInput: state.snapshot.Runtime.WaitingInput != nil,
		WaitingRequest: state.snapshot.Runtime.WaitingInput, InputEvents: state.snapshot.Runtime.InputEvents,
		Location: state.snapshot.Location, Output: append([]string(nil), state.snapshot.Runtime.Output...),
		Diagnostics: debuggerDiagnosticCodes(state), Timeline: state.snapshot.Timeline,
		HeapReferences: map[string]string{},
	}
	if state.lastErr != nil {
		actual.Error = state.lastErr.Error()
	}
	for _, entry := range state.snapshot.DebugLog {
		actual.DebugLog = append(actual.DebugLog, entry.Message)
	}
	if state.heap != nil {
		for _, object := range state.heap.Objects {
			actual.HeapObjectIDs = append(actual.HeapObjectIDs, object.ID)
			for _, field := range object.Fields {
				if field.Value.ObjectID != "" {
					actual.HeapReferences[object.ID+"."+field.Name] = field.Value.ObjectID
				}
			}
		}
	}
	if len(actual.HeapReferences) == 0 {
		actual.HeapReferences = nil
	}
	return &DebuggerReplayEvidence{
		ActionIndex: actionIndex,
		Actions:     append([]DebuggerAction(nil), actions...),
		Actual:      actual,
	}
}

func executeDebuggerAction(state *debuggerScenarioState, units []compiler.SourceUnit, scenario DebuggerScenario, action DebuggerAction) error {
	steps := action.Steps
	if steps == 0 {
		steps = 100_000
	}
	switch action.Op {
	case "snapshot-pipeline":
		state.pipelineState = state.pipeline.Snapshot()
	case "next-phase":
		state.pipelineState = state.pipeline.NextPhase()
	case "complete":
		state.pipelineState = state.pipeline.Complete()
		if debugger, ok := state.pipeline.DebugSession(); ok {
			state.debugger = debugger
			if scenario.ReverseLimits != nil {
				state.lastErr = debugger.SetReverseHistoryLimits(*scenario.ReverseLimits)
			}
			state.snapshot = debugger.Snapshot(false, 0)
		}
	case "promote":
		debugger, ok := state.pipeline.DebugSession()
		if !ok {
			state.lastErr = errors.New("completed pipeline has no debugger session")
			break
		}
		state.debugger = debugger
		if scenario.ReverseLimits != nil {
			state.lastErr = debugger.SetReverseHistoryLimits(*scenario.ReverseLimits)
		}
		state.snapshot = debugger.Snapshot(false, 0)
	case "set-reverse-limits":
		if state.debugger == nil {
			return errors.New("debugger is not prepared")
		}
		if scenario.ReverseLimits == nil {
			return errors.New("set-reverse-limits requires fixture reverseLimits")
		}
		state.lastErr = state.debugger.SetReverseHistoryLimits(*scenario.ReverseLimits)
	case "set-input":
		if state.debugger == nil {
			return errors.New("debugger is not prepared")
		}
		state.debugger.SetInput(action.Input)
		state.snapshot = state.debugger.Snapshot(false, 0)
	case "set-interactive":
		if state.debugger == nil {
			return errors.New("debugger is not prepared")
		}
		if action.Interactive == nil {
			return errors.New("set-interactive requires interactive")
		}
		state.debugger.SetInteractiveInput(*action.Interactive)
		state.snapshot = state.debugger.Snapshot(false, 0)
	case "set-watches":
		if state.debugger == nil {
			return errors.New("debugger is not prepared")
		}
		state.debugger.SetWatches(action.Watches)
		state.snapshot = state.debugger.Snapshot(false, 0)
	case "step-instruction":
		if state.debugger == nil {
			return errors.New("debugger is not prepared")
		}
		state.snapshot = state.debugger.StepInstruction()
		recordDebuggerHash(state)
	case "step-instructions":
		if state.debugger == nil {
			return errors.New("debugger is not prepared")
		}
		count := action.Count
		if count < 1 {
			return errors.New("step-instructions requires positive count")
		}
		for index := 0; index < count && !state.snapshot.Runtime.Halted; index++ {
			recordDebuggerHash(state)
			state.snapshot = state.debugger.StepInstruction()
			recordDebuggerHash(state)
		}
	case "step-into":
		if state.debugger == nil {
			return errors.New("debugger is not prepared")
		}
		state.snapshot = state.debugger.StepInto(steps, action.Breakpoints)
		recordDebuggerHash(state)
	case "step-over":
		if state.debugger == nil {
			return errors.New("debugger is not prepared")
		}
		state.snapshot = state.debugger.StepOver(steps, action.Breakpoints)
		recordDebuggerHash(state)
	case "step-out":
		if state.debugger == nil {
			return errors.New("debugger is not prepared")
		}
		state.snapshot = state.debugger.StepOut(steps, action.Breakpoints)
		recordDebuggerHash(state)
	case "run":
		if state.debugger == nil {
			return errors.New("debugger is not prepared")
		}
		state.snapshot = state.debugger.RunWithBreakpoints(steps, action.Breakpoints)
		recordDebuggerHash(state)
	case "run-to-cursor":
		if state.debugger == nil {
			return errors.New("debugger is not prepared")
		}
		if action.Cursor == nil {
			return errors.New("run-to-cursor requires cursor")
		}
		state.snapshot, state.lastErr = state.debugger.RunToCursor(steps, action.Breakpoints, *action.Cursor)
		recordDebuggerHash(state)
	case "reset":
		if state.debugger == nil {
			return errors.New("debugger is not prepared")
		}
		state.snapshot = state.debugger.Reset()
		recordDebuggerHash(state)
	case "provide-input":
		if state.debugger == nil {
			return errors.New("debugger is not prepared")
		}
		sequence := action.Count
		if sequence == 0 && state.snapshot.Runtime.WaitingInput != nil {
			sequence = state.snapshot.Runtime.WaitingInput.Sequence
		}
		state.snapshot, state.lastErr = state.debugger.ProvideInputLine(action.Input, sequence)
		recordDebuggerHash(state)
	case "finish-input":
		if state.debugger == nil {
			return errors.New("debugger is not prepared")
		}
		state.snapshot = state.debugger.FinishInteractiveInput()
		recordDebuggerHash(state)
	case "heap":
		if state.debugger == nil {
			return errors.New("debugger is not prepared")
		}
		if action.Heap == nil {
			return errors.New("heap action requires query")
		}
		heap, err := state.debugger.Heap(*action.Heap)
		state.lastErr = err
		if err == nil {
			state.heap = &heap
		}
	case "step-back":
		if state.debugger == nil {
			return errors.New("debugger is not prepared")
		}
		state.snapshot, state.lastErr = state.debugger.StepBack(action.ReverseMode)
	case "reverse-continue":
		if state.debugger == nil {
			return errors.New("debugger is not prepared")
		}
		state.snapshot, state.lastErr = state.debugger.ReverseContinue()
	case "repeat-wait":
		if state.debugger == nil {
			return errors.New("debugger is not prepared")
		}
		before := state.snapshot
		if before.Runtime.WaitingInput == nil {
			return errors.New("debugger is not waiting for input")
		}
		state.snapshot = state.debugger.RunWithBreakpoints(steps, action.Breakpoints)
		beforeIP, afterIP := -1, -1
		if len(before.Runtime.Frames) > 0 {
			beforeIP = before.Runtime.Frames[len(before.Runtime.Frames)-1].IP
		}
		if len(state.snapshot.Runtime.Frames) > 0 {
			afterIP = state.snapshot.Runtime.Frames[len(state.snapshot.Runtime.Frames)-1].IP
		}
		if state.snapshot.Runtime.ExecutedSteps != before.Runtime.ExecutedSteps || afterIP != beforeIP ||
			!reflect.DeepEqual(state.snapshot.Runtime.Stack, before.Runtime.Stack) ||
			!equalStringSlices(state.snapshot.Runtime.Output, before.Runtime.Output) ||
			state.snapshot.Runtime.InputEvents != before.Runtime.InputEvents {
			return errors.New("waiting input run changed instruction, stack, output, or input-event state")
		}
	case "scan-source-steps":
		if state.debugger == nil {
			return errors.New("debugger is not prepared")
		}
		count := action.Count
		if count < 1 {
			count = 200
		}
		for index := 0; index < count && !state.snapshot.Runtime.Halted; index++ {
			state.snapshot = state.debugger.StepInto(steps, action.Breakpoints)
			if location := state.snapshot.Location; location != nil {
				state.seenMethods[location.Method] = true
				state.seenLines[location.Span.StartLine] = true
				if location.FrameDepth > state.maxFrameDepth {
					state.maxFrameDepth = location.FrameDepth
				}
				state.sawSynthetic = state.sawSynthetic || location.Synthetic
			}
		}
		recordDebuggerHash(state)
	case "advance-to-location":
		if state.debugger == nil {
			return errors.New("debugger is not prepared")
		}
		count := action.Count
		if count < 1 {
			count = 100
		}
		for index := 0; index < count; index++ {
			state.snapshot = state.debugger.StepInto(steps, action.Breakpoints)
			location := state.snapshot.Location
			methodMatches := action.Expect.Method == "" || location != nil && location.Method == action.Expect.Method
			instructionMatches := action.Expect.Instruction == nil || location != nil && location.Instruction == *action.Expect.Instruction
			lineMatches := action.Expect.Line == 0 || location != nil && location.Span.StartLine == action.Expect.Line
			if methodMatches && instructionMatches && lineMatches {
				break
			}
			if state.snapshot.Runtime.Halted {
				break
			}
		}
		recordDebuggerHash(state)
	case "public-start":
		project := publiccompiler.Project{Sources: make([]publiccompiler.SourceUnit, 0, len(units))}
		for _, unit := range units {
			project.Sources = append(project.Sources, publiccompiler.SourceUnit{ID: unit.ID, Name: unit.Name, Content: unit.Content})
		}
		state.publicOptions = publiccompiler.DefaultOptions()
		state.publicOptions.Limits = publiccompiler.MaximumLimits()
		state.publicOptions.MaxSteps = state.publicOptions.Limits.MaxInstructionsPerRun
		state.publicDebugger, state.lastErr = publiccompiler.NewDebugSession(context.Background(), project, state.publicOptions)
	case "public-pause-run":
		if state.publicDebugger == nil {
			return errors.New("public debugger is not prepared")
		}
		done := make(chan error, 1)
		go func() {
			_, runErr := state.publicDebugger.Run(context.Background(), state.publicOptions.MaxSteps, nil)
			done <- runErr
		}()
		deadline := time.Now().Add(2 * time.Second)
		for {
			err := state.publicDebugger.Pause()
			if err == nil {
				break
			}
			if !errors.Is(err, publiccompiler.ErrNotRunning) || time.Now().After(deadline) {
				state.lastErr = err
				return nil
			}
		}
		state.lastErr = <-done
	case "public-close":
		if state.publicDebugger == nil {
			return errors.New("public debugger is not prepared")
		}
		state.lastErr = state.publicDebugger.Close()
	case "public-reset":
		if state.publicDebugger == nil {
			return errors.New("public debugger is not prepared")
		}
		_, state.lastErr = state.publicDebugger.Reset(context.Background())
	default:
		return fmt.Errorf("unknown debugger action %q", action.Op)
	}
	return nil
}

func recordDebuggerHash(state *debuggerScenarioState) {
	if state.snapshot.Timeline != nil && state.snapshot.Timeline.StateHash != "" {
		state.recordedHashes[state.snapshot.Runtime.ExecutedSteps] = state.snapshot.Timeline.StateHash
	}
}

func compareDebuggerExpectation(state debuggerScenarioState, expected DebuggerExpectation) error {
	if expected.ErrorContains == "" {
		if state.lastErr != nil {
			return fmt.Errorf("unexpected error: %w", state.lastErr)
		}
	} else if state.lastErr == nil || !strings.Contains(state.lastErr.Error(), expected.ErrorContains) {
		return fmt.Errorf("error %w does not contain %q", state.lastErr, expected.ErrorContains)
	}
	if expected.PipelinePhase != "" && string(state.pipelineState.CurrentPhase) != expected.PipelinePhase {
		return fmt.Errorf("pipeline phase %q, want %q", state.pipelineState.CurrentPhase, expected.PipelinePhase)
	}
	if expected.Complete != nil && state.pipelineState.Complete != *expected.Complete {
		return fmt.Errorf("pipeline complete %v, want %v", state.pipelineState.Complete, *expected.Complete)
	}
	if expected.Blocked != nil && state.pipelineState.Blocked != *expected.Blocked {
		return fmt.Errorf("pipeline blocked %v, want %v", state.pipelineState.Blocked, *expected.Blocked)
	}
	if expected.StopReason != "" && string(state.snapshot.StopReason) != expected.StopReason {
		return fmt.Errorf("stop reason %q, want %q", state.snapshot.StopReason, expected.StopReason)
	}
	if expected.Status != "" && state.snapshot.Status != expected.Status {
		return fmt.Errorf("status %q, want %q", state.snapshot.Status, expected.Status)
	}
	if expected.ExecutedSteps != nil && state.snapshot.Runtime.ExecutedSteps != *expected.ExecutedSteps {
		return fmt.Errorf("executed steps %d, want %d", state.snapshot.Runtime.ExecutedSteps, *expected.ExecutedSteps)
	}
	if state.snapshot.Runtime.ExecutedSteps < expected.MinimumExecutedSteps {
		return fmt.Errorf("executed steps %d, want at least %d", state.snapshot.Runtime.ExecutedSteps, expected.MinimumExecutedSteps)
	}
	if expected.Halted != nil && state.snapshot.Runtime.Halted != *expected.Halted {
		return fmt.Errorf("halted %v, want %v", state.snapshot.Runtime.Halted, *expected.Halted)
	}
	if expected.Paused != nil && state.snapshot.Runtime.Paused != *expected.Paused {
		return fmt.Errorf("paused %v, want %v", state.snapshot.Runtime.Paused, *expected.Paused)
	}
	waiting := state.snapshot.Runtime.WaitingInput != nil
	if expected.WaitingInput != nil && waiting != *expected.WaitingInput {
		return fmt.Errorf("waiting input %v, want %v", waiting, *expected.WaitingInput)
	}
	if expected.WaitingBuiltin != "" || expected.WaitingInputPosition != nil || expected.WaitingSequence != nil {
		request := state.snapshot.Runtime.WaitingInput
		if request == nil {
			return errors.New("expected waiting input metadata, got none")
		}
		if expected.WaitingBuiltin != "" && request.Builtin != expected.WaitingBuiltin {
			return fmt.Errorf("waiting builtin %q, want %q", request.Builtin, expected.WaitingBuiltin)
		}
		if expected.WaitingInputPosition != nil && request.InputPosition != *expected.WaitingInputPosition {
			return fmt.Errorf("waiting input position %d, want %d", request.InputPosition, *expected.WaitingInputPosition)
		}
		if expected.WaitingSequence != nil && request.Sequence != *expected.WaitingSequence {
			return fmt.Errorf("waiting sequence %d, want %d", request.Sequence, *expected.WaitingSequence)
		}
	}
	if expected.InputEvents != nil && state.snapshot.Runtime.InputEvents != *expected.InputEvents {
		return fmt.Errorf("input events %d, want %d", state.snapshot.Runtime.InputEvents, *expected.InputEvents)
	}
	if expected.SourceID != "" || expected.Line != 0 || expected.Method != "" || expected.Instruction != nil || expected.FrameDepth != nil || expected.Synthetic != nil {
		if state.snapshot.Location == nil {
			return errors.New("expected a source location, got none")
		}
		location := state.snapshot.Location
		if expected.SourceID != "" && location.SourceID != expected.SourceID {
			return fmt.Errorf("source %q, want %q", location.SourceID, expected.SourceID)
		}
		if expected.Line != 0 && location.Span.StartLine != expected.Line {
			return fmt.Errorf("line %d, want %d", location.Span.StartLine, expected.Line)
		}
		if expected.Method != "" && location.Method != expected.Method {
			return fmt.Errorf("method %q, want %q", location.Method, expected.Method)
		}
		if expected.Instruction != nil && location.Instruction != *expected.Instruction {
			return fmt.Errorf("instruction %d, want %d", location.Instruction, *expected.Instruction)
		}
		if expected.FrameDepth != nil && location.FrameDepth != *expected.FrameDepth {
			return fmt.Errorf("frame depth %d, want %d", location.FrameDepth, *expected.FrameDepth)
		}
		if expected.Synthetic != nil && location.Synthetic != *expected.Synthetic {
			return fmt.Errorf("synthetic %v, want %v", location.Synthetic, *expected.Synthetic)
		}
	}
	if expected.Output != nil && !equalStringSlices(state.snapshot.Runtime.Output, expected.Output) {
		return fmt.Errorf("output %q, want %q", state.snapshot.Runtime.Output, expected.Output)
	}
	if expected.Stack != nil {
		actual := make([]string, 0, len(state.snapshot.Runtime.Stack))
		for _, value := range state.snapshot.Runtime.Stack {
			actual = append(actual, value.Display)
		}
		if !equalStringSlices(actual, expected.Stack) {
			return fmt.Errorf("stack %v, want %v", actual, expected.Stack)
		}
	}
	actualDiagnostics := debuggerDiagnosticCodes(state)
	if expected.Diagnostics != nil && !equalStringSlices(actualDiagnostics, expected.Diagnostics) {
		return fmt.Errorf("diagnostics %v, want %v", actualDiagnostics, expected.Diagnostics)
	}
	if expected.DebugLog != nil {
		actual := make([]string, 0, len(state.snapshot.DebugLog))
		for _, entry := range state.snapshot.DebugLog {
			actual = append(actual, entry.Message)
		}
		if !equalStringSlices(actual, expected.DebugLog) {
			return fmt.Errorf("debug log %v, want %v", actual, expected.DebugLog)
		}
	}
	for id, display := range expected.WatchValues {
		found := false
		for _, watch := range state.snapshot.Watches {
			if watch.ID == id && watch.Value != nil {
				found = watch.Value.Display == display
			}
		}
		if !found {
			return fmt.Errorf("watch %q does not have value %q", id, display)
		}
	}
	for _, id := range expected.WatchDiagnostics {
		found := false
		for _, watch := range state.snapshot.Watches {
			found = found || watch.ID == id && watch.Diagnostic != ""
		}
		if !found {
			return fmt.Errorf("watch %q has no diagnostic", id)
		}
	}
	for id, hits := range expected.BreakpointHits {
		found := false
		for _, breakpoint := range state.snapshot.Breakpoints {
			found = found || breakpoint.Breakpoint.ID == id && breakpoint.Hits == hits
		}
		if !found {
			return fmt.Errorf("breakpoint %q does not have %d hits", id, hits)
		}
	}
	for id, verified := range expected.BreakpointVerified {
		found := false
		for _, breakpoint := range state.snapshot.Breakpoints {
			found = found || breakpoint.Breakpoint.ID == id && breakpoint.Verified == verified
		}
		if !found {
			return fmt.Errorf("breakpoint %q verified state is not %v", id, verified)
		}
	}
	if state.snapshot.Runtime.TraceOffset < expected.MinimumTraceOffset {
		return fmt.Errorf("trace offset %d, want at least %d", state.snapshot.Runtime.TraceOffset, expected.MinimumTraceOffset)
	}
	if expected.RetainedTraceMaximum != nil && len(state.snapshot.Runtime.Trace) > *expected.RetainedTraceMaximum {
		return fmt.Errorf("retained trace entries %d, want at most %d", len(state.snapshot.Runtime.Trace), *expected.RetainedTraceMaximum)
	}
	if expected.TimelineRevisionMinimum != 0 || expected.TimelineCurrentStep != nil || expected.EarliestReversibleMinimum != 0 || expected.TimelineCheckpointMaximum != nil || expected.TimelineEventMaximum != nil || expected.FutureDiscarded != nil || expected.StateHashMatchesRecorded {
		if state.snapshot.Timeline == nil {
			return errors.New("expected timeline, got none")
		}
		timeline := state.snapshot.Timeline
		if timeline.Revision < expected.TimelineRevisionMinimum {
			return fmt.Errorf("timeline revision %d, want at least %d", timeline.Revision, expected.TimelineRevisionMinimum)
		}
		if expected.TimelineCurrentStep != nil && timeline.CurrentStep != *expected.TimelineCurrentStep {
			return fmt.Errorf("timeline step %d, want %d", timeline.CurrentStep, *expected.TimelineCurrentStep)
		}
		if timeline.EarliestReversibleStep < expected.EarliestReversibleMinimum {
			return fmt.Errorf("earliest reversible step %d, want at least %d", timeline.EarliestReversibleStep, expected.EarliestReversibleMinimum)
		}
		if expected.TimelineCheckpointMaximum != nil && timeline.CheckpointCount > *expected.TimelineCheckpointMaximum {
			return fmt.Errorf("timeline checkpoints %d, want at most %d", timeline.CheckpointCount, *expected.TimelineCheckpointMaximum)
		}
		if expected.TimelineEventMaximum != nil && timeline.EventCount > *expected.TimelineEventMaximum {
			return fmt.Errorf("timeline events %d, want at most %d", timeline.EventCount, *expected.TimelineEventMaximum)
		}
		if expected.FutureDiscarded != nil && timeline.FutureDiscarded != *expected.FutureDiscarded {
			return fmt.Errorf("future discarded %v, want %v", timeline.FutureDiscarded, *expected.FutureDiscarded)
		}
		if expected.StateHashMatchesRecorded && state.recordedHashes[state.snapshot.Runtime.ExecutedSteps] != timeline.StateHash {
			return fmt.Errorf("state hash at step %d did not match recorded state", state.snapshot.Runtime.ExecutedSteps)
		}
	}
	if len(expected.HeapObjectIDs) > 0 || len(expected.HeapRootLabels) > 0 || len(expected.HeapReferences) > 0 || len(expected.HeapFieldOwners) > 0 || len(expected.HeapFieldValues) > 0 || expected.HeapTruncated != nil || expected.HeapTotalObjects != nil {
		if state.heap == nil {
			return errors.New("expected heap snapshot, got none")
		}
		ids := make([]string, 0, len(state.heap.Objects))
		for _, object := range state.heap.Objects {
			ids = append(ids, object.ID)
		}
		if expected.HeapObjectIDs != nil && !equalStringSlices(ids, expected.HeapObjectIDs) {
			return fmt.Errorf("heap objects %v, want %v", ids, expected.HeapObjectIDs)
		}
		labels := make([]string, 0, len(state.heap.Roots))
		for _, root := range state.heap.Roots {
			labels = append(labels, root.Label)
		}
		if expected.HeapRootLabels != nil && !equalStringSlices(labels, expected.HeapRootLabels) {
			return fmt.Errorf("heap roots %v, want %v", labels, expected.HeapRootLabels)
		}
		for key, target := range expected.HeapReferences {
			found := false
			for _, object := range state.heap.Objects {
				for _, field := range object.Fields {
					found = found || object.ID+"."+field.Name == key && field.Value.ObjectID == target
				}
			}
			if !found {
				return fmt.Errorf("heap reference %s -> %s not found", key, target)
			}
		}
		for key, owner := range expected.HeapFieldOwners {
			found := false
			for _, object := range state.heap.Objects {
				for _, field := range object.Fields {
					found = found || object.ID+"."+field.Name == key && field.Owner == owner
				}
			}
			if !found {
				return fmt.Errorf("heap field %s does not have owner %s", key, owner)
			}
		}
		for key, display := range expected.HeapFieldValues {
			found := false
			for _, object := range state.heap.Objects {
				for _, field := range object.Fields {
					found = found || object.ID+"."+field.Name == key && field.Value.Display == display
				}
			}
			if !found {
				return fmt.Errorf("heap field %s does not have value %s", key, display)
			}
		}
		if expected.HeapTruncated != nil && state.heap.Truncated != *expected.HeapTruncated {
			return fmt.Errorf("heap truncated %v, want %v", state.heap.Truncated, *expected.HeapTruncated)
		}
		if expected.HeapTotalObjects != nil && state.heap.TotalObjects != *expected.HeapTotalObjects {
			return fmt.Errorf("heap total %d, want %d", state.heap.TotalObjects, *expected.HeapTotalObjects)
		}
	}
	for _, method := range expected.RequiredMethods {
		if !state.seenMethods[method] {
			return fmt.Errorf("source-step scan did not see method %q", method)
		}
	}
	for _, line := range expected.RequiredLines {
		if !state.seenLines[line] {
			return fmt.Errorf("source-step scan did not see line %d", line)
		}
	}
	if state.maxFrameDepth < expected.MinimumFrameDepth {
		return fmt.Errorf("maximum frame depth %d, want at least %d", state.maxFrameDepth, expected.MinimumFrameDepth)
	}
	if expected.NoSyntheticStops && state.sawSynthetic {
		return errors.New("source-step scan stopped at a synthetic instruction")
	}
	return nil
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func debuggerDiagnosticCodes(state debuggerScenarioState) []string {
	seen := map[string]bool{}
	for _, diagnostic := range append(append([]compiler.Diagnostic{}, state.snapshot.Result.Diagnostics...), state.snapshot.Runtime.Diagnostics...) {
		seen[diagnostic.Code] = true
	}
	result := make([]string, 0, len(seen))
	for code := range seen {
		result = append(result, code)
	}
	sort.Strings(result)
	return result
}
