// SPDX-License-Identifier: AGPL-3.0-or-later
package compiler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Test-only vertical slice: scalar locals and calls on the singleton Main.
// Unsupported constructs fail closed; shipped code never invokes this emitter.
type wasmBytes []byte

func (b *wasmBytes) u(n int) {
	for {
		v := byte(n & 127)
		n >>= 7
		if n != 0 {
			v |= 128
		}
		*b = append(*b, v)
		if n == 0 {
			return
		}
	}
}
func (b *wasmBytes) s(n int32) {
	for {
		v := byte(n & 127)
		n >>= 7
		done := (n == 0 && v&64 == 0) || (n == -1 && v&64 != 0)
		if !done {
			v |= 128
		}
		*b = append(*b, v)
		if done {
			return
		}
	}
}
func (b *wasmBytes) name(s string) { b.u(len(s)); *b = append(*b, s...) }
func (b *wasmBytes) section(id byte, data wasmBytes) {
	*b = append(*b, id)
	b.u(len(data))
	*b = append(*b, data...)
}
func (b *wasmBytes) ins(op byte, n int) { *b = append(*b, op); b.u(n) }
func (b *wasmBytes) constant(n int32)   { *b = append(*b, 0x41); b.s(n) }

func experimentalWasm(e *executable) ([]byte, error) {
	for name, c := range e.Table.Classes {
		if !c.Builtin && (name != "Main" || len(e.Classes[name].Attributes) > 0) {
			return nil, fmt.Errorf("unsupported class or attributes: %s", name)
		}
	}
	methods := []*compiledMethod{}
	indexes := map[string]int{}
	for _, m := range e.MethodOrder {
		if m.Synthetic {
			continue
		}
		if m.Owner != "Main" {
			return nil, fmt.Errorf("unsupported owner")
		}
		if m.Name != "main" && m.ReturnType != typeInt && m.ReturnType != typeBool {
			return nil, fmt.Errorf("unsupported return type")
		}
		for _, typ := range m.FormalTypes {
			if typ != typeInt && typ != typeBool {
				return nil, fmt.Errorf("unsupported formal")
			}
		}
		indexes[methodKey(m.Owner, m.Name)] = 3 + len(methods)
		methods = append(methods, m)
	}
	types := wasmBytes{}
	// tick and fail return no values.
	types.u(4 + len(methods))
	for i, arity := range append([]int{2, 1, 2, 0}, wasmArities(methods)...) {
		types = append(types, 0x60)
		types.u(arity)
		for j := 0; j < arity; j++ {
			types = append(types, 0x7f)
		}
		if i == 1 || i == 2 {
			types = append(types, 0)
		} else {
			types = append(types, 1, 0x7f)
		}
	}
	imports := wasmBytes{3}
	for i, name := range []string{"out_int", "tick", "fail"} {
		imports.name("env")
		imports.name(name)
		imports = append(imports, 0)
		imports.u(i)
	}
	funcs := wasmBytes{}
	funcs.u(len(methods) + 1)
	for i := range methods {
		funcs.u(4 + i)
	}
	funcs.u(3)
	exports := wasmBytes{1}
	exports.name("run")
	exports = append(exports, 0)
	exports.u(3 + len(methods))
	bodies := wasmBytes{}
	bodies.u(len(methods) + 1)
	spans := []Span{}
	for _, m := range methods {
		code := wasmBytes{}
		left, right := len(m.Formals)+1, len(m.Formals)+2
		next := right + 1
		scopes := []map[string]int{{}}
		for i, name := range m.Formals {
			scopes[0][name] = i
		}
		for pos, in := range m.Code {
			spanID := len(spans)
			spans = append(spans, in.Span)
			code.constant(int32(spanID))
			code.ins(0x10, 1)
			switch in.Op {
			case opPushInt:
				code.constant(int32(in.Int))
			case opPushBool:
				if in.Bool {
					code.constant(1)
				} else {
					code.constant(0)
				}
			case opLoadSelf:
				code.ins(0x20, len(m.Formals))
			case opEnterScope:
				scopes = append(scopes, map[string]int{})
			case opExitScopeKeep:
				scopes = scopes[:len(scopes)-1]
			case opBind:
				if in.ValueType != typeInt && in.ValueType != typeBool {
					return nil, fmt.Errorf("unsupported binding %s", in.ValueType)
				}
				scopes[len(scopes)-1][in.Text] = next
				code.ins(0x21, next)
				next++
			case opLoad:
				found := false
				for i := len(scopes) - 1; i >= 0; i-- {
					if slot, ok := scopes[i][in.Text]; ok {
						code.ins(0x20, slot)
						found = true
						break
					}
				}
				if !found {
					return nil, fmt.Errorf("unsupported load %s", in.Text)
				}
			case opPop:
				code = append(code, 0x1a)
			case opDup:
				code.ins(0x22, left)
				code.ins(0x20, left)
			case opAdd:
				code = append(code, 0x6a)
			case opSub:
				code = append(code, 0x6b)
			case opMul:
				code = append(code, 0x6c)
			case opNeg:
				code.constant(-1)
				code = append(code, 0x6c)
			case opNot:
				code = append(code, 0x45)
			case opLess:
				code = append(code, 0x48)
			case opLessEqual:
				code = append(code, 0x4c)
			case opEqual:
				code = append(code, 0x46)
			case opDiv:
				code.ins(0x21, right)
				code.ins(0x21, left)
				code.ins(0x20, right)
				code = append(code, 0x45, 0x04, 0x40)
				code.constant(6015)
				code.constant(int32(spanID))
				code.ins(0x10, 2)
				code = append(code, 0, 0x0b)
				code.ins(0x20, left)
				code.constant(-2147483648)
				code = append(code, 0x46)
				code.ins(0x20, right)
				code.constant(-1)
				code = append(code, 0x46, 0x71, 0x04, 0x7f)
				code.constant(-2147483648)
				code = append(code, 0x05)
				code.ins(0x20, left)
				code.ins(0x20, right)
				code = append(code, 0x6d, 0x0b)
			case opCall:
				if pos == 0 || m.Code[pos-1].Op != opLoadSelf || in.Text == "main" {
					return nil, fmt.Errorf("unsupported receiver or main call")
				}
				lookup := "Main"
				if in.StaticType != "" {
					lookup = in.StaticType
				}
				info, ok := e.Table.LookupMethod(lookup, in.Text)
				if !ok {
					return nil, fmt.Errorf("missing method")
				}
				index, ok := indexes[methodKey(info.Owner, info.Name)]
				if info.Builtin && info.Owner == "IO" && info.Name == "out_int" {
					index = 0
					ok = true
				}
				if !ok {
					return nil, fmt.Errorf("unsupported call %s", in.Text)
				}
				code.ins(0x10, index)
			case opReturn:
				code = append(code, 0x0f)
			default:
				return nil, fmt.Errorf("unsupported instruction %s", in.Op)
			}
		}
		body := wasmBytes{1}
		body.u(next - len(m.Formals) - 1)
		body = append(body, 0x7f)
		body = append(body, code...)
		body = append(body, 0x0b)
		bodies.u(len(body))
		bodies = append(bodies, body...)
	}
	wrapper := wasmBytes{0}
	wrapper.constant(1)
	wrapper.ins(0x10, indexes["Main.main"])
	wrapper = append(wrapper, 0x0b)
	bodies.u(len(wrapper))
	bodies = append(bodies, wrapper...)
	out := wasmBytes{0, 97, 115, 109, 1, 0, 0, 0}
	out.section(1, types)
	out.section(2, imports)
	out.section(3, funcs)
	out.section(7, exports)
	out.section(10, bodies)
	encoded, err := json.Marshal(spans)
	if err != nil {
		return nil, err
	}
	custom := wasmBytes{}
	custom.name("cooler.spans")
	custom = append(custom, encoded...)
	out.section(0, custom)
	return out, nil
}
func wasmArities(methods []*compiledMethod) []int {
	out := []int{}
	for _, m := range methods {
		out = append(out, len(m.Formals)+1)
	}
	return out
}

