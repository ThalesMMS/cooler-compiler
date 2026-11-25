# LSP signature help (#67)

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

## Validation with an actual protocol client

The test corpus covers opening/comma/closing triggers, nested calls, Unicode
strings and nested comments, let-binding commas, lexical shadowing contexts,
static SELF_TYPE and chains, unknown receivers, attribute initializers, zero
parameters, multifile signature changes, builtin catalog drift and cancellation.

The following uses Microsoft's real `vscode-languageserver-protocol` client
implementation over the server's stdio transport, with a 20-second watchdog:

```sh
npm install --prefix dist/lsp-client --no-audit --no-fund vscode-languageserver-protocol@3.17.5
go build -o dist/cooler-lsp ./cmd/cooler-lsp
node scripts/check-signature-client.mjs dist/cooler-lsp
```

On Windows use `.exe` in both paths. The local run passed capability negotiation,
an actually incomplete `out_int(` document, builtin documentation, a version-2
change and closing-call dismissal. This is protocol-client validation; no visual
IDE popup is claimed by this test. Local editor packaging is tracked separately
in #74. The optional npm client is installed only in ignored dist and is not a
runtime dependency of cooler-lsp.

Protocol reference: [LSP signature help](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/#textDocument_signatureHelp).
