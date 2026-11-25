// SPDX-License-Identifier: AGPL-3.0-or-later
import {build} from '../experiments/editor-comparison/node_modules/esbuild/lib/main.js';
import {stat,readFile} from 'node:fs/promises';import {gzipSync} from 'node:zlib';
await build({entryPoints:['experiments/editor-comparison/main.mjs'],bundle:true,minify:true,format:'esm',outfile:'dist/editor-comparison.mjs'});
console.log({bytes:(await stat('dist/editor-comparison.mjs')).size,gzipBytes:gzipSync(await readFile('dist/editor-comparison.mjs')).length});
