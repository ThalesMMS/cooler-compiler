// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
)

var (
	ErrReverseHistoryUnavailable = errors.New("reverse history is unavailable")
	ErrReverseTargetEvicted      = errors.New("reverse target has been evicted")
	ErrTimelineRevision          = errors.New("timeline revision is stale")
)

var defaultReverseHistoryLimits = ReverseHistoryLimits{
	CheckpointInterval: 32,
	MaxCheckpoints:     64,
	MaxCheckpointBytes: 16 << 20,
	MaxEvents:          8_192,
	MaxEventBytes:      2 << 20,
}

var maximumReverseHistoryLimits = ReverseHistoryLimits{
	CheckpointInterval: 10_000,
	MaxCheckpoints:     1_024,
	MaxCheckpointBytes: 256 << 20,
	MaxEvents:          1_000_000,
	MaxEventBytes:      64 << 20,
}

func DefaultReverseHistoryLimits() ReverseHistoryLimits { return defaultReverseHistoryLimits }
func MaximumReverseHistoryLimits() ReverseHistoryLimits { return maximumReverseHistoryLimits }

func NormalizeReverseHistoryLimits(limits ReverseHistoryLimits) (ReverseHistoryLimits, error) {
	fields := []struct {
		name              string
		value             *int
		fallback, maximum int
	}{
		{"checkpointInterval", &limits.CheckpointInterval, defaultReverseHistoryLimits.CheckpointInterval, maximumReverseHistoryLimits.CheckpointInterval},
		{"maxCheckpoints", &limits.MaxCheckpoints, defaultReverseHistoryLimits.MaxCheckpoints, maximumReverseHistoryLimits.MaxCheckpoints},
		{"maxCheckpointBytes", &limits.MaxCheckpointBytes, defaultReverseHistoryLimits.MaxCheckpointBytes, maximumReverseHistoryLimits.MaxCheckpointBytes},
		{"maxEvents", &limits.MaxEvents, defaultReverseHistoryLimits.MaxEvents, maximumReverseHistoryLimits.MaxEvents},
		{"maxEventBytes", &limits.MaxEventBytes, defaultReverseHistoryLimits.MaxEventBytes, maximumReverseHistoryLimits.MaxEventBytes},
	}
	for _, field := range fields {
		if *field.value == 0 {
			*field.value = field.fallback
		}
		minimum := 1
		if field.name == "maxCheckpointBytes" || field.name == "maxEventBytes" {
			minimum = 1 << 10
		}
		if *field.value < minimum || *field.value > field.maximum {
			return defaultReverseHistoryLimits, fmt.Errorf("reverse history %s must be between %d and %d", field.name, minimum, field.maximum)
		}
	}
	return limits, nil
}

func (s *DebugSession) ensureReverseHistory() {
	if s == nil || s.vm == nil {
		return
	}
	if s.reverse == nil {
		s.reverse = newReverseHistory(s.vm, s.reverseLimits)
	}
	if s.reverse.executableHash == "" {
		s.reverse.executableHash = reverseExecutableHash(s)
	}
	s.reverse.attach(s.vm)
}

func (s *DebugSession) beginForwardTimeline() {
	s.ensureReverseHistory()
	if s.reverse != nil {
		s.reverse.beginForward(s.vm)
	}
}

func (s *DebugSession) finishForwardTimeline() {
	if s.reverse != nil && s.vm != nil {
		s.reverse.finishForward(s.vm, s.vm.stopReason)
	}
}

// SetReverseHistoryLimits replaces the history policy and starts a new bounded
// timeline. It is intended for local tooling before execution begins.
func (s *DebugSession) SetReverseHistoryLimits(limits ReverseHistoryLimits) error {
	normalized, err := NormalizeReverseHistoryLimits(limits)
	if err != nil {
		return err
	}
	if s.vm != nil && s.vm.executed > 0 {
		return errors.New("reverse history limits can only change before execution")
	}
	s.reverseLimits = normalized
	if s.vm != nil {
		revision := uint64(1)
		if s.reverse != nil {
			revision = s.reverse.revision + 1
		}
		s.reverse = newReverseHistory(s.vm, normalized)
		s.reverse.revision = revision
	}
	return nil
}

