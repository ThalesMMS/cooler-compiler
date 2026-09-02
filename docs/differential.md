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

It does not compare diagnostic wording, MIPS instruction sequences, object
layout, or Cooler's educational bytecode.

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
a reason, normative/reference evidence, issue link, expected Cooler and
reference outcomes, and a removal condition. Guessed or tool-wide exceptions
are not accepted. The harness rejects unknown, duplicate, mismatched, or stale
entries; an unexpected pass is therefore a gate failure rather than silent
allowlist growth.

## Local validation record

On 2026-09-01 the complete strict corpus ran on Windows amd64 with Go 1.26.4,
using WSL2 Debian GNU/Linux 13 as the execution environment for the classic
32-bit Linux tools. The locally installed StanfordOnline student-distribution
compiler does not expose a version flag; its underlying `coolc` executable had
SHA-256 `c03fe0a2ded81aeb902ccdced0e640e87e4eed4dd1eddcbd8c80f7d1667bb71a`.
The course runtime reported `SPIM Version 6.5 of January 4, 2003` and its
underlying executable had SHA-256
`7916869176b14fb975719e67dc69ed0010871243af734046540eff30787ec252`.
Both were invoked through path-translating Windows launchers with SHA-256
`0fb25796fb919ac3c49a42e27d76a190f751f700149cae6791ddbf245901d0aa`;
the machine report fingerprints those resolved launchers and exact arguments.

`STRICT=1 make differential` selected all 90 fixtures: 81 matched, 9 were exact
evidence-backed expected divergences, 0 were unexpected mismatches, 0 were
unexpected passes, and 0 were reference errors. The comparison exposed and
fixed one real Cooler defect: explicit dispatch evaluated the receiver before
its arguments instead of the manual/reference argument-first order. The nine
retained boundaries are the documented Unicode/host-input policies, modular
integer overflow, and Cooler-only debugger or deterministic execution-budget
protocols. The generated machine report remains uncommitted because it contains
host-specific paths; this record and the issue-closing evidence retain the
portable fingerprints and totals.
