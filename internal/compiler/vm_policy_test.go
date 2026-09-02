// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestVMPolicyDefaultsValidationAndReportMetadata(t *testing.T) {
	defaults, err := NormalizeVMPolicy(VMPolicy{})
	if err != nil || defaults != DefaultVMPolicy() {
		t.Fatalf("default policy = %+v, %v", defaults, err)
	}
	maximum := MaximumVMPolicy()
	if normalized, err := NormalizeVMPolicy(maximum); err != nil || normalized != maximum {
		t.Fatalf("maximum policy rejected: %+v, %v", normalized, err)
	}
	policyType := reflect.TypeOf(maximum)
	maximumValue := reflect.ValueOf(maximum)
	for index := 0; index < policyType.NumField(); index++ {
		name := strings.Split(policyType.Field(index).Tag.Get("json"), ",")[0]
		t.Run(name+"/above-maximum", func(t *testing.T) {
			invalid := maximum
			reflect.ValueOf(&invalid).Elem().Field(index).SetInt(maximumValue.Field(index).Int() + 1)
			if _, err := NormalizeVMPolicy(invalid); err == nil {
				t.Fatal("policy above the local ceiling was accepted")
			}
		})
		t.Run(name+"/negative", func(t *testing.T) {
			invalid := VMPolicy{}
			reflect.ValueOf(&invalid).Elem().Field(index).SetInt(-1)
			if _, err := NormalizeVMPolicy(invalid); err == nil {
				t.Fatal("negative policy value was accepted")
			}
		})
	}
	onlyTotal, err := NormalizeVMPolicy(VMPolicy{MaxInstructionsTotal: 10})
	if err != nil || onlyTotal.MaxInstructionsPerRun != 10 {
		t.Fatalf("total-only policy = %+v, %v", onlyTotal, err)
	}
	explicitConflict := VMPolicy{MaxInstructionsPerRun: 11, MaxInstructionsTotal: 10}
	if _, err := NormalizeVMPolicy(explicitConflict); err == nil {
		t.Fatal("explicit per-run limit above total was accepted")
	}

	result := Compile(`class Main { main() : Object { 0 }; };`)
	if result.SchemaVersion != ReportSchemaVersion || result.Options.VMPolicy != DefaultVMPolicy() {
		t.Fatalf("report omitted normalized policy: %+v", result.Options)
	}
	blocked := NewCompilationSessionWithContract(`class Main { main() : Object { 0 }; };`, ResultContract{
		Options: CompilationOptions{VMPolicy: VMPolicy{MaxInputBytes: -1}},
	}).Complete()
	if !blocked.Blocked || !hasDiagnosticCode(blocked.Result.Diagnostics, "COL0005") {
		t.Fatalf("invalid policy did not form an input barrier: %+v", blocked)
	}
}

func TestVMInstructionPolicyBoundaries(t *testing.T) {
	policy := DefaultVMPolicy()
	policy.MaxInstructionsPerRun = 2
	for _, requested := range []int{1, 2, 3} {
		vm := policyVM(policy, []byteInstruction{
			{Op: opPushInt, Span: emptySpan()}, {Op: opPop, Span: emptySpan()}, {Op: opPushInt, Span: emptySpan()}, {Op: opHalt, Span: emptySpan()},
		})
		vm.Run(requested, nil)
		want := requested
		if want > policy.MaxInstructionsPerRun {
			want = policy.MaxInstructionsPerRun
		}
		if vm.executed != want || vm.halted {
			t.Fatalf("per-run request %d executed=%d halted=%v", requested, vm.executed, vm.halted)
		}
	}

	policy = DefaultVMPolicy()
	policy.MaxInstructionsPerRun = 2
	policy.MaxInstructionsTotal = 2
	for _, before := range []int{1, 2, 3} {
		vm := policyVM(policy, []byteInstruction{{Op: opPushInt, Span: emptySpan()}, {Op: opHalt, Span: emptySpan()}})
		vm.executed = before
		vm.Step()
		if before < policy.MaxInstructionsTotal {
			if vm.executed != before+1 || hasDiagnosticCode(vm.diagnostics, "COL6002") {
				t.Fatalf("total limit-1 rejected: %+v", vm)
			}
		} else if !vm.halted || vm.executed != before || !hasDiagnosticCode(vm.diagnostics, "COL6002") {
			t.Fatalf("total boundary %d was not contained: %+v", before, vm)
		}
	}
}

