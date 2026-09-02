// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"fmt"
	"sort"
	"strings"
)

const (
	maxDebugLogEntries     = 256
	maxDebugLogBytes       = 64 << 10
	maxDebugLogMessage     = 1_024
	maxWatchExpressions    = 64
	maxLogpointExpressions = 32
)

type compiledBreakpoint struct {
	condition *parsedDebugExpression
	logParts  []debugLogPart
	targets   map[debugInstructionKey]bool
}

type debugLogPart struct {
	text string
	expr *parsedDebugExpression
}

func breakpointEnabled(breakpoint Breakpoint) bool {
	return breakpoint.Enabled == nil || *breakpoint.Enabled
}

func watchEnabled(watch WatchExpression) bool {
	return watch.Enabled == nil || *watch.Enabled
}

func breakpointIdentity(breakpoint Breakpoint) string {
	if breakpoint.ID != "" {
		return breakpoint.ID
	}
	return fmt.Sprintf("%s:%d:%d", breakpoint.SourceID, breakpoint.Line, breakpoint.Column)
}

func (vm *VM) prepareBreakpoints(breakpoints []Breakpoint) map[Breakpoint]bool {
	set := make(map[Breakpoint]bool, len(breakpoints))
	vm.activeBreakpoints = append(vm.activeBreakpoints[:0], breakpoints...)
	vm.compiledBreaks = map[string]compiledBreakpoint{}
	previousStates := vm.breakpointStates
	vm.breakpointStates = map[string]BreakpointState{}
	for _, breakpoint := range breakpoints {
		identity := breakpointIdentity(breakpoint)
		state := BreakpointState{Breakpoint: breakpoint, Hits: vm.breakpointHits[identity]}
		if previous, ok := previousStates[identity]; ok && sameBreakpointEvaluation(previous.Breakpoint, breakpoint) {
			state.Diagnostic = previous.Diagnostic
		}
		targets := vm.breakpointTargets(breakpoint)
		if len(targets) == 0 {
			state.Diagnostic = "no executable instruction at this source location"
			vm.breakpointStates[identity] = state
			continue
		}
		state.Verified = true
		compiled := compiledBreakpoint{targets: targets}
		if strings.TrimSpace(breakpoint.Condition) != "" {
			condition, err := parseDebugExpression(breakpoint.Condition)
			if err != nil {
				state.Diagnostic = "invalid condition: " + err.Error()
				vm.breakpointStates[identity] = state
				continue
			}
			compiled.condition = condition
		}
		if breakpoint.HitCount < 0 {
			state.Diagnostic = "hit count cannot be negative"
			vm.breakpointStates[identity] = state
			continue
		}
		if breakpoint.LogMessage != "" {
			parts, err := parseDebugLogTemplate(breakpoint.LogMessage)
			if err != nil {
				state.Diagnostic = "invalid log message: " + err.Error()
				vm.breakpointStates[identity] = state
				continue
			}
			compiled.logParts = parts
		}
		vm.compiledBreaks[identity] = compiled
		vm.breakpointStates[identity] = state
		if breakpointEnabled(breakpoint) {
			set[breakpoint] = true
		}
	}
	return set
}

func (vm *VM) breakpointTargets(breakpoint Breakpoint) map[debugInstructionKey]bool {
	result := map[debugInstructionKey]bool{}
	if vm.exec == nil || breakpoint.Line < 1 {
		return result
	}
	type candidate struct {
		key  debugInstructionKey
		span Span
	}
	candidates := []candidate{}
	for _, method := range vm.exec.MethodOrder {
		for index, instruction := range method.Code {
			span := instruction.Span
			if span.StartLine != breakpoint.Line || (breakpoint.SourceID != "" && span.SourceID != breakpoint.SourceID) {
				continue
			}
			if breakpoint.Column > 0 && (breakpoint.Column < span.StartColumn || breakpoint.Column >= span.EndColumn || span.StartLine != span.EndLine) {
				continue
			}
			candidates = append(candidates, candidate{key: debugInstructionKey{method: method.DisplayName, instruction: index}, span: span})
		}
	}
	if breakpoint.Column <= 0 {
		for _, candidate := range candidates {
			result[candidate.key] = true
		}
		return result
	}
	bestWidth := int(^uint(0) >> 1)
	bestStart := -1
	for _, candidate := range candidates {
		width := candidate.span.EndColumn - candidate.span.StartColumn
		if width < bestWidth || (width == bestWidth && candidate.span.StartColumn > bestStart) {
			bestWidth = width
			bestStart = candidate.span.StartColumn
			result = map[debugInstructionKey]bool{candidate.key: true}
		} else if width == bestWidth && candidate.span.StartColumn == bestStart {
			result[candidate.key] = true
		}
	}
	return result
}

