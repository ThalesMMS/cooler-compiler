// SPDX-License-Identifier: AGPL-3.0-or-later

// Package mipssim is Cooler's embedded, SPIM-compatible MIPS simulator for
// the optional PA5 backend. It assembles the SPIM assembly that Cooler
// generates and executes it with a pure-Go reimplementation of the Cool
// runtime contract (trap.handler's documented routines, start-up sequence,
// and memory-manager hooks). It never invokes a host spim and bundles no
// trap.handler source; see docs/mips-backend.md for the licensing decision.
package mipssim

import (
	"fmt"
	"strconv"
	"strings"
)

// Memory map, following SPIM's defaults.
const (
	TextBase  uint32 = 0x00400000
	DataBase  uint32 = 0x10000000
	StackTop  uint32 = 0x7ffffffc
	nativeBas uint32 = 0x00300000
	haltAddr  uint32 = 0x002ffff0
)

type operandKind int

const (
	opRegister operandKind = iota
	opImmediate
	opLabel
	opMemory // offset(register)
)

type operand struct {
	kind     operandKind
	register int
	value    int64
	label    string
}

type instruction struct {
	op       string
	operands []operand
	line     int
	text     string
}

// Program is an assembled MIPS program.
type Program struct {
	text    []instruction
	data    []byte
	symbols map[string]uint32
	// natives maps runtime-routine addresses to their names.
	natives map[uint32]string
}

// AssemblyError reports a malformed input line.
type AssemblyError struct {
	Line    int
	Message string
}

func (e *AssemblyError) Error() string { return fmt.Sprintf("line %d: %s", e.Line, e.Message) }

var registerNames = map[string]int{
	"zero": 0, "at": 1, "v0": 2, "v1": 3, "a0": 4, "a1": 5, "a2": 6, "a3": 7,
	"t0": 8, "t1": 9, "t2": 10, "t3": 11, "t4": 12, "t5": 13, "t6": 14, "t7": 15,
	"s0": 16, "s1": 17, "s2": 18, "s3": 19, "s4": 20, "s5": 21, "s6": 22, "s7": 23,
	"t8": 24, "t9": 25, "k0": 26, "k1": 27, "gp": 28, "sp": 29, "fp": 30, "ra": 31,
}

// RegisterName returns the conventional name of register n.
func RegisterName(n int) string {
	for name, index := range registerNames {
		if index == n {
			return "$" + name
		}
	}
	return fmt.Sprintf("$%d", n)
}

type dataFixup struct {
	offset int
	label  string
	line   int
}

