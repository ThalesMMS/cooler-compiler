// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type opcode string

const (
	opPushInt       opcode = "PUSH_INT"
	opPushString    opcode = "PUSH_STRING"
	opPushBool      opcode = "PUSH_BOOL"
	opPushVoid      opcode = "PUSH_VOID"
	opLoadSelf      opcode = "LOAD_SELF"
	opLoad          opcode = "LOAD"
	opStore         opcode = "STORE"
	opDup           opcode = "DUP"
	opPop           opcode = "POP"
	opEnterScope    opcode = "ENTER_SCOPE"
	opExitScopeKeep opcode = "EXIT_SCOPE_KEEP"
	opBind          opcode = "BIND"
	opAdd           opcode = "ADD"
	opSub           opcode = "SUB"
	opMul           opcode = "MUL"
	opDiv           opcode = "DIV"
	opNeg           opcode = "NEG"
	opNot           opcode = "NOT"
	opLess          opcode = "LESS"
	opLessEqual     opcode = "LESS_EQUAL"
	opEqual         opcode = "EQUAL"
	opIsVoid        opcode = "ISVOID"
	opJump          opcode = "JUMP"
	opJumpIfFalse   opcode = "JUMP_IF_FALSE"
	opNew           opcode = "NEW"
	opCall          opcode = "CALL"
	opReturn        opcode = "RETURN"
	opCaseSelect    opcode = "CASE_SELECT"
	opHalt          opcode = "HALT"
)

type caseTarget struct {
	Name   string
	Type   string
	Target int
}

type byteInstruction struct {
	Op         opcode
	Text       string
	Int        int
	Bool       bool
	Target     int
	ArgCount   int
	StaticType string
	Cases      []caseTarget
	Span       Span
	Comment    string
}

type compiledMethod struct {
	Owner       string
	Name        string
	DisplayName string
	Formals     []string
	FormalTypes []string
	ReturnType  string
	Code        []byteInstruction
	Span        Span
	Synthetic   bool
}

type runtimeAttribute struct {
	Name     string
	TypeName string
	Owner    string
}

type runtimeClass struct {
	Name       string
	Parent     string
	Attributes []runtimeAttribute
}

type executable struct {
	Table       *ClassTable
	Classes     map[string]*runtimeClass
	Methods     map[string]*compiledMethod
	MethodOrder []*compiledMethod
	Entry       *compiledMethod
	IR          []IRInstruction
	Assembly    []string
	ControlFlow ControlFlowGraph
	Diagnostics []Diagnostic
}

func methodKey(owner, name string) string {
	return owner + "." + name
}

func Generate(program *Program, analysis Analysis) *executable {
	exec := generateExecutable(program, analysis)
	materializeIR(exec)
	materializeAssembly(exec)
	return exec
}

func generateExecutable(program *Program, analysis Analysis) *executable {
	g := &generator{
		program: program,
		table:   analysis.Table,
		exec: &executable{
			Table:   analysis.Table,
			Classes: map[string]*runtimeClass{},
			Methods: map[string]*compiledMethod{},
		},
	}
	g.buildRuntimeClasses()
	g.compileInitializers()
	g.compileMethods()
	g.compileEntry()
	return g.exec
}

type generator struct {
	program *Program
	table   *ClassTable
	exec    *executable
	current *compiledMethod
}

func (g *generator) buildRuntimeClasses() {
	if g.table == nil {
		return
	}
	for _, name := range g.table.Order {
		info := g.table.Classes[name]
		class := &runtimeClass{Name: name, Parent: info.Parent}
		// Runtime objects carry every inherited attribute. Defaults are installed
		// at allocation; generated initializers then evaluate explicit expressions
		// in root-to-leaf order.
		lineage := []string{}
		for current := name; current != ""; current = g.table.ParentOf(current) {
			lineage = append(lineage, current)
		}
		for i := len(lineage) - 1; i >= 0; i-- {
			ancestor := g.table.Classes[lineage[i]]
			if ancestor == nil {
				continue
			}
			attrs := orderedAttributes(ancestor)
			for _, attr := range attrs {
				class.Attributes = append(class.Attributes, runtimeAttribute{Name: attr.Name, TypeName: attr.TypeName, Owner: attr.Owner})
			}
		}
		g.exec.Classes[name] = class
	}
}

