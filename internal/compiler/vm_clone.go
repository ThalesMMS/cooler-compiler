// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// cloneMutableVM copies every mutable VM-owned value while sharing the
// verified executable and parsed debugger expressions, which are immutable.
func cloneMutableVM(source *VM) *VM {
	if source == nil {
		return nil
	}
	clone := *source
	clone.reverseHook = nil

	objectMap := map[*runtimeObject]*runtimeObject{}
	queue := []*runtimeObject{}
	appendValueObject := func(value runtimeValueInternal) {
		if value.kind == valueObject && value.o != nil {
			queue = append(queue, value.o)
		}
	}
	for _, object := range source.objects {
		if object != nil {
			queue = append(queue, object)
		}
	}
	for _, value := range source.stack {
		appendValueObject(value)
	}
	for _, frame := range source.frames {
		if frame == nil {
			continue
		}
		appendValueObject(frame.self)
		for _, scope := range frame.scopes {
			for _, value := range scope {
				appendValueObject(value)
			}
		}
	}
	for len(queue) > 0 {
		object := queue[0]
		queue = queue[1:]
		if object == nil || objectMap[object] != nil {
			continue
		}
		objectMap[object] = &runtimeObject{id: object.id, class: object.class, allocationSpan: object.allocationSpan, fields: map[string]runtimeValueInternal{}}
		for _, value := range object.fields {
			appendValueObject(value)
		}
	}
	cloneValue := func(value runtimeValueInternal) runtimeValueInternal {
		result := value
		if value.kind == valueObject && value.o != nil {
			result.o = objectMap[value.o]
		}
		return result
	}
	for original, copied := range objectMap {
		for name, value := range original.fields {
			copied.fields[name] = cloneValue(value)
		}
	}
	clone.objects = make(map[int]*runtimeObject, len(source.objects))
	for id, object := range source.objects {
		clone.objects[id] = objectMap[object]
	}
	clone.stack = make([]runtimeValueInternal, len(source.stack))
	for index, value := range source.stack {
		clone.stack[index] = cloneValue(value)
	}
	clone.frames = make([]*vmFrame, len(source.frames))
	for index, frame := range source.frames {
		if frame == nil {
			continue
		}
		copied := *frame
		copied.self = cloneValue(frame.self)
		copied.scopes = make([]map[string]runtimeValueInternal, len(frame.scopes))
		for scopeIndex, scope := range frame.scopes {
			copied.scopes[scopeIndex] = make(map[string]runtimeValueInternal, len(scope))
			for name, value := range scope {
				copied.scopes[scopeIndex][name] = cloneValue(value)
			}
		}
		clone.frames[index] = &copied
	}
	clone.output = append([]string(nil), source.output...)
	clone.trace = make([]VMState, len(source.trace))
	for index, state := range source.trace {
		clone.trace[index] = cloneVMState(state)
	}
	clone.traceSizes = append([]int(nil), source.traceSizes...)
	clone.diagnostics = append([]Diagnostic(nil), source.diagnostics...)
	clone.input = append([]string(nil), source.input...)
	clone.configuredInput = append([]string(nil), source.configuredInput...)
	clone.inputEvents = append([]vmInputEvent(nil), source.inputEvents...)
	if source.waitingInput != nil {
		waiting := *source.waitingInput
		waiting.Output = append([]string(nil), source.waitingInput.Output...)
		clone.waitingInput = &waiting
	}
	clone.breakpointHits = cloneStringIntMap(source.breakpointHits)
	clone.breakpointStates = make(map[string]BreakpointState, len(source.breakpointStates))
	for key, state := range source.breakpointStates {
		clone.breakpointStates[key] = state
	}
	clone.compiledBreaks = make(map[string]compiledBreakpoint, len(source.compiledBreaks))
	for key, compiled := range source.compiledBreaks {
		copied := compiled
		copied.logParts = append([]debugLogPart(nil), compiled.logParts...)
		copied.targets = make(map[debugInstructionKey]bool, len(compiled.targets))
		for target, enabled := range compiled.targets {
			copied.targets[target] = enabled
		}
		clone.compiledBreaks[key] = copied
	}
	clone.activeBreakpoints = append([]Breakpoint(nil), source.activeBreakpoints...)
	clone.watches = append([]WatchExpression(nil), source.watches...)
	clone.watchCache = make(map[string]WatchResult, len(source.watchCache))
	for key, result := range source.watchCache {
		copied := result
		copied.Dependencies = append([]string(nil), result.Dependencies...)
		clone.watchCache[key] = copied
	}
	clone.debugLog = append([]DebugLogEntry(nil), source.debugLog...)
	return &clone
}

