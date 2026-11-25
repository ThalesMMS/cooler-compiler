# Known-target inlining experiment (#84)

## Decision

Do not add inlining to `none` or `basic`. The test-only prototype preserves
ordinary outcomes for its selected subset, but bypasses observable call-frame
limits and removes frames/source-stepping boundaries. Its explicit temporary
bindings also increased IR size and executed instructions in the small workloads
below. An execution-only option would need a separate debugger and resource
contract before adoption; the existing bytecode verifier is not an equivalence
proof.

## Reproduce locally

```bash
CGO_ENABLED=0 go test ./internal/compiler -run '^TestInliningPrototype' -count=1 -v
```

The implementation is entirely in `internal/compiler/inlining_prototype_test.go`,
so it is absent from distributed binaries. Tests are bounded to 100–500 VM
instructions and finish in seconds. No reference toolchain or service is used.

## Eligibility and substitution

The experiment accepts methods returning Int or Bool with at most 16 bytecode
instructions. Calls must have a proven non-void receiver: an immediately
preceding `new ConcreteClass`, or a static dispatch on `self` in a nonsynthetic
method. Incoming edges to the call reject that proof. Concrete allocations
permit dynamic method resolution against the actual class, including overrides;
static dispatch resolves against its explicit type. Potentially void receivers,
unknown dynamic destinations, built-ins and initializers are refused.

Eligible bodies have a straight-line return and may use literal operations,
scalar arithmetic, parameters and local `let` scopes. Attribute accesses,
writes, calls, allocations and control flow inside the body are refused. Caller
arguments already execute left-to-right before the receiver. The replacement
binds these existing values without reevaluation, renames formal/local bindings
with fresh `$inline` IR names that COOL identifiers cannot capture, and rewrites
`self` through the retained receiver. Local `SELF_TYPE` binding categories are
translated to the receiver context rather than the caller's unrelated class.
Original callee operation spans are retained for failures. Expansion stops at
256 added instructions across the executable; the resulting bytecode is
verified before experimental execution.

## Measurements and observations

The following deterministic measurements precede any subsequent optimizer pass:

| Workload | IR before → after | Executed instructions before → after | Result |
| --- | --- | --- | --- |
| Static dispatch through an override | 54 → 60 | 37 → 39 | 4 |
| Known concrete dynamic override | 54 → 60 | 37 → 39 | 5 |
| Unknown dynamic receiver | 58 → 58 | 41 → 41 | Refused; output 5 |
| Potentially void receiver | 47 → 47 | 15 → 15 | Refused; original void failure |
| Argument and constructor effects | 58 → 65 | 50 → 53 | ABR3, in that order |
| Nested local/formal shadowing | 57 → 67 | 44 → 46 | 499 |
| Division failure inside body | 43 → 49 | 22 → 24 | Same diagnostic and span |

Tests compare output, runtime diagnostics/spans, and object allocation counts.
They also validate a local `SELF_TYPE` binding in a different caller class.
Refusals are counted by reason in verbose output. These are measurements of
instruction/code growth, not a claim of wall-clock performance improvement.

The frame-limit test reaches `Main.main`, then sets the permitted frame depth
to its current depth. The baseline call to `self@Main.f(3)` fails with the
call-stack limit; the inlined variant prints 4. Even with ordinary budgets,
debugger frames, step-into/step-out stops, reverse history, watch scopes and
call-depth observations differ. The scope-limit test additionally proves that
a caller at depth two can call the original method successfully, while the
synthetic inline scope fails with the lexical-scope limit.
Those incompatibilities block production adoption despite
the ordinary-output equivalence tests.

## Validation

The short full Go suite, vet, pinned lint and documentation checks passed on
Windows. Focused prototype tests also executed in Debian/WSL from a Linux test
binary. Independent review identified the scope-budget incompatibility now
covered by a regression test. Native macOS validation was unavailable.
