// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestClassDeclarationDiagnostics(t *testing.T) {
	source := `
class SELF_TYPE {};
class Object {};
class Duplicate {};
class Duplicate {};
class ForbiddenInt inherits Int {};
class ForbiddenString inherits String {};
class ForbiddenBool inherits Bool {};
class ForbiddenSelf inherits SELF_TYPE {};
class MissingParent inherits Unknown {};
class Main { main() : Object { 0 }; };
`
	analysis := analyzeClassSource(t, source)
	assertSemanticCodeCounts(t, analysis.Diagnostics, map[string]int{
		"COL3001": 1,
		"COL3002": 2,
		"COL3010": 4,
		"COL3011": 1,
	})
	duplicate := semanticDiagnostic(t, analysis.Diagnostics, "COL3002", 1)
	if len(duplicate.Related) != 1 || duplicate.Related[0].Label != "first definition" {
		t.Fatalf("duplicate class related spans = %+v", duplicate.Related)
	}
	for _, name := range []string{"ForbiddenInt", "ForbiddenString", "ForbiddenBool", "ForbiddenSelf", "MissingParent"} {
		if got := analysis.Table.ParentOf(name); got != typeObject {
			t.Fatalf("recovered parent of %s = %q, want Object", name, got)
		}
	}
}

func TestFeatureDeclarationDiagnosticsAndRelatedSpans(t *testing.T) {
	source := `
class Broken {
  self : Missing;
  value : Int;
  value : String;
  duplicate() : Object { 0 };
  duplicate(x : Int) : Object { x };
  formals(self : SELF_TYPE, repeated : Missing, repeated : Int) : Missing { 0 };
};
class Main { main() : Object { 0 }; };
`
	analysis := analyzeClassSource(t, source)
	assertSemanticCodeCounts(t, analysis.Diagnostics, map[string]int{
		"COL3020": 1,
		"COL3021": 1,
		"COL3022": 1,
		"COL3030": 1,
		"COL3031": 1,
		"COL3032": 1,
		"COL3033": 1,
		"COL3034": 1,
		"COL3035": 1,
	})
	for _, code := range []string{"COL3022", "COL3030", "COL3035"} {
		if related := semanticDiagnostic(t, analysis.Diagnostics, code, 0).Related; len(related) != 1 {
			t.Fatalf("%s related spans = %+v", code, related)
		}
	}
}

func TestOverrideValidationAcrossDeepInheritance(t *testing.T) {
	source := `
class Root {
  value : Int;
  identify(x : Int) : Int { x };
};
class Middle inherits Root {};
class AttrMismatch inherits Middle { value : Int; };
class ArityMismatch inherits Middle { identify() : Int { 0 }; };
class FormalMismatch inherits Middle { identify(x : String) : Int { 0 }; };
class ReturnMismatch inherits Middle { identify(x : Int) : String { "" }; };
class ValidOverride inherits Middle { identify(x : Int) : Int { x }; };
class Main inherits ValidOverride { main() : Object { identify(value) }; };
`
	analysis := analyzeClassSource(t, source)
	assertSemanticCodeCounts(t, analysis.Diagnostics, map[string]int{"COL3040": 1, "COL3041": 3})
	for _, diagnostic := range analysis.Diagnostics {
		if diagnostic.Code == "COL3040" || diagnostic.Code == "COL3041" {
			if len(diagnostic.Related) != 1 {
				t.Fatalf("%s related spans = %+v", diagnostic.Code, diagnostic.Related)
			}
		}
	}
	method, found := analysis.Table.LookupMethod("Main", "identify")
	if !found || method.Owner != "ValidOverride" {
		t.Fatalf("deep method lookup = (%+v, %v)", method, found)
	}
	attribute, found := analysis.Table.LookupAttribute("Main", "value")
	if !found || attribute.Owner != "Root" {
		t.Fatalf("deep attribute lookup = (%+v, %v)", attribute, found)
	}
}

