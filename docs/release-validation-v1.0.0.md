# v1.0.0 target-runtime validation record

This is the authoritative evidence ledger template for the unified v1.0.0
artifact set. It is intentionally incomplete during Final 08/11. Final 10/11
must replace every pending field with evidence for one candidate commit and one
release set; until then, no v1.0.0 package or target is described as
runtime-tested.

**Release-set verdict: PENDING.**

## Status vocabulary

- `built`: produced from the candidate, but not executed on the target;
- `runtime-tested`: checksum verified before extraction and the required
  automated target smoke passed;
- `native GUI tested`: the exact native GUI package also passed its manual
  target scenarios;
- `source-build-only`: no supported prebuilt binary is distributed;
- `unsupported`: outside the advertised v1.0.0 target set;
- `pending`: required evidence has not been collected.

## Candidate identity

- Candidate version: `1.0.0`.
- Candidate commit: pending Final 09/11.
- `SOURCE_DATE_EPOCH`: pending Final 09/11.
- Toolchain and build invocation: pending Final 09/11.
- Reproducibility verdict: pending Final 09/11.
- `release-manifest.json` SHA-256: pending Final 10/11.
- `SHA256SUMS` SHA-256: pending Final 10/11.

## Unified package matrix

| Target | Package | SHA-256 | Runtime status | Native GUI status |
| --- | --- | --- | --- | --- |
| Linux amd64 | `cooler-compiler-v1.0.0-linux-amd64.tar.gz` | pending | pending | Not applicable |
| Linux arm64 | `cooler-compiler-v1.0.0-linux-arm64.tar.gz` | pending | pending | Not applicable |
| Windows 11 amd64 | `cooler-compiler-v1.0.0-windows-amd64.zip` | pending | pending | pending decision/evidence |
| macOS arm64 | `cooler-compiler-v1.0.0-macos-arm64.zip` | pending | pending | pending |

macOS amd64/universal, Windows arm64, and every unlisted combination remain
unsupported or source-build-only unless Final 10/11 records an explicit
decision and exact-hash evidence. A package may not be promoted from `built` to
`runtime-tested` based on cross-building, a different candidate, or a rebuilt
copy on the target.

## Required evidence for each target

Record the exact OS edition/version/build, architecture, hardware/VM/emulation,
package filename, byte size and SHA-256, prerequisites, clean-account or machine
preparation, tester, and date. Preserve checksum-before-extraction output and
the full automated smoke log.

The executed scenarios must cover CLI identity, success, frontend/static and
runtime failures, native multifile compilation, redirected and explicit input,
all emitted artifacts, optimization modes, report and manifest validation, and
stable exit codes. The packaged LSP smoke must cover initialize, document open,
diagnostics, hover, navigation, completion, semantic tokens, full/incremental
changes, shutdown, and exit. Browser GUI evidence must cover identity, CSP,
capability bootstrap, compiler phases, debugger controls, input, report/IR/ZIP
exports, manifest/digest checks, and browser-console health. Native macOS and,
if supported, native Windows additionally require the platform shell,
export/external-link, menu, close/Quit, and clean-termination scenarios.

## Cross-target equivalence

Record one normalized comparison of COOL acceptance/rejection, diagnostic
codes/stages and source locations, program output/runtime errors, signed 32-bit
boundaries, report structure, optimizer results where identity is expected,
architecture-neutral deterministic VM state, and LSP semantic responses.
Only non-language-visible path and newline differences may be normalized.

Final 10/11 may set `Release-set verdict: PASS` only when every advertised row
refers to the same candidate and release-set hashes and all required evidence
passes. Any source or documentation change invalidates the candidate and this
ledger until Final 09/11 and Final 10/11 are repeated.