func (s *DebugSession) ReverseTimeline() ReverseTimelineState {
	s.ensureReverseHistory()
	if s.reverse == nil {
		return ReverseTimelineState{Limits: defaultReverseHistoryLimits}
	}
	return s.reverse.state(s.vm)
}

func (s *DebugSession) StepBack(mode ReverseMode) (DebugSnapshot, error) {
	return s.StepBackContext(context.Background(), mode)
}

func (s *DebugSession) StepBackContext(ctx context.Context, mode ReverseMode) (DebugSnapshot, error) {
	s.ensureReverseHistory()
	if s.reverse == nil || s.vm == nil {
		return s.Snapshot(false, 0), ErrReverseHistoryUnavailable
	}
	if mode == "" {
		mode = ReverseModeInstruction
	}
	var target int
	var found bool
	switch mode {
	case ReverseModeInstruction:
		target = s.reverse.currentStep - 1
		found = target >= s.reverse.earliestStep()
	case ReverseModeSource:
		target, found = s.reverse.previousSourceStep(s.reverse.currentStep)
	default:
		return s.Snapshot(false, 0), fmt.Errorf("reverse mode must be instruction or source")
	}
	if !found {
		return s.Snapshot(false, 0), ErrReverseHistoryUnavailable
	}
	vm, err := s.reverse.restoreContext(ctx, target, StopReasonReverseStep)
	if err != nil {
		return s.Snapshot(false, 0), err
	}
	s.vm = vm
	s.vm.watches = append([]WatchExpression(nil), s.watches...)
	s.breakpoints = append([]Breakpoint(nil), s.vm.activeBreakpoints...)
	return s.Snapshot(false, 0), nil
}

func (s *DebugSession) ReverseContinue() (DebugSnapshot, error) {
	return s.ReverseContinueContext(context.Background())
}

func (s *DebugSession) ReverseContinueContext(ctx context.Context) (DebugSnapshot, error) {
	s.ensureReverseHistory()
	if s.reverse == nil || s.vm == nil {
		return s.Snapshot(false, 0), ErrReverseHistoryUnavailable
	}
	target, found := s.reverse.previousMeaningfulStep(s.reverse.currentStep)
	if !found {
		return s.Snapshot(false, 0), ErrReverseHistoryUnavailable
	}
	vm, err := s.reverse.restoreContext(ctx, target, StopReasonReverseContinue)
	if err != nil {
		return s.Snapshot(false, 0), err
	}
	s.vm = vm
	s.vm.watches = append([]WatchExpression(nil), s.watches...)
	s.breakpoints = append([]Breakpoint(nil), s.vm.activeBreakpoints...)
	return s.Snapshot(false, 0), nil
}

// SeekRetainedStep navigates the existing linear history without executing a
// new future. At a step with several non-instruction events, the latest retained
// checkpoint at that step is selected. Cancellation leaves the session intact.
func (s *DebugSession) SeekRetainedStep(ctx context.Context, target int, revision uint64) (DebugSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return s.Snapshot(false, 0), err
	}
	s.ensureReverseHistory()
	if s.reverse == nil || s.vm == nil {
		return s.Snapshot(false, 0), ErrReverseHistoryUnavailable
	}
	if revision != s.reverse.revision {
		return s.Snapshot(false, 0), ErrTimelineRevision
	}
	if target < 0 || target > s.reverse.latestStep {
		return s.Snapshot(false, 0), ErrReverseHistoryUnavailable
	}
	vm, err := s.reverse.restoreContext(ctx, target, StopReasonHistorySeek)
	if err != nil {
		return s.Snapshot(false, 0), err
	}
	s.vm = vm
	s.vm.watches = append([]WatchExpression(nil), s.watches...)
	s.breakpoints = append([]Breakpoint(nil), vm.activeBreakpoints...)
	return s.Snapshot(false, 0), nil
}

