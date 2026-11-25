// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"fmt"
	"sort"
	"strings"
)

const (
	typeObject  = "Object"
	typeIO      = "IO"
	typeInt     = "Int"
	typeString  = "String"
	typeBool    = "Bool"
	typeSelf    = "SELF_TYPE"
	typeNo      = "<no_type>"
	typeInvalid = "<error_type>"
)

type AttrInfo struct {
	Name     string
	TypeName string
	Owner    string
	Decl     *AttributeDecl
	Builtin  bool
	Span     Span
}

type MethodInfo struct {
	Name        string
	Owner       string
	FormalNames []string
	FormalTypes []string
	ReturnType  string
	Decl        *MethodDecl
	Builtin     bool
	Span        Span
}

type ClassInfo struct {
	Name           string
	Parent         string
	Decl           *ClassDecl
	Builtin        bool
	Attributes     map[string]*AttrInfo
	AttributeOrder []string
	Methods        map[string]*MethodInfo
	MethodOrder    []string
	Span           Span
}

type ClassTable struct {
	Classes map[string]*ClassInfo
	Order   []string
}

func (t *ClassTable) Has(name string) bool {
	if name == typeSelf {
		return true
	}
	_, ok := t.Classes[name]
	return ok
}

func (t *ClassTable) LookupMethod(className, method string) (*MethodInfo, bool) {
	if className == typeSelf {
		return nil, false
	}
	visited := map[string]bool{}
	for className != "" && !visited[className] {
		visited[className] = true
		class, ok := t.Classes[className]
		if !ok {
			return nil, false
		}
		if info, found := class.Methods[method]; found {
			return info, true
		}
		className = class.Parent
	}
	return nil, false
}

func (t *ClassTable) LookupAttribute(className, name string) (*AttrInfo, bool) {
	visited := map[string]bool{}
	for className != "" && !visited[className] {
		visited[className] = true
		class, ok := t.Classes[className]
		if !ok {
			return nil, false
		}
		if info, found := class.Attributes[name]; found {
			return info, true
		}
		className = class.Parent
	}
	return nil, false
}

func (t *ClassTable) ParentOf(name string) string {
	if class, ok := t.Classes[name]; ok {
		return class.Parent
	}
	return ""
}

func (t *ClassTable) IsSubclass(child, parent string) bool {
	if child == parent {
		return true
	}
	visited := map[string]bool{}
	for child != "" && !visited[child] {
		visited[child] = true
		class, ok := t.Classes[child]
		if !ok {
			return false
		}
		child = class.Parent
		if child == parent {
			return true
		}
	}
	return false
}

// Distance returns the number of inheritance edges from child to parent.
func (t *ClassTable) Distance(child, parent string) (int, bool) {
	if child == parent {
		return 0, true
	}
	distance := 0
	visited := map[string]bool{}
	for child != "" && !visited[child] {
		visited[child] = true
		class, ok := t.Classes[child]
		if !ok {
			return 0, false
		}
		child = class.Parent
		distance++
		if child == parent {
			return distance, true
		}
	}
	return 0, false
}

type Analysis struct {
	Table       *ClassTable
	Symbols     []Symbol
	Diagnostics []Diagnostic
	Classes     int
	Methods     int
	Attributes  int
}

type analyzer struct {
	program          *Program
	table            *ClassTable
	symbols          []Symbol
	diagnostics      []Diagnostic
	currentClass     string
	suggestionBudget int
}

func Analyze(program *Program) Analysis {
	a := &analyzer{program: program, table: newClassTable(), suggestionBudget: 16384}
	a.installBuiltinSymbols()
	if program == nil {
		return Analysis{Table: a.table, Symbols: a.symbols}
	}
	a.collectClasses()
	a.validateInheritance()
	a.collectFeatures()
	a.validateFeatureInheritance()
	a.validateMain()
	a.checkBodies()

	classes, methods, attributes := 0, 0, 0
	for _, name := range a.table.Order {
		class := a.table.Classes[name]
		if class.Builtin {
			continue
		}
		classes++
		methods += len(class.Methods)
		attributes += len(class.Attributes)
	}
	return Analysis{
		Table: a.table, Symbols: a.symbols, Diagnostics: a.diagnostics,
		Classes: classes, Methods: methods, Attributes: attributes,
	}
}

