# Inspect exported JSON reports

Use **Open report** in the GUI and choose a JSON file or paste its contents.
Inspection imports artifacts through the public `DecodeResult` facade and never
creates a VM session. Program output is a recorded snapshot, not new execution.
The banner identifies unavailable source contents and major absent artifacts;
existing tabs render the diagnostics, tokens, AST, IR, assembly and runtime data
that are present. Imported content uses the existing escaped renderers.

The endpoint requires the GUI capability, versioned compatible schema, valid
typed data, at most 1 MiB of JSON, nesting at most 64 and at most 50,000 JSON
values. Unknown session/token fields are discarded by the typed decoder. Sources
are accepted only when their identity, order, byte size and SHA-256 match the
report's metadata (up to 16 files). GUI JSON exports now include a `sources`
extension. Older single-file reports can recover their `source` field; older
multi-file reports containing only metadata remain inspectable but cannot be
recompiled without source contents. ZIP import is not supported.

The current documents and selection remain in a separate in-memory workspace.
Inspection makes imported sources read-only and disables mutation/execution,
including keyboard shortcuts and quick fixes. **Recompile imported sources**
explicitly creates a fresh compilation using only the imported workspace. It
does not run the program. **Return to my documents** disposes that fresh session
and restores the original documents. If imported sources were edited, returning
asks before discarding those edits. Save imported edits explicitly before leaving.
Inspection never replaces the original durable recovery copy; the temporary
imported workspace is not automatically persisted as recovery data.

Validation on Windows: export/decode roundtrips, multi-file source hashes,
metadata-only reports, malformed/incompatible/oversized/deep JSON, stripped
session fields and endpoint capability tests. Full Go tests, vet, pinned lint
and documentation checks were run. In the real in-app browser, invalid runtime
data produced an inline error; a valid snapshot displayed recorded output and
absent-artifact notices with read-only source and disabled execution. Explicit
recompile produced a new READY session with zero executed instructions. Returning
restored the original unsaved source and selection. Native file-picker automation,
native WebView and macOS validation were unavailable; the browser's JSON paste
path exercised the same importer and state transition.
