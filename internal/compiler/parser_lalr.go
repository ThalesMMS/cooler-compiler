// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

// The LALR parser is generated from cool.y by 'make generate-frontend'.

import (
	"fmt"
	"strconv"
)

// maxLALRDiagnostics mirrors the course parser, which gives up after more
// than 50 syntax errors.
const maxLALRDiagnostics = 50

// LALRSyntaxError is one syntax error reported by the generated LALR parser,
// with the lookahead token that the parser could not shift.
type LALRSyntaxError struct {
	Token Token
	// Location is where the course parser reports the error: the token
	// itself, or at end of input the last token read (its line counter only
	// advances when a token is read).
	Location Token
}

// lalrLexer adapts a Cooler token stream to the goyacc parser.
type lalrLexer struct {
	tokens      []Token
	position    int
	last        Token
	program     *Program
	reference   bool
	errors      []LALRSyntaxError
	diagnostics []Diagnostic
	tooMany     bool
	// lines records each expression node's course line number (the
	// node_lineno current when the course parser constructs it).
	lines map[Expr]int
	// lastReal is the last non-EOF token returned.
	lastReal Token
}

func (l *lalrLexer) node(expr Expr, loc int) Expr {
	if l.lines != nil {
		l.lines[expr] = loc
	}
	return expr
}

var lalrTokenCodes = map[TokenType]int{
	TokenClass: CLASS, TokenElse: ELSE, TokenFi: FI, TokenIf: IF, TokenIn: IN,
	TokenInherits: INHERITS, TokenLet: LET, TokenLoop: LOOP, TokenPool: POOL,
	TokenThen: THEN, TokenWhile: WHILE, TokenCase: CASE, TokenEsac: ESAC,
	TokenOf: OF, TokenCaseArrow: DARROW, TokenNew: NEW, TokenIsVoid: ISVOID,
	TokenString: STR_CONST, TokenInt: INT_CONST, TokenBool: BOOL_CONST,
	TokenTypeID: TYPEID, TokenObjectID: OBJECTID, TokenAssign: ASSIGN,
	TokenNot: NOT, TokenLessEqual: LE, TokenError: ERROR,
	TokenLBrace: '{', TokenRBrace: '}', TokenLParen: '(', TokenRParen: ')',
	TokenColon: ':', TokenSemicolon: ';', TokenComma: ',', TokenDot: '.',
	TokenAt: '@', TokenPlus: '+', TokenMinus: '-', TokenStar: '*',
	TokenSlash: '/', TokenTilde: '~', TokenLess: '<', TokenEqual: '=',
}

func (l *lalrLexer) Lex(lval *coolyySymType) int {
	if l.tooMany {
		return 0
	}
	for l.position < len(l.tokens) {
		token := l.tokens[l.position]
		l.position++
		if token.Type == TokenEOF {
			continue
		}
		code, ok := lalrTokenCodes[token.Type]
		if !ok {
			code = ERROR
		}
		l.last = token
		l.lastReal = token
		lval.tok = token
		lval.loc = referenceLine(token)
		return code
	}
	// Keep the end-of-input token's own span for Cooler diagnostics; the
	// course location (the last token read) is recorded separately.
	if l.last.Type != TokenEOF {
		l.lastReal = l.last
	}
	eof := Token{Type: TokenEOF, Line: l.lastReal.Line, Column: l.lastReal.Column, Offset: l.lastReal.Offset, Span: l.lastReal.Span}
	if count := len(l.tokens); count > 0 && l.tokens[count-1].Type == TokenEOF {
		eof = l.tokens[count-1]
	}
	l.last = eof
	return 0
}

func (l *lalrLexer) Error(string) {
	token := l.last
	location := token
	if token.Type == TokenEOF {
		location = l.lastReal
		location.Type = TokenEOF
	}
	l.errors = append(l.errors, LALRSyntaxError{Token: token, Location: location})
	if len(l.errors) > maxLALRDiagnostics {
		l.tooMany = true
		return
	}
	l.diagnostics = append(l.diagnostics, errorDiagnostic("parser", "COL2200",
		"syntax error at or near "+referenceParserTokenText(token),
		"The generated LALR parser could not shift this token; check the construct that ends here.", token.Span))
}

