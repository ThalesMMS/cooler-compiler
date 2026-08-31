// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"fmt"
	"strconv"
)

type parser struct {
	tokens      []Token
	current     int
	diagnostics []Diagnostic
}

func Parse(tokens []Token) (*Program, []Diagnostic) {
	p := &parser{tokens: tokens}
	program := p.parseProgram()
	return program, p.diagnostics
}

func (p *parser) parseProgram() *Program {
	program := &Program{}
	for !p.atEnd() {
		if !p.check(TokenClass) {
			p.report(p.peek(), "COL2001", "expected 'class' at the start of a class declaration", "A COOL program is a sequence of class declarations ending in semicolons.")
			p.synchronizeToClass()
			continue
		}
		if class := p.parseClass(); class != nil {
			program.Classes = append(program.Classes, class)
		}
	}
	if len(program.Classes) == 0 {
		span := emptySpan()
		if len(p.tokens) > 0 {
			span = p.tokens[0].Span
		}
		p.diagnostics = append(p.diagnostics, errorDiagnostic("parser", "COL2002", "program contains no class declarations", "Declare at least 'class Main { main() : Object { 0 }; };'.", span))
		program.Span = span
		return program
	}
	program.Span = joinSpans(program.Classes[0].Span, program.Classes[len(program.Classes)-1].Span)
	return program
}

func (p *parser) parseClass() *ClassDecl {
	classToken := p.advance() // class
	name, ok := p.consume(TokenTypeID, "COL2003", "expected a type identifier after 'class'", "Class names begin with an uppercase letter.")
	if !ok {
		p.synchronizeToClass()
		return nil
	}
	parent := "Object"
	parentSpan := name.Span
	if p.match(TokenInherits) {
		parentToken, found := p.consume(TokenTypeID, "COL2004", "expected a parent type after 'inherits'", "Use a class name such as Object or IO.")
		if found {
			parent = parentToken.Lexeme
			parentSpan = parentToken.Span
		}
	}
	if _, ok := p.consume(TokenLBrace, "COL2005", "expected '{' before the class features", "Open the class body with '{'."); !ok {
		p.synchronizeToClass()
		return nil
	}

	class := &ClassDecl{Name: name.Lexeme, Parent: parent, NameSpan: name.Span, ParentSpan: parentSpan}
	for !p.check(TokenRBrace) && !p.atEnd() {
		feature := p.parseFeature()
		if feature != nil {
			class.Features = append(class.Features, feature)
		}
	}
	close, closeOK := p.consume(TokenRBrace, "COL2006", "expected '}' after the class features", "Close the class body before its final semicolon.")
	semi, semiOK := p.consume(TokenSemicolon, "COL2007", "expected ';' after the class declaration", "COOL class declarations end with '};'.")
	end := classToken.Span
	if closeOK {
		end = close.Span
	}
	if semiOK {
		end = semi.Span
	}
	class.Span = joinSpans(classToken.Span, end)
	return class
}

func (p *parser) parseFeature() Feature {
	name, ok := p.consume(TokenObjectID, "COL2010", "expected an attribute or method name", "Feature names begin with a lowercase letter.")
	if !ok {
		p.synchronizeFeature()
		return nil
	}
	if p.match(TokenLParen) {
		return p.parseMethod(name)
	}
	return p.parseAttribute(name)
}