func orderedAttributes(class *ClassInfo) []*AttrInfo {
	if class == nil {
		return nil
	}
	if class.Decl != nil {
		result := []*AttrInfo{}
		for _, feature := range class.Decl.Features {
			attr, ok := feature.(*AttributeDecl)
			if !ok {
				continue
			}
			if info, found := class.Attributes[attr.Name]; found && info.Decl == attr {
				result = append(result, info)
			}
		}
		return result
	}
	names := make([]string, 0, len(class.Attributes))
	for name := range class.Attributes {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]*AttrInfo, 0, len(names))
	for _, name := range names {
		result = append(result, class.Attributes[name])
	}
	return result
}

func (g *generator) compileInitializers() {
	if g.table == nil {
		return
	}
	for _, className := range g.table.Order {
		class := g.table.Classes[className]
		method := &compiledMethod{
			Owner: className, Name: "$init", DisplayName: className + ".$init",
			ReturnType: typeSelf, Synthetic: true, Span: class.Span,
		}
		g.current = method
		if class.Parent != "" {
			g.emit(opLoadSelf, class.Span, "initialize inherited attributes")
			g.emitCall("$init", 0, class.Parent, class.Span, "static parent initializer")
			g.emit(opPop, class.Span, "discard parent initializer result")
		}
		for _, attr := range orderedAttributes(class) {
			if attr.Decl == nil || attr.Decl.Init == nil {
				continue
			}
			g.compileExpr(attr.Decl.Init)
			g.emitText(opStore, attr.Name, attr.Decl.NameSpan, "initialize attribute "+attr.Name)
		}
		g.emit(opLoadSelf, class.Span, "initializer returns self")
		g.emit(opReturn, class.Span, "return initialized object")
		g.addMethod(method)
	}
}

func (g *generator) compileMethods() {
	if g.program == nil || g.table == nil {
		return
	}
	for _, decl := range g.program.Classes {
		class := g.table.Classes[decl.Name]
		if class == nil || class.Decl != decl {
			continue
		}
		for _, feature := range decl.Features {
			methodDecl, ok := feature.(*MethodDecl)
			if !ok {
				continue
			}
			info := class.Methods[methodDecl.Name]
			if info == nil || info.Decl != methodDecl {
				continue
			}
			method := &compiledMethod{
				Owner: class.Name, Name: methodDecl.Name,
				DisplayName: class.Name + "." + methodDecl.Name,
				ReturnType:  methodDecl.ReturnType, Span: methodDecl.Span,
			}
			for _, formal := range methodDecl.Formals {
				method.Formals = append(method.Formals, formal.Name)
				method.FormalTypes = append(method.FormalTypes, formal.TypeName)
			}
			g.current = method
			g.compileExpr(methodDecl.Body)
			g.emit(opReturn, methodDecl.Body.GetSpan(), "return method result")
			g.addMethod(method)
		}
	}
}

func (g *generator) compileEntry() {
	span := emptySpan()
	if g.program != nil {
		span = g.program.Span
	}
	entry := &compiledMethod{
		Owner: "$program", Name: "$entry", DisplayName: "$program.$entry",
		ReturnType: typeObject, Synthetic: true, Span: span,
	}
	g.current = entry
	g.emitText(opNew, "Main", span, "allocate Main")
	g.emitCall("main", 0, "", span, "invoke program entry point")
	g.emit(opHalt, span, "program finished")
	g.addMethod(entry)
	g.exec.Entry = entry
}

func (g *generator) addMethod(method *compiledMethod) {
	key := methodKey(method.Owner, method.Name)
	g.exec.Methods[key] = method
	g.exec.MethodOrder = append(g.exec.MethodOrder, method)
}

func (g *generator) emit(op opcode, span Span, comment string) int {
	return g.emitInstruction(byteInstruction{Op: op, Span: span, Comment: comment})
}

func (g *generator) emitText(op opcode, text string, span Span, comment string) int {
	return g.emitInstruction(byteInstruction{Op: op, Text: text, Span: span, Comment: comment})
}

func (g *generator) emitInstruction(instruction byteInstruction) int {
	if instruction.Span.StartLine == 0 {
		instruction.Span = emptySpan()
	}
	index := len(g.current.Code)
	g.current.Code = append(g.current.Code, instruction)
	return index
}

func (g *generator) emitJump(op opcode, span Span, comment string) int {
	return g.emitInstruction(byteInstruction{Op: op, Target: -1, Span: span, Comment: comment})
}

