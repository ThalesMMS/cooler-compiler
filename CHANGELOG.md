# Changelog

All notable user-visible changes are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and releases follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html) for the documented
CLI, JSON, archive, and Go-facing contracts.

## 1.0.0

### Added

- A stable `compiler` Go facade with context-aware single/multifile and
  incremental compilation, immutable detached artifacts, a concurrency-safe
  debugger, optional heap/watch/reverse capabilities, executable Go examples,
  an external-module import test, and a pinned v1 compatibility fixture.
- A packaged `cooler-lsp` stdio server with cancellable/debounced multifile
  diagnostics, symbols, hover, definition/references, contextual completion,
  semantic tokens, safe code actions, deterministic workspace policy, bounded
  resources, protocol replay tests, and generic Neovim setup instructions.
- Deterministic phase, VM, API, payload, scaling, and browser benchmarks with
  machine-readable local baselines, advisory median comparison, bounded release
  smoke coverage, and allowlisted CPU/heap profiling workflows.
- A formally documented compilation/debug session lifecycle with per-session
  operation leases, active-safe deterministic TTL/LRU eviction, explicit
  cleanup, injectable clocks/IDs, collision handling, local metrics, shutdown
  containment, model-based stress, and browser epoch replacement stress.
- A centralized, report-visible VM execution policy covering instruction,
  stack, frame, scope, allocation, trace, string, output, input, and breakpoint
  bounds, with CLI/Go/HTTP overrides, cancellation, reset determinism,
  transactional failures, and host-panic containment.
- Seven bounded local fuzz targets spanning lexing, parsing, single/multifile
  compilation, bytecode verification, program input, and JSON round trips;
  shared example/conformance/regression seeds; typed valid-program generation;
  metamorphic properties; and local smoke/campaign commands.
- A pre-execution bytecode verifier with deterministic worklist analysis for
  operand categories, stack/scope merges, control flow, calls, terminals,
  method tables, spans, IR, CFG, and pseudo-assembly metadata, plus bounded
  mutation/fuzz coverage and dedicated `COL51xx` internal diagnostics.
- Native ordered `SourceUnit` project compilation with stable source IDs,
  file-local spans across every source-linked artifact, source-aware
  breakpoints, and explicit multifile JSON/ZIP exports.
- A local differential conformance harness for user-supplied `coolc` and SPIM
  tools, with safe process limits, multifile/input support, normalized reports,
  evidence bundles, strict discovery, and a stale-checked versioned allowlist.
- A machine-readable normative COOL conformance matrix, original fixture corpus,
  local runner, claim audit, and JSON report available through `make conformance`.
- Versioned v1 JSON schemas for complete reports, diagnostics, report ZIPs, and
  release provenance.
- Deterministic Linux amd64/arm64, Windows amd64, and macOS arm64 release
  packages with `SHA256SUMS` and a machine-readable release manifest.
- Local runtime-smoke scripts, release evidence ledger, macOS native Quit test,
  and byte-for-byte reproducibility gate.
- Roadmap, contribution, security, diagnostic, stack IR, VM, debugger, and
  multifile reference documents.

### Changed

- COOL `Int` now has one architecture-independent signed 32-bit representation
  across parsing, bytecode, VM arithmetic, input/output, trace, debugger, and
  JSON export. Boundary overflow follows documented modulo-2^32 behavior and
  out-of-range `in_int` input deterministically returns zero.
- Runtime conformance now covers evaluation order, dynamic/static dispatch,
  shallow copy, `case`, every equality category, Unicode String built-ins, input
  edges, stable user-error spans, overflow-safe substring bounds, and exportable
  halted VM state.
- Expression semantics now expose typed-AST goldens, exhaustive `SELF_TYPE`,
  dispatch, scope, `let`, `case`, control, and operator fixtures, plus an
  explicit `<error_type>` that prevents derivative diagnostic cascades.
- Class-table conformance now covers multifile duplicates, complete feature and
  entry-point validation, iterative cycle recovery, explicit class/feature
  order, deep hierarchy lookup, and parent-first source-order initialization.