func newClassTable() *ClassTable {
	t := &ClassTable{Classes: map[string]*ClassInfo{}}
	addClass := func(name, parent string) *ClassInfo {
		info := &ClassInfo{
			Name: name, Parent: parent, Builtin: true, Span: emptySpan(),
			Attributes: map[string]*AttrInfo{}, Methods: map[string]*MethodInfo{},
		}
		t.Classes[name] = info
		t.Order = append(t.Order, name)
		return info
	}
	object := addClass(typeObject, "")
	io := addClass(typeIO, typeObject)
	integer := addClass(typeInt, typeObject)
	str := addClass(typeString, typeObject)
	boolean := addClass(typeBool, typeObject)

	addMethod := func(class *ClassInfo, name string, formalNames, formalTypes []string, returnType string) {
		class.Methods[name] = &MethodInfo{
			Name: name, Owner: class.Name, FormalNames: formalNames,
			FormalTypes: formalTypes, ReturnType: returnType,
			Builtin: true, Span: emptySpan(),
		}
		class.MethodOrder = append(class.MethodOrder, name)
	}
	addMethod(object, "abort", nil, nil, typeObject)
	addMethod(object, "type_name", nil, nil, typeString)
	addMethod(object, "copy", nil, nil, typeSelf)
	addMethod(io, "out_string", []string{"x"}, []string{typeString}, typeSelf)
	addMethod(io, "out_int", []string{"x"}, []string{typeInt}, typeSelf)
	addMethod(io, "in_string", nil, nil, typeString)
	addMethod(io, "in_int", nil, nil, typeInt)
	addMethod(str, "length", nil, nil, typeInt)
	addMethod(str, "concat", []string{"s"}, []string{typeString}, typeString)
	addMethod(str, "substr", []string{"i", "l"}, []string{typeInt, typeInt}, typeString)

	_ = integer
	_ = boolean
	return t
}

func (a *analyzer) installBuiltinSymbols() {
	for _, className := range a.table.Order {
		class := a.table.Classes[className]
		a.addSymbol(className, "class", className, false, true, "<prelude>", className, class.Span)
		for _, name := range orderedMethodNames(class) {
			method := class.Methods[name]
			a.addSymbol(method.Name, "builtin method", methodSignature(method), false, true, className, className, method.Span)
		}
	}
}

func (a *analyzer) collectClasses() {
	for _, decl := range a.program.Classes {
		if decl.Name == typeSelf {
			a.error("COL3001", "SELF_TYPE cannot be used as a class name", "Choose a distinct class name beginning with an uppercase letter.", decl.NameSpan)
			continue
		}
		if existing, found := a.table.Classes[decl.Name]; found {
			d := errorDiagnostic("semantic", "COL3002", fmt.Sprintf("class %s is defined more than once", decl.Name), "Rename or remove the duplicate class declaration.", decl.NameSpan)
			d = withRelated(d, "first definition", existing.Span)
			d = withQuickFix(d, "Rename duplicate declaration", decl.Name+"2", decl.NameSpan)
			a.diagnostics = append(a.diagnostics, d)
			continue
		}
		info := &ClassInfo{
			Name: decl.Name, Parent: decl.Parent, Decl: decl, Span: decl.Span,
			Attributes: map[string]*AttrInfo{}, Methods: map[string]*MethodInfo{},
		}
		a.table.Classes[decl.Name] = info
		a.table.Order = append(a.table.Order, decl.Name)
		a.addSymbol(decl.Name, "class", decl.Name, false, true, "global", decl.Name, decl.NameSpan)
	}
}

func (a *analyzer) validateInheritance() {
	for _, decl := range a.program.Classes {
		class, ok := a.table.Classes[decl.Name]
		if !ok || class.Decl != decl {
			continue
		}
		parent := class.Parent
		if parent == "" {
			parent = typeObject
			class.Parent = parent
		}
		switch parent {
		case typeInt, typeString, typeBool, typeSelf:
			a.error("COL3010", fmt.Sprintf("class %s cannot inherit from %s", class.Name, parent), "Inherit from Object, IO, or another user-defined class.", decl.ParentSpan)
			class.Parent = typeObject
		default:
			if _, found := a.table.Classes[parent]; !found {
				a.error("COL3011", fmt.Sprintf("class %s inherits from undefined class %s", class.Name, parent), "Declare the parent class or inherit from Object.", decl.ParentSpan)
				a.suggestType(parent, decl.ParentSpan, "parent", class.Name)
				class.Parent = typeObject
			}
		}
	}

	resolved := map[string]bool{}
	for _, name := range a.table.Order {
		if resolved[name] {
			continue
		}
		path := []string{}
		positions := map[string]int{}
		current := name
		for current != "" && !resolved[current] {
			if cycleStart, found := positions[current]; found {
				members := append([]string(nil), path[cycleStart:]...)
				cycle := append(append([]string(nil), members...), current)
				class := a.table.Classes[current]
				span := class.Span
				if class.Decl != nil {
					span = class.Decl.ParentSpan
				}
				d := errorDiagnostic("semantic", "COL3012", "inheritance cycle: "+strings.Join(cycle, " -> "), "Break the cycle by changing at least one inherits clause.", span)
				for _, member := range members {
					info := a.table.Classes[member]
					if info == nil || info.Builtin {
						continue
					}
					if info.Decl != nil && member != current {
						d = withRelated(d, "cycle member "+member, info.Decl.ParentSpan)
					}
					info.Parent = typeObject
				}
				a.diagnostics = append(a.diagnostics, d)
				break
			}
			positions[current] = len(path)
			path = append(path, current)
			class := a.table.Classes[current]
			if class == nil {
				break
			}
			current = class.Parent
		}
		for _, visited := range path {
			resolved[visited] = true
		}
	}
}

