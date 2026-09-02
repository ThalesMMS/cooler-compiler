// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestGoldenTokensBasicProgram(t *testing.T) {
	source := readTestdata(t, "valid", "basic.cl")
	tokens, diagnostics := Lex(source)
	if hasErrors(diagnostics) {
		t.Fatalf("unexpected lexer diagnostics: %+v", diagnostics)
	}
	assertGolden(t, filepath.Join("golden", "basic.tokens"), formatGoldenTokens(tokens))
}

func TestGoldenTokensCommentsEscapesAndUTF16(t *testing.T) {
	source := readTestdata(t, "valid", "lexer_comments_escapes.cl")
	tokens, diagnostics := Lex(source)
	if hasErrors(diagnostics) {
		t.Fatalf("unexpected lexer diagnostics: %+v", diagnostics)
	}
	assertGolden(t, filepath.Join("golden", "lexer_comments_escapes.tokens"), formatGoldenTokens(tokens))

	var classToken *Token
	for index := range tokens {
		if tokens[index].Type == TokenClass {
			classToken = &tokens[index]
			break
		}
	}
	if classToken == nil {
		t.Fatal("missing class token")
	}
	wantOffset := utf16OffsetAt(source, classToken.Line, classToken.Column)
	if classToken.Span.StartOffset != wantOffset {
		t.Fatalf("class UTF-16 offset = %d, want %d", classToken.Span.StartOffset, wantOffset)
	}
	encoded, err := json.Marshal(classToken)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), fmt.Sprintf(`"startOffset":%d`, wantOffset)) {
		t.Fatalf("serialized token omitted UTF-16 start offset: %s", encoded)
	}
}

func TestGoldenTokensLexerConformance(t *testing.T) {
	source := readTestdata(t, "valid", "lexer_conformance.cl")
	tokens, diagnostics := Lex(source)
	if hasErrors(diagnostics) {
		t.Fatalf("unexpected lexer diagnostics: %+v", diagnostics)
	}
	assertGolden(t, filepath.Join("golden", "lexer_conformance.tokens"), formatGoldenTokens(tokens))
}

func TestGoldenASTAndIRBasicProgram(t *testing.T) {
	source := readTestdata(t, "valid", "basic.cl")
	result := Compile(source)
	if hasErrors(result.Diagnostics) {
		t.Fatalf("unexpected diagnostics: %+v", result.Diagnostics)
	}
	assertGolden(t, filepath.Join("golden", "basic.ast"), formatGoldenAST(result.AST))
	assertGolden(t, filepath.Join("golden", "basic.ir"), formatGoldenIR(result.IR))
}

func TestGoldenASTParserGrammar(t *testing.T) {
	program, diagnostics := parseSource(t, readTestdata(t, "valid", "parser_grammar.cl"))
	if len(diagnostics) != 0 {
		t.Fatalf("unexpected parser diagnostics: %+v", diagnostics)
	}
	assertGolden(t, filepath.Join("golden", "parser_grammar.ast"), formatGoldenAST(program.ToAST()))
}

func TestGoldenASTParserPrecedence(t *testing.T) {
	program, diagnostics := parseSource(t, readTestdata(t, "parser_precedence.cl"))
	if len(diagnostics) != 0 {
		t.Fatalf("unexpected parser diagnostics: %+v", diagnostics)
	}
	assertGolden(t, filepath.Join("golden", "parser_precedence.ast"), formatGoldenAST(program.ToAST()))
}

func TestGoldenTypedSemanticAST(t *testing.T) {
	source := readTestdata(t, "valid", "semantic_expressions.cl")
	result := Compile(source)
	if hasErrors(result.Diagnostics) {
		t.Fatalf("unexpected diagnostics: %+v", result.Diagnostics)
	}
	assertGolden(t, filepath.Join("golden", "semantic_expressions.ast"), formatGoldenAST(result.AST))
}

func TestGoldenSemanticErrorTypes(t *testing.T) {
	source := readTestdata(t, "invalid", "semantic_cascade.cl")
	result := Compile(source)
	if !hasErrors(result.Diagnostics) {
		t.Fatal("expected semantic errors")
	}
	assertGolden(t, filepath.Join("golden", "semantic_cascade.ast"), formatGoldenAST(result.AST))
	assertGolden(t, filepath.Join("golden", "semantic_cascade.diagnostics"), formatGoldenDiagnostics(result.Diagnostics))
}