- Parser conformance now has grammar and precedence AST goldens, deterministic
  class/feature recovery, partial-AST rules, and hard nesting/diagnostic bounds.
- Lexer conformance now covers ASCII identifiers and whitespace, every keyword
  and operator, 32-bit literal boundaries, 1024-character strings, UTF-8 and
  UTF-16 locations, deterministic recovery, and stable diagnostic categories.
- Complete reports identify schema/compiler versions, source commit, target,
  compile options, and ordered source-unit hashes.
- The local GUI service now requires a per-process capability for mutating API
  calls, validates Host/Origin/content type/body limits, and refuses non-loopback
  listeners unless the user explicitly supplies `--allow-remote`.
- Diagnostics and conformance behavior have stable regression fixtures and a
  catalog checked against compiler source.

### Security

- Documented the single-user, loopback-only threat model and private reporting
  route. See [SECURITY.md](SECURITY.md).

### Migration notes

- Existing v0.1 CLI forms remain supported. New subcommands and report metadata
  are additive.
- The Go report decoder upgrades the earlier unversioned v0 report shape and
  accepts compatible `1.x.y` reports. Consumers that parse JSON themselves
  should validate `schemaVersion` and tolerate additive fields.
- Custom GUI API clients must load the initial page and retain its in-memory
  process capability; direct mutating requests without
  `X-Cooler-Capability` now receive HTTP 403.
- Release consumers should select the versioned target archive, verify it
  against `SHA256SUMS`, and must not treat cross-build status as runtime proof.
- Multiple files are compiled as ordered native source units, each lexed and
  parsed in its own coordinate space. Cooler never concatenates sources or
  inserts separators, so diagnostics, spans, and the ordered source-unit hashes
  in a report are per unit. Consumers that assumed one aggregate source
  coordinate must read the originating unit ID.
- COOL `Int` is now an architecture-independent signed 32-bit value across
  parsing, bytecode, VM arithmetic, input/output, trace, debugger, and JSON
  export. Overflow follows documented modulo-2^32 behavior, and an invalid or
  out-of-range `in_int` line deterministically returns zero. Programs that
  relied on host-width integers must expect wrapping at 32 bits.
- The `compiler` package is the stable public Go facade, reporting `APIVersion`
  1.0.0, with context-aware single/multifile and incremental compilation and
  immutable detached results. Internal packages remain unstable and must not be
  imported by consumers.
- Program input is an ordered sequence of lines; each `IO.in_string` or
  `IO.in_int` consumes exactly one and the transport removes its terminating LF
  or CRLF. Exhaustion or interactive EOF returns `""` and `0`. Pre-supplied
  stdin keeps non-interactive execution deterministic, while
  `coolerc run --interactive` reads each requested line live after preceding
  output.
- The debugger and the packaged `cooler-lsp` stdio server are additive. Existing
  non-debug CLI and Go entry points are unaffected; editors launch the packaged
  `cooler-lsp` binary, and neither debugger nor LSP capabilities are required
  for compilation.

## 0.1.0

### Added

- Classic COOL lexer, parser, semantic analysis, stack IR, pseudo-assembly, and
  educational object VM.
- Local native/browser workbench with phase controls, breakpoints, VM state,
  examples, multifile tabs, program input, and artifact export.
- `coolerc` command-line execution and artifact modes.

## Versioning and migrations

The supported contract includes documented CLI commands, exit codes, diagnostic
codes and fields, JSON schemas, archive paths, release package names, and public
Go types explicitly described as stable. Internal packages, pseudo-assembly
formatting, message prose, and visual layout may change without a major release.

Additive optional JSON fields and new commands are minor changes. Removing or
renaming a stable field/command, changing its type or meaning, reusing a
diagnostic code, or changing archive path semantics requires a major contract
version and a migration note here. One previous report-schema major is readable
for at least one maintenance release or rejected with a specific unsupported
version error. See the canonical [artifact version policy](docs/artifact-contract.md#version-policy).

[1.0.0]: https://github.com/ThalesMMS/cooler-compiler/compare/v0.1.0...v1.0.0
[0.1.0]: https://github.com/ThalesMMS/cooler-compiler/releases/tag/v0.1.0
