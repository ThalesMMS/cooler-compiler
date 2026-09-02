# Multifile and source-unit semantics

Cooler accepts several source files or GUI tabs as one COOL program. Each input
is an ordered native source unit with a stable ID, display name, and exact
content. Source units are organizational boundaries only: they do not introduce
modules, namespaces, imports, separate compilation, object files, or linking.

## Native project compilation

`coolerc run helper.cl main.cl` reads the files in argument order and submits
two independent source units to the compiler. Each unit is lexed and parsed in
its own coordinate space, then the resulting complete class declarations are
combined for semantic analysis. Cooler never inserts separators, normalizes
line endings, or otherwise constructs a synthetic aggregate source.

The public Go API is:

```go
result, err := compiler.CompileProject(context.Background(), compiler.Project{
    Sources: []compiler.SourceUnit{
        {ID: "helper", Name: "helper.cl", Content: helperSource},
        {ID: "main", Name: "main.cl", Content: mainSource},
    },
}, compiler.DefaultOptions())
if err != nil {
    return err
}
```

`Compile`, `CompileWithInput`, and single-source pipeline constructors remain
compatibility wrappers around a one-unit project. Empty/comment-only units keep
their position. Duplicate display names are accepted; empty IDs, duplicate IDs,
and empty names produce stable input diagnostics.

## Diagnostics and artifact identity

Every source-linked span contains `sourceId`. Its line, column, and UTF-16
offsets are local to that source unit, including primary diagnostics, related
locations, quick fixes, tokens, AST nodes, symbols, stack IR, CFG blocks and
edges, pseudo-assembly mappings, VM trace entries, and runtime frames. No CLI or
GUI remapping step is required.

Complete reports include ordered `sourceUnits`. Each records `id`, display
`name`, zero-based `order`, original byte size, and SHA-256 of the exact content.
The legacy top-level `source` field contains the source only for a single-unit
project and is empty for multifile projects. `assemblySpans` is line-aligned
with `assembly`.

## GUI tabs and breakpoints

Open replaces the current project; Add appends a source unit. A tab keeps its
stable source ID across edits and reordering. Compilation and incremental
pipeline requests send explicit source units, and breakpoints use
`{sourceId, line}` rather than a tab index or aggregate line. Closing or moving
another tab therefore cannot retarget a breakpoint.

GUI ZIP exports store every source separately under an ordered safe path. The
report and archive manifest retain the same source-unit IDs and metadata. See
the [artifact contract](artifact-contract.md#report-zips).

## Program input

With one or more CLI source paths, stdin is reserved for line-oriented program
input:

```bash
go run ./cmd/coolerc run helper.cl main.cl < input.txt
```

With no file path, stdin is the single source unit and program input is empty.
Use `--input` with file sources to read program input from a named file. In the
GUI, program input is separate from every source tab. Detailed forms are in
[`docs/cli.md`](cli.md).

## Non-goals

Source units preserve origin and ordering; they do not change classic COOL
program structure. A class declaration cannot be split across units, and no
unit can import or hide declarations from another unit. Adding module or linker
semantics would require a separate language and artifact-contract decision.
