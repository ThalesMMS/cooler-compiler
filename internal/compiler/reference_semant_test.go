// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReferenceSemantAgreesWithAnalyzerOnExamples checks that every valid
// example passes the course-compatibility checker and that its annotated
// types equal Cooler's analyzer types on the same AST.
func TestReferenceSemantAgreesWithAnalyzerOnExamples(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("..", "..", "examples", "*.cl"))
	for _, file := range files {
		if strings.Contains(file, "error") || strings.Contains(file, "stack_machine") {
			continue
		}
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		units := []SourceUnit{{ID: "u", Name: filepath.Base(file), Content: string(content)}}
		parsed := parseReference(units)
		checker := &refSemant{parsed: parsed, classes: map[string]*refClass{}, types: map[Expr]string{}}
		checker.run()
		if len(checker.errors) > 0 {
			t.Fatalf("%s: unexpected errors %+v", file, checker.errors)
		}
		analysis := Analyze(parsed.program)
		if hasErrors(analysis.Diagnostics) {
			t.Fatalf("%s: analyzer errors", file)
		}
		for expr, refType := range checker.types {
			coolerType := expr.GetType()
			if coolerType == typeNo {
				coolerType = refNoType
			}
			if coolerType != refType {
				t.Fatalf("%s: %T at line %d: reference type %s, analyzer type %s", file, expr, checker.lineOf(expr), refType, coolerType)
			}
		}
	}
}

func TestReferenceTypedDump(t *testing.T) {
	source := `class A inherits IO {
  me() : SELF_TYPE { self };
  pick(b : Bool) : Object { if b then new A else new B fi };
  sum() : Int { let x : Int <- 1, y : Int in x + y };
  kind(o : Object) : String {
    case o of i : Int => "int"; s : String => s.concat("!"); esac
  };
  len() : Int { "abc".length() + type_name().length() };
};
class B inherits A {};
class Main {
  main() : Object { (new A).me().out_string("x") };
};
`
	got, ok := ReferenceSemantDump([]SourceUnit{{ID: "u", Name: "typed.cl", Content: source}})
	if !ok {
		t.Fatalf("semant failed:\n%s", got)
	}
	for _, fragment := range []string{
		// SELF_TYPE: self and a SELF_TYPE-returning method.
		"_object\nself\n: SELF_TYPE\n#3",
		// if: least upper bound of A and B.
		"_new\nB\n: B\n: A",
		// let: nested lets typed by the body, the missing init is _no_type.
		"_let\ny\nInt\n#4\n_no_expr\n: _no_type",
		"_plus\n#4\n_object\nx\n: Int\n#4\n_object\ny\n: Int\n: Int\n: Int\n: Int",
		// case: branches joined to String; basic-class methods typed.
		"concat\n(\n#6\n_string\n\"!\"\n: String\n)\n: String",
		": String\n: String\n#8",
		"length\n(\n)\n: Int",
		// implicit self in a dispatch and SELF_TYPE results.
		"_dispatch\n#8\n_object\nself\n: SELF_TYPE\ntype_name",
		"out_string\n(\n#12\n_string\n\"x\"\n: String\n)\n: A",
	} {
		if !contains(trimLines(got), fragment) {
			t.Fatalf("typed dump lacks %q:\n%s", fragment, got)
		}
	}
}