func TestMainEntryPointRules(t *testing.T) {
	tests := []struct {
		name   string
		source string
		codes  []string
	}{
		{"missing class", `class Helper {};`, []string{"COL3050"}},
		{"inherited main is rejected", `class Parent { main() : Object { 0 }; }; class Main inherits Parent {};`, []string{"COL3051"}},
		{"main has formals", `class Main { main(x : Int) : Object { x }; };`, []string{"COL3052"}},
		{"undefined return", `class Main { main() : Missing { 0 }; };`, []string{"COL3031"}},
		{"body mismatch", `class Main { main() : Int { "wrong" }; };`, []string{"COL3061"}},
		{"self type return", `class Main { main() : SELF_TYPE { self }; };`, nil},
		{"entry point survives unrelated errors", `class Broken inherits Missing {};`, []string{"COL3011", "COL3050"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			analysis := analyzeClassSource(t, test.source)
			actual := semanticErrorCodes(analysis.Diagnostics)
			if strings.Join(actual, ",") != strings.Join(test.codes, ",") {
				t.Fatalf("semantic codes = %v, want %v\ndiagnostics: %+v", actual, test.codes, analysis.Diagnostics)
			}
		})
	}
}

func TestIndependentCyclesRecoverDeterministically(t *testing.T) {
	source := `
class A inherits B {};
class B inherits A {};
class C inherits D {};
class D inherits E {};
class E inherits C {};
class F inherits A {};
class Main { main() : Object { 0 }; };
`
	var baseline string
	for iteration := 0; iteration < 20; iteration++ {
		analysis := analyzeClassSource(t, source)
		cycles := []Diagnostic{}
		for _, diagnostic := range analysis.Diagnostics {
			if diagnostic.Code == "COL3012" {
				cycles = append(cycles, diagnostic)
			}
		}
		if len(cycles) != 2 {
			t.Fatalf("cycle diagnostics = %+v", cycles)
		}
		if len(cycles[0].Related) != 1 || len(cycles[1].Related) != 2 {
			t.Fatalf("cycle related spans = %d, %d", len(cycles[0].Related), len(cycles[1].Related))
		}
		current := cycles[0].Message + "\n" + cycles[1].Message
		if iteration == 0 {
			baseline = current
		} else if current != baseline {
			t.Fatalf("cycle diagnostic order changed:\n%s\nvs\n%s", baseline, current)
		}
		for _, name := range []string{"A", "B", "C", "D", "E"} {
			if analysis.Table.ParentOf(name) != typeObject {
				t.Fatalf("cycle member %s did not recover to Object", name)
			}
			if !analysis.Table.IsSubclass(name, typeObject) {
				t.Fatalf("recovered %s does not conform to Object", name)
			}
		}
		if distance, ok := analysis.Table.Distance("F", typeObject); !ok || distance != 2 {
			t.Fatalf("F to Object distance = %d, %v", distance, ok)
		}
	}
}

func TestDeepHierarchyUsesBoundedIterativeValidation(t *testing.T) {
	const depth = 6000
	var source strings.Builder
	source.WriteString("class Root { value : Int; identify() : Int { value }; };\n")
	parent := "Root"
	for i := 0; i < depth; i++ {
		name := fmt.Sprintf("C%d", i)
		fmt.Fprintf(&source, "class %s inherits %s {};\n", name, parent)
		parent = name
	}
	fmt.Fprintf(&source, "class Main inherits %s { main() : Object { identify() }; };\n", parent)
	analysis := analyzeClassSource(t, source.String())
	if codes := semanticErrorCodes(analysis.Diagnostics); len(codes) != 0 {
		t.Fatalf("unexpected semantic diagnostics: %v", codes)
	}
	if distance, ok := analysis.Table.Distance("Main", "Root"); !ok || distance != depth+1 {
		t.Fatalf("Main to Root distance = %d, %v; want %d, true", distance, ok, depth+1)
	}
	if method, ok := analysis.Table.LookupMethod("Main", "identify"); !ok || method.Owner != "Root" {
		t.Fatalf("deep method lookup = (%+v, %v)", method, ok)
	}
	if attribute, ok := analysis.Table.LookupAttribute("Main", "value"); !ok || attribute.Owner != "Root" {
		t.Fatalf("deep attribute lookup = (%+v, %v)", attribute, ok)
	}
}

