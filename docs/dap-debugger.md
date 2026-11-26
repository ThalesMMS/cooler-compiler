# Local DAP debugger

Build `go build -o dist/cooler-dap.exe ./cmd/cooler-dap` on Windows (omit `.exe` on Unix). Set `cooler.dapPath` to its absolute path, alongside the LSP/compiler settings in the local VS Code extension. Rebuild/install `dist/cooler-local.vsix` with `npm run package` in `extensions/vscode-cooler`. No server, CDN, automatic binary download or Marketplace publication is involved.

Choose **Cooler COOL** in Run and Debug, then **Debug selected COOL project**. The default launch uses `cooler.sourceSet` in the selected workspace folder. All selected files are compiled together. Save edited sources before launch. For explicit multifile programs:

```json
{
  "version": "0.2.0",
  "configurations": [{
    "type": "cooler",
    "request": "launch",
    "name": "Local COOL",
    "sources": ["${workspaceFolder}/main.cl", "${workspaceFolder}/helper.cl"],
    "stopOnEntry": true,
    "input": "",
    "maxSteps": 100000
  }]
}
```

`program` selects one file; `sources` selects an ordered list. With neither, the extension resolves the selected source set. One trusted local workspace folder supplies the default set and working directory; explicit source paths may span folders. Virtual/untrusted workspaces and unsaved selected documents are rejected.

## Supported behavior

Launch/configuration, Continue, Pause, Step Into/Over/Out, line breakpoints, one VM thread, stack frames, read-only locals/self, input/output and termination are implemented over the public `compiler.DebugSession` API. Sources use native absolute paths, one-based lines/columns and UTF-16 spans. Symbolic links are resolved consistently. A line breakpoint moves to the next executable method position in its source; unsupported conditions, hit counts, logpoints or columns remain unverified. Frames and variable references expire at the next stop.

Preloaded `input` is consumed first. Exhausted input suspends at `IO.in_int` or `IO.in_string`. In **Debug Console**, enter `:input 42` (or `:input` followed by one space for an empty line), then Continue. This is a dedicated input command, not expression evaluation. Output is emitted through DAP stdout events exactly once per appended segment. The custom request `cooler/input` accepts a `line` for other local clients.

Pause is cooperative: Continue runs in blocks of at most 1,024 instructions and observes the pause flag between blocks. A source step observes pause when its bounded operation returns. Neither path invokes the VM's terminal cancellation mechanism for ordinary Pause. Runtime errors remain inspectable at an exception stop; Continue then ends with exit code 1. Hard time cancellation can produce COL6056 and requires a fresh launch, consistently with the public VM contract. Disconnect cancels ongoing work and the client owns transport/process teardown.

Reverse execution, historical suspension, watches, hover evaluation, object expansion, variable mutation, conditional/function/data/instruction breakpoints and restart are not advertised. Start another session to restart. No historical state is exposed, so active frame/variable IDs always refer to the current stop.

## Deterministic limits

- 1–64 regular source files, at most 1 MiB total; duplicate canonical paths and FIFOs are rejected.
- DAP input frames at most 1 MiB and headers at most 8 KiB; malformed/duplicate lengths fail closed.
- Compilation timeout 10 seconds; each execution command 5 seconds and 1–100,000 instructions. Total VM ceiling: 1,000,000 instructions, 128 call frames, 256 breakpoints, 64 KiB output and bounded trace retention.
- Each locals response shows self and at most 512 locals. Display values are UTF-8-safe previews capped at 1,024 bytes, names at 256 and types at 128, with an ellipsis for truncation. Values are read-only and do not recursively expand objects.
- Protocol messages go only to stdout; adapter startup/transport failures go to stderr. The process handles one launch and remains until the client closes its transport after disconnect.
