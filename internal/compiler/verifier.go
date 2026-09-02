// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
)

const verifierUnknownType = "<verifier-unknown>"

type opcodeVerifierSemantics struct {
	Stack       string
	ControlFlow string
	Scope       string
}

// verifierOpcodeSemantics is the executable documentation for every bytecode
// operation. Tests require this table and the verifier switch to stay complete.
var verifierOpcodeSemantics = map[opcode]opcodeVerifierSemantics{
	opPushInt:       {Stack: "[] -> [Int]", ControlFlow: "next"},
	opPushString:    {Stack: "[] -> [String]", ControlFlow: "next"},
	opPushBool:      {Stack: "[] -> [Bool]", ControlFlow: "next"},
	opPushVoid:      {Stack: "[] -> [void]", ControlFlow: "next"},
	opLoadSelf:      {Stack: "[] -> [SELF_TYPE]", ControlFlow: "next"},
	opLoad:          {Stack: "[] -> [declared]", ControlFlow: "next", Scope: "name must resolve"},
	opStore:         {Stack: "[value] -> []", ControlFlow: "next", Scope: "name must resolve and accept value"},
	opDup:           {Stack: "[value] -> [value,value]", ControlFlow: "next"},
	opPop:           {Stack: "[value] -> []", ControlFlow: "next"},
	opEnterScope:    {Stack: "unchanged", ControlFlow: "next", Scope: "push lexical scope"},
	opExitScopeKeep: {Stack: "[value] -> [value]", ControlFlow: "next", Scope: "pop non-root scope"},
	opBind:          {Stack: "[value] -> []", ControlFlow: "next", Scope: "bind declared local"},
	opAdd:           {Stack: "[Int,Int] -> [Int]", ControlFlow: "next"},
	opSub:           {Stack: "[Int,Int] -> [Int]", ControlFlow: "next"},
	opMul:           {Stack: "[Int,Int] -> [Int]", ControlFlow: "next"},
	opDiv:           {Stack: "[Int,Int] -> [Int]", ControlFlow: "next"},
	opNeg:           {Stack: "[Int] -> [Int]", ControlFlow: "next"},
	opNot:           {Stack: "[Bool] -> [Bool]", ControlFlow: "next"},
	opLess:          {Stack: "[Int,Int] -> [Bool]", ControlFlow: "next"},
	opLessEqual:     {Stack: "[Int,Int] -> [Bool]", ControlFlow: "next"},
	opEqual:         {Stack: "[value,value] -> [Bool]", ControlFlow: "next"},
	opIsVoid:        {Stack: "[value] -> [Bool]", ControlFlow: "next"},
	opJump:          {Stack: "unchanged", ControlFlow: "target"},
	opJumpIfFalse:   {Stack: "[Bool] -> []", ControlFlow: "target or next"},
	opNew:           {Stack: "[] -> [allocated]", ControlFlow: "next/call initializer"},
	opCall:          {Stack: "[args...,receiver] -> [result]", ControlFlow: "next/call target"},
	opReturn:        {Stack: "[result] -> caller", ControlFlow: "terminal", Scope: "root scope only"},
	opCaseSelect:    {Stack: "[subject] -> []", ControlFlow: "one branch target", Scope: "push selected branch scope"},
	opHalt:          {Stack: "[result] -> [result]", ControlFlow: "terminal", Scope: "entry root scope only"},
}

type verifierState struct {
	stack  []string
	scopes []map[string]string
}

type bytecodeVerifier struct {
	exec        *executable
	diagnostics []Diagnostic
	reported    map[string]bool
}

// verifyExecutable validates the complete backend product without executing an
// instruction. Diagnostics in the COL51xx family describe compiler defects,
// never errors attributable to a COOL program.
func verifyExecutable(exec *executable) []Diagnostic {
	v := &bytecodeVerifier{exec: exec, reported: map[string]bool{}}
	if exec == nil {
		v.fail("COL5111", nil, -1, "executable", "backend produced a nil executable", emptySpan())
		return v.diagnostics
	}
	v.verifyStructure()
	for _, method := range exec.MethodOrder {
		if method != nil {
			v.verifyMethod(method)
		}
	}
	v.verifyMetadata()
	return v.diagnostics
}

