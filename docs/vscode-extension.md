# Local VS Code extension

The extension lives in `extensions/vscode-cooler`. Build the local binaries with
`go build -o dist/cooler-lsp.exe ./cmd/cooler-lsp` and
`go build -o dist/coolerc.exe ./cmd/coolerc` on Windows; omit `.exe` on Unix.
Run `npm ci` and `npm run package` in the extension directory to produce
`dist/cooler-local.vsix`. Install via **Extensions: Install from VSIX**. Set
absolute `cooler.lspPath` and `cooler.compilerPath` settings, or put binaries on
PATH. The VSIX contains JavaScript, grammar, configuration and licenses; it does
not bundle/download platform binaries or publish to Marketplace.

Only trusted local workspaces can activate processes. Syntax highlighting and
language associations are declarative. The language server uses stdin/stdout;
compiler command output and exit codes use a separate **Cooler commands** channel.
Commands refuse unsaved selected sources and enforce ten seconds/1 MiB output.
One command runs at a time. Restarts are serialized, file watchers are disposed
with the extension and workspace-folder changes restart the server. File URIs
are normalized through Node's file URL conversion to match the server on Windows.

Select one COOL program using `cooler.sourceSet.include` and `.exclude`: exact
relative files or directory prefixes ending with `/`. Both editor and commands
exclude `.git`, `node_modules`, `vendor`, `dist` and `bin`. The 1,024-file command
bound applies after source selection. In multi-root workspaces, put shared binary
and source-set settings in the `.code-workspace` settings rather than individual
folder settings; all roots form one program. Warning selectors and each inlay
category are configurable. Debugger/F5 integration is provided by the local [DAP adapter](dap-debugger.md).
