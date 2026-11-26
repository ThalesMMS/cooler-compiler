# AST and control-flow graphs

The AST and Control flow tabs include a deterministic, bounded SVG graph plus a keyboard-accessible table. Nodes select their source span independently of the AST tree disclosure. Arrows pan, +/- zoom, Home fits, and Enter/Space select a focused node. Background dragging pans. Camera state survives artifact rendering; Fit restores it.

The layout uses stable breadth-first AST order or compiler CFG block order in a grid (at most five columns). It is intentionally not a graph optimization algorithm. Cycles and self-edges do not trigger recursive layout. At most 200 nodes and 1,000 edges are rendered; omitted content is announced. The supplementary tree/table is omitted when it would exceed these limits. Source IDs travel with node spans for multifile navigation.

Export artifact offers standalone AST/CFG SVG and PNG. SVG embeds colors, labels, arrows and a background without external resources. Labels are escaped and bounded. PNG scales 1–4 are checked before allocating a canvas: at most 8,192 pixels per side and 16 megapixels (up to 64 MiB for the raw RGBA canvas, plus browser encoding/decode overhead). Oversize exports ask for a smaller scale or SVG. PNG decoding uses a data URL permitted by the GUI image policy. Existing inheritance exports remain available.