func TestGoldenRuntimeConformanceOutput(t *testing.T) {
	tests := []struct {
		name  string
		path  []string
		input string
	}{
		{"dispatch", []string{"conformance", "runtime", "evaluation-order-valid", "main.cl"}, ""},
		{"copy", []string{"conformance", "runtime", "copy-valid", "main.cl"}, ""},
		{"operators", []string{"conformance", "runtime", "operators-equality-valid", "main.cl"}, ""},
		{"case", []string{"conformance", "runtime", "case-order-valid", "main.cl"}, ""},
		{"unicode", []string{"conformance", "builtins", "string-unicode-valid", "main.cl"}, ""},
		{"io", []string{"conformance", "builtins", "io-edge-valid", "main.cl"}, "not-an-int\n -42 \nhello\n"},
	}
	lines := []string{}
	for _, test := range tests {
		source := readTestdata(t, test.path...)
		result := CompileWithInput(source, test.input)
		if hasErrors(result.Diagnostics) {
			t.Fatalf("%s diagnostics: %+v", test.name, result.Diagnostics)
		}
		lines = append(lines, test.name+"|"+outputText(result.Runtime.Output))
	}
	assertGolden(t, filepath.Join("golden", "runtime_conformance.output"), strings.Join(lines, "\n"))
}

func TestGoldenRuntimeFailureDiagnostics(t *testing.T) {
	tests := []struct {
		name string
		path []string
	}{
		{"division", []string{"conformance", "runtime", "division-invalid", "main.cl"}},
		{"dispatch-void", []string{"conformance", "runtime", "void-dispatch-invalid", "main.cl"}},
		{"case-void", []string{"conformance", "runtime", "case-void-invalid", "main.cl"}},
		{"case-no-match", []string{"conformance", "runtime", "case-no-match-invalid", "main.cl"}},
		{"abort", []string{"conformance", "builtins", "abort-invalid", "main.cl"}},
		{"substr", []string{"conformance", "builtins", "string-substr-invalid", "main.cl"}},
	}
	lines := []string{}
	for _, test := range tests {
		result := Compile(readTestdata(t, test.path...))
		if len(result.Runtime.Diagnostics) != 1 {
			t.Fatalf("%s runtime diagnostics: %+v", test.name, result.Runtime.Diagnostics)
		}
		lines = append(lines, test.name+"|"+formatGoldenDiagnostics(result.Runtime.Diagnostics))
	}
	assertGolden(t, filepath.Join("golden", "runtime_failures.diagnostics"), strings.Join(lines, "\n"))
}

func TestValidFixturesCompileAndRun(t *testing.T) {
	expectedOutput := map[string]string{
		"basic.cl":                "3\n",
		"inheritance_dispatch.cl": "3\n",
	}
	entries, err := os.ReadDir(filepath.Join("testdata", "valid"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".cl") {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			result := Compile(readTestdata(t, "valid", entry.Name()))
			if hasErrors(result.Diagnostics) {
				t.Fatalf("unexpected diagnostics: %+v", result.Diagnostics)
			}
			if want, found := expectedOutput[entry.Name()]; found && strings.Join(result.Runtime.Output, "") != want {
				t.Fatalf("output = %q, want %q", strings.Join(result.Runtime.Output, ""), want)
			}
		})
	}
}

func TestInvalidFixturesReportErrors(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("testdata", "invalid"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".cl") {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			result := Compile(readTestdata(t, "invalid", entry.Name()))
			if !hasErrors(result.Diagnostics) {
				t.Fatalf("invalid fixture should produce an error: %+v", result.Diagnostics)
			}
		})
	}
}

func TestGoldenDiagnosticsByStage(t *testing.T) {
	tests := []struct {
		fixture string
		golden  string
	}{
		{"missing_fi.cl", "syntax.diagnostics"},
		{"semantic_error.cl", "semantic.diagnostics"},
		{"runtime_division_by_zero.cl", "runtime.diagnostics"},
	}
	for _, test := range tests {
		t.Run(test.fixture, func(t *testing.T) {
			result := Compile(readTestdata(t, "invalid", test.fixture))
			if !hasErrors(result.Diagnostics) {
				t.Fatalf("expected diagnostics: %+v", result.Diagnostics)
			}
			assertGolden(t, filepath.Join("golden", test.golden), formatGoldenDiagnostics(result.Diagnostics))
		})
	}
}

