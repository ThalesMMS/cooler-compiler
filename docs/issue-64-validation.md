# Issue #64: independent LSP source sets

## Plan and behavior

Extend the existing workspace discovery and analysis pipeline with one shared
source-set filter. Accept root-relative file names and directory prefixes in
initialization options and configuration notifications, apply them before
reading watched/discovered disk sources and before producing analysis
snapshots, and rescan transactionally when the selection changes. Keep inactive
open buffers within existing memory bounds and clear old diagnostics under the
same generation lock used to reject stale publication. Preserve deterministic
URI ordering and the unconfigured whole-workspace default.

The user-facing configuration and classroom example are in [the LSP
guide](lsp.md#independent-exercises).

## Evidence

- Before implementation, `TestSourceSetSeparatesExercisesAndPreservesBuffers`
  failed with four compiled documents and duplicate `Main` diagnostics.
- After implementation, the source-set regression tests passed: independent
  exercises, shared sources, directory/file exclusions, unsaved content and
  document versions, stale diagnostic rejection, old diagnostic clearing,
  watched creation/deletion, closing active and excluded buffers, path
  validation, configuration entry limits, workspace limits, cancellation, and
  transactional rejection.
- Independent code review found a disk/open-buffer byte-accounting issue.
  A regression reproduced rejection of a 60-byte effective project because
  its disk files totaled 120 bytes under a 100-byte limit. The scanner now
  counts retained editor contents first and skips their disk copies; the
  regression passes. Follow-up review found no further concrete defects.
- Windows: `CGO_ENABLED=0 go test ./...`, `go vet ./...`,
  `bash scripts/run-lint.sh`, and `go run ./cmd/cooler-doc-check` passed.
- Windows: `go test -race ./internal/lsp -count=1` passed.
- Debian/WSL: the complete LSP test executable, built with
  `CGO_ENABLED=0 GOOS=linux go test -c`, ran successfully on Linux. The
  distribution has no Go installation, so Windows produced the Linux binary.
- macOS and graphical editor integration were not exercised for this change.
  Initialization/configuration decoding and language-server analysis were
  exercised by the Go tests; these are not claims of application validation.