func TestReferenceSemantErrors(t *testing.T) {
	cases := []struct {
		name, source string
		want         []string
	}{
		{"graph errors halt before expressions", `class A inherits Int { f() : Int { undefined } ; };
class B inherits C {};
class Object {};
class B {};
class Main { main() : Object { 0 }; };`, []string{
			"s.cl:3: Redefinition of basic class Object.",
			"s.cl:4: Class B was previously defined.",
			"s.cl:1: Class A cannot inherit class Int.",
			"s.cl:2: Class B inherits from an undefined class C.",
		}},
		{"cycle", "class A inherits B {};\nclass B inherits A {};\nclass Main { main() : Object { 0 }; };", []string{
			"s.cl:1: Class A, an ancestor of A, is involved in an inheritance cycle.",
			"s.cl:2: Class B, an ancestor of B, is involved in an inheritance cycle.",
		}},
		{"missing Main", "class A {};", []string{"Class Main is not defined."}},
		{"features", `class P { x : Int; f(a : Int) : Int { a }; g() : Object { 0 }; };
class Main inherits P {
  x : Bool;
  self : Int;
  y : Missing;
  y : Int;
  f(a : String) : Int { 0 };
  g(b : Int) : Object { 0 };
  h(self : Int, c : SELF_TYPE, c : Nope) : Nada { 0 };
  h() : Int { 0 };
};`, []string{
			"s.cl:3: Attribute x is an attribute of an inherited class.",
			"s.cl:4: 'self' cannot be the name of an attribute.",
			"s.cl:5: Class Missing of attribute y is undefined.",
			"s.cl:6: Attribute y is multiply defined in class.",
			"s.cl:7: In redefined method f, parameter type String is different from original type Int",
			"s.cl:8: Incompatible number of formal parameters in redefined method g.",
			"s.cl:9: 'self' cannot be the name of a formal parameter.",
			"s.cl:9: Formal parameter c cannot have type SELF_TYPE.",
			"s.cl:9: Formal parameter c is multiply defined.",
			"s.cl:9: Class Nope of formal parameter c is undefined.",
			"s.cl:9: Undefined return type Nada in method h.",
			"s.cl:10: Method h is multiply defined.",
			"s.cl:2: No 'main' method in class Main.",
		}},
		{"expressions", `class Main inherits IO {
  a : Int <- "s";
  main() : Object { {
    x;
    self <- 1;
    z <- 2;
    a <- true;
    out_string(1);
    out_int(1, 2);
    nope(undefinedArg);
    if 1 then 2 else 3 fi;
    while 0 loop 1 pool;
    let self : Int in 0;
    let q : Missing <- 1 in q;
    let r : Int <- "s" in r;
    case 1 of self : Int => 1; t : SELF_TYPE => 2; u : Ghost => 3; v : Int => 4; esac;
    new Ghost;
    1 + "s";
    ~true;
    not 1;
    1 < "s";
    1 = "s";
    (new Main)@String.length();
  } };
  f() : String { 1 };
};`, []string{
			"s.cl:2: Inferred type String of initialization of attribute a does not conform to declared type Int.",
			"s.cl:4: Undeclared identifier x.",
			"s.cl:5: Cannot assign to 'self'.",
			"s.cl:6: Assignment to undeclared variable z.",
			"s.cl:7: Type Bool of assigned expression does not conform to declared type Int of identifier a.",
			"s.cl:8: In call of method out_string, type Int of parameter arg does not conform to declared type String.",
			"s.cl:9: Method out_int called with wrong number of arguments.",
			"s.cl:10: Undeclared identifier undefinedArg.",
			"s.cl:10: Dispatch to undefined method nope.",
			"s.cl:11: Predicate of 'if' does not have type Bool.",
			"s.cl:12: Loop condition does not have type Bool.",
			"s.cl:13: 'self' cannot be bound in a 'let' expression.",
			"s.cl:14: Class Missing of let-bound identifier q is undefined.",
			"s.cl:15: Inferred type String of initialization of r does not conform to identifier's declared type Int.",
			"s.cl:16: 'self' bound in 'case'.",
			"s.cl:16: Identifier t declared with type SELF_TYPE in case branch.",
			"s.cl:16: Class Ghost of case branch is undefined.",
			"s.cl:16: Duplicate branch Int in case statement.",
			"s.cl:17: 'new' used with undefined class Ghost.",
			"s.cl:18: non-Int arguments: Int + String",
			"s.cl:19: Argument of '~' has type Bool instead of Int.",
			"s.cl:20: Argument of 'not' has type Int instead of Bool.",
			"s.cl:21: non-Int arguments: Int < String",
			"s.cl:22: Illegal comparison with a basic type.",
			"s.cl:23: Expression type Main does not conform to declared static dispatch type String.",
			"s.cl:25: Inferred return type Int of method f does not conform to declared return type String.",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := ReferenceSemant([]SourceUnit{{ID: "u", Name: "s.cl", Content: tc.source}})
			if result.Stage != "semant" {
				t.Fatalf("stage %q report:\n%s%s", result.Stage, result.Report, result.Dump)
			}
			if strings.Join(result.Errors, "\n") != strings.Join(tc.want, "\n") {
				t.Fatalf("errors:\n%s\nwant:\n%s", strings.Join(result.Errors, "\n"), strings.Join(tc.want, "\n"))
			}
			if !strings.HasSuffix(result.Report, "Compilation halted due to static semantic errors.\n") {
				t.Fatalf("missing halt line:\n%s", result.Report)
			}
		})
	}
}

func TestReferenceSemantStopsAtParseErrors(t *testing.T) {
	result := ReferenceSemant([]SourceUnit{{ID: "u", Name: "p.cl", Content: "class Main { x };"}})
	if result.Stage != "parser" || !strings.HasSuffix(result.Report, "Compilation halted due to lex and parse errors\n") {
		t.Fatalf("%+v", result)
	}
}
