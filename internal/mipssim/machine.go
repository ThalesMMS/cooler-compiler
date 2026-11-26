// SPDX-License-Identifier: AGPL-3.0-or-later

package mipssim

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Run outcomes.
const (
	StatusHalted        = "halted"
	StatusRuntimeError  = "runtime-error"
	StatusStepLimit     = "step-limit"
	StatusWaitingInput  = "waiting-input"
	StatusCancelled     = "cancelled"
	StatusAssemblyError = "assembly-error"
)

// SuccessMessage is printed by the Cool runtime after Main.main returns.
const SuccessMessage = "COOL program successfully executed\n"

// Limits bound a run deterministically, in the spirit of the Go VM policy.
type Limits struct {
	MaxSteps       int `json:"maxSteps"`
	MaxHeapBytes   int `json:"maxHeapBytes"`
	StackBytes     int `json:"stackBytes"`
	MaxOutputBytes int `json:"maxOutputBytes"`
}

// DefaultLimits are used for zero fields.
func DefaultLimits() Limits {
	return Limits{MaxSteps: 20_000_000, MaxHeapBytes: 64 << 20, StackBytes: 4 << 20, MaxOutputBytes: 4 << 20}
}

// Options configure one run.
type Options struct {
	Limits Limits
	// Input lines consumed by IO.in_string / IO.in_int.
	Input []string
	// Interactive makes a read with no remaining input stop the run with
	// StatusWaitingInput instead of returning "" / 0.
	Interactive bool
}

// GCStats summarizes the memory manager's work during a run.
type GCStats struct {
	Collector      string `json:"collector"`
	CollectEvery   bool   `json:"collectEveryAllocation"`
	Allocations    int    `json:"allocations"`
	Collections    int    `json:"collections"`
	FreedObjects   int    `json:"freedObjects"`
	HeapBytes      int    `json:"heapBytes"`
	AssignmentLogs int    `json:"assignmentLogs"`
	HeaderChecks   int    `json:"headerChecks"`
}

// Result is the outcome of a run.
type Result struct {
	Status        string  `json:"status"`
	Output        string  `json:"output"`
	Error         string  `json:"error,omitempty"`
	ErrorCategory string  `json:"errorCategory,omitempty"`
	Steps         int     `json:"steps"`
	InputConsumed int     `json:"inputConsumed"`
	GC            GCStats `json:"gc"`
}

// runtimeError stops the machine with a Cool runtime message.
type runtimeError struct {
	category string
	message  string
}

func (e *runtimeError) Error() string { return e.message }

var errWaitingInput = errors.New("waiting for input")

type machine struct {
	program *Program
	options Options
	reg     [32]int32
	pc      uint32
	hi, lo  int32
	memory  []byte // data segment followed by the heap
	heapTop uint32
	stack   []byte
	steps   int
	output  strings.Builder
	input   int
	gc      *memoryManager
	ctx     context.Context
	halted  bool
}

// Run assembles source and executes it under the Cool runtime contract.
func Run(ctx context.Context, source string, options Options) Result {
	program, err := Assemble(source)
	if err != nil {
		return Result{Status: StatusAssemblyError, Error: err.Error(), ErrorCategory: "assembly"}
	}
	return RunProgram(ctx, program, options)
}

// RunProgram executes an assembled program.
func RunProgram(ctx context.Context, program *Program, options Options) Result {
	defaults := DefaultLimits()
	if options.Limits.MaxSteps <= 0 {
		options.Limits.MaxSteps = defaults.MaxSteps
	}
	if options.Limits.MaxHeapBytes <= 0 {
		options.Limits.MaxHeapBytes = defaults.MaxHeapBytes
	}
	if options.Limits.StackBytes <= 0 {
		options.Limits.StackBytes = defaults.StackBytes
	}
	if options.Limits.MaxOutputBytes <= 0 {
		options.Limits.MaxOutputBytes = defaults.MaxOutputBytes
	}
	m := &machine{program: program, options: options, ctx: ctx}
	m.memory = append([]byte(nil), program.data...)
	for len(m.memory)%8 != 0 {
		m.memory = append(m.memory, 0)
	}
	m.heapTop = DataBase + uint32(len(m.memory))
	m.stack = make([]byte, options.Limits.StackBytes)
	m.reg[29] = int32(StackTop)
	m.reg[30] = int32(StackTop)
	result := Result{}
	err := m.start()
	result.Output = m.output.String()
	result.Steps = m.steps
	result.InputConsumed = m.input
	if m.gc != nil {
		result.GC = m.gc.stats
	}
	var runtimeErr *runtimeError
	switch {
	case err == nil:
		result.Status = StatusHalted
	case errors.Is(err, errWaitingInput):
		result.Status = StatusWaitingInput
	case errors.As(err, &runtimeErr):
		result.Status = StatusRuntimeError
		result.Error = runtimeErr.message
		result.ErrorCategory = runtimeErr.category
		if runtimeErr.category == "step-limit" {
			result.Status = StatusStepLimit
		}
		if runtimeErr.category == "cancelled" {
			result.Status = StatusCancelled
		}
	default:
		result.Status = StatusRuntimeError
		result.Error = err.Error()
		result.ErrorCategory = "internal"
	}
	return result
}

