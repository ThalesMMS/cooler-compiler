// SPDX-License-Identifier: AGPL-3.0-or-later

package mipssim

import (
	"bytes"
	"strconv"
	"strings"
)

// The Cool runtime contract, reimplemented in Go from its documentation
// (the Cool runtime-system manual): the routines the generated code calls,
// with their register and stack conventions, and the start-up sequence. No
// trap.handler code is bundled or translated.
//
//   - Object.copy: $a0 object -> $a0 fresh copy (allocates)
//   - Object.abort: prints "Abort called from class C" and exits
//   - Object.type_name: $a0 object -> $a0 class-name String
//   - IO.out_string / IO.out_int: argument at 4($sp), popped; returns self
//   - IO.in_string / IO.in_int: read one input line; return a new object
//   - String.length / concat / substr: arguments on the stack, popped
//   - equality_test: compares $t1 and $t2; returns $a0 if equal, else $a1
//   - _dispatch_abort, _case_abort2: file name String in $a0, line in $t1
//   - _case_abort: unmatched object in $a0
//   - _gc_check: $a1 object header check (-T)
//   - _GenGC_Assign: $a1 address of the assigned attribute (-g)
//   - memory manager entry points selected by _MemMgr_INITIALIZER,
//     _MemMgr_COLLECTOR, and _MemMgr_TEST.
var nativeNames = []string{
	"Object.copy", "Object.abort", "Object.type_name",
	"IO.out_string", "IO.out_int", "IO.in_string", "IO.in_int",
	"String.length", "String.concat", "String.substr",
	"equality_test", "_dispatch_abort", "_case_abort", "_case_abort2",
	"_gc_check", "_GenGC_Assign",
	"_NoGC_Init", "_NoGC_Collect", "_GenGC_Init", "_GenGC_Collect",
}

var nativeIndex = func() map[string]int {
	index := map[string]int{}
	for position, name := range nativeNames {
		index[name] = position
	}
	return index
}()

const (
	objectTag  = 0
	objectSize = 4
	objectDisp = 8
	firstAttr  = 12
)

func (m *machine) symbol(name string) (uint32, error) {
	address, ok := m.program.Symbol(name)
	if !ok {
		return 0, m.fail("link", "Undefined symbol %s: the program was not generated for the Cool runtime.", name)
	}
	return address, nil
}

func (m *machine) wordAt(name string) (int32, error) {
	address, err := m.symbol(name)
	if err != nil {
		return 0, err
	}
	return m.loadWord(address)
}

// start mirrors the runtime's start-up: initialize the memory manager,
// copy Main's prototype, run Main_init and Main.main, and print the success
// line.
func (m *machine) start() error {
	initializer, err := m.wordAt("_MemMgr_INITIALIZER")
	if err != nil {
		return err
	}
	test, err := m.wordAt("_MemMgr_TEST")
	if err != nil {
		return err
	}
	collector := "NoGC"
	if name := m.program.natives[uint32(initializer)]; name == "_GenGC_Init" {
		collector = "GenGC"
	}
	m.gc = newMemoryManager(m, collector, test != 0)
	for _, name := range []string{"_int_tag", "_bool_tag", "_string_tag"} {
		value, err := m.wordAt(name)
		if err != nil {
			return err
		}
		m.gc.basicTags[name] = value
	}
	mainProto, err := m.symbol("Main_protObj")
	if err != nil {
		return err
	}
	mainObject, err := m.gc.copyObject(uint32(mainProto))
	if err != nil {
		return err
	}
	m.reg[4] = int32(mainObject)
	m.reg[16] = int32(mainObject)
	mainInit, err := m.symbol("Main_init")
	if err != nil {
		return err
	}
	if err := m.call(mainInit); err != nil || m.halted {
		return err
	}
	mainMethod, err := m.symbol("Main.main")
	if err != nil {
		return err
	}
	m.reg[4] = int32(mainObject)
	if err := m.call(mainMethod); err != nil || m.halted {
		return err
	}
	return m.write(SuccessMessage)
}

func (m *machine) pop() (int32, error) {
	value, err := m.loadWord(uint32(m.reg[29] + 4))
	if err != nil {
		return 0, err
	}
	m.reg[29] += 4
	return value, nil
}

func (m *machine) stringValue(address uint32) (string, error) {
	if address == 0 {
		return "", m.fail("void", "Exception: a String argument is void.")
	}
	lengthObject, err := m.loadWord(address + firstAttr)
	if err != nil {
		return "", err
	}
	length, err := m.loadWord(uint32(lengthObject) + firstAttr)
	if err != nil {
		return "", err
	}
	if length < 0 || int(length) > m.options.Limits.MaxHeapBytes {
		return "", m.fail("bad-object", "Exception: corrupt String object at 0x%08x.", address)
	}
	memory, offset, err := m.translate(address+firstAttr+4, int(length))
	if err != nil {
		return "", err
	}
	return string(memory[offset : offset+int(length)]), nil
}

