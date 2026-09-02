# Visualization and provenance model

Cooler reports from schema 1.3 can include a bounded `visualizations` bundle
after semantic analysis. It connects the compiler's existing source-linked
artifacts without changing compilation or VM behavior.

## Inheritance and logical layouts

`inheritance` contains basic and user classes in deterministic compiler order.
Each user node retains its original parent and, when semantic recovery changes
that edge, its `recoveredParent`. Cycle edges remain explicit and safe to
render. Class tables contain inherited and declared attributes in effective
slot order plus effective dynamic-dispatch slots. An override replaces the
inherited slot and records the replaced implementation. `lookupOrder` and each
method's `lookupStarts` expose static-dispatch starting points; SELF_TYPE
returns are explicit.

These are logical educational layouts. They do not specify object byte sizes,
alignment, vtable addresses, a native ABI, or the reference toolchain's memory
representation.

## Cross-phase provenance

`artifacts` gives stable IDs to source-linked tokens, typed AST nodes, symbols,
IR, CFG, assembly, VM trace states, and diagnostics. `groups` use recorded typed
AST spans as anchors and relate zero or more artifacts. Relations are
many-to-many and retain the source-unit ID. Compiler-generated artifacts have
`synthetic: true`; they are inspectable but are not assigned invented source
locations.

The compiler bounds class, artifact, group, relation, and relation-building
work. Clients must display `truncated` and may impose tighter viewport limits.
The GUI renders at most 120 graph nodes at once and provides search plus a
complete keyboard-operable table alternative.

## Exports

The GUI can export the structured visualization JSON, a deterministic
self-contained inheritance SVG, or DOT-like inheritance and control-flow
documents. Each format records the visualization/compiler version and ordered
source-unit IDs.
