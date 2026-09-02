// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"reflect"
	"strings"
	"testing"
)

func TestConformanceAndLUBWithSelfType(t *testing.T) {
	analysis := analyzeClassSource(t, `
class Parent {};
class Child inherits Parent {};
class Sibling inherits Parent {};
class Other {};
class Main { main() : Object { 0 }; };
`)
	a := &analyzer{table: analysis.Table, currentClass: "Child"}
	conformance := []struct {
		actual   string
		expected string
		want     bool
	}{
		{typeSelf, typeSelf, true},
		{typeSelf, "Child", true},
		{typeSelf, "Parent", true},
		{"Child", typeSelf, false},
		{"Child", "Parent", true},
		{"Parent", "Child", false},
		{"Child", "Other", false},
		{typeNo, "Other", true},
		{typeInvalid, "Other", true},
	}
	for _, test := range conformance {
		if got := a.conforms(test.actual, test.expected); got != test.want {
			t.Errorf("conforms(%s, %s) = %v, want %v", test.actual, test.expected, got, test.want)
		}
	}

	joins := []struct {
		left  string
		right string
		want  string
	}{
		{typeSelf, typeSelf, typeSelf},
		{typeSelf, "Child", "Child"},
		{typeSelf, "Parent", "Parent"},
		{"Child", "Sibling", "Parent"},
		{"Child", "Other", typeObject},
		{typeNo, "Child", "Child"},
		{typeInvalid, "Child", typeInvalid},
	}
	for _, test := range joins {
		if got := a.join(test.left, test.right); got != test.want {
			t.Errorf("join(%s, %s) = %s, want %s", test.left, test.right, got, test.want)
		}
	}
}

func TestSequentialLetAndCaseShadowing(t *testing.T) {
	result := Compile(`
class Main inherits IO {
  value : Int <- 100;
  resolve(value : Int) : Int {
    let value : Int <- value + 1, value : Int <- value + 1 in
      case value of value : Int => value; esac
  };
  main() : Object { out_int(resolve(1)) };
};
`)
	assertNoErrors(t, result.Diagnostics)
	if got := outputText(result.Runtime.Output); got != "3" {
		t.Fatalf("shadowing output = %q, want 3", got)
	}
}

func TestDispatchTypingAcrossInheritanceAndSelfType(t *testing.T) {
	program, analysis := analyzeTypedProgram(t, `
class Root {
  same() : SELF_TYPE { self };
};
class Child inherits Root {
  child() : Int { 1 };
  implicit_chain() : Int { same().child() };
};
class Main {
  dynamic() : Child { (new Child).same() };
  static() : Root { (new Child)@Root.same() };
  declared() : Root { let item : Root <- new Child in item.same() };
  main() : Object { (new Child).same().child() };
};
`)
	assertNoErrors(t, analysis.Diagnostics)
	want := map[string]string{
		"Child.implicit_chain": typeInt,
		"Main.dynamic":         "Child",
		"Main.static":          "Root",
		"Main.declared":        "Root",
		"Main.main":            typeInt,
	}
	for key, expected := range want {
		className, methodName, _ := strings.Cut(key, ".")
		method := typedMethod(t, program, className, methodName)
		if got := method.Body.GetType(); got != expected {
			t.Errorf("%s body type = %s, want %s", key, got, expected)
		}
	}
}

func TestExpressionDiagnosticFamilies(t *testing.T) {
	source := `
class Parent { method(x : Int) : SELF_TYPE { self }; };
class Other {};
class Main {
  assignment() : Object {{ self <- 1; missing <- 1; let x : Int in x <- "bad"; }};
  control() : Object {{ if 1 then 0 else 1 fi; while "bad" loop 0 pool; }};
  lets() : Object {
    let self : Int <- 0, missing_type : Missing <- 0, mismatch : Int <- "bad" in 0
  };
  cases() : Object {
    case 0 of
      self : Int => 0;
      x : SELF_TYPE => 0;
      y : Missing => 0;
      z : Int => 0;
    esac
  };
  dispatches() : Object {{
    self@SELF_TYPE.method(1);
    self@Missing.method(1);
    (new Other)@Parent.method(1);
    (new Parent).missing();
    (new Parent).method();
    (new Parent).method("bad");
  }};
  allocation() : Object { new Missing };
  main() : Object { 0 };
};
`
	analysis := analyzeClassSource(t, source)
	want := []string{
		"COL3101", "COL3102", "COL3103",
		"COL3110", "COL3111",
		"COL3120", "COL3121", "COL3122",
		"COL3130", "COL3131", "COL3132", "COL3133",
		"COL3170", "COL3171", "COL3172", "COL3173", "COL3174", "COL3175",
		"COL3140",
	}
	if got := semanticErrorCodes(analysis.Diagnostics); !reflect.DeepEqual(got, want) {
		t.Fatalf("semantic codes = %v, want %v\ndiagnostics: %+v", got, want, analysis.Diagnostics)
	}
	duplicateBranch := semanticDiagnostic(t, analysis.Diagnostics, "COL3133", 0)
	if len(duplicateBranch.Related) != 1 || duplicateBranch.Related[0].Label != "first branch" {
		t.Fatalf("duplicate branch related span = %+v", duplicateBranch.Related)
	}
}

