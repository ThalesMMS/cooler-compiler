# Symbol suggestions and quick fixes

Unknown identifiers, assignment targets, types and methods retain their existing
diagnostic codes. A diagnostic offers **Replace with NAME** only when exactly
one available name differs by one insertion, deletion, substitution or adjacent
transposition (`lenght` → `length`). Names shorter than three or longer than 64
ASCII bytes do not receive suggestions. Ties suppress the fix entirely; nothing
is applied automatically.

Variable candidates follow lexical shadowing and inherited attributes. Methods
come only from the receiver's static lookup class and its ancestors. Static
dispatch type candidates must be ancestors of the receiver. Parent suggestions
exclude sealed basic classes, self-inheritance and inheritance cycles. Formal
and case types exclude `SELF_TYPE`; contexts permitting it include it.

Searches visit at most 2,048 entries each and 16,384 entries per analysis,
including hierarchy/scope traversal. Exhaustion suppresses partial results, so
map iteration never chooses an arbitrary truncated winner. Distance checks are
linear and use no edit-distance matrix. A correction addresses the unknown
symbol; unrelated type/argument errors can still require separate edits.

The parser retains identifier and static dispatch type token spans separately
from enclosing expression spans. Thus `(cuont)` changes only `cuont`, and
`receiver@Paren.method()` changes only `Paren`. Source IDs and native UTF-16
offsets flow through the public compiler report to GUI and LSP.

LSP clients advertising `capabilities.workspace.workspaceEdit.documentChanges`
receive `documentChanges` with the analyzed open-document version (or null for
a closed file). Clients must reject a versioned edit after intervening changes.
Legacy clients retain `changes` edits without a version guard. The GUI resets
the compilation session and old actions after applying a fix or editing source.
