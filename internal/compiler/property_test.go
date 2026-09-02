// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type generatedExpression struct {
	kind                   string
	staticType             string
	value                  string
	name                   string
	typeName               string
	operator               string
	method                 string
	staticEquivalentTarget string
	receiver               *generatedExpression
	children               []*generatedExpression
	arguments              []*generatedExpression
	bindings               []generatedBinding
	branches               []generatedBranch
}

type generatedBinding struct {
	name, typeName string
	initial        *generatedExpression
}

type generatedBranch struct {
	name, typeName string
	body           *generatedExpression
}

type generatedAttribute struct {
	name, typeName string
	initial        *generatedExpression
}

type generatedFormal struct{ name, typeName string }

type generatedMethod struct {
	name, returnType string
	formals          []generatedFormal
	body             *generatedExpression
	isMain           bool
}

type generatedClass struct {
	name, parent string
	attributes   []generatedAttribute
	methods      []generatedMethod
}

type generatedCoolProgram struct {
	classes        []generatedClass
	input          string
	expectedOutput string
	renames        map[string]string
}

type generatedRenderOptions struct {
	comments          bool
	parenthesize      bool
	alphaRename       bool
	staticDispatch    bool
	unreachableBranch bool
}

func TestGeneratedTypedPrograms(t *testing.T) {
	for seed := uint64(0); seed < 64; seed++ {
		program := generateTypedCoolProgram(seed)
		program.assertTyped(t)
		source := program.render(generatedRenderOptions{})
		result := compileFuzzSource(source, program.input)
		assertGeneratedSuccess(t, result, map[string]string{"source-1": source})
		if got := strings.Join(result.Runtime.Output, ""); got != program.expectedOutput {
			t.Fatalf("seed %d output = %q, want %q\n%s", seed, got, program.expectedOutput, source)
		}
		again := compileFuzzSource(source, program.input)
		assertDeterministicResult(t, result, again)
	}
}

func TestGeneratedProgramsMetamorphicTransformations(t *testing.T) {
	for seed := uint64(0); seed < 16; seed++ {
		program := generateTypedCoolProgram(seed)
		baseSource := program.render(generatedRenderOptions{})
		base := compileFuzzSource(baseSource, program.input)
		assertGeneratedSuccess(t, base, map[string]string{"source-1": baseSource})

		commented := program.render(generatedRenderOptions{comments: true})
		assertTokenSequenceEqual(t, baseSource, commented)
		assertMetamorphicEquivalent(t, "comments", base, compileFuzzSource(commented, program.input), map[string]string{"source-1": commented}, false)

		crlf := strings.ReplaceAll(baseSource, "\n", "\r\n")
		assertTokenSequenceEqual(t, baseSource, crlf)
		assertMetamorphicEquivalent(t, "LF to CRLF", base, compileFuzzSource(crlf, program.input), map[string]string{"source-1": crlf}, false)

		parenthesized := program.render(generatedRenderOptions{parenthesize: true})
		assertMetamorphicEquivalent(t, "redundant parentheses", base, compileFuzzSource(parenthesized, program.input), map[string]string{"source-1": parenthesized}, false)

		renamed := program.render(generatedRenderOptions{alphaRename: true})
		assertMetamorphicEquivalent(t, "alpha rename", base, compileFuzzSource(renamed, program.input), map[string]string{"source-1": renamed}, false)

		reformatted := reformatCoolSource(baseSource)
		assertTokenSequenceEqual(t, baseSource, reformatted)
		assertMetamorphicEquivalent(t, "token-preserving reformat", base, compileFuzzSource(reformatted, program.input), map[string]string{"source-1": reformatted}, false)

		staticDispatch := program.render(generatedRenderOptions{staticDispatch: true})
		assertMetamorphicEquivalent(t, "proven static dispatch", base, compileFuzzSource(staticDispatch, program.input), map[string]string{"source-1": staticDispatch}, false)

		unreachable := program.render(generatedRenderOptions{unreachableBranch: true})
		assertMetamorphicEquivalent(t, "type-correct unreachable branch", base, compileFuzzSource(unreachable, program.input), map[string]string{"source-1": unreachable}, true)

		units := program.renderUnits(generatedRenderOptions{})
		sources := map[string]string{}
		for _, unit := range units {
			sources[unit.ID] = unit.Content
		}
		split := CompileProject(units, ProjectOptions{Input: program.input, VMMaxSteps: fuzzVMMaxSteps, CompilerVersion: "fuzz"})
		assertMetamorphicEquivalent(t, "split source units", base, split, sources, false)
	}
}

