# Roadmap

This roadmap separates released functionality from possible future work. It
describes direction, not a delivery-date promise. Development is prepared in
this private repository and reviewed snapshots are published in
[`ThalesMMS/cooler-compiler`](https://github.com/ThalesMMS/cooler-compiler)
under the repository's reviewed publication boundary.

## Supported release

The current supported public release is
[**Cooler Compiler v1.0.0**](https://github.com/ThalesMMS/cooler-compiler/releases/tag/v1.0.0),
published on September 2, 2026.

Its public source identity is:

- public commit: `07f68ab0600a6d61ef51f9160f3bc007149288ed`;
- annotated tag: `v1.0.0`;
- private release candidate: `e18373d9f8ebc5117a9451c871759f0c09580453`;
- sanitized public/private tree comparison: exact for 439 published files.

The release assets, checksums, provenance, platform statuses, and verification
boundaries are recorded in the
[v1.0.0 validation ledger](docs/release-validation-v1.0.0.md). The release is
complete within the declared educational compiler/workbench scope. This does
not imply MIPS/SPIM compatibility, reference-runtime ABI compatibility, native
code generation, modules/imports/linking, hosted execution, or CI.

## v1.0.0 completion status

The four completion states used during development have all been reached:

1. **Implemented** — the declared source, tests, fixtures, schemas, interfaces,
   and documentation were completed.
2. **Locally verified** — the candidate received the recorded bounded local
   checks, including module/build/export/package verification and the earlier
   complete conformance and differential evidence.
3. **Artifact-qualified** — the four platform archives were produced
   reproducibly, checksummed, and mapped to one source candidate. Windows amd64
   additionally passed bounded packaged CLI, LSP, browser-GUI, and native
   WebView2 execution. Linux amd64/arm64 and macOS arm64 were cross-built and
   checksum-qualified; runtime execution on those targets is not claimed.
4. **Publicly released** — the exact sanitized source, annotated tag, release
   assets, manifest, and checksum file were published. All six downloaded
   assets were then compared byte-for-byte with the qualified local files, and
   a clean external Go consumer resolved and used `@v1.0.0` successfully.

The [v1.0.0 completion contract](docs/v1.0-completion-contract.md) records the
closed sequence and the exact meaning of completion.

## Released implementation history

### v0.1.0 — initial standalone release

The first public release established the standalone lexer, parser, semantic
analyzer, educational stack IR, pseudo-assembly, Go VM, basic debugger, CLI,
GUI, examples, exports, and cross-platform packages.

### v1.0.0 — complete educational workbench

The current release adds and stabilizes:

- rule-level COOL conformance with a generated human-readable summary;
- native ordered `SourceUnit` multifile compilation;
- architecture-independent signed 32-bit COOL integers;
- bytecode verification and deterministic VM/session resource bounds;
- source-level and reverse debugging, conditional breakpoints, logpoints,
  watches, interactive input, bounded heap inspection, and relationship views;
- optional verified educational optimization;
- a stable public Go compiler/debugger API;
- the local `cooler-lsp` language server;
- versioned reports, deterministic ZIP exports, release manifests, checksums,
  reproducible packaging, and explicit platform evidence.

See [CHANGELOG.md](CHANGELOG.md) and the
[v1.0.0 release notes](docs/release-notes-v1.0.0.md) for the detailed change
record and migration guidance.

## Exploratory directions

These are possible enhancements beyond v1.0.0, not unfinished requirements:

- native machine-code backends or reference-toolchain binary compatibility;
- a third-party extension or compiler-pass ecosystem;
- authenticated remote or multi-user hosting;
- additional desktop architectures and mobile packages.

An exploratory direction becomes committed work only after it receives an
explicitly scoped issue or milestone. Until then, the project remains complete
within the v1.0.0 contract.

## Roadmap maintenance

Update this file when a new release or accepted milestone changes the supported
scope. Completed user-visible changes belong in [CHANGELOG.md](CHANGELOG.md);
implementation details belong in commits and issues rather than duplicated
narrative here.