func TestVMStackFrameScopeAndObjectBoundaries(t *testing.T) {
	policy := DefaultVMPolicy()
	policy.MaxOperandStackDepth = 2
	for _, before := range []int{1, 2, 3} {
		vm := &VM{policy: policy, stack: make([]runtimeValueInternal, before)}
		ok := vm.push(intValue(1), emptySpan())
		if before < 2 && (!ok || len(vm.stack) != before+1) {
			t.Fatalf("stack limit-1 rejected: %+v", vm)
		}
		if before >= 2 && (ok || len(vm.stack) != before || !hasDiagnosticCode(vm.diagnostics, "COL6010")) {
			t.Fatalf("stack boundary %d mutated: %+v", before, vm)
		}
	}

	policy = DefaultVMPolicy()
	policy.MaxCallFrameDepth = 2
	method := &compiledMethod{DisplayName: "Main.f", Formals: nil, Span: emptySpan()}
	for _, before := range []int{1, 2, 3} {
		vm := &VM{policy: policy, frames: make([]*vmFrame, before)}
		ok := vm.canStartCompiledCall(method, nil, emptySpan())
		if before < 2 && !ok {
			t.Fatalf("frame limit-1 rejected: %+v", vm)
		}
		if before >= 2 && (ok || len(vm.frames) != before || !hasDiagnosticCode(vm.diagnostics, "COL6024")) {
			t.Fatalf("frame boundary %d mutated: %+v", before, vm)
		}
	}

	policy = DefaultVMPolicy()
	policy.MaxScopeDepth = 2
	for _, before := range []int{1, 2, 3} {
		vm := policyVM(policy, []byteInstruction{{Op: opEnterScope, Span: emptySpan()}})
		vm.frames[0].scopes = make([]map[string]runtimeValueInternal, before)
		vm.Step()
		if before < 2 && (vm.halted || len(vm.frames[0].scopes) != before+1) {
			t.Fatalf("scope limit-1 rejected: %+v", vm)
		}
		if before >= 2 && (!vm.halted || len(vm.frames[0].scopes) != before || !hasDiagnosticCode(vm.diagnostics, "COL6050")) {
			t.Fatalf("scope boundary %d mutated: %+v", before, vm)
		}
	}

	policy = DefaultVMPolicy()
	policy.MaxObjectAllocations = 2
	for _, before := range []int{1, 2, 3} {
		vm := &VM{policy: policy, exec: &executable{Classes: map[string]*runtimeClass{"Box": {Name: "Box"}}}, objectCount: before, nextObjectID: before + 1}
		_, ok := vm.allocate("Box", emptySpan())
		if before < 2 && (!ok || vm.objectCount != before+1) {
			t.Fatalf("object limit-1 rejected: %+v", vm)
		}
		if before >= 2 && (ok || vm.objectCount != before || vm.nextObjectID != before+1 || !hasDiagnosticCode(vm.diagnostics, "COL6019")) {
			t.Fatalf("object boundary %d mutated: %+v", before, vm)
		}
	}
	for _, before := range []int{1, 2, 3} {
		receiver := objectValue(&runtimeObject{id: 1, class: "Box", fields: map[string]runtimeValueInternal{"self": voidValue()}})
		vm := &VM{policy: policy, objectCount: before, nextObjectID: before + 1}
		_, ok := vm.executeBuiltin(&MethodInfo{Owner: typeObject, Name: "copy", Builtin: true}, receiver, nil, emptySpan())
		if before < 2 && (!ok || vm.objectCount != before+1 || vm.nextObjectID != before+2) {
			t.Fatalf("copy limit-1 rejected: %+v", vm)
		}
		if before >= 2 && (ok || vm.objectCount != before || vm.nextObjectID != before+1 || !hasDiagnosticCode(vm.diagnostics, "COL6019")) {
			t.Fatalf("copy boundary %d mutated: %+v", before, vm)
		}
	}
}