func TestClassAndFeatureOrderIsExplicit(t *testing.T) {
	analysis := analyzeClassSource(t, `
class Ordered {
  z : Int;
  zeta() : Object { 0 };
  a : String;
  alpha() : Object { 0 };
};
class Main { main() : Object { 0 }; };
`)
	assertNoErrors(t, analysis.Diagnostics)
	wantClasses := []string{"Object", "IO", "Int", "String", "Bool", "Ordered", "Main"}
	if !reflect.DeepEqual(analysis.Table.Order, wantClasses) {
		t.Fatalf("class order = %v, want %v", analysis.Table.Order, wantClasses)
	}
	ordered := analysis.Table.Classes["Ordered"]
	if want := []string{"z", "a"}; !reflect.DeepEqual(ordered.AttributeOrder, want) {
		t.Fatalf("attribute order = %v, want %v", ordered.AttributeOrder, want)
	}
	if want := []string{"zeta", "alpha"}; !reflect.DeepEqual(ordered.MethodOrder, want) {
		t.Fatalf("method order = %v, want %v", ordered.MethodOrder, want)
	}
}

func TestAttributeDefaultsAndInitializationOrder(t *testing.T) {
	orderSource := `
class Parent inherits IO {
  p1 : Object <- out_string("p1");
  p2 : Object <- out_string("p2");
};
class Child inherits Parent {
  c1 : Object <- out_string("c1");
  c2 : Object <- out_string("c2");
};
class Main inherits Child { main() : Object { 0 }; };
`
	result := Compile(orderSource)
	assertNoErrors(t, result.Diagnostics)
	if got := outputText(result.Runtime.Output); got != "p1p2c1c2" {
		t.Fatalf("initializer output = %q, want parent/source order", got)
	}

	defaultsSource := `
class Main inherits IO {
  number : Int;
  text : String;
  flag : Bool;
  reference : Object;
  main() : Object {{
    out_int(number);
    out_string(text);
    if flag then out_string("true") else out_string("false") fi;
    if isvoid reference then out_string("void") else out_string("object") fi;
  }};
};
`
	result = Compile(defaultsSource)
	assertNoErrors(t, result.Diagnostics)
	if got := outputText(result.Runtime.Output); got != "0falsevoid" {
		t.Fatalf("default attribute output = %q", got)
	}
}

func TestSemanticClassErrorsBlockIR(t *testing.T) {
	result := Compile(`class Main inherits Missing { main() : Object { 0 }; };`)
	if !containsDiagnosticCode(result.Diagnostics, "COL3011") {
		t.Fatalf("missing inheritance diagnostic: %+v", result.Diagnostics)
	}
	if len(result.IR) != 0 || len(result.Assembly) != 0 || !result.Runtime.Halted {
		t.Fatalf("semantic barrier leaked backend state: IR=%d assembly=%d runtime=%+v", len(result.IR), len(result.Assembly), result.Runtime)
	}
}

func analyzeClassSource(t testing.TB, source string) Analysis {
	t.Helper()
	tokens, lexical := Lex(source)
	if hasErrors(lexical) {
		t.Fatalf("unexpected lexer diagnostics: %+v", lexical)
	}
	program, syntactic := Parse(tokens)
	if hasErrors(syntactic) {
		t.Fatalf("unexpected parser diagnostics: %+v", syntactic)
	}
	return Analyze(program)
}

func semanticErrorCodes(diagnostics []Diagnostic) []string {
	codes := []string{}
	for _, diagnostic := range diagnostics {
		if diagnostic.Stage == "semantic" && diagnostic.Severity == SeverityError {
			codes = append(codes, diagnostic.Code)
		}
	}
	return codes
}

func assertSemanticCodeCounts(t testing.TB, diagnostics []Diagnostic, want map[string]int) {
	t.Helper()
	got := map[string]int{}
	for _, diagnostic := range diagnostics {
		if diagnostic.Stage == "semantic" && diagnostic.Severity == SeverityError {
			got[diagnostic.Code]++
		}
	}
	for code, count := range want {
		if got[code] != count {
			t.Fatalf("diagnostic %s count = %d, want %d; all = %+v", code, got[code], count, diagnostics)
		}
	}
}

func semanticDiagnostic(t testing.TB, diagnostics []Diagnostic, code string, occurrence int) Diagnostic {
	t.Helper()
	for _, diagnostic := range diagnostics {
		if diagnostic.Stage == "semantic" && diagnostic.Code == code {
			if occurrence == 0 {
				return diagnostic
			}
			occurrence--
		}
	}
	t.Fatalf("semantic diagnostic %s occurrence not found: %+v", code, diagnostics)
	return Diagnostic{}
}
