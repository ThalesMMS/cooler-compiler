// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func TestRuntimeDispatchAndEvaluationOrder(t *testing.T) {
	source := `
class Base {
  label() : String { "Base" };
  inherited_call() : String { label() };
};
class Child inherits Base {
  label() : String { "Child" };
};
class Main inherits IO {
  count : Int <- 0;
  receiver() : SELF_TYPE {{ out_string("R"); self; }};
  mark(label : String, value : Int) : Int {{
    out_string(label);
    count <- count + 1;
    value;
  }};
  combine(first : Int, second : Int) : Int { first * 10 + second };
  main() : Object {{
    out_string((new Child).inherited_call());
    out_string("/");
    out_string((new Child)@Base.label());
    out_string("/");
    out_int(receiver().combine(mark("A", 1), mark("B", 2)));
    out_string("/");
    out_int(mark("L", 3) + mark("Q", 4));
    out_string("/");
    out_int(count);
  }};
};
`
	result := Compile(source)
	assertNoErrors(t, result.Diagnostics)
	if got, want := outputText(result.Runtime.Output), "Child/Base/ABR12/LQ7/4"; got != want {
		t.Fatalf("dispatch/evaluation output = %q, want %q", got, want)
	}
	assertRuntimeHaltConsistent(t, result.Runtime, "")
}

func TestRuntimeObjectCopyIsShallowDistinctAndDynamic(t *testing.T) {
	source := `
class Cell {
  value : Int <- 1;
  set(next : Int) : SELF_TYPE {{ value <- next; self; }};
  get() : Int { value };
};
class Holder {
  cell : Cell <- new Cell;
  get() : Cell { cell };
};
class FancyHolder inherits Holder {};
class Main inherits IO {
  emit(value : Bool) : Object { if value then out_string("T") else out_string("F") fi };
  main() : Object {
    let original : Holder <- new FancyHolder, clone : Holder <- original.copy() in {
      original.get().set(7);
      out_int(clone.get().get());
      emit(original = clone);
      emit(original.get() = clone.get());
      out_string(clone.type_name());
    }
  };
};
`
	result := Compile(source)
	assertNoErrors(t, result.Diagnostics)
	if got, want := outputText(result.Runtime.Output), "7FTFancyHolder"; got != want {
		t.Fatalf("copy output = %q, want %q", got, want)
	}
}

func TestRuntimeOperatorsAndEqualityCategories(t *testing.T) {
	source := `
class Main inherits IO {
  emit(value : Bool) : Object { if value then out_string("T") else out_string("F") fi };
  main() : Object {{
    emit(1 = 1);
    emit(1 = 2);
    emit("same" = "same");
    emit("left" = "right");
    emit(true = true);
    emit(true = false);
    let same : Object <- new Object, other : Object <- new Object, empty : Object in {
      emit(same = same);
      emit(same = other);
      emit(empty = empty);
      emit(empty = same);
      emit(isvoid empty);
      emit(isvoid same);
    };
    out_string("/");
    out_int(~1);
    out_string("/");
    out_int(1 + 2 * 3 - 4 / 2);
    out_string("/");
    emit(1 < 2);
    emit(2 <= 2);
    emit(not false);
  }};
};
`
	result := Compile(source)
	assertNoErrors(t, result.Diagnostics)
	if got, want := outputText(result.Runtime.Output), "TFTFTFTFTFTF/-1/5/TTT"; got != want {
		t.Fatalf("operator/equality output = %q, want %q", got, want)
	}
}

func TestRuntimeCaseEvaluatesOnceAndBindsOriginalValue(t *testing.T) {
	source := `
class Base {};
class Middle inherits Base {};
class Deep inherits Middle {};
class Main inherits IO {
  subject() : Base {{ out_string("S"); new Deep; }};
  main() : Object {
    case subject() of
      base : Base => out_string("Base");
      middle : Middle => out_string(middle.type_name());
    esac
  };
};
`
	result := Compile(source)
	assertNoErrors(t, result.Diagnostics)
	if got, want := outputText(result.Runtime.Output), "SDeep"; got != want {
		t.Fatalf("case output = %q, want %q", got, want)
	}
}

func TestRuntimeCaseHandlesDeepHierarchyIteratively(t *testing.T) {
	const depth = 6000
	table := newClassTable()
	parent := typeObject
	for i := 0; i < depth; i++ {
		name := "C" + strconv.Itoa(i)
		table.Classes[name] = &ClassInfo{Name: name, Parent: parent, Attributes: map[string]*AttrInfo{}, Methods: map[string]*MethodInfo{}}
		table.Order = append(table.Order, name)
		parent = name
	}
	method := &compiledMethod{Code: []byteInstruction{{Op: opCaseSelect}, {Op: opHalt}}}
	frame := &vmFrame{method: method, scopes: []map[string]runtimeValueInternal{{}}}
	value := objectValue(&runtimeObject{id: 1, class: parent, fields: map[string]runtimeValueInternal{}})
	vm := &VM{exec: &executable{Table: table}, frames: []*vmFrame{frame}, stack: []runtimeValueInternal{value}}
	instruction := byteInstruction{
		Op: opCaseSelect, Cases: []caseTarget{
			{Name: "root", Type: typeObject, Target: 1},
			{Name: "near", Type: "C5990", Target: 1},
			{Name: "middle", Type: "C3000", Target: 1},
		},
		Span: emptySpan(), OperandSpan: emptySpan(),
	}
	vm.executeCase(frame, instruction)
	if vm.halted || frame.ip != 1 || len(frame.scopes) != 2 {
		t.Fatalf("deep case state: halted=%v ip=%d scopes=%d diagnostics=%+v", vm.halted, frame.ip, len(frame.scopes), vm.diagnostics)
	}
	bound, found := frame.lookup("near")
	if !found || bound.o != value.o {
		t.Fatalf("case did not bind the original value: (%+v, %v)", bound, found)
	}
}