func TestVMStringOutputAndInputBoundaries(t *testing.T) {
	policy := DefaultVMPolicy()
	policy.MaxStringBytes = 2
	for _, size := range []int{1, 2, 3} {
		vm := &VM{policy: policy}
		ok := vm.checkStringSize(strings.Repeat("x", size), emptySpan())
		if size <= 2 && !ok {
			t.Fatalf("string size %d rejected", size)
		}
		if size > 2 && (ok || !hasDiagnosticCode(vm.diagnostics, "COL6051")) {
			t.Fatalf("string size %d accepted: %+v", size, vm)
		}
	}
	vm := &VM{policy: policy}
	if _, ok := vm.executeBuiltin(&MethodInfo{Owner: typeObject, Name: "type_name", Builtin: true}, objectValue(&runtimeObject{class: "Box"}), nil, emptySpan()); ok || !hasDiagnosticCode(vm.diagnostics, "COL6051") {
		t.Fatalf("Object.type_name escaped the string bound: %+v", vm)
	}

	policy = DefaultVMPolicy()
	policy.MaxOutputBytes = 2
	for _, before := range []int{1, 2, 3} {
		vm := &VM{policy: policy, outputBytes: before, output: []string{strings.Repeat("x", before)}}
		ok := vm.appendOutput("x", emptySpan())
		if before < 2 && (!ok || vm.outputBytes != before+1) {
			t.Fatalf("output limit-1 rejected: %+v", vm)
		}
		if before >= 2 && (ok || vm.outputBytes != before || len(vm.output) != 1 || !hasDiagnosticCode(vm.diagnostics, "COL6052")) {
			t.Fatalf("output boundary %d mutated: %+v", before, vm)
		}
	}

	policy = DefaultVMPolicy()
	policy.MaxInputBytes = 2
	for _, size := range []int{1, 2, 3} {
		vm := &VM{policy: policy}
		vm.SetInput(strings.Repeat("x", size))
		if size <= 2 && (vm.halted || len(vm.input) != 1) {
			t.Fatalf("input bytes %d rejected: %+v", size, vm)
		}
		if size > 2 && (!vm.halted || len(vm.input) != 0 || !hasDiagnosticCode(vm.diagnostics, "COL6053")) {
			t.Fatalf("input bytes %d accepted: %+v", size, vm)
		}
	}

	policy = DefaultVMPolicy()
	policy.MaxInputLines = 2
	for _, lines := range []int{1, 2, 3} {
		vm := &VM{policy: policy}
		vm.SetInput(strings.TrimSuffix(strings.Repeat("x\n", lines), "\n"))
		if lines <= 2 && (vm.halted || len(vm.input) != lines) {
			t.Fatalf("input lines %d rejected: %+v", lines, vm)
		}
		if lines > 2 && (!vm.halted || len(vm.input) != 0 || !hasDiagnosticCode(vm.diagnostics, "COL6054")) {
			t.Fatalf("input lines %d accepted: %+v", lines, vm)
		}
	}
}

func TestVMTraceAndBreakpointBoundaries(t *testing.T) {
	policy := DefaultVMPolicy()
	policy.MaxTraceEntries = 2
	for _, entries := range []int{1, 2, 3} {
		vm := &VM{policy: policy}
		for index := 0; index < entries; index++ {
			vm.record("TEST", emptySpan(), "")
		}
		wantRetained := entries
		if wantRetained > 2 {
			wantRetained = 2
		}
		if len(vm.trace) != wantRetained || vm.traceDropped != entries-wantRetained {
			t.Fatalf("trace entry boundary %d: retained=%d dropped=%d", entries, len(vm.trace), vm.traceDropped)
		}
	}

	probe := &VM{policy: DefaultVMPolicy()}
	probe.record("TEST", emptySpan(), "")
	oneState := probe.traceSizes[0]
	for _, bytes := range []int{oneState - 1, oneState, oneState + 1} {
		policy := DefaultVMPolicy()
		policy.MaxTraceBytes = bytes
		vm := &VM{policy: policy}
		vm.record("TEST", emptySpan(), "")
		want := 1
		if bytes < oneState {
			want = 0
		}
		if len(vm.trace) != want || vm.traceBytes > bytes {
			t.Fatalf("trace byte boundary %d retained=%d bytes=%d state=%d", bytes, len(vm.trace), vm.traceBytes, oneState)
		}
	}

	policy = DefaultVMPolicy()
	policy.MaxBreakpoints = 2
	for _, count := range []int{1, 2, 3} {
		vm := policyVM(policy, []byteInstruction{{Op: opPushInt, Span: emptySpan()}, {Op: opHalt, Span: emptySpan()}})
		breakpoints := map[Breakpoint]bool{}
		for index := 0; index < count; index++ {
			breakpoints[Breakpoint{Line: 100 + index}] = true
		}
		vm.Run(1, breakpoints)
		if count <= 2 && (vm.halted || vm.executed != 1) {
			t.Fatalf("breakpoint count %d rejected: %+v", count, vm)
		}
		if count > 2 && (!vm.halted || vm.executed != 0 || !hasDiagnosticCode(vm.diagnostics, "COL6055")) {
			t.Fatalf("breakpoint count %d accepted: %+v", count, vm)
		}
	}
}