// Assemble parses SPIM assembly. Labels that the program uses but does not
// define must name a Cool runtime routine (Object.copy, IO.out_string,
// _dispatch_abort, ...); they resolve to the simulator's native routines.
func Assemble(source string) (*Program, error) {
	program := &Program{symbols: map[string]uint32{}, natives: map[uint32]string{}}
	var fixups []dataFixup
	inText := true
	lines := strings.Split(source, "\n")
	for index, raw := range lines {
		lineNumber := index + 1
		line := stripComment(raw)
		for {
			trimmed := strings.TrimSpace(line)
			colon := labelEnd(trimmed)
			if colon < 0 {
				line = trimmed
				break
			}
			label := trimmed[:colon]
			if _, exists := program.symbols[label]; exists {
				return nil, &AssemblyError{lineNumber, "duplicate label " + label}
			}
			if inText {
				program.symbols[label] = TextBase + uint32(4*len(program.text))
			} else {
				program.symbols[label] = DataBase + uint32(len(program.data))
			}
			line = trimmed[colon+1:]
		}
		if line == "" {
			continue
		}
		fields := splitOperands(line)
		head := fields[0]
		if strings.HasPrefix(head, ".") {
			args := fields[1:]
			switch head {
			case ".text":
				inText = true
			case ".data":
				inText = false
			case ".globl", ".global", ".extern", ".ent", ".end":
			case ".align":
				if len(args) != 1 {
					return nil, &AssemblyError{lineNumber, ".align needs one argument"}
				}
				power, err := strconv.Atoi(args[0])
				if err != nil || power < 0 || power > 4 {
					return nil, &AssemblyError{lineNumber, "bad .align"}
				}
				for len(program.data)%(1<<power) != 0 {
					program.data = append(program.data, 0)
				}
			case ".word":
				if inText {
					return nil, &AssemblyError{lineNumber, ".word in the text segment is not supported"}
				}
				for len(program.data)%4 != 0 {
					program.data = append(program.data, 0)
				}
				for _, arg := range args {
					if value, err := parseInteger(arg); err == nil {
						program.data = appendWord(program.data, uint32(value))
					} else {
						fixups = append(fixups, dataFixup{offset: len(program.data), label: arg, line: lineNumber})
						program.data = appendWord(program.data, 0)
					}
				}
			case ".byte":
				for _, arg := range args {
					value, err := parseInteger(arg)
					if err != nil {
						return nil, &AssemblyError{lineNumber, "bad .byte " + arg}
					}
					program.data = append(program.data, byte(value))
				}
			case ".ascii", ".asciiz":
				text, err := parseStringLiteral(strings.TrimSpace(line[len(head):]))
				if err != nil {
					return nil, &AssemblyError{lineNumber, err.Error()}
				}
				program.data = append(program.data, text...)
				if head == ".asciiz" {
					program.data = append(program.data, 0)
				}
			case ".space":
				count, err := parseInteger(args[0])
				if err != nil || count < 0 {
					return nil, &AssemblyError{lineNumber, "bad .space"}
				}
				program.data = append(program.data, make([]byte, count)...)
			default:
				return nil, &AssemblyError{lineNumber, "unsupported directive " + head}
			}
			continue
		}
		if !inText {
			return nil, &AssemblyError{lineNumber, "instruction in the data segment"}
		}
		instruction, err := parseInstruction(fields, lineNumber, strings.TrimSpace(line))
		if err != nil {
			return nil, err
		}
		program.text = append(program.text, instruction)
	}
	resolve := func(label string, line int) (uint32, error) {
		if address, ok := program.symbols[label]; ok {
			return address, nil
		}
		if index, ok := nativeIndex[label]; ok {
			address := nativeBas + uint32(4*index)
			program.natives[address] = label
			return address, nil
		}
		return 0, &AssemblyError{line, "undefined label " + label}
	}
	for _, fixup := range fixups {
		address, err := resolve(fixup.label, fixup.line)
		if err != nil {
			return nil, err
		}
		putWord(program.data, fixup.offset, address)
	}
	for index := range program.text {
		for operandIndex, op := range program.text[index].operands {
			if op.kind != opLabel {
				continue
			}
			address, err := resolve(op.label, program.text[index].line)
			if err != nil {
				return nil, err
			}
			program.text[index].operands[operandIndex].value = int64(address)
		}
	}
	for name, index := range nativeIndex {
		address := nativeBas + uint32(4*index)
		program.natives[address] = name
	}
	return program, nil
}

// Symbol returns a label's address.
func (p *Program) Symbol(name string) (uint32, bool) {
	address, ok := p.symbols[name]
	if !ok {
		if index, native := nativeIndex[name]; native {
			return nativeBas + uint32(4*index), true
		}
	}
	return address, ok
}

func stripComment(line string) string {
	inString := false
	for index := 0; index < len(line); index++ {
		switch line[index] {
		case '\\':
			if inString {
				index++
			}
		case '"':
			inString = !inString
		case '#':
			if !inString {
				return line[:index]
			}
		}
	}
	return line
}

// labelEnd returns the index of a leading "label:" colon, or -1.
func labelEnd(line string) int {
	for index, char := range line {
		switch {
		case char == ':':
			if index == 0 {
				return -1
			}
			return index
		case char == '_' || char == '.' || char == '$' || char >= '0' && char <= '9' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z':
		default:
			return -1
		}
	}
	return -1
}

func splitOperands(line string) []string {
	return strings.FieldsFunc(line, func(r rune) bool { return r == ' ' || r == '\t' || r == ',' })
}

func parseInteger(text string) (int64, error) {
	return strconv.ParseInt(text, 0, 64)
}

func appendWord(data []byte, value uint32) []byte {
	return append(data, byte(value), byte(value>>8), byte(value>>16), byte(value>>24))
}

func putWord(data []byte, offset int, value uint32) {
	data[offset], data[offset+1], data[offset+2], data[offset+3] = byte(value), byte(value>>8), byte(value>>16), byte(value>>24)
}

