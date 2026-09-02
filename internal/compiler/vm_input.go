// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"errors"
	"strings"
)

var (
	ErrInputNotWaiting = errors.New("VM is not waiting for input")
	ErrInputSequence   = errors.New("input request is stale")
	ErrInputLimit      = errors.New("VM input limit exceeded")
)

type vmInputEvent struct {
	sequence int
	builtin  string
	line     string
	span     Span
	position int
	live     bool
}

func splitProgramInput(text string) []string {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	if normalized == "" {
		return nil
	}
	lines := strings.Split(normalized, "\n")
	if strings.HasSuffix(normalized, "\n") {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func (vm *VM) SetInput(text string) {
	vm.ensurePolicy()
	if len(text) > vm.policy.MaxInputBytes {
		vm.fail("COL6053", "VM program input byte limit exceeded", "Use a smaller input or increase the bounded input policy.", vm.nextSpan())
		return
	}
	lines := splitProgramInput(text)
	if len(lines) > vm.policy.MaxInputLines {
		vm.fail("COL6054", "VM program input line limit exceeded", "Use fewer input lines or increase the bounded input policy.", vm.nextSpan())
		return
	}
	vm.input = lines
	vm.configuredInput = append([]string(nil), lines...)
	vm.inputBytes = inputLinesBytes(lines)
	vm.liveInputStart = len(lines)
	if vm.waitingInput != nil && vm.inputPos < len(lines) {
		vm.waitingInput = nil
	}
}

func (vm *VM) readLine() string {
	line, _ := vm.takeLine("pre-supplied")
	return line
}

func (vm *VM) takeLine(builtin string) (string, bool) {
	if vm.inputPos >= len(vm.input) {
		return "", false
	}
	position := vm.inputPos
	line := vm.input[position]
	vm.inputPos++
	vm.inputEvents = append(vm.inputEvents, vmInputEvent{
		sequence: len(vm.inputEvents) + 1, builtin: builtin, line: line,
		span: vm.lastInstruction.Span, position: position, live: position >= vm.liveInputStart,
	})
	return line, true
}

func (vm *VM) waitForInput(builtin string, span Span) {
	if vm.waitingInput != nil {
		return
	}
	vm.nextInputSequence++
	method := vm.lastMethod
	if frame := vm.currentFrame(); frame != nil {
		method = frame.method.DisplayName
	}
	vm.waitingInput = &VMInputRequest{
		Sequence: vm.nextInputSequence, Builtin: builtin, Span: span, Method: method,
		Frame: len(vm.frames) - 1, Output: append([]string(nil), vm.output...),
		InputPosition: vm.inputPos, PreSuppliedRemaining: vm.inputPos < vm.liveInputStart,
	}
	vm.paused = false
	vm.stopReason = StopReasonWaitingInput
	vm.lastNote = builtin + " is waiting for a live input line"
}

func (vm *VM) provideInputLine(line string, sequence int) error {
	vm.ensurePolicy()
	if vm.waitingInput == nil {
		return ErrInputNotWaiting
	}
	if sequence != vm.waitingInput.Sequence {
		return ErrInputSequence
	}
	if len(line) > vm.policy.MaxStringBytes || len(vm.input) >= vm.policy.MaxInputLines || len(line) > vm.policy.MaxInputBytes-vm.inputBytes {
		return ErrInputLimit
	}
	vm.input = append(vm.input, line)
	vm.inputBytes += len(line)
	vm.waitingInput = nil
	return nil
}

func (vm *VM) finishInteractiveInput() {
	vm.interactiveInput = false
	vm.waitingInput = nil
}

func (vm *VM) inputEventLog(includeContent bool) []VMInputEvent {
	result := make([]VMInputEvent, 0, len(vm.inputEvents))
	for _, event := range vm.inputEvents {
		line := ""
		if includeContent {
			line = event.line
		}
		result = append(result, VMInputEvent{
			Sequence: event.sequence, Builtin: event.builtin, Line: line, Span: event.span,
			InputPosition: event.position, Live: event.live,
		})
	}
	return result
}

func inputLinesBytes(lines []string) int {
	total := 0
	for _, line := range lines {
		total += len(line)
	}
	return total
}
