// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"context"
	"errors"
)

var ErrNoExecutableSourcePosition = errors.New("source position has no executable instruction")

// AbortInternal makes an unexpected host-side failure terminal and inspectable.
// Reset is the only operation that may construct a fresh VM afterward.
func (s *DebugSession) AbortInternal(message string) {
	if s.vm == nil {
		return
	}
	s.vm.abortInternal(message)
	s.ensureReverseHistory()
	s.finishForwardTimeline()
}

// DebugSession owns one compiled program and one mutable VM. The HTTP layer
// serializes access to a session; compiler values themselves stay independent
// from transport concerns.
type DebugSession struct {
	source           string
	base             Result
	exec             *executable
	vm               *VM
	blocked          bool
	input            string
	policy           VMPolicy
	interactiveInput bool
	breakpoints      []Breakpoint
	watches          []WatchExpression
	reverse          *reverseHistory
	reverseLimits    ReverseHistoryLimits
}

func NewDebugSession(source string) *DebugSession {
	pipeline := NewCompilationSession(source)
	snapshot := pipeline.Complete()
	if session, ok := pipeline.DebugSession(); ok {
		return session
	}
	return &DebugSession{source: source, base: snapshot.Result, blocked: true}
}

func (s *DebugSession) SetInput(text string) {
	if s.vm != nil && text != s.input {
		s.beginForwardTimeline()
	}
	s.input = text
	if s.vm != nil {
		s.vm.SetInput(text)
		s.ensureReverseHistory()
		s.reverse.captureCheckpoint(s.vm, "")
	}
}

// SetInteractiveInput controls whether exhausted input pauses before the input
// opcode (true) or uses COOL's deterministic EOF fallback (false).
func (s *DebugSession) SetInteractiveInput(enabled bool) {
	if s.vm != nil && enabled != s.interactiveInput {
		s.beginForwardTimeline()
	}
	s.interactiveInput = enabled
	if s.vm != nil {
		s.vm.interactiveInput = enabled
		s.ensureReverseHistory()
		s.reverse.captureCheckpoint(s.vm, "")
	}
}

// ProvideInputLine accepts one numbered pending request and executes exactly
// that input opcode. A repeated or stale submission is rejected.
func (s *DebugSession) ProvideInputLine(line string, sequence int) (DebugSnapshot, error) {
	if s.vm == nil {
		return s.Snapshot(false, 0), ErrInputNotWaiting
	}
	if err := s.vm.provideInputLine(line, sequence); err != nil {
		return s.Snapshot(false, 0), err
	}
	s.beginForwardTimeline()
	s.vm.Step()
	s.reverse.noteInput(s.vm.executed, sequence, line)
	s.finishForwardTimeline()
	return s.Snapshot(false, 0), nil
}

// FinishInteractiveInput applies the deterministic EOF fallback to the pending
// opcode and all future exhausted reads.
func (s *DebugSession) FinishInteractiveInput() DebugSnapshot {
	if s.vm != nil && s.vm.waitingInput != nil {
		sequence := s.vm.waitingInput.Sequence
		s.beginForwardTimeline()
		s.vm.finishInteractiveInput()
		s.vm.Step()
		s.reverse.noteFinishedInput(s.vm.executed, sequence)
		s.finishForwardTimeline()
	}
	return s.Snapshot(false, 0)
}

// InputEvents returns a copy of the deterministic event log. Sensitive line
// content is absent unless includeContent is explicitly true.
func (s *DebugSession) InputEvents(includeContent bool) []VMInputEvent {
	if s.vm == nil {
		return nil
	}
	return s.vm.inputEventLog(includeContent)
}

func (s *DebugSession) Step() DebugSnapshot {
	return s.StepInstruction()
}

// StepInstruction preserves the debugger's original one-opcode behavior.
func (s *DebugSession) StepInstruction() DebugSnapshot {
	if s.vm != nil && !s.vm.halted {
		s.beginForwardTimeline()
		s.vm.Step()
		s.finishForwardTimeline()
	}
	return s.Snapshot(false, 0)
}

func (s *DebugSession) StepInto(maxSteps int, breakpoints []Breakpoint) DebugSnapshot {
	return s.StepIntoContext(context.Background(), maxSteps, breakpoints)
}

