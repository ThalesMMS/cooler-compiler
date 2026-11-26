# Local differential conformance

The differential harness runs Cooler's original conformance sources through a
user-supplied Stanford-compatible `coolc` and SPIM-style runtime, then compares
compiler acceptance and normalized runtime behavior. It never downloads,
installs, bundles, uploads, or redistributes a reference toolchain or course
material. The ordinary conformance corpus remains fully local and independent
of these optional tools.

## Tool discovery

The harness searches `PATH` for `coolc` and `spim`. Explicit paths take
precedence:

```bash
COOLER_REFERENCE_COOLC=/opt/cool/bin/coolc \
COOLER_REFERENCE_RUNNER=/opt/cool/bin/spim \
make differential
```

The equivalent CLI flags are `-coolc` and `-runner`. Repeated `-coolc-arg` and
`-runner-arg` flags add arguments without constructing a shell command. The
default runner invocation is `spim -file program.s`; supplying any
`-runner-arg` replaces `-file`. Repeated `-version-arg` flags replace the
default `-version` probe.

Every report records the host OS, architecture, Go version, resolved tool path,
file size, modification time, SHA-256, reported version when available, and
exact argument arrays. SHA-256 identifies a binary even when an old tool does
not implement a version flag.

If `coolc` is absent, a normal run writes a machine report and exits with a
clear local skip. Strict mode treats a missing compiler or runner as an error:

```bash
make differential STRICT=1
go run ./cmd/cooler-differential -strict
```

No account, virtual machine, Windows host, network service, or temporary user is
part of this contract.

## Selecting comparisons

Run the complete corpus or filter it using the same stable IDs as the normative
matrix:

```bash
make differential
make differential RULE=SEM-EXPR-ARITHMETIC
make differential FIXTURE=runtime-int32-boundaries-valid
```

Each source file is copied into a new permission-restricted temporary directory
and passed to `coolc` as a separate argument in fixture order. This preserves
multifile behavior. The fixture's program-input lines are sent unchanged to the
runtime process. A successful reference compilation must produce exactly one
`.s` artifact before runtime comparison begins.

Cooler always performs a complete compilation for this comparison, even for a
fixture whose normative test intentionally stops after an earlier phase. The
harness compares:

- compiler acceptance, first recognizable failing stage, broad diagnostic
  category, and reference artifact creation;
- runtime stdout, semantic stderr category, exit category, recognizable runtime
  error, and program-input behavior;
- multifile source order.

