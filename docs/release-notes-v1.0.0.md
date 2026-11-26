# Cooler Compiler v1.0.0 release notes

[Cooler Compiler v1.0.0](https://github.com/ThalesMMS/cooler-compiler/releases/tag/v1.0.0)
introduced the complete educational COOL compiler and workbench described by
the project. These notes summarize its user-visible interfaces and migration
guidance.

## Highlights

- Classic COOL language support within the documented
  [educational scope](../README.md#scope-and-compatibility), with a normative
  conformance matrix and executable examples.
- Ordered multi-file compilation with stable per-file source identity and
  diagnostic spans.
- Architecture-independent signed 32-bit integers and a verified virtual
  machine with deterministic resource limits.
- Source-level and reverse debugging, conditional breakpoints, logpoints,
  watches, bounded heap inspection, and interactive program input.
- Optional verified educational optimization, a public Go compiler and
  debugger API, and a local COOL language server.
- Versioned reports, deterministic report archives, checksums, and release
  manifests.

## Compatibility boundaries

Cooler is an educational compiler and workbench. The v1.0.0 release did not
provide MIPS/SPIM output or reference-runtime ABI compatibility, separate
compilation, native machine code, byte-for-byte reference diagnostic wording,
or hosted compiler and debugger services. Current project behavior is described
in the [language contract](cool-language.md), [CLI reference](cli.md),
[artifact contract](artifact-contract.md), and
[debugger protocol](debugger-protocol.md).

## Build from source

```bash
git clone https://github.com/ThalesMMS/cooler-compiler.git
cd cooler-compiler
git checkout v1.0.0
go build -o ./bin/coolerc ./cmd/coolerc
go build -o ./bin/cooler-lsp ./cmd/cooler-lsp
go build -o ./bin/coolergui ./cmd/coolergui
```

Go 1.25 or a compatible newer toolchain is required. Prebuilt archives are
available from the [release page](https://github.com/ThalesMMS/cooler-compiler/releases/tag/v1.0.0).
Verify downloaded files against the accompanying `SHA256SUMS` before use.

## Migration from v0.1.0

Multiple `.cl` arguments are ordered source units. Diagnostics, spans, and
report hashes identify their source file; consumers should not assume that
files are joined into one source coordinate space. COOL `Int` uses deterministic
signed 32-bit arithmetic. The versioned CLI subcommands are canonical, while
the documented legacy invocations remain compatibility aliases. Program input
is a sequence of lines: supplied stdin is deterministic, and
`coolerc run --interactive FILE...` reads a line only when the VM requests it.

Import `github.com/ThalesMMS/cooler-compiler/compiler` for the public Go API;
internal packages are not supported APIs. Use the context-aware compilation
entry points and treat returned results as immutable snapshots. Report
consumers should read `schemaVersion`, preserve ordered `sourceUnits`, reject
unknown major schema versions, and tolerate compatible additive fields.

## Language server

Configure the editor to launch `cooler-lsp` (`cooler-lsp.exe` on Windows) over
stdio. Standard output is reserved for JSON-RPC framing. The server requires no
network service or editor extension; see the [LSP guide](lsp.md) for its
protocol scope and setup details.
