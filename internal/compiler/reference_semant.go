// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"fmt"
	"strings"
)

// The course-compatibility semantic checker reproduces the course `semant`
// phase on a reference parse: its error text and order, its recovery rules,
// and the static types it annotates. Cooler's own analyzer (analyzer.go) is
// unchanged; this checker only feeds the opt-in reference views.
//
// Phases, in order:
//  1. class graph: basic-class redefinition, duplicate classes, illegal or
//     undefined parents, inheritance cycles, and a missing Main. Any error
//     here halts before features or expressions are checked;
//  2. features of every class in program order (attributes, methods,
//     formals, overrides), then the Main.main check;
//  3. attribute initializers and method bodies of every class in program
//     order.
// After an expression error, the expression's type is Object so checking can
// continue without cascading errors.

const (
	refNoType   = "_no_type"
	refPrimSlot = "_prim_slot"
)

type refMethod struct {
	name        string
	formalNames []string
	formalTypes []string
	returnType  string
}

type refClass struct {
	name       string
	parent     string
	decl       *ClassDecl
	basic      bool
	attrOrder  []string
	attrTypes  map[string]string
	methods    map[string]*refMethod
	graphValid bool
}

type refSemantError struct {
	file    string
	line    int
	message string
}

type refSemant struct {
	parsed  referenceProgram
	classes map[string]*refClass
	order   []string
	errors  []refSemantError
	types   map[Expr]string
	current *refClass
	scopes  []map[string]string
}

// ReferenceSemantResult is the outcome of the course-compatibility pipeline
// `lexer | parser | semant` for a set of units.
type ReferenceSemantResult struct {
	// Stage is "parser" when parsing halted, "semant" when semantic analysis
	// halted, and "" on success.
	Stage string
	// Report is the halted phase's stderr text (errors and halt line).
	Report string
	// Dump is the typed AST printed by semant on success.
	Dump string
	// Errors lists the semantic error lines in report order.
	Errors []string
}

// ReferenceSemant runs the course pipeline through semantic analysis.
func ReferenceSemant(units []SourceUnit) ReferenceSemantResult {
	return ReferencePipeline{}.Semant(units)
}

// Semant runs the course pipeline through semantic analysis (see
// ReferenceSemant).
func (p ReferencePipeline) Semant(units []SourceUnit) ReferenceSemantResult {
	parsed := p.parse(units)
	if len(parsed.errors) > 0 || len(parsed.program.Classes) == 0 {
		if len(parsed.errors) == 0 {
			parsed.errors = append(parsed.errors, LALRSyntaxError{Token: Token{Type: TokenEOF}})
		}
		return ReferenceSemantResult{Stage: "parser", Report: parsed.syntaxErrorText()}
	}
	checker := &refSemant{parsed: parsed, classes: map[string]*refClass{}, types: map[Expr]string{}}
	checker.run()
	if len(checker.errors) > 0 {
		lines := make([]string, 0, len(checker.errors))
		var report strings.Builder
		for _, failure := range checker.errors {
			line := failure.message
			if failure.file != "" {
				line = fmt.Sprintf("%s:%d: %s", failure.file, failure.line, failure.message)
			}
			lines = append(lines, line)
			report.WriteString(line)
			report.WriteByte('\n')
		}
		report.WriteString("Compilation halted due to static semantic errors.\n")
		return ReferenceSemantResult{Stage: "semant", Report: report.String(), Errors: lines}
	}
	return ReferenceSemantResult{Dump: parsed.dump(checker.types)}
}

// ReferenceSemantDump returns the course `semant` output: the typed AST dump
// on success (ok=true), or the halted phase's report (ok=false).
func ReferenceSemantDump(units []SourceUnit) (string, bool) {
	return ReferencePipeline{}.SemantDump(units)
}

// SemantDump returns the course semant output (see ReferenceSemantDump).
func (p ReferencePipeline) SemantDump(units []SourceUnit) (string, bool) {
	result := p.Semant(units)
	if result.Stage != "" {
		return result.Report, false
	}
	return result.Dump, true
}

func (s *refSemant) errorAt(class *ClassDecl, line int, format string, args ...any) {
	file := ""
	if class != nil {
		file = s.parsed.files[class.Span.SourceID]
	}
	s.errors = append(s.errors, refSemantError{file: file, line: line, message: fmt.Sprintf(format, args...)})
}

