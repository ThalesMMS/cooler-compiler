// SPDX-License-Identifier: AGPL-3.0-or-later
package compiler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Test-only RV32IM/RARS emitter, deliberately restricted to a straight-line main.
func experimentalRISCV(e *executable) (string, error) {
	for name, c := range e.Table.Classes {
		if !c.Builtin && (name != "Main" || len(e.Classes[name].Attributes) != 0) {
			return "", fmt.Errorf("unsupported class/attributes")
		}
	}
	for _, m := range e.MethodOrder {
		if !m.Synthetic && m.Name != "main" {
			return "", fmt.Errorf("helper methods not supported")
		}
	}
	m := e.Methods["Main.main"]
	if m == nil || len(m.Code) > 256 {
		return "", fmt.Errorf("missing or oversized main")
	}
	var out strings.Builder
	emit := func(format string, args ...any) { fmt.Fprintf(&out, format+"\n", args...) }
	push := func(reg string) { emit("  addi sp, sp, -16\n  sw %s, 0(sp)", reg) }
	pop := func(reg string) { emit("  lw %s, 0(sp)\n  addi sp, sp, 16", reg) }
	// Separate local area and 16-byte operand slots preserve stack alignment.
	emit("# RV32IM; RARS environment; singleton self=1; no general object ABI\n.text\n.globl main\nmain:\n  andi sp, sp, -16\n  addi sp, sp, -1024\n  mv s0, sp")
	scopes := []map[string]int{{}}
	next := 0
	for index, in := range m.Code {
		emit("# IR %d %s | source %q %d:%d-%d:%d", index, in.Op, in.Span.SourceID, in.Span.StartLine, in.Span.StartColumn, in.Span.EndLine, in.Span.EndColumn)
		switch in.Op {
		case opPushInt:
			emit("  li t0, %d", in.Int)
			push("t0")
		case opPushBool:
			v := 0
			if in.Bool {
				v = 1
			}
			emit("  li t0, %d", v)
			push("t0")
		case opLoadSelf:
			emit("  li t0, 1")
			push("t0")
		case opEnterScope:
			scopes = append(scopes, map[string]int{})
		case opExitScopeKeep:
			scopes = scopes[:len(scopes)-1]
		case opBind:
			if in.ValueType != typeInt && in.ValueType != typeBool {
				return "", fmt.Errorf("unsupported local")
			}
			pop("t0")
			emit("  sw t0, %d(s0)", next*4)
			scopes[len(scopes)-1][in.Text] = next
			next++
		case opLoad:
			found := false
			for i := len(scopes) - 1; i >= 0; i-- {
				if slot, ok := scopes[i][in.Text]; ok {
					emit("  lw t0, %d(s0)", slot*4)
					found = true
					break
				}
			}
			if !found {
				return "", fmt.Errorf("unknown local")
			}
			push("t0")
		case opPop:
			pop("t0")
		case opDup:
			emit("  lw t0, 0(sp)")
			push("t0")
		case opAdd, opSub, opMul, opDiv, opLess, opLessEqual, opEqual:
			pop("t1")
			pop("t0")
			switch in.Op {
			case opAdd:
				emit("  add t0, t0, t1")
			case opSub:
				emit("  sub t0, t0, t1")
			case opMul:
				emit("  mul t0, t0, t1")
			case opDiv:
				emit("  beq t1, zero, cooler_div_zero\n  div t0, t0, t1")
			case opLess:
				emit("  slt t0, t0, t1")
			case opLessEqual:
				emit("  slt t0, t1, t0\n  xori t0, t0, 1")
			case opEqual:
				emit("  xor t0, t0, t1\n  sltiu t0, t0, 1")
			}
			push("t0")
		case opNeg:
			pop("t0")
			emit("  sub t0, zero, t0")
			push("t0")
		case opNot:
			pop("t0")
			emit("  sltiu t0, t0, 1")
			push("t0")
		case opCall:
			if index == 0 || m.Code[index-1].Op != opLoadSelf || in.Text != "out_int" {
				return "", fmt.Errorf("unsupported call")
			}
			lookup := "Main"
			if in.StaticType != "" {
				lookup = in.StaticType
			}
			info, ok := e.Table.LookupMethod(lookup, in.Text)
			if !ok || !info.Builtin || info.Owner != "IO" {
				return "", fmt.Errorf("unsupported dispatch")
			}
			pop("t1")
			pop("a0")
			emit("  li a7, 1\n  ecall")
			push("t1")
		case opReturn:
			emit("  li a0, 0\n  li a7, 93\n  ecall")
		default:
			return "", fmt.Errorf("unsupported opcode %s", in.Op)
		}
	}
	emit("cooler_div_zero:\n  # COL6015 division by zero; process status 15\n  li a0, 15\n  li a7, 93\n  ecall")
	return out.String(), nil
}

