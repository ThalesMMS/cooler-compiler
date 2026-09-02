// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import "strings"

func (vm *VM) executeBuiltin(method *MethodInfo, receiver runtimeValueInternal, args []runtimeValueInternal, span Span) (runtimeValueInternal, bool) {
	vm.ensurePolicy()
	key := methodKey(method.Owner, method.Name)
	switch key {
	case "Object.abort":
		vm.fail("COL6040", "Object.abort was called", "Remove abort() or guard the path that reaches it.", span)
		return voidValue(), false
	case "Object.type_name":
		name := receiver.typeName()
		if !vm.checkStringSize(name, span) {
			return voidValue(), false
		}
		return stringValue(name), true
	case "Object.copy":
		if receiver.kind != valueObject || receiver.o == nil {
			return receiver, true
		}
		if vm.objectCount >= vm.policy.MaxObjectAllocations {
			vm.fail("COL6019", "VM object allocation limit exceeded", "Reduce object copying.", span)
			return voidValue(), false
		}
		copyObject := &runtimeObject{id: vm.nextObjectID, class: receiver.o.class, fields: map[string]runtimeValueInternal{}, allocationSpan: span}
		vm.nextObjectID++
		vm.objectCount++
		if vm.objects == nil {
			vm.objects = map[int]*runtimeObject{}
		}
		vm.objects[copyObject.id] = copyObject
		for name, value := range receiver.o.fields {
			copyObject.fields[name] = value
		}
		return objectValue(copyObject), true
	case "IO.out_string":
		if len(args) != 1 || args[0].kind != valueString {
			vm.fail("COL6041", "IO.out_string expected one String", "Pass a String argument.", span)
			return voidValue(), false
		}
		if !vm.appendOutput(args[0].s, span) {
			return voidValue(), false
		}
		return receiver, true
	case "IO.out_int":
		if len(args) != 1 || args[0].kind != valueInt {
			vm.fail("COL6042", "IO.out_int expected one Int", "Pass an Int argument.", span)
			return voidValue(), false
		}
		if !vm.appendOutput(args[0].i.String(), span) {
			return voidValue(), false
		}
		return receiver, true
	case "IO.in_string":
		line, available := vm.takeLine("in_string")
		if !available && vm.interactiveInput {
			vm.waitForInput("in_string", span)
			return voidValue(), false
		}
		vm.lastNote = "in_string read a program-input line"
		if !vm.checkStringSize(line, span) {
			return voidValue(), false
		}
		return stringValue(line), true
	case "IO.in_int":
		line, available := vm.takeLine("in_int")
		if !available && vm.interactiveInput {
			vm.waitForInput("in_int", span)
			return voidValue(), false
		}
		n, ok := parseCoolIntInput(line)
		if !ok {
			n = 0
		}
		vm.lastNote = "in_int read a program-input line"
		return intValue(n), true
	case "String.length":
		if receiver.kind != valueString {
			vm.fail("COL6043", "String.length received "+receiver.typeName(), "Call length on a String.", span)
			return voidValue(), false
		}
		return intValue(coolIntFromLength(len([]rune(receiver.s)))), true
	case "String.concat":
		if receiver.kind != valueString || len(args) != 1 || args[0].kind != valueString {
			vm.fail("COL6044", "String.concat requires a String receiver and argument", "Call concat with one String.", span)
			return voidValue(), false
		}
		if len(receiver.s) > vm.policy.MaxStringBytes-len(args[0].s) {
			vm.fail("COL6051", "VM string value limit exceeded", "Reduce repeated concatenation or input-line size.", span)
			return voidValue(), false
		}
		return stringValue(receiver.s + args[0].s), true
	case "String.substr":
		if receiver.kind != valueString || len(args) != 2 || args[0].kind != valueInt || args[1].kind != valueInt {
			vm.fail("COL6045", "String.substr requires Int start and length", "Call substr(i, l) with valid Int arguments.", span)
			return voidValue(), false
		}
		runes := []rune(receiver.s)
		start, length := int(args[0].i), int(args[1].i)
		if start < 0 || length < 0 || start > len(runes) || length > len(runes)-start {
			vm.fail("COL6046", "String.substr indexes are out of range", "Keep the selected interval within the String length.", span)
			return voidValue(), false
		}
		return stringValue(string(runes[start : start+length])), true
	default:
		vm.fail("COL6047", "unknown builtin method "+key, "Implement the builtin in the educational VM.", span)
		return voidValue(), false
	}
}

func (vm *VM) checkStringSize(value string, span Span) bool {
	if len(value) > vm.policy.MaxStringBytes {
		vm.fail("COL6051", "VM string value limit exceeded", "Reduce repeated concatenation or input-line size.", span)
		return false
	}
	return true
}

func (vm *VM) appendOutput(value string, span Span) bool {
	if len(value) > vm.policy.MaxOutputBytes-vm.outputBytes {
		vm.fail("COL6052", "VM program output limit exceeded", "Reduce output or increase the bounded output policy.", span)
		return false
	}
	vm.output = append(vm.output, value)
	vm.outputBytes += len(value)
	return true
}

func (instruction byteInstruction) operandSpan() Span {
	if instruction.OperandSpan.StartLine != 0 {
		return instruction.OperandSpan
	}
	return instruction.Span
}

func (vm *VM) fail(code, message, hint string, span Span) {
	if vm.halted && len(vm.diagnostics) > 0 {
		return
	}
	vm.diagnostics = append(vm.diagnostics, errorDiagnostic("runtime", code, message, hint, span))
	vm.halted = true
	vm.paused = false
	vm.waitingInput = nil
	vm.stopReason = StopReasonRuntimeError
	vm.lastNote = message
}