func (p *parser) parseMethod(name Token) Feature {
	formals := []Formal{}
	if !p.check(TokenRParen) {
		for {
			formalName, nameOK := p.consume(TokenObjectID, "COL2011", "expected a formal parameter name", "Formal names begin with a lowercase letter.")
			if !nameOK {
				p.synchronizeFeature()
				return nil
			}
			p.consume(TokenColon, "COL2012", "expected ':' after the formal parameter name", "Write formals as 'name : Type'.")
			formalType, typeOK := p.consume(TokenTypeID, "COL2013", "expected a formal parameter type", "Use a declared type name.")
			if !typeOK {
				p.synchronizeFeature()
				return nil
			}
			formals = append(formals, Formal{
				Name: formalName.Lexeme, TypeName: formalType.Lexeme,
				NameSpan: formalName.Span, TypeSpan: formalType.Span,
				Span: joinSpans(formalName.Span, formalType.Span),
			})
			if !p.match(TokenComma) {
				break
			}
		}
	}
	p.consume(TokenRParen, "COL2014", "expected ')' after the formal parameters", "Close the parameter list with ')'.")
	p.consume(TokenColon, "COL2015", "expected ':' before the method return type", "Write methods as 'method(...) : ReturnType { expression };'.")
	returnType, typeOK := p.consume(TokenTypeID, "COL2016", "expected a method return type", "Use a declared type name or SELF_TYPE.")
	if !typeOK {
		p.synchronizeFeature()
		return nil
	}
	p.consume(TokenLBrace, "COL2017", "expected '{' before the method body", "A method body is one COOL expression enclosed in braces.")
	body := p.parseExpression()
	close, closeOK := p.consume(TokenRBrace, "COL2018", "expected '}' after the method body", "Close the method body before its semicolon.")
	semi, semiOK := p.consume(TokenSemicolon, "COL2019", "expected ';' after the method declaration", "COOL methods end with '};'.")
	end := body.GetSpan()
	if closeOK {
		end = close.Span
	}
	if semiOK {
		end = semi.Span
	}
	return &MethodDecl{
		Name: name.Lexeme, Formals: formals, ReturnType: returnType.Lexeme, Body: body,
		NameSpan: name.Span, TypeSpan: returnType.Span, Span: joinSpans(name.Span, end),
	}
}

func (p *parser) parseAttribute(name Token) Feature {
	if _, ok := p.consume(TokenColon, "COL2020", "expected ':' after the attribute name", "Write attributes as 'name : Type' with an optional initializer."); !ok {
		p.synchronizeFeature()
		return nil
	}
	typeToken, typeOK := p.consume(TokenTypeID, "COL2021", "expected an attribute type", "Use a declared type name or SELF_TYPE.")
	if !typeOK {
		p.synchronizeFeature()
		return nil
	}
	var init Expr
	if p.match(TokenAssign) {
		init = p.parseExpression()
	}
	semi, semiOK := p.consume(TokenSemicolon, "COL2022", "expected ';' after the attribute declaration", "COOL attributes end with ';'.")
	end := typeToken.Span
	if init != nil {
		end = init.GetSpan()
	}
	if semiOK {
		end = semi.Span
	}
	return &AttributeDecl{
		Name: name.Lexeme, TypeName: typeToken.Lexeme, Init: init,
		NameSpan: name.Span, TypeSpan: typeToken.Span, Span: joinSpans(name.Span, end),
	}
}

func (p *parser) parseExpression() Expr {
	return p.parseAssignment()
}

func (p *parser) parseAssignment() Expr {
	left := p.parseNot()
	if p.match(TokenAssign) {
		operator := p.previous()
		value := p.parseAssignment()
		identifier, ok := left.(*IdentifierExpr)
		if !ok {
			p.diagnostics = append(p.diagnostics, errorDiagnostic("parser", "COL2030", "the left side of '<-' must be an object identifier", "Assign to a local variable, formal parameter, or attribute.", operator.Span))
			return value
		}
		return &AssignExpr{
			exprBase: exprBase{Span: joinSpans(left.GetSpan(), value.GetSpan())},
			Name:     identifier.Name, NameSpan: identifier.GetSpan(), Value: value,
		}
	}
	return left
}

// 'not' binds less tightly than comparisons in the classic COOL precedence
// table, so not a < b parses as not (a < b).
func (p *parser) parseNot() Expr {
	if p.match(TokenNot) {
		op := p.previous()
		value := p.parseNot()
		return &UnaryExpr{exprBase: exprBase{Span: joinSpans(op.Span, value.GetSpan())}, Operator: TokenNot, OpSpan: op.Span, Value: value}
	}
	return p.parseComparison()
}

func (p *parser) parseComparison() Expr {
	expr := p.parseAddition()
	if p.match(TokenLess, TokenLessEqual, TokenEqual) {
		op := p.previous()
		right := p.parseAddition()
		expr = &BinaryExpr{exprBase: exprBase{Span: joinSpans(expr.GetSpan(), right.GetSpan())}, Left: expr, Operator: op.Type, OpSpan: op.Span, Right: right}
		if p.check(TokenLess) || p.check(TokenLessEqual) || p.check(TokenEqual) {
			p.report(p.peek(), "COL2031", "comparison operators are non-associative in COOL", "Parenthesize the intended comparison instead of chaining operators.")
			for p.match(TokenLess, TokenLessEqual, TokenEqual) {
				_ = p.parseAddition()
			}
		}
	}
	return expr
}

