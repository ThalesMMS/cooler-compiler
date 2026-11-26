// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"fmt"
	"sort"
	"strings"
)

// The optional MIPS backend (PA5) emits SPIM assembly for the Cool runtime
// system (trap.handler conventions). It is selected with --target=mips or
// the GUI MIPS panel; the Go VM and educational pseudo-assembly remain the
// default. docs/mips-backend.md documents the object layout and ABI.

// Object layout (bytes from the object pointer):
//
//	-4  eyecatcher -1 (garbage-collector header)
//	 0  class tag
//	 4  object size in words, header included
//	 8  dispatch table pointer
//	12  first attribute; attributes follow at 4-byte steps
const (
	mipsWord           = 4
	mipsTagOffset      = 0
	mipsSizeOffset     = 4
	mipsDispatchOffset = 8
	mipsHeaderWords    = 3
	mipsEyecatcher     = -1
)

// MIPSGCOptions selects the Cool memory manager, mirroring coolc's flags:
// -g (generational collector), -t (collect at every allocation), and -T
// (check object headers on every attribute store).
type MIPSGCOptions struct {
	Generational bool `json:"generational,omitempty"`
	Test         bool `json:"test,omitempty"`
	Debug        bool `json:"debug,omitempty"`
}

// MIPSOptions configures MIPS generation.
type MIPSOptions struct {
	Frontend string        `json:"frontend,omitempty"`
	GC       MIPSGCOptions `json:"gc"`
}

// MIPSAttribute is one attribute slot in an object layout.
type MIPSAttribute struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Owner  string `json:"owner"`
	Offset int    `json:"offset"`
}

// MIPSClassLayout describes one class's tag, size, and attribute offsets.
type MIPSClassLayout struct {
	Name       string          `json:"name"`
	Parent     string          `json:"parent,omitempty"`
	Tag        int             `json:"tag"`
	MaxTag     int             `json:"maxTag"`
	SizeWords  int             `json:"sizeWords"`
	Attributes []MIPSAttribute `json:"attributes"`
	Methods    []string        `json:"methods"`
}

// mipsClass is the code generator's view of one class.
type mipsClass struct {
	name     string
	parent   *mipsClass
	children []*mipsClass
	info     *ClassInfo
	tag      int
	maxTag   int
	depth    int
	attrs    []MIPSAttribute
	// methods are the dispatch-table entries: slot order, label of the
	// implementing class's method.
	methods     []mipsMethodSlot
	attrDecls   map[string]*AttributeDecl
	ownAttrs    []string
	basicPrimal bool // Int, Bool, String: a raw value slot instead of attributes
}

type mipsMethodSlot struct {
	name  string
	owner string
}

func (slot mipsMethodSlot) label() string { return slot.owner + "." + slot.name }

type mipsProgram struct {
	classes []*mipsClass // by tag
	byName  map[string]*mipsClass
	table   *ClassTable
	program *Program
	files   map[string]string
}

// basicClassOrder fixes the basic classes' position among Object's children.
var basicClassOrder = []string{typeIO, typeInt, typeBool, typeString}

// buildMIPSLayout assigns class tags in depth-first preorder of the
// inheritance tree (basic classes first, then user classes in program
// order), so every class's descendants occupy the tag range [tag, maxTag],
// which `case` uses for branch tests. It then lays out attributes and
// dispatch slots.
func buildMIPSLayout(program *Program, table *ClassTable, files map[string]string) *mipsProgram {
	layout := &mipsProgram{byName: map[string]*mipsClass{}, table: table, program: program, files: files}
	for _, name := range table.Order {
		info := table.Classes[name]
		layout.byName[name] = &mipsClass{name: name, info: info, attrDecls: map[string]*AttributeDecl{}}
	}
	for _, name := range table.Order {
		class := layout.byName[name]
		if parent := table.Classes[name].Parent; parent != "" {
			class.parent = layout.byName[parent]
		}
	}
	childOrder := map[string]int{}
	for index, name := range basicClassOrder {
		childOrder[name] = index
	}
	for index, name := range table.Order {
		if _, ok := childOrder[name]; !ok {
			childOrder[name] = len(basicClassOrder) + index
		}
	}
	for _, name := range table.Order {
		class := layout.byName[name]
		if class.parent != nil {
			class.parent.children = append(class.parent.children, class)
		}
	}
	for _, class := range layout.byName {
		sort.SliceStable(class.children, func(i, j int) bool {
			return childOrder[class.children[i].name] < childOrder[class.children[j].name]
		})
	}
	var visit func(class *mipsClass, depth int)
	visit = func(class *mipsClass, depth int) {
		class.tag = len(layout.classes)
		class.depth = depth
		layout.classes = append(layout.classes, class)
		for _, child := range class.children {
			visit(child, depth+1)
		}
		class.maxTag = len(layout.classes) - 1
	}
	visit(layout.byName[typeObject], 0)
	for _, class := range layout.classes {
		layout.layoutClass(class)
	}
	return layout
}