func TestRISCVBackendPrototype(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"multiply", "out_int(7*6)"},
		{"multiply overflow", "out_int(2147483647*2)"},
		{"add overflow", "out_int(2147483647+1)"},
		{"division", "out_int((~7)/3)"},
		{"division overflow", "out_int((~2147483647-1)/(~1))"},
		{"zero", "{out_int(12);out_int(4/0);}"},
		{"order", "out_int({out_int(1);2;}+{out_int(3);4;})"},
		{"locals", "out_int(let x:Int<-9 in (let x:Int<-x+1 in x)+x)"},
		{"bool not", "not (3<=2)"},
		{"bool less", "3<2"},
		{"bool identity", "self=self"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := inlineExperimentExecutable(t, "class Main inherits IO {main():Object {"+tc.body+"};};")
			assembly, err := experimentalRISCV(e)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(assembly, "# IR 0") || !strings.Contains(assembly, "source") {
				t.Fatal("source mapping absent")
			}
			vm := newVM(e)
			vm.Run(1000, nil)
			wantOutput := strings.Join(vm.output, "")
			if strings.HasPrefix(tc.name, "bool ") {
				// Test-only observation of the normally discarded main result.
				assembly = strings.Replace(assembly, "  li a0, 0\n  li a7, 93", "  lw a0, 0(sp)\n  li a7, 1\n  ecall\n  li a0, 0\n  li a7, 93", 1)
				if len(vm.stack) != 1 || vm.stack[0].kind != valueBool {
					t.Fatal("missing VM Boolean result")
				}
				wantOutput = "0"
				if vm.stack[0].b {
					wantOutput = "1"
				}
			}
			jar := os.Getenv("COOLER_RARS_JAR")
			if jar == "" {
				t.Skip("set COOLER_RARS_JAR for actual RARS execution")
			}
			java, err := exec.LookPath("java")
			if err != nil {
				t.Fatal("COOLER_RARS_JAR requires Java on PATH")
			}
			path := filepath.Join(t.TempDir(), "program.s")
			if err := os.WriteFile(path, []byte(assembly), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, java, "-Djava.awt.headless=true", "-jar", jar, "nc", "me", "ae2", "se3", "10000", path)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err = cmd.Run()
			wantStatus := 0
			if firstRuntimeError(vm.diagnostics) != nil {
				wantStatus = 15
			}
			status := 0
			if err != nil {
				var failure *exec.ExitError
				if errors.As(err, &failure) {
					status = failure.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			if status != wantStatus || stdout.String() != wantOutput {
				t.Fatalf("status=%d want=%d output=%q VM=%q stderr=%s", status, wantStatus, stdout.String(), vm.output, stderr.String())
			}
			if strings.Contains(stderr.String(), "maximum") {
				t.Fatalf("simulator limit reached: %s", stderr.String())
			}
			t.Logf("executed RARS: output=%q status=%d assembly=%d bytes", stdout.String(), status, len(assembly))
		})
	}
}

func TestRISCVBackendPrototypeEscapesSourceID(t *testing.T) {
	e := inlineExperimentExecutable(t, `class Main {main():Object {1};};`)
	id := "file\r\n  li a0, 99\n# injected"
	for i := range e.Methods["Main.main"].Code {
		e.Methods["Main.main"].Code[i].Span.SourceID = id
	}
	assembly, err := experimentalRISCV(e)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(assembly, id) || !strings.Contains(assembly, fmt.Sprintf("source %q", id)) {
		t.Fatalf("source ID escaped incorrectly: %s", assembly)
	}
}

func TestRISCVBackendPrototypeRefusals(t *testing.T) {
	for _, source := range []string{
		`class Main {main():Object {"text"};};`,
		`class Main {x:Int; main():Object {x};};`,
		`class Main {f():Int {1}; main():Object {f()};};`,
		`class Main {main():Object {while false loop 1 pool};};`,
		`class Main {main():Object {new Main};};`,
		`class Main {main():Object {let x:Object<-1 in x=self};};`,
	} {
		if _, err := experimentalRISCV(inlineExperimentExecutable(t, source)); err == nil {
			t.Fatalf("accepted unsupported source %s", source)
		}
	}
}