func (v *bytecodeVerifier) verifyStructure() {
	if v.exec.Table == nil {
		v.fail("COL5111", nil, -1, "class table", "executable has no analyzed class table", emptySpan())
	}
	if v.exec.Methods == nil {
		v.fail("COL5111", nil, -1, "method table", "executable has no method table", emptySpan())
	}
	seen := map[string]*compiledMethod{}
	for order, method := range v.exec.MethodOrder {
		if method == nil {
			v.fail("COL5111", nil, order, "method order", "method order contains a nil entry", emptySpan())
			continue
		}
		key := methodKey(method.Owner, method.Name)
		if method.Owner == "" || method.Name == "" || method.DisplayName == "" {
			v.fail("COL5111", method, -1, "method identity", "owner, name, and display name must be non-empty", method.Span)
		}
		if method.DisplayName != key {
			v.fail("COL5111", method, -1, "method identity", fmt.Sprintf("display name %s does not match key %s", method.DisplayName, key), method.Span)
		}
		if method.Owner != "$program" && (v.exec.Table == nil || !v.exec.Table.Has(method.Owner)) {
			v.fail("COL5111", method, -1, "method owner", "unknown owner "+method.Owner, method.Span)
		}
		if previous := seen[key]; previous != nil {
			v.fail("COL5111", method, -1, "duplicate method key", fmt.Sprintf("%s appears more than once in method order", key), method.Span)
		} else {
			seen[key] = method
		}
		if mapped := v.exec.Methods[key]; mapped != method {
			v.fail("COL5111", method, -1, "method table", fmt.Sprintf("method table entry %s does not reference its ordered method", key), method.Span)
		}
		if len(method.Formals) != len(method.FormalTypes) {
			v.fail("COL5108", method, -1, "formal metadata", fmt.Sprintf("%d formal names but %d formal types", len(method.Formals), len(method.FormalTypes)), method.Span)
		}
		formalNames := map[string]bool{}
		for index, name := range method.Formals {
			if name == "" || formalNames[name] {
				v.fail("COL5108", method, -1, "formal metadata", fmt.Sprintf("formal %d has an empty or duplicate name", index), method.Span)
			}
			formalNames[name] = true
			if index < len(method.FormalTypes) && !v.validType(method.FormalTypes[index], method.Owner, false) {
				v.fail("COL5108", method, -1, "formal metadata", fmt.Sprintf("formal %s references invalid type %s", name, method.FormalTypes[index]), method.Span)
			}
		}
		if !v.validType(method.ReturnType, method.Owner, true) {
			v.fail("COL5108", method, -1, "return metadata", "invalid return type "+method.ReturnType, method.Span)
		}
		if !method.Synthetic && v.exec.Table != nil {
			class := v.exec.Table.Classes[method.Owner]
			var info *MethodInfo
			if class != nil {
				info = class.Methods[method.Name]
			}
			if info == nil || info.Owner != method.Owner || info.Name != method.Name ||
				!slices.Equal(info.FormalNames, method.Formals) ||
				!slices.Equal(info.FormalTypes, method.FormalTypes) ||
				info.ReturnType != method.ReturnType || info.Span != method.Span {
				v.fail("COL5112", method, -1, "method metadata", "compiled signature or span differs from the analyzed method table", method.Span)
			}
		}
		if len(method.Code) == 0 {
			v.fail("COL5109", method, -1, "terminal contract", "method has no instructions", method.Span)
		}
		if !validVerifierSpan(method.Span) {
			v.fail("COL5113", method, -1, "method span", "method span is malformed", method.Span)
		}
	}
	for key, method := range v.exec.Methods {
		if method == nil || seen[key] != method {
			span := emptySpan()
			if method != nil {
				span = method.Span
			}
			v.fail("COL5111", method, -1, "method table", fmt.Sprintf("unordered or mismatched method entry %s", key), span)
		}
	}
	v.verifyRuntimeContracts()
}