type reverseCheckpoint struct {
	step       int
	eventOrder uint64
	size       int
	vm         *VM
	reason     DebugStopReason
}

type reverseTraceEvent struct {
	order       uint64
	step        int
	op          string
	note        string
	span        Span
	instruction bool
	size        int
}

type reverseStop struct {
	step     int
	order    uint64
	reason   DebugStopReason
	location *DebugLocation
}

type reverseInput struct {
	step     int
	order    uint64
	sequence int
	line     string
	finished bool
}

type reverseHistory struct {
	limits            ReverseHistoryLimits
	checkpoints       []reverseCheckpoint
	checkpointBytes   int
	events            []reverseTraceEvent
	eventBytes        int
	eventOrder        uint64
	currentEventOrder uint64
	stops             []reverseStop
	inputs            []reverseInput
	revision          uint64
	currentStep       int
	latestStep        int
	futureDiscarded   bool
	replaying         bool
	breakpoints       []Breakpoint
	executableHash    string
}

func newReverseHistory(vm *VM, limits ReverseHistoryLimits) *reverseHistory {
	normalized, err := NormalizeReverseHistoryLimits(limits)
	if err != nil {
		normalized = defaultReverseHistoryLimits
	}
	history := &reverseHistory{limits: normalized, revision: 1}
	history.attach(vm)
	if vm != nil {
		history.currentStep = vm.executed
		history.latestStep = vm.executed
		history.captureCheckpoint(vm, StopReasonReady)
		history.stops = append(history.stops, reverseStop{step: vm.executed, order: history.eventOrder, reason: StopReasonReady, location: cloneDebugLocation(vm.currentDebugLocation())})
	}
	return history
}

func (history *reverseHistory) attach(vm *VM) {
	if vm == nil {
		return
	}
	vm.reverseHook = func(current *VM, state VMState) {
		history.record(current, state)
	}
}

func (history *reverseHistory) record(vm *VM, state VMState) {
	if history == nil || history.replaying {
		return
	}
	history.eventOrder++
	size := 64 + len(state.Op) + len(state.Note) + len(state.Method) + len(state.Span.SourceID)
	event := reverseTraceEvent{
		order: history.eventOrder, step: state.Step, op: state.Op, note: state.Note,
		span: state.Span, instruction: vm.instructionRecord, size: size,
	}
	history.events = append(history.events, event)
	history.eventBytes += size
	history.currentEventOrder = history.eventOrder
	history.currentStep = vm.executed
	if vm.executed > history.latestStep {
		history.latestStep = vm.executed
	}
	if vm.instructionRecord && vm.executed > 0 && vm.executed%history.limits.CheckpointInterval == 0 {
		history.captureCheckpoint(vm, "")
	}
	history.enforceBounds(vm)
}

func (history *reverseHistory) captureCheckpoint(vm *VM, reason DebugStopReason) {
	if history == nil || vm == nil || history.replaying {
		return
	}
	checkpointVM := cloneMutableVM(vm)
	size := approximateCheckpointBytes(checkpointVM)
	if size > history.limits.MaxCheckpointBytes {
		for index := range history.checkpoints {
			history.checkpoints[index].vm = nil
		}
		history.checkpoints = nil
		history.checkpointBytes = 0
		history.events = nil
		history.eventBytes = 0
		history.stops = nil
		history.inputs = nil
		history.currentStep = vm.executed
		return
	}
	checkpoint := reverseCheckpoint{step: vm.executed, eventOrder: history.eventOrder, size: size, vm: checkpointVM, reason: reason}
	if len(history.checkpoints) > 0 {
		last := history.checkpoints[len(history.checkpoints)-1]
		if last.step == checkpoint.step && last.eventOrder == checkpoint.eventOrder {
			history.checkpointBytes -= last.size
			history.checkpoints[len(history.checkpoints)-1] = checkpoint
			history.checkpointBytes += size
			history.enforceCheckpointBounds()
			return
		}
	}
	history.checkpoints = append(history.checkpoints, checkpoint)
	history.checkpointBytes += size
	history.enforceCheckpointBounds()
}