func (s *DebugSession) StepIntoContext(ctx context.Context, maxSteps int, breakpoints []Breakpoint) DebugSnapshot {
	if s.vm == nil || s.vm.halted || s.vm.waitingInput != nil {
		return s.Snapshot(false, 0)
	}
	s.beginForwardTimeline()
	start := s.vm.currentDebugLocation()
	s.vm.runUntilContext(ctx, maxSteps, s.prepareBreakpointSet(breakpoints), func(location DebugLocation) bool {
		return !location.Synthetic && !sameVisibleDebugLocation(start, &location)
	}, StopReasonStepInto)
	s.finishForwardTimeline()
	return s.Snapshot(false, 0)
}

func (s *DebugSession) StepOver(maxSteps int, breakpoints []Breakpoint) DebugSnapshot {
	return s.StepOverContext(context.Background(), maxSteps, breakpoints)
}

func (s *DebugSession) StepOverContext(ctx context.Context, maxSteps int, breakpoints []Breakpoint) DebugSnapshot {
	if s.vm == nil || s.vm.halted || s.vm.waitingInput != nil {
		return s.Snapshot(false, 0)
	}
	s.beginForwardTimeline()
	start := s.vm.currentDebugLocation()
	startFrame := s.vm.currentFrame()
	startDepth := len(s.vm.frames) - 1
	s.vm.runUntilContext(ctx, maxSteps, s.prepareBreakpointSet(breakpoints), func(location DebugLocation) bool {
		if location.Synthetic {
			return false
		}
		current := s.vm.currentFrame()
		if current == startFrame {
			return !sameVisibleDebugLocation(start, &location)
		}
		return len(s.vm.frames)-1 < startDepth
	}, StopReasonStepOver)
	s.finishForwardTimeline()
	return s.Snapshot(false, 0)
}

func (s *DebugSession) StepOut(maxSteps int, breakpoints []Breakpoint) DebugSnapshot {
	return s.StepOutContext(context.Background(), maxSteps, breakpoints)
}

func (s *DebugSession) StepOutContext(ctx context.Context, maxSteps int, breakpoints []Breakpoint) DebugSnapshot {
	if s.vm == nil || s.vm.halted || s.vm.waitingInput != nil {
		return s.Snapshot(false, 0)
	}
	s.beginForwardTimeline()
	startFrame := s.vm.currentFrame()
	startDepth := len(s.vm.frames) - 1
	s.vm.runUntilContext(ctx, maxSteps, s.prepareBreakpointSet(breakpoints), func(location DebugLocation) bool {
		return !location.Synthetic && s.vm.currentFrame() != startFrame && len(s.vm.frames)-1 < startDepth
	}, StopReasonStepOut)
	s.finishForwardTimeline()
	return s.Snapshot(false, 0)
}

func (s *DebugSession) RunToCursor(maxSteps int, breakpoints []Breakpoint, position SourcePosition) (DebugSnapshot, error) {
	return s.RunToCursorContext(context.Background(), maxSteps, breakpoints, position)
}

func (s *DebugSession) RunToCursorContext(ctx context.Context, maxSteps int, breakpoints []Breakpoint, position SourcePosition) (DebugSnapshot, error) {
	if s.vm == nil || s.vm.halted || s.vm.waitingInput != nil {
		return s.Snapshot(false, 0), nil
	}
	targets := s.executableLocations(position)
	if len(targets) == 0 {
		return s.Snapshot(false, 0), ErrNoExecutableSourcePosition
	}
	s.beginForwardTimeline()
	start := s.vm.currentDebugLocation()
	s.vm.runUntilContext(ctx, maxSteps, s.prepareBreakpointSet(breakpoints), func(location DebugLocation) bool {
		key := debugInstructionKey{method: location.Method, instruction: location.Instruction}
		return targets[key] && !sameVisibleDebugLocation(start, &location)
	}, StopReasonRunToCursor)
	s.finishForwardTimeline()
	return s.Snapshot(false, 0), nil
}

func (s *DebugSession) Run(maxSteps int, breakpoints []int) DebugSnapshot {
	typed := make([]Breakpoint, 0, len(breakpoints))
	for _, line := range breakpoints {
		typed = append(typed, Breakpoint{Line: line})
	}
	return s.RunWithBreakpoints(maxSteps, typed)
}

