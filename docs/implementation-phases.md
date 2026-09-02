# Implementation phases

## 1. Port the application shell

- retain a Go binary, loopback HTTP API, embedded web assets, browser fallback, and native macOS WebView;
- rename the commands, artifacts, routes, packaging metadata, and interface for Cooler Compiler Lab;
- preserve source-linked spans and bounded stateful VM sessions.

## 2. Replace the language frontend

- implement the classic COOL token set and lexical rules;
- create a class/feature/expression AST rather than adapting Swift-shaped declarations;
- implement full expression precedence, dispatch syntax, and parser recovery.

## 3. Add COOL static semantics

- install the basic-class prelude;
- validate inheritance, cycles, forbidden parents, feature duplication, and overrides;
- model `SELF_TYPE`, conformance, and least-upper-bound operations;
- type-check all expressions and validate `Main.main()`.

## 4. Lower to executable educational IR

- synthesize per-class initializers;
- lower methods and expressions to stack instructions;
- flatten method bytecode into source-linked IR and pseudo-assembly;
- derive basic blocks and control-flow edges.
- verify every method's stack, scope, control-flow, call, terminal, and metadata
  invariants before constructing a VM.

## 5. Implement the object VM

- allocate objects with inherited fields and COOL default values;
- execute dynamic/static dispatch, calls, returns, lexical scopes, and `case` selection;
- implement Object, IO, and String built-ins;
- expose call frames, operand stack, output, diagnostics, and trace entries;
- enforce deterministic safety limits.

## 6. Rebuild the laboratory UI

- provide editor/file/example workflows;
- render every compilation artifact;
- make source spans clickable;
- support breakpoints, step, run, reset, console output, light/dark themes, and JSON report export.

## 7. Verification and delivery

- unit-test lexer/parser/semantics/runtime paths;
- execute every valid bundled example and verify exact output;
- verify intentional semantic and runtime failures;
- test API guards, session lifecycle, and embedded assets;
- run formatting, tests, vet, JavaScript syntax validation, and release builds.
