# Compiler warnings

Warnings are educational diagnostics. They do not reject otherwise valid COOL,
remove code, skip initialization, or alter execution. Semantic errors remain
visible even when all warnings are disabled.

| Code | Rule |
| --- | --- |
| COL3161 | Division by constant zero will fail at runtime. |
| COL3180 | A `let` or `case` binding is never read. |
| COL3181 | A branch is unreachable because of a literal Boolean condition. |
| COL3182 | An assignment reads its own target without changing the value. |
| COL3183 | `isvoid` receives a literal or an allocation and is false if evaluation succeeds. |

`COL3180` points to the declaration's name. Binding identity respects nested
scopes and sequential `let` declarations, including repeated names. An
initializer can read an earlier binding. Assigning to a binding alone is a
write; reading it on the right-hand side counts as a read. Assignments retain
their expression value, and initializers retain all side effects. Attributes
and formal parameters are outside this rule's scope.

`COL3181` marks the unchosen branch of `if true`/`if false` and the body of
`while false`. It runs during semantic checking with or without optimization.
The entire branch remains type-checked, including unknown names and invalid
types. Comparisons, method calls and computed conditions are not inferred to
be constants by this warning. It does not remove code or alter execution.

`COL3182` detects a direct `x <- x` after resolving and validating the target.
It covers attributes, parameters, and local bindings, respecting shadowing.
Its optional QuickFix replaces the whole assignment with `x`, so the value
remains available to a containing block, call argument, or method body. A
nested expression that introduces another `x` is not treated as the same
declaration. Invalid targets and assignments to `self` retain errors without
offering this fix. The compiler never applies the QuickFix automatically.

`COL3183` covers direct Int/String/Bool literals and valid `new T` or
`new SELF_TYPE` expressions. Local variables, `self`, dispatches, and computed
expressions are deliberately outside this syntactic rule. The warning does
not prove evaluation will finish: constructor I/O, initialization failures,
allocations, and VM execution limits all remain observable. No QuickFix is
offered, including for literals, because replacing the expression can alter
instruction-limit behavior. The expression retains type Bool and its full span.

## Common rule selection

The default empty setting and `all` enable every supported warning. `none`
disables warnings. A comma-separated list of exact diagnostic codes enables
only those rules, for example `COL3161,COL3180`. Unknown codes, whitespace,
and selections longer than 256 bytes are rejected. Selection is recorded in
report options as `warningRules` when explicitly supplied.

- CLI: `coolerc check --warnings=COL3180 program.cl`, or
  `coolerc run --warnings=none program.cl`. Legacy flags also accept
  `--warnings`. Diagnostics preserve the usual source, line, column, code,
  severity, and JSON/text presentation.
- Public Go API: set `Options.WarningRules`; `NormalizeOptions` validates it.
- LSP: use `initializationOptions.warningRules`, or send
  `settings.cooler.warningRules` through `workspace/didChangeConfiguration`.
  Include the current `sourceSet` in that complete configuration when using
  independent exercises. Reconfiguration invalidates the shared analysis
  cache, and warnings publish as LSP severity 2 with UTF-16 ranges.
- Local GUI API: compilation, pipeline creation, and debugger creation accept
  `warningRules`. The GUI's ordinary compiler controls use the default and
  display warnings in Diagnostics with their code and source location.

For example, this produces `COL3180` while still printing `effect`:

```cool
class Main inherits IO {
  main() : Object {
    let unused : Object <- out_string("effect") in 0
  };
};
```