func generateTypedCoolProgram(seed uint64) generatedCoolProgram {
	suffix := strconv.FormatUint(seed%17, 10)
	baseName := "Base" + suffix
	midName := "Mid" + suffix
	derivedName := "Derived" + suffix
	seedAttribute := "seed" + suffix
	valueMethod := "value" + suffix
	selfMethod := "copySelf" + suffix
	chooseMethod := "choose" + suffix
	formalX := "x" + suffix
	formalFlag := "flag" + suffix
	local := "local" + suffix
	count := "count" + suffix
	item := "item" + suffix
	derivedBranch := "derived" + suffix
	baseBranch := "base" + suffix
	seedValue := int(seed%7) + 1
	inputValue := int(seed%9) + 1
	loopBound := int(seed%3) + 1
	label := "P" + suffix

	base := generatedClass{
		name:       baseName,
		attributes: []generatedAttribute{{name: seedAttribute, typeName: typeInt, initial: generatedInt(seedValue)}},
		methods: []generatedMethod{
			{name: selfMethod, returnType: typeSelf, body: generatedIdentifier("self", typeSelf)},
			{name: valueMethod, returnType: typeInt, formals: []generatedFormal{{formalX, typeInt}}, body: generatedBinary("+", typeInt, generatedIdentifier(formalX, typeInt), generatedIdentifier(seedAttribute, typeInt))},
		},
	}
	parent := baseName
	classes := []generatedClass{base}
	if seed%2 == 1 {
		classes = append(classes, generatedClass{name: midName, parent: baseName})
		parent = midName
	}
	derived := generatedClass{
		name: derivedName, parent: parent,
		attributes: []generatedAttribute{
			{name: "label" + suffix, typeName: typeString, initial: generatedString(label)},
			{name: "peer" + suffix, typeName: baseName},
		},
		methods: []generatedMethod{{
			name: chooseMethod, returnType: baseName, formals: []generatedFormal{{formalFlag, typeBool}},
			body: generatedIf(baseName, generatedIdentifier(formalFlag, typeBool), generatedIdentifier("self", typeSelf), generatedNew(baseName)),
		}},
	}
	classes = append(classes, derived)

	readInput := generatedDispatch(nil, "in_int", nil, typeInt, "")
	condition := generatedBinary("<", typeBool, generatedIdentifier(count, typeInt), generatedInt(loopBound))
	increment := generatedAssign(count, typeInt, generatedBinary("+", typeInt, generatedIdentifier(count, typeInt), generatedInt(1)))
	loop := generatedWhile(condition, increment)
	valueCall := generatedDispatch(generatedIdentifier(derivedBranch, derivedName), valueMethod, []*generatedExpression{generatedIdentifier(local, typeInt)}, typeInt, baseName)
	derivedBody := generatedBlock(typeSelf,
		generatedDispatch(nil, "out_int", []*generatedExpression{valueCall}, typeSelf, ""),
		generatedDispatch(nil, "out_string", []*generatedExpression{generatedString(label)}, typeSelf, ""),
	)
	baseValueCall := generatedDispatch(generatedIdentifier(baseBranch, baseName), valueMethod, []*generatedExpression{generatedIdentifier(local, typeInt)}, typeInt, "")
	selection := generatedCase(typeSelf, generatedIdentifier(item, derivedName),
		generatedBranch{derivedBranch, derivedName, derivedBody},
		generatedBranch{baseBranch, baseName, generatedDispatch(nil, "out_int", []*generatedExpression{baseValueCall}, typeSelf, "")},
	)
	mainBlock := generatedBlock(typeObject, loop, selection, generatedNew(typeObject))
	mainBody := generatedLet(typeObject, []generatedBinding{
		{name: local, typeName: typeInt, initial: readInput},
		{name: count, typeName: typeInt, initial: generatedInt(0)},
		{name: item, typeName: derivedName, initial: generatedNew(derivedName)},
	}, mainBlock)
	classes = append(classes, generatedClass{
		name: "Main", parent: typeIO,
		methods: []generatedMethod{{name: "main", returnType: typeObject, body: mainBody, isMain: true}},
	})
	renamed := map[string]string{}
	for _, name := range []string{formalX, formalFlag, local, count, item, derivedBranch, baseBranch} {
		renamed[name] = name + "Renamed"
	}
	return generatedCoolProgram{
		classes: classes, input: strconv.Itoa(inputValue) + "\n",
		expectedOutput: strconv.Itoa(inputValue+seedValue) + label, renames: renamed,
	}
}

