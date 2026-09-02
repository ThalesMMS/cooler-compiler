# Artifact and schema contract

Cooler v0.1.1 introduces version 1 of its complete-report, report-archive, and
release-provenance contracts. The machine-readable definitions live in
[`schemas/`](../schemas/), including the standalone diagnostics subdocument.

## Version policy

Schema versions use `MAJOR.MINOR.PATCH`:

- additive optional fields and relaxed validation increment `MINOR`;
- documentation corrections that do not change accepted data increment `PATCH`;
- removing a field, making an optional field required, changing a field's type
  or meaning, or changing archive path semantics increments `MAJOR`;
- one previous major is retained for at least one maintenance release, either
  through direct reading or a clear unsupported-version error;
- deprecated fields are documented for one supported major before removal.

The Go decoder accepts compatible `1.x.y` reports and upgrades the unversioned
v0 report shape emitted before v0.1.1. Other major versions return
`ErrUnsupportedReportSchema` rather than being interpreted heuristically.

## Complete reports

CLI `coolerc emit report`/legacy `-json`, the `/api/compile` result, incremental
pipeline results, full debugger snapshots, individual GUI report downloads, and the
report inside a ZIP share the same top-level identity fields:

- `schemaVersion`, `compilerVersion`, and optional `compilerCommit`;
- compiler host `target` (`os`, `arch`, and Go runtime);
- deterministic `options`, including the VM instruction chunk, complete
  normalized VM resource policy, and whether program input was supplied;
- ordered `sourceUnits` with stable ID, name, order, byte size, and SHA-256.

The compiler lexes and parses every source unit independently, then combines
complete class declarations for semantic analysis. The top-level `source` is a
single-source compatibility field and is empty for multifile projects. Exact
project semantics are documented in [`multifile.md`](multifile.md).

Arrays are emitted in compiler/source order. Map-shaped internal state is not
used for contractually ordered output. Generated JSON ends with LF.

Interactive runtime snapshots may include optional `waitingInput` metadata and
an `inputEvents` count. Complete reports never contain a submitted-line event
log (program output may naturally echo a line); the debugger's separate event
log API requires an explicit content opt-in.

Schema `1.2.0` requires `options.vmPolicy` with every validated limit. The Go
decoder continues accepting `1.0.x`/`1.1.x` reports and supplies the current
default policy when that field predates the contract. Invalid or partial policy
objects in a `1.2.x` report are rejected rather than silently normalized.

Schema `1.3.0` adds the optional `visualizations` bundle after semantic
analysis. The bundle is self-identifying (`schemaVersion`, compiler version,
and ordered source-unit IDs) and contains deterministic inheritance edges,
logical object/dispatch tables, and many-to-many source provenance. Its
published limits and `truncated` flag are part of the contract. Logical slots
are an educational Cooler model and must not be interpreted as a native or
reference-compiler ABI.

Schema `1.4.0` requires `options.optimization` (`none` or `basic`) and the
top-level `optimization` evidence object. It carries verified before/after IR
and CFG artifacts, execution selection, stable pass order and contracts,
bounded changes/refusals, and many-to-one `originalIndexes` provenance. The Go
decoder upgrades earlier v1 reports to the `none` default. Optimized instruction
counts and stepping can differ while source spans and original-IR identities
remain traceable; see the [optimization contract](optimization.md).

Every source-linked span includes its `sourceId`. Token and diagnostic
lines/columns are 1-based and count decoded Unicode runes;
ends are exclusive. Offsets are 0-based UTF-16 code units, not UTF-8 byte
positions. LF, CRLF, and bare CR each form one logical line. The full lexical
coordinate and malformed-UTF-8 behavior is documented in
[`cool-language.md`](cool-language.md#lexical-details).

## Report ZIPs

Every GUI ZIP contains `manifest.json`. It identifies the compiler and source
units and records the path, byte size, and SHA-256 for every other entry. The
manifest deliberately does not hash itself. Duplicate names, absolute paths,
backslashes, empty segments, and `.`/`..` traversal segments are rejected.

Each source unit is stored separately under an ordered safe path recorded on
its manifest entry. Archive entry order is fixed. ZIP timestamps use the DOS epoch by default,
generated text uses LF, and original source/program output bytes are preserved.
No current clock value is added unless a caller explicitly requests an
`exportedAt` field for a standalone report.

## Release provenance

Run from a clean reviewed commit:

```bash
make release-artifacts VERSION=1.0.0
```

The local builder creates deterministic Linux amd64/arm64 tarballs, Windows
amd64 and macOS arm64 ZIPs, `release-manifest.json`, and `SHA256SUMS`. The
manifest records the source commit, version, `SOURCE_DATE_EPOCH`, Go/host tools,
build flags, target tuples, package modes, sizes, digests, build status, skipped
checks, and the target-runtime evidence document. A successful cross-build is
recorded only as `built`.

The output directory must be empty and the working tree must be clean. The
release gate separately performs two complete local builds and compares every
package and metadata file byte for byte.
