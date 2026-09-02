# Robustness testing

Cooler's fuzzing and property tests run locally with Go's built-in fuzz engine.
They do not use a hosted service or CI. The routine release gate runs only a
short bounded smoke; longer campaigns are explicit developer actions.

## Commands

```bash
# Run every target for one second with four workers.
make fuzz-smoke

# Run one target for an opt-in duration.
make fuzz TARGET=FuzzCompile DURATION=10m

# Exercise the structured generator and all metamorphic transformations.
make property-test
```

`FUZZ_SMOKE_DURATION` and `FUZZ_WORKERS` tune the smoke locally. `TARGET` must
be an exact supported target and `DURATION` must be a positive integer duration
using `ms`, `s`, `m`, or `h`, so the wrapper does not interpolate arbitrary
commands. `make release-check` uses one second per target and four workers.

## Targets and invariants

| Target | Scope | Principal invariants |
| --- | --- | --- |
| `FuzzLex` | Arbitrary source bytes | Deterministic tokens/diagnostics, ordered UTF-16 spans |
| `FuzzParse` | Recovered token streams | Bounded diagnostics/depth, deterministic partial AST, valid spans |
| `FuzzCompile` | Complete single-source pipeline | Determinism, stage barriers, JSON safety, verified backend, VM step bound |
| `FuzzCompileProject` | One or two ordered source units | The pipeline invariants plus stable source IDs and multifile spans |
| `FuzzBytecodeVerifier` | Valid generated executables plus bounded mutations | No panic, deterministic internal diagnostics, valid locations |
| `FuzzProgramInput` | Source plus arbitrary input lines | Deterministic input parsing/output and VM containment |
| `FuzzJSONRoundTrip` | Complete reports | Supported schema, lossless stable encode/decode/encode |

The shared seed corpus is capped at 128 distinct inputs. Source inputs are
limited to 32 KiB, program input to 8 KiB, verifier mutations to 128 bytes, and
fuzzed VM runs to 512 instructions. Parser depth and diagnostic caps provide
additional frontend bounds. Any input over a target's cap is skipped before
compilation.

Every source-based target shares the same seeds:

- every bundled `examples/*.cl` program;
- representative valid and invalid lexer, parser, semantic, runtime, and
  multifile conformance fixtures;
- Unicode, malformed UTF-8, LF/CRLF, deep nesting, long identifier, string,
  comment, and integer cases;
- reviewed seeds from `internal/compiler/testdata/fuzz-regressions/`.

## Typed generator and metamorphic properties

The test-only generator constructs a bounded typed representation before
printing COOL. Across deterministic seeds it varies hierarchy depth, names,
literals, input, and loop counts while covering attributes, methods, basic and
user types, legal `SELF_TYPE`, `let`, `if`, `while`, `case`, assignment,
dynamic dispatch, static dispatch, allocation, and IO built-ins. Generated
programs have a known `Object` main-body type, deterministic input, expected
output, and terminating loop bound.

The metamorphic suite implements every transformation proposed for v0.2:

- comments and irrelevant whitespace;
- LF to CRLF conversion;
- redundant parentheses;
- consistent alpha-renaming of formals, locals, and case binders;
- splitting classes into ordered source units;
- reformatting while preserving the exact token sequence;
- dynamic-to-static dispatch only for a generated receiver whose target method
  is inherited and provably not overridden;
- a constant-true branch with a type-correct, unreachable `Object` alternative.

Each transformed program must remain diagnostic-free, retain the static type of
`main`, produce identical output, respect the VM limit, and pass backend
verification. Transformations that should not affect lowering also retain the
IR opcode sequence. Source spans may change, but every resulting span must stay
ordered, source-linked, and within the corresponding UTF-16 source length.

## Regression and corpus management

Go keeps generated corpus/cache data outside the repository. Do not copy bulk
or temporary corpora into Git. When fuzzing finds a crash or mismatch:

1. let Go minimize it, then reproduce it with the printed `go test` command;
2. reduce it further when that preserves the same invariant failure;
3. move the reviewed source to
   `internal/compiler/testdata/fuzz-regressions/`;
4. add its file, issue, bug class, and expected invariant to `manifest.json`;
5. run the seed tests, property tests, conformance, and release gate.

The repository ignores `.fuzz-tmp/` and `fuzz-corpus-tmp/`. Go-created failure
files below `testdata/fuzz/` are intentionally not ignored: they must either be
reviewed and promoted as minimized regressions or removed after diagnosis.

## Campaign record

Record pre-release campaigns here with the source revision (or pre-commit
worktree base), toolchain, host, duration per target, worker count, and result.
Routine one-second release smokes are not a substitute for that longer record.

| Date | Source | Toolchain/host | Campaign | Result |
| --- | --- | --- | --- | --- |
| 2026-08-31 | #31 worktree based on `af03b69` | Go 1.27.0, darwin/arm64 | All 7 targets, 10s each, 4 workers | PASS; 1,729,693 executions, no crash or mismatch |

The preceding one-second smoke found and minimized two defects before this
campaign: a zero-span recovered Program AST and a verifier diagnostic that
echoed a corrupted bytecode span. Both fixes have permanent regression inputs.

## VM resource-bound suite

`go test ./internal/compiler -run 'VM|Adversarial|Trace|Reset'` exercises every
policy field at `limit-1`, `limit`, and `limit+1`, plus recursive calls, rapid
allocation, repeated concatenation, large output/input, infinite loops,
transactional underflow/return failures, cancellation, cyclic object displays,
trace offset/delta eviction, runtime-error reset, and repeated deterministic
run/reset cycles. `BenchmarkVMTraceRetention` and `BenchmarkVMObjectAllocation`
measure the bounded trace and allocation hot paths. These tests use small
policies so the normal suite stays fast; the release gate supplies race, fuzz,
conformance, native-smoke, and packaging coverage.

## Session stress

`make session-stress` runs the GUI session lifecycle suite repeatedly under the
Go race detector. It uses channels and acquisition hooks instead of timing-only
sleeps to exercise same-session serialization, unrelated-session progress,
pipeline complete/next races, VM run/step/reset/input/dispose races, atomic
promotion, active TTL/LRU behavior, cancellation, response failure, shutdown,
ID collisions, deterministic randomized model sequences, and cleanup. The
browser tests additionally execute 1,000 rapid compile/edit/run invalidations
and prove that an obsolete ID/epoch cannot restore state.

The full acceptance run is `go test -race -count=20 -timeout=30m ./...`. The
explicit timeout is necessary because repeating the differential conformance
subprocess suite twenty times exceeds Go's default ten-minute package timeout;
it does not relax any per-process safety limit.
