// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import "strings"

// emitConstants writes the shared constant pool: every string constant
// (literals, class names, file names, and ""), every Int constant (literals
// and string lengths), and the two Bool constants. Each is a complete object
// with a -1 eyecatcher, so the runtime and the collector treat it like any
// other object; constants are never copied in place.
func (g *mipsGenerator) emitConstants() {
	for index := 0; index < len(g.stringOrder); index++ {
		value := g.stringOrder[index]
		label := g.strings[value]
		lengthLabel := g.intConstant(int32(len(value)))
		words := mipsHeaderWords + 1 + (len(value)+1+3)/4
		g.emit.directive(".word", mipsEyecatcher)
		g.emit.label(label)
		g.emit.directive(".word", g.tag(typeString))
		g.emit.directive(".word", words)
		g.emit.directive(".word", "String_dispTab")
		g.emit.directive(".word", lengthLabel)
		g.emitStringBytes(value)
		g.emit.directive(".byte", 0)
		g.emit.directive(".align", 2)
	}
	for _, value := range g.intOrder {
		g.emit.directive(".word", mipsEyecatcher)
		g.emit.label(g.ints[value])
		g.emit.directive(".word", g.tag(typeInt))
		g.emit.directive(".word", mipsHeaderWords+1)
		g.emit.directive(".word", "Int_dispTab")
		g.emit.directive(".word", value)
	}
	for index, label := range []string{"bool_const0", "bool_const1"} {
		g.emit.directive(".word", mipsEyecatcher)
		g.emit.label(label)
		g.emit.directive(".word", g.tag(typeBool))
		g.emit.directive(".word", mipsHeaderWords+1)
		g.emit.directive(".word", "Bool_dispTab")
		g.emit.directive(".word", index)
	}
}

// emitStringBytes writes printable runs with .ascii and every other byte
// with .byte, which SPIM's assembler accepts without escape ambiguity.
func (g *mipsGenerator) emitStringBytes(value string) {
	var run strings.Builder
	flush := func() {
		if run.Len() > 0 {
			g.emit.directive(".ascii", "\""+run.String()+"\"")
			run.Reset()
		}
	}
	for index := 0; index < len(value); index++ {
		b := value[index]
		if b >= 0x20 && b < 0x7f && b != '"' && b != '\\' {
			run.WriteByte(b)
			if run.Len() >= 48 {
				flush()
			}
			continue
		}
		flush()
		g.emit.directive(".byte", int(b))
	}
	flush()
}

// emitTables writes class_nameTab (class-name strings by tag),
// class_objTab (prototype and init-method pairs by tag, used by
// `new SELF_TYPE`), and every class's dispatch table. Dispatch slots start
// with the parent's slots in order; an override replaces the parent's label
// in the same slot and new methods are appended in declaration order.
func (g *mipsGenerator) emitTables() {
	g.emit.label("class_nameTab")
	for _, class := range g.layout.classes {
		g.emit.directive(".word", g.strings[class.name])
	}
	g.emit.directive(".globl", "class_objTab")
	g.emit.label("class_objTab")
	for _, class := range g.layout.classes {
		g.emit.directive(".word", class.name+"_protObj")
		g.emit.directive(".word", class.name+"_init")
	}
	for _, class := range g.layout.classes {
		g.emit.label(class.name + "_dispTab")
		for _, slot := range class.methods {
			g.emit.directive(".word", slot.label())
		}
	}
}