func (v *bytecodeVerifier) verifyRuntimeContracts() {
	entry := v.exec.Entry
	if entry == nil {
		v.fail("COL5114", nil, -1, "entry contract", "executable has no entry method", emptySpan())
	} else {
		if entry.Owner != "$program" || entry.Name != "$entry" || len(entry.Formals) != 0 || v.exec.Methods[methodKey(entry.Owner, entry.Name)] != entry {
			v.fail("COL5114", entry, -1, "entry contract", "entry must be the registered parameterless $program.$entry method", entry.Span)
		}
		if len(entry.Code) == 0 || entry.Code[len(entry.Code)-1].Op != opHalt {
			v.fail("COL5114", entry, len(entry.Code)-1, "entry contract", "entry method must end in HALT", entry.Span)
		}
	}
	if v.exec.Table == nil {
		return
	}
	for _, className := range v.exec.Table.Order {
		runtimeClass := v.exec.Classes[className]
		class := v.exec.Table.Classes[className]
		if runtimeClass == nil {
			v.fail("COL5114", nil, -1, "runtime class", "missing runtime class "+className, emptySpan())
		} else if class == nil || runtimeClass.Name != className || runtimeClass.Parent != class.Parent {
			v.fail("COL5114", nil, -1, "runtime class", "runtime class identity or parent differs for "+className, emptySpan())
		}
		initializer := v.exec.Methods[methodKey(className, "$init")]
		if initializer == nil {
			v.fail("COL5114", nil, -1, "initializer contract", "missing initializer "+className+".$init", emptySpan())
		} else if len(initializer.Formals) != 0 || initializer.ReturnType != typeSelf || len(initializer.Code) == 0 || initializer.Code[len(initializer.Code)-1].Op != opReturn {
			v.fail("COL5114", initializer, len(initializer.Code)-1, "initializer contract", "initializer must be parameterless, return SELF_TYPE, and end in RETURN", initializer.Span)
		}
		if class == nil {
			continue
		}
		for _, methodName := range class.MethodOrder {
			info := class.Methods[methodName]
			if info != nil && !info.Builtin && v.exec.Methods[methodKey(info.Owner, info.Name)] == nil {
				v.fail("COL5114", nil, -1, "compiled method contract", "missing compiled body "+methodKey(info.Owner, info.Name), info.Span)
			}
		}
	}
	for className := range v.exec.Classes {
		if !v.exec.Table.Has(className) {
			v.fail("COL5114", nil, -1, "runtime class", "runtime class has no class-table entry: "+className, emptySpan())
		}
	}
}

func (v *bytecodeVerifier) verifyMethod(method *compiledMethod) {
	if len(method.Code) == 0 {
		return
	}
	root := map[string]string{}
	for index, name := range method.Formals {
		valueType := verifierUnknownType
		if index < len(method.FormalTypes) {
			valueType = v.resolveSelf(method.FormalTypes[index], method.Owner)
		}
		root[name] = valueType
	}
	states := map[int]verifierState{0: {scopes: []map[string]string{root}}}
	queue := []int{0}
	queued := map[int]bool{0: true}
	for len(queue) > 0 {
		index := queue[0]
		queue = queue[1:]
		queued[index] = false
		state := cloneVerifierState(states[index])
		successors := v.applyInstruction(method, index, state)
		for _, successor := range successors {
			if successor.index < 0 || successor.index >= len(method.Code) {
				v.fail("COL5102", method, index, "control flow", fmt.Sprintf("successor %d is outside the method", successor.index), method.Code[index].Span)
				continue
			}
			existing, found := states[successor.index]
			if !found {
				states[successor.index] = cloneVerifierState(successor.state)
				if !queued[successor.index] {
					queue = append(queue, successor.index)
					queued[successor.index] = true
				}
				continue
			}
			merged, changed, ok := v.mergeStates(method, successor.index, existing, successor.state)
			if ok && changed {
				states[successor.index] = merged
				if !queued[successor.index] {
					queue = append(queue, successor.index)
					queued[successor.index] = true
				}
			}
		}
	}
	for index, instruction := range method.Code {
		if _, reachable := states[index]; !reachable {
			v.fail("COL5110", method, index, "reachability", "instruction is unreachable", instruction.Span)
		}
	}
}

type verifierSuccessor struct {
	index int
	state verifierState
}

