# `coolerc` command-line contract

`coolerc` separates execution, validation, and artifact production:

```text
coolerc run [options] [file.cl ...]
coolerc check [options] [file.cl ...]
coolerc test [--json] exercise.json
coolerc emit tokens|ast|symbols|cfg|ir|assembly|report [options] [file.cl ...]
coolerc version [--json|--verbose]
```

The cross-surface [program input contract](input.md) is normative. Without a
source file, COOL source is read from stdin. With source files, stdin
remains program input for `run`; `run --input FILE` takes precedence.
`run --interactive FILE...` reserves stdin for live, line-oriented program
input and streams output before each read, so prompts are visible while the VM
waits. Interactive mode requires at least one source file and is mutually
exclusive with `--input`; ordinary files and redirected stdin keep their
existing pre-supplied behavior when the flag is absent. Ctrl-D applies the
deterministic EOF values (`""` and `0`) to the pending and later reads. Ctrl-C
interrupts the process using the host shell's normal exit/signal convention and
does not inject a value into the program.
`--max-steps N` accepts 1 through 1,000,000 and must not exceed the selected
policy's `maxInstructionsPerRun`. `check` and `emit` complete compiler
preparation but never execute a VM instruction.

All compile commands accept `--vm-policy FILE`. The file is one JSON object,
smaller than 64 KiB, with only the fields listed in
[`vm-runtime.md`](vm-runtime.md#deterministic-limits). Omitted or zero fields use
safe defaults; invalid values fail with exit 2. For example:

```json
{
  "maxInstructionsPerRun": 200000,
  "maxInstructionsTotal": 400000,
  "maxOutputBytes": 8388608
}
```

All compile commands also accept `--opt=none|basic`; `none` is the default and
reference path. `basic` runs the conservative verified educational pass set
described in [`optimization.md`](optimization.md). For `emit ir`,
`--emit-ir=before|after|both` selects the unoptimized and/or execution IR, and
`--explain-optimizations` appends pass statistics, transformations, and refused
opportunities. Those two inspection flags are rejected for other commands.

All commands accept `--diagnostic-format text|json` and
`--color auto|always|never`. The current text renderer emits no ANSI color.
JSON diagnostics contain `file`, `line`, `column`, `code`, `stage`, `severity`,
`message`, optional `hint`, `span`, and `related` locations. Spans use the
originating source unit's stable `sourceId` and file-local coordinates.

Exit codes are stable:

The `test` command has [its own manifest and result contract](program-tests.md):
expected errors can pass, and exit `1` means a test mismatch. The following
compiler/runtime exit descriptions apply to ordinary compilation commands.

- `0`: success;
- `1`: compiler or runtime error, including an exhausted step budget;
- `2`: command-line usage or invalid option combination;
- `3`: source/input read, output, or internal tool failure.

The v0.1 forms remain supported: `coolerc file.cl`, `-json`, `-ir`, and
`-assembly`. These legacy emission flags retain their v0.1 VM execution
behavior; new `emit` commands are check-only. New scripts should use explicit
subcommands.

`emit report` and legacy `-json` use the same versioned complete-report contract
as the GUI and HTTP API. Top-level `schemaVersion`, `compilerVersion`, optional
`compilerCommit`, compiler-host `target`, deterministic `options`, and ordered
`sourceUnits` metadata identify the ordered native inputs. Multifile reports
leave the legacy aggregate `source` field empty. See
[`artifact-contract.md`](artifact-contract.md) and the machine-readable schema
in [`schemas/compile-report-v1.schema.json`](../schemas/compile-report-v1.schema.json).
Schema 1.4 reports include the selected optimization mode, before/after IR and
CFG, verifier outcomes, pass contracts, and original-instruction mappings.

`coolerc --version` prints the short version without reading source.
`coolerc version --json` includes the commit when Go build metadata or an
`-X main.commit=...` linker value is available, plus Go version and target. A
build date is omitted unless reproducibly supplied with `-X main.buildDate=...`.
