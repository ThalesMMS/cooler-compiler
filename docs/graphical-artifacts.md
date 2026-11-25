# AST and control-flow graphs

The AST and Control flow tabs include a deterministic, bounded SVG graph plus a keyboard-accessible table. Nodes select their source span independently of the AST tree disclosure. Arrows pan, +/- zoom, Home fits, and Enter/Space select a focused node. Background dragging pans. Camera state survives artifact rendering; Fit restores it.

The layout uses stable breadth-first AST order or compiler CFG block order in a grid (at most five columns). It is intentionally not a graph optimization algorithm. Cycles and self-edges do not trigger recursive layout. At most 200 nodes and 1,000 edges are rendered; omitted content is announced. The supplementary tree/table is omitted when it would exceed these limits. Source IDs travel with node spans for multifile navigation.

Export artifact offers standalone AST/CFG SVG and PNG. SVG embeds colors, labels, arrows and a background without external resources. Labels are escaped and bounded. PNG scales 1–4 are checked before allocating a canvas: at most 8,192 pixels per side and 16 megapixels (up to 64 MiB for the raw RGBA canvas, plus browser encoding/decode overhead). Oversize exports ask for a smaller scale or SVG. PNG decoding uses a data URL permitted by the GUI image policy. Existing inheritance exports remain available.

## Validation

- `node --test internal/web/static/graphs.test.mjs`: deterministic layout, empty graphs, label escaping, large AST, cyclic/multiple-method CFG and raster limits.
- `node scripts/serve-graph-validation.mjs`, then open its local URL: production graph module keyboard pan/fit/zoom and source selection on 2,001 AST nodes, bounded to 200; PNG decode verifies dimensions under the GUI image CSP. Chrome 152 in-app browser: 36.3 ms, 1,060 × 300 PNG, 21,812 bytes. This is an observation, not a performance threshold.
- Actual GUI in the same browser: compiled Hello, selected a string graph node at line 4 column 16, zoomed/fitted, exported AST PNG 2× successfully, changed theme and exercised CFG export.
- Full Go suite and static checks passed. Native OS WebViews and external screen readers were unavailable; accessibility-tree and programmatic keyboard checks do not establish those platforms' behavior.
