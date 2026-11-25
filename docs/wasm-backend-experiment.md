# Direct Stack IR → WebAssembly experiment (#86)

Decision: keep this backend test-only. A working vertical slice demonstrates direct
binary emission, not a second production runtime. The existing VM remains the
semantic, resource-policy and debugger reference. This differs from #85, which
compiles the Go implementation itself to Wasm.

## Reproduce

With Go and Node available:

```sh
go test ./internal/compiler -run '^TestWasmBackendPrototype' -v -count=1
```

`internal/compiler/wasm_backend_prototype_test.go` emits temporary modules from
verified, unoptimized Stack IR. The Node runner validates each binary with
`WebAssembly.validate`, instantiates it and executes its exported `run` function.
Tests skip execution explicitly when Node is absent; the refusal tests still run.
No WAT compiler, server, download or production flag is required. Each process has
a 20-second outer deadline; each execution has a 1,000 IR-instruction allowance.

## Executable subset and representation

The only user class is Main, without attributes. It can inherit Object or IO.
Its one pre-existing receiver has i32 identity 1; allocation and initialization
are outside the slice. Int and Bool are unboxed i32. Locals and formals must be
Int/Bool; helper methods return Int/Bool. Main may return an object or scalar,
but calling main from COOL is refused. These restrictions prevent treating boxed
primitive values as object identities. Only self receivers are accepted.

Supported operations are scalar constants, lexical let scopes (including
shadowing), loads, arithmetic, Boolean negation, comparisons, duplication/drop,
return, scalar helper calls and IO.out_int. Argument expressions execute in the
existing IR order, followed by the receiver; Wasm parameters use that same order.
Dynamic self calls have a proven singleton target; explicit static dispatch uses
the class-table target. There is no general dynamic dispatch implementation.

The binary contains type/import/function/export/code sections and a
`cooler.spans` custom section. Every original IR instruction calls `env.tick`
with an index into the original source spans. `env.fail` transports the division
diagnostic and span; `env.out_int` appends decimal output and returns self.
There are no filesystem, clock, network or input imports and no linear memory.
The host bounds recursion through instruction fuel. This is not parity with the
VM's call-frame, object, trace or instruction accounting: the wrapper omits the
VM bootstrap and initializer instructions. Budget exhaustion is a host outcome,
not a fabricated COOL diagnostic.

Wasm i32 add/subtract/multiply wrap naturally. Signed division needs explicit
guards: zero reports COL6015 at the original IR span; INT_MIN / -1 returns
INT_MIN instead of trapping. Encoding and arithmetic follow the
[WebAssembly core specification](https://webassembly.github.io/spec/core/).

## Coverage and observations

The comparison corpus checks overflow, division overflow and zero, argument side
effects and ordering (`136`), static calls, shadowed locals, true/false Boolean
results and singleton object identity. It compares exact output, scalar/identity
result and runtime diagnostic code plus full span against the native VM.
An additional recursive program proves host fuel termination. Refusal tests
cover strings, attributes, conditional CFG, allocation, Object-typed locals and
unsupported builtins. All unsupported opcodes return an emitter error before
any module is executed, including assignment, loops and case.

Initial local measurements: Windows, Go 1.26.4, Node 26.7.0. Eight initial corpus
modules including JSON source metadata measured 598–2,431 bytes. A 100-execution
warm batch per module averaged 0.0096–0.0178 ms per invocation, including tick
imports, output formatting and caught errors. These tiny samples include JIT
warmup and are not a throughput claim; compilation, process startup, native VM
timing and peak memory were not measured. The verbose test prints fresh numbers.
No performance advantage is asserted. Other JS engines were not validated.

## Architecture for expansion

1. General CFG: begin with a bounded program-counter dispatcher (`loop` and
   `br_table`), one case per verified basic block. Materialize cross-block stack
   values into typed locals from verifier stack states. Lower conditional edges
   and case selection explicitly. Later compare structured relooping for size
   and speed; preserve instruction origins in either approach.
2. Values and objects: define a tagged ABI before expanding the accepted subset.
   Object headers need class tag, byte size and dispatch-table identity, followed
   by inherited fields in class-table order. Zero represents void; Int/Bool must
   box when flowing into Object. Equality must distinguish primitive value
   equality from object identity. Allocation must run ancestors' initializers
   in the VM order; SELF_TYPE uses the receiver's dynamic class.
3. Calls: stable method slots plus Wasm tables and `call_indirect` implement
   dynamic dispatch; static dispatch uses direct calls. Null-receiver checks,
   evaluation order, frame bounds and stack traces require explicit lowering.
4. Strings and builtins: specify immutable UTF-8 byte storage with length and
   bounded allocation; port the VM's exact length/substr/concat/input behavior
   with Unicode differential fixtures before accepting String. Host imports
   should implement only bounded I/O. Abort, type_name and copy require the same
   observable contracts as the VM, including fresh identity for copy.
5. Memory: start with checked bump allocation and a fixed maximum number of
   Wasm memory pages, fail on quota exhaustion; this intentionally cannot reclaim
   objects. Measure lifetime workloads before selecting a tracing collector or
   Wasm GC. Neither host GC nor memory.grow alone implements VM object limits.
6. Diagnostics and tools: retain source IDs, UTF-16 spans and origin indexes in
   compact metadata; map explicit failure imports to stable diagnostics.
   Unexpected engine traps are backend failures. Debugger steps, retained
   history, breakpoints, resumable input and resource-policy parity need their
   own state-machine ABI, not reconstruction from native engine stack traces.

Adoption requires differential conformance for all accepted opcodes, objects,
inheritance, strings, input, error ordering and bounds; verifier-driven malformed
IR tests; several engines; cold/warm size and performance measurements; and a
decision on debugger compatibility. Until then, refusal is preferable to silently
changing a valid COOL program's meaning.
