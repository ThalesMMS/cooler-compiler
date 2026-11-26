# LSP signature help

`cooler-lsp` advertises `signatureHelpProvider` with `(` and `,` triggers and `)`
as a retrigger. Clients can also invoke `textDocument/signatureHelp` manually.
Responses use the standard LSP 3.17 signature/parameter shape, with plain-text
builtin documentation and the active parameter. Parameter labels include their
declared types. Zero-parameter methods omit activeParameter; excess arguments
keep the last parameter active while normal diagnostics report arity errors.

The call context is derived from lexer tokens up to the UTF-16 cursor position.
Nested parentheses, blocks and let-binding commas are tracked separately;
punctuation inside strings or nested comments is not counted. Closing the inner
call returns to its outer call; closing the outermost call returns null. Method
declaration parameter lists are excluded.

Signatures and lexical environments come from declared types in the recovered
syntax tree, so semantic analysis does not have to succeed and the tree does not
have to represent a valid program. This covers incomplete implicit/explicit/static
calls, new expressions, literals, chained calls, formals, attributes, let and case
scopes, including attribute initializers. A receiver whose type cannot be proven
from the recovered tree returns null, rather than guessing the current class.
For example, severely malformed compound receiver expressions may need more
typing before help becomes available. Inference of nested receiver chains stops
after 64 nodes; ordinary workspace/parser bounds also apply.

Inherited targets follow the receiver's static type. SELF_TYPE resolves in the
dynamic-dispatch receiver's static context, or in the explicit type after `@` for
static dispatch, matching this compiler's analyzer. Invalid unrelated static
targets and duplicate/ambiguous declarations do not produce speculative help.
Builtin fallback signatures are checked against the compiler's actual symbol
catalog in a regression test.

The request uses the current versioned workspace analysis, checks cancellation
during token scanning and before returning, and rejects a changed generation
with LSP ContentModified (-32801). `$/cancelRequest` uses the
existing cancellation path. No last-good-document cache is substituted for
current unsaved text.