func (p *parser) parseAddition() Expr {
	expr := p.parseMultiplication()
	for p.match(TokenPlus, TokenMinus) {
		op := p.previous()
		right := p.parseMultiplication()
		expr = &BinaryExpr{exprBase: exprBase{Span: joinSpans(expr.GetSpan(), right.GetSpan())}, Left: expr, Operator: op.Type, OpSpan: op.Span, Right: right}
	}
	return expr
}

func (p *parser) parseMultiplication() Expr {
	expr := p.parseIsVoid()
	for p.match(TokenStar, TokenSlash) {
		op := p.previous()
		right := p.parseIsVoid()
		expr = &BinaryExpr{exprBase: exprBase{Span: joinSpans(expr.GetSpan(), right.GetSpan())}, Left: expr, Operator: op.Type, OpSpan: op.Span, Right: right}
	}
	return expr
}

func (p *parser) parseIsVoid() Expr {
	if p.match(TokenIsVoid) {
		op := p.previous()
		value := p.parseIsVoid()
		return &IsVoidExpr{exprBase: exprBase{Span: joinSpans(op.Span, value.GetSpan())}, Value: value}
	}
	return p.parseNegation()
}

func (p *parser) parseNegation() Expr {
	if p.match(TokenTilde) {
		op := p.previous()
		value := p.parseNegation()
		return &UnaryExpr{exprBase: exprBase{Span: joinSpans(op.Span, value.GetSpan())}, Operator: TokenTilde, OpSpan: op.Span, Value: value}
	}
	return p.parseDispatch()
}

func (p *parser) parseDispatch() Expr {
	expr := p.parsePrimary()
	for {
		if p.match(TokenAt) {
			at := p.previous()
			staticType, ok := p.consume(TokenTypeID, "COL2040", "expected a type after '@'", "Static dispatch uses 'receiver@Type.method(...)'.")
			if !ok {
				return expr
			}
			p.consume(TokenDot, "COL2041", "expected '.' after the static dispatch type", "Static dispatch uses 'receiver@Type.method(...)'.")
			method, methodOK := p.consume(TokenObjectID, "COL2042", "expected a method name after '.'", "Method names begin with a lowercase letter.")
			if !methodOK {
				return expr
			}
			args, end := p.parseArguments()
			expr = &DispatchExpr{
				exprBase: exprBase{Span: joinSpans(expr.GetSpan(), end)}, Receiver: expr,
				StaticTypeName: staticType.Lexeme, Method: method.Lexeme, MethodSpan: method.Span, Arguments: args,
			}
			_ = at
			continue
		}
		if p.match(TokenDot) {
			method, ok := p.consume(TokenObjectID, "COL2043", "expected a method name after '.'", "Method names begin with a lowercase letter.")
			if !ok {
				return expr
			}
			args, end := p.parseArguments()
			expr = &DispatchExpr{
				exprBase: exprBase{Span: joinSpans(expr.GetSpan(), end)}, Receiver: expr,
				Method: method.Lexeme, MethodSpan: method.Span, Arguments: args,
			}
			continue
		}
		break
	}
	return expr
}

func (p *parser) parseArguments() ([]Expr, Span) {
	open, openOK := p.consume(TokenLParen, "COL2044", "expected '(' before the method arguments", "Method dispatch ends with an argument list in parentheses.")
	args := []Expr{}
	if !p.check(TokenRParen) && !p.atEnd() {
		for {
			args = append(args, p.parseExpression())
			if !p.match(TokenComma) {
				break
			}
		}
	}
	close, closeOK := p.consume(TokenRParen, "COL2045", "expected ')' after the method arguments", "Close the argument list with ')'.")
	if closeOK {
		return args, close.Span
	}
	if len(args) > 0 {
		return args, args[len(args)-1].GetSpan()
	}
	if openOK {
		return args, open.Span
	}
	return args, p.peek().Span
}