func (g *generator) patchTarget(index, target int) {
	if index >= 0 && index < len(g.current.Code) {
		g.current.Code[index].Target = target
	}
}

func (g *generator) emitCall(method string, argCount int, staticType string, span Span, comment string) int {
	return g.emitInstruction(byteInstruction{
		Op: opCall, Text: method, ArgCount: argCount,
		StaticType: staticType, Span: span, Comment: comment,
	})
}

func (g *generator) compileExpr(expr Expr) {
	if expr == nil {
		g.emit(opPushVoid, emptySpan(), "missing expression")
		return
	}
	switch e := expr.(type) {
	case *NoExpr:
		g.emit(opPushVoid, e.GetSpan(), "no expression")
	case *IntExpr:
		g.emitInstruction(byteInstruction{Op: opPushInt, Int: e.Value, Span: e.GetSpan(), Comment: "integer literal"})
	case *StringExpr:
		g.emitInstruction(byteInstruction{Op: opPushString, Text: e.Value, Span: e.GetSpan(), Comment: "string literal"})
	case *BoolExpr:
		g.emitInstruction(byteInstruction{Op: opPushBool, Bool: e.Value, Span: e.GetSpan(), Comment: "Boolean literal"})
	case *IdentifierExpr:
		if e.Name == "self" {
			g.emit(opLoadSelf, e.GetSpan(), "load self")
		} else {
			g.emitText(opLoad, e.Name, e.GetSpan(), "load "+e.Name)
		}
	case *AssignExpr:
		g.compileExpr(e.Value)
		g.emit(opDup, e.GetSpan(), "assignment expression keeps its value")
		g.emitText(opStore, e.Name, e.NameSpan, "store "+e.Name)
	case *DispatchExpr:
		if e.Receiver == nil {
			g.emit(opLoadSelf, e.GetSpan(), "implicit self receiver")
		} else {
			g.compileExpr(e.Receiver)
		}
		for _, arg := range e.Arguments {
			g.compileExpr(arg)
		}
		g.emitCall(e.Method, len(e.Arguments), e.StaticTypeName, e.GetSpan(), "dispatch "+e.Method)
	case *IfExpr:
		g.compileExpr(e.Condition)
		falseJump := g.emitJump(opJumpIfFalse, e.Condition.GetSpan(), "select else branch")
		g.compileExpr(e.Then)
		endJump := g.emitJump(opJump, e.Then.GetSpan(), "skip else branch")
		g.patchTarget(falseJump, len(g.current.Code))
		g.compileExpr(e.Else)
		g.patchTarget(endJump, len(g.current.Code))
	case *WhileExpr:
		start := len(g.current.Code)
		g.compileExpr(e.Condition)
		exit := g.emitJump(opJumpIfFalse, e.Condition.GetSpan(), "leave loop")
		g.compileExpr(e.Body)
		g.emit(opPop, e.Body.GetSpan(), "discard loop-body value")
		back := g.emitJump(opJump, e.GetSpan(), "repeat loop")
		g.patchTarget(back, start)
		g.patchTarget(exit, len(g.current.Code))
		g.emit(opPushVoid, e.GetSpan(), "while has type Object and yields void")
	case *BlockExpr:
		if len(e.Expressions) == 0 {
			g.emit(opPushVoid, e.GetSpan(), "empty block")
			return
		}
		for i, child := range e.Expressions {
			g.compileExpr(child)
			if i < len(e.Expressions)-1 {
				g.emit(opPop, child.GetSpan(), "discard non-final block value")
			}
		}
	case *LetExpr:
		g.emit(opEnterScope, e.GetSpan(), "enter let scope")
		for _, binding := range e.Bindings {
			if binding.Init != nil {
				g.compileExpr(binding.Init)
			} else {
				g.emitDefault(binding.TypeName, binding.Span)
			}
			g.emitText(opBind, binding.Name, binding.NameSpan, "bind "+binding.Name)
		}
		g.compileExpr(e.Body)
		g.emit(opExitScopeKeep, e.GetSpan(), "leave let scope and preserve result")
	case *CaseExpr:
		g.compileExpr(e.Value)
		selectIndex := g.emitInstruction(byteInstruction{Op: opCaseSelect, Span: e.GetSpan(), Comment: "select most specific matching branch"})
		endJumps := []int{}
		cases := make([]caseTarget, 0, len(e.Branches))
		for _, branch := range e.Branches {
			cases = append(cases, caseTarget{Name: branch.Name, Type: branch.TypeName, Target: len(g.current.Code)})
			g.compileExpr(branch.Body)
			g.emit(opExitScopeKeep, branch.Span, "leave case branch scope")
			endJumps = append(endJumps, g.emitJump(opJump, branch.Span, "leave case expression"))
		}
		end := len(g.current.Code)
		for _, jump := range endJumps {
			g.patchTarget(jump, end)
		}
		g.current.Code[selectIndex].Cases = cases
	case *NewExpr:
		g.emitText(opNew, e.TypeName, e.GetSpan(), "allocate and initialize "+e.TypeName)
	case *IsVoidExpr:
		g.compileExpr(e.Value)
		g.emit(opIsVoid, e.GetSpan(), "test void")
	case *UnaryExpr:
		g.compileExpr(e.Value)
		switch e.Operator {
		case TokenTilde:
			g.emit(opNeg, e.GetSpan(), "integer complement")
		case TokenNot:
			g.emit(opNot, e.GetSpan(), "Boolean complement")
		}
	case *BinaryExpr:
		g.compileExpr(e.Left)
		g.compileExpr(e.Right)
		op := opEqual
		switch e.Operator {
		case TokenPlus:
			op = opAdd
		case TokenMinus:
			op = opSub
		case TokenStar:
			op = opMul
		case TokenSlash:
			op = opDiv
		case TokenLess:
			op = opLess
		case TokenLessEqual:
			op = opLessEqual
		case TokenEqual:
			op = opEqual
		}
		g.emit(op, e.GetSpan(), "evaluate "+string(e.Operator))
	default:
		g.exec.Diagnostics = append(g.exec.Diagnostics, errorDiagnostic(
			"ir", "COL5001", fmt.Sprintf("unsupported expression %T", expr),
			"The internal compiler should lower every valid COOL expression.", expr.GetSpan(),
		))
		g.emit(opPushVoid, expr.GetSpan(), "unsupported expression fallback")
	}
}

