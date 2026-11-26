// SPDX-License-Identifier: AGPL-3.0-or-later

package examples_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ThalesMMS/cooler-compiler/examples"
	"github.com/ThalesMMS/cooler-compiler/internal/compiler"
)

// courseStackScript is the classic stack-machine course script shipped as
// examples/stack-machine/stack.test: display (d), push (digits, +, s),
// evaluate (e), and exit (x).
const courseStackScript = "e\ne\n1\n+\n2\ns\nd\ne\ne\nd\ne\n+\n1\ns\ns\ns\nd\ne\ne\ns\ne\ne\ne\nd\nx\n"

// courseStackOutput is the stdout the stack-machine program must print for
// courseStackScript. Each '>' is one prompt; the four display commands (d)
// print the stack top-first:
//
//	e e 1 + 2 s d   -> ">>>>>>>" then "s 2 + 1" (top first)
//	e e d           -> ">>>" then "3"          (swap, then 2+1)
//	e + 1 s s s d   -> ">>>>>>>" then "s s s 1 + 3"
//	e e s e e e d   -> ">>>>>>>" then "4"
//	x               -> ">" (final prompt before exit)
const courseStackOutput = ">>>>>>>s\n2\n+\n1\n>>>3\n>>>>>>>s\ns\ns\n1\n+\n3\n>>>>>>>4\n>"

// TestStackMachineMatchesCourseScript pins the PA1 stack-machine example to
// the course stack.test script on the default Go VM.
func TestStackMachineMatchesCourseScript(t *testing.T) {
	read := func(name string) string {
		t.Helper()
		content, err := os.ReadFile(filepath.FromSlash(name))
		if errors.Is(err, fs.ErrNotExist) {
			t.Skipf("%s is not present in this snapshot", name)
		}
		if err != nil {
			t.Fatal(err)
		}
		return string(content)
	}
	program := read("13_stack_machine.cl")
	atoi := read("stack-machine/atoi.cl")
	script := read("stack-machine/stack.test")

	if strings.ReplaceAll(script, "\r\n", "\n") != courseStackScript {
		t.Fatalf("stack-machine/stack.test is no longer the course script:\n%s", script)
	}
	if !strings.Contains(program, "Programming Assignment 1") || !strings.Contains(program, "Stack machine interpreter in Cool.") {
		t.Fatal("13_stack_machine.cl lost its assignment header")
	}

	units := []compiler.SourceUnit{
		{ID: "stack-machine", Name: "13_stack_machine.cl", Content: program},
		{ID: "atoi", Name: "atoi.cl", Content: atoi},
	}
	result := compiler.CompileProject(units, compiler.ProjectOptions{Input: script})
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Severity == compiler.SeverityError {
			t.Fatalf("%s %s: %s", diagnostic.Stage, diagnostic.Code, diagnostic.Message)
		}
	}
	if !result.Runtime.Halted {
		t.Fatal("VM did not halt")
	}
	if got := strings.Join(result.Runtime.Output, ""); got != courseStackOutput {
		t.Fatalf("stack-machine stdout on stack.test changed:\n got %q\nwant %q", got, courseStackOutput)
	}

	catalog, err := examples.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, example := range catalog {
		if example.ID == "13_stack_machine" {
			if example.Expected != courseStackOutput {
				t.Fatalf("catalog expected output drifted from the course script output:\n got %q\nwant %q", example.Expected, courseStackOutput)
			}
			if example.Input != script {
				t.Fatal("catalog input is not stack-machine/stack.test")
			}
			return
		}
	}
	t.Fatal("stack-machine example is missing from the catalog")
}