func (vm *VM) handleBreakpoints(next byteInstruction, breakpoints map[Breakpoint]bool) bool {
	if len(breakpoints) == 0 {
		return false
	}
	matches := make([]Breakpoint, 0, 1)
	for breakpoint := range breakpoints {
		if breakpoint.Line != next.Span.StartLine {
			continue
		}
		if breakpoint.SourceID != "" && breakpoint.SourceID != next.Span.SourceID {
			continue
		}
		if breakpoint.Column > 0 && (breakpoint.Column < next.Span.StartColumn || breakpoint.Column >= next.Span.EndColumn) {
			continue
		}
		matches = append(matches, breakpoint)
	}
	sort.Slice(matches, func(i, j int) bool { return breakpointIdentity(matches[i]) < breakpointIdentity(matches[j]) })
	shouldPause := false
	for _, breakpoint := range matches {
		identity := breakpointIdentity(breakpoint)
		compiled, managed := vm.compiledBreaks[identity]
		if !managed {
			shouldPause = true
			continue
		}
		frame := vm.currentFrame()
		if frame == nil || !compiled.targets[debugInstructionKey{method: frame.method.DisplayName, instruction: frame.ip}] {
			continue
		}
		vm.breakpointHits[identity]++
		state := vm.breakpointStates[identity]
		state.Hits = vm.breakpointHits[identity]
		if compiled.condition != nil {
			value, err := evaluateDebugExpression(compiled.condition, vm.currentFrame())
			if err != nil {
				state.Diagnostic = "condition evaluation failed: " + err.Error()
				vm.breakpointStates[identity] = state
				continue
			}
			if value.kind != valueBool {
				state.Diagnostic = "condition must evaluate to Bool, received " + value.typeName()
				vm.breakpointStates[identity] = state
				continue
			}
			if !value.b {
				state.Diagnostic = ""
				vm.breakpointStates[identity] = state
				continue
			}
			state.Diagnostic = ""
		}
		vm.breakpointStates[identity] = state
		if breakpoint.HitCount > 0 && state.Hits < breakpoint.HitCount {
			continue
		}
		if breakpoint.LogMessage != "" {
			vm.appendDebugLog(renderDebugLog(compiled.logParts, vm.currentFrame()), next)
			if !breakpoint.Pause {
				continue
			}
		}
		shouldPause = true
	}
	return shouldPause
}

func sameBreakpointEvaluation(left, right Breakpoint) bool {
	return left.SourceID == right.SourceID && left.Line == right.Line && left.Column == right.Column &&
		left.Condition == right.Condition && left.LogMessage == right.LogMessage && left.HitCount == right.HitCount
}

func parseDebugLogTemplate(message string) ([]debugLogPart, error) {
	if len(message) > maxDebugLogMessage {
		return nil, fmt.Errorf("message exceeds %d bytes", maxDebugLogMessage)
	}
	parts := []debugLogPart{}
	expressions := 0
	for len(message) > 0 {
		open := strings.IndexByte(message, '{')
		if open < 0 {
			if strings.Contains(message, "}") {
				return nil, fmt.Errorf("unmatched '}'")
			}
			parts = append(parts, debugLogPart{text: message})
			break
		}
		if open > 0 {
			parts = append(parts, debugLogPart{text: message[:open]})
		}
		close := strings.IndexByte(message[open+1:], '}')
		if close < 0 {
			return nil, fmt.Errorf("unmatched '{'")
		}
		close += open + 1
		expression := message[open+1 : close]
		parsed, err := parseDebugExpression(expression)
		if err != nil {
			return nil, err
		}
		expressions++
		if expressions > maxLogpointExpressions {
			return nil, fmt.Errorf("message exceeds %d interpolations", maxLogpointExpressions)
		}
		parts = append(parts, debugLogPart{expr: parsed})
		message = message[close+1:]
	}
	return parts, nil
}

func renderDebugLog(parts []debugLogPart, frame *vmFrame) string {
	var result strings.Builder
	for _, part := range parts {
		if part.expr == nil {
			result.WriteString(part.text)
			continue
		}
		value, err := evaluateDebugExpression(part.expr, frame)
		if err != nil {
			result.WriteString("<error: ")
			result.WriteString(err.Error())
			result.WriteByte('>')
		} else {
			result.WriteString(value.public().Display)
		}
		if result.Len() >= maxDebugLogMessage {
			break
		}
	}
	message := result.String()
	if len(message) > maxDebugLogMessage {
		message = message[:maxDebugLogMessage]
	}
	return message
}

func (vm *VM) appendDebugLog(message string, instruction byteInstruction) {
	entryBytes := len(message)
	for len(vm.debugLog) > 0 && (len(vm.debugLog) >= maxDebugLogEntries || vm.debugLogBytes+entryBytes > maxDebugLogBytes) {
		vm.debugLogBytes -= len(vm.debugLog[0].Message)
		vm.debugLog = vm.debugLog[1:]
	}
	if entryBytes > maxDebugLogBytes {
		message = message[:maxDebugLogBytes]
		entryBytes = len(message)
	}
	vm.debugLogOrder++
	vm.debugLog = append(vm.debugLog, DebugLogEntry{
		Order: vm.debugLogOrder, Message: message, SourceID: instruction.Span.SourceID,
		Line: instruction.Span.StartLine, Method: vm.currentFrame().method.DisplayName, Step: vm.executed,
	})
	vm.debugLogBytes += entryBytes
}

func (vm *VM) publicBreakpointStates() []BreakpointState {
	states := make([]BreakpointState, 0, len(vm.activeBreakpoints))
	for _, breakpoint := range vm.activeBreakpoints {
		identity := breakpointIdentity(breakpoint)
		state := vm.breakpointStates[identity]
		state.Hits = vm.breakpointHits[identity]
		states = append(states, state)
	}
	return states
}

func (vm *VM) publicDebugLog() []DebugLogEntry {
	return append([]DebugLogEntry(nil), vm.debugLog...)
}