func (m *machine) intValue(address uint32) (int32, error) {
	if address == 0 {
		return 0, m.fail("void", "Exception: an Int argument is void.")
	}
	return m.loadWord(address + firstAttr)
}

func (m *machine) className(object uint32) (string, error) {
	tag, err := m.loadWord(object + objectTag)
	if err != nil {
		return "", err
	}
	table, err := m.symbol("class_nameTab")
	if err != nil {
		return "", err
	}
	name, err := m.loadWord(table + uint32(4*tag))
	if err != nil {
		return "", err
	}
	return m.stringValue(uint32(name))
}

// newInt allocates an Int object holding value.
func (m *machine) newInt(value int32) (uint32, error) {
	proto, err := m.symbol("Int_protObj")
	if err != nil {
		return 0, err
	}
	object, err := m.gc.copyObject(proto)
	if err != nil {
		return 0, err
	}
	return object, m.storeWord(object+firstAttr, value)
}

// newString allocates a String object for value, including its length Int.
func (m *machine) newString(value string) (uint32, error) {
	length, err := m.newInt(int32(len(value)))
	if err != nil {
		return 0, err
	}
	m.gc.pin(length)
	defer m.gc.unpin(length)
	proto, err := m.symbol("String_protObj")
	if err != nil {
		return 0, err
	}
	words := 4 + (len(value)+1+3)/4
	object, err := m.gc.allocate(words)
	if err != nil {
		return 0, err
	}
	for offset := 0; offset < 12; offset += 4 {
		word, err := m.loadWord(proto + uint32(offset))
		if err != nil {
			return 0, err
		}
		if err := m.storeWord(object+uint32(offset), word); err != nil {
			return 0, err
		}
	}
	if err := m.storeWord(object+objectSize, int32(words)); err != nil {
		return 0, err
	}
	if err := m.storeWord(object+firstAttr, int32(length)); err != nil {
		return 0, err
	}
	memory, offset, err := m.translate(object+firstAttr+4, len(value)+1)
	if err != nil {
		return 0, err
	}
	copy(memory[offset:], value)
	memory[offset+len(value)] = 0
	return object, nil
}

func (m *machine) readLine() (string, bool, error) {
	if m.input < len(m.options.Input) {
		line := m.options.Input[m.input]
		m.input++
		return line, true, nil
	}
	if m.options.Interactive {
		return "", false, errWaitingInput
	}
	return "", false, nil
}