func (s *refSemant) exprError(expr Expr, format string, args ...any) {
	s.errorAt(s.current.decl, s.lineOf(expr), format, args...)
}

func (s *refSemant) lineOf(expr Expr) int {
	if line, ok := s.parsed.lines[expr]; ok {
		return line
	}
	return expr.GetSpan().StartLine
}

func (s *refSemant) run() {
	s.installBasicClasses()
	if !s.checkClassGraph() {
		return
	}
	for _, decl := range s.parsed.program.Classes {
		s.collectFeatures(s.classes[decl.Name], decl)
	}
	s.checkMain()
	for _, decl := range s.parsed.program.Classes {
		s.checkClassBodies(s.classes[decl.Name])
	}
}

func (s *refSemant) installBasicClasses() {
	add := func(name, parent string, attrs [][2]string, methods ...*refMethod) {
		class := &refClass{name: name, parent: parent, basic: true, attrTypes: map[string]string{}, methods: map[string]*refMethod{}, graphValid: true}
		for _, attr := range attrs {
			class.attrOrder = append(class.attrOrder, attr[0])
			class.attrTypes[attr[0]] = attr[1]
		}
		for _, method := range methods {
			class.methods[method.name] = method
		}
		s.classes[name] = class
		s.order = append(s.order, name)
	}
	method := func(name, returnType string, formals ...string) *refMethod {
		m := &refMethod{name: name, returnType: returnType}
		for index := 0; index+1 < len(formals); index += 2 {
			m.formalNames = append(m.formalNames, formals[index])
			m.formalTypes = append(m.formalTypes, formals[index+1])
		}
		return m
	}
	add(typeObject, "", nil, method("abort", typeObject), method("type_name", typeString), method("copy", typeSelf))
	add(typeIO, typeObject, nil, method("out_string", typeSelf, "arg", typeString), method("out_int", typeSelf, "arg", typeInt), method("in_string", typeString), method("in_int", typeInt))
	add(typeInt, typeObject, [][2]string{{"_val", refPrimSlot}})
	add(typeBool, typeObject, [][2]string{{"_val", refPrimSlot}})
	add(typeString, typeObject, [][2]string{{"_val", typeInt}, {"_str_field", refPrimSlot}},
		method("length", typeInt), method("concat", typeString, "arg", typeString), method("substr", typeString, "arg", typeInt, "arg2", typeInt))
}

func (s *refSemant) checkClassGraph() bool {
	before := len(s.errors)
	for _, decl := range s.parsed.program.Classes {
		line := decl.Span.StartLine
		switch {
		case decl.Name == typeSelf || (s.classes[decl.Name] != nil && s.classes[decl.Name].basic):
			s.errorAt(decl, line, "Redefinition of basic class %s.", decl.Name)
		case s.classes[decl.Name] != nil:
			s.errorAt(decl, line, "Class %s was previously defined.", decl.Name)
		default:
			s.classes[decl.Name] = &refClass{name: decl.Name, parent: decl.Parent, decl: decl, attrTypes: map[string]string{}, methods: map[string]*refMethod{}}
			s.order = append(s.order, decl.Name)
		}
	}
	for _, decl := range s.parsed.program.Classes {
		class := s.classes[decl.Name]
		if class == nil || class.decl != decl {
			continue
		}
		switch decl.Parent {
		case typeInt, typeBool, typeString, typeSelf:
			s.errorAt(decl, decl.Span.StartLine, "Class %s cannot inherit class %s.", decl.Name, decl.Parent)
		default:
			if s.classes[decl.Parent] == nil {
				s.errorAt(decl, decl.Span.StartLine, "Class %s inherits from an undefined class %s.", decl.Name, decl.Parent)
			}
		}
	}
	if len(s.errors) > before {
		return false
	}
	for _, decl := range s.parsed.program.Classes {
		class := s.classes[decl.Name]
		if class == nil || class.decl != decl {
			continue
		}
		visited := map[string]bool{}
		for current := class.parent; current != ""; current = s.classes[current].parent {
			if current == class.name {
				s.errorAt(decl, decl.Span.StartLine, "Class %s, an ancestor of %s, is involved in an inheritance cycle.", class.name, class.name)
				break
			}
			if visited[current] || s.classes[current] == nil {
				break
			}
			visited[current] = true
		}
	}
	if len(s.errors) > before {
		return false
	}
	if main := s.classes["Main"]; main == nil || main.basic {
		s.errorAt(nil, 0, "Class Main is not defined.")
		return false
	}
	return true
}

