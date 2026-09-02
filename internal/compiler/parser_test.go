// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"reflect"
	"strings"
	"testing"
)

func TestParserClassFeatureAndFormalGrammar(t *testing.T) {
	program, diagnostics := parseSource(t, `
class Empty {};
class Main inherits Empty {
  plain : Int;
  initialized : Int <- 1;
  zero() : Int { 0 };
  one(value : Int) : Int { value };
  many(a : Int, b : String, c : Bool) : Object { a };
  main() : Object { self };
};`)
	if len(diagnostics) != 0 || len(program.Classes) != 2 {
		t.Fatalf("classes=%d diagnostics=%+v", len(program.Classes), diagnostics)
	}
	main := program.Classes[1]
	if main.Parent != "Empty" || len(main.Features) != 6 {
		t.Fatalf("Main = %+v", main)
	}
	if attribute, ok := main.Features[0].(*AttributeDecl); !ok || attribute.Init != nil {
		t.Fatalf("plain attribute = %#v", main.Features[0])
	}
	if attribute, ok := main.Features[1].(*AttributeDecl); !ok || attribute.Init == nil {
		t.Fatalf("initialized attribute = %#v", main.Features[1])
	}
	wantFormals := []int{0, 1, 3, 0}
	for index, want := range wantFormals {
		method, ok := main.Features[index+2].(*MethodDecl)
		if !ok || len(method.Formals) != want {
			t.Fatalf("method[%d] = %#v, want %d formals", index, main.Features[index+2], want)
		}
	}
	assertASTSpans(t, program.ToAST(), program.Span)
}

func TestParserAssignmentAssociativityAndPrecedenceShape(t *testing.T) {
	program, diagnostics := parseSource(t, `class Main {
  main(a : Int, b : Int, c : Int, obj : Object) : Object {
    a <- b <- not a < b + c * isvoid ~obj@Object.copy().type_name()
  };
};`)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	body := program.Classes[0].Features[0].(*MethodDecl).Body
	outer, ok := body.(*AssignExpr)
	if !ok || outer.Name != "a" {
		t.Fatalf("outer assignment = %#v", body)
	}
	inner, ok := outer.Value.(*AssignExpr)
	if !ok || inner.Name != "b" {
		t.Fatalf("right-associative assignment = %#v", outer.Value)
	}
	notExpr, ok := inner.Value.(*UnaryExpr)
	if !ok || notExpr.Operator != TokenNot {
		t.Fatalf("not expression = %#v", inner.Value)
	}
	comparison, ok := notExpr.Value.(*BinaryExpr)
	if !ok || comparison.Operator != TokenLess {
		t.Fatalf("comparison = %#v", notExpr.Value)
	}
	addition, ok := comparison.Right.(*BinaryExpr)
	if !ok || addition.Operator != TokenPlus {
		t.Fatalf("addition = %#v", comparison.Right)
	}
	multiplication, ok := addition.Right.(*BinaryExpr)
	if !ok || multiplication.Operator != TokenStar {
		t.Fatalf("multiplication = %#v", addition.Right)
	}
	isvoid, ok := multiplication.Right.(*IsVoidExpr)
	if !ok {
		t.Fatalf("isvoid = %#v", multiplication.Right)
	}
	negation, ok := isvoid.Value.(*UnaryExpr)
	if !ok || negation.Operator != TokenTilde {
		t.Fatalf("negation = %#v", isvoid.Value)
	}
	outerDispatch, ok := negation.Value.(*DispatchExpr)
	if !ok || outerDispatch.Method != "type_name" {
		t.Fatalf("outer dispatch = %#v", negation.Value)
	}
	staticDispatch, ok := outerDispatch.Receiver.(*DispatchExpr)
	if !ok || staticDispatch.StaticTypeName != "Object" || staticDispatch.Method != "copy" {
		t.Fatalf("static dispatch = %#v", outerDispatch.Receiver)
	}
}

func TestParserParenthesesOverridePrecedenceAndNestedCalls(t *testing.T) {
	program, diagnostics := parseSource(t, `class Main {
  f(x : Int, y : Int) : Int { x };
  main(a : Int, b : Int, c : Int) : Object { f((a + b) * c, f(1, 2)) };
};`)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	body := program.Classes[0].Features[1].(*MethodDecl).Body.(*DispatchExpr)
	product := body.Arguments[0].(*BinaryExpr)
	if product.Operator != TokenStar || product.Left.(*BinaryExpr).Operator != TokenPlus {
		t.Fatalf("parenthesized expression = %#v", product)
	}
	if nested, ok := body.Arguments[1].(*DispatchExpr); !ok || nested.Receiver != nil || nested.Method != "f" {
		t.Fatalf("nested implicit call = %#v", body.Arguments[1])
	}
}

