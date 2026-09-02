// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	internal "github.com/ThalesMMS/cooler-compiler/internal/compiler"
)

var (
	// ErrNotRunning means Pause was requested without an active run operation.
	ErrNotRunning = errors.New("compiler: debugger is not running")
	// ErrNoExecutablePosition means a cursor has no corresponding instruction.
	ErrNoExecutablePosition = errors.New("compiler: source position is not executable")
	// ErrInputNotWaiting means live input was provided while the VM was not
	// blocked.
	ErrInputNotWaiting = errors.New("compiler: VM is not waiting for input")
	// ErrStaleInput means an input sequence no longer names the active request.
	ErrStaleInput = errors.New("compiler: input request is stale")
	// ErrInputLimit means a configured line or byte input ceiling was exceeded.
	ErrInputLimit = errors.New("compiler: VM input limit exceeded")
	// ErrHeapObjectNotFound means a heap object ID is absent from this session.
	ErrHeapObjectNotFound = errors.New("compiler: heap object not found")
	// ErrReverseUnavailable means requested history is disabled or was evicted.
	ErrReverseUnavailable = errors.New("compiler: reverse history is unavailable")
)

// Debugger is the stable core debugger contract. Concrete DebugSession values
// may also implement HeapInspector, WatchController, and ReverseDebugger.
type Debugger interface {
	Snapshot(context.Context, bool, int) (DebugSnapshot, error)
	StepInstruction(context.Context) (DebugSnapshot, error)
	StepInto(context.Context, int, []Breakpoint) (DebugSnapshot, error)
	StepOver(context.Context, int, []Breakpoint) (DebugSnapshot, error)
	StepOut(context.Context, int, []Breakpoint) (DebugSnapshot, error)
	Run(context.Context, int, []Breakpoint) (DebugSnapshot, error)
	RunToCursor(context.Context, int, []Breakpoint, SourcePosition) (DebugSnapshot, error)
	Pause() error
	Reset(context.Context) (DebugSnapshot, error)
	SetInput(context.Context, string) error
	Close() error
}

// DebugSession serializes all VM mutations. A Run may be interrupted from a
// different goroutine with Pause or Close; snapshots returned before/after an
// operation are detached values.
type DebugSession struct {
	op        chan struct{}
	stateMu   sync.Mutex
	raw       *internal.DebugSession
	options   Options
	closed    bool
	runCancel context.CancelFunc
	runID     uint64
}

func newDebugSession(raw *internal.DebugSession, options Options) *DebugSession {
	session := &DebugSession{op: make(chan struct{}, 1), raw: raw, options: options}
	session.op <- struct{}{}
	return session
}

// NewDebugSession compiles a stopped project and returns its debugger without
// executing Main.main.
func NewDebugSession(ctx context.Context, project Project, options Options) (debugger *DebugSession, err error) {
	options.Mode = ModeRun
	compilation, err := NewSession(ctx, project, options)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, compilation.Close()) }()
	_, err = compilation.Complete(ctx)
	if err != nil {
		return nil, err
	}
	debugger, err = compilation.Debugger(ctx)
	if err != nil {
		return nil, err
	}
	if err := debugger.SetInput(ctx, compilation.options.Input); err != nil {
		_ = debugger.Close()
		return nil, err
	}
	return debugger, nil
}

// Snapshot returns detached debugger state. A light snapshot omits static
// artifacts, and traceFrom requests only trace entries at or after that offset.
func (d *DebugSession) Snapshot(ctx context.Context, light bool, traceFrom int) (DebugSnapshot, error) {
	if traceFrom < 0 {
		return DebugSnapshot{}, fmt.Errorf("%w: trace offset cannot be negative", ErrInvalidOptions)
	}
	raw, release, err := d.acquire(ctx)
	if err != nil {
		return DebugSnapshot{}, err
	}
	defer release()
	return publicDebugSnapshot(raw.Snapshot(light, traceFrom))
}

// StepInstruction executes at most one VM instruction.
func (d *DebugSession) StepInstruction(ctx context.Context) (DebugSnapshot, error) {
	return d.simpleSnapshotOperation(ctx, func(raw *internal.DebugSession) internal.DebugSnapshot { return raw.StepInstruction() })
}

// StepInto runs to the next executable source position, including inside calls.
func (d *DebugSession) StepInto(ctx context.Context, maxSteps int, breakpoints []Breakpoint) (DebugSnapshot, error) {
	return d.contextOperation(ctx, maxSteps, breakpoints, func(raw *internal.DebugSession, operation context.Context, limit int, points []internal.Breakpoint) internal.DebugSnapshot {
		return raw.StepIntoContext(operation, limit, points)
	})
}