func TestVMFailuresAreTransactionalAndCancellationIsTerminal(t *testing.T) {
	vm := &VM{policy: DefaultVMPolicy(), stack: []runtimeValueInternal{intValue(1)}}
	before := append([]runtimeValueInternal(nil), vm.stack...)
	vm.executeIntegerBinary(byteInstruction{Op: opAdd, Span: emptySpan()})
	if !reflect.DeepEqual(vm.stack, before) || !hasDiagnosticCode(vm.diagnostics, "COL6011") {
		t.Fatalf("binary underflow mutated the stack: %+v", vm)
	}

	frame := &vmFrame{method: &compiledMethod{DisplayName: "Main.f"}, stackBase: 2}
	vm = &VM{policy: DefaultVMPolicy(), stack: []runtimeValueInternal{intValue(7)}, frames: []*vmFrame{frame}}
	vm.executeReturn(byteInstruction{Op: opReturn, Span: emptySpan()})
	if len(vm.stack) != 1 || len(vm.frames) != 1 || !hasDiagnosticCode(vm.diagnostics, "COL6027") {
		t.Fatalf("invalid return mutated state: %+v", vm)
	}

	policy := DefaultVMPolicy()
	policy.MaxOutputBytes = 1
	receiver := objectValue(&runtimeObject{id: 1, class: typeIO, fields: map[string]runtimeValueInternal{}})
	vm = &VM{
		policy: policy,
		exec:   &executable{Table: newClassTable()},
		stack:  []runtimeValueInternal{stringValue("too large"), receiver},
	}
	before = append([]runtimeValueInternal(nil), vm.stack...)
	vm.executeCall(byteInstruction{Op: opCall, Text: "out_string", ArgCount: 1, Span: emptySpan()})
	if !reflect.DeepEqual(vm.stack, before) || len(vm.output) != 0 || !hasDiagnosticCode(vm.diagnostics, "COL6052") {
		t.Fatalf("failed builtin call mutated observable state: %+v", vm)
	}

	policy = DefaultVMPolicy()
	policy.MaxScopeDepth = 1
	method := &compiledMethod{DisplayName: "Main.main", Code: []byteInstruction{{Op: opCaseSelect}}}
	frame = &vmFrame{method: method, scopes: []map[string]runtimeValueInternal{{}}}
	vm = &VM{policy: policy, exec: &executable{Table: newClassTable()}, stack: []runtimeValueInternal{intValue(7)}, frames: []*vmFrame{frame}}
	before = append([]runtimeValueInternal(nil), vm.stack...)
	vm.executeCase(frame, byteInstruction{Op: opCaseSelect, Cases: []caseTarget{{Name: "x", Type: typeInt, Target: 0}}, Span: emptySpan()})
	if !reflect.DeepEqual(vm.stack, before) || len(frame.scopes) != 1 || !hasDiagnosticCode(vm.diagnostics, "COL6050") {
		t.Fatalf("failed case selection mutated observable state: %+v", vm)
	}

	vm = policyVM(DefaultVMPolicy(), []byteInstruction{{Op: opPushInt, Span: emptySpan()}})
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	vm.RunContext(cancelled, 1, nil)
	if !vm.halted || vm.executed != 0 || !hasDiagnosticCode(vm.diagnostics, "COL6056") {
		t.Fatalf("cancellation did not contain execution: %+v", vm)
	}
	vm.Run(1, nil)
	if vm.executed != 0 {
		t.Fatal("cancelled VM resumed without reset")
	}
}

func TestPublicObjectValuesAreCycleSafe(t *testing.T) {
	object := &runtimeObject{id: 1, class: "Node", fields: map[string]runtimeValueInternal{}}
	object.fields["next"] = objectValue(object)
	vm := &VM{policy: DefaultVMPolicy(), stack: []runtimeValueInternal{objectValue(object)}}
	stack := vm.publicStack()
	if len(stack) != 1 || stack[0].Display != "Node#1" || stack[0].Object != "Node#1" {
		t.Fatalf("cycle-safe public identity changed: %+v", stack)
	}
}

func policyVM(policy VMPolicy, code []byteInstruction) *VM {
	method := &compiledMethod{Owner: "$program", Name: "$entry", DisplayName: "$program.$entry", Span: emptySpan(), Code: code}
	return &VM{
		policy:       policy,
		frames:       []*vmFrame{{method: method, scopes: []map[string]runtimeValueInternal{{}}}},
		nextObjectID: 1,
	}
}
