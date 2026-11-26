// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"fmt"
	"strings"
)

// referenceProgram is a program parsed the way the course pipeline
// `lexer files... | parser` parses it: all units' course token streams are
// concatenated and parsed by the LALR grammar in compatibility mode, which
// reproduces Bison's error detection, error recovery, and node line numbers.
type referenceProgram struct {
	program *Program
	lines   map[Expr]int
	files   map[string]string
	errors  []LALRSyntaxError
}

func parseReference(units []SourceUnit) referenceProgram {
	return ReferencePipeline{}.parse(units)
}

func (p ReferencePipeline) parse(units []SourceUnit) referenceProgram {
	files := map[string]string{}
	tokens := []Token{}
	for _, unit := range units {
		files[unit.ID] = unit.Name
		for _, token := range p.lex(unit.ID, unit.Content) {
			if token.Type != TokenEOF {
				tokens = append(tokens, token)
			}
		}
	}
	program, _, errors, lines := parseLALRWithLines(tokens, true)
	return referenceProgram{program: program, lines: lines, files: files, errors: errors}
}

// syntaxErrorText renders the course parser's stderr for a failed parse.
func (r referenceProgram) syntaxErrorText() string {
	var out strings.Builder
	for index, failure := range r.errors {
		if index >= maxLALRDiagnostics {
			out.WriteString("More than 50 errors\n")
			return out.String()
		}
		line := referenceLine(failure.Location)
		if line == 0 {
			line = 1
		}
		fmt.Fprintf(&out, "\"%s\", line %d: syntax error at or near %s\n",
			referenceEscape(r.files[failure.Location.Span.SourceID]), line, referenceParserTokenText(failure.Token))
	}
	out.WriteString("Compilation halted due to lex and parse errors\n")
	return out.String()
}

// ReferenceParserDump renders the course `parser` output for the units: the
// untyped AST dump on success, or the syntax-error report (ok=false), which
// the course parser writes to stderr before exiting with status 1.
func ReferenceParserDump(units []SourceUnit) (string, bool) {
	return ReferencePipeline{}.Parser(units)
}

// Parser renders the course parser output (see ReferenceParserDump).
func (p ReferencePipeline) Parser(units []SourceUnit) (string, bool) {
	parsed := p.parse(units)
	if len(parsed.errors) > 0 || len(parsed.program.Classes) == 0 {
		if len(parsed.errors) == 0 {
			parsed.errors = append(parsed.errors, LALRSyntaxError{Token: Token{Type: TokenEOF}})
		}
		return parsed.syntaxErrorText(), false
	}
	return parsed.dump(nil), true
}

type referenceDumper struct {
	out   strings.Builder
	lines map[Expr]int
	files map[string]string
	types map[Expr]string
}

func (r referenceProgram) dump(types map[Expr]string) string {
	d := &referenceDumper{lines: r.lines, files: r.files, types: types}
	program := r.program
	line := 1
	if len(program.Classes) > 0 {
		line = program.Classes[0].Span.StartLine
	}
	d.line(0, line)
	d.text(0, "_program")
	for _, class := range program.Classes {
		d.class(2, class)
	}
	return d.out.String()
}

func pad(n int) string { return strings.Repeat(" ", n) }

func (d *referenceDumper) line(n, line int) { fmt.Fprintf(&d.out, "%s#%d\n", pad(n), line) }
func (d *referenceDumper) text(n int, value string) {
	d.out.WriteString(pad(n))
	d.out.WriteString(value)
	d.out.WriteByte('\n')
}

func (d *referenceDumper) class(n int, class *ClassDecl) {
	d.line(n, class.Span.StartLine)
	d.text(n, "_class")
	d.text(n+2, class.Name)
	d.text(n+2, class.Parent)
	d.text(n+2, "\""+referenceEscape(d.files[class.Span.SourceID])+"\"")
	d.text(n+2, "(")
	for _, feature := range class.Features {
		switch f := feature.(type) {
		case *MethodDecl:
			d.line(n+2, f.NameSpan.StartLine)
			d.text(n+2, "_method")
			d.text(n+4, f.Name)
			for _, formal := range f.Formals {
				d.line(n+4, formal.NameSpan.StartLine)
				d.text(n+4, "_formal")
				d.text(n+6, formal.Name)
				d.text(n+6, formal.TypeName)
			}
			d.text(n+4, f.ReturnType)
			d.expr(n+4, f.Body)
		case *AttributeDecl:
			d.line(n+2, f.NameSpan.StartLine)
			d.text(n+2, "_attr")
			d.text(n+4, f.Name)
			d.text(n+4, f.TypeName)
			d.optional(n+4, f.Init, f.NameSpan.StartLine)
		}
	}
	d.text(n+2, ")")
}