func (a *analyzer) collectFeatures() {
	for _, decl := range a.program.Classes {
		class, ok := a.table.Classes[decl.Name]
		if !ok || class.Decl != decl {
			continue
		}
		for _, feature := range decl.Features {
			switch f := feature.(type) {
			case *AttributeDecl:
				a.collectAttribute(class, f)
			case *MethodDecl:
				a.collectMethod(class, f)
			}
		}
	}
}

func (a *analyzer) collectAttribute(class *ClassInfo, attr *AttributeDecl) {
	if attr.Name == "self" {
		a.error("COL3020", "'self' cannot be the name of an attribute", "Choose another lowercase identifier.", attr.NameSpan)
	}
	if !a.table.Has(attr.TypeName) {
		a.error("COL3021", fmt.Sprintf("attribute %s has undefined type %s", attr.Name, attr.TypeName), "Use a declared class or SELF_TYPE.", attr.TypeSpan)
		a.suggestType(attr.TypeName, attr.TypeSpan, "self", "")
	}
	if previous, found := class.Attributes[attr.Name]; found {
		d := errorDiagnostic("semantic", "COL3022", fmt.Sprintf("attribute %s is defined more than once in class %s", attr.Name, class.Name), "Remove or rename one attribute.", attr.NameSpan)
		d = withRelated(d, "first definition", previous.Span)
		a.diagnostics = append(a.diagnostics, d)
		return
	}
	info := &AttrInfo{Name: attr.Name, TypeName: attr.TypeName, Owner: class.Name, Decl: attr, Span: attr.Span}
	class.Attributes[attr.Name] = info
	class.AttributeOrder = append(class.AttributeOrder, attr.Name)
	a.addSymbol(attr.Name, "attribute", attr.TypeName, true, attr.Init != nil, class.Name, class.Name, attr.NameSpan)
}

func (a *analyzer) collectMethod(class *ClassInfo, method *MethodDecl) {
	if previous, found := class.Methods[method.Name]; found {
		d := errorDiagnostic("semantic", "COL3030", fmt.Sprintf("method %s is defined more than once in class %s", method.Name, class.Name), "Remove or rename one method.", method.NameSpan)
		d = withRelated(d, "first definition", previous.Span)
		a.diagnostics = append(a.diagnostics, d)
		return
	}
	if !a.table.Has(method.ReturnType) {
		a.error("COL3031", fmt.Sprintf("method %s has undefined return type %s", method.Name, method.ReturnType), "Use a declared class or SELF_TYPE.", method.TypeSpan)
		a.suggestType(method.ReturnType, method.TypeSpan, "self", "")
	}
	formalNames := make([]string, 0, len(method.Formals))
	formalTypes := make([]string, 0, len(method.Formals))
	seen := map[string]Formal{}
	for _, formal := range method.Formals {
		if formal.Name == "self" {
			a.error("COL3032", "'self' cannot be the name of a formal parameter", "Choose another lowercase identifier.", formal.NameSpan)
		}
		if formal.TypeName == typeSelf {
			a.error("COL3033", "SELF_TYPE cannot be used as a formal parameter type", "Use the current class or another declared class.", formal.TypeSpan)
		} else if !a.table.Has(formal.TypeName) {
			a.error("COL3034", fmt.Sprintf("formal parameter %s has undefined type %s", formal.Name, formal.TypeName), "Use a declared class.", formal.TypeSpan)
			a.suggestType(formal.TypeName, formal.TypeSpan, "concrete", "")
		}
		if previous, found := seen[formal.Name]; found {
			d := errorDiagnostic("semantic", "COL3035", fmt.Sprintf("formal parameter %s is repeated", formal.Name), "Formal parameter names must be unique within a method.", formal.NameSpan)
			d = withRelated(d, "first parameter", previous.NameSpan)
			a.diagnostics = append(a.diagnostics, d)
		} else {
			seen[formal.Name] = formal
		}
		formalNames = append(formalNames, formal.Name)
		formalTypes = append(formalTypes, formal.TypeName)
	}
	info := &MethodInfo{
		Name: method.Name, Owner: class.Name, FormalNames: formalNames,
		FormalTypes: formalTypes, ReturnType: method.ReturnType, Decl: method, Span: method.Span,
	}
	class.Methods[method.Name] = info
	class.MethodOrder = append(class.MethodOrder, method.Name)
	a.addSymbol(method.Name, "method", methodSignature(info), false, true, class.Name, class.Name, method.NameSpan)
}

