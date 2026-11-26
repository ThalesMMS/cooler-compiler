# COOL language server

`cooler-lsp` is a local Language Server Protocol 3.17 process for `.cl` and
`.cool` files. It uses the stable public Go compiler facade in semantic-check
mode; it does not import internal compiler packages or introduce a
second COOL type checker.

```bash
go run ./cmd/cooler-lsp
```

The default transport is LSP JSON-RPC over stdin/stdout. Stdout is reserved for
framed protocol messages. Operational logs and `--version` output go to stderr.
The command does not open a socket, make network requests, or add a hosted
service.

## Protocol and capabilities

The server implements:

- `initialize`, `initialized`, `shutdown`, and `exit`;
- incremental UTF-16 `didOpen`, `didChange`, and `didClose` synchronization;
- versioned `publishDiagnostics` for input, lexer, parser, and semantic stages;
- document/workspace symbols, hover, definition, references, completion,
  semantic tokens, and safe quick-fix code actions;
- `$/cancelRequest`, stale document-version rejection, watched file
  creation/change/deletion, and configuration-triggered reanalysis.

Diagnostics preserve stable `COLdddd` codes, severity, source-unit URI,
related cross-file locations, and hints. Ordinary edits use
`compiler.ModeCheck`; IR, optimizer, codegen, and runtime diagnostics are not
produced. A quick fix is exposed only when the compiler supplies an exact
source span and replacement. Currently that includes focused duplicate-class
renames.

Symbols cover classes, methods, attributes, formals, `let` bindings, and case
variables. Resolution uses typed AST nodes and symbols from the public API:
lexical bindings shadow attributes, inherited members resolve to their
declaring source, implicit dispatch selects the effective override, explicit
receivers use their static type, and `SELF_TYPE` hover text names its receiver
context. Built-in hovers and completion carry concise documentation but have no
fake filesystem definition.

Semantic tokens classify classes, methods, attributes, parameters, local/case
variables, types, keywords, strings, numbers, nested and line comments, and
operators. Declaration, readonly, and default-library modifiers are emitted
where applicable.

## Workspace policy

Warning rules and their shared configuration are documented in the
[compiler warnings guide](warnings.md).

COOL has no module or import system. The server therefore compiles one ordered
project. With no source-set configuration, the compatible default is:

1. every `.cl`/`.cool` file below each initialized workspace root is discovered;
2. open editor buffers replace the corresponding disk content;
3. unsaved files outside a root participate while open;
4. source units are sorted by normalized file URI, which deterministically
   controls declaration and diagnostic order without creating namespaces.

Watched create/change/delete notifications update closed disk documents. Open
buffers always win until `didClose`, when the current disk file is reloaded or
the removed document leaves the project. Renames are the client's normal
delete/create pair. Configuration changes rescan sources and invalidate the analysis cache.
File URI paths are decoded exactly once. Windows UNC authorities are preserved
and round-trip canonically; a non-local authority is rejected on Unix instead
of being silently reinterpreted as a local path.

### Independent exercises

Select an exercise with `initializationOptions.sourceSet` in the initialize
request, or send `workspace/didChangeConfiguration` with this payload:

```json
{
  "settings": {
    "cooler": {
      "sourceSet": {
        "include": ["exercises/01/", "shared/"],
        "exclude": ["exercises/01/solutions/"]
      }
    }
  }
}
```

For example, keep separate `Main` classes in `exercises/01/main.cl` and
`exercises/02/main.cl`, and reusable classes in `shared/`. Change the include
entry to `exercises/02/` to select the second exercise. Paths are relative to
each initialized workspace root and use `/` on every platform. A trailing `/`
selects a directory recursively; other entries select an exact file. Matching
is case-sensitive, exclusions win across all roots, and glob syntax, absolute
paths, `.` and `..` components are rejected. Empty includes select every COOL
source under the roots. A null or omitted source set restores the default,
including open files outside the roots. An explicit set excludes outside files.

The filter applies to discovery, watched files, and every language feature's
analysis snapshot. Open buffers outside the active selection are retained and
continue receiving edits, but do not contaminate the selected compilation.
Switching back uses their current unsaved contents and versions. Closing an
excluded buffer removes it from memory; opening or editing one does not switch
exercises automatically. Old exercise diagnostics are cleared on a successful
switch, and stale analysis cannot republish them.

