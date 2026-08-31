// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

type TokenType string

const (
	TokenEOF      TokenType = "EOF"
	TokenError    TokenType = "ERROR"
	TokenTypeID   TokenType = "TYPE_ID"
	TokenObjectID TokenType = "OBJECT_ID"
	TokenInt      TokenType = "INT_CONST"
	TokenString   TokenType = "STR_CONST"
	TokenBool     TokenType = "BOOL_CONST"

	TokenClass    TokenType = "CLASS"
	TokenElse     TokenType = "ELSE"
	TokenFi       TokenType = "FI"
	TokenIf       TokenType = "IF"
	TokenIn       TokenType = "IN"
	TokenInherits TokenType = "INHERITS"
	TokenIsVoid   TokenType = "ISVOID"
	TokenLet      TokenType = "LET"
	TokenLoop     TokenType = "LOOP"
	TokenPool     TokenType = "POOL"
	TokenThen     TokenType = "THEN"
	TokenWhile    TokenType = "WHILE"
	TokenCase     TokenType = "CASE"
	TokenEsac     TokenType = "ESAC"
	TokenNew      TokenType = "NEW"
	TokenOf       TokenType = "OF"
	TokenNot      TokenType = "NOT"

	TokenLBrace    TokenType = "LBRACE"
	TokenRBrace    TokenType = "RBRACE"
	TokenLParen    TokenType = "LPAREN"
	TokenRParen    TokenType = "RPAREN"
	TokenColon     TokenType = "COLON"
	TokenSemicolon TokenType = "SEMICOLON"
	TokenComma     TokenType = "COMMA"
	TokenDot       TokenType = "DOT"
	TokenAt        TokenType = "AT"

	TokenAssign    TokenType = "ASSIGN"     // <-
	TokenCaseArrow TokenType = "CASE_ARROW" // =>
	TokenPlus      TokenType = "PLUS"
	TokenMinus     TokenType = "MINUS"
	TokenStar      TokenType = "STAR"
	TokenSlash     TokenType = "SLASH"
	TokenTilde     TokenType = "TILDE"
	TokenLess      TokenType = "LESS"
	TokenLessEqual TokenType = "LESS_EQUAL"
	TokenEqual     TokenType = "EQUAL"
)

type Token struct {
	Type    TokenType `json:"type"`
	Lexeme  string    `json:"lexeme"`
	Literal string    `json:"literal,omitempty"`
	Line    int       `json:"line"`
	Column  int       `json:"column"`
	Offset  int       `json:"offset"`
	Span    Span      `json:"span"`
}

var coolKeywords = map[string]TokenType{
	"class":    TokenClass,
	"else":     TokenElse,
	"fi":       TokenFi,
	"if":       TokenIf,
	"in":       TokenIn,
	"inherits": TokenInherits,
	"isvoid":   TokenIsVoid,
	"let":      TokenLet,
	"loop":     TokenLoop,
	"pool":     TokenPool,
	"then":     TokenThen,
	"while":    TokenWhile,
	"case":     TokenCase,
	"esac":     TokenEsac,
	"new":      TokenNew,
	"of":       TokenOf,
	"not":      TokenNot,
}

func tokenDisplayName(t TokenType) string {
	switch t {
	case TokenEOF:
		return "end of file"
	case TokenTypeID:
		return "type identifier"
	case TokenObjectID:
		return "object identifier"
	case TokenInt:
		return "integer"
	case TokenString:
		return "string"
	case TokenBool:
		return "Boolean"
	case TokenLBrace:
		return "'{'"
	case TokenRBrace:
		return "'}'"
	case TokenLParen:
		return "'('"
	case TokenRParen:
		return "')'"
	case TokenColon:
		return "':'"
	case TokenSemicolon:
		return "';'"
	case TokenComma:
		return "','"
	case TokenDot:
		return "'.'"
	case TokenAt:
		return "'@'"
	case TokenAssign:
		return "'<-'"
	case TokenCaseArrow:
		return "'=>'"
	case TokenPlus:
		return "'+'"
	case TokenMinus:
		return "'-'"
	case TokenStar:
		return "'*'"
	case TokenSlash:
		return "'/'"
	case TokenTilde:
		return "'~'"
	case TokenLess:
		return "'<'"
	case TokenLessEqual:
		return "'<='"
	case TokenEqual:
		return "'='"
	default:
		return string(t)
	}
}