func (vm *VM) abortInternal(message string) {
	if vm.halted && len(vm.diagnostics) > 0 {
		return
	}
	vm.diagnostics = append(vm.diagnostics, errorDiagnostic(
		"runtime", "COL6099", "unexpected VM host failure: "+message,
		"Reset the session before attempting another execution.", vm.nextSpan(),
	))
	vm.halted = true
	vm.paused = false
	vm.waitingInput = nil
	vm.stopReason = StopReasonRuntimeError
	vm.lastNote = message
}

func (vm *VM) nextSpan() Span {
	frame := vm.currentFrame()
	if frame == nil || frame.ip < 0 || frame.ip >= len(frame.method.Code) {
		return emptySpan()
	}
	return frame.method.Code[frame.ip].Span
}

func (vm *VM) record(op string, span Span, note string) {
	vm.ensurePolicy()
	state := VMState{
		Step: vm.executed, IP: -1, Op: op, Note: note, Span: span,
		Stack: vm.publicStack(), Frames: vm.publicFrames(), Output: append([]string(nil), vm.output...),
	}
	if frame := vm.currentFrame(); frame != nil {
		state.IP = frame.ip
		state.Method = frame.method.DisplayName
	} else {
		state.Method = vm.lastMethod
	}
	size := approximateVMStateBytes(state)
	vm.trace = append(vm.trace, state)
	vm.traceSizes = append(vm.traceSizes, size)
	vm.traceBytes = addApproximateBytes(vm.traceBytes, size)
	for len(vm.trace) > 0 && (len(vm.trace) > vm.policy.MaxTraceEntries || vm.traceBytes > vm.policy.MaxTraceBytes) {
		vm.traceBytes -= vm.traceSizes[0]
		vm.trace[0] = VMState{}
		vm.traceSizes[0] = 0
		vm.trace = vm.trace[1:]
		vm.traceSizes = vm.traceSizes[1:]
		vm.traceDropped++
	}
	if len(vm.trace) == 0 {
		vm.trace = nil
		vm.traceSizes = nil
		vm.traceBytes = 0
	}
	if vm.reverseHook != nil {
		vm.reverseHook(vm, cloneVMState(state))
	}
}

func approximateVMStateBytes(state VMState) int {
	size := addApproximateBytes(128, len(state.Method), len(state.Op), len(state.Note))
	for _, value := range state.Stack {
		size = addApproximateBytes(size, approximateRuntimeValueBytes(value))
	}
	for _, frame := range state.Frames {
		size = addApproximateBytes(size, 96, len(frame.FunctionName), len(frame.Owner), approximateRuntimeValueBytes(frame.Self))
		for name, value := range frame.Locals {
			size = addApproximateBytes(size, len(name), approximateRuntimeValueBytes(value))
		}
	}
	for _, output := range state.Output {
		size = addApproximateBytes(size, 16, len(output))
	}
	return size
}

func approximateRuntimeValueBytes(value RuntimeValue) int {
	return addApproximateBytes(32, len(value.Type), len(value.Display), len(value.Object))
}

func addApproximateBytes(total int, amounts ...int) int {
	maximum := int(^uint(0) >> 1)
	for _, amount := range amounts {
		if amount < 0 || total > maximum-amount {
			return maximum
		}
		total += amount
	}
	return total
}

func (vm *VM) publicStack() []RuntimeValue {
	result := make([]RuntimeValue, 0, len(vm.stack))
	for _, value := range vm.stack {
		result = append(result, value.public())
	}
	return result
}

func (vm *VM) publicFrames() []RuntimeFrame {
	result := make([]RuntimeFrame, 0, len(vm.frames))
	for _, frame := range vm.frames {
		span := frame.method.Span
		if frame.ip >= 0 && frame.ip < len(frame.method.Code) {
			span = frame.method.Code[frame.ip].Span
		}
		result = append(result, RuntimeFrame{
			FunctionName: frame.method.Name, Owner: frame.method.Owner,
			IP: frame.ip, Self: frame.self.public(), Locals: frame.mergedLocals(), StackBase: frame.stackBase, Span: span,
		})
	}
	return result
}

func (vm *VM) result(traceFrom int) VMResult {
	trace := vm.trace
	offset := vm.traceDropped
	absoluteEnd := vm.traceDropped + len(vm.trace)
	if traceFrom > vm.traceDropped && traceFrom < absoluteEnd {
		index := traceFrom - vm.traceDropped
		trace = vm.trace[index:]
		offset = traceFrom
	} else if traceFrom >= absoluteEnd {
		trace = nil
		offset = absoluteEnd
	}
	return VMResult{
		Trace: append([]VMState(nil), trace...), ExecutedSteps: vm.executed,
		TraceOffset: offset, Output: append([]string(nil), vm.output...),
		Stack: vm.publicStack(), Frames: vm.publicFrames(), Halted: vm.halted,
		Paused: vm.paused, WaitingInput: cloneInputRequest(vm.waitingInput), InputEvents: len(vm.inputEvents),
		Diagnostics: append([]Diagnostic(nil), vm.diagnostics...),
	}
}

func cloneInputRequest(request *VMInputRequest) *VMInputRequest {
	if request == nil {
		return nil
	}
	result := *request
	result.Output = append([]string(nil), request.Output...)
	return &result
}

func outputText(output []string) string {
	return strings.Join(output, "")
}
