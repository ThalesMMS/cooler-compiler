// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"context"
	"fmt"
	"strconv"
)

type valueKind uint8

const (
	valueVoid valueKind = iota
	valueInt
	valueString
	valueBool
	valueObject
)

type runtimeValueInternal struct {
	kind valueKind
	i    coolInt
	s    string
	b    bool
	o    *runtimeObject
}

type runtimeObject struct {
	id             int
	class          string
	fields         map[string]runtimeValueInternal
	allocationSpan Span
}

func voidValue() runtimeValueInternal           { return runtimeValueInternal{kind: valueVoid} }
func intValue(v coolInt) runtimeValueInternal   { return runtimeValueInternal{kind: valueInt, i: v} }
func stringValue(v string) runtimeValueInternal { return runtimeValueInternal{kind: valueString, s: v} }
func boolValue(v bool) runtimeValueInternal     { return runtimeValueInternal{kind: valueBool, b: v} }
func objectValue(v *runtimeObject) runtimeValueInternal {
	if v == nil {
		return voidValue()
	}
	return runtimeValueInternal{kind: valueObject, o: v}
}

func (v runtimeValueInternal) typeName() string {
	switch v.kind {
	case valueInt:
		return typeInt
	case valueString:
		return typeString
	case valueBool:
		return typeBool
	case valueObject:
		if v.o != nil {
			return v.o.class
		}
	}
	return "<void>"
}

func (v runtimeValueInternal) isVoid() bool {
	return v.kind == valueVoid || (v.kind == valueObject && v.o == nil)
}

func (v runtimeValueInternal) public() RuntimeValue {
	result := RuntimeValue{Type: v.typeName()}
	switch v.kind {
	case valueVoid:
		result.Type = "Void"
		result.Display = "void"
	case valueInt:
		result.Display = v.i.String()
	case valueString:
		result.Display = strconv.Quote(v.s)
	case valueBool:
		result.Display = strconv.FormatBool(v.b)
	case valueObject:
		if v.o == nil {
			result.Type = "Void"
			result.Display = "void"
		} else {
			result.Display = fmt.Sprintf("%s#%d", v.o.class, v.o.id)
			result.Object = result.Display
		}
	}
	return result
}

type vmFrame struct {
	method    *compiledMethod
	ip        int
	self      runtimeValueInternal
	scopes    []map[string]runtimeValueInternal
	stackBase int
}

func (f *vmFrame) lookup(name string) (runtimeValueInternal, bool) {
	for i := len(f.scopes) - 1; i >= 0; i-- {
		if value, found := f.scopes[i][name]; found {
			return value, true
		}
	}
	return voidValue(), false
}

func (f *vmFrame) store(name string, value runtimeValueInternal) bool {
	for i := len(f.scopes) - 1; i >= 0; i-- {
		if _, found := f.scopes[i][name]; found {
			f.scopes[i][name] = value
			return true
		}
	}
	return false
}

func (f *vmFrame) mergedLocals() map[string]RuntimeValue {
	result := map[string]RuntimeValue{}
	for _, scope := range f.scopes {
		for name, value := range scope {
			result[name] = value.public()
		}
	}
	return result
}

type VM struct {
	exec              *executable
	policy            VMPolicy
	stack             []runtimeValueInternal
	frames            []*vmFrame
	output            []string
	trace             []VMState
	traceDropped      int
	traceBytes        int
	traceSizes        []int
	diagnostics       []Diagnostic
	executed          int
	halted            bool
	paused            bool
	nextObjectID      int
	objectCount       int
	objects           map[int]*runtimeObject
	outputBytes       int
	skipBreakpoint    bool
	lastInstruction   byteInstruction
	lastMethod        string
	lastNote          string
	input             []string
	inputPos          int
	configuredInput   []string
	inputBytes        int
	interactiveInput  bool
	waitingInput      *VMInputRequest
	nextInputSequence int
	inputEvents       []vmInputEvent
	liveInputStart    int
	stopReason        DebugStopReason
	breakpointHits    map[string]int
	breakpointStates  map[string]BreakpointState
	compiledBreaks    map[string]compiledBreakpoint
	activeBreakpoints []Breakpoint
	watches           []WatchExpression
	watchCache        map[string]WatchResult
	watchCacheStep    int
	watchCacheDepth   int
	debugLog          []DebugLogEntry
	debugLogBytes     int
	debugLogOrder     int
	reverseHook       func(*VM, VMState)
	instructionRecord bool
}

func (vm *VM) ensurePolicy() VMPolicy {
	policy, err := NormalizeVMPolicy(vm.policy)
	if err != nil {
		policy = DefaultVMPolicy()
	}
	vm.policy = policy
	return policy
}

func newVM(exec *executable) *VM {
	return newVMWithPolicy(exec, DefaultVMPolicy())
}

func newVMWithPolicy(exec *executable, policy VMPolicy) *VM {
	policy, err := NormalizeVMPolicy(policy)
	if err != nil {
		policy = DefaultVMPolicy()
	}
	vm := &VM{
		exec: exec, policy: policy, nextObjectID: 1,
		breakpointHits: map[string]int{}, breakpointStates: map[string]BreakpointState{},
		compiledBreaks: map[string]compiledBreakpoint{}, watchCache: map[string]WatchResult{},
		objects: map[int]*runtimeObject{},
	}
	if exec == nil || exec.Entry == nil {
		vm.halted = true
		return vm
	}
	if len(exec.MethodOrder) > 0 && !exec.verified {
		diagnostics := verifyExecutable(exec)
		if hasErrors(diagnostics) {
			vm.halted = true
			vm.diagnostics = append(vm.diagnostics, diagnostics...)
			return vm
		}
		exec.verified = true
	}
	vm.frames = append(vm.frames, &vmFrame{
		method: exec.Entry, self: voidValue(), scopes: []map[string]runtimeValueInternal{{}},
	})
	vm.record("START", exec.Entry.Span, "VM ready")
	return vm
}