func TestParserRejectsEveryChainedComparison(t *testing.T) {
	for _, source := range []string{"1 < 2 < 3", "1 <= 2 = 3", "1 = 2 <= 3"} {
		t.Run(source, func(t *testing.T) {
			_, diagnostics := parseSource(t, "class Main { main() : Object { "+source+" }; };")
			assertParserCodes(t, diagnostics, "COL2031")
		})
	}
	_, diagnostics := parseSource(t, "class Main { main() : Object { (1 < 2) = (2 < 3) }; };")
	if len(diagnostics) != 0 {
		t.Fatalf("parenthesized comparisons rejected: %+v", diagnostics)
	}
}

func TestParserRecoversAtNextClassBoundary(t *testing.T) {
	program, diagnostics := parseSource(t, `class Broken { value : Int;
class Main { main() : Object { 0 }; };`)
	assertParserCodes(t, diagnostics, "COL2006")
	if len(program.Classes) != 2 || program.Classes[0].Name != "Broken" || program.Classes[1].Name != "Main" {
		t.Fatalf("recovered classes = %+v", program.Classes)
	}
}

func TestParserDepthAndDiagnosticLimits(t *testing.T) {
	deep := "class Main { main() : Object { " + strings.Repeat("(", 2_000) + "0" + strings.Repeat(")", 2_000) + " }; };"
	_, diagnostics := parseSource(t, deep)
	if len(diagnostics) != maxParserDiagnostics {
		t.Fatalf("deep diagnostics count=%d, want %d", len(diagnostics), maxParserDiagnostics)
	}
	if diagnostics[0].Code != "COL2110" || diagnostics[len(diagnostics)-1].Code != "COL2111" {
		t.Fatalf("deep diagnostics first=%s last=%s", diagnostics[0].Code, diagnostics[len(diagnostics)-1].Code)
	}

	malformed := "class Main {" + strings.Repeat(" : ;", 10_000) + "};"
	_, diagnostics = parseSource(t, malformed)
	if len(diagnostics) > maxParserDiagnostics {
		t.Fatalf("diagnostic limit exceeded: %d", len(diagnostics))
	}

	for name, expression := range map[string]string{
		"assignment": strings.Repeat("x <- ", 2_000) + "0",
		"not":        strings.Repeat("not ", 2_000) + "true",
		"isvoid":     strings.Repeat("isvoid ", 2_000) + "0",
		"negation":   strings.Repeat("~", 2_000) + "0",
	} {
		t.Run(name, func(t *testing.T) {
			_, nestedDiagnostics := parseSource(t, "class Main { main() : Object { "+expression+" }; };")
			if !containsDiagnosticCode(nestedDiagnostics, "COL2110") {
				t.Fatalf("recursive expression bypassed depth guard: %+v", nestedDiagnostics)
			}
		})
	}
}

func TestParserDiagnosticsAndPartialASTAreDeterministic(t *testing.T) {
	source := `class Good {};
class Main { broken : ; main() : Object { if true 0 else fi }; };`
	firstProgram, first := parseSource(t, source)
	secondProgram, second := parseSource(t, source)
	if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(firstProgram.ToAST(), secondProgram.ToAST()) {
		t.Fatal("parser recovery changed between identical runs")
	}
	if firstProgram.ToAST() == nil || len(first) == 0 {
		t.Fatal("missing partial AST or diagnostics")
	}
	session := NewCompilationSession(source)
	session.NextPhase()
	parsed := session.NextPhase()
	if !parsed.Blocked || parsed.Result.AST == nil || len(parsed.Result.IR) != 0 || len(parsed.Result.Assembly) != 0 {
		t.Fatalf("parser barrier leaked backend state: %+v", parsed)
	}
}