func (m *machine) native(name string) error {
	a0 := uint32(m.reg[4])
	switch name {
	case "Object.copy":
		if a0 == 0 {
			return m.fail("void", "Exception: Object.copy of a void object.")
		}
		copied, err := m.gc.copyObject(a0)
		if err != nil {
			return err
		}
		m.reg[4] = int32(copied)
	case "Object.abort":
		class, err := m.className(a0)
		if err != nil {
			return err
		}
		if err := m.write("Abort called from class " + class + "\n"); err != nil {
			return err
		}
		return m.fail("abort", "Abort called from class %s", class)
	case "Object.type_name":
		tag, err := m.loadWord(a0 + objectTag)
		if err != nil {
			return err
		}
		table, err := m.symbol("class_nameTab")
		if err != nil {
			return err
		}
		nameObject, err := m.loadWord(table + uint32(4*tag))
		if err != nil {
			return err
		}
		m.reg[4] = nameObject
	case "IO.out_string":
		argument, err := m.pop()
		if err != nil {
			return err
		}
		text, err := m.stringValue(uint32(argument))
		if err != nil {
			return err
		}
		return m.write(text)
	case "IO.out_int":
		argument, err := m.pop()
		if err != nil {
			return err
		}
		value, err := m.intValue(uint32(argument))
		if err != nil {
			return err
		}
		return m.write(strconv.Itoa(int(value)))
	case "IO.in_string":
		line, _, err := m.readLine()
		if err != nil {
			return err
		}
		if strings.IndexByte(line, 0) >= 0 {
			line = ""
		}
		object, err := m.newString(line)
		if err != nil {
			return err
		}
		m.reg[4] = int32(object)
	case "IO.in_int":
		line, _, err := m.readLine()
		if err != nil {
			return err
		}
		value, parseErr := strconv.ParseInt(strings.TrimSpace(line), 10, 32)
		if parseErr != nil {
			value = 0
		}
		object, err := m.newInt(int32(value))
		if err != nil {
			return err
		}
		m.reg[4] = int32(object)
	case "String.length":
		length, err := m.loadWord(a0 + firstAttr)
		if err != nil {
			return err
		}
		m.reg[4] = length
	case "String.concat":
		argument, err := m.pop()
		if err != nil {
			return err
		}
		left, err := m.stringValue(a0)
		if err != nil {
			return err
		}
		right, err := m.stringValue(uint32(argument))
		if err != nil {
			return err
		}
		object, err := m.newString(left + right)
		if err != nil {
			return err
		}
		m.reg[4] = int32(object)
	case "String.substr":
		lengthArgument, err := m.pop()
		if err != nil {
			return err
		}
		indexArgument, err := m.pop()
		if err != nil {
			return err
		}
		text, err := m.stringValue(a0)
		if err != nil {
			return err
		}
		index, err := m.intValue(uint32(indexArgument))
		if err != nil {
			return err
		}
		length, err := m.intValue(uint32(lengthArgument))
		if err != nil {
			return err
		}
		switch {
		case index < 0:
			return m.abortWith("substr", "Index to substr is negative\n")
		case int(index) > len(text):
			return m.abortWith("substr", "Index to substr is too big\n")
		case length < 0:
			return m.abortWith("substr", "Length to substr is negative\n")
		case int(index)+int(length) > len(text):
			return m.abortWith("substr", "Length to substr too long\n")
		}
		object, err := m.newString(text[index : index+length])
		if err != nil {
			return err
		}
		m.reg[4] = int32(object)
	case "equality_test":
		equal, err := m.equal(uint32(m.reg[9]), uint32(m.reg[10]))
		if err != nil {
			return err
		}
		if !equal {
			m.reg[4] = m.reg[5]
		}
	case "_dispatch_abort", "_case_abort2":
		file, err := m.stringValue(a0)
		if err != nil {
			return err
		}
		message := "Dispatch to void."
		category := "dispatch-void"
		if name == "_case_abort2" {
			message, category = "Match on void in case statement.", "case-void"
		}
		return m.abortWith(category, file+":"+strconv.Itoa(int(m.reg[9]))+": "+message+"\n")
	case "_case_abort":
		class, err := m.className(a0)
		if err != nil {
			return err
		}
		return m.abortWith("case-no-match", "No match in case statement for Class "+class+"\n")
	case "_gc_check":
		return m.gc.check(uint32(m.reg[5]))
	case "_GenGC_Assign":
		m.gc.recordAssignment(uint32(m.reg[5]))
	case "_NoGC_Init", "_GenGC_Init", "_NoGC_Collect", "_GenGC_Collect":
		if strings.HasSuffix(name, "Collect") {
			return m.gc.collect()
		}
	default:
		return m.fail("link", "Unknown runtime routine %s.", name)
	}
	return nil
}

func (m *machine) abortWith(category, message string) error {
	if err := m.write(message); err != nil {
		return err
	}
	return m.fail(category, "%s", strings.TrimSuffix(message, "\n"))
}

// equal implements equality_test: Int and Bool compare values, String
// compares contents, and objects of other or different classes are equal
// only when identical (the caller already checked identity).
func (m *machine) equal(left, right uint32) (bool, error) {
	if left == right {
		return true, nil
	}
	if left == 0 || right == 0 {
		return false, nil
	}
	leftTag, err := m.loadWord(left + objectTag)
	if err != nil {
		return false, err
	}
	rightTag, err := m.loadWord(right + objectTag)
	if err != nil {
		return false, err
	}
	if leftTag != rightTag {
		return false, nil
	}
	switch leftTag {
	case m.gc.basicTags["_int_tag"], m.gc.basicTags["_bool_tag"]:
		a, err := m.loadWord(left + firstAttr)
		if err != nil {
			return false, err
		}
		b, err := m.loadWord(right + firstAttr)
		return a == b, err
	case m.gc.basicTags["_string_tag"]:
		a, err := m.stringValue(left)
		if err != nil {
			return false, err
		}
		b, err := m.stringValue(right)
		return a == b, err
	}
	return false, nil
}

// syscall implements the SPIM system calls a hand-written program may use.
func (m *machine) syscall() (bool, error) {
	switch m.reg[2] {
	case 1:
		return false, m.write(strconv.Itoa(int(m.reg[4])))
	case 4:
		var text bytes.Buffer
		for address := uint32(m.reg[4]); ; address++ {
			b, err := m.loadByte(address)
			if err != nil {
				return false, err
			}
			if b == 0 {
				break
			}
			text.WriteByte(b)
			if text.Len() > m.options.Limits.MaxOutputBytes {
				return false, m.fail("output-limit", "Execution stopped: program output exceeded %d bytes.", m.options.Limits.MaxOutputBytes)
			}
		}
		return false, m.write(text.String())
	case 11:
		return false, m.write(string([]byte{byte(m.reg[4])}))
	case 10, 17:
		return true, nil
	}
	return false, m.fail("syscall", "Exception: unsupported syscall %d.", m.reg[2])
}
