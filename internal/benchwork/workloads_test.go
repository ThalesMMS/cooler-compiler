// SPDX-License-Identifier: AGPL-3.0-or-later

package benchwork_test

import (
	"reflect"
	"testing"

	"github.com/ThalesMMS/cooler-compiler/internal/benchwork"
	"github.com/ThalesMMS/cooler-compiler/internal/compiler"
)

func TestGeneratedWorkloadsAreDeterministicAndValid(t *testing.T) {
	if first, second := benchwork.StandardProjects(), benchwork.StandardProjects(); !reflect.DeepEqual(first, second) {
		t.Fatal("standard benchmark projects are not deterministic")
	}
	workloads := []benchwork.Workload{
		benchwork.Project("test", benchwork.ProjectSize{Classes: 8, MethodsPerClass: 3, ExpressionTerms: 6, ClassesPerUnit: 3}),
		benchwork.DeepInheritance(20),
		benchwork.NestedExpression(100),
		benchwork.LongText(10),
		benchwork.ArithmeticLoop(20),
		benchwork.RecursiveCalls(20),
		benchwork.Dispatch(20, false),
		benchwork.Dispatch(20, true),
		benchwork.AllocationLoop(20),
		benchwork.DeepCase(20),
		benchwork.StringOperations(10),
		benchwork.IOOperations(10),
	}
	for _, workload := range workloads {
		t.Run(workload.Name, func(t *testing.T) {
			var result compiler.Result
			if len(workload.Units) == 0 {
				result = compiler.CompileWithInput(workload.Source, workload.Input)
			} else {
				units := make([]compiler.SourceUnit, 0, len(workload.Units))
				for _, unit := range workload.Units {
					units = append(units, compiler.SourceUnit{ID: unit.ID, Name: unit.Name, Content: unit.Content})
				}
				result = compiler.CompileProject(units, compiler.ProjectOptions{Input: workload.Input, VMMaxSteps: 100_000})
			}
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Severity == compiler.SeverityError {
					t.Fatalf("generated workload failed: %s %s", diagnostic.Code, diagnostic.Message)
				}
			}
		})
	}
}

func TestGeneratedWorkloadBounds(t *testing.T) {
	project := benchwork.Project("bounded", benchwork.ProjectSize{Classes: 10_000, MethodsPerClass: 100, ExpressionTerms: 1_000, ClassesPerUnit: 10_000})
	if len(project.Units) != 1 || len(project.Source) > 20<<20 {
		t.Fatalf("project generator escaped bounds: units=%d bytes=%d", len(project.Units), len(project.Source))
	}
	if got := benchwork.DeepInheritance(10_000); len(got.Source) > 1<<20 {
		t.Fatalf("deep generator escaped bounds: %d bytes", len(got.Source))
	}
}