func (s *DebugSession) RunWithBreakpoints(maxSteps int, breakpoints []Breakpoint) DebugSnapshot {
	return s.RunWithBreakpointsContext(context.Background(), maxSteps, breakpoints)
}

func (s *DebugSession) RunWithBreakpointsContext(ctx context.Context, maxSteps int, breakpoints []Breakpoint) DebugSnapshot {
	if s.vm != nil && !s.vm.halted {
		s.beginForwardTimeline()
		s.vm.RunContext(ctx, maxSteps, s.prepareBreakpointSet(breakpoints))
		s.finishForwardTimeline()
	}
	return s.Snapshot(false, 0)
}

func (s *DebugSession) Reset() DebugSnapshot {
	revision := uint64(0)
	discarded := false
	if s.reverse != nil {
		revision = s.reverse.revision
		discarded = s.vm != nil && s.vm.executed > 0
	}
	if !s.blocked && s.exec != nil && s.exec.verified {
		s.vm = newVMWithPolicy(s.exec, s.policy)
		s.vm.SetInput(s.input)
		s.vm.interactiveInput = s.interactiveInput
		s.vm.stopReason = StopReasonReady
		s.vm.watches = append([]WatchExpression(nil), s.watches...)
		s.vm.prepareBreakpoints(s.breakpoints)
		s.reverse = newReverseHistory(s.vm, s.reverseLimits)
		s.reverse.revision = revision + 1
		s.reverse.futureDiscarded = discarded
	}
	return s.Snapshot(false, 0)
}

func (s *DebugSession) Snapshot(light bool, traceFrom int) DebugSnapshot {
	result := cloneResult(s.base)
	status := "blocked"
	if s.vm != nil {
		s.ensureReverseHistory()
		runtime := s.vm.result(traceFrom)
		result.Runtime = runtime
		result.Diagnostics = append(result.Diagnostics, runtime.Diagnostics...)
		result.Stages = updateRuntimeStage(result.Stages, runtime)
		status = "ready"
		if runtime.Halted {
			status = "halted"
		} else if runtime.WaitingInput != nil {
			status = "waiting-input"
		} else if runtime.Paused {
			status = "paused"
		} else if runtime.ExecutedSteps > 0 {
			status = "running"
		}
		if light {
			result.Source = ""
			result.Tokens = nil
			result.AST = nil
			result.Symbols = nil
			result.IR = nil
			result.Assembly = nil
			result.AssemblySpans = nil
			result.ControlFlow = ControlFlowGraph{}
		}
		reason := s.vm.stopReason
		if reason == "" {
			reason = StopReasonReady
		}
		timeline := s.reverse.state(s.vm)
		return DebugSnapshot{
			Result: result, Runtime: runtime, Status: status, Location: s.vm.currentDebugLocation(), StopReason: reason,
			Breakpoints: s.vm.publicBreakpointStates(), Watches: s.vm.evaluateWatches(), DebugLog: s.vm.publicDebugLog(),
			Timeline: &timeline,
		}
	}
	result.Runtime = VMResult{Halted: true}
	return DebugSnapshot{Result: result, Runtime: result.Runtime, Status: status, StopReason: StopReasonRuntimeError}
}

type debugInstructionKey struct {
	method      string
	instruction int
}

func breakpointSet(breakpoints []Breakpoint) map[Breakpoint]bool {
	set := map[Breakpoint]bool{}
	for _, breakpoint := range breakpoints {
		if breakpoint.Line > 0 {
			set[breakpoint] = true
		}
	}
	return set
}

func (s *DebugSession) prepareBreakpointSet(breakpoints []Breakpoint) map[Breakpoint]bool {
	s.breakpoints = append([]Breakpoint(nil), breakpoints...)
	if s.vm == nil {
		return breakpointSet(breakpoints)
	}
	set := s.vm.prepareBreakpoints(breakpoints)
	s.ensureReverseHistory()
	s.reverse.noteBreakpoints(s.vm, breakpoints)
	return set
}

// SetWatches replaces the bounded persistent watch list. Evaluation remains
// lazy and only happens in debugger-stopped snapshots.
func (s *DebugSession) SetWatches(watches []WatchExpression) {
	if len(watches) > maxWatchExpressions {
		watches = watches[:maxWatchExpressions]
	}
	s.watches = append([]WatchExpression(nil), watches...)
	if s.vm != nil {
		s.vm.watches = append([]WatchExpression(nil), s.watches...)
	}
}