func TestWasmBackendPrototype(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node required for actual Wasm execution")
	}
	for _, tc := range []struct{ name, body, extra string }{
		{"overflow", "out_int(2147483647+1)", ""},
		{"division overflow", "out_int((~2147483647-1)/(~1))", ""},
		{"division error", "out_int(4/0)", ""},
		{"evaluation order", "out_int(f({out_int(1);2;},{out_int(3);4;}))", "f(a:Int,b:Int):Int {a+b};"},
		{"static call", "out_int(self@Main.f(7))", "f(a:Int):Int {a*3-1};"},
		{"shadowing", "out_int(let x:Int<-9 in (let x:Int<-x+1 in x)+x)", ""},
		{"boolean", "not (3<=2)", ""},
		{"false boolean", "3<2", ""},
		{"identity", "self=self", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := inlineExperimentExecutable(t, "class Main inherits IO {"+tc.extra+"main():Object {"+tc.body+"};};")
			data, err := experimentalWasm(e)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "program.wasm")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			raw, err := exec.CommandContext(ctx, node, "testdata/wasm_prototype_runner.mjs", path).CombinedOutput()
			if err != nil {
				t.Fatalf("%v: %s", err, raw)
			}
			var got struct {
				Output string
				Result int32
				Error  *struct {
					Code string
					Span Span
				}
				Bytes  int
				MeanMs float64
			}
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			vm := newVM(e)
			vm.Run(1000, nil)
			if got.Output != strings.Join(vm.output, "") {
				t.Fatalf("output %q vs %q", got.Output, vm.output)
			}
			failure := firstRuntimeError(vm.diagnostics)
			if failure != nil {
				if got.Error == nil || got.Error.Code != failure.Code || got.Error.Span != failure.Span {
					t.Fatalf("error mismatch: %+v vs %+v", got.Error, failure)
				}
			} else if got.Error != nil {
				t.Fatalf("unexpected error %+v", got.Error)
			}
			if failure == nil {
				if len(vm.stack) != 1 {
					t.Fatalf("unexpected VM stack: %+v", vm.stack)
				}
				value := vm.stack[0]
				want := int32(1) // singleton Main identity
				if value.kind == valueInt {
					want = int32(value.i)
				} else if value.kind == valueBool && !value.b {
					want = 0
				}
				if got.Result != want {
					t.Fatalf("result %d vs VM %+v", got.Result, value)
				}
			}
			t.Logf("bytes=%d warm mean=%.6fms output=%q result=%d", got.Bytes, got.MeanMs, got.Output, got.Result)
		})
	}
}

func TestWasmBackendPrototypeRefusals(t *testing.T) {
	for _, source := range []string{
		`class Main {main():Object {"text"};};`,
		`class Main {x:Int;main():Object {x};};`,
		`class Main {main():Object {if true then 1 else 2 fi};};`,
		`class Main {main():Object {new Main};};`,
		`class Main {main():Object {let x:Object<-1 in x=self};};`,
		`class Main {main():Object {self.copy()};};`,
	} {
		if _, err := experimentalWasm(inlineExperimentExecutable(t, source)); err == nil {
			t.Fatalf("accepted unsupported source %s", source)
		}
	}
}

func TestWasmBackendPrototypeBudget(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node required for actual Wasm execution")
	}
	e := inlineExperimentExecutable(t, `class Main {f():Int {f()}; main():Object {f()};};`)
	data, err := experimentalWasm(e)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "recursive.wasm")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	raw, err := exec.CommandContext(ctx, node, "testdata/wasm_prototype_runner.mjs", path).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	var got struct {
		Ticks int
		Error struct {
			Code string
			Span Span
		}
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Ticks != 1001 || got.Error.Code != "budget" || got.Error.Span.StartLine != 1 {
		t.Fatalf("budget did not stop recursion: %s", raw)
	}
}