// StepOver runs to the next executable position outside any deeper call.
func (d *DebugSession) StepOver(ctx context.Context, maxSteps int, breakpoints []Breakpoint) (DebugSnapshot, error) {
	return d.contextOperation(ctx, maxSteps, breakpoints, func(raw *internal.DebugSession, operation context.Context, limit int, points []internal.Breakpoint) internal.DebugSnapshot {
		return raw.StepOverContext(operation, limit, points)
	})
}

// StepOut runs until execution returns from the current frame.
func (d *DebugSession) StepOut(ctx context.Context, maxSteps int, breakpoints []Breakpoint) (DebugSnapshot, error) {
	return d.contextOperation(ctx, maxSteps, breakpoints, func(raw *internal.DebugSession, operation context.Context, limit int, points []internal.Breakpoint) internal.DebugSnapshot {
		return raw.StepOutContext(operation, limit, points)
	})
}

// Run executes until halt, a breakpoint, live input, cancellation, runtime
// failure, or the maxSteps budget is reached.
func (d *DebugSession) Run(ctx context.Context, maxSteps int, breakpoints []Breakpoint) (DebugSnapshot, error) {
	return d.contextOperation(ctx, maxSteps, breakpoints, func(raw *internal.DebugSession, operation context.Context, limit int, points []internal.Breakpoint) internal.DebugSnapshot {
		return raw.RunWithBreakpointsContext(operation, limit, points)
	})
}

// RunToCursor executes until an instruction mapped to position is reached.
func (d *DebugSession) RunToCursor(ctx context.Context, maxSteps int, breakpoints []Breakpoint, position SourcePosition) (DebugSnapshot, error) {
	if position.SourceID == "" || position.Line < 1 || position.Column < 0 {
		return DebugSnapshot{}, fmt.Errorf("%w: invalid source position", ErrInvalidOptions)
	}
	limit, points, err := d.validateRun(maxSteps, breakpoints)
	if err != nil {
		return DebugSnapshot{}, err
	}
	raw, release, err := d.acquire(ctx)
	if err != nil {
		return DebugSnapshot{}, err
	}
	defer release()
	operation, finish := d.runningContext(ctx)
	snapshot, actionErr := raw.RunToCursorContext(operation, limit, points, internal.SourcePosition{SourceID: position.SourceID, Line: position.Line, Column: position.Column})
	operationErr := operation.Err()
	finish()
	public, convertErr := publicDebugSnapshot(snapshot)
	if convertErr != nil {
		return DebugSnapshot{}, convertErr
	}
	if actionErr != nil {
		return public, publicDebugError(actionErr)
	}
	if operationErr != nil {
		return public, operationErr
	}
	return public, nil
}

// Pause interrupts an active Run or source-stepping operation. It is the only
// mutation intentionally allowed concurrently with another operation.
func (d *DebugSession) Pause() error {
	d.stateMu.Lock()
	defer d.stateMu.Unlock()
	if d.closed {
		return ErrClosed
	}
	if d.runCancel == nil {
		return ErrNotRunning
	}
	d.runCancel()
	return nil
}

// Reset restores the stopped VM and configured input while clearing execution
// state and branching reverse history.
func (d *DebugSession) Reset(ctx context.Context) (DebugSnapshot, error) {
	return d.simpleSnapshotOperation(ctx, func(raw *internal.DebugSession) internal.DebugSnapshot { return raw.Reset() })
}

// SetInput replaces pre-supplied program input for the next Reset or execution.
func (d *DebugSession) SetInput(ctx context.Context, input string) error {
	raw, release, err := d.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	raw.SetInput(input)
	return nil
}

// SetInteractiveInput enables or disables suspension at exhausted IO input.
func (d *DebugSession) SetInteractiveInput(ctx context.Context, enabled bool) error {
	raw, release, err := d.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	raw.SetInteractiveInput(enabled)
	return nil
}

// ProvideInputLine satisfies the active live input request when sequence is
// current, then resumes only the waiting builtin.
func (d *DebugSession) ProvideInputLine(ctx context.Context, line string, sequence int) (DebugSnapshot, error) {
	if sequence < 1 {
		return DebugSnapshot{}, fmt.Errorf("%w: input sequence must be positive", ErrInvalidOptions)
	}
	raw, release, err := d.acquire(ctx)
	if err != nil {
		return DebugSnapshot{}, err
	}
	defer release()
	snapshot, actionErr := raw.ProvideInputLine(line, sequence)
	public, convertErr := publicDebugSnapshot(snapshot)
	if convertErr != nil {
		return DebugSnapshot{}, convertErr
	}
	return public, publicDebugError(actionErr)
}

// FinishInteractiveInput resolves an active request using deterministic EOF
// fallback behavior and disables further live input suspension.
func (d *DebugSession) FinishInteractiveInput(ctx context.Context) (DebugSnapshot, error) {
	return d.simpleSnapshotOperation(ctx, func(raw *internal.DebugSession) internal.DebugSnapshot { return raw.FinishInteractiveInput() })
}