func (v *bytecodeVerifier) applyInstruction(method *compiledMethod, index int, state verifierState) []verifierSuccessor {
	instruction := method.Code[index]
	if !validVerifierSpan(instruction.Span) {
		v.fail("COL5113", method, index, "instruction span", "instruction span is malformed", instruction.Span)
	}
	next := func(current verifierState) []verifierSuccessor {
		if index+1 >= len(method.Code) {
			v.fail("COL5109", method, index, "terminal contract", "normal control flow falls out of the method", instruction.Span)
			return nil
		}
		return []verifierSuccessor{{index: index + 1, state: current}}
	}
	pop := func(count int, invariant string) ([]string, bool) {
		if len(state.stack) < count {
			v.fail("COL5103", method, index, invariant, fmt.Sprintf("needs %d stack value(s), has %d", count, len(state.stack)), instruction.Span)
			return nil, false
		}
		start := len(state.stack) - count
		values := append([]string(nil), state.stack[start:]...)
		state.stack = state.stack[:start]
		return values, true
	}
	push := func(valueType string) { state.stack = append(state.stack, valueType) }
	require := func(actual, expected, invariant string) {
		if actual != verifierUnknownType && !v.categoryConforms(actual, expected, method.Owner) {
			v.fail("COL5105", method, index, invariant, fmt.Sprintf("expected %s but found %s", expected, actual), instruction.Span)
		}
	}

	switch instruction.Op {
	case opPushInt:
		push(typeInt)
	case opPushString:
		push(typeString)
	case opPushBool:
		push(typeBool)
	case opPushVoid:
		push("<void>")
	case opLoadSelf:
		push(v.resolveSelf(method.Owner, method.Owner))
	case opLoad:
		valueType, found := v.lookupStorage(method, state, instruction.Text)
		if !found {
			v.fail("COL5107", method, index, "load storage", "unknown storage "+instruction.Text, instruction.Span)
			valueType = verifierUnknownType
		}
		push(valueType)
	case opStore:
		values, ok := pop(1, "store stack effect")
		if !ok {
			return nil
		}
		targetType, found := v.lookupStorage(method, state, instruction.Text)
		if !found {
			v.fail("COL5107", method, index, "store storage", "unknown assignment target "+instruction.Text, instruction.Span)
		} else if !v.categoryConforms(values[0], targetType, method.Owner) {
			v.fail("COL5107", method, index, "store category", fmt.Sprintf("cannot store %s in %s", values[0], targetType), instruction.Span)
		}
	case opDup:
		values, ok := pop(1, "dup stack effect")
		if !ok {
			return nil
		}
		push(values[0])
		push(values[0])
	case opPop:
		if _, ok := pop(1, "pop stack effect"); !ok {
			return nil
		}
	case opEnterScope:
		state.scopes = append(state.scopes, map[string]string{})
	case opExitScopeKeep:
		values, ok := pop(1, "exit scope stack effect")
		if !ok {
			return nil
		}
		if len(state.scopes) <= 1 {
			v.fail("COL5106", method, index, "scope balance", "cannot exit the root scope", instruction.Span)
			return nil
		}
		state.scopes = state.scopes[:len(state.scopes)-1]
		push(values[0])
	case opBind:
		values, ok := pop(1, "bind stack effect")
		if !ok {
			return nil
		}
		if len(state.scopes) == 0 || instruction.Text == "" {
			v.fail("COL5107", method, index, "bind storage", "binding requires a name and active scope", instruction.Span)
			return nil
		}
		declared := instruction.ValueType
		if declared == "" {
			declared = values[0]
		} else if !v.validType(declared, method.Owner, true) {
			v.fail("COL5107", method, index, "bind metadata", "invalid binding type "+declared, instruction.Span)
			declared = verifierUnknownType
		} else if !v.categoryConforms(values[0], declared, method.Owner) {
			v.fail("COL5107", method, index, "bind category", fmt.Sprintf("cannot bind %s as %s", values[0], declared), instruction.Span)
		}
		state.scopes[len(state.scopes)-1][instruction.Text] = v.resolveSelf(declared, method.Owner)
	case opAdd, opSub, opMul, opDiv, opLess, opLessEqual:
		values, ok := pop(2, "integer operator stack effect")
		if !ok {
			return nil
		}
		require(values[0], typeInt, "left integer operand")
		require(values[1], typeInt, "right integer operand")
		if instruction.Op == opLess || instruction.Op == opLessEqual {
			push(typeBool)
		} else {
			push(typeInt)
		}
	case opEqual:
		if _, ok := pop(2, "equality stack effect"); !ok {
			return nil
		}
		push(typeBool)
	case opNeg:
		values, ok := pop(1, "integer complement stack effect")
		if !ok {
			return nil
		}
		require(values[0], typeInt, "integer complement operand")
		push(typeInt)
	case opNot:
		values, ok := pop(1, "Boolean complement stack effect")
		if !ok {
			return nil
		}
		require(values[0], typeBool, "Boolean complement operand")
		push(typeBool)
	case opIsVoid:
		if _, ok := pop(1, "isvoid stack effect"); !ok {
			return nil
		}
		push(typeBool)
	case opJump:
		if !v.validTarget(method, instruction.Target) {
			v.fail("COL5102", method, index, "jump target", fmt.Sprintf("target %d is not an instruction", instruction.Target), instruction.Span)
			return nil
		}
		return []verifierSuccessor{{index: instruction.Target, state: state}}
	case opJumpIfFalse:
		values, ok := pop(1, "conditional stack effect")
		if !ok {
			return nil
		}
		require(values[0], typeBool, "conditional operand")
		if !v.validTarget(method, instruction.Target) {
			v.fail("COL5102", method, index, "conditional target", fmt.Sprintf("target %d is not an instruction", instruction.Target), instruction.Span)
			return next(state)
		}
		successors := []verifierSuccessor{{index: instruction.Target, state: cloneVerifierState(state)}}
		if index+1 >= len(method.Code) {
			v.fail("COL5109", method, index, "terminal contract", "conditional true edge falls out of the method", instruction.Span)
		} else {
			successors = append(successors, verifierSuccessor{index: index + 1, state: state})
		}
		return successors
	case opNew:
		allocated := v.resolveSelf(instruction.Text, method.Owner)
		if !v.validType(instruction.Text, method.Owner, true) || v.exec.Methods[methodKey(allocated, "$init")] == nil {
			v.fail("COL5114", method, index, "allocation contract", "invalid or uninitialized allocation type "+instruction.Text, instruction.Span)
			allocated = verifierUnknownType
		}
		push(allocated)
	case opCall:
		v.applyCall(method, index, instruction, &state)
	case opReturn:
		values, ok := pop(1, "return stack effect")
		if !ok {
			return nil
		}
		if len(state.stack) != 0 {
			v.fail("COL5109", method, index, "return stack", fmt.Sprintf("return leaves %d extra value(s)", len(state.stack)), instruction.Span)
		}
		if len(state.scopes) != 1 {
			v.fail("COL5106", method, index, "return scope", fmt.Sprintf("return has scope depth %d, want 1", len(state.scopes)), instruction.Span)
		}
		if !v.categoryConforms(values[0], method.ReturnType, method.Owner) {
			v.fail("COL5105", method, index, "return category", fmt.Sprintf("returns %s but method declares %s", values[0], method.ReturnType), instruction.Span)
		}
		if method == v.exec.Entry {
			v.fail("COL5109", method, index, "entry terminal", "entry method may not RETURN", instruction.Span)
		}
		return nil
	case opCaseSelect:
		_, ok := pop(1, "case stack effect")
		if !ok {
			return nil
		}
		if len(instruction.Cases) == 0 {
			v.fail("COL5102", method, index, "case targets", "CASE_SELECT has no branches", instruction.Span)
			return nil
		}
		seenTypes := map[string]bool{}
		successors := make([]verifierSuccessor, 0, len(instruction.Cases))
		for _, target := range instruction.Cases {
			if !v.validTarget(method, target.Target) {
				v.fail("COL5102", method, index, "case target", fmt.Sprintf("target %d for %s is invalid", target.Target, target.Type), instruction.Span)
				continue
			}
			if target.Name == "" || !v.validType(target.Type, method.Owner, false) || seenTypes[target.Type] {
				v.fail("COL5102", method, index, "case metadata", "branch names/types must be non-empty, valid, and unique", instruction.Span)
				continue
			}
			seenTypes[target.Type] = true
			branchState := cloneVerifierState(state)
			branchState.scopes = append(branchState.scopes, map[string]string{target.Name: v.resolveSelf(target.Type, method.Owner)})
			successors = append(successors, verifierSuccessor{index: target.Target, state: branchState})
		}
		return successors
	case opHalt:
		if method != v.exec.Entry {
			v.fail("COL5109", method, index, "halt contract", "HALT is only valid in the entry method", instruction.Span)
		}
		if len(state.stack) != 1 {
			v.fail("COL5109", method, index, "halt stack", fmt.Sprintf("HALT requires exactly one result, has %d", len(state.stack)), instruction.Span)
		}
		if len(state.scopes) != 1 {
			v.fail("COL5106", method, index, "halt scope", fmt.Sprintf("HALT has scope depth %d, want 1", len(state.scopes)), instruction.Span)
		}
		return nil
	default:
		v.fail("COL5101", method, index, "opcode", "unknown opcode "+string(instruction.Op), instruction.Span)
		return nil
	}
	return next(state)
}

