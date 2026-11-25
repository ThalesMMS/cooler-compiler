# Symbol QuickFixes (#66)

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

## Reproduction and validation

```sh
CGO_ENABLED=0 go test ./internal/compiler -run 'TestSymbolSuggestions|TestSuggestionBounds|TestOneEditApart' -count=1
CGO_ENABLED=0 go test ./internal/lsp -run TestSymbolQuickFixVersionedUTF16 -count=1
```

Tests cover variables/assignment, inherited members, shadowing, every unknown
type declaration site, allocation and static dispatch, ambiguity, unavailable
members, distance and search limits. Applying each positive single-file fix
recompiles without errors. The LSP test applies a version-7 action after an
emoji in a two-file project and successfully analyzes the corrected version 8.

Browser GUI validation on Windows used:

```cool
class Main inherits IO {
  main():Object {{"😀"; out_int("abcd".lenght());}};
};
```

Compile displayed COL3173 and **Replace with length**. Clicking it changed only
the method token, cleared stale artifacts, and recompilation/run halted with
output `4` (22 instructions). This exercised the actual browser UI, not just a
server build. Native macOS application validation was unavailable.

The full short Go suite, vet, pinned lint and documentation checks passed on
Windows. The focused LSP test also passed with the race detector; suggestion
tests executed successfully in Debian/WSL. Independent review found no defects.