func (p *parser) parsePrimary() Expr {
	if p.match(TokenIf) {
		return p.parseIf(p.previous())
	}
	if p.match(TokenWhile) {
		return p.parseWhile(p.previous())
	}
	if p.match(TokenLBrace) {
		return p.parseBlock(p.previous())
	}
	if p.match(TokenLet) {
		return p.parseLet(p.previous())
	}
	if p.match(TokenCase) {
		return p.parseCase(p.previous())
	}
	if p.match(TokenNew) {
		start := p.previous()
		typeToken, ok := p.consume(TokenTypeID, "COL2050", "expected a type after 'new'", "Allocate a declared class or SELF_TYPE.")
		if !ok {
			return &NoExpr{exprBase: exprBase{Span: start.Span}}
		}
		return &NewExpr{exprBase: exprBase{Span: joinSpans(start.Span, typeToken.Span)}, TypeName: typeToken.Lexeme, TypeSpan: typeToken.Span}
	}
	if p.match(TokenLParen) {
		open := p.previous()
		expr := p.parseExpression()
		close, ok := p.consume(TokenRParen, "COL2051", "expected ')' after the parenthesized expression", "Close the expression with ')'.")
		if ok {
			// Preserve the grouped range without introducing a separate AST node.
			expr.SetType(expr.GetType())
			switch concrete := expr.(type) {
			case *AssignExpr:
				concrete.Span = joinSpans(open.Span, close.Span)
			case *DispatchExpr:
				concrete.Span = joinSpans(open.Span, close.Span)
			case *IfExpr:
				concrete.Span = joinSpans(open.Span, close.Span)
			case *WhileExpr:
				concrete.Span = joinSpans(open.Span, close.Span)
			case *BlockExpr:
				concrete.Span = joinSpans(open.Span, close.Span)
			case *LetExpr:
				concrete.Span = joinSpans(open.Span, close.Span)
			case *CaseExpr:
				concrete.Span = joinSpans(open.Span, close.Span)
			case *NewExpr:
				concrete.Span = joinSpans(open.Span, close.Span)
			case *IsVoidExpr:
				concrete.Span = joinSpans(open.Span, close.Span)
			case *BinaryExpr:
				concrete.Span = joinSpans(open.Span, close.Span)
			case *UnaryExpr:
				concrete.Span = joinSpans(open.Span, close.Span)
			case *IdentifierExpr:
				concrete.Span = joinSpans(open.Span, close.Span)
			case *IntExpr:
				concrete.Span = joinSpans(open.Span, close.Span)
			case *StringExpr:
				concrete.Span = joinSpans(open.Span, close.Span)
			case *BoolExpr:
				concrete.Span = joinSpans(open.Span, close.Span)
			}
		}
		return expr
	}
	if p.match(TokenInt) {
		tok := p.previous()
		value64, err := strconv.ParseInt(tok.Literal, 10, 32)
		if err != nil {
			p.diagnostics = append(p.diagnostics, errorDiagnostic("parser", "COL2052", "integer constant is outside the signed 32-bit range", "Use a value from 0 through 2147483647; write negative values with '~'.", tok.Span))
			value64 = 0
		}
		return &IntExpr{exprBase: exprBase{Span: tok.Span}, Value: int(value64), Raw: tok.Lexeme}
	}
	if p.match(TokenString) {
		tok := p.previous()
		return &StringExpr{exprBase: exprBase{Span: tok.Span}, Value: tok.Literal}
	}
	if p.match(TokenBool) {
		tok := p.previous()
		return &BoolExpr{exprBase: exprBase{Span: tok.Span}, Value: tok.Literal == "true"}
	}
	if p.match(TokenObjectID) {
		identifier := p.previous()
		if p.check(TokenLParen) {
			args, end := p.parseArguments()
			return &DispatchExpr{
				exprBase: exprBase{Span: joinSpans(identifier.Span, end)},
				Method:   identifier.Lexeme, MethodSpan: identifier.Span, Arguments: args,
			}
		}
		return &IdentifierExpr{exprBase: exprBase{Span: identifier.Span}, Name: identifier.Lexeme}
	}

	unexpected := p.peek()
	p.report(unexpected, "COL2053", "expected a COOL expression", "Use a literal, identifier, dispatch, control expression, block, let, case, new, or parenthesized expression.")
	if !p.atEnd() {
		p.advance()
	}
	return &NoExpr{exprBase: exprBase{Span: unexpected.Span}}
}