func (v *bytecodeVerifier) applyCall(method *compiledMethod, index int, instruction byteInstruction, state *verifierState) {
	if instruction.ArgCount < 0 || instruction.Text == "" {
		v.fail("COL5108", method, index, "call metadata", "method name must be non-empty and argument count non-negative", instruction.Span)
		state.stack = append(state.stack, verifierUnknownType)
		return
	}
	needed := instruction.ArgCount + 1
	if len(state.stack) < needed {
		v.fail("COL5103", method, index, "call stack effect", fmt.Sprintf("needs receiver plus %d argument(s), has %d value(s)", instruction.ArgCount, len(state.stack)), instruction.Span)
		state.stack = nil
		return
	}
	start := len(state.stack) - needed
	values := append([]string(nil), state.stack[start:]...)
	state.stack = state.stack[:start]
	receiver, args := values[len(values)-1], values[:len(values)-1]
	if !validVerifierSpan(instruction.operandSpan()) {
		v.fail("COL5113", method, index, "call operand span", "receiver span is malformed", instruction.Span)
	}
	if instruction.Text == "$init" {
		owner := instruction.StaticType
		if owner == "" {
			owner = receiver
		}
		owner = v.resolveSelf(owner, method.Owner)
		target := v.exec.Methods[methodKey(owner, "$init")]
		if !v.validType(owner, method.Owner, false) || !v.categoryConforms(receiver, owner, method.Owner) ||
			target == nil || instruction.ArgCount != 0 || len(target.Formals) != 0 {
			v.fail("COL5108", method, index, "initializer call", "initializer target or arity is invalid", instruction.Span)
		}
		state.stack = append(state.stack, receiver)
		return
	}
	lookupType := receiver
	if instruction.StaticType != "" {
		lookupType = instruction.StaticType
		if !v.validType(lookupType, method.Owner, false) || !v.categoryConforms(receiver, lookupType, method.Owner) {
			v.fail("COL5108", method, index, "static dispatch", fmt.Sprintf("receiver %s does not support static target %s", receiver, lookupType), instruction.Span)
		}
	}
	lookupType = v.resolveSelf(lookupType, method.Owner)
	info, found := v.lookupCall(lookupType, instruction.Text, instruction.ArgCount)
	if !found {
		v.fail("COL5108", method, index, "call target", fmt.Sprintf("cannot resolve %s/%d on %s", instruction.Text, instruction.ArgCount, lookupType), instruction.Span)
		state.stack = append(state.stack, verifierUnknownType)
		return
	}
	if instruction.ArgCount != len(info.FormalTypes) {
		v.fail("COL5108", method, index, "call arity", fmt.Sprintf("metadata says %d arguments but target expects %d", instruction.ArgCount, len(info.FormalTypes)), instruction.Span)
	}
	for argumentIndex, actual := range args {
		if argumentIndex < len(info.FormalTypes) && !v.categoryConforms(actual, info.FormalTypes[argumentIndex], method.Owner) {
			v.fail("COL5108", method, index, "call argument", fmt.Sprintf("argument %d is %s, target expects %s", argumentIndex, actual, info.FormalTypes[argumentIndex]), instruction.Span)
		}
	}
	if !info.Builtin && v.exec.Methods[methodKey(info.Owner, info.Name)] == nil {
		v.fail("COL5108", method, index, "call body", "compiled target body is missing for "+methodKey(info.Owner, info.Name), instruction.Span)
	}
	resultType := info.ReturnType
	if resultType == typeSelf {
		resultType = receiver
	}
	state.stack = append(state.stack, v.resolveSelf(resultType, method.Owner))
}

