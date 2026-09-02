# Educational VM runtime

Cooler's VM executes the [stack IR](stack-ir.md) for teaching, inspection, and
deterministic local tests. It is not the Stanford COOL runtime, a security
sandbox, or a portable production VM.

## Value and object model

The internal value kinds are `Void`, `Int`, `String`, `Bool`, and object
reference. `Int` uses a dedicated `int32`-backed value rather than Go `int`;
other basic values are immediate Go values. A non-basic object has a
monotonic session-local ID, runtime class name, and field map; public snapshots
display it as `Class#ID`. Object equality is identity. Basic equality compares
values, and void equals only void.

Defaults are `0`, `""`, `false`, and void for object types. `Object.copy` makes
a shallow copy with a new identity and the receiver's dynamic class. Referenced
objects remain shared; immediate `Int`, `String`, and `Bool` values are returned
as-is. Monotonic object IDs exist only in debugger displays and never determine
COOL equality. Go owns memory reclamation, so there is no COOL heap layout or
selectable garbage collector.

## Integer model

Every runtime `Int`, bytecode integer constant, local/attribute value, trace
entry, debugger display, and JSON snapshot uses the same signed 32-bit
abstraction. Public values are rendered as signed decimal strings, so their JSON
shape is stable across host architectures.

The lexer separately accepts only non-negative decimal literals through
`2147483647`; `-2147483648` can be produced by an expression such as
`(~2147483647) - 1` or read by `IO.in_int`. Addition, subtraction,
multiplication, and integer complement wrap modulo 2^32. Division truncates
toward zero; zero is a runtime error, while `-2147483648 / -1` returns
`-2147483648`. Comparisons and equality operate on the resulting signed values.

The COOL manual does not specify arithmetic overflow. Cooler therefore treats
this modular behavior as its explicit deterministic policy, consistent with a
classic 32-bit representation but independent of any host Go word size. A local
reference toolchain may be used for differential observations; it is not
required for this defined behavior.

## Initialization and dispatch

Allocation first creates every inherited and local attribute with its default.
Generated `$init` methods then execute from the root ancestor toward the leaf,
evaluating explicit initializers in source declaration order. `new SELF_TYPE`
allocates the runtime class of `self`.

Normal calls dispatch from the receiver's runtime type; `expr@Type.method()`
uses the specified static lookup type. A frame contains receiver, formal locals,
nested `let`/`case` scopes, method-local IP, and operand-stack base. Return
truncates the operand stack to that base and pushes one result into the caller.
The receiver is evaluated first, arguments are evaluated left to right, and
binary operands are evaluated left to right. Implicit dispatch still performs
runtime lookup on `self`; static dispatch starts at the named class.

`case` evaluates its scrutinee exactly once. Void produces `COL6030`; otherwise
the VM chooses the branch with the shortest inheritance distance regardless of
source order and binds the original runtime value. If no branch type is an
ancestor, execution stops with `COL6031`. Parent lookup and distance are
iterative/cycle guarded for deep hierarchies.

Builtins are `Object.abort`, `Object.type_name`, `Object.copy`,
`IO.out_string`, `IO.out_int`, `IO.in_string`, `IO.in_int`, `String.length`,
`String.concat`, and `String.substr`. Input is line-oriented: exhaustion returns
`""`; `in_int` accepts trimmed signed decimal values from `-2147483648` through
`2147483647` and returns `0` for an invalid or out-of-range line. `out_int`
formats the signed 32-bit value in canonical base-10 form.

The cross-surface [program input contract](input.md) defines channel selection,
EOF, terminal, GUI, API, reset, and replay behavior. Interactive sessions change
only what happens when the pre-supplied lines are
exhausted. The VM enters `waiting-input` before the `in_string`/`in_int` call:
the instruction pointer, executed-instruction count, operand stack, and call
result remain unchanged. `waitingInput` identifies the builtin, source span,
method/frame, preceding output, zero-based input position, and monotonically
increasing request sequence. Supplying exactly that sequence accepts one
bounded line and executes exactly the pending input opcode. Stale, duplicate,
oversized, and out-of-state submissions are rejected. Non-interactive runs and
an explicit interactive EOF retain the deterministic `""`/`0` fallback.

Each consumed line creates a deterministic session-local input event containing
its sequence, builtin, span, input position, and whether it was supplied live.
Snapshots/reports expose only the event count. `InputEvents(false)` returns the
metadata log with contents redacted; line contents require the caller's
explicit `InputEvents(true)` request. Reset discards live lines and events,
rewinds the configured pre-supplied input, and creates fresh request sequences.

The classic manual describes String operations in characters but does not
define a Unicode encoding model; classic COOL source is ASCII. Cooler explicitly
extends string values to Unicode and defines `String.length` and `substr(i, l)`
in decoded Unicode code points, not UTF-8 bytes or UTF-16 units. `concat`
preserves decoded values. Substring bounds avoid adding `i + l`, so even
integer addition cannot overflow bounds validation. See the broader
[language compatibility contract](cool-language.md).

## Deterministic limits

