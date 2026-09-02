# Cooler Compiler v1.0.0 release notes

[Cooler Compiler v1.0.0](https://github.com/ThalesMMS/cooler-compiler/releases/tag/v1.0.0)
was published on September 2, 2026. It completes the declared educational COOL
compiler/workbench scope and replaces v0.1.0 as the current supported public
release.

The corrected public tag points to the reviewed source commit recorded in
`release-manifest.json`. The exact asset identities and bounded platform
evidence are in the [v1.0.0 validation ledger](release-validation-v1.0.0.md).

## Highlights

- Complete classic COOL language support within the declared
  [educational scope](../README.md#scope-and-compatibility), backed by a
  normative conformance matrix and executable fixtures.
- Ordered native source units with stable per-file identity and spans; sources
  are never concatenated into an artificial coordinate space.
- Architecture-independent signed 32-bit `Int` behavior and a verified,
  deterministic VM with documented resource limits.
- Source-level and reverse debugging, conditional breakpoints, logpoints,
  watches, bounded heap inspection, interactive input, and cross-phase
  educational views.
- Optional verified educational optimization, a stable public Go compiler and
  debugger facade, and a bounded local COOL language server.
- Versioned reports, deterministic report ZIPs, release manifests, checksums,
  reproducible local packaging, and explicit platform qualification boundaries.

The release remains intentionally local-first and has no CI, hosted compiler,
hosted LSP, or hosted test matrix.

## Known boundaries

Cooler is an educational compiler and workbench, not a drop-in replacement for
the reference compiler/runtime. It does not promise:

- MIPS/SPIM output or reference-runtime ABI compatibility;
- modules, imports, separate compilation, object files, or linking;
- native machine code;
- byte-for-byte reference diagnostic wording;
- a hosted or multi-user compiler, debugger, or LSP service.

Exact behavioral boundaries are canonical in the
[language contract](cool-language.md), [artifact contract](artifact-contract.md),
[debugger protocol](debugger-protocol.md), and [LSP contract](lsp.md).

The v1.0.0 qualification policy was bounded. Windows amd64 received packaged
runtime smoke coverage. Linux and macOS archives were reproduced and
checksum-qualified but were not executed on those target systems. The macOS
archive includes a `Cooler Compiler.app` bundle; when assembled on a non-macOS
host its executable uses the browser fallback. This release does not present
cross-building as runtime certification.

## Published package and platform matrix

| Target | Package | Included modes | v1.0.0 status |
| --- | --- | --- | --- |
| Linux amd64 | `cooler-compiler-v1.0.0-linux-amd64.tar.gz` | CLI, LSP, browser-hosted GUI | Reproducibly cross-built and checksum-qualified; runtime not executed |
| Linux arm64 | `cooler-compiler-v1.0.0-linux-arm64.tar.gz` | CLI, LSP, browser-hosted GUI | Reproducibly cross-built and checksum-qualified; runtime not executed |
| Windows 11 amd64 | `cooler-compiler-v1.0.0-windows-amd64.zip` | CLI, LSP, browser-hosted GUI, native WebView2 shell | Bounded packaged CLI, LSP, browser-GUI, and native WebView2 runtime smoke passed |
| macOS arm64 | `cooler-compiler-v1.0.0-macos-arm64.zip` | CLI, LSP, browser-hosted GUI, `Cooler Compiler.app` bundle | Reproducibly cross-built and checksum-qualified; macOS runtime, signing, notarization, and Gatekeeper tests not executed |

The authoritative SHA-256 values for the corrected assets are published in the
attached `SHA256SUMS` and `release-manifest.json` files.

macOS amd64/universal, Windows arm64, and other unlisted combinations are
unsupported or source-build-only for v1.0.0.

## Install from a clean clone

```bash
git clone https://github.com/ThalesMMS/cooler-compiler.git
cd cooler-compiler
git checkout v1.0.0
go build -o ./bin/coolerc ./cmd/coolerc
go build -o ./bin/cooler-lsp ./cmd/cooler-lsp
go build -o ./bin/coolergui ./cmd/coolergui
```

Go 1.25 or a compatible newer toolchain is required. Run
`./scripts/install-golangci-lint.sh` once and `make release-check` for the full
repository-local gate. Prebuilt archives are available from the release page and
do not require a Go toolchain.

## Verify checksums and provenance

Download the selected archive together with `SHA256SUMS` and
`release-manifest.json`. Verify the archive before extracting it:

```bash
sha256sum --check SHA256SUMS
```

On Windows PowerShell:

```powershell
$expected = (Select-String -LiteralPath .\SHA256SUMS -Pattern 'cooler-compiler-v1.0.0-windows-amd64.zip$').Line.Split()[0]
$actual = (Get-FileHash -Algorithm SHA256 -LiteralPath .\cooler-compiler-v1.0.0-windows-amd64.zip).Hash.ToLowerInvariant()
if ($actual -ne $expected) { throw 'checksum mismatch' }
```

Then compare the manifest's `version`, `sourceCommit`, target, filename, byte
size, SHA-256, toolchain, build flags, and `sourceDateEpoch` with the downloaded
file and the validation ledger. The manifest records build provenance; the
ledger records what was actually executed on each platform. The canonical field
meanings and determinism policy are in the
[artifact contract](artifact-contract.md).

## Migration from v0.1.0

### CLI and source projects

Multiple `.cl` arguments are ordered native source units. Diagnostics, spans,
and report hashes identify the originating unit, so consumers must not assume
one concatenated source coordinate. COOL `Int` now wraps deterministically as a
signed 32-bit value. The versioned CLI subcommands are canonical; documented
legacy invocations remain compatibility aliases. Program input is a sequence of
lines: supplied stdin is deterministic, while
`coolerc run --interactive FILE...` reads a line only when the VM requests it.

### Go API consumers

Import `github.com/ThalesMMS/cooler-compiler/compiler`; internal packages are
not supported APIs. Use the context-aware single-file, multifile, or incremental
entry points and treat returned results as immutable detached snapshots. Check
the public `APIVersion` before depending on facade changes. GUI API clients must
bootstrap and send the per-process capability described by the
[security policy](../SECURITY.md); unauthenticated mutating requests fail. See
the [public Go API guide](public-go-api.md) for the supported surface.

### Report consumers

Read `schemaVersion` before decoding a complete report or archive manifest.
Reports preserve ordered `sourceUnits`, their IDs and hashes, 32-bit runtime
values, debugger/trace data, and build provenance. Unknown major schema versions
must be rejected; additive compatible fields must be ignored unless needed.
Validate documents with the schemas in `schemas/` and follow the
[artifact version policy](artifact-contract.md#version-policy).

## LSP setup from a packaged archive

Verify and extract the target archive, then configure the editor to launch the
included `cooler-lsp` (`cooler-lsp.exe` on Windows) over stdio. Do not point the
editor at `coolergui`, use a TCP transport, or wrap stdout with logging; stdout
is reserved for JSON-RPC framing. For example, after replacing the path:

```lua
vim.lsp.start({
  name = "cooler-lsp",
  cmd = { "/absolute/path/to/extracted/cooler-lsp" },
  root_dir = vim.fs.root(0, { ".git" }) or vim.fn.getcwd(),
})
```

The packaged server requires no editor extension or network service. Editor
configuration, protocol scope, resource limits, and a deterministic smoke are
documented in the [LSP guide](lsp.md).

## Publication verification

The public update preserved the v0.1.0 history and published the sanitized
v1.0.0 tree as commit `07f68ab0600a6d61ef51f9160f3bc007149288ed` with an
annotated `v1.0.0` tag. The public and candidate trees matched for 439 files.

After upload:

- all six assets were downloaded and compared byte-for-byte with their
  qualified local counterparts;
- the clean public clone passed module verification and CLI/LSP/GUI builds in
  the recorded publication environment;
- an unrelated Go module resolved
  `github.com/ThalesMMS/cooler-compiler@v1.0.0`, compiled, and exercised the
  public API successfully;
- repository visibility, `main`, AGPL-3.0 detection, topics, links, and release
  identity were checked.

See the [v1.0.0 validation ledger](release-validation-v1.0.0.md) for the exact
evidence and limitations.