func (p *parser) parseIf(start Token) Expr {
	condition := p.parseExpression()
	p.consume(TokenThen, "COL2060", "expected 'then' after the if condition", "COOL conditionals use 'if condition then expression else expression fi'.")
	thenExpr := p.parseExpression()
	p.consume(TokenElse, "COL2061", "expected 'else' after the then expression", "Every COOL conditional has an else branch.")
	elseExpr := p.parseExpression()
	fi, ok := p.consume(TokenFi, "COL2062", "expected 'fi' after the conditional", "Terminate the conditional with 'fi'.")
	end := elseExpr.GetSpan()
	if ok {
		end = fi.Span
	}
	return &IfExpr{exprBase: exprBase{Span: joinSpans(start.Span, end)}, Condition: condition, Then: thenExpr, Else: elseExpr}
}

func (p *parser) parseWhile(start Token) Expr {
	condition := p.parseExpression()
	p.consume(TokenLoop, "COL2070", "expected 'loop' after the while condition", "COOL loops use 'while condition loop expression pool'.")
	body := p.parseExpression()
	pool, ok := p.consume(TokenPool, "COL2071", "expected 'pool' after the loop body", "Terminate the loop with 'pool'.")
	end := body.GetSpan()
	if ok {
		end = pool.Span
	}
	return &WhileExpr{exprBase: exprBase{Span: joinSpans(start.Span, end)}, Condition: condition, Body: body}
}

func (p *parser) parseBlock(start Token) Expr {
	expressions := []Expr{}
	for !p.check(TokenRBrace) && !p.atEnd() {
		expr := p.parseExpression()
		expressions = append(expressions, expr)
		if _, ok := p.consume(TokenSemicolon, "COL2080", "expected ';' after an expression in the block", "Every expression inside a COOL block ends with ';'."); !ok {
			if p.check(TokenRBrace) {
				break
			}
		}
	}
	close, ok := p.consume(TokenRBrace, "COL2081", "expected '}' after the block", "Close the block with '}'.")
	end := start.Span
	if len(expressions) > 0 {
		end = expressions[len(expressions)-1].GetSpan()
	}
	if ok {
		end = close.Span
	}
	if len(expressions) == 0 {
		p.diagnostics = append(p.diagnostics, errorDiagnostic("parser", "COL2082", "a COOL block must contain at least one expression", "Add an expression followed by ';' inside the block.", joinSpans(start.Span, end)))
	}
	return &BlockExpr{exprBase: exprBase{Span: joinSpans(start.Span, end)}, Expressions: expressions}
}

func (p *parser) parseLet(start Token) Expr {
	bindings := []LetBinding{}
	for {
		name, nameOK := p.consume(TokenObjectID, "COL2090", "expected a binding name after 'let'", "Let bindings use 'name : Type' with an optional initializer.")
		if !nameOK {
			break
		}
		p.consume(TokenColon, "COL2091", "expected ':' after the let binding name", "Write the binding as 'name : Type'.")
		typeToken, typeOK := p.consume(TokenTypeID, "COL2092", "expected a type in the let binding", "Use a declared type name or SELF_TYPE.")
		if !typeOK {
			break
		}
		var init Expr
		end := typeToken.Span
		if p.match(TokenAssign) {
			init = p.parseExpression()
			end = init.GetSpan()
		}
		bindings = append(bindings, LetBinding{
			Name: name.Lexeme, TypeName: typeToken.Lexeme, Init: init,
			NameSpan: name.Span, TypeSpan: typeToken.Span, Span: joinSpans(name.Span, end),
		})
		if !p.match(TokenComma) {
			break
		}
	}
	p.consume(TokenIn, "COL2093", "expected 'in' after the let bindings", "Finish the declarations with 'in' before the body expression.")
	body := p.parseExpression()
	return &LetExpr{exprBase: exprBase{Span: joinSpans(start.Span, body.GetSpan())}, Bindings: bindings, Body: body}
}