func (history *reverseHistory) enforceCheckpointBounds() {
	for len(history.checkpoints) > 1 && (len(history.checkpoints) > history.limits.MaxCheckpoints || history.checkpointBytes > history.limits.MaxCheckpointBytes) {
		history.checkpointBytes -= history.checkpoints[0].size
		history.checkpoints[0].vm = nil
		history.checkpoints = history.checkpoints[1:]
	}
	history.trimBeforeEarliest()
}

func (history *reverseHistory) enforceBounds(vm *VM) {
	if len(history.events) <= history.limits.MaxEvents && history.eventBytes <= history.limits.MaxEventBytes {
		return
	}
	history.captureCheckpoint(vm, "")
	if len(history.checkpoints) > 1 {
		latest := history.checkpoints[len(history.checkpoints)-1]
		for index := range history.checkpoints[:len(history.checkpoints)-1] {
			history.checkpoints[index].vm = nil
		}
		history.checkpoints = []reverseCheckpoint{latest}
		history.checkpointBytes = latest.size
	}
	history.trimBeforeEarliest()
}

func (history *reverseHistory) trimBeforeEarliest() {
	if len(history.checkpoints) == 0 {
		return
	}
	earliest := history.checkpoints[0].step
	for len(history.events) > 0 && history.events[0].step < earliest {
		history.eventBytes -= history.events[0].size
		history.events = history.events[1:]
	}
	for len(history.stops) > 0 && history.stops[0].step < earliest {
		history.stops = history.stops[1:]
	}
	for len(history.inputs) > 0 && history.inputs[0].step <= earliest {
		history.inputs = history.inputs[1:]
	}
	history.recountEventBytes()
}

func (history *reverseHistory) beginForward(vm *VM) {
	if history == nil || vm == nil {
		return
	}
	if history.currentStep < history.latestStep {
		cutStep := history.currentStep
		cutOrder := history.currentEventOrder
		history.checkpoints = filterCheckpointsThrough(history.checkpoints, cutStep, cutOrder)
		history.recountCheckpointBytes()
		history.events = filterEventsThrough(history.events, cutStep, cutOrder)
		history.recountEventBytes()
		history.stops = filterStopsThrough(history.stops, cutStep, cutOrder)
		history.inputs = filterInputsThrough(history.inputs, cutStep, cutOrder)
		history.eventOrder = cutOrder
		history.captureCheckpoint(vm, vm.stopReason)
		history.latestStep = cutStep
		history.revision++
		history.futureDiscarded = true
	}
	history.attach(vm)
}

func (history *reverseHistory) finishForward(vm *VM, reason DebugStopReason) {
	if history == nil || vm == nil {
		return
	}
	history.currentStep = vm.executed
	if vm.executed > history.latestStep {
		history.latestStep = vm.executed
	}
	if meaningfulReverseStop(reason) {
		history.captureCheckpoint(vm, reason)
		history.stops = append(history.stops, reverseStop{step: vm.executed, order: history.eventOrder, reason: reason, location: cloneDebugLocation(vm.currentDebugLocation())})
	}
	history.enforceBounds(vm)
}

func meaningfulReverseStop(reason DebugStopReason) bool {
	switch reason {
	case StopReasonStepInto, StopReasonStepOver, StopReasonStepOut, StopReasonRunToCursor,
		StopReasonBreakpoint, StopReasonWaitingInput, StopReasonInstructionLimit,
		StopReasonRuntimeError, StopReasonHalt, StopReasonCancelled:
		return true
	default:
		return false
	}
}

func (history *reverseHistory) noteInput(step, sequence int, line string) {
	if history == nil {
		return
	}
	history.inputs = append(history.inputs, reverseInput{step: step, order: history.eventOrder, sequence: sequence, line: line})
	history.eventBytes += 24 + len(line)
}