func (m *machine) fail(category, format string, args ...any) error {
	return &runtimeError{category: category, message: fmt.Sprintf(format, args...)}
}

// translate maps an address to a byte slice and offset.
func (m *machine) translate(address uint32, size int) ([]byte, int, error) {
	if address >= DataBase && address+uint32(size) <= m.heapTop {
		return m.memory, int(address - DataBase), nil
	}
	stackBase := StackTop + 4 - uint32(len(m.stack))
	if address >= stackBase && address+uint32(size) <= StackTop+4 {
		return m.stack, int(address - stackBase), nil
	}
	if address < stackBase && address >= stackBase-0x1000 {
		return nil, 0, m.fail("stack-overflow", "Stack overflow: the program exceeded %d bytes of stack.", len(m.stack))
	}
	return nil, 0, m.fail("bad-address", "Exception: bad data address 0x%08x.", address)
}

func (m *machine) loadWord(address uint32) (int32, error) {
	if address%4 != 0 {
		return 0, m.fail("bad-address", "Exception: unaligned word load at 0x%08x.", address)
	}
	memory, offset, err := m.translate(address, 4)
	if err != nil {
		return 0, err
	}
	return int32(uint32(memory[offset]) | uint32(memory[offset+1])<<8 | uint32(memory[offset+2])<<16 | uint32(memory[offset+3])<<24), nil
}

func (m *machine) storeWord(address uint32, value int32) error {
	if address%4 != 0 {
		return m.fail("bad-address", "Exception: unaligned word store at 0x%08x.", address)
	}
	memory, offset, err := m.translate(address, 4)
	if err != nil {
		return err
	}
	putWord(memory, offset, uint32(value))
	return nil
}

func (m *machine) loadByte(address uint32) (byte, error) {
	memory, offset, err := m.translate(address, 1)
	if err != nil {
		return 0, err
	}
	return memory[offset], nil
}

func (m *machine) storeByte(address uint32, value byte) error {
	memory, offset, err := m.translate(address, 1)
	if err != nil {
		return err
	}
	memory[offset] = value
	return nil
}

func (m *machine) set(register int, value int32) {
	if register != 0 {
		m.reg[register] = value
	}
}

func (m *machine) value(op operand) int32 {
	if op.kind == opRegister {
		return m.reg[op.register]
	}
	return int32(op.value)
}

// call runs MIPS code at address until it returns to the driver.
func (m *machine) call(address uint32) error {
	m.reg[31] = int32(haltAddr)
	m.pc = address
	for m.pc != haltAddr {
		if err := m.step(); err != nil {
			return err
		}
	}
	return nil
}

