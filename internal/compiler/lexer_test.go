// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"bytes"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestLexerIdentifiersKeywordsAndBooleans(t *testing.T) {
	tests := []struct {
		source  string
		want    TokenType
		literal string
	}{
		{"alpha_09", TokenObjectID, "alpha_09"},
		{"Alpha_09", TokenTypeID, "Alpha_09"},
		{"self", TokenObjectID, "self"},
		{"SELF_TYPE", TokenTypeID, "SELF_TYPE"},
		{"className", TokenObjectID, "className"},
		{"cLaSs", TokenClass, ""},
		{"true", TokenBool, "true"},
		{"tRuE", TokenBool, "true"},
		{"false", TokenBool, "false"},
		{"fAlSe", TokenBool, "false"},
		{"True", TokenTypeID, "True"},
		{"FALSE", TokenTypeID, "FALSE"},
	}
	for _, test := range tests {
		t.Run(test.source, func(t *testing.T) {
			tokens, diagnostics := Lex(test.source)
			if len(diagnostics) != 0 || len(tokens) != 2 {
				t.Fatalf("Lex(%q) tokens=%+v diagnostics=%+v", test.source, tokens, diagnostics)
			}
			if tokens[0].Type != test.want || tokens[0].Literal != test.literal {
				t.Fatalf("Lex(%q) = %s/%q, want %s/%q", test.source, tokens[0].Type, tokens[0].Literal, test.want, test.literal)
			}
		})
	}
}

func TestLexerEveryKeywordAndEmbeddedKeyword(t *testing.T) {
	keywords := make([]string, 0, len(coolKeywords))
	for keyword := range coolKeywords {
		keywords = append(keywords, keyword)
	}
	sort.Strings(keywords)
	for _, keyword := range keywords {
		mixed := strings.ToUpper(keyword[:1]) + keyword[1:]
		tokens, diagnostics := Lex(mixed + " " + keyword + "Suffix")
		if len(diagnostics) != 0 || tokens[0].Type != coolKeywords[keyword] || tokens[1].Type != TokenObjectID {
			t.Fatalf("keyword %q tokens=%+v diagnostics=%+v", keyword, tokens, diagnostics)
		}
	}
	for _, boolean := range []string{"trueValue", "falseValue"} {
		tokens, diagnostics := Lex(boolean)
		if len(diagnostics) != 0 || tokens[0].Type != TokenObjectID {
			t.Fatalf("embedded Boolean keyword %q tokens=%+v diagnostics=%+v", boolean, tokens, diagnostics)
		}
	}
}

func TestLexerRejectsInvalidIdentifierStartsAndNonASCIIIdentifiers(t *testing.T) {
	tokens, diagnostics := Lex("_hidden café")
	assertLexerDiagnosticCodes(t, diagnostics, "COL1009", "COL1001")
	if got := usefulTokenSignatures(tokens); !reflect.DeepEqual(got, []string{"OBJECT_ID|caf|caf"}) {
		t.Fatalf("tokens = %v", got)
	}
}

func TestLexerIntegerBoundariesWithoutOverflow(t *testing.T) {
	valid := []string{"0", "007", "2147483647", "00000000002147483647"}
	for _, source := range valid {
		tokens, diagnostics := Lex(source)
		if len(diagnostics) != 0 || tokens[0].Literal != source {
			t.Fatalf("valid integer %q tokens=%+v diagnostics=%+v", source, tokens, diagnostics)
		}
	}

	for _, source := range []string{"2147483648", strings.Repeat("9", 100_000)} {
		tokens, diagnostics := Lex(source)
		assertLexerDiagnosticCodes(t, diagnostics, "COL1007")
		if tokens[0].Type != TokenInt || tokens[0].Literal != "0" || tokens[0].Lexeme != source {
			t.Fatalf("invalid integer recovery token = %+v", tokens[0])
		}
	}

	result := Compile("class Main { main() : Object { 2147483648 }; };")
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "COL2052" {
			t.Fatalf("integer range rejection cascaded into parser diagnostic: %+v", result.Diagnostics)
		}
	}
}

