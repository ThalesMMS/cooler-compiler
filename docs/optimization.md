# Educational IR optimization

Cooler keeps the verified unoptimized stack IR as its default and reference
execution path. Optimization is opt-in with `--opt=basic` in the CLI or
**Opt: basic** in the GUI. The mode is intentionally small, deterministic, and
educational; it is not an `-O` compatibility promise with another COOL
compiler.

## Pipeline and verification barriers

The backend order is:

```text
typed AST -> unoptimized IR -> verifier -> optional passes -> verifier after
each pass -> final verifier -> pseudo-assembly / VM
```

An optimization pass receives a cloned verified executable. The unoptimized
executable remains available in the report and is used when optimization is
disabled. A verifier failure after any pass blocks VM preparation instead of
executing suspect bytecode. The schema 1.4 `optimization` object records both
IR and CFG versions, the pass order, verifier outcomes, transformations,
refusals, and the exact contract declared by each pass.

Every optimized instruction retains one or more `originalIndexes` plus its
source span. A folded instruction therefore maps back to all unoptimized
instructions it replaced. This supports source selection and explanations; it
does not make optimized instruction stepping identical to baseline stepping.
Use `none` when teaching or debugging the original instruction sequence.

## Basic pass set

The fixed pass order is:

1. `constant-fold` evaluates adjacent, typed literal integer/Boolean operations.
   Division by zero stays at runtime with its original diagnostic span. String
   equality is deliberately left alone because evaluating it at compile time
   could bypass runtime string-size policy.
2. `algebraic-identities` removes typed `x+0`, `x-0`, `x*1`, double integer
   negation and double Boolean negation. Operand evaluation stays in place,
   including calls, input, allocations and failures. Any incoming edge into
   the identity pair prevents rewriting; `x*0` is explicitly refused.
3. `constant-conditions` selects a branch only when the condition is a literal
   Boolean and the candidate instructions have no external incoming edge.
4. `cfg-cleanup` threads unconditional jumps, removes jumps to the immediately
   following instruction, and removes unreachable instructions.
5. `dead-computations` removes self-contained, discarded local computations
   proved removable by a bounded stack/effect analysis. It retains operations
   that may write, fail, allocate, diverge, or alter control flow.

The passes may change instruction count and control flow. They must preserve
left-to-right evaluation, output, input consumption, runtime-error category and
source attribution, allocation count/order, copy and identity behavior,
dispatch, and termination under an equivalent semantic budget. They do not
move operations across calls, I/O, allocation, copy, or possible runtime
errors. Refused opportunities are report evidence, not silent failures.

## CLI and GUI inspection

```bash
coolerc emit ir --opt=basic --emit-ir=both program.cl
coolerc emit ir --opt=basic --emit-ir=both --explain-optimizations program.cl
coolerc run --opt=basic program.cl
```

`--emit-ir=before|after|both` and `--explain-optimizations` apply only to
`emit ir`. `after` means the selected execution IR: it equals the baseline for
`--opt=none` and the verified optimized IR for `--opt=basic`.

The GUI Optimize panel shows before/after IR and CFG tables, source/original-IR
mappings, pass contracts, changes, verifier status, and bounded refusal
evidence. Changing the optimization selector invalidates the current compiler
and debugger session, so the next compile cannot accidentally reuse bytecode
from the previous mode.

## Equivalence and limits

Optimized and baseline programs preserve output, input consumption,
halt/wait/error state, diagnostic identity and span, object-copy behavior, and
heap projections when run under equivalent semantic budgets. Instruction
counts may differ, so a deliberately tight instruction budget can pause one
variant before the other. That reflects a resource limit rather than a
language-semantic difference.

The algebraic pass uses Cooler's signed 32-bit arithmetic, including wrapping
negation at the minimum integer. It merges all removed origins into the
retained instruction and preserves that operand instruction's source span,
so failures in operand evaluation remain attributed to the same source.
Acceptance and refusal evidence appears alongside the other passes. Changes
reduce executed instructions and can reduce transient stack usage; use the
baseline for experiments with exact instruction or operand-stack budgets.
The bytecode verifier establishes structural/type invariants after the pass;
it is not a formal proof of semantic equivalence.

## Dead computations and effects

The local effect table admits Int/Bool/void/self pushes, typed non-dividing
arithmetic/comparisons, negation, `isvoid`, duplication and discard. A candidate
must consume only values it produced, end with `POP` and leave no value, have
no interior incoming edge, and fit within 128 instructions. All admitted
operations terminate under the equivalent semantic budget used for optimization.

String pushes and variable reads remain conservative because they can carry
failure/resource checks. Division is never removed by this pass; a proven
nonzero literal division may already have been folded by `constant-fold`.
Calls, I/O, allocation/constructors, writes, scope changes and potentially
divergent control flow are retained. This is not dead-store elimination.
The report records removals and refusals. Removed origins transfer to the next
retained instruction while its own source span stays unchanged; the verifier
checks the resulting stack and CFG. Exact instruction and transient stack
budgets, trace lengths, and debugger instruction stops may differ, as with the
other opt-in passes.

