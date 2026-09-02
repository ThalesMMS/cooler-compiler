# Stack IR reference

This document is the canonical reference for Cooler's educational stack IR.
It describes the current implementation, not a portable bytecode or a promise
of binary compatibility. The emitted JSON shape is stable only where covered by
the [artifact contract](artifact-contract.md); opcode changes are recorded in
the [changelog](../CHANGELOG.md).

## Execution model

Each compiled method owns a zero-based instruction array. A frame has an
instruction pointer, `self`, nested local scopes, and an operand-stack base.
Instruction arguments use method-local label syntax (`L12`) in text output,
while the report's instruction `index` is global across methods. Every
instruction retains a source [span](artifact-contract.md#complete-reports) and
an `originalIndexes` array. Baseline instructions point to their own global
index; an optimized instruction lists every baseline instruction it replaces.
Compiler-generated `$entry`, `$init`, and `HALT` instructions use the empty,
unlinked span (`sourceId` empty with zero coordinates); Cooler never invents a
user source location for synthetic instructions.
The optional transformation rules and before/after artifacts are documented in
the [optimization contract](optimization.md).

Stack effects below use `a b` for operands with `b` on top, `recv` for a method
receiver, `arg…` in source order, and `→` for the resulting stack suffix.

## Instruction set

| Opcode | Operands | Stack effect | Meaning |
| --- | --- | --- | --- |
| `PUSH_INT` | signed 32-bit integer | `→ value` | Push an `Int` literal without host-width conversion. |
| `PUSH_STRING` | quoted text | `→ value` | Push a `String` literal. |
| `PUSH_BOOL` | Boolean | `→ value` | Push a `Bool` literal. |
| `PUSH_VOID` | none | `→ void` | Push the uninitialized object value. |
| `LOAD_SELF` | none | `→ self` | Push the frame receiver. |
| `LOAD` | name | `→ value` | Resolve the innermost local, then an attribute of `self`. |
| `STORE` | name | `value →` | Assign an existing local or attribute. Assignment lowering uses `DUP` first. |
| `DUP` | none | `value → value value` | Duplicate the top operand. |
| `POP` | none | `value →` | Discard the top operand. |
| `ENTER_SCOPE` | none | unchanged | Push an empty lexical-local scope. |
| `BIND` | name, declared type | `value →` | Bind a typed name in the innermost scope. |
| `EXIT_SCOPE_KEEP` | none | `value → value` | Remove one scope while preserving the expression result. |
| `ADD` | none | `Int Int → Int` | Signed 32-bit addition modulo 2^32. |
| `SUB` | none | `Int Int → Int` | Signed 32-bit subtraction modulo 2^32. |
| `MUL` | none | `Int Int → Int` | Signed 32-bit multiplication modulo 2^32. |
| `DIV` | none | `Int Int → Int` | Truncating signed division; zero is an error and `MIN/-1` yields `MIN`. |
| `NEG` | none | `Int → Int` | COOL `~` integer complement modulo 2^32. |
| `NOT` | none | `Bool → Bool` | Boolean complement. |
| `LESS` | none | `Int Int → Bool` | Strict integer ordering. |
| `LESS_EQUAL` | none | `Int Int → Bool` | Non-strict integer ordering. |
| `EQUAL` | none | `a b → Bool` | Value equality for basics, identity for objects, and void equality. |
| `ISVOID` | none | `value → Bool` | Test the void representation. |
| `JUMP` | label | unchanged | Set the current method IP to the label. |
| `JUMP_IF_FALSE` | label | `Bool →` | Jump when the popped condition is false. |
| `NEW` | type | `→ value` | Allocate defaults and run the selected `$init`; `SELF_TYPE` uses `self`'s runtime class. |
| `CALL` | method, arity, optional `@Type` | `arg… recv → result` | Source arguments are evaluated left-to-right before the receiver. Dynamic dispatch is used unless a static type is present. A compiled call creates a frame; a builtin returns directly. |
| `RETURN` | none | `result → result` | Remove the current frame, restore its stack base, and return one value. |
| `CASE_SELECT` | `name:type→label` entries | `value →` | Choose the closest conforming runtime type, bind the value in a new scope, and jump. |
| `HALT` | none | unchanged | Stop the synthetic program entry method. |

The definitions live in
[`internal/compiler/codegen.go`](../internal/compiler/codegen.go), execution in
[`internal/compiler/vm_execute.go`](../internal/compiler/vm_execute.go), and
stack/runtime guards in
[`internal/compiler/vm_stack.go`](../internal/compiler/vm_stack.go).

## Calls, initialization, and dispatch

`$program.$entry` performs `NEW Main`, calls `main`, then halts. Each class has a
synthetic `$init`: it calls its parent's initializer first, evaluates explicit
attributes in declaration order, and returns `self`. Allocation installs
defaults for inherited and local attributes before that chain begins.

Dynamic `CALL` looks up the receiver's runtime class. `@Type` starts lookup at
that static type. The semantic analyzer checks source-level arity and
conformance, and the bytecode verifier independently checks backend call
metadata before runtime.

## Pre-execution verification

After IR, CFG, and pseudo-assembly materialization, Cooler runs a deterministic
worklist analysis for every compiled method. Its initial abstract frame has an
empty operand stack and one root scope containing the formals. Each opcode uses
the stack, control-flow, and scope effect in the table above. Branch successors
must merge at the same stack height and with identical scope shape; compatible
value categories are joined to their least known common type.

The verifier rejects unknown opcodes, underflow, invalid or cross-method jump
targets, fallthrough past the method, unreachable instructions, wrong operand
categories, unbalanced scopes, unresolved storage, invalid call signatures,
bad entry/initializer/return contracts, malformed spans, duplicate method keys,
and inconsistent IR/CFG/assembly mappings. `RETURN` requires exactly one result
above the frame base and the root scope; `HALT` requires exactly one entry
result. Unreachable instructions are diagnosed rather than silently retained.

Failures use the internal `COL51xx` family and include the stable method name,
instruction index, invariant, and source span. The pipeline blocks Runtime, and
the defensive VM constructor refuses an unverified full executable. VM-side
internal checks remain as defense in depth but should be unreachable for a
verified compiler result. Direct mutation tests and bounded verifier fuzz seeds
live in [`verifier_test.go`](../internal/compiler/verifier_test.go).

## Control flow

Basic-block leaders are the first instruction, every valid branch target, and
the instruction after a conditional/unconditional branch. `CASE_SELECT` adds
each case target. CFG edges are derived from branch, case, fallthrough, and
method termination behavior; see
[`controlFlowForMethod`](../internal/compiler/codegen.go).

For this executable COOL fragment:

```cool
class Main { main() : Int { if 1 < 2 then 10 else 20 fi }; };
```

`coolerc emit ir` produces literal pushes, `LESS`, `JUMP_IF_FALSE`, one branch
result, a `JUMP` over the other branch, and `RETURN`. Exact formatting is a
debugging aid and may evolve; tests in
[`internal/compiler/codegen_test.go`](../internal/compiler/codegen_test.go)
lock down semantic behavior.
