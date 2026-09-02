// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"encoding/json"
	"strings"
	"testing"
	"unsafe"
)

func TestCoolIntHasExplicitWidthAndModularArithmetic(t *testing.T) {
	if got := unsafe.Sizeof(coolInt(0)); got != 4 {
		t.Fatalf("coolInt width = %d bytes, want 4", got)
	}

	tests := []struct {
		name string
		got  coolInt
		want coolInt
	}{
		{name: "maximum plus one", got: coolIntMax.add(1), want: coolIntMin},
		{name: "minimum minus one", got: coolIntMin.subtract(1), want: coolIntMax},
		{name: "maximum times two", got: coolIntMax.multiply(2), want: -2},
		{name: "minimum complement", got: coolIntMin.complement(), want: coolIntMin},
		{name: "negative maximum", got: coolIntMax.complement(), want: -2147483647},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.got != test.want {
				t.Fatalf("result = %s, want %s", test.got, test.want)
			}
		})
	}

	if got, ok := coolIntMin.divide(-1); !ok || got != coolIntMin {
		t.Fatalf("minimum / -1 = %s, %v; want %s, true", got, ok, coolIntMin)
	}
	if _, ok := coolIntMax.divide(0); ok {
		t.Fatal("division by zero was accepted")
	}
	if !coolIntMin.less(coolIntMax) || !coolIntMax.lessEqual(coolIntMax) || !coolIntMin.equal(coolIntMin) {
		t.Fatal("boundary comparisons do not use signed COOL Int order")
	}
}

func TestCoolIntLiteralAndInputParsingAreSeparate(t *testing.T) {
	literalTests := []struct {
		text string
		want coolInt
		ok   bool
	}{
		{text: "0", want: 0, ok: true},
		{text: "00000000002147483647", want: coolIntMax, ok: true},
		{text: "2147483648", ok: false},
		{text: "-1", ok: false},
	}
	for _, test := range literalTests {
		got, ok := parseCoolIntLiteral(test.text)
		if got != test.want || ok != test.ok {
			t.Errorf("parseCoolIntLiteral(%q) = %s, %v; want %s, %v", test.text, got, ok, test.want, test.ok)
		}
	}

	inputTests := []struct {
		text string
		want coolInt
		ok   bool
	}{
		{text: " 2147483647 ", want: coolIntMax, ok: true},
		{text: "-2147483648", want: coolIntMin, ok: true},
		{text: "+1", want: 1, ok: true},
		{text: "2147483648", ok: false},
		{text: "-2147483649", ok: false},
		{text: strings.Repeat("9", 100_000), ok: false},
		{text: "not-an-int", ok: false},
		{text: "", ok: false},
	}
	for _, test := range inputTests {
		got, ok := parseCoolIntInput(test.text)
		if got != test.want || ok != test.ok {
			t.Errorf("parseCoolIntInput(%q) = %s, %v; want %s, %v", test.text, got, ok, test.want, test.ok)
		}
	}
}

func TestCoolIntBoundariesExecuteEndToEnd(t *testing.T) {
	result := Compile(`class Main inherits IO {
  min_int() : Int { (~2147483647) - 1 };
  main() : Object {{
    out_int(2147483647 + 1); out_string("/");
    out_int(min_int() - 1); out_string("/");
    out_int(2147483647 * 2); out_string("/");
    out_int(~min_int()); out_string("/");
    out_int(min_int() / ~1); out_string("/");
    if (2147483647 + 1) < 0 then out_string("T") else out_string("F") fi;
    if (2147483647 + 1) = min_int() then out_string("T") else out_string("F") fi;
    out_string("/"); out_int(min_int()); out_string("/"); out_int(~2147483647);
  }};
};`)
	assertNoErrors(t, result.Diagnostics)
	want := "-2147483648/2147483647/-2/-2147483648/-2147483648/TT/-2147483648/-2147483647"
	if got := outputText(result.Runtime.Output); got != want {
		t.Fatalf("boundary output = %q, want %q", got, want)
	}

	encoded, err := json.Marshal(result.Runtime)
	if err != nil {
		t.Fatalf("marshal runtime: %v", err)
	}
	if !strings.Contains(string(encoded), `"display":"-2147483648"`) {
		t.Fatalf("runtime JSON does not preserve signed 32-bit display: %s", encoded)
	}
}

func TestCoolIntInputAndOutputBoundaries(t *testing.T) {
	result := CompileWithInput(`class Main inherits IO {
  main() : Object {{
    out_int(in_int()); out_string("/"); out_int(in_int()); out_string("/");
    out_int(in_int()); out_string("/"); out_int(in_int()); out_string("/");
    out_int(in_int()); out_string("/"); out_int(in_int());
  }};
};`, "2147483647\n-2147483648\n2147483648\n-2147483649\n999999999999999999999999999999\n +1 \n")
	assertNoErrors(t, result.Diagnostics)
	if got, want := outputText(result.Runtime.Output), "2147483647/-2147483648/0/0/0/1"; got != want {
		t.Fatalf("input boundary output = %q, want %q", got, want)
	}
}

func TestCoolIntBytecodePreservesSignedBitPattern(t *testing.T) {
	instruction := byteInstruction{Op: opPushInt, Int: coolIntMin, Span: emptySpan()}
	if got := instructionArgs(instruction); len(got) != 1 || got[0] != "-2147483648" {
		t.Fatalf("PUSH_INT arguments = %v, want [-2147483648]", got)
	}

	method := &compiledMethod{
		Owner: "$program", Name: "$entry", DisplayName: "$program.$entry",
		Code: []byteInstruction{instruction, {Op: opHalt, Span: emptySpan()}},
		Span: emptySpan(), Synthetic: true,
	}
	vm := newVM(&executable{Entry: method})
	vm.Run(defaultVMMaxSteps, nil)
	result := vm.result(0)
	if len(result.Stack) != 1 || result.Stack[0].Display != "-2147483648" {
		t.Fatalf("bytecode result stack = %+v", result.Stack)
	}
}
