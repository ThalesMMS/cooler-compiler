// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"fmt"
	"strings"
)

// Reference ("course") output formats reproduce the text printed by the
// classic Stanford/UC Berkeley COOL support code: the PA2 lexer token dump,
// the PA3 parser AST dump, and the PA4 semant dump and diagnostics. They are
// opt-in views beside Cooler's own JSON/text artifacts and never replace them.

// ReferenceTokenDump renders the course `lexer` token stream for each unit:
// a `#name "file"` header per unit followed by `#line TOKEN [value]` lines.
// Lexical errors appear in the stream as ERROR tokens, as in the course
// lexer, which leaves halting to the parser.
func ReferenceTokenDump(units []SourceUnit) string {
	return ReferencePipeline{}.Tokens(units)
}

// ReferencePipeline runs the course-compatibility phases with a selected
// front-end lexer: "" or FrontendHandwritten for the hand-written lexer, or
// FrontendGenerated for the spec-driven lexer. Parsing always uses the LALR
// grammar, whose Bison semantics the reference formats depend on.
type ReferencePipeline struct {
	Frontend string
}

func (p ReferencePipeline) lex(sourceID, source string) []Token {
	if p.Frontend == FrontendGenerated {
		return LexGeneratedReference(sourceID, source)
	}
	return LexReferenceSource(sourceID, source)
}

// Tokens renders the course lexer token stream (see ReferenceTokenDump).
func (p ReferencePipeline) Tokens(units []SourceUnit) string {
	var out strings.Builder
	for _, unit := range units {
		fmt.Fprintf(&out, "#name \"%s\"\n", referenceEscape(unit.Name))
		for _, token := range p.lex(unit.ID, unit.Content) {
			if token.Type == TokenEOF {
				continue
			}
			out.WriteString(referenceTokenLine(token))
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// referenceTokenLine formats one token like the support code's
// dump_cool_token. The line is the lexer's line counter after the token was
// recognized, which is the token's last line (multi-line strings and errors
// report where they end).
func referenceTokenLine(token Token) string {
	line := token.Span.EndLine
	if line == 0 {
		line = token.Line
	}
	text := fmt.Sprintf("#%d %s", line, referenceTokenName(token.Type))
	switch token.Type {
	case TokenString:
		text += " \"" + referenceEscape(token.Literal) + "\""
	case TokenInt, TokenTypeID, TokenObjectID:
		text += " " + token.Lexeme
	case TokenBool:
		text += " " + token.Literal
	case TokenError:
		text += " \"" + referenceEscape(token.Literal) + "\""
	}
	return text
}

// referenceTokenName mirrors cool_token_to_string.
func referenceTokenName(t TokenType) string {
	switch t {
	case TokenEOF:
		return "EOF"
	case TokenError:
		return "ERROR"
	case TokenTypeID:
		return "TYPEID"
	case TokenObjectID:
		return "OBJECTID"
	case TokenInt:
		return "INT_CONST"
	case TokenString:
		return "STR_CONST"
	case TokenBool:
		return "BOOL_CONST"
	case TokenAssign:
		return "ASSIGN"
	case TokenCaseArrow:
		return "DARROW"
	case TokenLessEqual:
		return "LE"
	case TokenClass, TokenElse, TokenFi, TokenIf, TokenIn, TokenInherits, TokenIsVoid, TokenLet,
		TokenLoop, TokenPool, TokenThen, TokenWhile, TokenCase, TokenEsac, TokenNew, TokenOf, TokenNot:
		return string(t)
	}
	if char := referenceTokenChar(t); char != "" {
		return "'" + char + "'"
	}
	return string(t)
}

func referenceTokenChar(t TokenType) string {
	switch t {
	case TokenLBrace:
		return "{"
	case TokenRBrace:
		return "}"
	case TokenLParen:
		return "("
	case TokenRParen:
		return ")"
	case TokenColon:
		return ":"
	case TokenSemicolon:
		return ";"
	case TokenComma:
		return ","
	case TokenDot:
		return "."
	case TokenAt:
		return "@"
	case TokenPlus:
		return "+"
	case TokenMinus:
		return "-"
	case TokenStar:
		return "*"
	case TokenSlash:
		return "/"
	case TokenTilde:
		return "~"
	case TokenLess:
		return "<"
	case TokenEqual:
		return "="
	}
	return ""
}

// referenceEscape mirrors print_escaped_string: it works on bytes, escapes
// backslash, quote, and the named control characters, and prints any other
// non-printable byte as a three-digit octal escape.
func referenceEscape(value string) string {
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		b := value[i]
		switch b {
		case '\\':
			out.WriteString(`\\`)
		case '"':
			out.WriteString(`\"`)
		case '\n':
			out.WriteString(`\n`)
		case '\t':
			out.WriteString(`\t`)
		case '\b':
			out.WriteString(`\b`)
		case '\f':
			out.WriteString(`\f`)
		default:
			if b >= 0x20 && b < 0x7f {
				out.WriteByte(b)
			} else {
				fmt.Fprintf(&out, "\\%03o", b)
			}
		}
	}
	return out.String()
}
