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
2. `constant-conditions` selects a branch only when the condition is a literal
   Boolean and the candidate instructions have no external incoming edge.
3. `cfg-cleanup` threads unconditional jumps, removes jumps to the immediately
   following instruction, and removes unreachable instructions.

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

Local tests compare baseline and optimized execution for output, input events,
halt/wait/error state, runtime diagnostic identity and span, object-copy
behavior, and heap projections. Generated and fuzzed constant programs extend
that corpus. Verifier checks run before optimization, after every pass, and at
the normal final backend barrier.

Raw executed-instruction counts are expected to differ. Equivalence campaigns
therefore use a policy high enough for both variants and compare semantic
outcomes rather than requiring the same step count. A deliberately tight
instruction budget can pause one variant before the other; that is a resource
limit observation, not a language-semantic difference.
