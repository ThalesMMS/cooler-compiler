// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"fmt"
	"strconv"
	"strings"
)

type Program struct {
	Classes []*ClassDecl
	Span    Span
}

type ClassDecl struct {
	Name       string
	Parent     string
	Features   []Feature
	Span       Span
	NameSpan   Span
	ParentSpan Span
}

type Feature interface {
	featureNode()
	GetSpan() Span
	FeatureName() string
	ToAST() ASTNode
}

type AttributeDecl struct {
	Name     string
	TypeName string
	Init     Expr
	Span     Span
	NameSpan Span
	TypeSpan Span
}

func (*AttributeDecl) featureNode()          {}
func (a *AttributeDecl) GetSpan() Span       { return a.Span }
func (a *AttributeDecl) FeatureName() string { return a.Name }
func (a *AttributeDecl) ToAST() ASTNode {
	children := []ASTNode{}
	if a.Init != nil {
		children = append(children, a.Init.ToAST())
	}
	return ASTNode{
		Kind: "Attribute", Name: a.Name, Type: a.TypeName,
		Text: a.Name + " : " + a.TypeName, Children: children,
		Line: a.Span.StartLine, Column: a.Span.StartColumn, Span: a.Span,
	}
}

type MethodDecl struct {
	Name       string
	Formals    []Formal
	ReturnType string
	Body       Expr
	Span       Span
	NameSpan   Span
	TypeSpan   Span
}

func (*MethodDecl) featureNode()          {}
func (m *MethodDecl) GetSpan() Span       { return m.Span }
func (m *MethodDecl) FeatureName() string { return m.Name }
func (m *MethodDecl) ToAST() ASTNode {
	children := make([]ASTNode, 0, len(m.Formals)+1)
	for _, f := range m.Formals {
		children = append(children, ASTNode{
			Kind: "Formal", Name: f.Name, Type: f.TypeName,
			Text: f.Name + " : " + f.TypeName,
			Line: f.Span.StartLine, Column: f.Span.StartColumn, Span: f.Span,
		})
	}
	if m.Body != nil {
		children = append(children, m.Body.ToAST())
	}
	return ASTNode{
		Kind: "Method", Name: m.Name, Type: m.ReturnType,
		Text: m.Name + "(...) : " + m.ReturnType, Children: children,
		Line: m.Span.StartLine, Column: m.Span.StartColumn, Span: m.Span,
	}
}

type Formal struct {
	Name     string
	TypeName string
	Span     Span
	NameSpan Span
	TypeSpan Span
}

type Expr interface {
	exprNode()
	GetSpan() Span
	GetType() string
	SetType(string)
	ToAST() ASTNode
}

type exprBase struct {
	Span       Span
	StaticType string
}

func (e *exprBase) GetSpan() Span    { return e.Span }
func (e *exprBase) GetType() string  { return e.StaticType }
func (e *exprBase) SetType(t string) { e.StaticType = t }

func astExpr(kind, text string, base *exprBase, children ...ASTNode) ASTNode {
	return ASTNode{
		Kind: kind, Text: text, Type: base.StaticType, Children: children,
		Line: base.Span.StartLine, Column: base.Span.StartColumn, Span: base.Span,
	}
}

type AssignExpr struct {
	exprBase
	Name     string
	NameSpan Span
	Value    Expr
}

func (*AssignExpr) exprNode() {}
func (e *AssignExpr) ToAST() ASTNode {
	return astExpr("Assignment", e.Name+" <-", &e.exprBase, e.Value.ToAST())
}

type DispatchExpr struct {
	exprBase
	Receiver       Expr
	StaticTypeName string
	StaticTypeSpan Span
	Method         string
	MethodSpan     Span
	Arguments      []Expr
}

func (*DispatchExpr) exprNode() {}
func (e *DispatchExpr) ToAST() ASTNode {
	children := make([]ASTNode, 0, len(e.Arguments)+1)
	if e.Receiver != nil {
		children = append(children, e.Receiver.ToAST())
	}
	for _, arg := range e.Arguments {
		children = append(children, arg.ToAST())
	}
	text := e.Method + "(...)"
	if e.StaticTypeName != "" {
		text = "@" + e.StaticTypeName + "." + text
	}
	return astExpr("Dispatch", text, &e.exprBase, children...)
}

type IfExpr struct {
	exprBase
	Condition Expr
	Then      Expr
	Else      Expr
}

func (*IfExpr) exprNode() {}
func (e *IfExpr) ToAST() ASTNode {
	return astExpr("If", "if … then … else … fi", &e.exprBase, e.Condition.ToAST(), e.Then.ToAST(), e.Else.ToAST())
}

type WhileExpr struct {
	exprBase
	Condition Expr
	Body      Expr
}

func (*WhileExpr) exprNode() {}
func (e *WhileExpr) ToAST() ASTNode {
	return astExpr("While", "while … loop … pool", &e.exprBase, e.Condition.ToAST(), e.Body.ToAST())
}

type BlockExpr struct {
	exprBase
	Expressions []Expr
}

func (*BlockExpr) exprNode() {}
func (e *BlockExpr) ToAST() ASTNode {
	children := make([]ASTNode, 0, len(e.Expressions))
	for _, child := range e.Expressions {
		children = append(children, child.ToAST())
	}
	return astExpr("Block", "{ … }", &e.exprBase, children...)
}

type LetBinding struct {
	Name     string
	TypeName string
	Init     Expr
	Span     Span
	NameSpan Span
	TypeSpan Span
}

type LetExpr struct {
	exprBase
	Bindings []LetBinding
	Body     Expr
}

