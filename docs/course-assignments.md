# Course assignment modes

Course mode adds optional formats and execution targets commonly used in COOL
compiler courses. It is available from the command line and the graphical
workbench. These modes do not change the default compiler, VM, diagnostics, or
pseudo-assembly behavior.

## Assignment formats

The CLI can emit the reference token stream with
`coolerc emit tokens --format=reference`, an untyped parser tree with
`coolerc emit ast --format=reference`, and a semantically annotated tree with
`coolerc emit ast --format=reference --stage=semant`. Use
`coolerc check --diagnostic-format=reference` for course-style lexer, parser,
and semantic error reports. See the [CLI reference](cli.md) for output details.

Reference formats aim to match the classic course tools, including token
names, tree structure, error text, and ordering. They are opt-in and are not
the default Cooler output or a general claim of compatibility with every
course distribution.

## Generated front-end

Select `--frontend=generated` to use the spec-driven lexer and LALR parser.
The default `handwritten` front-end remains available. Both front-ends share
the compiler's semantic analysis and downstream pipeline. Generated source is
committed, so normal builds do not run a generator or require generator tools.

## MIPS target

Select `--target=mips` to emit SPIM-compatible assembly or run it with Cooler's
embedded MIPS simulator. The simulator runs locally and does not launch a host
SPIM executable. Collector options `-g`, `-t`, and `-T` are available for this
target. See the [MIPS backend guide](mips-backend.md) for the object layout,
calling convention, runtime, and simulator details.

The GUI presents these features in Course mode, including reference
diagnostics, front-end selection, and a MIPS output tab. The regular VM and
default compiler workflow remain available in the same session.