// InputEvents returns detached input history. Content is redacted unless
// includeContent is true.
func (d *DebugSession) InputEvents(ctx context.Context, includeContent bool) ([]InputEvent, error) {
	raw, release, err := d.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	return convertJSON[[]InputEvent](raw.InputEvents(includeContent))
}

// SetWatches replaces the bounded watch-expression set evaluated at each stop.
func (d *DebugSession) SetWatches(ctx context.Context, watches []WatchExpression) error {
	raw, release, err := d.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	converted, err := convertJSON[[]internal.WatchExpression](watches)
	if err != nil {
		return err
	}
	raw.SetWatches(converted)
	return nil
}

// Heap returns a bounded, read-only heap projection for the requested mode.
func (d *DebugSession) Heap(ctx context.Context, query HeapQuery) (HeapSnapshot, error) {
	raw, release, err := d.acquire(ctx)
	if err != nil {
		return HeapSnapshot{}, err
	}
	defer release()
	converted, err := convertJSON[internal.HeapQuery](query)
	if err != nil {
		return HeapSnapshot{}, err
	}
	snapshot, actionErr := raw.Heap(converted)
	if actionErr != nil {
		return HeapSnapshot{}, publicDebugError(actionErr)
	}
	return convertJSON[HeapSnapshot](snapshot)
}

// StepBack restores the previous retained instruction or source position.
func (d *DebugSession) StepBack(ctx context.Context, mode ReverseMode) (DebugSnapshot, error) {
	if mode != "" && mode != ReverseInstruction && mode != ReverseSource {
		return DebugSnapshot{}, fmt.Errorf("%w: reverse mode must be instruction or source", ErrInvalidOptions)
	}
	raw, release, err := d.acquire(ctx)
	if err != nil {
		return DebugSnapshot{}, err
	}
	defer release()
	snapshot, actionErr := raw.StepBack(internal.ReverseMode(mode))
	public, convertErr := publicDebugSnapshot(snapshot)
	if convertErr != nil {
		return DebugSnapshot{}, convertErr
	}
	return public, publicDebugError(actionErr)
}

// ReverseContinue restores the nearest earlier meaningful debugger stop.
func (d *DebugSession) ReverseContinue(ctx context.Context) (DebugSnapshot, error) {
	raw, release, err := d.acquire(ctx)
	if err != nil {
		return DebugSnapshot{}, err
	}
	defer release()
	snapshot, actionErr := raw.ReverseContinue()
	public, convertErr := publicDebugSnapshot(snapshot)
	if convertErr != nil {
		return DebugSnapshot{}, convertErr
	}
	return public, publicDebugError(actionErr)
}

// Close interrupts active execution, waits for it to release the serialized
// operation, releases VM state, and is idempotent.
func (d *DebugSession) Close() error {
	d.stateMu.Lock()
	if d.closed {
		d.stateMu.Unlock()
		return nil
	}
	if d.runCancel != nil {
		d.runCancel()
	}
	d.stateMu.Unlock()
	<-d.op
	d.stateMu.Lock()
	d.closed = true
	d.raw = nil
	d.stateMu.Unlock()
	d.op <- struct{}{}
	return nil
}

func (d *DebugSession) simpleSnapshotOperation(ctx context.Context, action func(*internal.DebugSession) internal.DebugSnapshot) (DebugSnapshot, error) {
	raw, release, err := d.acquire(ctx)
	if err != nil {
		return DebugSnapshot{}, err
	}
	defer release()
	return publicDebugSnapshot(action(raw))
}

func (d *DebugSession) contextOperation(ctx context.Context, maxSteps int, breakpoints []Breakpoint, action func(*internal.DebugSession, context.Context, int, []internal.Breakpoint) internal.DebugSnapshot) (DebugSnapshot, error) {
	limit, points, err := d.validateRun(maxSteps, breakpoints)
	if err != nil {
		return DebugSnapshot{}, err
	}
	raw, release, err := d.acquire(ctx)
	if err != nil {
		return DebugSnapshot{}, err
	}
	defer release()
	operation, finish := d.runningContext(ctx)
	snapshot := action(raw, operation, limit, points)
	operationErr := operation.Err()
	finish()
	public, err := publicDebugSnapshot(snapshot)
	if err != nil {
		return DebugSnapshot{}, err
	}
	if operationErr != nil {
		return public, operationErr
	}
	return public, nil
}

func (d *DebugSession) acquire(ctx context.Context) (*internal.DebugSession, func(), error) {
	if err := contextError(ctx); err != nil {
		return nil, nil, err
	}
	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case <-d.op:
	}
	d.stateMu.Lock()
	if d.closed || d.raw == nil {
		d.stateMu.Unlock()
		d.op <- struct{}{}
		return nil, nil, ErrClosed
	}
	raw := d.raw
	d.stateMu.Unlock()
	return raw, func() { d.op <- struct{}{} }, nil
}