func (*LetExpr) exprNode() {}
func (e *LetExpr) ToAST() ASTNode {
	children := make([]ASTNode, 0, len(e.Bindings)+1)
	for _, binding := range e.Bindings {
		bindingChildren := []ASTNode{}
		if binding.Init != nil {
			bindingChildren = append(bindingChildren, binding.Init.ToAST())
		}
		children = append(children, ASTNode{
			Kind: "LetBinding", Name: binding.Name, Type: binding.TypeName,
			Text:     binding.Name + " : " + binding.TypeName,
			Children: bindingChildren, Line: binding.Span.StartLine,
			Column: binding.Span.StartColumn, Span: binding.Span,
		})
	}
	children = append(children, e.Body.ToAST())
	return astExpr("Let", "let … in …", &e.exprBase, children...)
}

type CaseBranch struct {
	Name     string
	TypeName string
	Body     Expr
	Span     Span
	NameSpan Span
	TypeSpan Span
}

type CaseExpr struct {
	exprBase
	Value    Expr
	Branches []CaseBranch
}

func (*CaseExpr) exprNode() {}
func (e *CaseExpr) ToAST() ASTNode {
	children := []ASTNode{e.Value.ToAST()}
	for _, branch := range e.Branches {
		children = append(children, ASTNode{
			Kind: "CaseBranch", Name: branch.Name, Type: branch.TypeName,
			Text:     branch.Name + " : " + branch.TypeName,
			Children: []ASTNode{branch.Body.ToAST()},
			Line:     branch.Span.StartLine, Column: branch.Span.StartColumn, Span: branch.Span,
		})
	}
	return astExpr("Case", "case … of … esac", &e.exprBase, children...)
}

type NewExpr struct {
	exprBase
	TypeName string
	TypeSpan Span
}

func (*NewExpr) exprNode()        {}
func (e *NewExpr) ToAST() ASTNode { return astExpr("New", "new "+e.TypeName, &e.exprBase) }

type IsVoidExpr struct {
	exprBase
	Value Expr
}

func (*IsVoidExpr) exprNode() {}
func (e *IsVoidExpr) ToAST() ASTNode {
	return astExpr("IsVoid", "isvoid", &e.exprBase, e.Value.ToAST())
}

type BinaryExpr struct {
	exprBase
	Left     Expr
	Operator TokenType
	OpSpan   Span
	Right    Expr
}

func (*BinaryExpr) exprNode() {}
func (e *BinaryExpr) ToAST() ASTNode {
	return astExpr("Binary", string(e.Operator), &e.exprBase, e.Left.ToAST(), e.Right.ToAST())
}

type UnaryExpr struct {
	exprBase
	Operator TokenType
	OpSpan   Span
	Value    Expr
}

func (*UnaryExpr) exprNode() {}
func (e *UnaryExpr) ToAST() ASTNode {
	return astExpr("Unary", string(e.Operator), &e.exprBase, e.Value.ToAST())
}

type IdentifierExpr struct {
	exprBase
	Name     string
	NameSpan Span
}

func (*IdentifierExpr) exprNode() {}
func (e *IdentifierExpr) ToAST() ASTNode {
	node := astExpr("Identifier", e.Name, &e.exprBase)
	node.Name = e.Name
	return node
}

type IntExpr struct {
	exprBase
	Value coolInt
	Raw   string
}

func (*IntExpr) exprNode() {}
func (e *IntExpr) ToAST() ASTNode {
	node := astExpr("Int", e.Raw, &e.exprBase)
	node.Value = e.Value.String()
	return node
}

type StringExpr struct {
	exprBase
	Value string
}

func (*StringExpr) exprNode() {}
func (e *StringExpr) ToAST() ASTNode {
	node := astExpr("String", strconv.Quote(e.Value), &e.exprBase)
	node.Value = e.Value
	return node
}

type BoolExpr struct {
	exprBase
	Value bool
}

func (*BoolExpr) exprNode() {}
func (e *BoolExpr) ToAST() ASTNode {
	value := strconv.FormatBool(e.Value)
	node := astExpr("Bool", value, &e.exprBase)
	node.Value = value
	return node
}

type NoExpr struct{ exprBase }

func (*NoExpr) exprNode()        {}
func (e *NoExpr) ToAST() ASTNode { return astExpr("NoExpr", "<no expression>", &e.exprBase) }

func (p *Program) ToAST() *ASTNode {
	if p == nil {
		return nil
	}
	children := make([]ASTNode, 0, len(p.Classes))
	for _, class := range p.Classes {
		featureNodes := make([]ASTNode, 0, len(class.Features))
		for _, feature := range class.Features {
			featureNodes = append(featureNodes, feature.ToAST())
		}
		text := "class " + class.Name
		if class.Parent != "" && class.Parent != "Object" {
			text += " inherits " + class.Parent
		}
		children = append(children, ASTNode{
			Kind: "Class", Name: class.Name, Type: class.Parent, Text: text,
			Children: featureNodes, Line: class.Span.StartLine,
			Column: class.Span.StartColumn, Span: class.Span,
		})
	}
	return &ASTNode{
		Kind: "Program", Text: summarizeCount(len(p.Classes), "class", "classes"),
		Children: children, Line: p.Span.StartLine, Column: p.Span.StartColumn, Span: p.Span,
	}
}

func expressionLabel(expr Expr) string {
	if expr == nil {
		return "<nil>"
	}
	switch e := expr.(type) {
	case *IdentifierExpr:
		return e.Name
	case *IntExpr:
		return e.Raw
	case *StringExpr:
		return strconv.Quote(e.Value)
	case *BoolExpr:
		return strconv.FormatBool(e.Value)
	case *DispatchExpr:
		return e.Method + "()"
	case *NewExpr:
		return "new " + e.TypeName
	default:
		return strings.TrimSpace(fmt.Sprintf("%T", expr))
	}
}