func sameVisibleDebugLocation(left, right *DebugLocation) bool {
	if left == nil || right == nil {
		return false
	}
	return left.Method == right.Method && left.FrameDepth == right.FrameDepth && sameDebugSpan(left.Span, right.Span)
}

func (s *DebugSession) executableLocations(position SourcePosition) map[debugInstructionKey]bool {
	result := map[debugInstructionKey]bool{}
	if s.exec == nil || position.SourceID == "" || position.Line <= 0 {
		return result
	}
	type candidate struct {
		key      debugInstructionKey
		location DebugLocation
	}
	candidates := []candidate{}
	for _, method := range s.exec.MethodOrder {
		for index := range method.Code {
			instruction := method.Code[index]
			span := instruction.Span
			synthetic := span.SourceID == "" || span.StartLine <= 0 || (method.Synthetic && sameDebugSpan(span, method.Span))
			if synthetic || span.SourceID != position.SourceID || position.Line < span.StartLine || position.Line > span.EndLine {
				continue
			}
			location := DebugLocation{SourceID: span.SourceID, Span: span, Method: method.DisplayName, Instruction: index, Synthetic: false}
			candidates = append(candidates, candidate{key: debugInstructionKey{method: method.DisplayName, instruction: index}, location: location})
		}
	}
	if position.Column <= 0 {
		for _, candidate := range candidates {
			result[candidate.key] = true
		}
		return result
	}
	bestContainedColumn := -1
	bestFollowingColumn := 0
	for _, candidate := range candidates {
		span := candidate.location.Span
		contains := position.Line > span.StartLine || position.Column >= span.StartColumn
		contains = contains && (position.Line < span.EndLine || position.Column < span.EndColumn)
		if contains && span.StartColumn > bestContainedColumn {
			bestContainedColumn = span.StartColumn
			result = map[debugInstructionKey]bool{candidate.key: true}
		} else if contains && span.StartColumn == bestContainedColumn {
			result[candidate.key] = true
		} else if bestContainedColumn < 0 && span.StartLine == position.Line && span.StartColumn >= position.Column {
			if bestFollowingColumn == 0 || span.StartColumn < bestFollowingColumn {
				bestFollowingColumn = span.StartColumn
				result = map[debugInstructionKey]bool{candidate.key: true}
			} else if span.StartColumn == bestFollowingColumn {
				result[candidate.key] = true
			}
		}
	}
	return result
}

func updateRuntimeStage(stages []Stage, runtime VMResult) []Stage {
	result := append([]Stage(nil), stages...)
	for index := range result {
		if result[index].ID != string(PhaseRuntime) {
			continue
		}
		result[index].Summary = summarizeCount(runtime.ExecutedSteps, "executed instruction", "executed instructions")
		switch {
		case hasStageErrors(runtime.Diagnostics, string(PhaseRuntime)):
			result[index].Status = "error"
		case runtime.WaitingInput != nil:
			result[index].Status = "waiting-input"
		case runtime.Paused:
			result[index].Status = "paused"
		case runtime.Halted:
			result[index].Status = "ok"
		case runtime.ExecutedSteps > 0:
			result[index].Status = "ready"
		}
		break
	}
	return result
}

func cloneResult(source Result) Result {
	result := source
	result.SourceUnits = append([]SourceUnitMetadata(nil), source.SourceUnits...)
	result.Stages = append([]Stage(nil), source.Stages...)
	result.Tokens = append([]Token(nil), source.Tokens...)
	result.Symbols = append([]Symbol(nil), source.Symbols...)
	result.IR = cloneIRInstructions(source.IR)
	result.Assembly = append([]string(nil), source.Assembly...)
	result.AssemblySpans = append([]Span(nil), source.AssemblySpans...)
	result.Diagnostics = append([]Diagnostic(nil), source.Diagnostics...)
	result.ControlFlow.Blocks = append([]ControlFlowBlock(nil), source.ControlFlow.Blocks...)
	result.ControlFlow.Edges = append([]ControlFlowEdge(nil), source.ControlFlow.Edges...)
	result.Visualizations = cloneVisualizations(source.Visualizations)
	result.Optimization = cloneOptimizationReport(source.Optimization)
	return result
}