func TestParserMalformedSyntaxCoverage(t *testing.T) {
	tests := []struct {
		name   string
		source string
		code   string
	}{
		{"missing class semicolon", `class Main {}`, "COL2007"},
		{"missing class brace", `class Main { main() : Object { 0 };`, "COL2006"},
		{"invalid feature start", `class Main { Int; };`, "COL2010"},
		{"missing attribute colon", `class Main { value Int; };`, "COL2020"},
		{"missing feature semicolon", `class Main { value : Int };`, "COL2022"},
		{"malformed formal", `class Main { main(value Int) : Object { 0 }; };`, "COL2012"},
		{"missing formal comma", `class Main { main(a : Int b : Int) : Object { 0 }; };`, "COL2014"},
		{"missing method brace", `class Main { main() : Object 0 }; };`, "COL2017"},
		{"missing argument close", `class Main { main() : Object { f(1 }; };`, "COL2045"},
		{"incomplete dynamic dispatch", `class Main { main() : Object { self.missing }; };`, "COL2044"},
		{"incomplete static dispatch", `class Main { main() : Object { self@.missing() }; };`, "COL2040"},
		{"missing then", `class Main { main() : Object { if true 1 else 0 fi }; };`, "COL2060"},
		{"missing fi", `class Main { main() : Object { if true then 1 else 0 }; };`, "COL2062"},
		{"missing pool", `class Main { main() : Object { while false loop 0 }; };`, "COL2071"},
		{"empty block", `class Main { main() : Object { {} }; };`, "COL2082"},
		{"missing block semicolon", `class Main { main() : Object { { 1 2; } }; };`, "COL2080"},
		{"malformed let name", `class Main { main() : Object { let : Int in 0 }; };`, "COL2090"},
		{"missing let colon", `class Main { main() : Object { let x Int in x }; };`, "COL2091"},
		{"missing case of", `class Main { main() : Object { case self x : Object => x; esac }; };`, "COL2100"},
		{"missing case arrow", `class Main { main() : Object { case self of x : Object x; esac }; };`, "COL2104"},
		{"empty case", `class Main { main() : Object { case self of esac }; };`, "COL2107"},
		{"missing esac", `class Main { main() : Object { case self of x : Object => x; }; };`, "COL2106"},
		{"class eof", `class`, "COL2003"},
		{"inheritance eof", `class Main inherits`, "COL2004"},
		{"new eof", `class Main { main() : Object { new`, "COL2050"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, diagnostics := parseSource(t, test.source)
			if !containsDiagnosticCode(diagnostics, test.code) {
				t.Fatalf("missing %s in diagnostics: %+v", test.code, diagnostics)
			}
			if len(diagnostics) > maxParserDiagnostics {
				t.Fatalf("diagnostic limit exceeded: %d", len(diagnostics))
			}
		})
	}
}

func TestParserMarksOnlySyntheticExpressionsAsNoExpr(t *testing.T) {
	program, diagnostics := parseSource(t, `class Main { main() : Object { new ; }; };`)
	if !containsDiagnosticCode(diagnostics, "COL2050") {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	body := program.Classes[0].Features[0].(*MethodDecl).Body
	if _, ok := body.(*NoExpr); !ok || body.ToAST().Kind != "NoExpr" {
		t.Fatalf("synthetic body = %#v / %+v", body, body.ToAST())
	}

	program, diagnostics = parseSource(t, `class Main { main() : Object { if true then 1 else 0 }; };`)
	if !containsDiagnosticCode(diagnostics, "COL2062") {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	if _, ok := program.Classes[0].Features[0].(*MethodDecl).Body.(*IfExpr); !ok {
		t.Fatal("real recovered if components were replaced by a synthetic node")
	}
}

func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		"", "class Main {};", "class A { class B {};", "class Main { main() : Object { (0) }; };",
		"class Main { main(x Int) Object { let : in case of esac }; };",
		"ClAss A{a(a A A", strings.Repeat("(", 700), strings.Repeat("class X {};", 50),
	} {
		f.Add(seed)
	}
	addCompilerFuzzSourceSeeds(f, func(source string) { f.Add(source) })
	f.Fuzz(func(t *testing.T, source string) {
		tokens, _ := Lex(source)
		program, diagnostics := Parse(tokens)
		if len(diagnostics) > maxParserDiagnostics {
			t.Fatalf("diagnostic count = %d", len(diagnostics))
		}
		if program != nil && program.ToAST() != nil {
			assertASTSpans(t, program.ToAST(), program.Span)
		}
		againProgram, againDiagnostics := Parse(tokens)
		if !reflect.DeepEqual(diagnostics, againDiagnostics) || !reflect.DeepEqual(program, againProgram) {
			t.Fatal("parser output is nondeterministic")
		}
	})
}

func parseSource(t testing.TB, source string) (*Program, []Diagnostic) {
	t.Helper()
	tokens, lexerDiagnostics := Lex(source)
	if len(lexerDiagnostics) != 0 {
		t.Fatalf("unexpected lexer diagnostics: %+v", lexerDiagnostics)
	}
	return Parse(tokens)
}

func assertParserCodes(t testing.TB, diagnostics []Diagnostic, want ...string) {
	t.Helper()
	got := make([]string, len(diagnostics))
	for index, diagnostic := range diagnostics {
		got[index] = diagnostic.Code
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parser codes=%v, want=%v diagnostics=%+v", got, want, diagnostics)
	}
}

func containsDiagnosticCode(diagnostics []Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func assertASTSpans(t testing.TB, node *ASTNode, root Span) {
	t.Helper()
	if node == nil {
		return
	}
	span := node.Span
	if span.StartLine < 1 || span.StartColumn < 1 || span.EndLine < span.StartLine || span.StartOffset < root.StartOffset || span.EndOffset < span.StartOffset || span.EndOffset > root.EndOffset {
		t.Fatalf("AST node %s has invalid span %+v inside %+v", node.Kind, span, root)
	}
	for index := range node.Children {
		assertASTSpans(t, &node.Children[index], root)
	}
}
