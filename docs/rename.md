# Safe LSP rename

`textDocument/prepareRename` and `textDocument/rename` support user classes,
attributes, methods, parameters, let bindings and case bindings in the selected
source set. Methods rename the entire connected override family, including
static and dynamic dispatch references. Main and Main.main remain entry points;
builtins, builtin override families, self and SELF_TYPE are not editable.

Identifiers must follow COOL capitalization and contain 1–64 ASCII letters,
digits or underscores, starting with a letter. Keywords are rejected. Invalid or
unresolved projects cannot be renamed. Method collisions in related classes are
rejected even when they would still type-check by creating a new override.

The server builds proposed source in memory, reanalyzes it and checks the identity
of every indexed declaration and reference. This catches shadow capture as well
as compiler-reported collisions. Parentheses and comments remain untouched.
No files or server buffers are changed: the client receives a complete
`WorkspaceEdit.documentChanges`, with original URIs, UTF-16 ranges and versions
for open documents (null for closed documents). Clients must advertise
`workspace.workspaceEdit.documentChanges`; unversioned fallback is refused.

Rename is bounded to 1,000 edits, 32,768 indexed occurrences and 2 MiB of resulting
source, also respecting configured file/workspace limits and analysis timeout.
Source replacement uses one pass. Cancellation or a changed generation discards
the result. The guarantee covers the loaded source set; select the complete
project to include all external references before renaming shared symbols.

Windows validation: focused parameter/let/case/class/attribute/override tests,
capture and inherited collision rejection, multi-file versions and emoji ranges,
source trivia preservation and expansion limits. The real Microsoft LSP 3.17.5
protocol client in `scripts/check-rename-client.mjs` prepares, applies a versioned
rename and checks the updated document. Run it with the built cooler-lsp path,
using the dependency setup in `scripts/check-signature-client.mjs`. Native IDE
rename UI and macOS validation were unavailable; protocol execution is not a
visual IDE test.
