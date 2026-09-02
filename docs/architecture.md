# Architecture

Related canonical references: [stack IR](stack-ir.md),
[VM runtime](vm-runtime.md), [debugger/session protocol](debugger-protocol.md),
[multifile semantics](multifile.md), and the
[artifact contract](artifact-contract.md).

Cooler Compiler Lab separates the desktop and browser interface from a self-contained compiler pipeline and VM. Each language-dependent stage implements COOL semantics directly.

```text
Native WKWebView / browser
            |
            v
      local JSON API
            |
            v
Lexer -> Parser -> Type checker -> Stack IR -> Verifier -> Opt? -> Verifier -> Assembly -> VM
  |        |             |             |          |         |         |          |       |
tokens   typed AST    class table    bytecode  invariants  passes  invariants   text   trace/frames
```

## Packages

```text
cmd/coolergui/
    Starts a loopback-only HTTP server, serves the embedded UI, owns bounded
    debug sessions, and opens either WKWebView on macOS or a browser fallback.

cmd/coolerc/
    Reads COOL source from a file or stdin and exposes normal, IR, assembly,
    and complete JSON output modes.

cmd/cooler-lsp/ and internal/lsp/
    Expose a bounded stdio JSON-RPC language server. Workspace analysis uses
    the public compiler facade; the transport/index layer owns LSP versions,
    cancellation, URI ordering, lexical scope/member lookup projections, and
    editor response shapes.

compiler/
    Stable public Go facade. It owns versioned value projections, immutable
    results, context-aware compile/incremental operations, and a serialized
    debugger contract without exposing implementation objects.

internal/compiler/
    Contains all language and runtime logic. It has no dependency on HTTP,
    WebView, or filesystem APIs.

internal/web/
    Embeds static HTML, CSS, JavaScript, and the SVG icon with go:embed.

examples/
    Embeds one canonical catalog used by the GUI and integration tests.
```

## Compilation contract

The supported `compiler.Compile(ctx, source, options)` public facade returns an
immutable result whose detached `Report` contains:

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

The CLI's non-interactive flow and the GUI's stateless compile endpoint consume
this facade. Their advanced incremental HTTP session transport remains an
internal adapter over the same implementation. Public API stability,
concurrency, error, and deprecation rules are documented in
[`public-go-api.md`](public-go-api.md).

## Incremental compilation sessions

`CompilationSession` is the single orchestration path used by incremental requests, `Compile`, and `NewDebugSession`. It retains tokens, the internal AST, semantic analysis, executable bytecode, formatted artifacts, and the prepared VM without serializing those private compiler values.

Each `NextPhase` call performs one transition:

```text
initial -> lexer -> parser -> semantic -> ir -> codegen -> runtime
```

Stack IR materializes bytecode, IR, and control flow. The executable bytecode is
stored independently of its pseudo-assembly rendering; that rendering is created
only during codegen, so assembly is intentionally unavailable during the IR phase.
The optional `basic` optimizer clones the verified baseline, runs a fixed pass
sequence with a verifier barrier after every pass, and retains both artifact
versions with original-instruction provenance. Before Runtime, the codegen
phase again verifies opcode, stack, scope, control-flow,
call, method, span, IR, assembly, and CFG invariants. A `COL51xx` failure blocks
VM construction. Runtime creates a stopped VM with zero executed instructions
only after that barrier succeeds.

Diagnostics are accumulated as compiler results. Lexer recovery can feed Parser, and Parser can retain a partial AST. Lexer or Parser errors form the frontend barrier; semantic errors block the backend; IR and codegen errors block their dependent phases. A terminal session is idempotent and marks every unexecuted dependency as `blocked`.

## Browser application modules

The embedded GUI uses native JavaScript module loading without a build step or
runtime text injection. `app.js` is the browser-delivered entrypoint and loads
`app-bootstrap.mjs`, which reports module or initialization failures in the
visible status and toast regions. The bootstrap starts `app-main.mjs`, while
`app-state.mjs` owns initial application state, session invalidation, and
Run/Pause transitions. Reusable source-file, artifact-export, VM-chunk, and AST
helpers remain in the independently tested `client-utils.js` module.

Every shipped `.js` and `.mjs` file is independently syntax-checked by the
local release gate. The module tests cover successful loading, visible startup
failure, deferred disposal of stale running sessions, and Run/Pause state
transitions. The browser and native WebView load the same embedded entrypoint.

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

Classes and features retain explicit source-order slices alongside lookup maps;
map iteration cannot determine analysis, symbol, or backend order. Parent-chain
validation is iterative. Each detected cycle is reported once in class order,
its member edges recover to `Object`, and all lookup/conformance/distance
operations remain cycle guarded. Duplicate and override diagnostics carry a
related span for the prior declaration.

`SELF_TYPE` is preserved where required and resolved to the current class only
for inheritance operations. Invalid expressions receive the absorbing internal
type `<error_type>`: analysis still visits independent siblings, but conformance
and LUB do not turn one root error into parent-level cascades. Any semantic error
blocks IR and VM preparation.

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

Every full `Result` from the direct, incremental, and debugger APIs carries the
same v1 report identity as `coolerc emit report`: schema/compiler version,
optional commit, host target, deterministic VM options, and ordered source-unit
digests. Light VM deltas omit static artifacts and are merged into the most
recent full result by the client. The contract and compatibility rules are
documented in [`artifact-contract.md`](artifact-contract.md).

Sessions are random-ID, in-memory, independently locked, TTL-bound, and LRU-evicted at a fixed capacity. Per-session locking serializes concurrent phase requests so transitions cannot be skipped. A cryptographically random per-process capability is injected only into the uncached initial HTML, removed from the DOM by the entrypoint, retained in JavaScript memory, and required on every state-changing/session request. It is never included in source data, diagnostics, reports, recent files, exported artifacts, or the printed server URL.

The server binds to loopback by default and rejects non-loopback addresses unless the operator supplies `--allow-remote`, which emits an exposure warning. Host and Origin must identify the active listener; an Origin-less mutation still needs the process capability. Strict methods and JSON media types, a 1 MiB body limit, single-value JSON decoding, fixed-format session IDs, bounded breakpoints and VM steps, and read/write/idle/header timeouts constrain requests. Every response carries a script-inline-free Content Security Policy, MIME sniffing protection, no-referrer policy, restrictive browser permissions, and frame denial; API and capability-bearing HTML responses use `Cache-Control: no-store`.