func (vm *VM) currentFrame() *vmFrame {
	if len(vm.frames) == 0 {
		return nil
	}
	return vm.frames[len(vm.frames)-1]
}

func (vm *VM) Step() {
	vm.ensurePolicy()
	if vm.waitingInput != nil {
		vm.stopReason = StopReasonWaitingInput
		return
	}
	vm.paused = false
	vm.skipBreakpoint = false
	vm.stopReason = ""
	vm.executeOne()
	vm.finishStopReason(StopReasonInstruction)
}

func (vm *VM) Run(maxSteps int, breakpoints map[Breakpoint]bool) {
	vm.RunContext(context.Background(), maxSteps, breakpoints)
}

func (vm *VM) RunContext(ctx context.Context, maxSteps int, breakpoints map[Breakpoint]bool) {
	vm.runUntilContext(ctx, maxSteps, breakpoints, nil, "")
}

func (vm *VM) runUntilContext(ctx context.Context, maxSteps int, breakpoints map[Breakpoint]bool, stop func(DebugLocation) bool, sourceReason DebugStopReason) {
	vm.ensurePolicy()
	vm.paused = false
	vm.stopReason = ""
	if len(breakpoints) > vm.policy.MaxBreakpoints {
		vm.fail("COL6055", "VM breakpoint limit exceeded", "Use fewer breakpoints or increase the bounded breakpoint policy.", vm.nextSpan())
		return
	}
	if maxSteps <= 0 {
		maxSteps = vm.policy.MaxInstructionsPerRun
	}
	if maxSteps > vm.policy.MaxInstructionsPerRun {
		maxSteps = vm.policy.MaxInstructionsPerRun
	}
	start := vm.executed
	for !vm.halted && vm.waitingInput == nil && vm.executed-start < maxSteps {
		select {
		case <-ctx.Done():
			vm.fail("COL6056", "VM execution cancelled", "Reset the session before starting another run.", vm.nextSpan())
			vm.stopReason = StopReasonCancelled
			return
		default:
		}
		frame := vm.currentFrame()
		if frame == nil || frame.ip < 0 || frame.ip >= len(frame.method.Code) {
			vm.fail("COL6001", "instruction pointer escaped the current method", "This indicates invalid generated bytecode.", emptySpan())
			break
		}
		next := frame.method.Code[frame.ip]
		line := next.Span.StartLine
		if !vm.skipBreakpoint && vm.handleBreakpoints(next, breakpoints) {
			vm.paused = true
			vm.skipBreakpoint = true
			vm.stopReason = StopReasonBreakpoint
			vm.record("BREAKPOINT", next.Span, fmt.Sprintf("paused before line %d", line))
			return
		}
		if vm.executed > start && stop != nil {
			if current := vm.currentDebugLocation(); current != nil && stop(*current) {
				vm.paused = true
				vm.stopReason = sourceReason
				vm.record("SOURCE_STEP", current.Span, "paused at "+string(sourceReason))
				return
			}
		}
		vm.skipBreakpoint = false
		vm.executeOne()
	}
	if !vm.halted && vm.waitingInput == nil && vm.executed-start >= maxSteps {
		vm.paused = true
		vm.stopReason = StopReasonInstructionLimit
		vm.record("PAUSE", vm.nextSpan(), fmt.Sprintf("paused after %d instructions", maxSteps))
	}
	vm.finishStopReason("")
}

func (vm *VM) finishStopReason(fallback DebugStopReason) {
	if vm.stopReason != "" {
		return
	}
	switch {
	case vm.waitingInput != nil:
		vm.stopReason = StopReasonWaitingInput
	case vm.halted && len(vm.diagnostics) > 0:
		vm.stopReason = StopReasonRuntimeError
	case vm.halted:
		vm.stopReason = StopReasonHalt
	default:
		vm.stopReason = fallback
	}
}

func (vm *VM) currentDebugLocation() *DebugLocation {
	frame := vm.currentFrame()
	if frame == nil || frame.ip < 0 || frame.ip >= len(frame.method.Code) {
		return nil
	}
	instruction := frame.method.Code[frame.ip]
	span := instruction.Span
	synthetic := span.SourceID == "" || span.StartLine <= 0 || (frame.method.Synthetic && sameDebugSpan(span, frame.method.Span))
	return &DebugLocation{
		SourceID: span.SourceID, Span: span, Method: frame.method.DisplayName,
		Instruction: frame.ip, FrameDepth: len(vm.frames) - 1, Synthetic: synthetic,
	}
}

func sameDebugSpan(left, right Span) bool {
	return left.SourceID == right.SourceID && left.StartOffset == right.StartOffset && left.EndOffset == right.EndOffset &&
		left.StartLine == right.StartLine && left.StartColumn == right.StartColumn && left.EndLine == right.EndLine && left.EndColumn == right.EndColumn
}
