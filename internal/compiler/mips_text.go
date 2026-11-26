// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import "fmt"

// Calling convention and register invariants (docs/mips-backend.md):
//
//   - $a0 is the accumulator: every expression leaves its value there; on
//     method entry it holds self, and a method returns its value in $a0.
//   - $s0 holds self for the whole method body; it is callee-saved.
//   - $fp is the frame pointer; $ra the return address; both callee-saved.
//   - $t1, $t2, and $a1 are scratch; nothing is kept in them across a call.
//   - Arguments are pushed by the caller, first argument first, then the
//     receiver is evaluated into $a0; the callee pops its arguments.
//   - Temporaries and let/case variables live on the stack, so the garbage
//     collector sees every live object pointer (stack words and $s0/$a0).
//
// Frame layout after the prologue ($sp points to the next free word):
//
//	fp+12+4*(n-1-i)  argument i of n
//	fp+8             saved $fp
//	fp+4             saved $s0
//	fp+0             saved $ra
//	fp-4, fp-8, ...  temporaries and let/case variables
const mipsFrameWords = 3

type mipsContext struct {
	class  *mipsClass
	file   string
	depth  int
	scopes []map[string]int
	args   int
}

func (c *mipsContext) lookup(name string) (int, bool) {
	for index := len(c.scopes) - 1; index >= 0; index-- {
		if offset, ok := c.scopes[index][name]; ok {
			return offset, true
		}
	}
	return 0, false
}

func (g *mipsGenerator) ins(format string, args ...any) {
	fmt.Fprintf(&g.emit.out, "\t%s\n", fmt.Sprintf(format, args...))
}

func (g *mipsGenerator) push(ctx *mipsContext, register string) {
	g.ins("sw\t%s 0($sp)", register)
	g.ins("addiu\t$sp $sp -4")
	ctx.depth++
}

func (g *mipsGenerator) pop(ctx *mipsContext, register string) {
	g.ins("lw\t%s 4($sp)", register)
	g.ins("addiu\t$sp $sp 4")
	ctx.depth--
}

func (g *mipsGenerator) emitText() {
	g.emit.directive(".text")
	for _, name := range []string{"Main_init", "Int_init", "String_init", "Bool_init", "Main.main"} {
		g.emit.directive(".globl", name)
	}
	for _, class := range g.layout.classes {
		g.emitInit(class)
	}
	for _, class := range g.layout.classes {
		if class.info.Decl == nil {
			continue
		}
		for _, feature := range class.info.Decl.Features {
			if method, ok := feature.(*MethodDecl); ok && class.info.Methods[method.Name] != nil && class.info.Methods[method.Name].Decl == method {
				g.emitMethod(class, method)
			}
		}
	}
}

func (g *mipsGenerator) prologue() {
	g.ins("addiu\t$sp $sp -12")
	g.ins("sw\t$fp 12($sp)")
	g.ins("sw\t$s0 8($sp)")
	g.ins("sw\t$ra 4($sp)")
	g.ins("addiu\t$fp $sp 4")
	g.ins("move\t$s0 $a0")
}

func (g *mipsGenerator) epilogue(args int) {
	g.ins("lw\t$fp 12($sp)")
	g.ins("lw\t$s0 8($sp)")
	g.ins("lw\t$ra 4($sp)")
	g.ins("addiu\t$sp $sp %d", (mipsFrameWords+args)*mipsWord)
	g.ins("jr\t$ra")
}

// emitInit writes Class_init: it runs the parent's init with self in $a0,
// then evaluates this class's attribute initializers in declaration order
// with self in $s0, and returns self. Attributes without an initializer keep
// the prototype's default (0, "", false, or void).
func (g *mipsGenerator) emitInit(class *mipsClass) {
	g.emit.label(class.name + "_init")
	g.prologue()
	if class.parent != nil {
		g.ins("jal\t%s_init", class.parent.name)
	}
	ctx := &mipsContext{class: class, file: g.fileOf(class), scopes: []map[string]int{{}}}
	for _, name := range class.ownAttrs {
		decl := class.attrDecls[name]
		if decl.Init == nil {
			continue
		}
		g.expr(ctx, decl.Init)
		attr, _ := class.attribute(name)
		g.storeAttribute(attr.Offset)
	}
	g.ins("move\t$a0 $s0")
	g.epilogue(0)
}

