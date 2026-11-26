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