func (s *refSemant) defined(name string) bool {
	return name == typeSelf || s.classes[name] != nil
}

// inheritedAttribute reports whether an ancestor of class declares name.
func (s *refSemant) inheritedAttribute(class *refClass, name string) bool {
	for parent := class.parent; parent != ""; parent = s.classes[parent].parent {
		ancestor := s.classes[parent]
		if ancestor.decl != nil {
			for _, feature := range ancestor.decl.Features {
				if attr, ok := feature.(*AttributeDecl); ok && attr.Name == name {
					return true
				}
			}
		} else if _, ok := ancestor.attrTypes[name]; ok {
			return true
		}
	}
	return false
}

// inheritedMethod finds the nearest ancestor's declaration of a method,
// reading declarations directly so class order does not matter.
func (s *refSemant) inheritedMethod(class *refClass, name string) *refMethod {
	for parent := class.parent; parent != ""; parent = s.classes[parent].parent {
		ancestor := s.classes[parent]
		if ancestor.decl == nil {
			if method := ancestor.methods[name]; method != nil {
				return method
			}
			continue
		}
		for _, feature := range ancestor.decl.Features {
			if method, ok := feature.(*MethodDecl); ok && method.Name == name {
				return declaredMethod(method)
			}
		}
	}
	return nil
}

func declaredMethod(decl *MethodDecl) *refMethod {
	method := &refMethod{name: decl.Name, returnType: decl.ReturnType}
	for _, formal := range decl.Formals {
		method.formalNames = append(method.formalNames, formal.Name)
		method.formalTypes = append(method.formalTypes, formal.TypeName)
	}
	return method
}

func (s *refSemant) collectFeatures(class *refClass, decl *ClassDecl) {
	if class == nil || class.decl != decl {
		return
	}
	for _, feature := range decl.Features {
		switch f := feature.(type) {
		case *AttributeDecl:
			line := f.NameSpan.StartLine
			switch {
			case f.Name == "self":
				s.errorAt(decl, line, "'self' cannot be the name of an attribute.")
				continue
			case class.attrTypes[f.Name] != "":
				s.errorAt(decl, line, "Attribute %s is multiply defined in class.", f.Name)
				continue
			case s.inheritedAttribute(class, f.Name):
				s.errorAt(decl, line, "Attribute %s is an attribute of an inherited class.", f.Name)
				continue
			}
			if !s.defined(f.TypeName) {
				s.errorAt(decl, line, "Class %s of attribute %s is undefined.", f.TypeName, f.Name)
			}
			class.attrOrder = append(class.attrOrder, f.Name)
			class.attrTypes[f.Name] = f.TypeName
		case *MethodDecl:
			line := f.NameSpan.StartLine
			if class.methods[f.Name] != nil {
				s.errorAt(decl, line, "Method %s is multiply defined.", f.Name)
				continue
			}
			seen := map[string]bool{}
			for _, formal := range f.Formals {
				formalLine := formal.NameSpan.StartLine
				switch {
				case formal.Name == "self":
					s.errorAt(decl, formalLine, "'self' cannot be the name of a formal parameter.")
				case seen[formal.Name]:
					s.errorAt(decl, formalLine, "Formal parameter %s is multiply defined.", formal.Name)
				}
				seen[formal.Name] = true
				if formal.TypeName == typeSelf {
					s.errorAt(decl, formalLine, "Formal parameter %s cannot have type SELF_TYPE.", formal.Name)
				} else if !s.defined(formal.TypeName) {
					s.errorAt(decl, formalLine, "Class %s of formal parameter %s is undefined.", formal.TypeName, formal.Name)
				}
			}
			if !s.defined(f.ReturnType) {
				s.errorAt(decl, line, "Undefined return type %s in method %s.", f.ReturnType, f.Name)
			}
			method := declaredMethod(f)
			if inherited := s.inheritedMethod(class, f.Name); inherited != nil {
				switch {
				case len(inherited.formalTypes) != len(method.formalTypes):
					s.errorAt(decl, line, "Incompatible number of formal parameters in redefined method %s.", f.Name)
				default:
					for index, formalType := range method.formalTypes {
						if formalType != inherited.formalTypes[index] {
							s.errorAt(decl, line, "In redefined method %s, parameter type %s is different from original type %s", f.Name, formalType, inherited.formalTypes[index])
						}
					}
					if method.returnType != inherited.returnType {
						s.errorAt(decl, line, "In redefined method %s, return type %s is different from original return type %s.", f.Name, method.returnType, inherited.returnType)
					}
				}
			}
			class.methods[f.Name] = method
		}
	}
}

