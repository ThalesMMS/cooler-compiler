// SPDX-License-Identifier: AGPL-3.0-or-later
const path=require('node:path');
const fs=require('node:fs');
require('esbuild').buildSync({entryPoints:['src/extension.cjs'],bundle:true,platform:'node',format:'cjs',external:['vscode'],outfile:'out/extension.cjs'});
fs.copyFileSync(path.join(path.dirname(require.resolve('vscode-languageclient/node')),'lib/node/terminateProcess.sh'),'out/terminateProcess.sh');
