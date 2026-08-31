// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import "strings"

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
	vm.input = splitProgramInput(text)
}

func (vm *VM) readLine() string {
	if vm.inputPos >= len(vm.input) {
		return ""
	}
	line := vm.input[vm.inputPos]
	vm.inputPos++
	return line
}