func (history *reverseHistory) noteFinishedInput(step, sequence int) {
	if history == nil {
		return
	}
	history.inputs = append(history.inputs, reverseInput{step: step, order: history.eventOrder, sequence: sequence, finished: true})
	history.eventBytes += 24
}

func (history *reverseHistory) noteBreakpoints(vm *VM, breakpoints []Breakpoint) {
	if history == nil || reflect.DeepEqual(history.breakpoints, breakpoints) {
		return
	}
	history.breakpoints = append([]Breakpoint(nil), breakpoints...)
	history.captureCheckpoint(vm, "")
}

func (history *reverseHistory) restoreContext(ctx context.Context, target int, reason DebugStopReason) (*VM, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if history == nil || len(history.checkpoints) == 0 {
		return nil, ErrReverseHistoryUnavailable
	}
	if target < history.checkpoints[0].step {
		return nil, ErrReverseTargetEvicted
	}
	checkpointIndex := -1
	for index := range history.checkpoints {
		if history.checkpoints[index].step <= target {
			checkpointIndex = index
		}
	}
	if checkpointIndex < 0 {
		return nil, ErrReverseTargetEvicted
	}
	checkpoint := history.checkpoints[checkpointIndex]
	if target-checkpoint.step > maximumReverseHistoryLimits.CheckpointInterval {
		return nil, ErrReverseHistoryUnavailable
	}
	vm := cloneMutableVM(checkpoint.vm)
	history.replaying = true
	defer func() {
		history.replaying = false
		history.attach(vm)
	}()
	stepsWithoutProgress := 0
	for vm.executed < target && !vm.halted {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		before := vm.executed
		if vm.waitingInput != nil {
			input, found := history.inputFor(vm.waitingInput.Sequence, target)
			if !found {
				break
			}
			if input.finished {
				vm.finishInteractiveInput()
			} else if err := vm.provideInputLine(input.line, vm.waitingInput.Sequence); err != nil {
				return nil, err
			}
		}
		frame := vm.currentFrame()
		if frame == nil || frame.ip < 0 || frame.ip >= len(frame.method.Code) {
			break
		}
		if !vm.skipBreakpoint {
			triggered := vm.handleBreakpoints(frame.method.Code[frame.ip], breakpointSet(vm.activeBreakpoints))
			if triggered {
				vm.skipBreakpoint = true
				vm.paused = false
				vm.stopReason = ""
				stepsWithoutProgress++
				if stepsWithoutProgress > 2 {
					return nil, errors.New("reverse replay made no progress at breakpoint")
				}
				continue
			}
		}
		vm.paused = false
		vm.stopReason = ""
		vm.skipBreakpoint = false
		vm.executeOne()
		if vm.executed == before {
			stepsWithoutProgress++
			if stepsWithoutProgress > 2 {
				break
			}
		} else {
			stepsWithoutProgress = 0
		}
	}
	if vm.executed != target {
		return nil, fmt.Errorf("could not replay to instruction %d from checkpoint %d", target, checkpoint.step)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !vm.halted && vm.waitingInput == nil {
		vm.paused = true
	}
	vm.stopReason = reason
	history.currentStep = target
	history.currentEventOrder = checkpoint.eventOrder
	for _, event := range history.events {
		if event.step <= target && event.order > history.currentEventOrder {
			history.currentEventOrder = event.order
		}
	}
	return vm, nil
}

func (history *reverseHistory) inputFor(sequence, target int) (reverseInput, bool) {
	for _, input := range history.inputs {
		if input.sequence == sequence && input.step <= target {
			return input, true
		}
	}
	return reverseInput{}, false
}

func (history *reverseHistory) previousSourceStep(current int) (int, bool) {
	for index := len(history.stops) - 1; index >= 0; index-- {
		stop := history.stops[index]
		if stop.step >= current || stop.location == nil || stop.location.Synthetic {
			continue
		}
		switch stop.reason {
		case StopReasonStepInto, StopReasonStepOver, StopReasonStepOut, StopReasonRunToCursor, StopReasonBreakpoint:
			return stop.step, true
		}
	}
	if len(history.checkpoints) > 0 && history.checkpoints[0].step < current {
		return history.checkpoints[0].step, true
	}
	return 0, false
}

func (history *reverseHistory) previousMeaningfulStep(current int) (int, bool) {
	for index := len(history.stops) - 1; index >= 0; index-- {
		if history.stops[index].step < current {
			return history.stops[index].step, true
		}
	}
	return 0, false
}

func (history *reverseHistory) state(vm *VM) ReverseTimelineState {
	earliest := history.earliestStep()
	_, canContinue := history.previousMeaningfulStep(history.currentStep)
	return ReverseTimelineState{
		Revision: history.revision, CurrentStep: history.currentStep, LatestStep: history.latestStep,
		EarliestReversibleStep: earliest, CanStepBack: history.currentStep > earliest,
		CanReverseContinue: canContinue, FutureDiscarded: history.futureDiscarded,
		CheckpointCount: len(history.checkpoints), CheckpointBytes: history.checkpointBytes,
		EventCount: len(history.events), EventBytes: history.eventBytes, StateHash: reverseStateHash(vm),
		ExecutableHash: history.executableHash, Limits: history.limits,
	}
}

func reverseExecutableHash(session *DebugSession) string {
	if session == nil {
		return ""
	}
	payload := struct {
		SchemaVersion   string               `json:"schemaVersion"`
		CompilerVersion string               `json:"compilerVersion"`
		CompilerCommit  string               `json:"compilerCommit"`
		SourceUnits     []SourceUnitMetadata `json:"sourceUnits"`
		Policy          VMPolicy             `json:"policy"`
		Source          string               `json:"source,omitempty"`
	}{
		SchemaVersion: session.base.SchemaVersion, CompilerVersion: session.base.CompilerVersion,
		CompilerCommit: session.base.CompilerCommit, SourceUnits: session.base.SourceUnits, Policy: session.policy,
	}
	if len(payload.SourceUnits) == 0 {
		payload.Source = session.source
	}
	encoded, _ := json.Marshal(payload)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func (history *reverseHistory) earliestStep() int {
	if history == nil || len(history.checkpoints) == 0 {
		if history != nil {
			return history.currentStep
		}
		return 0
	}
	return history.checkpoints[0].step
}

func cloneDebugLocation(location *DebugLocation) *DebugLocation {
	if location == nil {
		return nil
	}
	copy := *location
	return &copy
}

func filterCheckpointsThrough(items []reverseCheckpoint, step int, order uint64) []reverseCheckpoint {
	kept := 0
	for index := range items {
		item := items[index]
		if item.step < step || (item.step == step && item.eventOrder <= order) {
			items[kept] = item
			kept++
		}
	}
	for index := kept; index < len(items); index++ {
		items[index].vm = nil
	}
	return items[:kept]
}

func filterEventsThrough(items []reverseTraceEvent, step int, order uint64) []reverseTraceEvent {
	result := items[:0]
	for _, item := range items {
		if item.step < step || (item.step == step && item.order <= order) {
			result = append(result, item)
		}
	}
	return result
}

func filterStopsThrough(items []reverseStop, step int, order uint64) []reverseStop {
	result := items[:0]
	for _, item := range items {
		if item.step < step || (item.step == step && item.order <= order) {
			result = append(result, item)
		}
	}
	return result
}

func filterInputsThrough(items []reverseInput, step int, order uint64) []reverseInput {
	result := items[:0]
	for _, item := range items {
		if item.step < step || (item.step == step && item.order <= order) {
			result = append(result, item)
		}
	}
	return result
}

func (history *reverseHistory) recountCheckpointBytes() {
	history.checkpointBytes = 0
	for _, checkpoint := range history.checkpoints {
		history.checkpointBytes += checkpoint.size
	}
}

func (history *reverseHistory) recountEventBytes() {
	history.eventBytes = 0
	for _, event := range history.events {
		history.eventBytes += event.size
	}
	for _, input := range history.inputs {
		history.eventBytes += 24 + len(input.line)
	}
}