func TestDiagnosticOrderingAndSerializedSpansAreStable(t *testing.T) {
	source := readTestdata(t, "invalid", "semantic_error.cl")
	first, err := json.Marshal(Compile(source).Diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(Compile(source).Diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("diagnostic serialization is not deterministic\nfirst:  %s\nsecond: %s", first, second)
	}
	for _, diagnostic := range Compile(source).Diagnostics {
		want := utf16OffsetAt(source, diagnostic.Span.StartLine, diagnostic.Span.StartColumn)
		if diagnostic.Span.StartOffset != want {
			t.Fatalf("%s startOffset = %d, want UTF-16 offset %d", diagnostic.Code, diagnostic.Span.StartOffset, want)
		}
	}
}

func TestGoldenFixturesArePinnedToLF(t *testing.T) {
	attributes, err := os.ReadFile(filepath.Join("..", "..", ".gitattributes"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(attributes), "internal/compiler/testdata/** text eol=lf") {
		t.Fatalf(".gitattributes does not pin compiler fixtures to LF: %s", attributes)
	}
	for _, path := range []string{
		filepath.Join("testdata", "valid", "basic.cl"),
		filepath.Join("testdata", "golden", "basic.tokens"),
	} {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(contents), "\r") {
			t.Fatalf("fixture %s contains CR despite LF contract", path)
		}
	}
}

func readTestdata(t *testing.T, parts ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{"testdata"}, parts...)...)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertGolden(t *testing.T, name, actual string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	expected, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			t.Fatalf("golden %s is missing; actual output:\n%s", name, actual)
		}
		t.Fatal(err)
	}
	if normalizeGolden(actual) != normalizeGolden(string(expected)) {
		t.Fatalf("golden %s differs\n--- expected ---\n%s\n--- got ---\n%s", name, expected, actual)
	}
}

func normalizeGolden(value string) string {
	return strings.TrimSpace(strings.ReplaceAll(value, "\r\n", "\n"))
}

func formatGoldenTokens(tokens []Token) string {
	var lines []string
	for _, token := range tokens {
		if token.Type == TokenEOF {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s|%q|%q|%d:%d|%d-%d", token.Type, token.Lexeme, token.Literal, token.Line, token.Column, token.Span.StartOffset, token.Span.EndOffset))
	}
	return strings.Join(lines, "\n")
}

func formatGoldenIR(ir []IRInstruction) string {
	var lines []string
	for _, instruction := range ir {
		args := "-"
		if len(instruction.Args) > 0 {
			args = strings.Join(instruction.Args, ",")
		}
		lines = append(lines, fmt.Sprintf("%03d|%s|%s|%s", instruction.Index, instruction.Method, instruction.Op, args))
	}
	return strings.Join(lines, "\n")
}

func formatGoldenAST(root *ASTNode) string {
	var lines []string
	var walk func(*ASTNode, int)
	walk = func(node *ASTNode, depth int) {
		if node == nil {
			return
		}
		parts := []string{node.Kind}
		if node.Name != "" {
			parts = append(parts, "name="+node.Name)
		}
		if node.Type != "" {
			parts = append(parts, "type="+node.Type)
		}
		if node.Value != "" {
			parts = append(parts, fmt.Sprintf("value=%q", node.Value))
		}
		if node.Text != "" {
			parts = append(parts, fmt.Sprintf("text=%q", node.Text))
		}
		lines = append(lines, strings.Repeat("  ", depth)+strings.Join(parts, " "))
		for index := range node.Children {
			walk(&node.Children[index], depth+1)
		}
	}
	walk(root, 0)
	return strings.Join(lines, "\n")
}

func formatGoldenDiagnostics(diagnostics []Diagnostic) string {
	var lines []string
	for _, diagnostic := range diagnostics {
		lines = append(lines, fmt.Sprintf("%s|%s|%s|%d:%d|%d-%d|%s", diagnostic.Code, diagnostic.Stage, diagnostic.Severity, diagnostic.Line, diagnostic.Column, diagnostic.Span.StartOffset, diagnostic.Span.EndOffset, diagnostic.Message))
	}
	return strings.Join(lines, "\n")
}

func utf16OffsetAt(source string, line, column int) int {
	currentLine, currentColumn := 1, 1
	offset := 0
	for _, r := range source {
		if currentLine == line && currentColumn == column {
			return offset
		}
		offset += len(utf16.Encode([]rune{r}))
		if r == '\n' {
			currentLine++
			currentColumn = 1
		} else {
			currentColumn++
		}
	}
	return offset
}
