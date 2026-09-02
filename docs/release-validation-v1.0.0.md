# v1.0.0 release validation and platform qualification record

This document is the authoritative evidence ledger for the published
[Cooler Compiler v1.0.0 release](https://github.com/ThalesMMS/cooler-compiler/releases/tag/v1.0.0).
It replaces the pre-release template that previously contained pending fields.

**Integrity qualification verdict: PASS.**

This verdict means that the released source and all published assets are mapped
to one candidate, reproducibly built, checksummed, and verified after download.
It is deliberately **not** a claim of full runtime certification on every
platform listed below.

## Status vocabulary

- `runtime-tested`: the exact archive hash was verified before extraction and a
  packaged target smoke was executed on that target;
- `native GUI tested`: the exact native GUI package was launched and exercised
  in its native shell;
- `cross-built and checksum-qualified`: the archive was produced reproducibly,
  structurally validated, and checksum-matched, but not executed on the named
  target;
- `source-build-only`: source support exists, but no supported prebuilt binary
  is distributed;
- `unsupported`: outside the advertised v1.0.0 target set.

## Release identity

- Version: `1.0.0`.
- Public source commit: the commit referenced by the corrected `v1.0.0` tag.
- Annotated public tag: `v1.0.0`.
- `SOURCE_DATE_EPOCH`: recorded in the corrected `release-manifest.json` asset.
- The package manifest records the full source commit used for the corrected
  release assets.
- Reproducibility: two complete release builds produced 6/6 byte-identical
  files.
- The release's `release-manifest.json` and `SHA256SUMS` assets are the
  authoritative package identities.

## Published package matrix

| Target | Package | Qualification status |
| --- | --- | --- |
| Linux amd64 | `cooler-compiler-v1.0.0-linux-amd64.tar.gz` | Cross-built and checksum-qualified; runtime not executed |
| Linux arm64 | `cooler-compiler-v1.0.0-linux-arm64.tar.gz` | Cross-built and checksum-qualified; runtime not executed |
| Windows 11 amd64 | `cooler-compiler-v1.0.0-windows-amd64.zip` | Includes Windows executables; bounded packaged CLI, LSP, browser GUI, and native WebView2 runtime tests passed |
| macOS arm64 | `cooler-compiler-v1.0.0-macos-arm64.zip` | Includes `Cooler Compiler.app`; cross-built and checksum-qualified; macOS runtime, signing, notarization, and Gatekeeper tests not executed |

The release also publishes `release-manifest.json` and `SHA256SUMS`; those files
contain the exact byte sizes and SHA-256 values for every corrected asset.

macOS amd64/universal, Windows arm64, and every unlisted combination are
unsupported or source-build-only for v1.0.0. No untested package is presented as
runtime-tested merely because it cross-compiled successfully.

## Candidate verification actually executed

The corrected candidate preserves the public-safe source tree and adds the
application-bundle requirement to release packaging. The macOS ZIP now carries
the complete `Cooler Compiler.app` directory, including `Info.plist` and its
executable, while Windows continues to carry the `.exe` application builds.

The following bounded checks were executed on the exact final candidate:

- `go mod verify`;
- `git diff --check HEAD --`;
- `go test ./examples`;
- clean CLI, LSP, and GUI builds from the corrected source;
- two complete release builds with 6/6 byte-identical files;
- packaged Windows CLI `version --json` and sample execution;
- packaged LSP version and initialize response;
- packaged native Windows WebView2 launch, close, and exit-zero smoke;
- archive structure, metadata, manifests, checksums, and source identity.

The immediately preceding stabilization candidate had recorded:

- 63 conformance rules and 90 fixtures;
- 61 supported rules and 2 intentional divergences;
- zero fixture failures and zero incomplete statuses;
- 90 differential cases, 81 matches, 9 expected divergences, zero unexpected
  mismatches, and zero reference-tool errors;
- local docs, lint, property, session stress, repeated race, benchmark,
  CLI/GUI/API/LSP, and release-gate evidence.

Those extended suites were **not rerun** after the final public-safe README-only
candidate adjustment and are not represented as if they were. Long-running
fuzz, repeated race/stress, and full cross-platform runtime campaigns were
explicitly removed from the final candidate requirements by owner decision on
September 2, 2026. The release therefore makes only the bounded claims recorded
in this ledger.

## Windows amd64 qualification

The exact Windows archive passed bounded packaged verification covering:

- CLI version identity and a sample COOL execution;
- packaged LSP identity and initialize response;
- browser-hosted GUI startup;
- native WebView2 window startup;
- native window close and clean process exit.

The package is therefore described as runtime-tested for those bounded
scenarios. This does not claim exhaustive Windows certification or substitute
for user testing in every Windows configuration.

## Linux and macOS qualification boundaries

Linux amd64, Linux arm64, and macOS arm64 archives were produced from the same
candidate and source timestamp, reproduced byte-for-byte across two builds,
and included in the unified checksum and manifest set.

Runtime execution was not performed for those exact v1.0.0 archives. In
particular, the macOS package was not qualified for WKWebView execution,
code-signing identity, notarization, or Gatekeeper acceptance. The package does
contain the `Cooler Compiler.app` bundle, but it must not be interpreted as
Developer ID distribution.

## Public publication verification

After publication:

- all six release assets were downloaded from GitHub and compared byte-for-byte
  with the qualified local files;
- a clean clone at `v1.0.0` resolved the annotated tag to the expected public
  commit and contained no workflow configuration;
- the clean clone matched the source commit recorded by the release manifest;
- `go mod verify` and clean builds of `coolerc`, `cooler-lsp`, and `coolergui`
  passed in the recorded publication environment;
- `go list -m github.com/ThalesMMS/cooler-compiler@v1.0.0` resolved the tagged
  module;
- an unrelated external module downloaded v1.0.0, compiled, and executed the
  public Go API, reporting API and compiler version `1.0.0`;
- the repository remained public on `main`, with AGPL-3.0 license detection and
  project topics present.

## Interpretation

The v1.0.0 source and release artifacts are complete and published within the
declared educational scope and bounded verification policy. The strongest
platform claim belongs to Windows amd64. Linux and macOS archives are valid,
reproducible release artifacts, but this ledger intentionally does not convert
cross-build evidence into runtime evidence.
