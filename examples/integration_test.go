// SPDX-License-Identifier: AGPL-3.0-or-later

package examples_test

import (
	"strings"
	"testing"

	"github.com/ThalesMMS/cooler-compiler/examples"
	"github.com/ThalesMMS/cooler-compiler/internal/compiler"
)

func TestValidExamplesCompileAndMatchOutput(t *testing.T) {
	catalog, err := examples.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, example := range catalog {
		if example.IntentionalError {
			continue
		}
		t.Run(example.ID, func(t *testing.T) {
			var result compiler.Result
			if len(example.Files) > 0 {
				units := make([]compiler.SourceUnit, len(example.Files))
				for index, file := range example.Files {
					units[index] = compiler.SourceUnit{ID: "example-" + example.ID + "-" + file.Name, Name: file.Name, Content: file.Source}
				}
				result = compiler.CompileProject(units, compiler.ProjectOptions{Input: example.Input})
			} else if example.Input != "" {
				result = compiler.CompileWithInput(example.Source, example.Input)
			} else {
				result = compiler.Compile(example.Source)
			}
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Severity == compiler.SeverityError {
					t.Fatalf("%s %s: %s", diagnostic.Stage, diagnostic.Code, diagnostic.Message)
				}
			}
			if !result.Runtime.Halted {
				t.Fatal("VM did not halt")
			}
			if got := strings.Join(result.Runtime.Output, ""); got != example.Expected {
				t.Fatalf("output %q, want %q", got, example.Expected)
			}
		})
	}
}

func TestIntentionalErrorExamplesProduceErrors(t *testing.T) {
	catalog, err := examples.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, example := range catalog {
		if !example.IntentionalError {
			continue
		}
		t.Run(example.ID, func(t *testing.T) {
			result := compiler.Compile(example.Source)
			found := false
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Severity == compiler.SeverityError {
					found = true
					break
				}
			}
			if !found {
				t.Fatal("expected at least one error diagnostic")
			}
		})
	}
}