func cloneStringIntMap(source map[string]int) map[string]int {
	result := make(map[string]int, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func cloneVMState(source VMState) VMState {
	result := source
	result.Stack = append([]RuntimeValue(nil), source.Stack...)
	result.Output = append([]string(nil), source.Output...)
	result.Frames = make([]RuntimeFrame, len(source.Frames))
	for index, frame := range source.Frames {
		copied := frame
		copied.Locals = make(map[string]RuntimeValue, len(frame.Locals))
		for name, value := range frame.Locals {
			copied.Locals[name] = value
		}
		result.Frames[index] = copied
	}
	return result
}

func approximateCheckpointBytes(vm *VM) int {
	if vm == nil {
		return 0
	}
	size := 512 + vm.traceBytes + vm.outputBytes + vm.inputBytes + vm.debugLogBytes
	size += len(vm.stack)*32 + len(vm.frames)*128 + len(vm.diagnostics)*192
	for _, frame := range vm.frames {
		if frame == nil {
			continue
		}
		for _, scope := range frame.scopes {
			size += 64 + len(scope)*64
			for name, value := range scope {
				size += len(name) + approximateInternalValueBytes(value)
			}
		}
	}
	for _, object := range vm.objects {
		if object == nil {
			continue
		}
		size += 96 + len(object.class)
		for name, value := range object.fields {
			size += 48 + len(name) + approximateInternalValueBytes(value)
		}
	}
	return size
}

func approximateInternalValueBytes(value runtimeValueInternal) int {
	if value.kind == valueString {
		return 32 + len(value.s)
	}
	return 32
}

func reverseStateHash(vm *VM) string {
	if vm == nil {
		return ""
	}
	type canonicalValue struct {
		Kind valueKind `json:"kind"`
		Int  int32     `json:"int,omitempty"`
		Bool bool      `json:"bool,omitempty"`
		Text string    `json:"text,omitempty"`
		Ref  int       `json:"ref,omitempty"`
	}
	value := func(source runtimeValueInternal) canonicalValue {
		result := canonicalValue{Kind: source.kind, Int: int32(source.i), Bool: source.b, Text: source.s}
		if source.o != nil {
			result.Ref = source.o.id
		}
		return result
	}
	type canonicalField struct {
		Name  string         `json:"name"`
		Value canonicalValue `json:"value"`
	}
	type canonicalObject struct {
		ID     int              `json:"id"`
		Class  string           `json:"class"`
		Fields []canonicalField `json:"fields"`
	}
	objects := make([]canonicalObject, 0, len(vm.objects))
	ids := make([]int, 0, len(vm.objects))
	for id := range vm.objects {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		object := vm.objects[id]
		if object == nil {
			continue
		}
		names := make([]string, 0, len(object.fields))
		for name := range object.fields {
			names = append(names, name)
		}
		sort.Strings(names)
		item := canonicalObject{ID: id, Class: object.class, Fields: make([]canonicalField, 0, len(names))}
		for _, name := range names {
			item.Fields = append(item.Fields, canonicalField{Name: name, Value: value(object.fields[name])})
		}
		objects = append(objects, item)
	}
	type canonicalFrame struct {
		Method string                      `json:"method"`
		IP     int                         `json:"ip"`
		Self   canonicalValue              `json:"self"`
		Scopes []map[string]canonicalValue `json:"scopes"`
		Base   int                         `json:"base"`
	}
	frames := make([]canonicalFrame, 0, len(vm.frames))
	for _, frame := range vm.frames {
		if frame == nil {
			continue
		}
		item := canonicalFrame{IP: frame.ip, Self: value(frame.self), Base: frame.stackBase, Scopes: make([]map[string]canonicalValue, len(frame.scopes))}
		if frame.method != nil {
			item.Method = frame.method.DisplayName
		}
		for index, scope := range frame.scopes {
			item.Scopes[index] = make(map[string]canonicalValue, len(scope))
			for name, local := range scope {
				item.Scopes[index][name] = value(local)
			}
		}
		frames = append(frames, item)
	}
	stack := make([]canonicalValue, len(vm.stack))
	for index, item := range vm.stack {
		stack[index] = value(item)
	}
	payload := struct {
		Executed       int               `json:"executed"`
		Halted         bool              `json:"halted"`
		Waiting        *VMInputRequest   `json:"waiting,omitempty"`
		NextID         int               `json:"nextId"`
		Objects        []canonicalObject `json:"objects"`
		Frames         []canonicalFrame  `json:"frames"`
		Stack          []canonicalValue  `json:"stack"`
		Output         []string          `json:"output"`
		Input          []string          `json:"input"`
		InputPos       int               `json:"inputPos"`
		InputEvents    []VMInputEvent    `json:"inputEvents"`
		Diagnostics    []Diagnostic      `json:"diagnostics"`
		BreakpointHits map[string]int    `json:"breakpointHits"`
		DebugLog       []DebugLogEntry   `json:"debugLog"`
		Policy         VMPolicy          `json:"policy"`
	}{
		Executed: vm.executed, Halted: vm.halted, Waiting: vm.waitingInput, NextID: vm.nextObjectID,
		Objects: objects, Frames: frames, Stack: stack, Output: vm.output, Input: vm.input, InputPos: vm.inputPos,
		InputEvents: vm.inputEventLog(true), Diagnostics: vm.diagnostics, BreakpointHits: vm.breakpointHits, DebugLog: vm.debugLog, Policy: vm.policy,
	}
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