Every execution carries one normalized `VMPolicy`. A zero field selects the
desktop default; negative values, values above the local ceiling, and a per-run
instruction limit above the lifetime limit are rejected before compilation as
`COL0005`. Reports include the complete normalized policy, so an execution can
be reproduced without relying on ambient defaults.

| Policy field | Default | Maximum override | Result when exceeded |
| --- | ---: | ---: | --- |
| `maxInstructionsPerRun` | 100,000 | 1,000,000 | Chunk pauses; it can be resumed |
| `maxInstructionsTotal` | 100,000 | 1,000,000 | `COL6002`, halted |
| `maxOperandStackDepth` | 4,096 | 65,536 | `COL6010`, halted |
| `maxCallFrameDepth` | 256 | 4,096 | `COL6024`, halted |
| `maxScopeDepth` | 1,024 | 16,384 | `COL6050`, halted |
| `maxObjectAllocations` | 10,000 | 1,000,000 | `COL6019`, halted |
| `maxTraceEntries` | 5,000 | 50,000 | Old states dropped; `traceOffset` advances |
| `maxTraceBytes` | 16 MiB | 64 MiB | Old states dropped; `traceOffset` advances |
| `maxStringBytes` | 1 MiB | 16 MiB | `COL6051`, halted |
| `maxOutputBytes` | 4 MiB | 64 MiB | `COL6052`, halted |
| `maxInputBytes` | 1 MiB | 16 MiB | `COL6053`, halted |
| `maxInputLines` | 10,000 | 100,000 | `COL6054`, halted |
| `maxBreakpoints` | 1,024 | 10,000 | `COL6055`, halted |

Trace byte accounting covers the retained snapshot payload (values, frames,
locals, output, notes, and their strings) with deterministic saturating
accounting; it is not a claim about the Go allocator's private overhead.
Debugger watches and logpoints have separate debugger-session bounds rather
than `VMPolicy` fields: at most 64 watches are accepted, log messages are at
most 1,024 bytes with 32 interpolations, and the session retains at most 256
log entries or 64 KiB. Breakpoints remain a `VMPolicy` field because they are
an input to VM execution. The full debugger limits and behavior are in the
[debugger protocol](debugger-protocol.md#conditions-hit-counts-logpoints-and-watches).

The CLI accepts partial overrides from `--vm-policy FILE`; the Go API uses
`CompilationOptions.VMPolicy`/`ProjectOptions.VMPolicy`; `/api/compile` and
`/api/vm/start` accept `vmPolicy`. The 1 MiB HTTP request-body limit remains an
independent transport boundary. Source is still bounded by the lexer/parser and
host input boundary, while retained runtime memory is proportional to the
configured stack, frames/scopes, live object graph, input, output, strings, and
trace payload bounds. Canonical defaults and ceilings live in
[`internal/compiler/vm_policy.go`](../internal/compiler/vm_policy.go).

Run chunks are additionally cancellable through `RunWithBreakpointsContext`.
Cancellation produces terminal `COL6056`; reset is required before another run.

## Runtime errors

Runtime diagnostics use stage `runtime` and the `COL6xxx` range. Expected user
errors are `COL6015` (division by zero), `COL6020` (dispatch on void), `COL6030`
and `COL6031` (case on void/no match), `COL6040` (`abort`), and `COL6046`
(substring bounds), plus the documented safety-limit codes. Dispatch/case
subject failures point at the evaluated receiver/scrutinee span.

Opcode, stack, jump, target, frame, lookup, arity, and invalid-builtin failures
are internal-invariant categories: well-typed generated programs cannot produce
them. Every failure records at most one diagnostic, clears pause, halts without
panicking the host, retains valid frame/stack bases and trace state, and remains
JSON-exportable for inspection.

Potentially failing stack, call, allocation, scope, output, and input operations
preflight their bounds before mutating observable VM state. Reset constructs a
fresh VM from the verified executable and policy, rewinds input, object IDs,
instruction accounting, output, trace, breakpoints, frames, stack, and
diagnostics. Repeated reset/run and breakpoint sequences are deterministic.
The local HTTP session boundary converts an unexpected host panic into terminal
`COL6099`, unlocks the session, and returns a controlled response; a final outer
HTTP recovery boundary prevents a process crash if failure occurs outside a
session.

```cool
class Main { main() : Int { 1 / 0 }; };
```

Run it with `go run ./cmd/coolerc run broken.cl`; the command exits with the
documented compilation/runtime error status and reports `COL6015`. Codes and
stability rules are in [diagnostics.md](diagnostics.md).

## Snapshots and trace

A snapshot exposes output fragments, operand stack, frames/locals, halted and
paused flags, executed-step count, diagnostics, and trace states. Each trace
state includes the absolute step, next local IP, method, opcode/note, source
span, stack, frames, and output. `traceFrom` requests a suffix by absolute trace
offset; it cannot recover entries already dropped by the retention limit.

The public JSON types are in
[`internal/compiler/model.go`](../internal/compiler/model.go), execution is in
[`internal/compiler/vm_execute.go`](../internal/compiler/vm_execute.go), and
behavioral tests start in
[`internal/compiler/compiler_test.go`](../internal/compiler/compiler_test.go).