func generatedInt(value int) *generatedExpression {
	return &generatedExpression{kind: "int", staticType: typeInt, value: strconv.Itoa(value)}
}

func generatedString(value string) *generatedExpression {
	return &generatedExpression{kind: "string", staticType: typeString, value: value}
}

func generatedIdentifier(name, staticType string) *generatedExpression {
	return &generatedExpression{kind: "identifier", staticType: staticType, name: name}
}

func generatedNew(typeName string) *generatedExpression {
	return &generatedExpression{kind: "new", staticType: typeName, typeName: typeName}
}

func generatedBinary(operator, staticType string, left, right *generatedExpression) *generatedExpression {
	return &generatedExpression{kind: "binary", staticType: staticType, operator: operator, children: []*generatedExpression{left, right}}
}

func generatedAssign(name, staticType string, value *generatedExpression) *generatedExpression {
	return &generatedExpression{kind: "assign", staticType: staticType, name: name, children: []*generatedExpression{value}}
}

func generatedIf(staticType string, condition, thenBranch, elseBranch *generatedExpression) *generatedExpression {
	return &generatedExpression{kind: "if", staticType: staticType, children: []*generatedExpression{condition, thenBranch, elseBranch}}
}

func generatedWhile(condition, body *generatedExpression) *generatedExpression {
	return &generatedExpression{kind: "while", staticType: typeObject, children: []*generatedExpression{condition, body}}
}

func generatedBlock(staticType string, expressions ...*generatedExpression) *generatedExpression {
	return &generatedExpression{kind: "block", staticType: staticType, children: expressions}
}

func generatedLet(staticType string, bindings []generatedBinding, body *generatedExpression) *generatedExpression {
	return &generatedExpression{kind: "let", staticType: staticType, bindings: bindings, children: []*generatedExpression{body}}
}

func generatedCase(staticType string, value *generatedExpression, branches ...generatedBranch) *generatedExpression {
	return &generatedExpression{kind: "case", staticType: staticType, children: []*generatedExpression{value}, branches: branches}
}

func generatedDispatch(receiver *generatedExpression, method string, arguments []*generatedExpression, staticType, equivalentTarget string) *generatedExpression {
	return &generatedExpression{
		kind: "dispatch", staticType: staticType, receiver: receiver, method: method,
		arguments: arguments, staticEquivalentTarget: equivalentTarget,
	}
}

func (program generatedCoolProgram) render(options generatedRenderOptions) string {
	parts := make([]string, 0, len(program.classes))
	for index := range program.classes {
		parts = append(parts, program.renderClass(program.classes[index], options))
	}
	separator := "\n\n"
	if options.comments {
		separator = "\n\n-- metamorphic comment between classes\n\n"
	}
	prefix := ""
	if options.comments {
		prefix = "(* acceptance-preserving fuzz comment *)\n\n"
	}
	return prefix + strings.Join(parts, separator) + "\n"
}

func (program generatedCoolProgram) renderUnits(options generatedRenderOptions) []SourceUnit {
	units := make([]SourceUnit, 0, len(program.classes))
	for index, class := range program.classes {
		units = append(units, SourceUnit{
			ID: fmt.Sprintf("generated-%02d", index), Name: strings.ToLower(class.name) + ".cl",
			Content: program.renderClass(class, options) + "\n",
		})
	}
	return units
}

