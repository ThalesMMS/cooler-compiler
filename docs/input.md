# Program input contract

This document is the canonical v1.0 contract for program input across the CLI,
GUI, public Go API, and debugger. Input is always line-oriented, bounded by the
selected VM policy, and separate from COOL source text.

## Non-overlapping input modes

| Surface | COOL source | Program input | Interaction policy |
| --- | --- | --- | --- |
| `coolerc` with no source file | stdin, through EOF | empty by default; `run --input FILE` is the distinct deterministic input channel | `--interactive` is rejected because stdin belongs to source |
| `coolerc FILE...` | ordered files | redirected/piped stdin, or `run --input FILE` | finite pre-supplied lines; a terminal stdin is not read implicitly |
| `coolerc run --interactive FILE...` | ordered files | terminal stdin | reads one line only after `waiting-input`; mutually exclusive with `--input` |
| GUI | ordered editor tabs | Program input field, then submitted live lines | preloaded lines are consumed first; the live form appears only at `waiting-input` |
| public Go compile/session API | `Project.Sources` | `Options.Input` or `SetInput` | deterministic unless `SetInteractiveInput(true)` is explicitly selected |

For file-backed CLI runs, `--input FILE` takes precedence over redirected stdin.
Source-from-stdin plus `--input FILE` is unambiguous: stdin remains source and
the named file is program input.

## Line and EOF semantics

Each `IO.in_string` or `IO.in_int` consumes exactly one line. The input
transport removes its terminating LF or CRLF; `in_string` preserves the
remaining content, including whitespace and embedded NUL. Deterministic exhaustion and
explicit interactive EOF return `""` from `in_string` and `0` from `in_int`.

`in_int` trims surrounding whitespace, accepts a signed decimal from
`-2147483648` through `2147483647`, and returns `0` for empty, invalid, or
out-of-range text. Output already produced before an input boundary remains in
order. Cooler itself emits no terminal prompt: a prompt is ordinary program
stdout written before the read. Diagnostics and tool errors remain on stderr,
including JSON diagnostics, so they do not enter program output.

In explicit CLI interaction, Ctrl-D applies EOF to the pending and later reads.
Ctrl-C follows the host terminal's normal interrupt/exit convention and does
not inject a COOL value or diagnostic into the running program.

## Waiting, reset, disposal, and replay

When interactive input is exhausted, the VM stops before consuming the input
opcode. Repeated Run requests preserve the instruction pointer, executed count,
stack, preceding output, and input-event count. `waitingInput` identifies the
builtin, source location, input position, and monotonically numbered request.
Exactly one bounded line with the current sequence executes that pending opcode;
stale, duplicate, oversized, and out-of-state submissions fail without changing
the request.

Reset rewinds the configured preloaded lines and clears live lines and input
events. Session cancellation and disposal serialize with Run and input delivery;
closed or replaced sessions cannot accept a later line. The GUI renders
submitted-input history separately from program stdout and clears that history
on reset.

Reverse execution replays previously recorded input without asking again until
it reaches a live boundary. Supplying a different line after reversing discards
the former future, increments the timeline revision, and reports
`futureDiscarded`. Input-event contents are sensitive: normal snapshots expose
only a count, metadata can be requested with redacted text, and content requires
an explicit privileged request.

The executable conformance scenarios are linked to `BUILTIN-IO` and
`TOOL-INTERACTIVE-INPUT` in the
[normative matrix](../internal/compiler/testdata/conformance/matrix.json).