func (v *bytecodeVerifier) lookupCall(receiverType, name string, arity int) (*MethodInfo, bool) {
	if v.exec.Table == nil {
		return nil, false
	}
	if receiverType != verifierUnknownType && receiverType != "<void>" {
		info, found := v.exec.Table.LookupMethod(receiverType, name)
		return info, found
	}
	var candidate *MethodInfo
	for _, className := range v.exec.Table.Order {
		info, found := v.exec.Table.LookupMethod(className, name)
		if !found || len(info.FormalTypes) != arity {
			continue
		}
		if candidate == nil {
			candidate = info
			continue
		}
		if candidate.ReturnType != info.ReturnType {
			return &MethodInfo{Name: name, ReturnType: verifierUnknownType, FormalTypes: candidate.FormalTypes, Builtin: true}, true
		}
	}
	return candidate, candidate != nil
}

func (v *bytecodeVerifier) mergeStates(method *compiledMethod, index int, left, right verifierState) (verifierState, bool, bool) {
	span := method.Code[index].Span
	if len(left.stack) != len(right.stack) {
		v.fail("COL5104", method, index, "stack merge", fmt.Sprintf("incoming heights differ: %d and %d", len(left.stack), len(right.stack)), span)
		return left, false, false
	}
	if len(left.scopes) != len(right.scopes) {
		v.fail("COL5106", method, index, "scope merge", fmt.Sprintf("incoming scope depths differ: %d and %d", len(left.scopes), len(right.scopes)), span)
		return left, false, false
	}
	merged := cloneVerifierState(left)
	changed := false
	for i := range merged.stack {
		joined := v.joinCategory(left.stack[i], right.stack[i], method.Owner)
		if joined != merged.stack[i] {
			merged.stack[i] = joined
			changed = true
		}
	}
	for scopeIndex := range merged.scopes {
		leftScope, rightScope := left.scopes[scopeIndex], right.scopes[scopeIndex]
		if len(leftScope) != len(rightScope) {
			v.fail("COL5106", method, index, "scope merge", "incoming scopes bind different names", span)
			return left, false, false
		}
		names := make([]string, 0, len(leftScope))
		for name := range leftScope {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			rightType, exists := rightScope[name]
			if !exists {
				v.fail("COL5106", method, index, "scope merge", "incoming scopes bind different names", span)
				return left, false, false
			}
			joined := v.joinCategory(leftScope[name], rightType, method.Owner)
			if joined != merged.scopes[scopeIndex][name] {
				merged.scopes[scopeIndex][name] = joined
				changed = true
			}
		}
	}
	return merged, changed, true
}