func (program generatedCoolProgram) renderClass(class generatedClass, options generatedRenderOptions) string {
	parent := ""
	if class.parent != "" && class.parent != typeObject {
		parent = " inherits " + class.parent
	}
	lines := []string{"class " + class.name + parent + " {"}
	for _, attribute := range class.attributes {
		line := "  " + attribute.name + " : " + attribute.typeName
		if attribute.initial != nil {
			line += " <- " + program.renderExpression(attribute.initial, options)
		}
		lines = append(lines, line+";")
	}
	for _, method := range class.methods {
		formals := make([]string, len(method.formals))
		for index, formal := range method.formals {
			formals[index] = program.renderName(formal.name, options) + " : " + formal.typeName
		}
		body := program.renderExpression(method.body, options)
		if method.isMain && options.unreachableBranch {
			body = "if true then " + body + " else new Object fi"
		}
		lines = append(lines, "  "+method.name+"("+strings.Join(formals, ", ")+") : "+method.returnType+" { "+body+" };")
	}
	lines = append(lines, "};")
	return strings.Join(lines, "\n")
}

func (program generatedCoolProgram) renderExpression(expression *generatedExpression, options generatedRenderOptions) string {
	if expression == nil {
		return "0"
	}
	render := func(child *generatedExpression) string { return program.renderExpression(child, options) }
	var text string
	switch expression.kind {
	case "int":
		text = expression.value
	case "string":
		text = strconv.Quote(expression.value)
	case "identifier":
		text = program.renderName(expression.name, options)
	case "new":
		text = "new " + expression.typeName
	case "binary":
		text = render(expression.children[0]) + " " + expression.operator + " " + render(expression.children[1])
	case "assign":
		text = program.renderName(expression.name, options) + " <- " + render(expression.children[0])
	case "if":
		text = "if " + render(expression.children[0]) + " then " + render(expression.children[1]) + " else " + render(expression.children[2]) + " fi"
	case "while":
		text = "while " + render(expression.children[0]) + " loop " + render(expression.children[1]) + " pool"
	case "block":
		parts := make([]string, len(expression.children))
		for index, child := range expression.children {
			parts[index] = render(child) + ";"
		}
		text = "{ " + strings.Join(parts, " ") + " }"
	case "let":
		bindings := make([]string, len(expression.bindings))
		for index, binding := range expression.bindings {
			bindings[index] = program.renderName(binding.name, options) + " : " + binding.typeName
			if binding.initial != nil {
				bindings[index] += " <- " + render(binding.initial)
			}
		}
		text = "let " + strings.Join(bindings, ", ") + " in " + render(expression.children[0])
	case "case":
		branches := make([]string, len(expression.branches))
		for index, branch := range expression.branches {
			branches[index] = program.renderName(branch.name, options) + " : " + branch.typeName + " => " + render(branch.body) + ";"
		}
		text = "case " + render(expression.children[0]) + " of " + strings.Join(branches, " ") + " esac"
	case "dispatch":
		arguments := make([]string, len(expression.arguments))
		for index, argument := range expression.arguments {
			arguments[index] = render(argument)
		}
		if expression.receiver == nil {
			text = expression.method + "(" + strings.Join(arguments, ", ") + ")"
		} else {
			target := ""
			if options.staticDispatch && expression.staticEquivalentTarget != "" {
				target = "@" + expression.staticEquivalentTarget
			}
			text = render(expression.receiver) + target + "." + expression.method + "(" + strings.Join(arguments, ", ") + ")"
		}
	default:
		panic("unknown generated expression kind " + expression.kind)
	}
	if options.parenthesize && expression.kind != "block" && expression.kind != "let" && expression.kind != "case" && expression.kind != "while" && expression.kind != "if" {
		return "(" + text + ")"
	}
	return text
}

func (program generatedCoolProgram) renderName(name string, options generatedRenderOptions) string {
	if options.alphaRename {
		if replacement := program.renames[name]; replacement != "" {
			return replacement
		}
	}
	return name
}

func (program generatedCoolProgram) assertTyped(t testing.TB) {
	t.Helper()
	for _, class := range program.classes {
		for _, attribute := range class.attributes {
			if attribute.initial != nil {
				assertGeneratedExpressionTyped(t, attribute.initial)
			}
		}
		for _, method := range class.methods {
			assertGeneratedExpressionTyped(t, method.body)
		}
	}
}

