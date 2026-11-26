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

## Course reference formats (opt-in)

These flags are a secondary/test surface for the course-assignment modes; the
GUI exposes the same artifacts (see [`course-assignments.md`](course-assignments.md)).
Default output is unchanged when they are omitted.

### `emit tokens --format=reference` (PA2)

```text
coolerc emit tokens --format=reference [file.cl ...]
```

Prints the course `lexer` token stream instead of Cooler's JSON tokens: a
`#name "file"` header for each file (the path exactly as given on the command
line, or `<stdin>`), then one `#line TOKEN [value]` line per token. Token names
follow the course support code (`TYPEID`, `OBJECTID`, `INT_CONST`,
`STR_CONST`, `BOOL_CONST`, `ASSIGN`, `DARROW`, `LE`, keywords in upper case,
and punctuation as `'{'`). String constants and error messages use the
support code's byte-wise escaping (`\n`, `\t`, `\b`, `\f`, `\\`, `\"`, and
three-digit octal for other non-printable bytes).

Lexical errors become `ERROR "message"` tokens in the stream and the command
exits `0`, as the course lexer leaves halting to the parser. The messages are
`Unterminated string constant`, `EOF in string constant`,
`String contains null character.`, `String contains escaped null character.`,
`String constant too long`, `EOF in comment`, `Unmatched *)`, and the invalid
character itself for any other unexpected byte (a multibyte UTF-8 character
yields one `ERROR` per byte; a leading `_` is one invalid character). Integer
literals are printed as written, without Cooler's 32-bit range diagnostic.

Line numbers are the lexer's line counter after the token is recognized: a
string continued with an escaped newline, and an unterminated string, report
the line on which they end. Only `\n` advances the line in this mode's output
model, matching the support code; Cooler's own spans also count a lone `\r`.
Byte-identity with a particular course binary is checked by the optional
differential harness (`docs/differential.md`) when the user supplies one.

### `emit ast --format=reference` (PA3)

```text
coolerc emit ast --format=reference [file.cl ...]
```

Prints the course `parser` dump of the untyped AST (`lexer files | parser`):
`#line` markers, `_program`, `_class` (name, parent, quoted file name, and the
parenthesized features), `_method`, `_attr`, `_formal`, every expression node
(`_assign`, `_dispatch`, `_static_dispatch`, `_cond`, `_loop`, `_block`,
`_let`, `_typcase`/`_branch`, `_new`, `_isvoid`, `_plus`, `_sub`, `_mul`,
`_divide`, `_neg`, `_lt`, `_leq`, `_eq`, `_comp`, `_int`, `_string`, `_bool`,
`_object`, `_no_expr`), each followed by `: _no_type`. A multi-binding `let`
prints as nested single-binding `_let` nodes, and an implicit-receiver call
prints its `self` object.

All files are lexed in course-compatibility mode and their token streams are
concatenated and parsed by the generated LALR(1) grammar
(`internal/compiler/cool.y`), whichever front-end is selected for ordinary
compilation. That grammar has the course precedence table and the classic
error productions (class, feature, block statement, and let binding), so
error detection, recovery, and node line numbers follow Bison semantics: a
node's line is the line of the first token of the production that built it,
and a `no_expr` placeholder has its enclosing feature's or binding's line.

On a syntax error the command writes the course report to stderr and exits
`1`:

```text
"file.cl", line 2: syntax error at or near '}'
Compilation halted due to lex and parse errors
```

The token after `at or near` is printed like the course parser
(`OBJECTID = name`, `TYPEID = Name`, `INT_CONST = 7`, `STR_CONST = "..."`,
`ERROR = "message"` for a lexical error token, `EOF`, keywords, or a quoted
character). At end of file the line is the last token's line. More than 50
errors stop the report with `More than 50 errors`.

### `emit ast --format=reference --stage=semant` (PA4)

```text
coolerc emit ast --format=reference --stage=semant [file.cl ...]
```

Prints the course `semant` output (`lexer | parser | semant`): the same tree
as the parser dump with every expression's static type in place of
`_no_type` (`: SELF_TYPE`, `: Int`, the least upper bound of `if`/`case`
branches, the body type of each nested `_let`; `_no_expr` stays `_no_type`).
A program with syntax errors prints the parser report; a program with
semantic errors never prints an annotated tree: the semant report goes to
stderr and the command exits `1`.

### `check --diagnostic-format=reference` (PA4)

```text
coolerc check --diagnostic-format=reference [file.cl ...]
```

Reports errors the way the course phases do and prints nothing for an
accepted program. Syntax errors use the parser report above. Semantic errors
are `file:line: message` lines followed by
`Compilation halted due to static semantic errors.`, in this order:

1. class graph (basic-class redefinition, `Class X was previously defined.`,
   `Class X cannot inherit class Int.`, undefined parents, inheritance cycles,
   then `Class Main is not defined.`); any error here halts before features
   or expressions are checked;
2. features of each class in program order (attributes, methods, formals, and
   overrides), then `No 'main' method in class Main.`;
3. attribute initializers and method bodies in program order.

Recovery follows the course rules: dispatch arguments are type-checked before
`Dispatch to undefined method m.` is reported; a wrong argument count reports
`Method m called with wrong number of arguments.` without per-argument
conformance errors; `self` may be referenced but not bound by `let`, `case`
(`'self' bound in 'case'.`), or a formal; and an erroneous expression gets
type `Object` so checking continues. The default `text`/`json` diagnostic
contract is unchanged; this mode is accepted by `check` and by
`emit ast --format=reference --stage=semant` only.

The messages follow the classic course conventions. See the
[course assignment guide](course-assignments.md) for compatibility boundaries.

### `--frontend=handwritten|generated`

All compile commands (`run`, `check`, `emit`, and the legacy form) accept
`--frontend`. `handwritten` (the default) uses the hand-written lexer and
recursive-descent parser. `generated` uses the spec-driven lexer and the
goyacc LALR(1) parser generated from `internal/compiler/cool.y`; generated Go
sources are committed, so no generator runs at build, compile, or app runtime.
Both
front-ends produce the same AST; the generated parser reports syntax errors
as `COL2200 syntax error at or near X`. Reports record
`"options": {"frontend": "generated"}` only when it is selected. The GUI
offers the same choice in the session bar.

### `emit assembly --target=mips` (PA5)

```text
coolerc emit assembly --target=mips [file.cl ...]
```

Prints SPIM assembly for the Cool runtime system instead of Cooler's
educational pseudo-assembly (`--target=vm`, the default). The program must
pass semantic analysis; otherwise the diagnostics are written to stderr in
the selected `--diagnostic-format` and the command exits `1`.
`coolerc run --target=mips` runs the same program on the embedded MIPS
simulator (no host `spim`); the Cool runtime's messages are part of stdout,
and a runtime error exits `1`. `-g`, `-t`, and `-T` select the generational
collector, collection at every allocation, and object-header checks, as in
coolc; they require `--target=mips`. The object
layout, tables, calling convention, and lowering are documented in
[`mips-backend.md`](mips-backend.md). The GUI's **MIPS** tab shows and exports
the same file.