func parseStringLiteral(text string) ([]byte, error) {
	if len(text) < 2 || text[0] != '"' || text[len(text)-1] != '"' {
		return nil, fmt.Errorf("expected a quoted string")
	}
	var out []byte
	body := text[1 : len(text)-1]
	for index := 0; index < len(body); index++ {
		if body[index] != '\\' {
			out = append(out, body[index])
			continue
		}
		index++
		if index >= len(body) {
			return nil, fmt.Errorf("dangling escape")
		}
		switch body[index] {
		case 'n':
			out = append(out, '\n')
		case 't':
			out = append(out, '\t')
		case '0':
			out = append(out, 0)
		default:
			out = append(out, body[index])
		}
	}
	return out, nil
}

func parseRegister(text string) (int, bool) {
	if !strings.HasPrefix(text, "$") {
		return 0, false
	}
	name := text[1:]
	if number, err := strconv.Atoi(name); err == nil && number >= 0 && number < 32 {
		return number, true
	}
	number, ok := registerNames[name]
	return number, ok
}

func parseOperand(text string) (operand, error) {
	if register, ok := parseRegister(text); ok {
		return operand{kind: opRegister, register: register}, nil
	}
	if open := strings.IndexByte(text, '('); open >= 0 && strings.HasSuffix(text, ")") {
		register, ok := parseRegister(text[open+1 : len(text)-1])
		if !ok {
			return operand{}, fmt.Errorf("bad base register in %q", text)
		}
		offset := int64(0)
		if open > 0 {
			value, err := parseInteger(text[:open])
			if err != nil {
				return operand{}, fmt.Errorf("bad offset in %q", text)
			}
			offset = value
		}
		return operand{kind: opMemory, register: register, value: offset}, nil
	}
	if value, err := parseInteger(text); err == nil {
		return operand{kind: opImmediate, value: value}, nil
	}
	if text == "" || strings.ContainsAny(text, "()$\"") {
		return operand{}, fmt.Errorf("bad operand %q", text)
	}
	return operand{kind: opLabel, label: text}, nil
}

// instructionShapes lists the accepted operand kinds for each mnemonic:
// r register, i immediate, l label, m memory, x register or immediate,
// a address (label or memory).
var instructionShapes = map[string]string{
	"la": "ra", "li": "ri", "lw": "ra", "sw": "ra", "lb": "ra", "lbu": "ra", "sb": "ra",
	"move": "rr", "addiu": "rri", "addi": "rri", "andi": "rri", "ori": "rri", "slti": "rri",
	"addu": "rrx", "add": "rrx", "subu": "rrx", "sub": "rrx", "mul": "rrx", "div": "rrx", "rem": "rrx",
	"and": "rrx", "or": "rrx", "xor": "rrx", "slt": "rrx", "sltu": "rrx",
	"neg": "rr", "negu": "rr", "not": "rr", "sll": "rri", "srl": "rri", "sra": "rri",
	"beq": "rxl", "bne": "rxl", "blt": "rxl", "ble": "rxl", "bgt": "rxl", "bge": "rxl",
	"beqz": "rl", "bnez": "rl", "b": "l", "j": "l", "jal": "l", "jalr": "r", "jr": "r",
	"nop": "", "syscall": "", "mflo": "r", "mfhi": "r",
}

func parseInstruction(fields []string, line int, text string) (instruction, error) {
	op := fields[0]
	shape, ok := instructionShapes[op]
	if !ok {
		return instruction{}, &AssemblyError{line, "unsupported instruction " + op}
	}
	args := fields[1:]
	if len(args) != len(shape) {
		return instruction{}, &AssemblyError{line, fmt.Sprintf("%s expects %d operands", op, len(shape))}
	}
	result := instruction{op: op, line: line, text: text}
	for index, kind := range shape {
		parsed, err := parseOperand(args[index])
		if err != nil {
			return instruction{}, &AssemblyError{line, err.Error()}
		}
		valid := false
		switch kind {
		case 'r':
			valid = parsed.kind == opRegister
		case 'i':
			valid = parsed.kind == opImmediate
		case 'l':
			valid = parsed.kind == opLabel || parsed.kind == opImmediate
		case 'x':
			valid = parsed.kind == opRegister || parsed.kind == opImmediate
		case 'a':
			valid = parsed.kind == opLabel || parsed.kind == opMemory
		}
		if !valid {
			return instruction{}, &AssemblyError{line, fmt.Sprintf("operand %d of %s has the wrong kind", index+1, op)}
		}
		result.operands = append(result.operands, parsed)
	}
	return result, nil
}
