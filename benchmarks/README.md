# Reviewed local baselines

This directory contains compact, reviewable output from `cooler-bench`. The
format is defined by
[`benchmark-baseline-v1.schema.json`](../schemas/benchmark-baseline-v1.schema.json)
and the operating procedure is in
[`docs/performance.md`](../docs/performance.md).

Baselines are machine-specific evidence. Never treat a comparison across a
different CPU, OS, architecture, Go toolchain, benchmark options, or workload
metadata as a release gate. Raw samples are retained so medians and variance can
be recomputed without rerunning an old commit.

- [`v0.2-initial.json`](baselines/v0.2-initial.json) — three complete samples on
  Apple M4 Pro / macOS arm64 / Go 1.27.0; variance notes are in the performance
  guide.
