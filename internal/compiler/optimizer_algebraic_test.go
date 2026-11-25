// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestAlgebraicIdentitiesPreserveEvaluation(t *testing.T) {
	for _, expression := range []string{"x + 0", "x - 0", "x * 1", "~(~x)"} {
		for _, value := range []string{"0", "1", "2147483647", "-2147483648"} {
			source := fmt.Sprintf(`class Main inherits IO {main():Object {let x:Int <- in_int() in out_int(%s)};};`, expression)
			baseline := compileWithOptimization(source, value+"\n", OptimizationNone)
			optimized := compileWithOptimization(source, value+"\n", OptimizationBasic)
			if optimizationFullObservable(baseline) != optimizationFullObservable(optimized) {
				t.Fatalf("%s %s changed behavior", expression, value)
			}
			found := false
			for _, pass := range optimized.Optimization.Passes {
				if pass.Contract.Name == "algebraic-identities" && pass.Transformations > 0 && pass.Verified {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s was not simplified", expression)
			}
			if len(optimized.IR) >= len(baseline.IR) {
				t.Fatal("no instructions removed")
			}
		}
	}
	source := `class Main inherits IO {main():Object {if not (not (in_int() = 1)) then out_string("yes") else out_string("no") fi};};`
	for _, input := range []string{"1\n", "2\n"} {
		baseline := compileWithOptimization(source, input, OptimizationNone)
		optimized := compileWithOptimization(source, input, OptimizationBasic)
		if optimizationFullObservable(baseline) != optimizationFullObservable(optimized) {
			t.Fatal("Boolean identity changed input/output")
		}
		if len(optimized.IR) >= len(baseline.IR) {
			t.Fatal("Boolean negation was not simplified")
		}
	}
}

func TestAlgebraicKeepsOperandFailureAndHeap(t *testing.T) {
	for _, source := range []string{
		`class Main inherits IO {main():Object {out_int((1 / 0) + 0)};};`,
		`class Box inherits IO {x:Object <- out_string("new"); value():Int {7};}; class Main inherits IO {main():Object {out_int((new Box).value() * 1)};};`,
	} {
		baseline := compileWithOptimization(source, "", OptimizationNone)
		optimized := compileWithOptimization(source, "", OptimizationBasic)
		if optimizationFullObservable(baseline) != optimizationFullObservable(optimized) {
			t.Fatalf("operand behavior changed: %+v %+v", baseline.Diagnostics, optimized.Diagnostics)
		}
		if !hasErrors(baseline.Diagnostics) && !reflect.DeepEqual(heapAfterOptimization(t, source, OptimizationNone), heapAfterOptimization(t, source, OptimizationBasic)) {
			t.Fatal("identity changed heap")
		}
	}
}

func TestAlgebraicRefusesInteriorTargetsAndZeroMultiply(t *testing.T) {
	for _, target := range []int{1, 2} {
		method := &compiledMethod{DisplayName: "Main.main", Code: []byteInstruction{{Op: opLoad, Text: "x"}, {Op: opPushInt, Int: 0}, {Op: opAdd}, {Op: opJump, Target: target}}}
		before := append([]byteInstruction(nil), method.Code...)
		report := algebraicIdentityPass(&executable{MethodOrder: []*compiledMethod{method}})
		if report.Transformations != 0 || report.Rejected == 0 || !reflect.DeepEqual(method.Code, before) {
			t.Fatalf("interior target %d changed", target)
		}
	}
	result := compileWithOptimization(`class Main inherits IO {main():Object {out_int(in_int() * 0)};};`, "7\n", OptimizationBasic)
	found := false
	for _, pass := range result.Optimization.Passes {
		for _, r := range pass.Rejections {
			found = found || strings.Contains(r.Reason, "zero multiplication")
		}
	}
	if !found {
		t.Fatal("zero multiplication refusal missing")
	}
}

func TestAlgebraicPreservesOriginsAndOperandSpan(t *testing.T) {
	span := Span{SourceID: "unit-2", StartLine: 2, StartColumn: 3, EndLine: 2, EndColumn: 8}
	method := &compiledMethod{DisplayName: "Main.main", Code: []byteInstruction{
		{Op: opLoad, Text: "x", Span: span, OriginalIndexes: []int{10}},
		{Op: opPushInt, Int: 0, OriginalIndexes: []int{11}},
		{Op: opAdd, OriginalIndexes: []int{12}},
		{Op: opReturn, OriginalIndexes: []int{13}},
	}}
	report := algebraicIdentityPass(&executable{MethodOrder: []*compiledMethod{method}})
	if report.Transformations != 1 || len(method.Code) != 2 || method.Code[0].Span != span || !reflect.DeepEqual(method.Code[0].OriginalIndexes, []int{10, 11, 12}) {
		t.Fatalf("lost operand/origins: %+v", method.Code)
	}
}