func (v *bytecodeVerifier) verifyMetadata() {
	if v.exec == nil {
		return
	}
	expectedIR := make([]IRInstruction, 0)
	expectedBlocks := []ControlFlowBlock{}
	expectedEdges := []ControlFlowEdge{}
	globalIndex := 0
	for _, method := range v.exec.MethodOrder {
		if method == nil {
			continue
		}
		methodStart := globalIndex
		for _, instruction := range method.Code {
			expectedIR = append(expectedIR, IRInstruction{
				Index: globalIndex, Method: method.DisplayName, Op: string(instruction.Op),
				Args: instructionArgs(instruction), Comment: instruction.Comment,
				Line: instruction.Span.StartLine, Column: instruction.Span.StartColumn, Span: instruction.Span,
				Synthetic:       method.Synthetic || instruction.Span.SourceID == "",
				OriginalIndexes: append([]int(nil), instruction.OriginalIndexes...),
			})
			globalIndex++
		}
		blocks, edges := controlFlowForMethod(method, methodStart)
		expectedBlocks = append(expectedBlocks, blocks...)
		expectedEdges = append(expectedEdges, edges...)
	}
	if !reflect.DeepEqual(v.exec.IR, expectedIR) {
		v.fail("COL5112", nil, -1, "IR metadata", "flattened IR indexes, methods, instructions, or spans do not match bytecode", emptySpan())
	}
	if !reflect.DeepEqual(v.exec.ControlFlow.Blocks, expectedBlocks) || !reflect.DeepEqual(v.exec.ControlFlow.Edges, expectedEdges) {
		v.fail("COL5112", nil, -1, "CFG metadata", "CFG blocks or edges do not match bytecode control flow", emptySpan())
	}
	expectedAssembly, expectedSpans := assemblyForExecutable(v.exec)
	if !reflect.DeepEqual(v.exec.Assembly, expectedAssembly) || !reflect.DeepEqual(v.exec.AssemblySpans, expectedSpans) {
		v.fail("COL5112", nil, -1, "assembly metadata", "pseudo-assembly lines or source mappings do not match bytecode", emptySpan())
	}
}