func orderedMethodNames(class *ClassInfo) []string {
	if class == nil {
		return nil
	}
	if len(class.MethodOrder) == len(class.Methods) {
		return append([]string(nil), class.MethodOrder...)
	}
	names := make([]string, 0, len(class.Methods))
	for name := range class.Methods {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (a *analyzer) validateFeatureInheritance() {
	for _, name := range a.table.Order {
		class := a.table.Classes[name]
		if class.Builtin || class.Parent == "" {
			continue
		}
		for _, attr := range orderedAttributes(class) {
			if inherited, found := a.lookupAttributeFrom(class.Parent, attr.Name); found {
				d := errorDiagnostic("semantic", "COL3040", fmt.Sprintf("attribute %s in class %s redefines an inherited attribute", attr.Name, class.Name), "Inherited attributes cannot be redefined in COOL.", attr.Span)
				d = withRelated(d, "inherited attribute", inherited.Span)
				a.diagnostics = append(a.diagnostics, d)
			}
		}
		methodNames := orderedMethodNames(class)
		for _, methodName := range methodNames {
			method := class.Methods[methodName]
			inherited, found := a.lookupMethodFrom(class.Parent, methodName)
			if !found {
				continue
			}
			if method.ReturnType != inherited.ReturnType || !equalStrings(method.FormalTypes, inherited.FormalTypes) {
				d := errorDiagnostic("semantic", "COL3041", fmt.Sprintf("method %s in class %s does not match the inherited signature", methodName, class.Name), "An overriding method must preserve every formal type and the return type exactly.", method.Span)
				d = withRelated(d, "inherited method", inherited.Span)
				a.diagnostics = append(a.diagnostics, d)
			}
		}
	}
}

func (a *analyzer) validateMain() {
	mainClass, found := a.table.Classes["Main"]
	if !found || mainClass.Builtin {
		span := a.program.Span
		a.error("COL3050", "program does not define class Main", "Add 'class Main { main() : Object { 0 }; };'.", span)
		return
	}
	mainMethod, found := mainClass.Methods["main"]
	if !found {
		a.error("COL3051", "class Main does not define method main", "Add a parameterless 'main()' method to class Main.", mainClass.Span)
		return
	}
	if len(mainMethod.FormalTypes) != 0 {
		a.error("COL3052", "method Main.main must not declare formal parameters", "Remove the parameters from main().", mainMethod.Span)
	}
}

func (a *analyzer) checkBodies() {
	for _, name := range a.table.Order {
		class := a.table.Classes[name]
		if class.Builtin {
			continue
		}
		a.currentClass = class.Name
		for _, attr := range orderedAttributes(class) {
			if attr.Decl == nil || attr.Decl.Init == nil {
				continue
			}
			env := newTypeEnv()
			actual := a.checkExpr(attr.Decl.Init, env, class.Name+".<attribute "+attr.Name+">")
			if actual != typeInvalid && a.table.Has(attr.TypeName) && !a.conforms(actual, attr.TypeName) {
				a.typeError("COL3060", fmt.Sprintf("initializer type %s does not conform to declared type %s of attribute %s", actual, attr.TypeName, attr.Name), attr.Decl.Init.GetSpan())
			}
		}
		methodNames := orderedMethodNames(class)
		for _, methodName := range methodNames {
			method := class.Methods[methodName]
			if method.Decl == nil {
				continue
			}
			a.checkMethod(class, method)
		}
	}
}

func (a *analyzer) checkMethod(class *ClassInfo, method *MethodInfo) {
	env := newTypeEnv()
	scope := class.Name + "." + method.Name
	for _, formal := range method.Decl.Formals {
		if formal.Name != "self" {
			formalType := formal.TypeName
			if formalType == typeSelf || !a.table.Has(formalType) {
				formalType = typeInvalid
			}
			env.Define(formal.Name, formalType)
		}
		a.addSymbol(formal.Name, "formal", formal.TypeName, false, true, scope, class.Name, formal.NameSpan)
	}
	actual := a.checkExpr(method.Decl.Body, env, scope)
	if actual != typeInvalid && a.table.Has(method.ReturnType) && !a.conforms(actual, method.ReturnType) {
		d := errorDiagnostic("semantic", "COL3061", fmt.Sprintf("body type %s does not conform to declared return type %s of method %s", actual, method.ReturnType, method.Name), "Change the body expression or the declared return type.", method.Decl.Body.GetSpan())
		d = withRelated(d, "declared return type", method.Decl.TypeSpan)
		a.diagnostics = append(a.diagnostics, d)
	}
}

type typeEnv struct {
	scopes []map[string]*localBinding
}

type localBinding struct {
	typeName string
	read     bool
}

func newTypeEnv() *typeEnv {
	return &typeEnv{scopes: []map[string]*localBinding{{}}}
}
func (e *typeEnv) Push() { e.scopes = append(e.scopes, map[string]*localBinding{}) }
func (e *typeEnv) Pop() {
	if len(e.scopes) > 1 {
		e.scopes = e.scopes[:len(e.scopes)-1]
	}
}
func (e *typeEnv) Define(name, typeName string) *localBinding {
	binding := &localBinding{typeName: typeName}
	e.scopes[len(e.scopes)-1][name] = binding
	return binding
}
func (e *typeEnv) Read(name string) {
	for i := len(e.scopes) - 1; i >= 0; i-- {
		if binding := e.scopes[i][name]; binding != nil {
			binding.read = true
			return
		}
	}
}
func (e *typeEnv) Lookup(name string) (string, bool) {
	for i := len(e.scopes) - 1; i >= 0; i-- {
		if value, ok := e.scopes[i][name]; ok {
			return value.typeName, true
		}
	}
	return "", false
}

func (a *analyzer) checkExpr(expr Expr, env *typeEnv, scope string) string {
	if expr == nil {
		return typeNo
	}
	set := func(t string) string {
		expr.SetType(t)
		return t
	}
	switch e := expr.(type) {
	case *NoExpr:
		return set(typeNo)
	case *IntExpr:
		return set(typeInt)
	case *StringExpr:
		return set(typeString)
	case *BoolExpr:
		return set(typeBool)
	case *IdentifierExpr:
		env.Read(e.Name)
		if e.Name == "self" {
			return set(typeSelf)
		}
		if t, found := env.Lookup(e.Name); found {
			if !a.table.Has(t) {
				return set(typeInvalid)
			}
			return set(t)
		}
		if attr, found := a.table.LookupAttribute(a.currentClass, e.Name); found {
			if !a.table.Has(attr.TypeName) {
				return set(typeInvalid)
			}
			return set(attr.TypeName)
		}
		a.error("COL3100", fmt.Sprintf("identifier %s is not declared", e.Name), "Declare it in a let binding, formal parameter, or attribute.", e.GetSpan())
		a.suggestVariable(e.Name, e.NameSpan, env, false)
		return set(typeInvalid)
	case *AssignExpr:
		valueType := a.checkExpr(e.Value, env, scope)
		if e.Name == "self" {
			a.error("COL3101", "cannot assign to 'self'", "Assign to an attribute or local binding instead.", e.NameSpan)
			return set(typeInvalid)
		}
		declared, found := env.Lookup(e.Name)
		if !found {
			if attr, attrFound := a.table.LookupAttribute(a.currentClass, e.Name); attrFound {
				declared, found = attr.TypeName, true
			}
		}
		if !found {
			a.error("COL3102", fmt.Sprintf("assignment target %s is not declared", e.Name), "Declare the variable or attribute before assigning it.", e.NameSpan)
			a.suggestVariable(e.Name, e.NameSpan, env, true)
			return set(typeInvalid)
		}
		if valueType == typeInvalid || !a.table.Has(declared) {
			return set(typeInvalid)
		}
		if !a.conforms(valueType, declared) {
			a.typeError("COL3103", fmt.Sprintf("assigned type %s does not conform to declared type %s of %s", valueType, declared, e.Name), e.Value.GetSpan())
			return set(typeInvalid)
		}
		// A direct identifier has no intervening binder or receiver: equal
		// names resolve through the same environment/attribute lookup here.
		if value, ok := e.Value.(*IdentifierExpr); ok && value.Name == e.Name {
			a.selfAssignment(e)
		}
		return set(valueType)
	case *DispatchExpr:
		return set(a.checkDispatch(e, env, scope))
	case *IfExpr:
		if condition, ok := e.Condition.(*BoolExpr); ok {
			dead := e.Then
			if condition.Value {
				dead = e.Else
			}
			a.unreachableBranch(dead.GetSpan())
		}
		conditionType := a.checkExpr(e.Condition, env, scope)
		valid := conditionType == typeBool
		if conditionType != typeBool && conditionType != typeInvalid {
			a.typeError("COL3110", fmt.Sprintf("if condition has type %s instead of Bool", conditionType), e.Condition.GetSpan())
		}
		thenType := a.checkExpr(e.Then, env, scope)
		elseType := a.checkExpr(e.Else, env, scope)
		result := a.join(thenType, elseType)
		if !valid || result == typeInvalid {
			return set(typeInvalid)
		}
		return set(result)
	case *WhileExpr:
		if condition, ok := e.Condition.(*BoolExpr); ok && !condition.Value {
			a.unreachableBranch(e.Body.GetSpan())
		}
		conditionType := a.checkExpr(e.Condition, env, scope)
		if conditionType != typeBool && conditionType != typeInvalid {
			a.typeError("COL3111", fmt.Sprintf("while condition has type %s instead of Bool", conditionType), e.Condition.GetSpan())
		}
		bodyType := a.checkExpr(e.Body, env, scope)
		if conditionType != typeBool || bodyType == typeInvalid {
			return set(typeInvalid)
		}
		return set(typeObject)
	case *BlockExpr:
		result := typeObject
		invalid := false
		for _, child := range e.Expressions {
			result = a.checkExpr(child, env, scope)
			invalid = invalid || result == typeInvalid
		}
		if invalid {
			return set(typeInvalid)
		}
		return set(result)
	case *LetExpr:
		env.Push()
		defer env.Pop()
		invalid := false
		bindings := make([]*localBinding, len(e.Bindings))
		for index, binding := range e.Bindings {
			if binding.Name == "self" {
				a.error("COL3120", "'self' cannot be bound in a let expression", "Choose another binding name.", binding.NameSpan)
				invalid = true
			}
			knownType := a.table.Has(binding.TypeName)
			if !knownType {
				a.error("COL3121", fmt.Sprintf("let binding %s has undefined type %s", binding.Name, binding.TypeName), "Use a declared class or SELF_TYPE.", binding.TypeSpan)
				a.suggestType(binding.TypeName, binding.TypeSpan, "self", "")
				invalid = true
			}
			if binding.Init != nil {
				initType := a.checkExpr(binding.Init, env, scope)
				if initType == typeInvalid {
					invalid = true
				} else if knownType && !a.conforms(initType, binding.TypeName) {
					a.typeError("COL3122", fmt.Sprintf("initializer type %s does not conform to declared type %s of let binding %s", initType, binding.TypeName, binding.Name), binding.Init.GetSpan())
					invalid = true
				}
			}
			if binding.Name != "self" {
				declaredType := binding.TypeName
				if !knownType {
					declaredType = typeInvalid
				}
				bindings[index] = env.Define(binding.Name, declaredType)
			}
			a.addSymbol(binding.Name, "let", binding.TypeName, true, binding.Init != nil, scope+"/let", a.currentClass, binding.NameSpan)
		}
		bodyType := a.checkExpr(e.Body, env, scope)
		for index, binding := range bindings {
			if binding != nil && !binding.read {
				a.unusedBinding(e.Bindings[index].Name, e.Bindings[index].NameSpan)
			}
		}
		if invalid || bodyType == typeInvalid {
			return set(typeInvalid)
		}
		return set(bodyType)
	case *CaseExpr:
		invalid := a.checkExpr(e.Value, env, scope) == typeInvalid
		seenTypes := map[string]CaseBranch{}
		result := typeNo
		for _, branch := range e.Branches {
			if branch.Name == "self" {
				a.error("COL3130", "'self' cannot be bound by a case branch", "Choose another branch variable name.", branch.NameSpan)
				invalid = true
			}
			branchTypeName := branch.TypeName
			if branch.TypeName == typeSelf {
				a.error("COL3131", "SELF_TYPE cannot be used as a case branch type", "Use a concrete class name.", branch.TypeSpan)
				branchTypeName = typeInvalid
				invalid = true
			} else if !a.table.Has(branch.TypeName) {
				a.error("COL3132", fmt.Sprintf("case branch has undefined type %s", branch.TypeName), "Use a declared class.", branch.TypeSpan)
				a.suggestType(branch.TypeName, branch.TypeSpan, "concrete", "")
				branchTypeName = typeInvalid
				invalid = true
			}
			if previous, found := seenTypes[branch.TypeName]; found {
				d := errorDiagnostic("semantic", "COL3133", fmt.Sprintf("case expression repeats branch type %s", branch.TypeName), "Every branch type must be unique.", branch.TypeSpan)
				d = withRelated(d, "first branch", previous.TypeSpan)
				a.diagnostics = append(a.diagnostics, d)
				invalid = true
			} else {
				seenTypes[branch.TypeName] = branch
			}
			env.Push()
			var binding *localBinding
			if branch.Name != "self" {
				binding = env.Define(branch.Name, branchTypeName)
			}
			a.addSymbol(branch.Name, "case", branch.TypeName, false, true, scope+"/case", a.currentClass, branch.NameSpan)
			branchType := a.checkExpr(branch.Body, env, scope)
			if binding != nil && !binding.read {
				a.unusedBinding(branch.Name, branch.NameSpan)
			}
			env.Pop()
			invalid = invalid || branchType == typeInvalid
			result = a.join(result, branchType)
		}
		if result == typeNo {
			result = typeObject
		}
		if invalid || result == typeInvalid {
			return set(typeInvalid)
		}
		return set(result)
	case *NewExpr:
		if !a.table.Has(e.TypeName) {
			a.error("COL3140", fmt.Sprintf("cannot allocate undefined type %s", e.TypeName), "Declare the class or allocate SELF_TYPE.", e.TypeSpan)
			a.suggestType(e.TypeName, e.TypeSpan, "self", "")
			return set(typeInvalid)
		}
		return set(e.TypeName)
	case *IsVoidExpr:
		if a.checkExpr(e.Value, env, scope) == typeInvalid {
			return set(typeInvalid)
		}
		switch e.Value.(type) {
		case *IntExpr, *StringExpr, *BoolExpr, *NewExpr:
			a.redundantIsvoid(e.GetSpan())
		}
		return set(typeBool)
	case *UnaryExpr:
		valueType := a.checkExpr(e.Value, env, scope)
		if valueType == typeInvalid {
			return set(typeInvalid)
		}
		switch e.Operator {
		case TokenTilde:
			if valueType != typeInt {
				a.typeError("COL3150", fmt.Sprintf("'~' expects Int but received %s", valueType), e.Value.GetSpan())
				return set(typeInvalid)
			}
			return set(typeInt)
		case TokenNot:
			if valueType != typeBool {
				a.typeError("COL3151", fmt.Sprintf("'not' expects Bool but received %s", valueType), e.Value.GetSpan())
				return set(typeInvalid)
			}
			return set(typeBool)
		default:
			return set(typeObject)
		}
	case *BinaryExpr:
		left := a.checkExpr(e.Left, env, scope)
		right := a.checkExpr(e.Right, env, scope)
		if left == typeInvalid || right == typeInvalid {
			return set(typeInvalid)
		}
		switch e.Operator {
		case TokenPlus, TokenMinus, TokenStar, TokenSlash:
			if left != typeInt || right != typeInt {
				a.typeError("COL3160", fmt.Sprintf("arithmetic operator %s expects Int operands but received %s and %s", e.Operator, left, right), e.GetSpan())
				return set(typeInvalid)
			}
			if e.Operator == TokenSlash {
				if literal, ok := e.Right.(*IntExpr); ok && literal.Value == 0 {
					a.diagnostics = append(a.diagnostics, warningDiagnostic("semantic", "COL3161", "division by the constant zero will fail at runtime", "Use a nonzero divisor or guard the operation.", e.Right.GetSpan()))
				}
			}
			return set(typeInt)
		case TokenLess, TokenLessEqual:
			if left != typeInt || right != typeInt {
				a.typeError("COL3162", fmt.Sprintf("comparison operator %s expects Int operands but received %s and %s", e.Operator, left, right), e.GetSpan())
				return set(typeInvalid)
			}
			return set(typeBool)
		case TokenEqual:
			if isBasicComparable(left) || isBasicComparable(right) {
				if left != right {
					a.typeError("COL3163", fmt.Sprintf("equality between %s and %s is not allowed for basic COOL types", left, right), e.GetSpan())
					return set(typeInvalid)
				}
			}
			return set(typeBool)
		default:
			return set(typeObject)
		}
	default:
		return set(typeInvalid)
	}
}

func (a *analyzer) checkDispatch(e *DispatchExpr, env *typeEnv, scope string) string {
	receiverType := typeSelf
	if e.Receiver != nil {
		receiverType = a.checkExpr(e.Receiver, env, scope)
	}
	lookupClass := a.resolveSelf(receiverType)
	staticReturnReceiver := receiverType
	invalid := receiverType == typeInvalid
	if e.StaticTypeName != "" {
		if e.StaticTypeName == typeSelf {
			a.error("COL3170", "SELF_TYPE cannot be used as a static dispatch type", "Use a concrete ancestor class after '@'.", e.GetSpan())
			invalid = true
		} else if !a.table.Has(e.StaticTypeName) {
			a.error("COL3171", fmt.Sprintf("static dispatch uses undefined type %s", e.StaticTypeName), "Use a declared ancestor class.", e.GetSpan())
			a.suggestType(e.StaticTypeName, e.StaticTypeSpan, "ancestor", lookupClass)
			invalid = true
		} else {
			if receiverType != typeInvalid && !a.conforms(receiverType, e.StaticTypeName) {
				a.typeError("COL3172", fmt.Sprintf("receiver type %s does not conform to static dispatch type %s", receiverType, e.StaticTypeName), e.Receiver.GetSpan())
				invalid = true
			}
			lookupClass = e.StaticTypeName
			staticReturnReceiver = e.StaticTypeName
		}
	}
	if invalid {
		for _, arg := range e.Arguments {
			a.checkExpr(arg, env, scope)
		}
		return typeInvalid
	}
	method, found := a.table.LookupMethod(lookupClass, e.Method)
	if !found {
		a.error("COL3173", fmt.Sprintf("type %s has no method %s", lookupClass, e.Method), "Check the method name and the receiver's static type.", e.MethodSpan)
		a.suggestMethod(e.Method, e.MethodSpan, lookupClass)
		for _, arg := range e.Arguments {
			a.checkExpr(arg, env, scope)
		}
		return typeInvalid
	}
	if len(e.Arguments) != len(method.FormalTypes) {
		a.error("COL3174", fmt.Sprintf("method %s expects %d arguments but received %d", e.Method, len(method.FormalTypes), len(e.Arguments)), "Pass exactly one expression for each formal parameter.", e.GetSpan())
		invalid = true
	}
	for i, arg := range e.Arguments {
		actual := a.checkExpr(arg, env, scope)
		if actual == typeInvalid {
			invalid = true
			continue
		}
		if i < len(method.FormalTypes) {
			expected := method.FormalTypes[i]
			if expected == typeSelf || !a.table.Has(expected) {
				invalid = true
			} else if !a.conforms(actual, expected) {
				a.typeError("COL3175", fmt.Sprintf("argument %d of %s has type %s but expected %s", i+1, e.Method, actual, expected), arg.GetSpan())
				invalid = true
			}
		}
	}
	if !a.table.Has(method.ReturnType) {
		invalid = true
	}
	if invalid {
		return typeInvalid
	}
	if method.ReturnType == typeSelf {
		return staticReturnReceiver
	}
	return method.ReturnType
}

func (a *analyzer) resolveSelf(typeName string) string {
	if typeName == typeSelf {
		return a.currentClass
	}
	return typeName
}

func (a *analyzer) conforms(actual, expected string) bool {
	if actual == typeNo || actual == typeInvalid || expected == typeInvalid {
		return true
	}
	if expected == typeSelf {
		return actual == typeSelf
	}
	actual = a.resolveSelf(actual)
	return a.table.IsSubclass(actual, expected)
}

func (a *analyzer) join(left, right string) string {
	if left == typeInvalid || right == typeInvalid {
		return typeInvalid
	}
	if left == typeNo {
		return right
	}
	if right == typeNo {
		return left
	}
	if left == typeSelf && right == typeSelf {
		return typeSelf
	}
	left = a.resolveSelf(left)
	right = a.resolveSelf(right)
	ancestors := map[string]bool{}
	for current := left; current != ""; current = a.table.ParentOf(current) {
		ancestors[current] = true
	}
	for current := right; current != ""; current = a.table.ParentOf(current) {
		if ancestors[current] {
			return current
		}
	}
	return typeObject
}

func (a *analyzer) lookupAttributeFrom(parent, name string) (*AttrInfo, bool) {
	return a.table.LookupAttribute(parent, name)
}

func (a *analyzer) lookupMethodFrom(parent, name string) (*MethodInfo, bool) {
	return a.table.LookupMethod(parent, name)
}

func (a *analyzer) typeError(code, message string, span Span) {
	a.error(code, message, "Adjust the expression so its static type conforms to the required COOL type.", span)
}

func (a *analyzer) error(code, message, hint string, span Span) {
	a.diagnostics = append(a.diagnostics, errorDiagnostic("semantic", code, message, hint, span))
}

func (a *analyzer) addSymbol(name, kind, typeName string, mutable, initialized bool, scope, owner string, span Span) {
	if span.StartLine == 0 {
		span = emptySpan()
	}
	a.symbols = append(a.symbols, Symbol{
		Name: name, Kind: kind, Type: typeName, Mutable: mutable,
		Initialized: initialized, Scope: scope, Owner: owner,
		Line: span.StartLine, Column: span.StartColumn, Span: span,
	})
}

func methodSignature(method *MethodInfo) string {
	parts := make([]string, 0, len(method.FormalTypes))
	for i, formalType := range method.FormalTypes {
		name := fmt.Sprintf("arg%d", i+1)
		if i < len(method.FormalNames) && method.FormalNames[i] != "" {
			name = method.FormalNames[i]
		}
		parts = append(parts, name+" : "+formalType)
	}
	return method.Name + "(" + strings.Join(parts, ", ") + ") : " + method.ReturnType
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func isBasicComparable(typeName string) bool {
	return typeName == typeInt || typeName == typeString || typeName == typeBool
}
