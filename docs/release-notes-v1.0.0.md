# Cooler Compiler Lab v1.0.0 release notes (draft)

These notes are prepared for the eventual public release but do not announce
that v1.0.0 is published. The source scope is implemented; the exact candidate,
target-runtime evidence, and public release remain gated by
[Final 09/11](https://github.com/ThalesMMS/cooler-compiler-dev/issues/59),
[Final 10/11](https://github.com/ThalesMMS/cooler-compiler-dev/issues/60), and
[Final 11/11](https://github.com/ThalesMMS/cooler-compiler-dev/issues/61).
Release publication must copy platform verdicts and hashes from the final
[v1.0.0 validation ledger](release-validation-v1.0.0.md); a successful local or
cross-build is not target-runtime evidence.

## Highlights

- Complete classic COOL language support within the declared
  [educational scope](../README.md#scope-and-compatibility), backed by the
  normative conformance matrix and executable fixtures.
- Ordered native source units with stable per-file identity and spans; sources
  are never concatenated into an artificial coordinate space.
- Architecture-independent signed 32-bit `Int` behavior and a verified,
  deterministic VM with documented resource limits.
- Source-level and reverse debugging, conditional breakpoints, logpoints,
  watches, bounded heap inspection, and cross-phase educational views.
- Optional verified educational optimization, a stable public Go compiler
  facade, and a bounded local COOL language server.
- Versioned reports, deterministic report ZIPs, release manifests, checksums,
  and reproducible local packaging.

The release remains intentionally local-first and has no CI, hosted compiler,
hosted LSP, or hosted test matrix.

## Known boundaries

Cooler is an educational compiler and workbench, not a drop-in replacement for
the reference compiler/runtime. It does not promise MIPS/SPIM output, reference
runtime ABI compatibility, modules, imports, separate compilation or linking,
native machine code, or byte-for-byte reference diagnostic wording. The GUI
and LSP are local tools, not multi-user network services. Exact behavioral
boundaries are canonical in the
[language contract](cool-language.md), [artifact contract](artifact-contract.md),
[debugger protocol](debugger-protocol.md), and [LSP contract](lsp.md).

macOS packages, when produced, use an ad-hoc signature unless the final ledger
records a different identity. Ad-hoc signing is not Developer ID distribution
or notarization. Native Windows support is advertised only if the exact
WebView2 package is built and runtime-tested on Windows 11; otherwise Windows
uses the packaged browser-hosted GUI and the native shell is excluded from the
supported binary matrix.

## Planned package and platform matrix

Every row is **pending** until the final ledger records the exact package hash
and target run. `built` and `pending` never mean `runtime-tested`.

| Target | Planned package | Included modes | Pre-qualification status |
| --- | --- | --- | --- |
| Linux amd64 | `cooler-compiler-v1.0.0-linux-amd64.tar.gz` | CLI, LSP, browser-hosted GUI | Pending exact-hash target test |
| Linux arm64 | `cooler-compiler-v1.0.0-linux-arm64.tar.gz` | CLI, LSP, browser-hosted GUI | Pending exact-hash target test |
| Windows 11 amd64 | `cooler-compiler-v1.0.0-windows-amd64.zip` | CLI, LSP, browser-hosted GUI; native WebView2 only if qualified | Pending exact-hash target test |
| macOS arm64 | `cooler-compiler-v1.0.0-macos-arm64.zip` | CLI, LSP, browser-hosted GUI, native WKWebView app | Pending exact-hash target test |

macOS amd64/universal, Windows arm64, and other combinations are unsupported
unless the final ledger explicitly qualifies them. Cross-compilation alone
does not expand this matrix.

## Install from a clean clone

Until the public tag exists, these commands are candidate instructions only.
After Final 11/11 publishes the tag, a clean source installation is:

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
local verification gate. The release page will also provide prebuilt archives
for the qualified rows above; those archives do not require a Go toolchain.

## Verify checksums and provenance

Download the selected archive together with `SHA256SUMS` and
`release-manifest.json`. From their directory, verify the archive before
extracting it:

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
file and the final validation ledger. The manifest records build provenance;
only the ledger records target execution. The canonical field meanings and
determinism policy are in the [artifact contract](artifact-contract.md).

## Migration from v0.1.0

### CLI and source projects

Multiple `.cl` arguments are ordered native source units. Diagnostics, spans,
and report hashes identify the originating unit, so consumers must not assume
one concatenated source coordinate. COOL `Int` now wraps deterministically as a
signed 32-bit value. The versioned CLI subcommands are canonical; documented
legacy invocations remain compatibility aliases. Program input is a sequence
of lines: supplied stdin is deterministic, while `coolerc run --interactive
FILE...` reads a line only when the VM requests it.

### Go API consumers

Import `github.com/ThalesMMS/cooler-compiler/compiler`; internal packages are
not supported APIs. Use the context-aware single-file, multifile, or incremental
entry points and treat returned results as immutable detached snapshots. Check
the public `APIVersion` before depending on facade changes. GUI API clients
must bootstrap and send the per-process capability described by the
[security policy](../SECURITY.md); unauthenticated mutating requests fail.
See the [public Go API guide](public-go-api.md) for the supported surface.

### Report consumers

Read `schemaVersion` before decoding a complete report or archive manifest.
Reports now preserve ordered `sourceUnits`, their IDs and hashes, 32-bit runtime
values, debugger/trace data, and build provenance. Unknown major schema
versions must be rejected; additive compatible fields must be ignored unless
needed. Validate documents with the schemas in `schemas/` and follow the
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

## Public repository update notes

Final 11/11 must export the exact qualified private candidate through the
documented one-way publication process, preserving the public v0.1.0 history
and tag. The public `main`, annotated `v1.0.0` tag, Go module, source archive,
release notes, manifest, `SHA256SUMS`, and uploaded packages must all map to the
same candidate. No source, documentation, generated file, or archive may be
rewritten during publication. The final check uses a separate clean clone,
resolves `github.com/ThalesMMS/cooler-compiler@v1.0.0` from an external module,
and downloads each public asset to compare its bytes with the qualified local
file. See the [development/publication contract](development-publication.md).
