// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestDeadComputationsRemoveDiscardedPureValues(t *testing.T) {
	source := `class Main inherits IO {main():Object {{1 + 2; not false; isvoid 1; out_string("kept");}};};`
	base := compileWithOptimization(source, "", OptimizationNone)
	optimized := compileWithOptimization(source, "", OptimizationBasic)
	if optimizationFullObservable(base) != optimizationFullObservable(optimized) {
		t.Fatal("dead value removal changed behavior")
	}
	found := false
	for _, pass := range optimized.Optimization.Passes {
		if pass.Contract.Name == "dead-computations" && pass.Transformations >= 3 && pass.Verified {
			found = true
		}
	}
	if !found {
		t.Fatal("discarded pure values were not removed")
	}
	for _, instruction := range optimized.IR {
		if len(instruction.OriginalIndexes) == 0 {
			t.Fatal("origin lost")
		}
	}
}

func TestDeadComputationsPreserveObservableEvaluation(t *testing.T) {
	for _, source := range []string{
		`class Main inherits IO {main():Object {{1 / 0;0;}};};`,
		`class Main inherits IO {main():Object {{in_int();out_string("kept");}};};`,
		`class T inherits IO {x:Object <- out_string("allocated");}; class Main {main():Object {{new T;0;}};};`,
		`class Main {main():Object {{while true loop 0 pool;0;}};};`,
	} {
		compile := func(mode string) Result {
			return CompileWithContract(source, "3\n", ResultContract{Options: CompilationOptions{Execute: true, VMMaxSteps: 100, Optimization: mode}})
		}
		base := compile(OptimizationNone)
		optimized := compile(OptimizationBasic)
		if base.Runtime.Halted != optimized.Runtime.Halted || base.Runtime.InputEvents != optimized.Runtime.InputEvents || strings.Join(base.Runtime.Output, "") != strings.Join(optimized.Runtime.Output, "") {
			t.Fatal("observable discarded evaluation removed")
		}
		baseError, optError := firstRuntimeError(base.Diagnostics), firstRuntimeError(optimized.Diagnostics)
		if !reflect.DeepEqual(baseError, optError) {
			t.Fatalf("discarded error changed: %+v %+v", baseError, optError)
		}
	}
}

func TestDeadComputationRejectsInteriorTarget(t *testing.T) {
	method := &compiledMethod{DisplayName: "Main.main", Code: []byteInstruction{{Op: opPushInt, Int: 1}, {Op: opPop}, {Op: opJump, Target: 1}}}
	before := append([]byteInstruction(nil), method.Code...)
	report := deadComputationPass(&executable{MethodOrder: []*compiledMethod{method}})
	if report.Transformations != 0 || report.Rejected == 0 || !reflect.DeepEqual(before, method.Code) {
		t.Fatal("incoming edge bypassed producer")
	}
}

func TestGeneratedDeadComputationsAndStringFailure(t *testing.T) {
	for value := 0; value < 16; value++ {
		source := fmt.Sprintf(`class Main inherits IO {main():Object {{%d + 7; %d < 8; out_int(%d);}};};`, value, value, value)
		base := compileWithOptimization(source, "", OptimizationNone)
		optimized := compileWithOptimization(source, "", OptimizationBasic)
		if optimizationFullObservable(base) != optimizationFullObservable(optimized) {
			t.Fatalf("generated discarded computation %d changed behavior", value)
		}
	}
	source := `class Main {main():Object {{"too long";0;}};};`
	policy := DefaultVMPolicy()
	policy.MaxStringBytes = 3
	compile := func(mode string) Result {
		return CompileWithContract(source, "", ResultContract{Options: CompilationOptions{Execute: true, VMMaxSteps: 100, Optimization: mode, VMPolicy: policy}})
	}
	base, optimized := compile(OptimizationNone), compile(OptimizationBasic)
	if firstRuntimeError(base.Diagnostics) == nil || !reflect.DeepEqual(firstRuntimeError(base.Diagnostics), firstRuntimeError(optimized.Diagnostics)) {
		t.Fatal("discarded string resource check was removed")
	}
}
