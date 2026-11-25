# Cooler COOL for VS Code

Local COOL highlighting and a stdio language client. Build `cooler-lsp` and
`coolerc` from this repository, install the local VSIX, and configure absolute
`cooler.lspPath` and `cooler.compilerPath` paths (or put the binaries on PATH).
The extension never downloads or executes a remote service. Processes start
only in trusted local workspaces; no support is advertised for virtual workspaces.

Open `.cl` or `.cool` to start the server. Configure `cooler.sourceSet` with exact
relative files or directory prefixes ending in `/`; all selected files form one
COOL program. Use `Cooler: Restart Language Server` after changing binary setup.
`Cooler: Check Project` and `Cooler: Run Project` require saved source files and
show compiler output and exit status in **Cooler commands**, separately from LSP
logs. Runs are limited to ten seconds and 1 MiB of output. No interactive stdin
is supplied; use the CLI, GUI or DAP debugger for interactive programs.

The LSP supplies diagnostics, completion, hover, navigation, semantic tokens,
QuickFix, formatting, safe rename, signature help and opt-in inlay hints.
Build `cooler-dap` and configure `cooler.dapPath` to enable **Run and Debug → Cooler COOL**.
The default debug launch uses the selected source set; explicit `sources` supports
multifile programs. Line breakpoints, source steps, locals/self and pause/continue
are supported. At an input stop, enter `:input 42` in Debug Console, then Continue.
Reverse execution, watches and variable mutation are not advertised.

Build locally with `npm ci`, `npm run package`, then **Extensions: Install from
VSIX** using `dist/cooler-local.vsix`. No Marketplace publication is performed.
The extension is AGPL-3.0-or-later; bundled library notices are included.
