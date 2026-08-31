// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"strconv"
	"strings"
)

func (vm *VM) executeBuiltin(method *MethodInfo, receiver runtimeValueInternal, args []runtimeValueInternal, span Span) (runtimeValueInternal, bool) {
	key := methodKey(method.Owner, method.Name)
	switch key {
	case "Object.abort":
		vm.fail("COL6040", "Object.abort was called", "Remove abort() or guard the path that reaches it.", span)
		return voidValue(), false
	case "Object.type_name":
		return stringValue(receiver.typeName()), true
	case "Object.copy":
		if receiver.kind != valueObject || receiver.o == nil {
			return receiver, true
		}
		if vm.objectCount >= maxVMObjects {
			vm.fail("COL6019", "VM object allocation limit exceeded", "Reduce object copying.", span)
			return voidValue(), false
		}
		copyObject := &runtimeObject{id: vm.nextObjectID, class: receiver.o.class, fields: map[string]runtimeValueInternal{}}
		vm.nextObjectID++
		vm.objectCount++
		for name, value := range receiver.o.fields {
			copyObject.fields[name] = value
		}
		return objectValue(copyObject), true
	case "IO.out_string":
		if len(args) != 1 || args[0].kind != valueString {
			vm.fail("COL6041", "IO.out_string expected one String", "Pass a String argument.", span)
			return voidValue(), false
		}
		vm.output = append(vm.output, args[0].s)
		return receiver, true
	case "IO.out_int":
		if len(args) != 1 || args[0].kind != valueInt {
			vm.fail("COL6042", "IO.out_int expected one Int", "Pass an Int argument.", span)
			return voidValue(), false
		}
		vm.output = append(vm.output, strconv.Itoa(args[0].i))
		return receiver, true
	case "IO.in_string":
		vm.lastNote = "in_string read a program-input line"
		return stringValue(vm.readLine()), true
	case "IO.in_int":
		line := strings.TrimSpace(vm.readLine())
		n, err := strconv.Atoi(line)
		if err != nil {
			n = 0
		}
		vm.lastNote = "in_int read a program-input line"
		return intValue(n), true
	case "String.length":
		if receiver.kind != valueString {
			vm.fail("COL6043", "String.length received "+receiver.typeName(), "Call length on a String.", span)
			return voidValue(), false
		}
		return intValue(len([]rune(receiver.s))), true
	case "String.concat":
		if receiver.kind != valueString || len(args) != 1 || args[0].kind != valueString {
			vm.fail("COL6044", "String.concat requires a String receiver and argument", "Call concat with one String.", span)
			return voidValue(), false
		}
		return stringValue(receiver.s + args[0].s), true
	case "String.substr":
		if receiver.kind != valueString || len(args) != 2 || args[0].kind != valueInt || args[1].kind != valueInt {
			vm.fail("COL6045", "String.substr requires Int start and length", "Call substr(i, l) with valid Int arguments.", span)
			return voidValue(), false
		}
		runes := []rune(receiver.s)
		start, length := args[0].i, args[1].i
		if start < 0 || length < 0 || start > len(runes) || start+length > len(runes) {
			vm.fail("COL6046", "String.substr indexes are out of range", "Keep the selected interval within the String length.", span)
			return voidValue(), false
		}
		return stringValue(string(runes[start : start+length])), true
	default:
		vm.fail("COL6047", "unknown builtin method "+key, "Implement the builtin in the educational VM.", span)
		return voidValue(), false
	}
}

func (vm *VM) fail(code, message, hint string, span Span) {
	if vm.halted && len(vm.diagnostics) > 0 {
		return
	}
	vm.diagnostics = append(vm.diagnostics, errorDiagnostic("runtime", code, message, hint, span))
	vm.halted = true
	vm.paused = false
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
	vm.trace = append(vm.trace, state)
	if len(vm.trace) > maxVMTrace {
		drop := len(vm.trace) - maxVMTrace
		vm.trace = append([]VMState(nil), vm.trace[drop:]...)
		vm.traceDropped += drop
	}
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
		result = append(result, RuntimeFrame{
			FunctionName: frame.method.Name, Owner: frame.method.Owner,
			IP: frame.ip, Self: frame.self.public(), Locals: frame.mergedLocals(), StackBase: frame.stackBase,
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
		Paused: vm.paused, Diagnostics: append([]Diagnostic(nil), vm.diagnostics...),
	}
}

func outputText(output []string) string {
	return strings.Join(output, "")
}