func (d *DebugSession) runningContext(parent context.Context) (context.Context, func()) {
	operation, cancel := context.WithCancel(parent)
	d.stateMu.Lock()
	d.runID++
	id := d.runID
	d.runCancel = cancel
	d.stateMu.Unlock()
	return operation, func() {
		cancel()
		d.stateMu.Lock()
		if d.runID == id {
			d.runCancel = nil
		}
		d.stateMu.Unlock()
	}
}

func (d *DebugSession) validateRun(maxSteps int, breakpoints []Breakpoint) (int, []internal.Breakpoint, error) {
	if maxSteps == 0 {
		maxSteps = d.options.MaxSteps
	}
	if maxSteps < 1 || maxSteps > d.options.Limits.MaxInstructionsPerRun {
		return 0, nil, fmt.Errorf("%w: max steps must be between 1 and %d", ErrInvalidOptions, d.options.Limits.MaxInstructionsPerRun)
	}
	if len(breakpoints) > d.options.Limits.MaxBreakpoints {
		return 0, nil, fmt.Errorf("%w: too many breakpoints", ErrInvalidOptions)
	}
	for _, breakpoint := range breakpoints {
		if breakpoint.Line < 1 || breakpoint.Column < 0 {
			return 0, nil, fmt.Errorf("%w: invalid breakpoint position", ErrInvalidOptions)
		}
	}
	points, err := convertJSON[[]internal.Breakpoint](breakpoints)
	return maxSteps, points, err
}

func publicDebugSnapshot(snapshot internal.DebugSnapshot) (DebugSnapshot, error) {
	result, err := resultFromInternal(snapshot.Result)
	if err != nil {
		return DebugSnapshot{}, err
	}
	runtime, err := convertJSON[RuntimeResult](snapshot.Runtime)
	if err != nil {
		return DebugSnapshot{}, err
	}
	location, err := convertJSON[*DebugLocation](snapshot.Location)
	if err != nil {
		return DebugSnapshot{}, err
	}
	breakpoints, err := convertJSON[[]BreakpointState](snapshot.Breakpoints)
	if err != nil {
		return DebugSnapshot{}, err
	}
	watches, err := convertJSON[[]WatchResult](snapshot.Watches)
	if err != nil {
		return DebugSnapshot{}, err
	}
	debugLog, err := convertJSON[[]DebugLogEntry](snapshot.DebugLog)
	if err != nil {
		return DebugSnapshot{}, err
	}
	timeline, err := convertJSON[*ReverseTimeline](snapshot.Timeline)
	if err != nil {
		return DebugSnapshot{}, err
	}
	return DebugSnapshot{
		Result: result, Runtime: runtime, Status: snapshot.Status, Location: location,
		StopReason: StopReason(snapshot.StopReason), Breakpoints: breakpoints,
		Watches: watches, DebugLog: debugLog, Timeline: timeline,
	}, nil
}

func convertJSON[T any](value any) (T, error) {
	var converted T
	encoded, err := json.Marshal(value)
	if err != nil {
		return converted, fmt.Errorf("%w: convert public value: %w", ErrInternal, err)
	}
	if err := json.Unmarshal(encoded, &converted); err != nil {
		return converted, fmt.Errorf("%w: convert public value: %w", ErrInternal, err)
	}
	return converted, nil
}

func publicDebugError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, internal.ErrNoExecutableSourcePosition):
		return fmt.Errorf("%w: %w", ErrNoExecutablePosition, err)
	case errors.Is(err, internal.ErrInputNotWaiting):
		return fmt.Errorf("%w: %w", ErrInputNotWaiting, err)
	case errors.Is(err, internal.ErrInputSequence):
		return fmt.Errorf("%w: %w", ErrStaleInput, err)
	case errors.Is(err, internal.ErrInputLimit):
		return fmt.Errorf("%w: %w", ErrInputLimit, err)
	case errors.Is(err, internal.ErrHeapObjectNotFound):
		return fmt.Errorf("%w: %w", ErrHeapObjectNotFound, err)
	case errors.Is(err, internal.ErrReverseHistoryUnavailable), errors.Is(err, internal.ErrReverseTargetEvicted):
		return fmt.Errorf("%w: %w", ErrReverseUnavailable, err)
	default:
		return fmt.Errorf("compiler: debugger operation: %w", err)
	}
}

var (
	_ Debugger        = (*DebugSession)(nil)
	_ HeapInspector   = (*DebugSession)(nil)
	_ WatchController = (*DebugSession)(nil)
	_ ReverseDebugger = (*DebugSession)(nil)
)
