// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	maxCoolStringCharacters = 1024
	maxLexerDiagnostics     = 100
)

type lexer struct {
	sourceID    string
	runes       []rune
	invalidUTF8 []bool
	start       int
	current     int
	line        int
	column      int
	offset16    int
	startLine   int
	startColumn int
	startOffset int
	tokens      []Token
	diagnostics []Diagnostic
	previousCR  bool
}

func Lex(source string) ([]Token, []Diagnostic) {
	return LexSource("", source)
}

func LexSource(sourceID, source string) ([]Token, []Diagnostic) {
	runes, invalidUTF8 := decodeSource(source)
	l := &lexer{sourceID: sourceID, runes: runes, invalidUTF8: invalidUTF8, line: 1, column: 1}
	l.scan()
	return l.tokens, l.diagnostics
}

func (l *lexer) scan() {
	for !l.atEnd() {
		l.start = l.current
		l.startLine = l.line
		l.startColumn = l.column
		l.startOffset = l.offset16
		l.scanToken()
	}
	span := Span{
		SourceID:  l.sourceID,
		StartLine: l.line, StartColumn: l.column,
		EndLine: l.line, EndColumn: l.column,
		StartOffset: l.offset16, EndOffset: l.offset16,
	}
	l.tokens = append(l.tokens, Token{Type: TokenEOF, Line: l.line, Column: l.column, Offset: l.offset16, Span: span})
}

func (l *lexer) scanToken() {
	invalidUTF8 := l.invalidUTF8[l.current]
	c := l.advance()
	if invalidUTF8 {
		l.addError("COL1008", "source contains malformed UTF-8", "Save COOL source as valid UTF-8.", l.currentSpan())
		return
	}
	switch c {
	case ' ', '\r', '\t', '\f', '\v':
		return
	case '\n':
		return
	case '{':
		l.addSimple(TokenLBrace)
	case '}':
		l.addSimple(TokenRBrace)
	case '(':
		if l.match('*') {
			l.blockComment()
			return
		}
		l.addSimple(TokenLParen)
	case ')':
		l.addSimple(TokenRParen)
	case ':':
		l.addSimple(TokenColon)
	case ';':
		l.addSimple(TokenSemicolon)
	case ',':
		l.addSimple(TokenComma)
	case '.':
		l.addSimple(TokenDot)
	case '@':
		l.addSimple(TokenAt)
	case '+':
		l.addSimple(TokenPlus)
	case '-':
		if l.match('-') {
			l.lineComment()
			return
		}
		l.addSimple(TokenMinus)
	case '*':
		if l.match(')') {
			span := l.currentSpan()
			l.addError("COL1004", "unmatched block-comment terminator '*)'", "Remove the terminator or add the matching '(*'.", span)
			return
		}
		l.addSimple(TokenStar)
	case '/':
		l.addSimple(TokenSlash)
	case '~':
		l.addSimple(TokenTilde)
	case '<':
		if l.match('-') {
			l.addSimple(TokenAssign)
		} else if l.match('=') {
			l.addSimple(TokenLessEqual)
		} else {
			l.addSimple(TokenLess)
		}
	case '=':
		if l.match('>') {
			l.addSimple(TokenCaseArrow)
		} else {
			l.addSimple(TokenEqual)
		}
	case '"':
		l.stringLiteral()
	case '_':
		for isIdentifierPart(l.peek()) {
			l.advance()
		}
		l.addError("COL1009", "identifier cannot begin with '_'", "Begin object identifiers with a lowercase ASCII letter and type identifiers with an uppercase ASCII letter.", l.currentSpan())
	default:
		switch {
		case isASCIIDigit(c):
			l.number()
		case isIdentifierStart(c):
			l.identifier()
		default:
			span := l.currentSpan()
			l.addError("COL1001", "unrecognized character "+strconv.QuoteRune(c), "Use a token from the COOL lexical grammar.", span)
		}
	}
}

func (l *lexer) lineComment() {
	for !l.atEnd() && l.peek() != '\n' && l.peek() != '\r' {
		l.advance()
	}
}

