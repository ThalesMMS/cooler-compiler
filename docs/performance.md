# Local performance engineering

Cooler treats performance data as local engineering evidence, not as a portable
release promise. CPU scheduling, thermal state, toolchain, OS, architecture, and
background work all affect results. The benchmark baseline records the commit,
Go toolchain, platform, processor, run options, deterministic workload sizes,
and every raw sample so comparisons remain reviewable.

## Benchmark coverage

The Go suite covers lexing, parsing, class-table construction, expression
semantics, conformance/LUB queries, IR lowering, bytecode verification, CFG
generation, assembly formatting, complete single-source and project builds,
and JSON serialization. VM workloads cover dispatch, arithmetic, calls and
recursion, static/dynamic dispatch, allocation and initialization, deep `case`,
String built-ins, IO, minimized/retained traces, full/light snapshots, reset,
and replay. HTTP benchmarks exercise compile, incremental pipeline, batch VM,
and payload serialization paths. A Node benchmark measures the actual client
helpers used to render ASTs, merge trace deltas, build reports, and serialize
large result payloads.

All generators in `internal/benchwork` are deterministic and bounded. The
standard project sizes are small (2 classes), medium (32), and large (200),
with separate deep-inheritance, nested-expression, long-text, long-running VM,
and trace-heavy inputs. Benchmarks report input/artifact counts alongside
`ns/op`, `B/op`, and `allocs/op` where applicable.

## Commands

Run the complete suite three times (the default):

```sh
make benchmark
```

Save reviewed raw samples under the controlled baseline directory:

```sh
make benchmark-save BENCH_NAME=my-machine-before
```

Only letters, numbers, `.`, `_`, and `-` are accepted in a saved name, so this
workflow cannot escape `benchmarks/baselines/`. A run without `benchmark-save`
writes JSON to standard output and does not create repository files.

Compare medians without turning normal machine variance into a gate:

```sh
make benchmark-compare \
  BASE=benchmarks/baselines/v0.2-initial.json \
  NEW=benchmarks/baselines/my-machine-before.json
```

Changes above `BENCH_THRESHOLD` (10 percent by default) in time or allocation
metrics are marked `REGRESSION?`. The comparison remains advisory and exits
successfully; a flagged result must be reproduced and profiled before changing
code or accepting a regression. Added and removed measurements are explicit.

The release check runs only `make benchmark-smoke`: one bounded operation per
Go benchmark and two iterations per web benchmark. It catches broken workloads
and benchmark infrastructure without making release correctness depend on
timing or running the full performance suite in CI. This project does not use
CI for performance measurements.

## Profiles

Create both CPU and heap profiles for one allowlisted workload:

```sh
make profile PROFILE_TARGET=compile-large
make profile PROFILE_TARGET=vm-trace
make profile PROFILE_TARGET=api-compile
```

`PROFILE_TIME` defaults to `3s`; `PROFILE_DIR` defaults to `dist/profiles`.
The script keeps the matching test binary beside both `.pprof` files (so pprof
retains exact symbols), prints their paths and a `go tool pprof` command, and
does not write elsewhere. It rejects unknown targets and malformed durations
rather than interpolating an arbitrary benchmark command.

## Interpreting variance

Compare results only on the same machine and with the same options. Prefer the
median of the three raw samples. Re-run a noisy benchmark after closing other
heavy applications; inspect both absolute values and input/artifact metrics to
catch accidental workload drift. Source-unit names and tab order are metadata:
they may change output ordering, but should not materially change asymptotic
cost. Scaling benchmarks at 8, 32, 128, and 256 classes expose nonlinear growth.

## Initial v0.2 baseline

[`v0.2-initial.json`](../benchmarks/baselines/v0.2-initial.json) records 222 raw
samples from three complete repetitions at commit `b0a7d9133e8c`, using Go
1.27.0 on macOS arm64 / Apple M4 Pro (`100ms` Go benchtime and 100 web
iterations). The max-minus-min `ns/op` spread relative to the median was below
8% for all but four measurements. Full VM snapshot creation was the noisiest at
42.2%; small lexing reached 18.6%, small compile API 15.5%, and large project
compilation 10.1%. These are recorded outliers, not accepted regressions, and
must be rerun before drawing conclusions. Large complete compilation varied
7.3%, while all browser measurements stayed within 3.3%.

The 64-class project median was 7.236 ms with stable source metadata, 7.257 ms
after renaming every source ID/file (+0.3%), and 7.226 ms with reversed unit
order (-0.1%). This confirms within observed noise that names and tab order do
not introduce a separate performance penalty; the scaling series remains the
tool for detecting asymptotic changes.