func (p *parser) parseCase(start Token) Expr {
	value := p.parseExpression()
	p.consume(TokenOf, "COL2100", "expected 'of' after the case expression", "COOL case expressions use 'case expression of branches esac'.")
	branches := []CaseBranch{}
	for !p.check(TokenEsac) && !p.atEnd() {
		name, nameOK := p.consume(TokenObjectID, "COL2101", "expected a case branch variable", "Case branches use 'name : Type => expression;'.")
		if !nameOK {
			p.synchronizeCaseBranch()
			continue
		}
		p.consume(TokenColon, "COL2102", "expected ':' after the case branch variable", "Write the branch as 'name : Type => expression;'.")
		typeToken, typeOK := p.consume(TokenTypeID, "COL2103", "expected a type in the case branch", "Use a declared non-SELF_TYPE class name.")
		if !typeOK {
			p.synchronizeCaseBranch()
			continue
		}
		p.consume(TokenCaseArrow, "COL2104", "expected '=>' in the case branch", "Separate the branch type from its expression with '=>'.")
		body := p.parseExpression()
		semi, semiOK := p.consume(TokenSemicolon, "COL2105", "expected ';' after the case branch", "Each case branch ends with ';'.")
		end := body.GetSpan()
		if semiOK {
			end = semi.Span
		}
		branches = append(branches, CaseBranch{
			Name: name.Lexeme, TypeName: typeToken.Lexeme, Body: body,
			NameSpan: name.Span, TypeSpan: typeToken.Span, Span: joinSpans(name.Span, end),
		})
	}
	esac, ok := p.consume(TokenEsac, "COL2106", "expected 'esac' after the case branches", "Terminate the case expression with 'esac'.")
	end := value.GetSpan()
	if len(branches) > 0 {
		end = branches[len(branches)-1].Span
	}
	if ok {
		end = esac.Span
	}
	if len(branches) == 0 {
		p.diagnostics = append(p.diagnostics, errorDiagnostic("parser", "COL2107", "a case expression must contain at least one branch", "Add a branch such as 'x : Object => x;'.", joinSpans(start.Span, end)))
	}
	return &CaseExpr{exprBase: exprBase{Span: joinSpans(start.Span, end)}, Value: value, Branches: branches}
}

func (p *parser) consume(t TokenType, code, message, hint string) (Token, bool) {
	if p.check(t) {
		return p.advance(), true
	}
	p.report(p.peek(), code, message, hint)
	return Token{Type: t, Span: p.peek().Span, Line: p.peek().Line, Column: p.peek().Column}, false
}

func (p *parser) report(token Token, code, message, hint string) {
	actual := tokenDisplayName(token.Type)
	if token.Type != TokenEOF && token.Lexeme != "" {
		message = fmt.Sprintf("%s; found %q", message, token.Lexeme)
	} else if token.Type == TokenEOF {
		message += "; reached end of file"
	} else {
		message = fmt.Sprintf("%s; found %s", message, actual)
	}
	p.diagnostics = append(p.diagnostics, errorDiagnostic("parser", code, message, hint, token.Span))
}

func (p *parser) synchronizeToClass() {
	for !p.atEnd() {
		if p.check(TokenClass) {
			return
		}
		p.advance()
	}
}

func (p *parser) synchronizeFeature() {
	for !p.atEnd() {
		if p.previous().Type == TokenSemicolon {
			return
		}
		if p.check(TokenRBrace) || p.check(TokenObjectID) {
			return
		}
		p.advance()
	}
}

func (p *parser) synchronizeCaseBranch() {
	for !p.atEnd() {
		if p.match(TokenSemicolon) || p.check(TokenEsac) {
			return
		}
		p.advance()
	}
}

func (p *parser) match(types ...TokenType) bool {
	for _, t := range types {
		if p.check(t) {
			p.advance()
			return true
		}
	}
	return false
}

func (p *parser) check(t TokenType) bool {
	if p.atEnd() {
		return t == TokenEOF
	}
	return p.peek().Type == t
}

func (p *parser) advance() Token {
	if !p.atEnd() {
		p.current++
	}
	return p.previous()
}

func (p *parser) atEnd() bool {
	return p.peek().Type == TokenEOF
}

func (p *parser) peek() Token {
	if len(p.tokens) == 0 {
		return Token{Type: TokenEOF, Span: emptySpan(), Line: 1, Column: 1}
	}
	if p.current >= len(p.tokens) {
		return p.tokens[len(p.tokens)-1]
	}
	return p.tokens[p.current]
}

func (p *parser) previous() Token {
	if p.current == 0 || len(p.tokens) == 0 {
		return p.peek()
	}
	return p.tokens[p.current-1]
}