func (g *generator) emitDefault(typeName string, span Span) {
	switch typeName {
	case typeInt:
		g.emitInstruction(byteInstruction{Op: opPushInt, Int: 0, Span: span, Comment: "default Int value"})
	case typeString:
		g.emitInstruction(byteInstruction{Op: opPushString, Text: "", Span: span, Comment: "default String value"})
	case typeBool:
		g.emitInstruction(byteInstruction{Op: opPushBool, Bool: false, Span: span, Comment: "default Bool value"})
	default:
		g.emit(opPushVoid, span, "default object value")
	}
}

func materializeIR(exec *executable) {
	exec.IR = nil
	exec.ControlFlow = ControlFlowGraph{}
	globalIndex := 0
	blocks := []ControlFlowBlock{}
	edges := []ControlFlowEdge{}
	for _, method := range exec.MethodOrder {
		methodStart := globalIndex
		for _, instruction := range method.Code {
			args := instructionArgs(instruction)
			ir := IRInstruction{
				Index: globalIndex, Method: method.DisplayName, Op: string(instruction.Op),
				Args: args, Comment: instruction.Comment,
				Line: instruction.Span.StartLine, Column: instruction.Span.StartColumn,
				Span: instruction.Span,
			}
			exec.IR = append(exec.IR, ir)
			globalIndex++
		}
		methodEnd := globalIndex - 1
		if methodEnd < methodStart {
			methodEnd = methodStart
		}
		methodBlocks, methodEdges := controlFlowForMethod(method, methodStart)
		blocks = append(blocks, methodBlocks...)
		edges = append(edges, methodEdges...)
	}
	exec.ControlFlow = ControlFlowGraph{Blocks: blocks, Edges: edges}
}

func materializeAssembly(exec *executable) {
	assembly := []string{}
	for _, method := range exec.MethodOrder {
		assembly = append(assembly, "", method.DisplayName+":")
		for localIndex, instruction := range method.Code {
			args := instructionArgs(instruction)
			line := fmt.Sprintf("  %04d  %-16s", localIndex, instruction.Op)
			if len(args) > 0 {
				line += " " + strings.Join(args, ", ")
			}
			if instruction.Comment != "" {
				line += "    ; " + instruction.Comment
			}
			assembly = append(assembly, line)
		}
	}
	if len(assembly) > 0 && assembly[0] == "" {
		assembly = assembly[1:]
	}
	exec.Assembly = assembly
}

