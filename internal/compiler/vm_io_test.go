// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"reflect"
	"testing"
)

func TestSplitProgramInput(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{name: "empty", text: "", want: nil},
		{name: "single line without newline", text: "hi", want: []string{"hi"}},
		{name: "trailing newline dropped", text: "hi\n", want: []string{"hi"}},
		{name: "two lines with trailing newline", text: "a\nb\n", want: []string{"a", "b"}},
		{name: "two lines without trailing newline", text: "a\nb", want: []string{"a", "b"}},
		{name: "crlf normalized", text: "a\r\nb\r\n", want: []string{"a", "b"}},
		{name: "only newline is one empty line", text: "\n", want: []string{""}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := splitProgramInput(test.text)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("splitProgramInput(%q) = %#v, want %#v", test.text, got, test.want)
			}
		})
	}
}

func TestInStringConsumesTwoLines(t *testing.T) {
	source := `class Main inherits IO {
  main() : Object {
    out_string(in_string().concat(in_string()))
  };
};`
	result := CompileWithInput(source, "hello\nworld\n")
	assertNoErrors(t, result.Diagnostics)
	if got := outputText(result.Runtime.Output); got != "helloworld" {
		t.Fatalf("output %q, want %q", got, "helloworld")
	}
}

func TestInStringExhaustedInputReturnsEmpty(t *testing.T) {
	source := `class Main inherits IO {
  main() : Object {
    {
      out_string(in_string());
      out_string("|");
      out_string(in_string());
    }
  };
};`
	result := CompileWithInput(source, "only\n")
	assertNoErrors(t, result.Diagnostics)
	if got := outputText(result.Runtime.Output); got != "only|" {
		t.Fatalf("output %q, want %q", got, "only|")
	}
}

func TestInIntParsesALine(t *testing.T) {
	source := `class Main inherits IO {
  main() : Object {
    out_int(in_int())
  };
};`
	result := CompileWithInput(source, "42\n")
	assertNoErrors(t, result.Diagnostics)
	if got := outputText(result.Runtime.Output); got != "42" {
		t.Fatalf("output %q, want %q", got, "42")
	}
}

func TestCompileWithoutInputReturnsEmptyAndZero(t *testing.T) {
	source := `class Main inherits IO {
  main() : Object {
    {
      out_string(in_string());
      out_int(in_int());
    }
  };
};`
	result := Compile(source)
	assertNoErrors(t, result.Diagnostics)
	if got := outputText(result.Runtime.Output); got != "0" {
		t.Fatalf("output %q, want %q", got, "0")
	}
}

func TestSetInputPreservesInputPos(t *testing.T) {
	vm := &VM{}
	vm.SetInput("a\nb\n")
	if got := vm.readLine(); got != "a" {
		t.Fatalf("first readLine = %q, want %q", got, "a")
	}
	vm.SetInput("x\ny\nz\n")
	if vm.inputPos != 1 {
		t.Fatalf("inputPos = %d, want 1", vm.inputPos)
	}
	if got := vm.readLine(); got != "y" {
		t.Fatalf("next readLine = %q, want %q", got, "y")
	}
}
