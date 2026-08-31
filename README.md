# Cooler Compiler Lab

**Cooler Compiler Lab** is a standalone educational compiler and debugger for the **COOL (Classroom Object-Oriented Language)** programming language. It combines a Go compiler core, an embedded browser/WebView interface, source-linked artifacts, a stack IR, pseudo-assembly, and an interactive VM session.

The compiler and local server are self-contained. On macOS, the GUI opens in a native `WKWebView`; on Windows, a CGO build uses the native WebView2-backed window. Linux and CGO-disabled desktop builds automatically use the default browser.

## Get the source

```bash
git clone https://github.com/ThalesMMS/cooler-compiler.git
cd cooler-compiler
```

## Scope and compatibility

Cooler v0.1 is an end-to-end educational COOL compiler, debugger, and virtual machine. The pipeline is complete within Cooler's declared educational scope: it tokenizes, parses, type-checks, lowers, displays generated artifacts, and executes every supported construct listed in [`docs/cool-language.md`](docs/cool-language.md). “Complete” does not mean drop-in compatibility with every `coolc`, SPIM, MIPS, or reference-runtime artifact.

| Area | Implemented in Cooler v0.1 | Compatibility boundary |
| --- | --- | --- |
| Lexer | Classic identifiers, keywords, literals, nested block comments, line comments, string escapes, diagnostics, and UTF-16-compatible source spans | Accepts the lexical rules documented for Cooler; diagnostics and source metadata are Cooler-specific |
| Parser | Classes, features, all documented COOL expression forms, precedence, and recovery with partial artifacts | No preprocessor, module syntax, or separate compilation/linking |
| Semantic analysis | Basic-class prelude, inheritance and cycle checks, override rules, scopes, conformance, least upper bounds, `SELF_TYPE`, and `Main.main()` validation | Aims at the documented classic semantics, not byte-for-byte reference diagnostic text |
| Stack IR and control flow | Source-linked executable stack instructions plus basic blocks and CFG edges | Internal educational bytecode, not the reference runtime ABI |
| Pseudo-assembly | Readable text generated from the stack bytecode | Not MIPS, executable assembly, an object file, or linker input |
| Virtual machine | Object initialization, dynamic/static dispatch, calls, `case`, built-ins, runtime diagnostics, output, and deterministic safety limits | Executes only in Cooler's Go VM; no SPIM or reference runtime |
| Debugger | Incremental phases, breakpoints, step, run/pause, reset, trace deltas, stack, frames, and source-linked state | In-process Cooler sessions, not a reference debugger protocol |
| CLI (`coolerc`) | Source from stdin or ordered `.cl` files; normal execution, stack IR, pseudo-assembly, and JSON modes | Its flags, output, diagnostics, and artifacts are not a `coolc` command-line compatibility layer |
| GUI | Local editor, examples, diagnostics, artifacts, exports, program input, and VM controls in native or browser-hosted UI | A local educational workbench, not a hosted compiler service |
| Multifile programs | Concatenates complete class declarations in file/tab order into one COOL compilation unit and maps diagnostics back to files | No modules, imports, independent object files, or linker |
| Program input | Line-oriented input from the GUI box or redirected/piped CLI stdin after file arguments | Interactive CLI TTY input is intentionally treated as empty; exhaustion and invalid `in_int` are deterministic as documented below |

Important intentional differences from the reference toolchain:

- `-assembly` and the Assembly panel expose educational pseudo-assembly only. Cooler does not emit executable or linkable MIPS and does not invoke SPIM.
- Programs execute through Cooler's purpose-built Go VM. Runtime objects are managed by Go's garbage collector; Cooler does not reproduce the reference runtime's object layout, collector choices, or GC test modes.
- Integer literals are checked against the classic non-negative signed 32-bit literal range, but arithmetic uses the host Go `int`. Operations therefore do not emulate strict 32-bit overflow wrapping.
- `IO.in_string` and `IO.in_int` consume one supplied line per call. Exhausted input returns `""`/`0`; `in_int` trims whitespace and returns `0` for an invalid line. With CLI file arguments, redirected or piped stdin is program input; without file arguments, stdin is source, and interactive terminal stdin is not consumed as program input.
- Execution is bounded for reproducible debugging: the VM limits instructions, operand-stack size, call depth, object allocations, and retained trace entries.

This section is the canonical release-note link for the v0.1 compatibility contract. The detailed grammar, built-ins, and precedence rules remain in [`docs/cool-language.md`](docs/cool-language.md).

