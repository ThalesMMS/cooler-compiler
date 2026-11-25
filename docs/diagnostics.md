# Diagnostic catalog and stability

Diagnostics are structured compiler results, not text scraped from stderr. The
normative JSON fields and span coordinate system are defined by
[`diagnostics-v1.schema.json`](../schemas/diagnostics-v1.schema.json); CLI rendering
and exit behavior are in [`docs/cli.md`](cli.md).

## Stability policy

Within a supported contract major:

- a published `COLdddd` code is never reused for a different condition;
- `stage`, `severity`, and the semantic meaning of the code are stable;
- source spans remain 1-based lines/columns with exclusive UTF-16-code-unit end
  offsets as described in the [report contract](artifact-contract.md#complete-reports);
- message and hint prose may improve without being a breaking change;
- related spans, quick fixes, suggestions, and new optional fields are additive;
- removal or semantic reassignment requires a major contract version and a
  migration entry in [`CHANGELOG.md`](../CHANGELOG.md#versioning-and-migrations).

Tests may intentionally pin exact prose for a regression even when prose is not
globally stable. Consumers should branch on `code`, `stage`, and `severity`, not
English wording.

## Code ranges

| Range | Stage | Purpose | Canonical implementation |
| --- | --- | --- | --- |
| `COL0xxx` | `input` | Missing or invalid project source-unit identity | [`pipeline.go`](../internal/compiler/pipeline.go) |
| `COL1xxx` | `lexer` | Invalid characters, strings, comments, and lexical limits | [`lexer.go`](../internal/compiler/lexer.go) |
| `COL2xxx` | `parser` | Recovery, declarations, expressions, and syntax expectations | [`parser.go`](../internal/compiler/parser.go) |
| `COL3xxx` | `semantic` | Classes, inheritance, scopes, types, dispatch, and entry point | [`analyzer.go`](../internal/compiler/analyzer.go) |
| `COL5xxx` | `ir` | Unsupported lowering/internal compiler invariant | [`codegen.go`](../internal/compiler/codegen.go) |
| `COL51xx` | `codegen` | Pre-execution bytecode and backend-metadata verification | [`verifier.go`](../internal/compiler/verifier.go) |
| `COL52xx` | `optimization` | A named optional pass violated the verifier barrier | [`optimizer.go`](../internal/compiler/optimizer.go) |
| `COL6xxx` | `runtime` | VM errors, deterministic limits, builtins, and invalid bytecode | [`vm_execute.go`](../internal/compiler/vm_execute.go) |

Project-input codes are stable before language analysis:

| Code | Meaning |
| --- | --- |
| `COL0001` | Project has no source units |
| `COL0002` | Source unit has an empty ID |
| `COL0003` | Source-unit ID is duplicated |
| `COL0004` | Source unit has an empty display name |
| `COL0005` | VM execution policy is invalid or above a local ceiling |
| `COL0006` | Optimization mode is neither `none` nor `basic` |
| `COL0007` | Warning selector exceeds its size limit or contains an unknown warning code |

Lexer codes are divided by stable error category:

| Code | Meaning |
| --- | --- |
| `COL1001` | Rune outside the ASCII COOL grammar |
| `COL1002` | String terminated by an unescaped line break or EOF |
| `COL1003` | EOF inside a nested block comment |
| `COL1004` | Unmatched `*)` block-comment terminator |
| `COL1005` | Raw NUL inside a string |
| `COL1006` | Decoded string longer than 1024 characters |
| `COL1007` | Integer literal above `2147483647` |
| `COL1008` | Malformed UTF-8 source byte |
| `COL1009` | Identifier beginning with underscore |
| `COL1010` | Escaped NUL inside a string |

Lexer diagnostics are ordered by encounter position. A single malformed string
produces at most one fatal string diagnostic; message and hint prose remain
non-normative under the stability policy.

Parser codes preserve these category boundaries:

| Range/code | Meaning |
| --- | --- |
| `COL2001`–`COL2022` | Program, class, feature, and formal structure |
| `COL2030`–`COL2053` | Assignment, precedence, dispatch, primaries, and delimiters |
| `COL2060`–`COL2107` | Control, block, let, and case forms |
| `COL2110` | Expression nesting exceeded 512 levels |
| `COL2111` | The deterministic 100-diagnostic parser cap was reached |

Parser diagnostics are emitted in encounter order. Missing delimiters can share
a recovery position, but one root error cannot produce an unbounded cascade.
The partial-AST and backend-barrier contract is documented under
[parser recovery](cool-language.md#parser-recovery-and-partial-ast).

Class-table semantic codes preserve these category boundaries:

| Range/code | Meaning |
| --- | --- |
| `COL3001`–`COL3002` | Reserved, basic, or duplicate class declarations |
| `COL3010`–`COL3012` | Forbidden/undefined parents and inheritance cycles |
| `COL3020`–`COL3022` | Attribute names, declared types, and duplicates |
| `COL3030`–`COL3035` | Methods, return types, formals, and duplicate formals |
| `COL3040`–`COL3041` | Inherited attribute redefinition and incompatible overrides |
| `COL3050`–`COL3052` | Missing/invalid `Main.main` entry point |
| `COL3060`–`COL3061` | Attribute initializer and method body conformance |
| `COL3100`–`COL3103` | Identifier lookup and assignment |
| `COL3110`–`COL3111` | `if` and `while` conditions |
| `COL3120`–`COL3122` | `let` names, types, and initializer conformance |
| `COL3130`–`COL3133` | `case` branch names, types, and uniqueness |
| `COL3140` | Allocation of an undefined type |
| `COL3150`–`COL3163` | Unary, arithmetic, comparison, equality, and constant-zero checks |
| `COL3170`–`COL3175` | Static/dynamic dispatch type, lookup, arity, and arguments |
| `COL3180` | Warning: a local binding is never read; its initializer and assignments still execute |
| `COL3181` | Warning: a Boolean literal condition makes a branch unreachable; it remains type-checked |
| `COL3182` | Warning: assigning a variable to itself preserves its value |
| `COL3183` | Warning: `isvoid` is false if the literal or allocation evaluates successfully; evaluation remains required |

Class collection, parent recovery, feature collection, inherited-feature
validation, entry-point validation, and body checking run in that fixed order.
Within a pass, user classes and features use project source-unit order. Duplicate,
cycle, and override diagnostics include related spans when another declaration
participates. An error in this range blocks all backend phases.
`COL3180`–`COL3183` have severity `warning` and do not block backend phases,
including Runtime.

After a root expression error, its AST node and dependent parents use
`<error_type>`. Operators, conditions, dispatch, conformance, and LUB suppress
only diagnostics that would restate that invalid child; other operands,
arguments, branches, and sibling expressions are still analyzed in source
order. Golden AST and diagnostic fixtures lock down that containment behavior.

Verifier codes identify backend defects before a VM exists:

| Code/range | Invariant |
| --- | --- |
| `COL5101`–`COL5102` | Known opcode and valid method-local control-flow targets |
| `COL5103`–`COL5105` | No underflow, equal merge heights, and valid operand categories |
| `COL5106`–`COL5107` | Balanced/mergeable scopes and valid typed storage |
| `COL5108`–`COL5110` | Call signatures, terminal contracts, and reachability |
| `COL5111`–`COL5114` | Method tables, IR/CFG/assembly metadata, spans, entry/initializer contracts |
| `COL5200` | A named optimization pass produced invalid verified bytecode |

These diagnostics use stage `codegen`, name the stable method and instruction,
and always block Runtime. They represent compiler defects rather than COOL
program errors.

Runtime codes separate user-program failures from defensive VM invariants:

| Category | Codes |
| --- | --- |
| User arithmetic/dispatch/case failures | `COL6015`, `COL6020`, `COL6030`, `COL6031` |
| User built-in failures | `COL6040` (`abort`), `COL6046` (substring bounds) |
| Deterministic resource limits | `COL6002`, `COL6010`, `COL6019`, `COL6024`, `COL6050`–`COL6055` |
| Execution cancellation | `COL6056` |
| Recovered host failure | `COL6099` |
| Defensive VM invariants (unreachable after verification) | `COL6001`, `COL6003`–`COL6008`, `COL6011`–`COL6014`, `COL6016`–`COL6018`, `COL6021`–`COL6027`, `COL6032`, `COL6041`–`COL6045`, `COL6047` |

Receiver and case-subject failures use the exact operand span; arithmetic and
built-in failures use the complete failing expression/call. Runtime halt keeps
the first failure only and preserves exportable stack, frame, trace, output, and
diagnostic state.

The structured constructor and field definitions are in
[`internal/compiler/model.go`](../internal/compiler/model.go). Stage-specific
goldens cover lexer/parser, semantic, and runtime examples in
[`internal/compiler/testdata/golden/`](../internal/compiler/testdata/golden/).

## Using diagnostics

This valid program emits no errors:

```cool
class Main inherits IO {
  main() : Object { out_string("ok\n") };
};
```

This program reaches semantic `COL3173` because `Object` has no `concat` method:

```cool
class Main {
  main() : Object {
    (new Object).concat("!")
  };
};
```

Request stable JSON-shaped diagnostics with:

```bash
go run ./cmd/coolerc check --diagnostic-format json broken.cl
```

Exact end-to-end fixtures and intentional golden-update instructions are in
[`CONTRIBUTING.md`](../CONTRIBUTING.md#adding-a-cool-conformance-fixture).

## Current code inventory

This section is generated-reference data: `make docs-check` compares it with
codes present in the compiler and CLI source. Add the code here in numeric order
in the same change that introduces it.

<!-- BEGIN GENERATED DIAGNOSTIC CODES -->
- `COL0001`
- `COL0002`
- `COL0003`
- `COL0004`
- `COL0005`
- `COL0006`
- `COL0007`
- `COL1001`
- `COL1002`
- `COL1003`
- `COL1004`
- `COL1005`
- `COL1006`
- `COL1007`
- `COL1008`
- `COL1009`
- `COL1010`
- `COL2001`
- `COL2002`
- `COL2003`
- `COL2004`
- `COL2005`
- `COL2006`
- `COL2007`
- `COL2010`
- `COL2011`
- `COL2012`
- `COL2013`
- `COL2014`
- `COL2015`
- `COL2016`
- `COL2017`
- `COL2018`
- `COL2019`
- `COL2020`
- `COL2021`
- `COL2022`
- `COL2030`
- `COL2031`
- `COL2040`
- `COL2041`
- `COL2042`
- `COL2043`
- `COL2044`
- `COL2045`
- `COL2050`
- `COL2051`
- `COL2052`
- `COL2053`
- `COL2060`
- `COL2061`
- `COL2062`
- `COL2070`
- `COL2071`
- `COL2080`
- `COL2081`
- `COL2082`
- `COL2090`
- `COL2091`
- `COL2092`
- `COL2093`
- `COL2100`
- `COL2101`
- `COL2102`
- `COL2103`
- `COL2104`
- `COL2105`
- `COL2106`
- `COL2107`
- `COL2110`
- `COL2111`
- `COL3001`
- `COL3002`
- `COL3010`
- `COL3011`
- `COL3012`
- `COL3020`
- `COL3021`
- `COL3022`
- `COL3030`
- `COL3031`
- `COL3032`
- `COL3033`
- `COL3034`
- `COL3035`
- `COL3040`
- `COL3041`
- `COL3050`
- `COL3051`
- `COL3052`
- `COL3060`
- `COL3061`
- `COL3100`
- `COL3101`
- `COL3102`
- `COL3103`
- `COL3110`
- `COL3111`
- `COL3120`
- `COL3121`
- `COL3122`
- `COL3130`
- `COL3131`
- `COL3132`
- `COL3133`
- `COL3140`
- `COL3150`
- `COL3151`
- `COL3160`
- `COL3161`
- `COL3162`
- `COL3163`
- `COL3170`
- `COL3171`
- `COL3172`
- `COL3173`
- `COL3174`
- `COL3175`
- `COL3180`
- `COL3181`
- `COL3182`
- `COL3183`
- `COL5001`
- `COL5101`
- `COL5102`
- `COL5103`
- `COL5104`
- `COL5105`
- `COL5106`
- `COL5107`
- `COL5108`
- `COL5109`
- `COL5110`
- `COL5111`
- `COL5112`
- `COL5113`
- `COL5114`
- `COL5200`
- `COL6001`
- `COL6002`
- `COL6003`
- `COL6004`
- `COL6005`
- `COL6006`
- `COL6007`
- `COL6008`
- `COL6010`
- `COL6011`
- `COL6012`
- `COL6013`
- `COL6014`
- `COL6015`
- `COL6016`
- `COL6017`
- `COL6018`
- `COL6019`
- `COL6020`
- `COL6021`
- `COL6022`
- `COL6023`
- `COL6024`
- `COL6025`
- `COL6026`
- `COL6027`
- `COL6030`
- `COL6031`
- `COL6032`
- `COL6040`
- `COL6041`
- `COL6042`
- `COL6043`
- `COL6044`
- `COL6045`
- `COL6046`
- `COL6047`
- `COL6050`
- `COL6051`
- `COL6052`
- `COL6053`
- `COL6054`
- `COL6055`
- `COL6056`
- `COL6099`
<!-- END GENERATED DIAGNOSTIC CODES -->
