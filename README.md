# Cooler Compiler Lab

**Cooler Compiler Lab** is a standalone educational compiler and debugger for the **COOL (Classroom Object-Oriented Language)** programming language. It combines a Go compiler core, an embedded browser/WebView interface, source-linked artifacts, a stack IR, pseudo-assembly, and an interactive VM session.

The compiler and local server are self-contained. On macOS, the GUI opens in a native `WKWebView`; on Windows, a CGO build uses the native WebView2-backed window. Linux and CGO-disabled desktop builds automatically use the default browser.

## Get the source

```bash
git clone https://github.com/ThalesMMS/cooler-compiler.git
cd cooler-compiler
```

## Scope and compatibility

Cooler v1.0.0 is an end-to-end educational COOL compiler and virtual machine. The pipeline is complete within Cooler's declared educational scope: it tokenizes, parses, type-checks, lowers, displays generated artifacts, and executes every supported construct listed in [`docs/cool-language.md`](docs/cool-language.md). “Complete” does not mean drop-in compatibility with every `coolc`, SPIM, MIPS, or reference-runtime artifact.

Every row below is part of the declared v1.0.0 contract. v1.0.0 is
implemented but not publicly released; the supported public release remains
v0.1.0 until [Final 11/11](https://github.com/ThalesMMS/cooler-compiler-dev/issues/61) closes.

| Area | Implemented in Cooler v1.0.0 | Compatibility boundary |
| --- | --- | --- |
| Lexer | Classic identifiers, keywords, literals, nested block comments, line comments, string escapes, diagnostics, and UTF-16-compatible source spans | Accepts the lexical rules documented for Cooler; diagnostics and source metadata are Cooler-specific |
| Parser | Classes, features, all documented COOL expression forms, precedence, and recovery with partial artifacts | No preprocessor, module syntax, or separate compilation/linking |
| Semantic analysis | Basic-class prelude, inheritance and cycle checks, override rules, scopes, conformance, least upper bounds, `SELF_TYPE`, and `Main.main()` validation | Aims at the documented classic semantics, not byte-for-byte reference diagnostic text |
| Stack IR and control flow | Source-linked executable stack instructions plus basic blocks and CFG edges, verified before VM execution | Internal educational bytecode, not the reference runtime ABI |
| Pseudo-assembly | Readable text generated from the stack bytecode | Not MIPS, executable assembly, an object file, or linker input |
| Virtual machine | Object initialization, dynamic/static dispatch, calls, `case`, built-ins, runtime diagnostics, output, and deterministic safety limits | Executes only in Cooler's Go VM; no SPIM or reference runtime |
| Debugger | Incremental phases, source stepping, conditional breakpoints, logpoints, watches, run/pause, reset, trace deltas, stack, frames, and source-linked state | In-process Cooler sessions, not a reference debugger protocol |
| CLI (`coolerc`) | Source from stdin or ordered `.cl` files; normal execution, stack IR, pseudo-assembly, and JSON modes | Its flags, output, diagnostics, and artifacts are not a `coolc` command-line compatibility layer |
| GUI | Local editor, examples, diagnostics, artifacts, exports, program input, and VM controls in native or browser-hosted UI | A local educational workbench, not a hosted compiler service |
| Multifile programs | Compiles ordered native source units with stable IDs and file-local spans across every artifact | No modules, imports, independent object files, or linker |
| Program input | Line-oriented input from the GUI box or redirected/piped CLI stdin after file arguments | Deterministic supplied input is the default; explicit `run --interactive FILE...` and GUI live input wait only at an exhausted input opcode |

Important intentional differences from the reference toolchain:

- `-assembly` and the Assembly panel expose educational pseudo-assembly only. Cooler does not emit executable or linkable MIPS and does not invoke SPIM.
- Programs execute through Cooler's purpose-built Go VM. Runtime objects are managed by Go's garbage collector; Cooler does not reproduce the reference runtime's object layout, collector choices, or GC test modes.
- COOL `Int` is an architecture-independent signed 32-bit value. Non-negative literals stop at `2147483647`; arithmetic and `~` wrap modulo 2^32, including `-2147483648 / -1 == -2147483648`, as an explicit deterministic Cooler policy where the manual leaves overflow unspecified.
- `IO.in_string` and `IO.in_int` consume one supplied line per call. Exhausted input returns `""`/`0`; `in_int` trims whitespace and returns `0` for an invalid or out-of-range signed 32-bit line. With CLI file arguments, redirected or piped stdin is deterministic program input; without file arguments, stdin is source. Explicit `run --interactive FILE...` reserves stdin for program input and reads only when the VM reports `waiting-input`.
- Execution is bounded for reproducible debugging: the VM limits instructions, operand-stack size, call depth, object allocations, and retained trace entries.

This section is the canonical release-note link for the v1.0.0 compatibility contract. The detailed grammar, built-ins, and precedence rules remain in [`docs/cool-language.md`](docs/cool-language.md).

## Features

- classic COOL lexical grammar, including nested `(* ... *)` comments and `--` line comments;
- classes, single inheritance, attributes, methods, overriding, and `SELF_TYPE`;
- dynamic and static dispatch (`receiver.method(...)` and `receiver@Type.method(...)`);
- `if`, `while`, blocks, `let`, `case`, `new`, `isvoid`, assignment, arithmetic, comparison, and Boolean complement;
- built-in `Object`, `IO`, `Int`, `String`, and `Bool` classes;
- inheritance-cycle, override-signature, scope, arity, conformance, and entry-point validation;
- source spans in tokens, AST nodes, symbols, diagnostics, IR, CFG blocks, and VM trace entries;
- generated stack IR and readable pseudo-assembly;
- opt-in verified educational IR optimization with before/after IR and CFG,
  source/original-instruction provenance, and explicit pass explanations;
- executable VM with object allocation, attribute initialization, dispatch, call frames, breakpoints, and runtime diagnostics;
- incremental compiler sessions that execute Lexer, Parser, Semantic Analysis, Stack IR, Pseudo-assembly, and VM preparation one phase at a time;
- GUI with syntax-highlighted source, multi-file Open/Add tabs, a program-input box for `IO.in_string` / `IO.in_int`, examples, recent files, clickable diagnostics, filterable tokens, typed AST, symbol table, IR, control-flow graph, VM trace, stack, frames, an accessible bounded heap inspector, and individual or ZIP artifact export;
- incremental Run/Pause execution that keeps the interface responsive and merges lightweight VM trace deltas;
- accessible About dialog with a concise pipeline guide, control reference, project link, author, version, and license details;
- `coolerc` command-line interface for local scripts and editor/tool integration.
- stable public Go compiler/debugger facade with immutable artifacts,
  multifile and incremental compilation, context cancellation, and optional
  heap/watch/reverse capabilities.
- local `cooler-lsp` stdio language server with diagnostics, navigation,
  symbols, hover, completion, semantic tokens, and safe quick fixes.

## Use as a Go library

```go
package main

import (
    "context"
    "fmt"
    "strings"

    "github.com/ThalesMMS/cooler-compiler/compiler"
)

func main() {
    result, err := compiler.Compile(context.Background(), `
class Main inherits IO {
  main() : Object { out_string("hello from Go\n") };
};`, compiler.DefaultOptions())
    if err != nil {
        panic(err)
    }
    fmt.Print(strings.Join(result.Runtime().Output, ""))
}
```

Use `CompileProject` for ordered source units, `ModeCheck` for diagnostics
without backend execution, `NewSession` for phase-by-phase compilation, and
`NewDebugSession` for a stopped debugger. The complete stability, error,
concurrency, and schema contract is in the
[`public Go API guide`](docs/public-go-api.md).

## Run the GUI

```bash
go run ./cmd/coolergui
```

On macOS with CGO enabled, this opens a native application window. A Windows native build requires CGO plus a MinGW-w64 toolchain and uses WebView2. Browser mode is available everywhere:

```bash
go run ./cmd/coolergui --browser
```

To run the server without opening a browser:

```bash
go run ./cmd/coolergui --browser --open=false --addr=127.0.0.1:9090
```

The GUI server is a local desktop/browser companion, not a hosted multi-user
service. It refuses non-loopback listeners unless `--allow-remote` is supplied;
that explicit mode prints a prominent exposure warning. Each process generates
a fresh capability that is injected into the uncached initial page, kept only in
browser memory, and required for compilation and session/VM requests. API
responses use `no-store`, and the server validates the listener Host, request
Origin, method, JSON media type, body shape, and bounded execution parameters.
Do not expose the server through a proxy or public interface.

### Compiler controls

- **Next Phase** executes exactly one compiler phase and keeps its intermediate state in the backend. Six clicks advance through Lexer, Parser, Semantic Analysis, Stack IR, Pseudo-assembly, and Educational VM preparation.
- **Compile** completes every remaining compiler phase but does not execute a VM instruction.
- **Opt: none/basic** selects the reference IR or the conservative verified
  pass pipeline. Changing it disposes the current session; the Optimize panel
  compares IR/CFG, mappings, transformations, refusals, and verifier status.
- **Run / Pause** completes pending phases and runs the program in small batches until it halts, reaches a breakpoint, waits for live input, reports a runtime error, or the user requests a pause.
- **Instr** completes pending phases and executes exactly one VM instruction.
- **Into / Over / Out** stop on distinct source constructs using source-unit,
  span, method, instruction, and frame-depth identity; generated scaffolding is
  skipped and built-ins are atomic.
- **Cursor** runs to the executable construct at the editor caret and reports a
  clear error for a non-executable location.
- **Debugger tools** edits enabled/conditional/hit-count breakpoints,
  non-pausing logpoints, and side-effect-free watches. Logpoints stay separate
  from the COOL console; **Save locally** explicitly persists this workspace
  configuration.
- **Heap** queries the stopped VM on demand, groups roots by frame/stack, and
  navigates stable object IDs through expandable, source-linked field tables.
  Explicit limits keep cycles, deep graphs, large strings, and payload bytes
  bounded without confusing omitted references with `void`.
- **Back / Reverse** restore bounded deterministic checkpoints and replay to a
  previous instruction/source stop. The timeline announces current, earliest,
  latest, revision, historical output, and explicit future discard; changing
  execution after going back creates a new revision.
- **Reset** recreates the stopped VM from the completed executable.
- **Open** can select several `.cl` files at once; each file becomes a native source unit with a stable ID and file-local coordinates. **Add** appends another file without replacing existing tabs.
- **Program input** in the console panel preloads `IO.in_string` / `IO.in_int` (same as `coolerc a.cl b.cl < input.txt`). When those lines are exhausted, Run exposes a labelled live-input field and a separate submitted-input history. Reset removes live history and restores only the preloaded lines. Catalog examples load their `input` field when present and otherwise clear the box.

Editing the source disposes the current compiler/VM session and restores `Next: Lexer`. Lexer diagnostics still allow the Parser phase to run so recovered tokens and a partial AST can remain visible. Lexer or Parser errors then block Semantic Analysis; semantic errors block IR, assembly, and VM preparation. Blocked phases are shown explicitly instead of exposing future artifacts.

## Run the CLI

```bash
go run ./cmd/coolerc examples/02_factorial.cl
```

Other useful modes:

```bash
go run ./cmd/coolerc run --input input.txt --max-steps 100000 program.cl
go run ./cmd/coolerc run --interactive program.cl
go run ./cmd/coolerc check program.cl
go run ./cmd/coolerc emit tokens program.cl
go run ./cmd/coolerc emit ast program.cl
go run ./cmd/coolerc emit symbols program.cl
go run ./cmd/coolerc emit cfg program.cl
go run ./cmd/coolerc emit ir program.cl
go run ./cmd/coolerc emit ir --opt=basic --emit-ir=both --explain-optimizations program.cl
go run ./cmd/coolerc emit assembly program.cl
go run ./cmd/coolerc emit report --diagnostic-format json program.cl
go run ./cmd/coolerc version --json

# v0.1-compatible forms
go run ./cmd/coolerc -ir examples/02_factorial.cl
go run ./cmd/coolerc -assembly examples/02_factorial.cl
go run ./cmd/coolerc -json examples/02_factorial.cl > result.json
cat examples/01_hello.cl | go run ./cmd/coolerc
go run ./cmd/coolerc program.cl < input.txt
go run ./cmd/coolerc stack.cl atoi.cl < stack.test
```

Several `.cl` files are compiled as one program in argument order. Each remains an independent source unit for lexing, parsing, diagnostics, artifacts, and debugging; this does not add a module or linking step. When one or more program file paths are given, redirected or piped stdin supplies program input after file arguments for `IO.in_string` / `IO.in_int`; explicit `run --interactive` consumes it one requested line at a time. When source comes from stdin (no file argument), stdin is the Cool source and program input is empty, so interactive mode requires a source file.

See [`docs/cli.md`](docs/cli.md) for the command contract, JSON diagnostic
shape, compatibility policy, and stable exit codes. Complete JSON reports,
GUI ZIPs, and release provenance follow the versioned
[`artifact contract`](docs/artifact-contract.md).

## Run the language server

```bash
go run ./cmd/cooler-lsp
```

The process speaks LSP JSON-RPC over stdin/stdout and writes logs only to
stderr. It is intended to be launched by an editor, not used as an interactive
terminal command. Workspace ordering, limits, features, cancellation behavior,
and a Neovim smoke configuration are documented in the
[`COOL language server guide`](docs/lsp.md).

## Test and build

```bash
make test
make vet
make lint
make fuzz-smoke
make property-test
make build
make build-all
make release-artifacts VERSION=1.0.0
```

Before preparing a release, run the complete local gate:

```bash
make release-check
```

The command checks module integrity, whitespace, formatting, the normative conformance corpus, differential-harness self-tests, tests, a bounded local fuzz smoke, vet, the exact repository-pinned `golangci-lint`, shell and JavaScript syntax, examples/resources, release builds, and two byte-identical package builds; it also reports platform-specific checks as passed or skipped. Install the pinned linter once with `./scripts/install-golangci-lint.sh`; verification itself performs no tool download. Run `make conformance` alone for its human summary and `dist/conformance-report.json`. `make fuzz TARGET=FuzzCompile DURATION=10m` runs one opt-in campaign; the [robustness testing guide](docs/robustness-testing.md) documents every target, bound, property, and regression rule. An optional `make differential` compares the original corpus with user-supplied `coolc`/SPIM tools and skips cleanly when they are absent; `STRICT=1`, `RULE=...`, and `FIXTURE=...` control strict discovery and filtering. See [`docs/conformance.md`](docs/conformance.md) and the [`differential guide`](docs/differential.md) for the local contracts, and [`docs/release.md`](docs/release.md) for release prerequisites and result interpretation. Target execution is tracked separately, because a successful cross-build is not runtime evidence; the latest recorded target evidence is the versioned [`v0.1.1 runtime-validation record`](docs/release-validation-v0.1.1.md), and v1.0.0 target evidence is recorded when [Final 10/11](https://github.com/ThalesMMS/cooler-compiler-dev/issues/60) closes. This project intentionally does not use CI.

The same operations without Make are:

```bash
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go vet ./...
go build ./cmd/coolergui
go build ./cmd/coolerc
```

## Documentation

- Project policy: [roadmap](ROADMAP.md), [changelog](CHANGELOG.md),
  [contributing guide](CONTRIBUTING.md), and [security policy](SECURITY.md).
- Compiler/runtime: [language coverage](docs/cool-language.md),
  [normative conformance matrix](docs/conformance.md),
  [generated conformance summary](docs/conformance-summary.md),
  [local differential testing](docs/differential.md),
  [diagnostic catalog](docs/diagnostics.md), [stack IR](docs/stack-ir.md),
  [VM runtime](docs/vm-runtime.md), and
  [multifile semantics](docs/multifile.md).
- Interfaces/artifacts: [CLI](docs/cli.md),
  [debugger protocol](docs/debugger-protocol.md), [program input](docs/input.md),
  [JSON and archive contract](docs/artifact-contract.md),
  [educational optimization contract](docs/optimization.md),
  [visualization and provenance model](docs/visualization-model.md), and
  [v0.1.1 target evidence](docs/release-validation-v0.1.1.md).
- Engineering/release: [architecture](docs/architecture.md),
  [robustness testing](docs/robustness-testing.md),
  [local performance engineering](docs/performance.md),
  [adversarial v1.0.0 audit](docs/adversarial-audit-v1.0.0.md),
  [v1.0.0 completion contract](docs/v1.0-completion-contract.md),
  [draft v1.0.0 release notes](docs/release-notes-v1.0.0.md),
  [v1.0.0 target-evidence ledger](docs/release-validation-v1.0.0.md),
  [local release process](docs/release.md), and
  [development/publication boundary](docs/development-publication.md).

Run `make docs-check` to validate local links and headings, JSON schema
identities, required index entries, and the generated diagnostic-code inventory.

`make build-all` emits browser-fallback GUI binaries and `coolerc` binaries for Linux, Windows, and macOS. To cross-compile the native Windows WebView2 GUI, install MinGW-w64 and run:

```bash
make build-windows-native
```

`make release-artifacts` requires a clean tree and an empty `dist/`. It creates
the four versioned `.tar.gz`/`.zip` packages, `release-manifest.json`, and
`SHA256SUMS` with an explicit source commit and `SOURCE_DATE_EPOCH`.

Create a macOS application bundle:

```bash
make macos-app
```

The result is written to:

```text
dist/Cooler Compiler Lab.app
```

The bundle includes the project icon from `assets/AppIcon.icns`. The 1024+ pixel source artwork is kept in `assets/AppIcon.png` so the icon can be regenerated for other platforms or sizes.

## Example

```cool
class Main inherits IO {
  factorial(n : Int) : Int {
    if n <= 1 then 1 else n * factorial(n - 1) fi
  };

  main() : Object {
    {
      out_string("5! = ");
      out_int(factorial(5));
      out_string("\n");
    }
  };
};
```

## Project structure

```text
cmd/coolergui/               local HTTP server and desktop/browser launcher
cmd/coolerc/                 command-line compiler
internal/compiler/           lexer, parser, type checker, IR, CFG, VM, debugger
internal/web/                embedded GUI assets
assets/                      application icon source and macOS icon bundle
examples/                    guided .cl programs and embedded catalog
docs/                        architecture, language coverage, and implementation notes
scripts/                     packaging helpers
```

## License

Project-owned code, documentation, and assets are licensed under the [GNU Affero General Public License v3.0 or later](LICENSE). See [`NOTICE`](NOTICE) for the project copyright and license declaration.

Dependencies, platform WebViews, bundled teaching material, and asset provenance are documented in [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md). Third-party material retains its own license terms and is not relicensed by the project's AGPL declaration.