## Features

- classic COOL lexical grammar, including nested `(* ... *)` comments and `--` line comments;
- classes, single inheritance, attributes, methods, overriding, and `SELF_TYPE`;
- dynamic and static dispatch (`receiver.method(...)` and `receiver@Type.method(...)`);
- `if`, `while`, blocks, `let`, `case`, `new`, `isvoid`, assignment, arithmetic, comparison, and Boolean complement;
- built-in `Object`, `IO`, `Int`, `String`, and `Bool` classes;
- inheritance-cycle, override-signature, scope, arity, conformance, and entry-point validation;
- source spans in tokens, AST nodes, symbols, diagnostics, IR, CFG blocks, and VM trace entries;
- generated stack IR and readable pseudo-assembly;
- executable VM with object allocation, attribute initialization, dispatch, call frames, breakpoints, and runtime diagnostics;
- incremental compiler sessions that execute Lexer, Parser, Semantic Analysis, Stack IR, Pseudo-assembly, and VM preparation one phase at a time;
- GUI with syntax-highlighted source, multi-file Open/Add tabs, a program-input box for `IO.in_string` / `IO.in_int`, examples, recent files, clickable diagnostics, filterable tokens, typed AST, symbol table, IR, control-flow graph, VM trace, stack, frames, and individual or ZIP artifact export;
- incremental Run/Pause execution that keeps the interface responsive and merges lightweight VM trace deltas;
- accessible About dialog with a concise pipeline guide, control reference, project link, author, version, and license details;
- `coolerc` command-line interface for scripts and CI.

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

### Compiler controls

- **Next Phase** executes exactly one compiler phase and keeps its intermediate state in the backend. Six clicks advance through Lexer, Parser, Semantic Analysis, Stack IR, Pseudo-assembly, and Educational VM preparation.
- **Compile** completes every remaining compiler phase but does not execute a VM instruction.
- **Run / Pause** completes pending phases and runs the program in small batches until it halts, reaches a breakpoint, reports a runtime error, or the user requests a pause.
- **Step** completes pending phases and then executes one VM instruction.
- **Reset** recreates the stopped VM from the completed executable.
- **Open** can select several `.cl` files at once; each file becomes a source tab and Compile concatenates complete class declarations in tab order. **Add** appends another file without replacing existing tabs.
- **Program input** in the console panel is fed to `IO.in_string` / `IO.in_int` (same as `coolerc a.cl b.cl < input.txt`). Catalog examples load their `input` field when present and otherwise clear the box. Empty input stays empty so existing examples stay deterministic.

Editing the source disposes the current compiler/VM session and restores `Next: Lexer`. Lexer diagnostics still allow the Parser phase to run so recovered tokens and a partial AST can remain visible. Lexer or Parser errors then block Semantic Analysis; semantic errors block IR, assembly, and VM preparation. Blocked phases are shown explicitly instead of exposing future artifacts.

## Run the CLI

```bash
go run ./cmd/coolerc examples/02_factorial.cl
```

Other useful modes:

```bash
go run ./cmd/coolerc -ir examples/02_factorial.cl
go run ./cmd/coolerc -assembly examples/02_factorial.cl
go run ./cmd/coolerc -json examples/02_factorial.cl > result.json
cat examples/01_hello.cl | go run ./cmd/coolerc
go run ./cmd/coolerc program.cl < input.txt
go run ./cmd/coolerc stack.cl atoi.cl < stack.test
```

Several `.cl` files are compiled as one program: class definitions are concatenated in argument order without a module or linking step. When one or more program file paths are given, redirected or piped stdin supplies program input after file arguments for `IO.in_string` / `IO.in_int`. When source comes from stdin (no file argument), stdin is the Cool source and program input is empty.

## Test and build

```bash
make test
make vet
make build
make build-all
```

Before preparing a release, run the complete local gate:

```bash
make release-check
```

The command checks module integrity, whitespace, formatting, tests, vet, JavaScript, examples/resources, and release builds; it also reports platform/tool-specific checks as passed or skipped. See [`docs/release.md`](docs/release.md) for prerequisites and result interpretation. This project intentionally does not use CI.

The same operations without Make are:

```bash
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go vet ./...
go build ./cmd/coolergui
go build ./cmd/coolerc
```

`make build-all` emits browser-fallback GUI binaries and `coolerc` binaries for Linux, Windows, and macOS. To cross-compile the native Windows WebView2 GUI, install MinGW-w64 and run:

```bash
make build-windows-native
```

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
