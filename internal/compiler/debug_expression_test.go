// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"reflect"
	"strings"
	"testing"
)

func TestDebugExpressionEvaluatesSafeSubsetWithoutMutation(t *testing.T) {
	object := &runtimeObject{id: 1, class: "Main", fields: map[string]runtimeValueInternal{"value": intValue(4)}}
	frame := &vmFrame{
		self:   objectValue(object),
		scopes: []map[string]runtimeValueInternal{{"x": intValue(1)}, {"x": intValue(3), "empty": voidValue()}},
	}
	beforeScopes := cloneDebugScopes(frame.scopes)
	beforeFields := cloneDebugFields(object.fields)

	tests := []struct {
		expression string
		want       string
	}{
		{"x + value = 7", "true"},
		{"not (x < value)", "false"},
		{"~x", "-3"},
		{"isvoid empty", "true"},
		{"isvoid void", "true"},
		{"self = self", "true"},
		{"\"cool\" = \"cool\"", "true"},
	}
	for _, test := range tests {
		parsed, err := parseDebugExpression(test.expression)
		if err != nil {
			t.Fatalf("parse %q: %v", test.expression, err)
		}
		value, err := evaluateDebugExpression(parsed, frame)
		if err != nil {
			t.Fatalf("evaluate %q: %v", test.expression, err)
		}
		if got := value.public().Display; got != test.want {
			t.Fatalf("evaluate %q = %s, want %s", test.expression, got, test.want)
		}
	}
	if !reflect.DeepEqual(frame.scopes, beforeScopes) || !reflect.DeepEqual(object.fields, beforeFields) {
		t.Fatal("debugger evaluation mutated the frame or heap")
	}
}

func TestDebugExpressionRejectsSideEffectsAndReportsEvaluationErrors(t *testing.T) {
	for _, expression := range []string{"x <- 1", "new Object", "copy()", "self.copy()", "if true then 1 else 0 fi", "let x : Int <- 1 in x"} {
		if _, err := parseDebugExpression(expression); err == nil {
			t.Fatalf("unsafe expression %q was accepted", expression)
		}
	}
	frame := &vmFrame{scopes: []map[string]runtimeValueInternal{{"x": intValue(1)}}}
	for _, test := range []struct {
		expression string
		contains   string
	}{{"x / 0", "division by zero"}, {"x + true", "expects Int"}, {"missing", "unknown"}} {
		parsed, err := parseDebugExpression(test.expression)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = evaluateDebugExpression(parsed, frame); err == nil || !strings.Contains(err.Error(), test.contains) {
			t.Fatalf("evaluate %q error = %v, want %q", test.expression, err, test.contains)
		}
	}
}

func TestConditionalBreakpointLogpointWatchesAndResetAreDeterministic(t *testing.T) {
	const source = `class Main {
  main() : Object {
    let i : Int <- 0 in
      while i < 3 loop
        i <- i + 1
      pool
  };
};`
	session := NewDebugSession(source)
	column := breakpointColumnForLine(t, session, 5)
	enabled := true
	conditional := Breakpoint{ID: "loop", Line: 5, Column: column, Condition: "i = 1", Enabled: &enabled}
	session.SetWatches([]WatchExpression{{ID: "i", Expression: "i"}, {ID: "bad", Expression: "i <- 9"}})
	paused := session.RunWithBreakpoints(100_000, []Breakpoint{conditional})
	if paused.StopReason != StopReasonBreakpoint || !paused.Runtime.Paused {
		t.Fatalf("conditional breakpoint did not pause: %+v", paused)
	}
	if len(paused.Breakpoints) != 1 || !paused.Breakpoints[0].Verified || paused.Breakpoints[0].Hits < 2 {
		t.Fatalf("breakpoint state = %+v", paused.Breakpoints)
	}
	if len(paused.Watches) != 2 || paused.Watches[0].Value == nil || paused.Watches[0].Value.Display != "1" || paused.Watches[1].Diagnostic == "" {
		t.Fatalf("watch results = %+v", paused.Watches)
	}

	reset := session.Reset()
	if len(reset.Breakpoints) != 1 || reset.Breakpoints[0].Hits != 0 || len(reset.DebugLog) != 0 {
		t.Fatalf("reset retained debugger counters or log: %+v", reset)
	}
	logpoint := Breakpoint{ID: "loop-log", Line: 5, Column: column, LogMessage: "i={i}", Enabled: &enabled}
	finished := session.RunWithBreakpoints(100_000, []Breakpoint{logpoint})
	if !finished.Runtime.Halted || finished.StopReason != StopReasonHalt || len(finished.DebugLog) != 3 {
		t.Fatalf("logpoint should run without pausing: %+v", finished)
	}
	for index, entry := range finished.DebugLog {
		if entry.Order != index+1 || entry.Message != "i="+string(rune('0'+index)) {
			t.Fatalf("debug log[%d] = %+v", index, entry)
		}
	}

	threshold := Breakpoint{ID: "third", Line: 5, Column: column, HitCount: 3, Enabled: &enabled}
	for replay := 0; replay < 2; replay++ {
		session.Reset()
		third := session.RunWithBreakpoints(100_000, []Breakpoint{threshold})
		if third.StopReason != StopReasonBreakpoint || len(third.Breakpoints) != 1 || third.Breakpoints[0].Hits != 3 || third.Runtime.ExecutedSteps != 42 {
			t.Fatalf("third-hit replay %d was not deterministic: %+v", replay, third)
		}
	}
}