func assertGeneratedExpressionTyped(t testing.TB, expression *generatedExpression) {
	t.Helper()
	if expression == nil || expression.staticType == "" {
		t.Fatalf("generated expression has no known static type: %+v", expression)
	}
	if expression.receiver != nil {
		assertGeneratedExpressionTyped(t, expression.receiver)
	}
	for _, child := range expression.children {
		assertGeneratedExpressionTyped(t, child)
	}
	for _, argument := range expression.arguments {
		assertGeneratedExpressionTyped(t, argument)
	}
	for _, binding := range expression.bindings {
		if binding.initial != nil {
			assertGeneratedExpressionTyped(t, binding.initial)
		}
	}
	for _, branch := range expression.branches {
		assertGeneratedExpressionTyped(t, branch.body)
	}
}

func assertGeneratedSuccess(t testing.TB, result Result, sources map[string]string) {
	t.Helper()
	assertFuzzResult(t, result, sources)
	if hasErrors(result.Diagnostics) || !result.Runtime.Halted || result.Runtime.Paused {
		t.Fatalf("generated valid program failed: diagnostics=%+v runtime=%+v", result.Diagnostics, result.Runtime)
	}
	if bodyType := mainBodyStaticType(result.AST); bodyType != typeObject {
		t.Fatalf("generated main body static type = %q, want Object", bodyType)
	}
}

func assertMetamorphicEquivalent(t testing.TB, name string, base, transformed Result, sources map[string]string, allowIRDifference bool) {
	t.Helper()
	assertGeneratedSuccess(t, transformed, sources)
	if !reflect.DeepEqual(base.Diagnostics, transformed.Diagnostics) {
		t.Fatalf("%s changed diagnostics: base=%+v transformed=%+v", name, base.Diagnostics, transformed.Diagnostics)
	}
	if !reflect.DeepEqual(base.Runtime.Output, transformed.Runtime.Output) {
		t.Fatalf("%s changed output: base=%v transformed=%v", name, base.Runtime.Output, transformed.Runtime.Output)
	}
	if mainBodyStaticType(base.AST) != mainBodyStaticType(transformed.AST) {
		t.Fatalf("%s changed main static type: %s -> %s", name, mainBodyStaticType(base.AST), mainBodyStaticType(transformed.AST))
	}
	if !allowIRDifference && !reflect.DeepEqual(irOpcodeSequence(base.IR), irOpcodeSequence(transformed.IR)) {
		t.Fatalf("%s changed IR opcode sequence", name)
	}
}

func mainBodyStaticType(root *ASTNode) string {
	if root == nil {
		return ""
	}
	if root.Kind == "Method" && root.Name == "main" && len(root.Children) > 0 {
		return root.Children[len(root.Children)-1].Type
	}
	for index := range root.Children {
		if result := mainBodyStaticType(&root.Children[index]); result != "" {
			return result
		}
	}
	return ""
}

func irOpcodeSequence(instructions []IRInstruction) []string {
	result := make([]string, len(instructions))
	for index, instruction := range instructions {
		result[index] = instruction.Op
	}
	return result
}

func reformatCoolSource(source string) string {
	tokens, _ := Lex(source)
	parts := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if token.Type != TokenEOF {
			parts = append(parts, token.Lexeme)
		}
	}
	return strings.Join(parts, " \n") + "\n"
}

func assertTokenSequenceEqual(t testing.TB, first, second string) {
	t.Helper()
	firstTokens, firstDiagnostics := Lex(first)
	secondTokens, secondDiagnostics := Lex(second)
	if hasErrors(firstDiagnostics) || hasErrors(secondDiagnostics) {
		t.Fatalf("token-preserving transform did not lex: first=%+v second=%+v", firstDiagnostics, secondDiagnostics)
	}
	signature := func(tokens []Token) []string {
		result := make([]string, 0, len(tokens))
		for _, token := range tokens {
			result = append(result, string(token.Type)+"\x00"+token.Lexeme+"\x00"+token.Literal)
		}
		return result
	}
	if !reflect.DeepEqual(signature(firstTokens), signature(secondTokens)) {
		t.Fatal("metamorphic source changed the token sequence")
	}
}