func (s *refSemant) checkMain() {
	main := s.classes["Main"]
	if s.lookupMethod("Main", "main") == nil {
		s.errorAt(main.decl, main.decl.Span.StartLine, "No 'main' method in class Main.")
	}
}

func (s *refSemant) lookupMethod(className, name string) *refMethod {
	for current := className; current != ""; {
		class := s.classes[current]
		if class == nil {
			return nil
		}
		if method := class.methods[name]; method != nil {
			return method
		}
		current = class.parent
	}
	return nil
}

func (s *refSemant) lookupAttribute(className, name string) (string, bool) {
	for current := className; current != ""; {
		class := s.classes[current]
		if class == nil {
			return "", false
		}
		if typeName, ok := class.attrTypes[name]; ok && !class.basic {
			return typeName, true
		}
		current = class.parent
	}
	return "", false
}

func (s *refSemant) checkClassBodies(class *refClass) {
	if class == nil || class.decl == nil {
		return
	}
	s.current = class
	for _, feature := range class.decl.Features {
		s.scopes = []map[string]string{{}}
		switch f := feature.(type) {
		case *AttributeDecl:
			if f.Init == nil {
				continue
			}
			actual := s.expr(f.Init)
			if s.defined(f.TypeName) && !s.conforms(actual, f.TypeName) {
				s.errorAt(class.decl, f.NameSpan.StartLine, "Inferred type %s of initialization of attribute %s does not conform to declared type %s.", actual, f.Name, f.TypeName)
			}
		case *MethodDecl:
			for _, formal := range f.Formals {
				if formal.Name != "self" {
					s.scopes[0][formal.Name] = formal.TypeName
				}
			}
			actual := s.expr(f.Body)
			if s.defined(f.ReturnType) && !s.conforms(actual, f.ReturnType) {
				s.errorAt(class.decl, f.NameSpan.StartLine, "Inferred return type %s of method %s does not conform to declared return type %s.", actual, f.Name, f.ReturnType)
			}
		}
	}
}

func (s *refSemant) lookupVariable(name string) (string, bool) {
	for index := len(s.scopes) - 1; index >= 0; index-- {
		if typeName, ok := s.scopes[index][name]; ok {
			return typeName, true
		}
	}
	return s.lookupAttribute(s.current.name, name)
}

func (s *refSemant) resolve(typeName string) string {
	if typeName == typeSelf {
		return s.current.name
	}
	return typeName
}

func (s *refSemant) conforms(actual, expected string) bool {
	if actual == refNoType {
		return true
	}
	if expected == typeSelf {
		return actual == typeSelf
	}
	actual = s.resolve(actual)
	for current := actual; current != ""; {
		if current == expected {
			return true
		}
		class := s.classes[current]
		if class == nil {
			return false
		}
		current = class.parent
	}
	return false
}

func (s *refSemant) lub(left, right string) string {
	if left == typeSelf && right == typeSelf {
		return typeSelf
	}
	left, right = s.resolve(left), s.resolve(right)
	ancestors := map[string]bool{}
	for current := left; current != ""; {
		ancestors[current] = true
		class := s.classes[current]
		if class == nil {
			break
		}
		current = class.parent
	}
	for current := right; current != ""; {
		if ancestors[current] {
			return current
		}
		class := s.classes[current]
		if class == nil {
			break
		}
		current = class.parent
	}
	return typeObject
}