func (l *lexer) blockComment() {
	depth := 1
	for depth > 0 && !l.atEnd() {
		if l.peek() == '(' && l.peekNext() == '*' {
			l.advance()
			l.advance()
			depth++
			continue
		}
		if l.peek() == '*' && l.peekNext() == ')' {
			l.advance()
			l.advance()
			depth--
			continue
		}
		l.advance()
	}
	if depth > 0 {
		l.addError("COL1003", "unterminated block comment", "Close every '(*' with a matching '*)'.", l.currentSpan())
	}
}

func (l *lexer) stringLiteral() {
	var value strings.Builder
	characters := 0
	failed := false
	fail := func(code, message, hint string, span Span) {
		if failed {
			return
		}
		failed = true
		l.addError(code, message, hint, span)
	}
	appendCharacter := func(r rune, span Span) {
		if failed {
			return
		}
		characters++
		if characters > maxCoolStringCharacters {
			fail("COL1006", "string constant is too long", "Keep string constants at or below 1024 characters.", span)
			return
		}
		value.WriteRune(r)
	}
	for !l.atEnd() {
		characterLine, characterColumn, characterOffset := l.line, l.column, l.offset16
		invalidUTF8 := l.invalidUTF8[l.current]
		c := l.advance()
		characterSpan := l.spanFrom(characterLine, characterColumn, characterOffset)
		if invalidUTF8 {
			fail("COL1008", "string contains malformed UTF-8", "Save COOL source as valid UTF-8.", characterSpan)
			continue
		}
		switch c {
		case '"':
			if !failed {
				l.addToken(TokenString, value.String())
			}
			return
		case '\n', '\r':
			fail("COL1002", "unterminated string constant", "Keep the string on one line or escape the newline with a backslash.", l.currentSpan())
			return
		case 0:
			fail("COL1005", "string contains a null character", "Remove the null character from the string.", characterSpan)
		case '\\':
			if l.atEnd() {
				continue
			}
			escapeIndex := l.current
			escaped := l.advance()
			escapeSpan := l.spanFrom(characterLine, characterColumn, characterOffset)
			if l.invalidUTF8[escapeIndex] {
				fail("COL1008", "string escape contains malformed UTF-8", "Save COOL source as valid UTF-8.", escapeSpan)
				continue
			}
			switch escaped {
			case 'b':
				appendCharacter('\b', escapeSpan)
			case 't':
				appendCharacter('\t', escapeSpan)
			case 'n':
				appendCharacter('\n', escapeSpan)
			case 'f':
				appendCharacter('\f', escapeSpan)
			case '\n':
				appendCharacter('\n', escapeSpan)
			case '\r':
				if l.peek() == '\n' {
					l.advance()
					escapeSpan = l.spanFrom(characterLine, characterColumn, characterOffset)
				}
				appendCharacter('\n', escapeSpan)
			case 0:
				fail("COL1010", "string contains an escaped null character", "Remove the escaped null character from the string.", escapeSpan)
			default:
				// COOL treats \c as c for otherwise unrecognized escapes.
				appendCharacter(escaped, escapeSpan)
			}
		default:
			appendCharacter(c, characterSpan)
		}
	}
	fail("COL1002", "unterminated string constant", "Add a closing double quote.", l.currentSpan())
}

func (l *lexer) number() {
	for isASCIIDigit(l.peek()) {
		l.advance()
	}
	lexeme := string(l.runes[l.start:l.current])
	literal := lexeme
	if !validIntegerLiteral(lexeme) {
		l.addError("COL1007", "integer constant is outside the signed 32-bit range", "Use a value from 0 through 2147483647; write negative values with '~'.", l.currentSpan())
		literal = "0"
	}
	l.addToken(TokenInt, literal)
}