func instructionArgs(instruction byteInstruction) []string {
	switch instruction.Op {
	case opPushInt:
		return []string{strconv.Itoa(instruction.Int)}
	case opPushString:
		return []string{strconv.Quote(instruction.Text)}
	case opPushBool:
		return []string{strconv.FormatBool(instruction.Bool)}
	case opLoad, opStore, opBind, opNew:
		return []string{instruction.Text}
	case opJump, opJumpIfFalse:
		return []string{fmt.Sprintf("L%d", instruction.Target)}
	case opCall:
		args := []string{instruction.Text, strconv.Itoa(instruction.ArgCount)}
		if instruction.StaticType != "" {
			args = append(args, "@"+instruction.StaticType)
		}
		return args
	case opCaseSelect:
		args := make([]string, 0, len(instruction.Cases))
		for _, target := range instruction.Cases {
			args = append(args, fmt.Sprintf("%s:%s->L%d", target.Name, target.Type, target.Target))
		}
		return args
	default:
		return nil
	}
}

func controlFlowForMethod(method *compiledMethod, globalStart int) ([]ControlFlowBlock, []ControlFlowEdge) {
	if len(method.Code) == 0 {
		return nil, nil
	}
	leaders := map[int]bool{0: true}
	for i, instruction := range method.Code {
		switch instruction.Op {
		case opJump, opJumpIfFalse:
			if instruction.Target >= 0 && instruction.Target < len(method.Code) {
				leaders[instruction.Target] = true
			}
			if i+1 < len(method.Code) {
				leaders[i+1] = true
			}
		case opCaseSelect:
			for _, target := range instruction.Cases {
				if target.Target >= 0 && target.Target < len(method.Code) {
					leaders[target.Target] = true
				}
			}
			if i+1 < len(method.Code) {
				leaders[i+1] = true
			}
		case opReturn, opHalt:
			if i+1 < len(method.Code) {
				leaders[i+1] = true
			}
		}
	}
	leaderList := make([]int, 0, len(leaders))
	for leader := range leaders {
		leaderList = append(leaderList, leader)
	}
	sort.Ints(leaderList)
	blockForInstruction := map[int]string{}
	blocks := make([]ControlFlowBlock, 0, len(leaderList))
	for i, start := range leaderList {
		end := len(method.Code) - 1
		if i+1 < len(leaderList) {
			end = leaderList[i+1] - 1
		}
		name := fmt.Sprintf("%s:B%d", method.DisplayName, i)
		span := method.Code[start].Span
		if end >= start {
			span = joinSpans(span, method.Code[end].Span)
		}
		blocks = append(blocks, ControlFlowBlock{
			Name: name, Method: method.DisplayName,
			Start: globalStart + start, End: globalStart + end,
			Range: fmt.Sprintf("%d–%d", globalStart+start, globalStart+end), Span: span,
		})
		for j := start; j <= end; j++ {
			blockForInstruction[j] = name
		}
	}
	edgeSet := map[string]bool{}
	edges := []ControlFlowEdge{}
	addEdge := func(from, to, kind string) {
		if from == "" || to == "" {
			return
		}
		key := from + "\x00" + to + "\x00" + kind
		if edgeSet[key] {
			return
		}
		edgeSet[key] = true
		edges = append(edges, ControlFlowEdge{From: from, To: to, Kind: kind})
	}
	for i, instruction := range method.Code {
		from := blockForInstruction[i]
		switch instruction.Op {
		case opJump:
			addEdge(from, blockForInstruction[instruction.Target], "jump")
		case opJumpIfFalse:
			addEdge(from, blockForInstruction[instruction.Target], "false")
			if i+1 < len(method.Code) {
				addEdge(from, blockForInstruction[i+1], "true")
			}
		case opCaseSelect:
			for _, target := range instruction.Cases {
				addEdge(from, blockForInstruction[target.Target], "case "+target.Type)
			}
		case opReturn, opHalt:
			// Terminal.
		default:
			if i+1 < len(method.Code) && blockForInstruction[i] != blockForInstruction[i+1] {
				addEdge(from, blockForInstruction[i+1], "fallthrough")
			}
		}
	}
	return blocks, edges
}