// optional prints an initializer or the no_expr placeholder, which the course
// parser creates with the enclosing construct's line.
func (d *referenceDumper) optional(n int, expr Expr, line int) {
	if expr == nil {
		d.line(n, line)
		d.text(n, "_no_expr")
		d.text(n, ": _no_type")
		return
	}
	d.expr(n, expr)
}

func (d *referenceDumper) typeOf(n int, expr Expr) {
	typeName := "_no_type"
	if d.types != nil {
		if value, ok := d.types[expr]; ok && value != "" {
			typeName = value
		}
	}
	d.text(n, ": "+typeName)
}

func (d *referenceDumper) exprLine(expr Expr) int {
	if line, ok := d.lines[expr]; ok {
		return line
	}
	return expr.GetSpan().StartLine
}

func (d *referenceDumper) expr(n int, expr Expr) {
	line := d.exprLine(expr)
	switch e := expr.(type) {
	case *LetExpr:
		d.let(n, e, 0, line)
		return
	case *NoExpr:
		d.line(n, line)
		d.text(n, "_no_expr")
		d.text(n, ": _no_type")
		return
	}
	d.line(n, line)
	switch e := expr.(type) {
	case *AssignExpr:
		d.text(n, "_assign")
		d.text(n+2, e.Name)
		d.expr(n+2, e.Value)
	case *DispatchExpr:
		if e.StaticTypeName != "" {
			d.text(n, "_static_dispatch")
			d.expr(n+2, e.Receiver)
			d.text(n+2, e.StaticTypeName)
		} else {
			d.text(n, "_dispatch")
			if e.Receiver == nil {
				d.line(n+2, line)
				d.text(n+2, "_object")
				d.text(n+4, "self")
				if d.types != nil {
					d.text(n+2, ": SELF_TYPE")
				} else {
					d.text(n+2, ": _no_type")
				}
			} else {
				d.expr(n+2, e.Receiver)
			}
		}
		d.text(n+2, e.Method)
		d.text(n+2, "(")
		for _, argument := range e.Arguments {
			d.expr(n+2, argument)
		}
		d.text(n+2, ")")
	case *IfExpr:
		d.text(n, "_cond")
		d.expr(n+2, e.Condition)
		d.expr(n+2, e.Then)
		d.expr(n+2, e.Else)
	case *WhileExpr:
		d.text(n, "_loop")
		d.expr(n+2, e.Condition)
		d.expr(n+2, e.Body)
	case *BlockExpr:
		d.text(n, "_block")
		for _, child := range e.Expressions {
			d.expr(n+2, child)
		}
	case *CaseExpr:
		d.text(n, "_typcase")
		d.expr(n+2, e.Value)
		for _, branch := range e.Branches {
			d.line(n+2, branch.NameSpan.StartLine)
			d.text(n+2, "_branch")
			d.text(n+4, branch.Name)
			d.text(n+4, branch.TypeName)
			d.expr(n+4, branch.Body)
		}
	case *NewExpr:
		d.text(n, "_new")
		d.text(n+2, e.TypeName)
	case *IsVoidExpr:
		d.text(n, "_isvoid")
		d.expr(n+2, e.Value)
	case *BinaryExpr:
		d.text(n, map[TokenType]string{
			TokenPlus: "_plus", TokenMinus: "_sub", TokenStar: "_mul", TokenSlash: "_divide",
			TokenLess: "_lt", TokenLessEqual: "_leq", TokenEqual: "_eq",
		}[e.Operator])
		d.expr(n+2, e.Left)
		d.expr(n+2, e.Right)
	case *UnaryExpr:
		if e.Operator == TokenTilde {
			d.text(n, "_neg")
		} else {
			d.text(n, "_comp")
		}
		d.expr(n+2, e.Value)
	case *IdentifierExpr:
		d.text(n, "_object")
		d.text(n+2, e.Name)
	case *IntExpr:
		d.text(n, "_int")
		d.text(n+2, e.Raw)
	case *StringExpr:
		d.text(n, "_string")
		d.text(n+2, "\""+referenceEscape(e.Value)+"\"")
	case *BoolExpr:
		d.text(n, "_bool")
		if e.Value {
			d.text(n+2, "1")
		} else {
			d.text(n+2, "0")
		}
	}
	d.typeOf(n, expr)
}

// let prints a multi-binding let as the course AST's nested single-binding
// lets. The first level has the let's line; later levels have the line of
// their binding's identifier.
func (d *referenceDumper) let(n int, e *LetExpr, index, line int) {
	binding := e.Bindings[index]
	d.line(n, line)
	d.text(n, "_let")
	d.text(n+2, binding.Name)
	d.text(n+2, binding.TypeName)
	d.optional(n+2, binding.Init, line)
	if index+1 < len(e.Bindings) {
		d.let(n+2, e, index+1, e.Bindings[index+1].NameSpan.StartLine)
	} else {
		d.expr(n+2, e.Body)
	}
	d.typeOf(n, e)
}