func TestInvalidConditionalBreakpointCannotHaltProgram(t *testing.T) {
	const source = "class Main { main() : Object { 1 + 2 }; };"
	session := NewDebugSession(source)
	enabled := true
	finished := session.RunWithBreakpoints(100, []Breakpoint{{ID: "invalid", Line: 1, Condition: "1 + 2", Enabled: &enabled}})
	if !finished.Runtime.Halted || len(finished.Runtime.Diagnostics) != 0 {
		t.Fatalf("invalid debugger condition affected program: %+v", finished.Runtime)
	}
	if len(finished.Breakpoints) != 1 || !strings.Contains(finished.Breakpoints[0].Diagnostic, "Bool") {
		t.Fatalf("condition diagnostic = %+v", finished.Breakpoints)
	}

	failing := NewDebugSession("class Main { main() : Object { 1 / 0 }; };")
	runtimeError := failing.RunWithBreakpoints(100, []Breakpoint{{ID: "false", Line: 1, Condition: "false", Enabled: &enabled}})
	if runtimeError.StopReason != StopReasonRuntimeError || !hasDiagnosticCode(runtimeError.Runtime.Diagnostics, "COL6015") {
		t.Fatalf("false condition masked the program runtime error: %+v", runtimeError)
	}

	const chunkedSource = "class Main {\n  main() : Object {\n    let i : Int <- 0 in i + 1\n  };\n};"
	chunked := NewDebugSession(chunkedSource)
	bindColumn := breakpointColumnForOp(t, chunked, 3, opBind)
	invalid := Breakpoint{ID: "chunked", Line: 3, Column: bindColumn, Condition: "i", Enabled: &enabled}
	firstChunk := chunked.RunWithBreakpoints(14, []Breakpoint{invalid})
	if len(firstChunk.Breakpoints) != 1 || firstChunk.Breakpoints[0].Diagnostic == "" {
		t.Fatalf("first chunk omitted condition diagnostic: %+v", firstChunk.Breakpoints)
	}
	lastChunk := chunked.RunWithBreakpoints(100, []Breakpoint{invalid})
	if len(lastChunk.Breakpoints) != 1 || lastChunk.Breakpoints[0].Diagnostic == "" {
		t.Fatalf("later chunk lost condition diagnostic: %+v", lastChunk.Breakpoints)
	}
}

func TestConditionalBreakpointUsesRecursiveFrameAndAttributes(t *testing.T) {
	const source = `class Main {
  value : Int <- 2;
  down(n : Int) : Object {
    if n = 0 then value else down(n - 1) fi
  };
  main() : Object { down(3) };
};`
	session := NewDebugSession(source)
	column := breakpointColumnForOp(t, session, 4, opCall)
	enabled := true
	session.SetWatches([]WatchExpression{{ID: "state", Expression: "n + value"}, {ID: "void", Expression: "isvoid void"}})
	paused := session.RunWithBreakpoints(100_000, []Breakpoint{{
		ID: "recursive", Line: 4, Column: column, Condition: "n = 1", Enabled: &enabled,
	}})
	if paused.StopReason != StopReasonBreakpoint || paused.Location == nil || paused.Location.FrameDepth < 2 {
		t.Fatalf("recursive breakpoint = %+v", paused)
	}
	if len(paused.Watches) != 2 || paused.Watches[0].Value == nil || paused.Watches[0].Value.Display != "3" || paused.Watches[1].Value == nil || paused.Watches[1].Value.Display != "true" {
		t.Fatalf("recursive attribute/void watches = %+v", paused.Watches)
	}
}

func TestDebuggerLogIsBounded(t *testing.T) {
	method := &compiledMethod{DisplayName: "Main.main"}
	vm := newVMWithPolicy(&executable{Entry: method}, DefaultVMPolicy())
	vm.frames = []*vmFrame{{method: method}}
	instruction := byteInstruction{Span: Span{SourceID: "main", StartLine: 4}}
	message := strings.Repeat("x", maxDebugLogMessage)
	for index := 0; index < maxDebugLogEntries+50; index++ {
		vm.appendDebugLog(message, instruction)
	}
	if len(vm.debugLog) > maxDebugLogEntries || vm.debugLogBytes > maxDebugLogBytes || vm.debugLog[0].Order <= 1 {
		t.Fatalf("debugger log bounds = entries %d bytes %d first order %d", len(vm.debugLog), vm.debugLogBytes, vm.debugLog[0].Order)
	}
}

func breakpointColumnForLine(t *testing.T, session *DebugSession, line int) int {
	t.Helper()
	for _, method := range session.exec.MethodOrder {
		for _, instruction := range method.Code {
			if instruction.Span.StartLine == line && instruction.Op == opStore {
				return instruction.Span.StartColumn
			}
		}
	}
	t.Fatalf("no store instruction on line %d", line)
	return 0
}

func breakpointColumnForOp(t *testing.T, session *DebugSession, line int, operation opcode) int {
	t.Helper()
	for _, method := range session.exec.MethodOrder {
		for _, instruction := range method.Code {
			if instruction.Span.StartLine == line && instruction.Op == operation {
				return instruction.Span.StartColumn
			}
		}
	}
	t.Fatalf("no %s instruction on line %d", operation, line)
	return 0
}

func cloneDebugScopes(scopes []map[string]runtimeValueInternal) []map[string]runtimeValueInternal {
	result := make([]map[string]runtimeValueInternal, len(scopes))
	for index, scope := range scopes {
		result[index] = cloneDebugFields(scope)
	}
	return result
}

func cloneDebugFields(fields map[string]runtimeValueInternal) map[string]runtimeValueInternal {
	result := map[string]runtimeValueInternal{}
	for key, value := range fields {
		result[key] = value
	}
	return result
}