func (s *refSemant) set(expr Expr, typeName string) string {
	s.types[expr] = typeName
	return typeName
}

func (s *refSemant) expr(expr Expr) string {
	switch e := expr.(type) {
	case *IntExpr:
		return s.set(e, typeInt)
	case *StringExpr:
		return s.set(e, typeString)
	case *BoolExpr:
		return s.set(e, typeBool)
	case *NoExpr:
		return s.set(e, refNoType)
	case *IdentifierExpr:
		if e.Name == "self" {
			return s.set(e, typeSelf)
		}
		if typeName, ok := s.lookupVariable(e.Name); ok {
			return s.set(e, typeName)
		}
		s.exprError(e, "Undeclared identifier %s.", e.Name)
		return s.set(e, typeObject)
	case *AssignExpr:
		valueType := s.expr(e.Value)
		if e.Name == "self" {
			s.exprError(e, "Cannot assign to 'self'.")
			return s.set(e, valueType)
		}
		declared, ok := s.lookupVariable(e.Name)
		if !ok {
			s.exprError(e, "Assignment to undeclared variable %s.", e.Name)
			return s.set(e, valueType)
		}
		if !s.conforms(valueType, declared) {
			s.exprError(e, "Type %s of assigned expression does not conform to declared type %s of identifier %s.", valueType, declared, e.Name)
		}
		return s.set(e, valueType)
	case *DispatchExpr:
		return s.set(e, s.dispatch(e))
	case *IfExpr:
		if s.expr(e.Condition) != typeBool {
			s.exprError(e, "Predicate of 'if' does not have type Bool.")
		}
		return s.set(e, s.lub(s.expr(e.Then), s.expr(e.Else)))
	case *WhileExpr:
		if s.expr(e.Condition) != typeBool {
			s.exprError(e, "Loop condition does not have type Bool.")
		}
		s.expr(e.Body)
		return s.set(e, typeObject)
	case *BlockExpr:
		result := typeObject
		for _, child := range e.Expressions {
			result = s.expr(child)
		}
		return s.set(e, result)
	case *LetExpr:
		return s.set(e, s.let(e, 0))
	case *CaseExpr:
		s.expr(e.Value)
		seen := map[string]bool{}
		result := ""
		for _, branch := range e.Branches {
			line := branch.NameSpan.StartLine
			branchType := branch.TypeName
			switch {
			case branch.Name == "self":
				s.errorAt(s.current.decl, line, "'self' bound in 'case'.")
			case branch.TypeName == typeSelf:
				s.errorAt(s.current.decl, line, "Identifier %s declared with type SELF_TYPE in case branch.", branch.Name)
				branchType = typeObject
			case !s.defined(branch.TypeName):
				s.errorAt(s.current.decl, line, "Class %s of case branch is undefined.", branch.TypeName)
				branchType = typeObject
			}
			if seen[branch.TypeName] {
				s.errorAt(s.current.decl, line, "Duplicate branch %s in case statement.", branch.TypeName)
			}
			seen[branch.TypeName] = true
			s.scopes = append(s.scopes, map[string]string{})
			if branch.Name != "self" {
				s.scopes[len(s.scopes)-1][branch.Name] = branchType
			}
			bodyType := s.expr(branch.Body)
			s.scopes = s.scopes[:len(s.scopes)-1]
			if result == "" {
				result = bodyType
			} else {
				result = s.lub(result, bodyType)
			}
		}
		if result == "" {
			result = typeObject
		}
		return s.set(e, result)
	case *NewExpr:
		if !s.defined(e.TypeName) {
			s.exprError(e, "'new' used with undefined class %s.", e.TypeName)
			return s.set(e, typeObject)
		}
		return s.set(e, e.TypeName)
	case *IsVoidExpr:
		s.expr(e.Value)
		return s.set(e, typeBool)
	case *UnaryExpr:
		valueType := s.expr(e.Value)
		if e.Operator == TokenTilde {
			if valueType != typeInt {
				s.exprError(e, "Argument of '~' has type %s instead of Int.", valueType)
			}
			return s.set(e, typeInt)
		}
		if valueType != typeBool {
			s.exprError(e, "Argument of 'not' has type %s instead of Bool.", valueType)
		}
		return s.set(e, typeBool)
	case *BinaryExpr:
		left, right := s.expr(e.Left), s.expr(e.Right)
		symbol := map[TokenType]string{TokenPlus: "+", TokenMinus: "-", TokenStar: "*", TokenSlash: "/", TokenLess: "<", TokenLessEqual: "<="}[e.Operator]
		switch e.Operator {
		case TokenPlus, TokenMinus, TokenStar, TokenSlash:
			if left != typeInt || right != typeInt {
				s.exprError(e, "non-Int arguments: %s %s %s", left, symbol, right)
			}
			return s.set(e, typeInt)
		case TokenLess, TokenLessEqual:
			if left != typeInt || right != typeInt {
				s.exprError(e, "non-Int arguments: %s %s %s", left, symbol, right)
			}
			return s.set(e, typeBool)
		default:
			if (isBasicComparable(left) || isBasicComparable(right)) && left != right {
				s.exprError(e, "Illegal comparison with a basic type.")
			}
			return s.set(e, typeBool)
		}
	}
	return s.set(expr, typeObject)
}

