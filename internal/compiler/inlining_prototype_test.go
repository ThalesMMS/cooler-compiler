// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// Test-only experiment. Never called by the shipped optimizer or debugger.
type inlineExperimentReport struct {
	accepted, before, after int
	refused                 map[string]int
}

func experimentalInline(baseline *executable) (*executable, inlineExperimentReport) {
	result := cloneExecutableForOptimization(baseline)
	report := inlineExperimentReport{before: executableInstructionCount(baseline), refused: map[string]int{}}
	serial, growth := 0, 0
	for _, caller := range result.MethodOrder {
		code := caller.Code
		targets := instructionTargets(code)
		mapping := make([]int, len(code))
		output := []byteInstruction{}
		for index, call := range code {
			mapping[index] = len(output)
			if call.Op != opCall {
				output = append(output, cloneByteInstruction(call))
				continue
			}
			receiverType := ""
			if index > 0 && !targets[index] {
				previous := code[index-1]
				if previous.Op == opNew && previous.Text != typeSelf {
					receiverType = previous.Text
				}
				if previous.Op == opLoadSelf && call.StaticType != "" && !caller.Synthetic {
					receiverType = caller.Owner
				}
			}
			lookup := receiverType
			if call.StaticType != "" {
				lookup = call.StaticType
			}
			info, known := baseline.Table.LookupMethod(lookup, call.Text)
			reason := ""
			var callee *compiledMethod
			if receiverType == "" {
				reason = "receiver may be void or dynamic target is not proven"
			} else if !known || info.Builtin {
				reason = "builtin or missing target"
			} else {
				callee = baseline.Methods[methodKey(info.Owner, info.Name)]
				if callee == nil || callee.Synthetic || len(callee.Code) > 16 || (callee.ReturnType != typeInt && callee.ReturnType != typeBool) {
					reason = "target is not a short scalar method"
				}
			}
			var expansion []byteInstruction
			if reason == "" {
				serial++
				expansion, reason = experimentalInlineBody(callee, call, receiverType, serial)
			}
			if reason == "" && growth+len(expansion)-1 > 256 {
				reason = "256-instruction growth budget exceeded"
			}
			if reason != "" {
				report.refused[reason]++
				output = append(output, cloneByteInstruction(call))
				continue
			}
			growth += len(expansion) - 1
			report.accepted++
			output = append(output, expansion...)
		}
		retargetInstructions(output, mapping)
		caller.Code = output
	}
	materializeIR(result)
	materializeAssembly(result)
	report.after = executableInstructionCount(result)
	return result, report
}

func experimentalInlineBody(callee *compiledMethod, call byteInstruction, receiverType string, serial int) ([]byteInstruction, string) {
	if len(callee.Code) == 0 || callee.Code[len(callee.Code)-1].Op != opReturn {
		return nil, "callee has no simple return"
	}
	nameIndex := 0
	fresh := func() string { nameIndex++; return fmt.Sprintf("$inline%d_%d", serial, nameIndex) }
	receiver := fresh()
	scopes := []map[string]string{{}}
	code := []byteInstruction{{Op: opEnterScope, Span: call.Span}, {Op: opBind, Text: receiver, ValueType: receiverType, Span: call.Span}}
	for index := len(callee.Formals) - 1; index >= 0; index-- {
		name := fresh()
		scopes[0][callee.Formals[index]] = name
		code = append(code, byteInstruction{Op: opBind, Text: name, ValueType: callee.FormalTypes[index], Span: call.Span})
	}
	for _, original := range callee.Code[:len(callee.Code)-1] {
		instruction := cloneByteInstruction(original)
		switch instruction.Op {
		case opPushInt, opPushBool, opPushString, opPushVoid, opAdd, opSub, opMul, opDiv, opNeg, opNot, opLess, opLessEqual, opEqual, opIsVoid, opPop, opDup:
		case opLoadSelf:
			instruction.Op = opLoad
			instruction.Text = receiver
		case opLoad:
			resolved := ""
			for index := len(scopes) - 1; index >= 0; index-- {
				if name := scopes[index][instruction.Text]; name != "" {
					resolved = name
					break
				}
			}
			if resolved == "" {
				return nil, "attribute access needs receiver-aware lowering"
			}
			instruction.Text = resolved
		case opEnterScope:
			scopes = append(scopes, map[string]string{})
		case opBind:
			name := fresh()
			scopes[len(scopes)-1][instruction.Text] = name
			instruction.Text = name
			if instruction.ValueType == typeSelf {
				instruction.ValueType = receiverType
			}
		case opExitScopeKeep:
			if len(scopes) < 2 {
				return nil, "unbalanced local scopes"
			}
			scopes = scopes[:len(scopes)-1]
		default:
			return nil, "callee has calls, writes, allocation or control flow"
		}
		instruction.OriginalIndexes = mergeInstructionOrigins(original, call)
		code = append(code, instruction)
	}
	if len(scopes) != 1 {
		return nil, "unbalanced local scopes"
	}
	code = append(code, byteInstruction{Op: opExitScopeKeep, Span: call.Span})
	for index := range code {
		if len(code[index].OriginalIndexes) == 0 {
			code[index].OriginalIndexes = append([]int(nil), call.OriginalIndexes...)
		}
	}
	return code, ""
}

func inlineExperimentExecutable(t *testing.T, source string) *executable {
	t.Helper()
	pipeline := NewCompilationSession(source)
	snapshot := pipeline.Complete()
	if snapshot.Blocked {
		t.Fatalf("prototype source blocked: %+v", snapshot.Result.Diagnostics)
	}
	return pipeline.exec
}

