# Normative COOL conformance

This document defines how Cooler makes and verifies language-conformance claims.
The executable source of truth is
[`internal/compiler/testdata/conformance/matrix.json`](../internal/compiler/testdata/conformance/matrix.json).
The checked-in [generated conformance summary](conformance-summary.md) renders
the manual checklist, rule statuses, evidence kinds, and divergence details;
`make docs-check` rejects it when it is stale.
The matrix is intentionally local and deterministic: it does not download a
reference toolchain, contact a service, or run in hosted CI.

## Normative hierarchy

When two sources disagree or leave behavior ambiguous, use this precedence:

1. The canonical [COOL Reference Manual](https://web.stanford.edu/class/cs143/materials/cool-manual.pdf)
   defines the language syntax and semantics.
2. The documented [Stanford CS143 reference tools](https://web.stanford.edu/class/cs143/)
   guide behavior that the manual leaves ambiguous. Comparisons must record the
   tool and environment; diagnostic wording is not normative unless a rule says
   otherwise.
3. [`docs/cool-language.md`](cool-language.md) records an explicit Cooler
   decision when reference behavior is unavailable, nondeterministic, or outside
   this educational implementation's scope.

A lower-ranked source cannot silently override a higher-ranked source. A
difference must be represented as `intentional-divergence`, with the Cooler
behavior and reference comparison written into the rule.

## Matrix contract

Rule IDs are stable and begin with one of `LEX`, `PAR`, `SEM-CLASS`, `SEM-EXPR`,
`RUN`, `BUILTIN`, or `TOOL`. Every rule records:

- a name, normative source and section, compiler stage, and observable behavior;
- one of the statuses below;
- positive and negative fixtures, expected diagnostics or output where relevant;
- a reference comparison and a clear boundary for every incomplete claim.

The matrix also contains the complete ordered section index of the selected
COOL Reference Manual. Every section maps to rule IDs or an explicit
`not-applicable` rationale. The loader rejects omissions, reordered or duplicate
sections, unknown mappings, and manual-cited rules absent from the checklist.

The allowed statuses have precise meanings:

- `supported`: the stated behavior is implemented and every linked fixture must
  pass;
- `partial`: a tested subset works, with the remaining gap documented;
- `intentional-divergence`: Cooler deliberately differs and documents the result;
- `unsupported`: the behavior is outside the current implementation and its
  boundary is documented;
- `not-yet-tested`: no sufficient fixture exists yet and the gap is identified.

The matrix also maps each user-visible README compatibility claim to rule IDs.
The loader checks that the exact claim still exists and that every rule is
claimed. This makes README drift a failing validation error instead of a manual
audit note.

## Corpus and fixture metadata

Fixtures live by concern below
[`internal/compiler/testdata/conformance/`](../internal/compiler/testdata/conformance/):
`lexer/`, `parser/`, `semantics/classes/`, `semantics/expressions/`, `runtime/`,
`builtins/`, `projects/`, and `debugger/`. Each leaf has a `fixture.json` and one or more
ordered `.cl` sources.

Fixture metadata uses schema version `1.0.0` and declares:

- a stable fixture ID and reciprocal matrix rule IDs;
- `valid` or `invalid`, plus the last successfully completed compiler stage;
- exact diagnostic code/stage/severity tuples, exact program output, and required
  compiler artifacts when applicable;
- optional exact diagnostic source-unit identity, UTF-16 span, and ordered
  labeled related spans;
- an optional `runThrough` phase and exact token expectations for isolated lexer
  or frontend rules;
- selected AST paths with exact node kind and optional text, name, inferred type,
  value, and span expectations;
- optional runtime status (`halted` or `paused`) and ordered input lines;
- optional partial `vmPolicy`, ordered `runSteps`, and breakpoint lines for
  small deterministic resource-boundary programs;
- optional minimum absolute trace offset and maximum retained trace count for
  trace-eviction evidence;
- an optional bounded `debuggerScenario` action sequence. Debugger actions cover
  incremental preparation, execution controls, observations, heap queries,
  reverse execution, interactive input, multifile mapping, and public-session
  lifecycle transitions. Expectations compare stable stop, location, frame,
  output, diagnostic, trace, timeline, watch, breakpoint, and heap fields;
- ordered source paths, optionally asserted result-contract source-unit identities,
  and a provenance statement.

Unknown JSON fields, duplicate rule or fixture IDs, missing reciprocal links,
orphan fixtures, unsafe source paths, missing artifacts, stale document claims,
unsupported status values, invalid VM policies, unsafe run chunks, and invalid
breakpoint lines fail before any program runs. Execution then compares
diagnostics in emission order, rich locations where declared, selected AST
shape/types, and output byte-for-byte. Supported lexer rules must have token
evidence, parser rules AST evidence, semantic rules typed-AST or diagnostic
evidence, and runtime/basic-class rules output/state or diagnostic evidence.
Completed rules may not retain unresolved tracking metadata. Intentional divergences must record
their exact boundary, reason, user consequence, and scope assertion.

Every debugger scenario declares maximum actions and instructions and runs
under a normalized VM policy with bounded allocation, stack, strings, trace,
input, and output. A debugger failure adds a compact `replay` object to the
machine report: the one-based failing action, its exact action prefix, and the
stable actual snapshot fields needed to reproduce the checked-in fixture.

## Running and interpreting the report

Run the complete corpus locally:

```bash
make conformance
```

The command prints one line per fixture and a summary of passed, failed, partial,
divergent, and untested rules. It writes the deterministic machine report to
`dist/conformance-report.json` and regenerates `docs/conformance-summary.md`.
Override the report destination without changing the
repository:

```bash
make CONFORMANCE_REPORT=/tmp/cooler-conformance.json conformance
```

Any fixture failure or invalid metadata exits nonzero. `partial`, `divergent`,
and `untested` are honest declared outcomes, not execution failures. The local
`make release-check` gate runs the same corpus into a temporary directory. This
project intentionally has no CI, so contributors and release maintainers run the
gate on their target hosts.

Optional comparisons against user-supplied reference tools use
`make differential`, with `RULE=...` and `FIXTURE=...` filters and `STRICT=1`
when absence must fail. Tool discovery, containment, normalization, mismatch
evidence, and the versioned divergence allowlist are specified in the
[differential conformance guide](differential.md). The harness never downloads
or redistributes reference binaries and skips clearly when they are absent.

## Licensing and provenance

All committed conformance programs are original Cooler project fixtures and are
licensed under `AGPL-3.0-or-later` with the repository. Fixture metadata must say
so explicitly. Do not copy programs, expected output, or diagnostics from a
course distribution or third-party test suite merely because it is publicly
reachable. External material may be cited as a normative specification; adding
third-party corpus content requires a compatible license, preserved notices, and
an explicit provenance review before commit.