func (m *machine) step() error {
	m.steps++
	if m.steps > m.options.Limits.MaxSteps {
		return m.fail("step-limit", "Execution stopped after %d instructions (step limit).", m.options.Limits.MaxSteps)
	}
	if m.steps%4096 == 0 && m.ctx != nil && m.ctx.Err() != nil {
		return m.fail("cancelled", "Execution cancelled.")
	}
	if name, ok := m.program.natives[m.pc]; ok {
		if err := m.native(name); err != nil {
			return err
		}
		m.pc = uint32(m.reg[31])
		return nil
	}
	if m.pc < TextBase || (m.pc-TextBase)%4 != 0 || int((m.pc-TextBase)/4) >= len(m.program.text) {
		return m.fail("bad-address", "Exception: bad instruction address 0x%08x.", m.pc)
	}
	ins := m.program.text[(m.pc-TextBase)/4]
	next := m.pc + 4
	ops := ins.operands
	reg := func(index int) int { return ops[index].register }
	address := func(op operand) uint32 {
		if op.kind == opMemory {
			return uint32(m.reg[op.register] + int32(op.value))
		}
		return uint32(op.value)
	}
	branch := func(taken bool, target operand) {
		if taken {
			next = uint32(target.value)
		}
	}
	switch ins.op {
	case "la":
		m.set(reg(0), int32(address(ops[1])))
	case "li":
		m.set(reg(0), int32(ops[1].value))
	case "lw":
		value, err := m.loadWord(address(ops[1]))
		if err != nil {
			return err
		}
		m.set(reg(0), value)
	case "sw":
		if err := m.storeWord(address(ops[1]), m.reg[reg(0)]); err != nil {
			return err
		}
	case "lb", "lbu":
		value, err := m.loadByte(address(ops[1]))
		if err != nil {
			return err
		}
		if ins.op == "lb" {
			m.set(reg(0), int32(int8(value)))
		} else {
			m.set(reg(0), int32(value))
		}
	case "sb":
		if err := m.storeByte(address(ops[1]), byte(m.reg[reg(0)])); err != nil {
			return err
		}
	case "move":
		m.set(reg(0), m.reg[reg(1)])
	case "addiu", "addi":
		m.set(reg(0), m.reg[reg(1)]+int32(ops[2].value))
	case "andi":
		m.set(reg(0), m.reg[reg(1)]&int32(ops[2].value))
	case "ori":
		m.set(reg(0), m.reg[reg(1)]|int32(ops[2].value))
	case "slti":
		m.set(reg(0), boolInt(m.reg[reg(1)] < int32(ops[2].value)))
	case "addu", "add":
		m.set(reg(0), m.reg[reg(1)]+m.value(ops[2]))
	case "subu", "sub":
		m.set(reg(0), m.reg[reg(1)]-m.value(ops[2]))
	case "mul":
		m.set(reg(0), m.reg[reg(1)]*m.value(ops[2]))
	case "div", "rem":
		divisor := m.value(ops[2])
		if divisor == 0 {
			return m.fail("division-by-zero", "Exception: division by zero.")
		}
		dividend := m.reg[reg(1)]
		if dividend == -1<<31 && divisor == -1 {
			if ins.op == "div" {
				m.set(reg(0), dividend)
			} else {
				m.set(reg(0), 0)
			}
			break
		}
		if ins.op == "div" {
			m.set(reg(0), dividend/divisor)
		} else {
			m.set(reg(0), dividend%divisor)
		}
	case "and":
		m.set(reg(0), m.reg[reg(1)]&m.value(ops[2]))
	case "or":
		m.set(reg(0), m.reg[reg(1)]|m.value(ops[2]))
	case "xor":
		m.set(reg(0), m.reg[reg(1)]^m.value(ops[2]))
	case "slt":
		m.set(reg(0), boolInt(m.reg[reg(1)] < m.value(ops[2])))
	case "sltu":
		m.set(reg(0), boolInt(uint32(m.reg[reg(1)]) < uint32(m.value(ops[2]))))
	case "neg", "negu":
		m.set(reg(0), -m.reg[reg(1)])
	case "not":
		m.set(reg(0), ^m.reg[reg(1)])
	case "sll":
		m.set(reg(0), m.reg[reg(1)]<<uint(ops[2].value&31))
	case "srl":
		m.set(reg(0), int32(uint32(m.reg[reg(1)])>>uint(ops[2].value&31)))
	case "sra":
		m.set(reg(0), m.reg[reg(1)]>>uint(ops[2].value&31))
	case "beq":
		branch(m.reg[reg(0)] == m.value(ops[1]), ops[2])
	case "bne":
		branch(m.reg[reg(0)] != m.value(ops[1]), ops[2])
	case "blt":
		branch(m.reg[reg(0)] < m.value(ops[1]), ops[2])
	case "ble":
		branch(m.reg[reg(0)] <= m.value(ops[1]), ops[2])
	case "bgt":
		branch(m.reg[reg(0)] > m.value(ops[1]), ops[2])
	case "bge":
		branch(m.reg[reg(0)] >= m.value(ops[1]), ops[2])
	case "beqz":
		branch(m.reg[reg(0)] == 0, ops[1])
	case "bnez":
		branch(m.reg[reg(0)] != 0, ops[1])
	case "b", "j":
		next = uint32(ops[0].value)
	case "jal":
		m.reg[31] = int32(next)
		next = uint32(ops[0].value)
	case "jalr":
		target := uint32(m.reg[reg(0)])
		m.reg[31] = int32(next)
		next = target
	case "jr":
		next = uint32(m.reg[reg(0)])
	case "mflo":
		m.set(reg(0), m.lo)
	case "mfhi":
		m.set(reg(0), m.hi)
	case "nop":
	case "syscall":
		halted, err := m.syscall()
		if err != nil {
			return err
		}
		if halted {
			next = haltAddr
			m.halted = true
		}
	default:
		return m.fail("bad-instruction", "Exception: unsupported instruction %s.", ins.op)
	}
	if m.reg[29] < 0 || uint32(m.reg[29]) < StackTop+4-uint32(len(m.stack)) {
		return m.fail("stack-overflow", "Stack overflow: the program exceeded %d bytes of stack.", len(m.stack))
	}
	m.pc = next
	return nil
}

func boolInt(value bool) int32 {
	if value {
		return 1
	}
	return 0
}

func (m *machine) write(text string) error {
	if m.output.Len()+len(text) > m.options.Limits.MaxOutputBytes {
		return m.fail("output-limit", "Execution stopped: program output exceeded %d bytes.", m.options.Limits.MaxOutputBytes)
	}
	m.output.WriteString(text)
	return nil
}
