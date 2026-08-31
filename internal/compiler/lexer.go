// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"strconv"
	"strings"
	"unicode"
)

type lexer struct {
	source      string
	runes       []rune
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
}

func Lex(source string) ([]Token, []Diagnostic) {
	l := &lexer{source: source, runes: []rune(source), line: 1, column: 1}
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
		StartLine: l.line, StartColumn: l.column,
		EndLine: l.line, EndColumn: l.column,
		StartOffset: l.offset16, EndOffset: l.offset16,
	}
	l.tokens = append(l.tokens, Token{Type: TokenEOF, Line: l.line, Column: l.column, Offset: l.offset16, Span: span})
}

func (l *lexer) scanToken() {
	c := l.advance()
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
	for !l.atEnd() && l.peek() != '\n' {
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
	valid := true
	for !l.atEnd() {
		c := l.advance()
		switch c {
		case '"':
			if valid {
				l.addToken(TokenString, value.String())
			}
			return
		case '\n':
			l.addError("COL1002", "unterminated string constant", "Keep the string on one line or escape the newline with a backslash.", l.currentSpan())
			return
		case 0:
			valid = false
			l.addError("COL1005", "string contains a null character", "Remove the null character from the string.", l.currentSpan())
		case '\\':
			if l.atEnd() {
				break
			}
			escaped := l.advance()
			switch escaped {
			case 'b':
				value.WriteByte('\b')
			case 't':
				value.WriteByte('\t')
			case 'n':
				value.WriteByte('\n')
			case 'f':
				value.WriteByte('\f')
			case '\n':
				value.WriteByte('\n')
			default:
				// COOL treats \c as c for otherwise unrecognized escapes.
				value.WriteRune(escaped)
			}
		default:
			value.WriteRune(c)
		}
		if value.Len() > 1024 {
			valid = false
			l.addError("COL1006", "string constant is too long", "Keep string constants at or below 1024 bytes.", l.currentSpan())
			// Continue scanning to the closing quote so the lexer can recover.
		}
	}
	l.addError("COL1002", "unterminated string constant", "Add a closing double quote.", l.currentSpan())
}

func (l *lexer) number() {
	for isASCIIDigit(l.peek()) {
		l.advance()
	}
	l.addToken(TokenInt, string(l.runes[l.start:l.current]))
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
	if (lower == "true" || lower == "false") && len(lexeme) > 0 && unicode.IsLower([]rune(lexeme)[0]) {
		l.addToken(TokenBool, lower)
		return
	}
	first := []rune(lexeme)[0]
	if unicode.IsUpper(first) {
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
	l.diagnostics = append(l.diagnostics, errorDiagnostic("lexer", code, message, hint, span))
}

func (l *lexer) currentSpan() Span {
	return Span{
		StartLine: l.startLine, StartColumn: l.startColumn,
		EndLine: l.line, EndColumn: l.column,
		StartOffset: l.startOffset, EndOffset: l.offset16,
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
	if r == '\n' {
		l.line++
		l.column = 1
	} else {
		l.column++
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
	return r == '_' || unicode.IsLetter(r)
}

func isIdentifierPart(r rune) bool {
	return isIdentifierStart(r) || isASCIIDigit(r)
}