func (s *refSemant) let(e *LetExpr, index int) string {
	binding := e.Bindings[index]
	line := s.lineOf(e)
	if index > 0 {
		line = binding.NameSpan.StartLine
	}
	declared := binding.TypeName
	if binding.Init != nil {
		initType := s.expr(binding.Init)
		if s.defined(declared) && !s.conforms(initType, declared) {
			s.errorAt(s.current.decl, line, "Inferred type %s of initialization of %s does not conform to identifier's declared type %s.", initType, binding.Name, declared)
		}
	}
	if binding.Name == "self" {
		s.errorAt(s.current.decl, line, "'self' cannot be bound in a 'let' expression.")
	}
	if !s.defined(declared) {
		s.errorAt(s.current.decl, line, "Class %s of let-bound identifier %s is undefined.", declared, binding.Name)
		declared = typeObject
	}
	s.scopes = append(s.scopes, map[string]string{})
	if binding.Name != "self" {
		s.scopes[len(s.scopes)-1][binding.Name] = declared
	}
	var result string
	if index+1 < len(e.Bindings) {
		result = s.let(e, index+1)
	} else {
		result = s.expr(e.Body)
	}
	s.scopes = s.scopes[:len(s.scopes)-1]
	return result
}

// dispatch type-checks the receiver and every argument before looking the
// method up, and reports a wrong argument count without per-argument
// conformance errors, as the course semant does.
func (s *refSemant) dispatch(e *DispatchExpr) string {
	receiverType := typeSelf
	if e.Receiver != nil {
		receiverType = s.expr(e.Receiver)
	}
	argumentTypes := make([]string, len(e.Arguments))
	for index, argument := range e.Arguments {
		argumentTypes[index] = s.expr(argument)
	}
	lookup := s.resolve(receiverType)
	if e.StaticTypeName != "" {
		if e.StaticTypeName == typeSelf || s.classes[e.StaticTypeName] == nil {
			s.exprError(e, "Static dispatch to undefined class %s.", e.StaticTypeName)
			return typeObject
		}
		if !s.conforms(receiverType, e.StaticTypeName) {
			s.exprError(e, "Expression type %s does not conform to declared static dispatch type %s.", receiverType, e.StaticTypeName)
			return typeObject
		}
		lookup = e.StaticTypeName
	}
	method := s.lookupMethod(lookup, e.Method)
	if method == nil {
		if e.StaticTypeName != "" {
			s.exprError(e, "Static dispatch to undefined method %s.", e.Method)
		} else {
			s.exprError(e, "Dispatch to undefined method %s.", e.Method)
		}
		return typeObject
	}
	if len(method.formalTypes) != len(argumentTypes) {
		s.exprError(e, "Method %s called with wrong number of arguments.", e.Method)
	} else {
		for index, argumentType := range argumentTypes {
			if !s.conforms(argumentType, method.formalTypes[index]) {
				s.exprError(e, "In call of method %s, type %s of parameter %s does not conform to declared type %s.", e.Method, argumentType, method.formalNames[index], method.formalTypes[index])
			}
		}
	}
	if method.returnType == typeSelf {
		return receiverType
	}
	return method.returnType
}
