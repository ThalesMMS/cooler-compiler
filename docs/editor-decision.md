# Editor decision and delivered scope

Keep the native textarea and its compiler/debugger integration for this release. Ship diagnostic navigation (F8/Shift+F8 and buttons), block indent/outdent (Ctrl/Cmd+]/[ and buttons), per-source selection/scroll restoration, bounded per-source undo/redo, composition-safe shortcuts and conservative breakpoint movement. Tab now leaves the editor through normal keyboard navigation.

Every source edit invalidates compiled spans and the VM. Breakpoints follow unchanged prefix/suffix lines; same-height replacements retain explicit line intent, while deleted or ambiguous changed lines lose the breakpoint. Recompile resolves executable positions again. Diagnostic navigation only uses current compiled sources, switches source tabs and announces the message through a status region. Report inspection remains read-only.

History keeps at most 100 operations and 4 MiB of source text across tabs, measured as two bytes per UTF-16 unit. Undo/redo is isolated by document instance, including imported reports with reused source IDs. Large edits can evict history. Indentation uses the browser editing command with a setRangeText fallback; the application history covers both paths. Ordinary paste/cut are native input events. Selection offsets remain UTF-16 throughout.

## Local component comparison

`experiments/editor-comparison/package-lock.json` pins CodeMirror 6 state/view/commands/language/lint and esbuild. Run `npm ci --prefix experiments/editor-comparison`, `node scripts/build-editor-comparison.mjs`, then `node scripts/serve-editor-comparison.mjs` and open `http://127.0.0.1:9093/experiments/editor-comparison/index.html`. No CDN is used. Dependencies remain experiment-only and are not included in GUI releases.

The component prototype exercises folding, next diagnostic, two cursors with a shared edit and undo/redo. It bundles 287,178 bytes (93,509 gzip). Its runtime packages use MIT licenses (see the installed package licenses and lockfile); retaining their notices would be required for distribution. The existing editor adds no third-party runtime. CodeMirror exposes an editable accessibility tree and a named fold placeholder; the textarea exposes a native multiline field. Neither observation substitutes for screen-reader testing.

Chrome 152 in-app browser, one 95,037 UTF-16-unit source: component creation 12.1 ms, 50 edits 8.9 ms; bare textarea assignment 1.2 ms, 50 setRangeText edits 81.0 ms. These timings exclude compiler work and are observations, not thresholds. Coarse browser heap readings were 7,511,918 bytes before/after CodeMirror and 10,816,253 after both: caching and shared browser heap make these unsuitable for attributing memory to one component.

CodeMirror clearly offers better large-document editing machinery. Adopting it still requires replacing direct textarea value/selection/scroll operations, gutter line coordinates, token overlay, read-only report behavior, draft restoration and source-tab history with transactions. Folding also needs a COOL-aware parser or reliable fold service. Multiple cursors need defined interactions with source-span selection and run-to-cursor. These features and diagnostic underline decorations are deferred; the standalone prototype does not count as a shipped editor replacement. The delivered subset above is integrated into the real GUI.

## Validation and limits

`node --test internal/web/static/editor-tools.test.mjs` checks Unicode block edits, line-boundary selections, breakpoint mapping, diagnostic cycling, source history isolation and memory bounds. The browser page `http://127.0.0.1:9093/experiments/editor-validation.html` passed native undo/redo, Unicode insertion/deletion shaped like paste/cut, synthetic composition events, read-only protection and input delivery.

For production integration, run coolergui in browser mode on 127.0.0.1:9094, then `node scripts/serve-editor-integration.mjs` and open 127.0.0.1:9095. This loopback-only development proxy injects a DOM test into the real GUI without adding a production test hook. It passed main/helper multifile opening, diagnostic selection, indentation, undo/redo, cross-tab history isolation, retained selection, breakpoint movement, compilation, execution to the moved breakpoint, resume/halt and theme switching.

Full Go tests, vet and lint passed. Native OS WebViews, physical IME input, actual system clipboard gestures and external screen readers were unavailable. Synthetic composition and clipboard-shaped edits are narrower evidence. Folding/multiple cursors are only prototype features, so there is no folded-source breakpoint behavior in the shipped editor. Large files still incur full gutter/overlay rendering; the comparison does not claim this bottleneck is solved.
The integrated GUI also completed five edits of a 60,000 UTF-16-unit, 5,000-line source in 800.2 ms total. Coarse heap changed from 8,384,806 to 11,243,063 bytes. This includes gutter/overlay rebuilding and is not directly comparable with the bare component timings above.