func TestLexerStringEscapesAndCharacterLimit(t *testing.T) {
	source := `"\b\t\n\f\"\\\q"` + " " + "\"left\\\r\nright\""
	tokens, diagnostics := Lex(source)
	if len(diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diagnostics)
	}
	if got := []string{tokens[0].Literal, tokens[1].Literal}; !reflect.DeepEqual(got, []string{"\b\t\n\f\"\\q", "left\nright"}) {
		t.Fatalf("decoded strings = %q", got)
	}

	atLimit := "\"" + strings.Repeat("é", maxCoolStringCharacters) + "\""
	tokens, diagnostics = Lex(atLimit)
	if len(diagnostics) != 0 || len([]rune(tokens[0].Literal)) != maxCoolStringCharacters {
		t.Fatalf("1024-character Unicode string tokens=%d diagnostics=%+v", len(tokens), diagnostics)
	}
	belowLimit := "\"" + strings.Repeat("é", maxCoolStringCharacters-1) + "\""
	if tokens, diagnostics = Lex(belowLimit); len(diagnostics) != 0 || len([]rune(tokens[0].Literal)) != maxCoolStringCharacters-1 {
		t.Fatalf("1023-character Unicode string tokens=%d diagnostics=%+v", len(tokens), diagnostics)
	}

	overLimit := "\"" + strings.Repeat("é", maxCoolStringCharacters+1) + "\" class"
	tokens, diagnostics = Lex(overLimit)
	assertLexerDiagnosticCodes(t, diagnostics, "COL1006")
	if got := usefulTokenSignatures(tokens); !reflect.DeepEqual(got, []string{"CLASS|class|"}) {
		t.Fatalf("long-string recovery tokens = %v", got)
	}
}

func TestLexerMalformedStringRecovery(t *testing.T) {
	tests := []struct {
		name   string
		source string
		code   string
		want   []string
	}{
		{"newline", "\"bad\nclass", "COL1002", []string{"CLASS|class|"}},
		{"eof", "\"bad", "COL1002", []string{}},
		{"null", "\"a\x00b\" class", "COL1005", []string{"CLASS|class|"}},
		{"escaped null", "\"a\\\x00b\" class", "COL1010", []string{"CLASS|class|"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tokens, diagnostics := Lex(test.source)
			assertLexerDiagnosticCodes(t, diagnostics, test.code)
			if got := usefulTokenSignatures(tokens); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("recovery tokens = %v, want %v", got, test.want)
			}
		})
	}
}

func TestLexerCommentsWhitespaceAndLongestMatchOperators(t *testing.T) {
	source := "-- lf\nclass -- crlf\r\nelse -- cr\rif (* outer (* nested *) \" *)"
	tokens, diagnostics := Lex(source)
	if len(diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diagnostics)
	}
	want := []struct {
		type_  TokenType
		line   int
		column int
	}{{TokenClass, 2, 1}, {TokenElse, 3, 1}, {TokenIf, 4, 1}}
	for index, expected := range want {
		if tokens[index].Type != expected.type_ || tokens[index].Line != expected.line || tokens[index].Column != expected.column {
			t.Fatalf("token[%d] = %+v, want %s at %d:%d", index, tokens[index], expected.type_, expected.line, expected.column)
		}
	}

	operatorSource := "{ } ( ) : ; , . @ + - * / ~ < <= <- = =>"
	tokens, diagnostics = Lex(operatorSource)
	if len(diagnostics) != 0 {
		t.Fatalf("operator diagnostics: %+v", diagnostics)
	}
	wantTypes := []TokenType{
		TokenLBrace, TokenRBrace, TokenLParen, TokenRParen, TokenColon, TokenSemicolon,
		TokenComma, TokenDot, TokenAt, TokenPlus, TokenMinus, TokenStar, TokenSlash,
		TokenTilde, TokenLess, TokenLessEqual, TokenAssign, TokenEqual, TokenCaseArrow,
	}
	for index, wantType := range wantTypes {
		if tokens[index].Type != wantType {
			t.Fatalf("operator token[%d] = %s, want %s", index, tokens[index].Type, wantType)
		}
	}

	_, diagnostics = Lex("(* outer (* inner")
	assertLexerDiagnosticCodes(t, diagnostics, "COL1003")
	_, diagnostics = Lex("*)")
	assertLexerDiagnosticCodes(t, diagnostics, "COL1004")

	tokens, diagnostics = Lex(`"comment markers (* -- *) stay in strings"`)
	if len(diagnostics) != 0 || tokens[0].Type != TokenString || tokens[0].Literal != "comment markers (* -- *) stay in strings" {
		t.Fatalf("comment delimiters affected string tokenization: tokens=%+v diagnostics=%+v", tokens, diagnostics)
	}
}

func TestLexerUTF8AndSpanCoordinateContract(t *testing.T) {
	source := "\"😀\"\r\nclass"
	tokens, diagnostics := Lex(source)
	if len(diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diagnostics)
	}
	class := tokens[1]
	if class.Line != 2 || class.Column != 1 || class.Span.StartOffset != 6 || strings.Index(source, "class") != 8 {
		t.Fatalf("class coordinates = %+v, byte index=%d", class, strings.Index(source, "class"))
	}

	malformed := string([]byte{'a', 0xff, 'b'})
	tokens, diagnostics = Lex(malformed)
	assertLexerDiagnosticCodes(t, diagnostics, "COL1008")
	if got := usefulTokenSignatures(tokens); !reflect.DeepEqual(got, []string{"OBJECT_ID|a|a", "OBJECT_ID|b|b"}) {
		t.Fatalf("malformed UTF-8 recovery tokens = %v", got)
	}
	assertLexInvariants(t, malformed, tokens, diagnostics)

	_, diagnostics = Lex(string(bytes.Repeat([]byte{0xff}, maxLexerDiagnostics*2)))
	if len(diagnostics) != maxLexerDiagnostics || diagnostics[len(diagnostics)-1].Code != "COL1010" {
		t.Fatalf("malformed UTF-8 diagnostic bound = %+v", diagnostics)
	}

	_, diagnostics = Lex("\uFFFD\u00a0")
	assertLexerDiagnosticCodes(t, diagnostics, "COL1001", "COL1001")
}