func (p *mipsProgram) layoutClass(class *mipsClass) {
	if class.parent != nil {
		class.attrs = append(class.attrs, class.parent.attrs...)
		class.methods = append(class.methods, class.parent.methods...)
	}
	switch class.name {
	case typeInt, typeBool:
		class.basicPrimal = true
		class.attrs = append(class.attrs, MIPSAttribute{Name: "_val", Type: "_prim_slot", Owner: class.name})
	case typeString:
		class.basicPrimal = true
		class.attrs = append(class.attrs, MIPSAttribute{Name: "_val", Type: typeInt, Owner: class.name})
	}
	if class.info.Decl != nil {
		for _, feature := range class.info.Decl.Features {
			if attr, ok := feature.(*AttributeDecl); ok {
				if _, declared := class.info.Attributes[attr.Name]; !declared || class.info.Attributes[attr.Name].Decl != attr {
					continue
				}
				class.attrDecls[attr.Name] = attr
				class.ownAttrs = append(class.ownAttrs, attr.Name)
				class.attrs = append(class.attrs, MIPSAttribute{Name: attr.Name, Type: attr.TypeName, Owner: class.name})
			}
		}
	}
	for index := range class.attrs {
		class.attrs[index].Offset = (mipsHeaderWords + index) * mipsWord
	}
	for _, name := range orderedMethodNames(class.info) {
		replaced := false
		for index, slot := range class.methods {
			if slot.name == name {
				class.methods[index].owner = class.name
				replaced = true
			}
		}
		if !replaced {
			class.methods = append(class.methods, mipsMethodSlot{name: name, owner: class.name})
		}
	}
}

func (c *mipsClass) sizeWords() int {
	if c.name == typeString {
		// The empty-string prototype: length pointer plus one word of chars.
		return mipsHeaderWords + 2
	}
	return mipsHeaderWords + len(c.attrs)
}

func (c *mipsClass) attribute(name string) (MIPSAttribute, bool) {
	for _, attr := range c.attrs {
		if attr.Name == name {
			return attr, true
		}
	}
	return MIPSAttribute{}, false
}

func (c *mipsClass) methodOffset(name string) (int, bool) {
	for index, slot := range c.methods {
		if slot.name == name {
			return index * mipsWord, true
		}
	}
	return 0, false
}

// Layouts reports every class's layout, by tag.
func (p *mipsProgram) Layouts() []MIPSClassLayout {
	layouts := make([]MIPSClassLayout, 0, len(p.classes))
	for _, class := range p.classes {
		layout := MIPSClassLayout{Name: class.name, Tag: class.tag, MaxTag: class.maxTag, SizeWords: class.sizeWords(), Attributes: append([]MIPSAttribute{}, class.attrs...)}
		if class.parent != nil {
			layout.Parent = class.parent.name
		}
		for _, slot := range class.methods {
			layout.Methods = append(layout.Methods, slot.label())
		}
		layouts = append(layouts, layout)
	}
	return layouts
}

// mipsEmitter accumulates assembly text.
type mipsEmitter struct {
	out strings.Builder
}

func (e *mipsEmitter) label(name string) { fmt.Fprintf(&e.out, "%s:\n", name) }
func (e *mipsEmitter) directive(name string, args ...any) {
	fmt.Fprintf(&e.out, "\t%s", name)
	if len(args) > 0 {
		e.out.WriteString("\t")
		for index, arg := range args {
			if index > 0 {
				e.out.WriteString(", ")
			}
			fmt.Fprint(&e.out, arg)
		}
	}
	e.out.WriteByte('\n')
}
func (e *mipsEmitter) comment(format string, args ...any) {
	fmt.Fprintf(&e.out, "# %s\n", fmt.Sprintf(format, args...))
}

// defaultAttributeValue is the prototype word for an attribute of type
// typeName: the shared default constant for basic types, otherwise void.
func (g *mipsGenerator) defaultAttributeValue(typeName string) string {
	switch typeName {
	case typeInt:
		return g.intConstant(0)
	case typeString:
		return g.stringConstant("")
	case typeBool:
		return "bool_const0"
	}
	return "0"
}

// emitPrototypes writes each class's prototype object, preceded by the
// garbage collector's -1 eyecatcher. Object.copy clones a prototype on new.
func (g *mipsGenerator) emitPrototypes() {
	for _, class := range g.layout.classes {
		g.emit.directive(".word", mipsEyecatcher)
		g.emit.label(class.name + "_protObj")
		g.emit.directive(".word", class.tag)
		g.emit.directive(".word", class.sizeWords())
		g.emit.directive(".word", class.name+"_dispTab")
		switch class.name {
		case typeInt, typeBool:
			g.emit.directive(".word", 0)
		case typeString:
			g.emit.directive(".word", g.intConstant(0))
			g.emit.directive(".word", 0)
		default:
			for _, attr := range class.attrs {
				g.emit.directive(".word", g.defaultAttributeValue(attr.Type))
			}
		}
	}
}
