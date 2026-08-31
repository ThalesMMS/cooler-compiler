# Architecture

Cooler Compiler Lab separates the desktop and browser interface from a self-contained compiler pipeline and VM. Each language-dependent stage implements COOL semantics directly.

```text
Native WKWebView / browser
            |
            v
      local JSON API
            |
            v
Lexer -> Parser -> Type checker -> Stack IR -> Pseudo-assembly -> VM
  |        |             |             |             |             |
tokens   typed AST    class table    bytecode       text      trace/frames
```

## Packages

```text
cmd/coolergui/
    Starts a loopback-only HTTP server, serves the embedded UI, owns bounded
    debug sessions, and opens either WKWebView on macOS or a browser fallback.

cmd/coolerc/
    Reads COOL source from a file or stdin and exposes normal, IR, assembly,
    and complete JSON output modes.

internal/compiler/
    Contains all language and runtime logic. It has no dependency on HTTP,
    WebView, or filesystem APIs.

internal/web/
    Embeds static HTML, CSS, JavaScript, and the SVG icon with go:embed.

examples/
    Embeds one canonical catalog used by the GUI and integration tests.
```

## Compilation contract

`compiler.Compile(source string) Result` returns one serializable object containing:

- original source;
- stage summaries;
- tokens with UTF-16-compatible source spans;
- typed AST;
- class, method, attribute, formal, `let`, and `case` symbols;
- diagnostics with hints, related spans, and optional quick fixes;
- stack IR and pseudo-assembly;
- control-flow blocks and edges;
- VM trace, output, operand stack, and call frames;
- aggregate metrics.

UTF-16 offsets intentionally match JavaScript string indexes. Clicking an artifact can therefore select the exact range in the source editor without an offset conversion layer.

## Incremental compilation sessions

`CompilationSession` is the single orchestration path used by incremental requests, `Compile`, and `NewDebugSession`. It retains tokens, the internal AST, semantic analysis, executable bytecode, formatted artifacts, and the prepared VM without serializing those private compiler values.

Each `NextPhase` call performs one transition:

```text
initial -> lexer -> parser -> semantic -> ir -> codegen -> runtime
```

Stack IR materializes bytecode, IR, and control flow. Pseudo-assembly formats that stored bytecode separately, so assembly cannot appear during the IR phase. Runtime creates a stopped VM with zero executed instructions.

Diagnostics are accumulated as compiler results. Lexer recovery can feed Parser, and Parser can retain a partial AST. Lexer or Parser errors form the frontend barrier; semantic errors block the backend; IR and codegen errors block their dependent phases. A terminal session is idempotent and marks every unexecuted dependency as `blocked`.

## Frontend

### Lexer

The scanner handles COOL identifiers, case-insensitive keywords, the special Boolean-literal casing rule, string escapes, line comments, and nested block comments. It reports unmatched terminators, null characters, unterminated strings/comments, and oversized string constants.

### Parser

A hand-written recursive-descent/precedence parser produces a strongly typed internal AST. Assignment is right-associative, comparisons are non-associative, and dispatch binds more tightly than arithmetic. Recovery synchronizes at class and feature boundaries so the GUI can still show useful partial artifacts.

### Semantic analysis

The class table starts with the COOL prelude. Analysis then proceeds in passes:

1. collect classes;
2. validate parents and break inheritance cycles for safe continued analysis;
3. collect attribute and method signatures;
4. validate inherited attributes and exact override signatures;
5. validate `Main.main()`;
6. type-check feature bodies with lexical environments, conformance, and least-upper-bound calculation.

`SELF_TYPE` is preserved where required and resolved to the current class only for inheritance operations.

## Backend and VM

Every user method and a synthetic `$init` method are lowered to stack instructions. Initializers call their parent initializer before evaluating explicit attributes in source order. `new T` allocates defaults and invokes `T.$init`; `new SELF_TYPE` uses the receiver's dynamic class.

A VM frame contains:

- the compiled method and instruction pointer;
- `self`;
- nested lexical scopes;
- the operand-stack base used to restore caller state.

Dynamic dispatch consults the runtime class; static dispatch starts lookup at the explicit type. `case` computes inheritance distance and chooses the nearest matching ancestor branch. The VM implements deterministic limits for instructions, stack size, call depth, and object count.

## Pipeline and debug sessions

The incremental API exposes:

```text
POST /api/pipeline/start
POST /api/pipeline/next
POST /api/pipeline/complete
POST /api/pipeline/dispose
```

`start` creates an ID and executes only Lexer. `next` performs one transition, while `complete` performs all remaining transitions until success or a diagnostic barrier. A successful Runtime transition promotes the same ID to a mutable `DebugSession`; no recompilation is needed before VM actions.

The VM API remains:

```text
POST /api/vm/start
POST /api/vm/step
POST /api/vm/run
POST /api/vm/reset
POST /api/vm/dispose
```

`Compile`, `Run`, and `Step` complete a partial pipeline before their normal action. `NewDebugSession` also uses `CompilationSession`, so complete and incremental flows share artifacts and diagnostics.

Sessions are random-ID, in-memory, independently locked, TTL-bound, and LRU-evicted at a fixed capacity. Per-session locking serializes concurrent phase requests so transitions cannot be skipped. Requests are accepted only from loopback origins, JSON bodies are size-limited, and the server binds to loopback by default.