func TestRuntimeUnicodeStringBuiltinsUseCodePoints(t *testing.T) {
	result := Compile(`
class Main inherits IO {
  main() : Object {{
    out_int("😀é".length());
    out_string("/");
    out_string("😀é".substr(1, 1));
    out_string("/");
    out_string("😀".concat("é"));
  }};
};
`)
	assertNoErrors(t, result.Diagnostics)
	if got, want := outputText(result.Runtime.Output), "2/é/😀é"; got != want {
		t.Fatalf("Unicode builtins output = %q, want %q", got, want)
	}
}

func TestSubstringBoundsHandleMaximumCoolInt(t *testing.T) {
	method := &MethodInfo{Owner: typeString, Name: "substr", Builtin: true}
	vm := &VM{}
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("String.substr panicked: %v", recovered)
		}
	}()
	_, ok := vm.executeBuiltin(method, stringValue("abc"), []runtimeValueInternal{intValue(1), intValue(coolIntMax)}, emptySpan())
	if ok || !vm.halted || len(vm.diagnostics) != 1 || vm.diagnostics[0].Code != "COL6046" {
		t.Fatalf("overflow bounds result: ok=%v halted=%v diagnostics=%+v", ok, vm.halted, vm.diagnostics)
	}
}

func TestRuntimeFailureSpansAndStateAreStable(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		code       string
		spanSource string
	}{
		{
			name: "dispatch void receiver",
			source: `class Value { get() : Int { 1 }; };
class Main { main() : Object { let target : Value in target.get() }; };`,
			code: "COL6020", spanSource: "target.get",
		},
		{
			name: "case void subject",
			source: `class Value {};
class Main { main() : Object { let target : Value in case target of value : Value => value; esac }; };`,
			code: "COL6030", spanSource: "target of",
		},
		{
			name: "case no match subject",
			source: `class Value {}; class Other {};
class Main { main() : Object { case new Value of other : Other => other; esac }; };`,
			code: "COL6031", spanSource: "new Value of",
		},
		{
			name:   "division by zero",
			source: `class Main { main() : Int { 10 / (1 - 1) }; };`,
			code:   "COL6015", spanSource: "10 / (1 - 1)",
		},
		{
			name:   "substring bounds",
			source: `class Main { main() : String { "abc".substr(2, 2) }; };`,
			code:   "COL6046", spanSource: `"abc".substr(2, 2)`,
		},
		{
			name:   "abort",
			source: `class Main { main() : Object { abort() }; };`,
			code:   "COL6040", spanSource: "abort()",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := Compile(test.source)
			diagnostic := runtimeDiagnostic(t, result.Runtime.Diagnostics, test.code)
			start := strings.Index(test.source, test.spanSource)
			if start < 0 {
				t.Fatalf("span marker %q not found", test.spanSource)
			}
			wantEnd := start + len(test.spanSource)
			if test.code == "COL6020" || test.code == "COL6030" || test.code == "COL6031" {
				wantEnd = start + strings.Index(test.spanSource, map[string]string{"COL6020": ".", "COL6030": " of", "COL6031": " of"}[test.code])
			}
			if diagnostic.Span.StartOffset != start || diagnostic.Span.EndOffset != wantEnd {
				t.Fatalf("%s span = %d-%d, want %d-%d", test.code, diagnostic.Span.StartOffset, diagnostic.Span.EndOffset, start, wantEnd)
			}
			assertRuntimeHaltConsistent(t, result.Runtime, test.code)
		})
	}
}

func TestInternalVMFailureIsDistinctAndExportable(t *testing.T) {
	method := &compiledMethod{
		Owner: "Main", Name: "broken", DisplayName: "Main.broken", Span: emptySpan(),
		Code: []byteInstruction{{Op: opcode("BROKEN"), Span: emptySpan()}},
	}
	vm := newVM(&executable{Entry: method})
	vm.Run(10, nil)
	result := vm.result(0)
	assertRuntimeHaltConsistent(t, result, "COL6008")
	if _, err := json.Marshal(result); err != nil {
		t.Fatalf("marshal failed VM state: %v", err)
	}
}

func assertRuntimeHaltConsistent(t testing.TB, result VMResult, code string) {
	t.Helper()
	if !result.Halted || result.Paused {
		t.Fatalf("runtime terminal flags: halted=%v paused=%v", result.Halted, result.Paused)
	}
	if code != "" {
		runtimeDiagnostic(t, result.Diagnostics, code)
	}
	for _, frame := range result.Frames {
		if frame.StackBase < 0 || frame.StackBase > len(result.Stack) {
			t.Fatalf("frame %s.%s stack base %d exceeds exported stack %d", frame.Owner, frame.FunctionName, frame.StackBase, len(result.Stack))
		}
	}
	if len(result.Trace) == 0 {
		t.Fatal("terminal runtime omitted trace")
	}
	if _, err := json.Marshal(result); err != nil {
		t.Fatalf("terminal runtime is not exportable: %v", err)
	}
}

func runtimeDiagnostic(t testing.TB, diagnostics []Diagnostic, code string) Diagnostic {
	t.Helper()
	for _, diagnostic := range diagnostics {
		if diagnostic.Stage == "runtime" && diagnostic.Code == code {
			return diagnostic
		}
	}
	t.Fatalf("runtime diagnostic %s not found: %+v", code, diagnostics)
	return Diagnostic{}
}