func (g *mipsGenerator) fileOf(class *mipsClass) string {
	if class.info.Decl == nil {
		return ""
	}
	return g.layout.files[class.info.Decl.Span.SourceID]
}

func (g *mipsGenerator) emitMethod(class *mipsClass, method *MethodDecl) {
	g.emit.label(class.name + "." + method.Name)
	g.prologue()
	ctx := &mipsContext{class: class, file: g.fileOf(class), scopes: []map[string]int{{}}, args: len(method.Formals)}
	for index, formal := range method.Formals {
		ctx.scopes[0][formal.Name] = mipsWord*(mipsFrameWords) + mipsWord*(len(method.Formals)-1-index)
	}
	g.expr(ctx, method.Body)
	g.epilogue(len(method.Formals))
}

// storeAttribute stores $a0 into self's attribute at offset, with the
// collector hooks: -T checks the stored object's header, and -g records the
// assignment for the generational collector (then reloads $a0).
func (g *mipsGenerator) storeAttribute(offset int) {
	if g.options.GC.Debug {
		g.ins("move\t$a1 $a0")
		g.ins("jal\t_gc_check")
	}
	g.ins("sw\t$a0 %d($s0)", offset)
	if g.options.GC.Generational {
		g.ins("addiu\t$a1 $s0 %d", offset)
		g.ins("jal\t_GenGC_Assign")
		g.ins("lw\t$a0 %d($s0)", offset)
	}
}

func (g *mipsGenerator) staticClass(ctx *mipsContext, typeName string) *mipsClass {
	if typeName == typeSelf || typeName == "" {
		return ctx.class
	}
	if class := g.layout.byName[typeName]; class != nil {
		return class
	}
	return ctx.class
}

func (g *mipsGenerator) loadDefault(typeName string) {
	switch typeName {
	case typeInt, typeString, typeBool:
		g.ins("la\t$a0 %s", g.defaultAttributeValue(typeName))
	default:
		g.ins("move\t$a0 $zero")
	}
}

// expr leaves the value of e in $a0.
func (g *mipsGenerator) expr(ctx *mipsContext, e Expr) {
	switch e := e.(type) {
	case *IntExpr:
		g.ins("la\t$a0 %s", g.intConstant(int32(e.Value)))
	case *StringExpr:
		g.ins("la\t$a0 %s", g.stringConstant(e.Value))
	case *BoolExpr:
		if e.Value {
			g.ins("la\t$a0 bool_const1")
		} else {
			g.ins("la\t$a0 bool_const0")
		}
	case *NoExpr:
		g.ins("move\t$a0 $zero")
	case *IdentifierExpr:
		g.identifier(ctx, e.Name)
	case *AssignExpr:
		g.expr(ctx, e.Value)
		if offset, ok := ctx.lookup(e.Name); ok {
			g.ins("sw\t$a0 %d($fp)", offset)
		} else if attr, ok := ctx.class.attribute(e.Name); ok {
			g.storeAttribute(attr.Offset)
		}
	case *DispatchExpr:
		g.dispatch(ctx, e)
	case *IfExpr:
		elseLabel, endLabel := g.newLabel(), g.newLabel()
		g.expr(ctx, e.Condition)
		g.ins("lw\t$t1 12($a0)")
		g.ins("beqz\t$t1 %s", elseLabel)
		g.expr(ctx, e.Then)
		g.ins("b\t%s", endLabel)
		g.emit.label(elseLabel)
		g.expr(ctx, e.Else)
		g.emit.label(endLabel)
	case *WhileExpr:
		loopLabel, endLabel := g.newLabel(), g.newLabel()
		g.emit.label(loopLabel)
		g.expr(ctx, e.Condition)
		g.ins("lw\t$t1 12($a0)")
		g.ins("beqz\t$t1 %s", endLabel)
		g.expr(ctx, e.Body)
		g.ins("b\t%s", loopLabel)
		g.emit.label(endLabel)
		g.ins("move\t$a0 $zero")
	case *BlockExpr:
		for _, child := range e.Expressions {
			g.expr(ctx, child)
		}
	case *LetExpr:
		g.let(ctx, e, 0)
	case *CaseExpr:
		g.caseExpr(ctx, e)
	case *NewExpr:
		g.newObject(ctx, e.TypeName)
	case *IsVoidExpr:
		g.expr(ctx, e.Value)
		g.boolFromTest("beqz\t$t1", true)
	case *UnaryExpr:
		g.expr(ctx, e.Value)
		if e.Operator == TokenTilde {
			g.ins("jal\tObject.copy")
			g.ins("lw\t$t1 12($a0)")
			g.ins("neg\t$t1 $t1")
			g.ins("sw\t$t1 12($a0)")
		} else {
			g.ins("lw\t$t1 12($a0)")
			g.boolFromTest("beqz\t$t1", false)
		}
	case *BinaryExpr:
		g.binary(ctx, e)
	default:
		g.ins("move\t$a0 $zero")
	}
}