func (v *bytecodeVerifier) lookupStorage(method *compiledMethod, state verifierState, name string) (string, bool) {
	if name == "" {
		return "", false
	}
	for index := len(state.scopes) - 1; index >= 0; index-- {
		if valueType, found := state.scopes[index][name]; found {
			return valueType, true
		}
	}
	if v.exec.Table != nil {
		if attribute, found := v.exec.Table.LookupAttribute(method.Owner, name); found {
			return v.resolveSelf(attribute.TypeName, method.Owner), true
		}
	}
	return "", false
}

func (v *bytecodeVerifier) validType(typeName, owner string, allowSelf bool) bool {
	if typeName == verifierUnknownType {
		return true
	}
	if typeName == typeSelf {
		return allowSelf && owner != "$program"
	}
	return v.exec != nil && v.exec.Table != nil && v.exec.Table.Has(typeName)
}

func (v *bytecodeVerifier) resolveSelf(typeName, owner string) string {
	if typeName == typeSelf {
		return owner
	}
	return typeName
}

func (v *bytecodeVerifier) categoryConforms(actual, expected, owner string) bool {
	if actual == verifierUnknownType || expected == verifierUnknownType {
		return true
	}
	actual = v.resolveSelf(actual, owner)
	expected = v.resolveSelf(expected, owner)
	if actual == "<void>" {
		return expected != typeInt && expected != typeString && expected != typeBool
	}
	if actual == expected {
		return true
	}
	return v.exec != nil && v.exec.Table != nil && v.exec.Table.IsSubclass(actual, expected)
}

func (v *bytecodeVerifier) validTarget(method *compiledMethod, target int) bool {
	return method != nil && target >= 0 && target < len(method.Code)
}

func (v *bytecodeVerifier) fail(code string, method *compiledMethod, instruction int, invariant, detail string, span Span) {
	methodName := "<executable>"
	if method != nil && method.DisplayName != "" {
		methodName = method.DisplayName
	}
	key := fmt.Sprintf("%s\x00%s\x00%d\x00%s", code, methodName, instruction, invariant)
	if v.reported[key] {
		return
	}
	v.reported[key] = true
	if !validVerifierSpan(span) {
		sourceID := span.SourceID
		if method != nil && validVerifierSpan(method.Span) {
			span = method.Span
		} else {
			span = emptySpan()
			span.SourceID = sourceID
		}
	}
	location := methodName
	if instruction >= 0 {
		location += fmt.Sprintf(" at instruction %d", instruction)
	}
	v.diagnostics = append(v.diagnostics, errorDiagnostic(
		"codegen", code,
		fmt.Sprintf("bytecode verification failed in %s: %s: %s", location, invariant, detail),
		"This is an internal compiler defect; the VM was not started.", span,
	))
}

func cloneVerifierState(source verifierState) verifierState {
	result := verifierState{stack: append([]string(nil), source.stack...), scopes: make([]map[string]string, len(source.scopes))}
	for index, scope := range source.scopes {
		result.scopes[index] = make(map[string]string, len(scope))
		for name, valueType := range scope {
			result.scopes[index][name] = valueType
		}
	}
	return result
}

func (v *bytecodeVerifier) joinCategory(left, right, owner string) string {
	if left == right {
		return left
	}
	if left == verifierUnknownType || right == verifierUnknownType {
		return verifierUnknownType
	}
	left = v.resolveSelf(left, owner)
	right = v.resolveSelf(right, owner)
	if left == "<void>" {
		if right == typeInt || right == typeString || right == typeBool {
			return typeObject
		}
		return right
	}
	if right == "<void>" {
		if left == typeInt || left == typeString || left == typeBool {
			return typeObject
		}
		return left
	}
	if v.exec == nil || v.exec.Table == nil {
		return verifierUnknownType
	}
	visited := map[string]bool{}
	for candidate := left; candidate != "" && !visited[candidate]; candidate = v.exec.Table.ParentOf(candidate) {
		visited[candidate] = true
		if v.exec.Table.IsSubclass(right, candidate) {
			return candidate
		}
	}
	return verifierUnknownType
}

func validVerifierSpan(span Span) bool {
	if span.StartLine < 1 || span.StartColumn < 1 || span.EndLine < span.StartLine || span.EndColumn < 1 || span.StartOffset < 0 || span.EndOffset < span.StartOffset {
		return false
	}
	return span.EndLine != span.StartLine || span.EndColumn >= span.StartColumn
}