Configuration permits at most 256 include/exclude entries combined, each at
most 4096 bytes. Existing scan time, filesystem-entry, file, and byte limits
still apply; retained inactive buffers count against memory limits. Invalid,
cancelled, unreadable, or over-limit reconfiguration is rejected transactionally
and logged on stderr, keeping the previous selection and editor buffers. No
configuration files, modules, imports, or hosted services are introduced.

The complete typed project must be reconsidered after a content change because
one class can affect inheritance and dispatch everywhere. The public facade
exposes immutable results rather than mutable parser/class-table state, so a
changed generation is compiled afresh for correctness. Repeated diagnostics,
hover, symbol, completion, and navigation requests share that generation's
compiled result and semantic index instead of recompiling per request. No
private or unsafe compiler object crosses the public API boundary.

## Debounce, cancellation, and stale results

Edit notifications cancel the previous debounce/analysis and schedule a new
analysis after 75 ms. Compilation has a five-second default deadline. A result
is cached or published only when its workspace generation and open-document
versions still match. Published diagnostics include the LSP document version,
so clients can reject an older notification even if a new edit is already in
transport.

Requests are independently cancellable. A cancellation received just before
request dispatch is remembered, avoiding a scheduler race. Panics are recovered
at each request/notification boundary and logged to stderr; they do not escape
and terminate the editor process.

At most 64 ordinary requests execute concurrently by default; excess requests
receive a bounded JSON-RPC server error instead of creating another goroutine.
Active request IDs must be LSP strings or integers whose encoded form is at
most 256 bytes, duplicate active IDs are rejected, and at most 256 early
cancellations are retained. `shutdown` cancels and drains earlier work before
its response, invalidates pending diagnostics, and ignores later notifications
except the required `exit`. Document/configuration notifications are likewise
ignored until initialization has completed.

A lexer or parser error is retained with its own source URI. Lexing/parsing is
performed for every source unit before the normal compiler barrier, so one
malformed file does not erase independent frontend diagnostics in another.
Semantic analysis resumes after those syntax errors are corrected.

## Resource limits

Defaults are deliberately local and bounded:

| Resource | Default |
| --- | ---: |
| JSON-RPC payload | 4 MiB |
| Header bytes | 16 KiB |
| Encoded request ID | 256 bytes |
| Concurrent ordinary requests | 64 |
| Remembered early cancellations | 256 |
| Incremental changes per notification | 4,096 |
| Workspace roots | 32 |
| Filesystem entries visited at initialization | 100,000 |
| Open documents | 256 |
| Workspace COOL files | 1,024 |
| One source file | 4 MiB |
| Combined workspace source | 32 MiB |
| Initial workspace scan | 5 seconds |
| One analysis | 5 seconds |

The command flags `--max-message-bytes`, `--max-open-documents`,
`--max-workspace-files`, `--max-workspace-roots`, `--max-content-changes`,
`--max-workspace-entries`, `--workspace-scan-timeout`, and
`--analysis-timeout` lower or raise the operational limits. Compiler/VM limits
remain governed by the public compiler API; the LSP never executes user
programs.

Malformed framed JSON receives JSON-RPC `-32700` and the process continues.
Invalid requests/params and unknown methods receive the standard errors.
Malformed, duplicate, or oversized framing terminates the stdio session because
safely finding the next frame would require trusting an attacker-controlled
header or body length. Header input is rejected while it is read, before an
unbounded line can be accumulated.

Workspace discovery, disk reload, and watched-file reads enforce their byte
ceilings while reading rather than trusting a prior file size, and reject
non-regular source paths such as devices, pipes, and symlinks. One watched-file
batch retains at most the configured workspace file count and combined source
bytes, with duplicate URIs collapsed to the final event. Open, changed,
reloaded, and watched documents also preserve the combined retained-source
ceiling across batches. An incremental edit that exceeds its change-count
ceiling marks the buffer desynchronized and requires a bounded full-text update
before analysis resumes.