// boolFromTest turns a test on the value in $a0 into bool_const1/0. For
// isvoid the tested register is $a0 itself; for not it is the Bool's value
// already loaded into $t1.
func (g *mipsGenerator) boolFromTest(branch string, isvoid bool) {
	done := g.newLabel()
	if isvoid {
		g.ins("move\t$t1 $a0")
	}
	g.ins("la\t$a0 bool_const1")
	g.ins("%s %s", branch, done)
	g.ins("la\t$a0 bool_const0")
	g.emit.label(done)
}

func (g *mipsGenerator) identifier(ctx *mipsContext, name string) {
	if name == "self" {
		g.ins("move\t$a0 $s0")
		return
	}
	if offset, ok := ctx.lookup(name); ok {
		g.ins("lw\t$a0 %d($fp)", offset)
		return
	}
	if attr, ok := ctx.class.attribute(name); ok {
		g.ins("lw\t$a0 %d($s0)", attr.Offset)
		return
	}
	g.ins("move\t$a0 $zero")
}

// newObject copies the prototype and runs the init method. For SELF_TYPE
// both come from class_objTab, indexed by self's tag.
func (g *mipsGenerator) newObject(ctx *mipsContext, typeName string) {
	if typeName != typeSelf {
		g.ins("la\t$a0 %s_protObj", typeName)
		g.ins("jal\tObject.copy")
		g.ins("jal\t%s_init", typeName)
		return
	}
	g.ins("la\t$t1 class_objTab")
	g.ins("lw\t$t2 0($s0)")
	g.ins("sll\t$t2 $t2 3")
	g.ins("addu\t$t1 $t1 $t2")
	g.push(ctx, "$t1")
	g.ins("lw\t$a0 0($t1)")
	g.ins("jal\tObject.copy")
	g.pop(ctx, "$t1")
	g.ins("lw\t$t1 4($t1)")
	g.ins("jalr\t$t1")
}

// dispatch pushes the arguments, evaluates the receiver, aborts through
// _dispatch_abort (file name in $a0, line in $t1) when it is void, and calls
// the method through the receiver's dispatch table, or through the static
// type's table for e@T.f(...).
func (g *mipsGenerator) dispatch(ctx *mipsContext, e *DispatchExpr) {
	for _, argument := range e.Arguments {
		g.expr(ctx, argument)
		g.push(ctx, "$a0")
	}
	receiverType := typeSelf
	if e.Receiver != nil {
		g.expr(ctx, e.Receiver)
		receiverType = e.Receiver.GetType()
	} else {
		g.ins("move\t$a0 $s0")
	}
	ok := g.newLabel()
	g.ins("bne\t$a0 $zero %s", ok)
	g.ins("la\t$a0 %s", g.stringConstant(ctx.file))
	g.ins("li\t$t1 %d", e.GetSpan().StartLine)
	g.ins("jal\t_dispatch_abort")
	g.emit.label(ok)
	class := g.staticClass(ctx, receiverType)
	if e.StaticTypeName != "" {
		class = g.layout.byName[e.StaticTypeName]
		g.ins("la\t$t1 %s_dispTab", class.name)
	} else {
		g.ins("lw\t$t1 %d($a0)", mipsDispatchOffset)
	}
	offset, _ := class.methodOffset(e.Method)
	g.ins("lw\t$t1 %d($t1)", offset)
	g.ins("jalr\t$t1")
	ctx.depth -= len(e.Arguments) // the callee popped its arguments
}