This runtime comparison does not compare diagnostic wording (the
[phase comparisons](#phase-comparisons-pa2pa4) do), MIPS instruction
sequences, object layout, or Cooler's educational bytecode.

## Containment and normalization

Each compiler and runtime process has an independent timeout (five seconds by
default) and separate one-MiB stdout/stderr capture limits. Use `-timeout` and
`-output-limit` to tighten them. Timeout, crash, launch failure, excessive
output, a successful compiler that emits no artifact, and multiple artifacts
are isolated reference errors; one case cannot terminate the remaining run. An
exact fixture/rule allowlist entry may classify an infrastructure outcome such
as the reference runtime timing out on a deliberately nonterminating
safety-limit fixture as an expected divergence. The same outcome without that
exact entry remains a reference error.

Normalization is intentionally narrow:

- CRLF and bare CR become LF;
- the isolated temporary path becomes `<TMP>`;
- known leading SPIM/QtSpim launcher lines and the classic SPIM success footer
  are removed;
- diagnostic-only whitespace is made stable.

Program output, leading or trailing whitespace, acceptance, evaluation order,
integer values, runtime errors, and input consumption are never normalized
away. Use `-keep-temporary` only for local diagnosis; retained paths are written
to the report and are never committed automatically.

## Reports, evidence, and divergences

The versioned JSON report is written to `dist/differential-report.json`.
Unexpected mismatches and reference infrastructure errors also create a compact
bundle under `dist/differential-evidence/<fixture>/` containing the original
ordered source files, input, normalized command observations, tool fingerprints,
host metadata, and the comparison result.

Outcomes are `matched`, `expected-divergence`, `unexpected-mismatch`,
`unexpected-pass`, or `reference-error`. Mismatches, reference errors, and
unexpected passes exit nonzero. An unexpected pass means an allowlist entry is
stale and must be removed.

The versioned allowlist is
[`differential-allowlist.json`](../internal/compiler/testdata/conformance/differential-allowlist.json).
Every entry identifies exactly one fixture and reciprocal rule ID and includes
a reason, normative/reference evidence, expected Cooler and reference outcomes,
and a removal condition. Guessed or tool-wide exceptions are not accepted. The
harness rejects unknown, duplicate, mismatched, or stale entries; an unexpected
pass is therefore a gate failure rather than silent allowlist growth.

## Phase comparisons (PA2–PA4)

`make differential` also compares Cooler's opt-in reference formats with a
user-supplied course `lexer`, `parser`, and `semant` (see
[`course-assignments.md`](course-assignments.md)):

- **tokens**: `lexer files...` against `coolerc emit tokens --format=reference`;
- **parser**: `lexer files... | parser` against
  `coolerc emit ast --format=reference` (stdout on success, the error report on
  stderr after a syntax error);
- **semant**: `lexer | parser | semant` against
  `coolerc emit ast --format=reference --stage=semant` and the reference
  diagnostics (typed AST, or error text and order).

The corpus is every selected conformance fixture (multifile fixtures keep their
source order) plus the PA-style battery in `internal/course/testdata/phases`.
`-rule`/`-fixture` restrict the conformance part and skip the battery. Each
case runs in a new private directory where the sources are copied under their
base names, so both sides print the same `#name` and `_class` file names; the
only normalization removes that directory prefix if a tool prints it. Message
text and token values are never normalized.

Tool discovery: `-lexer`, `-parser`, `-semant` (or `COOLER_REFERENCE_LEXER`,
`COOLER_REFERENCE_PARSER`, `COOLER_REFERENCE_SEMANT`), then `PATH`, then the
classic layouts under the Cool root (`-cool-root` or `COOLER_COOL_ROOT`,
default `/var/tmp/cool`): `bin/`, `lib/.x86_64/`, `lib/.i686/`. Without a
`lexer` the phase run is a documented skip; with `STRICT=1` a missing lexer,
parser, or semant exits `2`. The phase report
(`dist/differential-phases-report.json`, `-phase-report`) records each tool's
path, resolved path, size, modification time, and SHA-256, plus every
comparison's outcome and first differing line. Use `-phases=false` to run only
the runtime comparison.

An evidenced divergence can be allowlisted with `-phase-allowlist FILE`:

```json
{
  "schemaVersion": "1.0.0",
  "entries": [
    {
      "id": "lexer-unterminated-line",
      "case": "battery/lex_errors.cl",
      "phase": "tokens",
      "reason": "why the outputs differ",
      "evidence": "tool SHA-256 and the differing lines",
      "removalCondition": "when this entry can be deleted"
    }
  ]
}
```

The same live comparisons also run as Go tests (`go test ./internal/course`),
which skip without the tools unless `STRICT=1` or `COOLER_REFERENCE_STRICT=1`.
Course binaries and course-tree fixtures are never downloaded or committed.

## MIPS end-to-end comparison (PA5)

`make differential` finally runs Cooler-generated MIPS on a user-supplied real
SPIM with the Cool runtime's `trap.handler`, for CI/dev only:

- for every program in the PA5 corpus (`internal/course/testdata/pa5`, or
  `-mips-corpus DIR`) plus the Cool root's `examples/` directory when it
  exists (so the course's own `hello_world.cl`, `arith.cl`, `list.cl`,
  `complex.cl`, ... are compared without being committed), and for each
  collector flag set in `-mips-gc` (default: none, `-g`, `-g -t`);
- Cooler's `.s` runs on SPIM; with a reference `coolc`, the reference `.s`
  for the same flags runs on the same SPIM, and normalized stdout and the
  runtime-error category (`ok`, `dispatch-void`, `case-void`,
  `case-no-match`, `abort`, `substr`, `error`) must match;
- the embedded simulator's output must equal Cooler's output on real SPIM.

SPIM's banner lines (`SPIM Version`, copyright, `Loaded:`) are the only
normalization. Discovery: `-runner`/`COOLER_REFERENCE_RUNNER` (or `PATH`, or
the Cool root) for `spim`, `-trap-handler`/`COOLER_TRAP_HANDLER` (or
`<cool-root>/lib/trap.handler`) for the runtime, and `-coolc` for the
reference compiler. Without `spim` the comparison is a documented skip; with
`STRICT=1`, a missing `spim` or `coolc` exits `2`. The report is
`dist/differential-mips-report.json` (`-mips-report`); `-mips=false` disables
the comparison. Real SPIM is never used by the app: the GUI and
`coolerc run --target=mips` use the embedded simulator.
