# Public Go API

Educational diagnostics can be selected with `Options.WarningRules`; see
[compiler warnings](warnings.md) for codes, defaults, and suppression.

The supported import path is:

```go
import "github.com/ThalesMMS/cooler-compiler/compiler"
```

`internal/compiler` is an implementation package. External tools must not copy,
reflect on, or depend on its AST, class tables, executable instructions, VM,
maps, mutexes, or pointer identity.

## Compatibility policy

The public facade has three independently versioned surfaces:

- `compiler.APIVersion` uses semantic versioning. The v1 package keeps source
  compatibility for documented exported identifiers. New methods, value
  fields, and optional capability interfaces may be added in a minor release.
- `compiler.ReportSchemaVersion` versions serialized compilation/debugger
  results. Its compatibility policy is defined by the
  [artifact contract](artifact-contract.md). Consumers should reject an
  unsupported schema instead of guessing.
- diagnostic codes are the stable machine-readable identity of COOL failures
  and follow the separate [diagnostic policy](diagnostics.md). Message wording
  is explanatory text, not an API enum.

`Result` accessors, compile functions, `Compilation`, the `Debugger` core
interface, and the artifact types returned by those accessors are stable.
The `Report.Optimization` and `Report.Visualizations` raw payloads are
experimental schema extensions; use their versioned JSON fields rather than
assuming a Go layout. `HeapInspector`, `WatchController`, and
`ReverseDebugger` are optional additive capabilities. Code that needs one can
assert the corresponding interface without making it a requirement for every
debugger implementation.

An exported stable identifier is deprecated in its Go documentation for at
least one minor release before removal in a future major version. The
`compiler/compatibility/v1` compile fixture pins the first stable surface.

## Compilation

`DefaultOptions` selects bounded execution without optimization. Options with
zero-valued limits are normalized to safe desktop defaults; invalid modes,
limits, optimization names, project identities, and nil contexts return Go
errors. COOL lexical, parse, semantic, verifier, and runtime failures remain
structured diagnostics in `Result`.

```go
options := compiler.DefaultOptions()
result, err := compiler.Compile(ctx, source, options)
if err != nil {
    // invalid API use, cancellation, or an internal host failure
}
for _, diagnostic := range result.Diagnostics() {
    // COOL errors and warnings
}
```

Set `Options.Mode = compiler.ModeCheck` to stop after semantic analysis. Use
`CompileProject` with ordered `SourceUnit` values for native multifile
compilation. IDs must be unique and remain the source identity in diagnostics,
tokens, AST views, symbols, IR, CFG, assembly spans, runtime traces, and
debugger positions.

`LoadProject` and `CompileFiles` are optional filesystem helpers. They preserve
argument order and wrap filesystem failures with `ErrIO`; callers that already
own source text should prefer `SourceUnit` so the compiler performs no I/O.

Results are immutable and safe for concurrent readers. Every accessor and
`Report()` returns a detached copy; changing one copy never affects a later
read or another goroutine. `json.Marshal(result)` emits the normal report.
`DecodeResult` validates compatible v1 reports, upgrades the legacy
unversioned form using the implementation's migration rules, and rejects a
different schema major with `ErrUnsupportedSchema`.

## Incremental compilation

`NewSession` creates an idle `Compilation`. `Next` advances one phase and
`Complete` advances to the mode's terminal phase or a diagnostic barrier.
Every long-running entry point accepts `context.Context`. Cancellation is
checked before and after compiler phases and continuously during VM execution.

`Compilation` methods are concurrency-safe and serialized. `Close` is
idempotent; subsequent operations return `ErrClosed`. A completed run-mode
session can transfer its stopped VM once with `Debugger`, avoiding
recompilation and preventing two public owners from mutating the same VM.

## Debugger

`NewDebugSession` compiles a project and returns a stopped VM. The stable core
supports instruction stepping, source `Into`/`Over`/`Out`, run-to-cursor,
bounded run, breakpoints, reset, snapshots, pre-supplied and interactive input,
and disposal. `SetInteractiveInput`, `ProvideInputLine`,
`FinishInteractiveInput`, and `InputEvents` expose the live-input protocol.

All debugger operations are serialized. `Pause` and `Close` are the deliberate
exceptions: another goroutine may call them while `Run` or source stepping is
active. They cancel that operation, which returns a stopped snapshot together
with `context.Canceled`. Snapshot values are detached from the VM.

Breakpoint/source positions use 1-based lines and columns; zero column means
any executable construct on a line where documented. Invalid positions and
limits are Go errors. A source position that is valid but not executable
returns `ErrNoExecutablePosition`.

Heap, watch, and reverse execution are exposed through optional capability
interfaces:

```go
if heap, ok := debugger.(compiler.HeapInspector); ok {
    snapshot, err := heap.Heap(ctx, compiler.HeapQuery{Mode: "roots"})
    _ = snapshot
    _ = err
}
```

Heap data is bounded and read-only. Watch expressions are side-effect-free.
Reverse execution retains a bounded timeline and may return
`ErrReverseUnavailable` after eviction.

The LSP is a separate process API rather than part of this Go facade. Embedder
code should start `cmd/cooler-lsp` and speak the documented JSON-RPC transport;
see the [LSP guide](lsp.md) for lifecycle, cancellation, and workspace limits.

## Examples

Package documentation provides executable examples for source and multifile
compilation, incremental phases, detached artifacts, debugger stepping, input,
heap/watch/reverse capabilities, cancellation, and explicit disposal.