func TestLexerDiagnosticOrderingIsDeterministic(t *testing.T) {
	source := "_bad " + string([]byte{0xff}) + " *) \"x\x00\""
	firstTokens, first := Lex(source)
	secondTokens, second := Lex(source)
	if !reflect.DeepEqual(firstTokens, secondTokens) || !reflect.DeepEqual(first, second) {
		t.Fatal("lexer output changed between identical runs")
	}
	assertLexerDiagnosticCodes(t, first, "COL1009", "COL1008", "COL1004", "COL1005")
}

func TestLexerAdversarialLengthsRemainBoundedAndRecover(t *testing.T) {
	const size = 1 << 20
	tests := []struct {
		name       string
		source     string
		wantCode   string
		wantTokens int
	}{
		{"integer", strings.Repeat("9", size), "COL1007", 1},
		{"string", "\"" + strings.Repeat("x", size) + "\" class", "COL1006", 1},
		{"comment", "(*" + strings.Repeat("x", size), "COL1003", 0},
		{"identifier", strings.Repeat("_", size), "COL1009", 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tokens, diagnostics := Lex(test.source)
			assertLexerDiagnosticCodes(t, diagnostics, test.wantCode)
			if got := len(tokens) - 1; got != test.wantTokens {
				t.Fatalf("useful token count = %d, want %d", got, test.wantTokens)
			}
		})
	}
}

func FuzzLex(f *testing.F) {
	for _, seed := range []string{
		"", "_bad", "(* nested (*", "\"unterminated", "\"a\x00b\"",
		"\"escaped\\\nnewline\"", "2147483648", string([]byte{0xff, 0xfe}),
		"-- comment\r\nclass Main {};", strings.Repeat("9", 4096),
	} {
		f.Add(seed)
	}
	addCompilerFuzzSourceSeeds(f, func(source string) { f.Add(source) })
	f.Fuzz(func(t *testing.T, source string) {
		tokens, diagnostics := Lex(source)
		assertLexInvariants(t, source, tokens, diagnostics)
		againTokens, againDiagnostics := Lex(source)
		if !reflect.DeepEqual(tokens, againTokens) || !reflect.DeepEqual(diagnostics, againDiagnostics) {
			t.Fatal("lexer output is nondeterministic")
		}
	})
}

func assertLexInvariants(t testing.TB, source string, tokens []Token, diagnostics []Diagnostic) {
	t.Helper()
	runes, _ := decodeSource(source)
	totalOffset := 0
	for _, r := range runes {
		totalOffset += utf16RuneLen(r)
	}
	previousEnd := 0
	for index, token := range tokens {
		span := token.Span
		if span.StartLine < 1 || span.StartColumn < 1 || span.EndLine < span.StartLine || span.StartOffset < previousEnd || span.EndOffset < span.StartOffset || span.EndOffset > totalOffset {
			t.Fatalf("token[%d] has invalid or regressing span %+v (total UTF-16=%d)", index, span, totalOffset)
		}
		previousEnd = span.EndOffset
	}
	previousDiagnosticEnd := 0
	for index, diagnostic := range diagnostics {
		span := diagnostic.Span
		if span.StartLine < 1 || span.StartColumn < 1 || span.EndLine < span.StartLine || span.StartOffset < previousDiagnosticEnd || span.EndOffset < span.StartOffset || span.EndOffset > totalOffset {
			t.Fatalf("diagnostic[%d] has invalid span %+v (total UTF-16=%d)", index, span, totalOffset)
		}
		previousDiagnosticEnd = span.EndOffset
	}
}

func assertLexerDiagnosticCodes(t testing.TB, diagnostics []Diagnostic, want ...string) {
	t.Helper()
	got := make([]string, len(diagnostics))
	for index, diagnostic := range diagnostics {
		got[index] = diagnostic.Code
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diagnostic codes = %v, want %v; diagnostics=%+v", got, want, diagnostics)
	}
}

func usefulTokenSignatures(tokens []Token) []string {
	result := []string{}
	for _, token := range tokens {
		if token.Type != TokenEOF {
			result = append(result, string(token.Type)+"|"+token.Lexeme+"|"+token.Literal)
		}
	}
	return result
}
