# Cooler Compiler v1.1.0 release notes

Cooler Compiler v1.1.0 expands the local COOL workbench with editor tools,
course-assignment modes, and additional inspection and execution targets.

## Highlights

- Course mode can print course-style token, AST, and semantic output, use a
  generated lexer and LALR parser, emit MIPS assembly, and run it in the
  embedded simulator. The default front-end, VM, diagnostics, and pseudo-
  assembly remain unchanged.
- The CLI can format COOL sources and run manifest-driven program tests.
- Compiler warnings and symbol suggestions help locate common source issues.
  The language server adds rename, signature help, and optional inlay hints.
- The workbench can visualize and export AST and control-flow graphs, recover
  unsaved drafts, and inspect exported reports without executing them.
- A local VS Code extension includes a DAP adapter for source-level debugging.
- IR optimization adds algebraic simplification and safe removal of discarded
  local computations. The reverse debugger can seek through its retained
  history.

## Compatibility

The additions are opt-in and preserve the v1.0.0 defaults. Course-style output
aims to match the classic course tools; it does not promise compatibility with
every tool distribution. MIPS output targets the included Cool runtime and
simulator. See the [course assignment guide](course-assignments.md),
[MIPS backend guide](mips-backend.md), and [CLI reference](cli.md).

The public Go API is version 1.1.0. Compile reports use schema 1.6.0, which
adds the optional `options.frontend` field. The decoder continues reading
compatible earlier 1.x report schemas. See the
[public Go API](public-go-api.md) and [artifact contract](artifact-contract.md).

## Downloads and checksums

The release includes Linux amd64 and arm64, Windows amd64, and macOS arm64
archives, plus `release-manifest.json` and `SHA256SUMS`. Verify the selected
archive against `SHA256SUMS` before extracting it. The platform support table
describes the scope of these builds.

## Build from source

```bash
git clone https://github.com/ThalesMMS/cooler-compiler.git
cd cooler-compiler
git checkout v1.1.0
go build -o ./bin/coolerc ./cmd/coolerc
go build -o ./bin/cooler-lsp ./cmd/cooler-lsp
go build -o ./bin/coolergui ./cmd/coolergui
```

Go 1.25 or a compatible newer toolchain is required. See the
[release guide](release.md) for release packaging and the README for the
complete compiler and GUI usage overview.
