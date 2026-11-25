# Local LSP latency baseline (#77)

`BenchmarkLSPEditSession` drives initialized framed JSON-RPC sessions through an
in-process pipe and recording transport. It includes notification parsing,
document mutation, background scheduling, analysis, protocol serialization and
client receipt. It excludes process startup, editor rendering and OS stdio
transport. This is a measurement harness, not an interactive editor validation.

Two controlled workspaces contain 2 files / 152 initial source bytes and
33 files / 3,274 bytes. Helper methods contain 32 discarded literals and a
result. Sources stay valid; diagnostic publications contain empty arrays.
Revision comments add a few bytes during editing. Each measured operation is:

| Scenario | Operation and completion |
| --- | --- |
| typing | Four versioned full-text edits; receive the final main-file diagnostics |
| multifile | Edit a helper plus the main-file burst; receive final diagnostics |
| hover | Main-file burst and three concurrent hover requests; await all responses and diagnostics |
| cancel | Occupy analysis slot, await request queue entry, cancel over JSON-RPC, await cancellation before releasing slot |
| cache | Hover on unchanged workspace after warmup; no compilation allowed |

Warmup and initialization are excluded. The harness uses a 2 ms debounce to
keep runs short; production defaults to 75 ms. Each session has a 30-second
deadline and bounded transport queues. Every publication is checked against
the document version **at write time under the server lock**. Editing workloads
also attempt to publish the pre-burst analysis, exercising stale-generation
rejection while normal analysis and requests compete. Correctness failures
fail the benchmark; latency variance does not.

## Reproduce and compare

```sh
CGO_ENABLED=0 go run ./cmd/cooler-bench run \
  -bench '^BenchmarkLSPEditSession$' -benchtime=10x -count=3 \
  -web-iterations=1 -name lsp-local -output dist/lsp-local.json
go run ./cmd/cooler-bench compare \
  -base benchmarks/baselines/issue77-windows.json -new dist/lsp-local.json
```

The existing command also includes its four small web helpers per sample.
LSP measurements have package `github.com/ThalesMMS/cooler-compiler/internal/lsp`.
Commit, toolchain, OS/architecture, CPU, options, source sizes and metrics are
saved using the existing baseline format. `workingTreeDirty` flags tracked
uncommitted changes. The checked-in [baseline](../benchmarks/baselines/issue77-windows.json)
was recaptured from clean commit `2db6e0a` (no tracked uncommitted changes), Go 1.26.4, Windows/amd64, Intel i7-14700K.

Each phase and end-to-end operation reports mean, population standard deviation,
p50, nearest-rank p95 and sample count. Phase probes separate debounce, analysis
token queue, compile, report conversion, semantic index, diagnostic conversion,
JSON serialization and publication write. Phase values are per occurrence,
**not additive**: concurrent requests overlap and one operation publishes
multiple files. Missing compilation metrics in cache/cancel mean no compilation
occurred, not a measured zero-duration compilation.

Go `B/op` and `allocs/op` include the local client/transport and probes as well
as server work. Endpoint heap sizes and process-wide total allocated bytes are
also recorded; they are neither server-exclusive nor peak/resident memory.
GC and warmup retention affect these values. Phase distributions retain the
first 8,192 occurrences; use the fixed-operation command above to avoid this
cap. Windows measurements use QueryPerformanceCounter, following Go's own
`testing/testing_windows.go` rationale: ordinary `time.Time` can round short
intervals down to zero. Other platforms use Go's monotonic clock.

## Observed first sample (10 operations)

All durations below are milliseconds; the artifact includes all three samples.

| Files | Scenario | End-to-end mean | p95 | Std. dev. |
| --- | --- | ---: | ---: | ---: |
| 2 | typing | 4.634 | 4.848 | 0.193 |
| 2 | multifile | 5.171 | 10.768 | 1.873 |
| 2 | hover | 2.644 | 3.342 | 0.339 |
| 2 | cancel | 0.062 | 0.186 | 0.055 |
| 2 | cache | 0.028 | 0.051 | 0.011 |
| 33 | typing | 58.948 | 70.131 | 3.814 |
| 33 | multifile | 58.892 | 61.728 | 1.530 |
| 33 | hover | 56.647 | 58.329 | 1.007 |
| 33 | cancel | 0.042 | 0.084 | 0.017 |
| 33 | cache | 0.039 | 0.145 | 0.037 |

In this run, 33-file typing averaged 44.850 ms in compilation. Concurrent hover
averaged 41.637 ms waiting for the analysis slot. Requests can start compilation
before debounce expires, explaining why the small concurrent scenario can
finish before debounced typing. These are local observations, not portable
thresholds or proof that an incremental compiler is warranted. Reproduce under
the same clock, toolchain and workload, then profile compile/report phases
before proposing performance changes.

## Validation

The short full Go suite, vet, pinned lint and documentation checks passed on
Windows. Scenario correctness tests passed three times under the race detector
and executed successfully in Debian/WSL. Independent review caught and verified
the fix for an early-cancellation/queued-cancellation measurement ambiguity.
Native macOS execution was unavailable. No hours-long suite was run.