func (l *lexer) identifier() {
	for isIdentifierPart(l.peek()) {
		l.advance()
	}
	lexeme := string(l.runes[l.start:l.current])
	lower := strings.ToLower(lexeme)
	if keyword, ok := coolKeywords[lower]; ok {
		l.addToken(keyword, "")
		return
	}
	// The classic COOL rule makes keywords case-insensitive but requires true
	// and false to start with a lowercase letter.
	if (lower == "true" || lower == "false") && len(lexeme) > 0 && isASCIILower(rune(lexeme[0])) {
		l.addToken(TokenBool, lower)
		return
	}
	first := rune(lexeme[0])
	if isASCIIUpper(first) {
		l.addToken(TokenTypeID, lexeme)
	} else {
		l.addToken(TokenObjectID, lexeme)
	}
}

func (l *lexer) addSimple(t TokenType) {
	l.addToken(t, "")
}

func (l *lexer) addToken(t TokenType, literal string) {
	lexeme := string(l.runes[l.start:l.current])
	span := l.currentSpan()
	l.tokens = append(l.tokens, Token{
		Type: t, Lexeme: lexeme, Literal: literal,
		Line: span.StartLine, Column: span.StartColumn, Offset: span.StartOffset, Span: span,
	})
}

func (l *lexer) addError(code, message, hint string, span Span) {
	if len(l.diagnostics) >= maxLexerDiagnostics {
		return
	}
	if len(l.diagnostics) == maxLexerDiagnostics-1 {
		l.diagnostics = append(l.diagnostics, errorDiagnostic("lexer", "COL1010", "lexer diagnostic limit reached", "Fix the earliest lexical errors before requesting more diagnostics.", span))
		return
	}
	l.diagnostics = append(l.diagnostics, errorDiagnostic("lexer", code, message, hint, span))
}

func (l *lexer) currentSpan() Span {
	return Span{
		SourceID:  l.sourceID,
		StartLine: l.startLine, StartColumn: l.startColumn,
		EndLine: l.line, EndColumn: l.column,
		StartOffset: l.startOffset, EndOffset: l.offset16,
	}
}

func (l *lexer) spanFrom(line, column, offset int) Span {
	return Span{
		SourceID:  l.sourceID,
		StartLine: line, StartColumn: column,
		EndLine: l.line, EndColumn: l.column,
		StartOffset: offset, EndOffset: l.offset16,
	}
}

func (l *lexer) atEnd() bool {
	return l.current >= len(l.runes)
}

func (l *lexer) advance() rune {
	if l.atEnd() {
		return 0
	}
	r := l.runes[l.current]
	l.current++
	l.offset16 += utf16RuneLen(r)
	if r == '\r' {
		l.line++
		l.column = 1
		l.previousCR = true
	} else if r == '\n' {
		if !l.previousCR {
			l.line++
		}
		l.column = 1
		l.previousCR = false
	} else {
		l.column++
		l.previousCR = false
	}
	return r
}

func (l *lexer) peek() rune {
	if l.atEnd() {
		return 0
	}
	return l.runes[l.current]
}

func (l *lexer) peekNext() rune {
	if l.current+1 >= len(l.runes) {
		return 0
	}
	return l.runes[l.current+1]
}

func (l *lexer) match(expected rune) bool {
	if l.atEnd() || l.runes[l.current] != expected {
		return false
	}
	l.advance()
	return true
}

func isASCIIDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

func isIdentifierStart(r rune) bool {
	return isASCIIUpper(r) || isASCIILower(r)
}

func isIdentifierPart(r rune) bool {
	return isIdentifierStart(r) || isASCIIDigit(r) || r == '_'
}

func isASCIIUpper(r rune) bool { return r >= 'A' && r <= 'Z' }

func isASCIILower(r rune) bool { return r >= 'a' && r <= 'z' }

func decodeSource(source string) ([]rune, []bool) {
	runes := make([]rune, 0, utf8.RuneCountInString(source))
	invalid := make([]bool, 0, cap(runes))
	for len(source) > 0 {
		r, size := utf8.DecodeRuneInString(source)
		malformed := r == utf8.RuneError && size == 1
		runes = append(runes, r)
		invalid = append(invalid, malformed)
		source = source[size:]
	}
	return runes, invalid
}
