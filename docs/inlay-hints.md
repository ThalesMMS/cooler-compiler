# Optional LSP inlay hints

`textDocument/inlayHint` is available with two independently opt-in categories:

```json
{"initializationOptions":{"inlayHints":{"parameterNames":true,"selfType":true}}}
```

At runtime send `workspace/didChangeConfiguration` with
`settings.cooler.inlayHints`. Omitted categories and unrelated project settings
are preserved; `false` disables a category. Both categories default to false.

Parameter hints use short labels such as `count:` before arguments, with right
padding and a tooltip containing the parameter type and method owner. An argument
already named `count` receives no redundant hint. Let declarations receive none.
SELF_TYPE results show the expression's static context, such as `: Child` or
`: Base` for explicit dispatch through `@Base`. The tooltip explicitly states that
this does not infer runtime dynamic types. Declared ordinary return types are not
repeated. Nested calls, inherited methods and overrides use resolved declarations.

Hints are filtered by the requested UTF-16 range and limited to 500 per response.
Analysis errors suppress hints conservatively, including errors elsewhere in the
selected source set. Cancellation and generation checks prevent obsolete results.
Configuration changes invalidate cached results and reschedule diagnostics.

Validation on Windows: focused resolution, nested-call, emoji position, range,
version, cancellation and configuration regression tests; full Go tests, vet,
pinned lint and documentation checks. `scripts/check-inlay-client.mjs` runs the
actual Microsoft `vscode-languageserver-protocol` 3.17.5 stdio client against the
built server and checks labels, spacing, tooltip and independent category toggles.
Install that pinned dependency under ignored `dist/lsp-client` as described in
`scripts/check-signature-client.mjs`, then run:

```text
go build -o dist/cooler-lsp.exe ./cmd/cooler-lsp
node scripts/check-inlay-client.mjs dist/cooler-lsp.exe
```

Protocol-level readability was checked; rendered hint placement in a native IDE
and macOS application validation were unavailable in this run. This is not a
claim that an IDE popup was visually tested.