func TestInliningPrototype(t *testing.T) {
	for _, tc := range []struct {
		name, source, output string
		accepted             bool
	}{
		{"static override", `class Base {f(x:Int):Int {x+1};}; class Child inherits Base {f(x:Int):Int {x+2};}; class Main inherits IO {main():Object {out_int((new Child)@Base.f(3))};};`, "4", true},
		{"known dynamic", `class Base {f(x:Int):Int {x+1};}; class Child inherits Base {f(x:Int):Int {x+2};}; class Main inherits IO {main():Object {out_int((new Child).f(3))};};`, "5", true},
		{"unknown dynamic", `class Base {f(x:Int):Int {x+1};}; class Child inherits Base {f(x:Int):Int {x+2};}; class Main inherits IO {main():Object {let b:Base <- new Child in out_int(b.f(3))};};`, "5", false},
		{"void", `class Base {f(x:Int):Int {x+1};}; class Main {main():Object {let b:Base in b@Base.f(3)};};`, "", false},
		{"argument receiver order", `class T inherits IO {a:Object <- out_string("R");f(x:Int,y:Int):Int {x+y};}; class Main inherits IO {main():Object {out_int((new T).f({out_string("A");1;},{out_string("B");2;}))};};`, "ABR3", true},
		{"capture", `class T {f(x:Int):Int {let x:Int <- x+1 in x};}; class Main inherits IO {main():Object {let x:Int <- 99 in {out_int((new T).f(3));out_int(x);}};};`, "499", true},
		{"self type local", `class T {f(x:Int):Int {let current:SELF_TYPE <- self in x+1};}; class Main inherits IO {main():Object {out_int((new T).f(3))};};`, "4", true},
		{"failure span", `class T {f(x:Int):Int {1/x};}; class Main {main():Object {(new T).f(0)};};`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			baseline := inlineExperimentExecutable(t, tc.source)
			candidate, report := experimentalInline(baseline)
			if (report.accepted > 0) != tc.accepted {
				t.Fatalf("eligibility %+v", report)
			}
			if diagnostics := verifyExecutable(candidate); hasErrors(diagnostics) {
				t.Fatalf("prototype verifier: %+v", diagnostics)
			}
			baseVM, inlineVM := newVM(baseline), newVM(candidate)
			baseVM.Run(500, nil)
			inlineVM.Run(500, nil)
			if strings.Join(baseVM.output, "") != tc.output || !reflect.DeepEqual(baseVM.output, inlineVM.output) || !reflect.DeepEqual(baseVM.diagnostics, inlineVM.diagnostics) || baseVM.objectCount != inlineVM.objectCount {
				t.Fatalf("prototype changed behavior: output=%v/%v diagnostics=%+v/%+v", baseVM.output, inlineVM.output, baseVM.diagnostics, inlineVM.diagnostics)
			}
			t.Logf("IR %d -> %d; steps %d -> %d; accepted %d; refusals %v", report.before, report.after, baseVM.executed, inlineVM.executed, report.accepted, report.refused)
		})
	}
}

func TestInliningPrototypeExposesScopeLimitIncompatibility(t *testing.T) {
	source := `class Main inherits IO {f(x:Int):Int {x+1};main():Object {let n:Int <- 3 in out_int(self@Main.f(n))};};`
	baseline := inlineExperimentExecutable(t, source)
	candidate, report := experimentalInline(baseline)
	if report.accepted != 1 {
		t.Fatalf("expected inline candidate: %+v", report)
	}
	baseVM, inlineVM := newVM(baseline), newVM(candidate)
	baseVM.policy.MaxScopeDepth = 2
	inlineVM.policy.MaxScopeDepth = 2
	baseVM.Run(100, nil)
	inlineVM.Run(100, nil)
	if hasErrors(baseVM.diagnostics) || strings.Join(baseVM.output, "") != "4" || !hasDiagnosticCode(inlineVM.diagnostics, "COL6050") {
		t.Fatalf("expected scope-policy incompatibility: %+v / %+v", baseVM.diagnostics, inlineVM.diagnostics)
	}
}

func TestInliningPrototypeExposesFrameLimitIncompatibility(t *testing.T) {
	source := `class Main inherits IO {f(x:Int):Int {x+1};main():Object {out_int(self@Main.f(3))};};`
	baseline := inlineExperimentExecutable(t, source)
	candidate, report := experimentalInline(baseline)
	if report.accepted != 1 {
		t.Fatalf("expected inline candidate: %+v", report)
	}
	run := func(exec *executable) *VM {
		vm := newVM(exec)
		for steps := 0; steps < 100 && vm.currentFrame().method.DisplayName != "Main.main"; steps++ {
			vm.Step()
		}
		if vm.currentFrame().method.DisplayName != "Main.main" {
			t.Fatal("did not reach main")
		}
		vm.policy.MaxCallFrameDepth = len(vm.frames)
		vm.Run(100, nil)
		return vm
	}
	baseVM, inlineVM := run(baseline), run(candidate)
	if firstRuntimeError(baseVM.diagnostics) == nil || hasErrors(inlineVM.diagnostics) || strings.Join(inlineVM.output, "") != "4" {
		t.Fatalf("expected frame-policy incompatibility: %+v / %+v", baseVM.diagnostics, inlineVM.diagnostics)
	}
	t.Log("Decision: do not adopt; inlining bypasses an observable call-frame limit and changes debugger frames/steps")
}