func TestInvalidChildrenUseErrorTypeWithoutCascades(t *testing.T) {
	program, analysis := analyzeTypedProgram(t, `
class Main {
  main() : Object {{
    missing + 1;
    (new Missing).call(missing_argument);
    if missing_condition then missing_left + 1 else missing_receiver.call() fi;
    let item : Missing <- missing_initializer in item.call();
    case missing_case of branch : Missing => branch + 1; esac;
    not 1;
  }};
};
`)
	want := []string{
		"COL3100",
		"COL3140", "COL3100",
		"COL3100", "COL3100", "COL3100",
		"COL3121", "COL3100",
		"COL3100", "COL3132",
		"COL3151",
	}
	if got := semanticErrorCodes(analysis.Diagnostics); !reflect.DeepEqual(got, want) {
		t.Fatalf("semantic codes = %v, want %v\ndiagnostics: %+v", got, want, analysis.Diagnostics)
	}
	for _, forbidden := range []string{"COL3061", "COL3110", "COL3122", "COL3160", "COL3173"} {
		if containsDiagnosticCode(analysis.Diagnostics, forbidden) {
			t.Fatalf("derived diagnostic %s was not suppressed: %+v", forbidden, analysis.Diagnostics)
		}
	}
	method := typedMethod(t, program, "Main", "main")
	assertInvalidNodeIsMarked(t, method.Body)
}

func TestDeepInvalidExpressionProducesOneRootDiagnostic(t *testing.T) {
	expression := "missing" + strings.Repeat(" + 1", 400)
	program, analysis := analyzeTypedProgram(t, "class Main { main() : Object { "+expression+" }; };")
	if got := semanticErrorCodes(analysis.Diagnostics); !reflect.DeepEqual(got, []string{"COL3100"}) {
		t.Fatalf("deep invalid expression diagnostics = %v", got)
	}
	if body := typedMethod(t, program, "Main", "main").Body; body.GetType() != typeInvalid {
		t.Fatalf("deep invalid expression type = %s", body.GetType())
	}
}

func TestDeepSequentialLetShadowing(t *testing.T) {
	const bindings = 2000
	var expression strings.Builder
	expression.WriteString("let x : Int <- 0")
	for i := 1; i < bindings; i++ {
		expression.WriteString(", x : Int <- x + 1")
	}
	expression.WriteString(" in x")
	program, analysis := analyzeTypedProgram(t, "class Main { main() : Object { "+expression.String()+" }; };")
	assertNoErrors(t, analysis.Diagnostics)
	if body := typedMethod(t, program, "Main", "main").Body; body.GetType() != typeInt {
		t.Fatalf("deep sequential let type = %s, want Int", body.GetType())
	}
}

func analyzeTypedProgram(t testing.TB, source string) (*Program, Analysis) {
	t.Helper()
	tokens, lexical := Lex(source)
	if hasErrors(lexical) {
		t.Fatalf("unexpected lexer diagnostics: %+v", lexical)
	}
	program, syntactic := Parse(tokens)
	if hasErrors(syntactic) {
		t.Fatalf("unexpected parser diagnostics: %+v", syntactic)
	}
	return program, Analyze(program)
}

func typedMethod(t testing.TB, program *Program, className, methodName string) *MethodDecl {
	t.Helper()
	for _, class := range program.Classes {
		if class.Name != className {
			continue
		}
		for _, feature := range class.Features {
			if method, ok := feature.(*MethodDecl); ok && method.Name == methodName {
				return method
			}
		}
	}
	t.Fatalf("method %s.%s not found", className, methodName)
	return nil
}

func assertInvalidNodeIsMarked(t testing.TB, expr Expr) {
	t.Helper()
	if expr.GetType() != typeInvalid {
		t.Fatalf("invalid node %T has static type %q", expr, expr.GetType())
	}
}