func (l *lalrLexer) intExpr(token Token) Expr {
	literal := token.Literal
	if l.reference {
		literal = token.Lexeme
	}
	value, ok := parseCoolIntLiteral(literal)
	if !ok && !l.reference {
		l.diagnostics = append(l.diagnostics, errorDiagnostic("parser", "COL2052", "integer constant is outside the signed 32-bit range", "Use a value from 0 through 2147483647; write negative values with '~'.", token.Span))
		value = 0
	}
	if !ok && l.reference {
		// The course compiler keeps the literal text; wrap like a C int.
		if parsed, err := strconv.ParseUint(token.Lexeme, 10, 64); err == nil {
			value = coolInt(int32(uint32(parsed)))
		}
	}
	return &IntExpr{exprBase: exprBase{Span: token.Span}, Value: value, Raw: token.Lexeme}
}

// ParseLALR parses tokens with the goyacc-generated LALR(1) parser (the
// "generated" front-end). The resulting AST uses the same node types and
// spans as Parse.
func ParseLALR(tokens []Token) (*Program, []Diagnostic) {
	program, diagnostics, _ := parseLALR(tokens, false)
	return program, diagnostics
}

func parseLALR(tokens []Token, reference bool) (*Program, []Diagnostic, []LALRSyntaxError) {
	program, diagnostics, errors, _ := parseLALRWithLines(tokens, reference)
	return program, diagnostics, errors
}

func parseLALRWithLines(tokens []Token, reference bool) (*Program, []Diagnostic, []LALRSyntaxError, map[Expr]int) {
	lexer := &lalrLexer{tokens: tokens, reference: reference, lines: map[Expr]int{}}
	coolyyNewParser().Parse(lexer)
	program := lexer.program
	if program == nil {
		program = &Program{}
	}
	if len(program.Classes) > 0 {
		program.Span = joinSpans(program.Classes[0].Span, program.Classes[len(program.Classes)-1].Span)
	} else {
		program.Span = emptySpan()
		if len(tokens) > 0 {
			program.Span = tokens[0].Span
		}
	}
	return program, lexer.diagnostics, lexer.errors, lexer.lines
}

// referenceLine is the course lexer's line for a token: the line counter
// after the token was recognized.
func referenceLine(token Token) int {
	if token.Span.EndLine > 0 {
		return token.Span.EndLine
	}
	return token.Line
}

func appendClass(classes []*ClassDecl, class *ClassDecl) []*ClassDecl {
	if class == nil {
		return classes
	}
	return append(classes, class)
}

func withFeatureEnd(feature Feature, end Span) Feature {
	switch f := feature.(type) {
	case *MethodDecl:
		f.Span = joinSpans(f.Span, end)
	case *AttributeDecl:
		f.Span = joinSpans(f.Span, end)
	}
	return feature
}

func binaryExpr(left Expr, operator Token, kind TokenType, right Expr) Expr {
	return &BinaryExpr{exprBase: exprBase{Span: joinSpans(left.GetSpan(), right.GetSpan())}, Left: left, Operator: kind, OpSpan: operator.Span, Right: right}
}

// referenceParserTokenText mirrors the course parser's print_cool_token for
// the "syntax error at or near" message.
func referenceParserTokenText(token Token) string {
	switch token.Type {
	case TokenEOF:
		return "EOF"
	case TokenString:
		return "STR_CONST = \"" + referenceEscape(token.Literal) + "\""
	case TokenInt:
		return "INT_CONST = " + token.Lexeme
	case TokenBool:
		return "BOOL_CONST = " + token.Literal
	case TokenTypeID:
		return "TYPEID = " + token.Lexeme
	case TokenObjectID:
		return "OBJECTID = " + token.Lexeme
	case TokenError:
		return "ERROR = \"" + referenceEscape(token.Literal) + "\""
	}
	if char := referenceTokenChar(token.Type); char != "" {
		return "'" + char + "'"
	}
	if name := referenceTokenName(token.Type); name != "" {
		return name
	}
	return fmt.Sprint(token.Type)
}