func (g *mipsGenerator) let(ctx *mipsContext, e *LetExpr, index int) {
	if index == len(e.Bindings) {
		g.expr(ctx, e.Body)
		return
	}
	binding := e.Bindings[index]
	if binding.Init != nil {
		g.expr(ctx, binding.Init)
	} else {
		g.loadDefault(binding.TypeName)
	}
	offset := -mipsWord * (ctx.depth + 1)
	g.push(ctx, "$a0")
	ctx.scopes = append(ctx.scopes, map[string]int{binding.Name: offset})
	g.let(ctx, e, index+1)
	ctx.scopes = ctx.scopes[:len(ctx.scopes)-1]
	g.ins("addiu\t$sp $sp 4")
	ctx.depth--
}

// caseExpr aborts through _case_abort2 (file in $a0, line in $t1) on void,
// then tests the dynamic tag against each branch's tag range, most specific
// branch first, and calls _case_abort (object in $a0) when none matches.
func (g *mipsGenerator) caseExpr(ctx *mipsContext, e *CaseExpr) {
	g.expr(ctx, e.Value)
	notVoid, end := g.newLabel(), g.newLabel()
	g.ins("bne\t$a0 $zero %s", notVoid)
	g.ins("la\t$a0 %s", g.stringConstant(ctx.file))
	g.ins("li\t$t1 %d", e.GetSpan().StartLine)
	g.ins("jal\t_case_abort2")
	g.emit.label(notVoid)
	g.ins("lw\t$t1 0($a0)")
	branches := append([]CaseBranch(nil), e.Branches...)
	depthOf := func(branch CaseBranch) int { return g.layout.byName[branch.TypeName].depth }
	for i := 1; i < len(branches); i++ {
		for j := i; j > 0 && depthOf(branches[j]) > depthOf(branches[j-1]); j-- {
			branches[j], branches[j-1] = branches[j-1], branches[j]
		}
	}
	for _, branch := range branches {
		class := g.layout.byName[branch.TypeName]
		next := g.newLabel()
		g.ins("blt\t$t1 %d %s", class.tag, next)
		g.ins("bgt\t$t1 %d %s", class.maxTag, next)
		offset := -mipsWord * (ctx.depth + 1)
		g.push(ctx, "$a0")
		ctx.scopes = append(ctx.scopes, map[string]int{branch.Name: offset})
		g.expr(ctx, branch.Body)
		ctx.scopes = ctx.scopes[:len(ctx.scopes)-1]
		g.ins("addiu\t$sp $sp 4")
		ctx.depth--
		g.ins("b\t%s", end)
		g.emit.label(next)
	}
	g.ins("jal\t_case_abort")
	g.emit.label(end)
}

func (g *mipsGenerator) binary(ctx *mipsContext, e *BinaryExpr) {
	g.expr(ctx, e.Left)
	g.push(ctx, "$a0")
	g.expr(ctx, e.Right)
	switch e.Operator {
	case TokenPlus, TokenMinus, TokenStar, TokenSlash:
		// Int objects are immutable: copy the right operand for the result.
		g.ins("jal\tObject.copy")
		g.pop(ctx, "$t1")
		g.ins("lw\t$t1 12($t1)")
		g.ins("lw\t$t2 12($a0)")
		op := map[TokenType]string{TokenPlus: "add", TokenMinus: "sub", TokenStar: "mul", TokenSlash: "div"}[e.Operator]
		g.ins("%s\t$t1 $t1 $t2", op)
		g.ins("sw\t$t1 12($a0)")
	case TokenLess, TokenLessEqual:
		g.pop(ctx, "$t1")
		g.ins("lw\t$t1 12($t1)")
		g.ins("lw\t$t2 12($a0)")
		branch := map[TokenType]string{TokenLess: "blt", TokenLessEqual: "ble"}[e.Operator]
		done := g.newLabel()
		g.ins("la\t$a0 bool_const1")
		g.ins("%s\t$t1 $t2 %s", branch, done)
		g.ins("la\t$a0 bool_const0")
		g.emit.label(done)
	case TokenEqual:
		// Identical pointers are equal; otherwise equality_test compares
		// Int, String, and Bool values and returns $a0 (true) or $a1.
		g.pop(ctx, "$t1")
		g.ins("move\t$t2 $a0")
		done := g.newLabel()
		g.ins("la\t$a0 bool_const1")
		g.ins("beq\t$t1 $t2 %s", done)
		g.ins("la\t$a1 bool_const0")
		g.ins("jal\tequality_test")
		g.emit.label(done)
	}
}
