// SPDX-License-Identifier: AGPL-3.0-or-later
import {createServer} from 'node:http';import {readFile} from 'node:fs/promises';
const allowed=new Set(['/experiments/editor-comparison/index.html','/dist/editor-comparison.mjs','/experiments/editor-validation.html','/internal/web/static/editor-tools.mjs']);
createServer(async(req,res)=>{const path=new URL(req.url,'http://localhost').pathname;if(req.method!=='GET'||!allowed.has(path)){res.writeHead(404).end();return;}try{const contents=await readFile(new URL('..'+path,import.meta.url));res.writeHead(200,{'Content-Type':path.endsWith('.html')?'text/html':'text/javascript','Cache-Control':'no-store'}).end(contents);}catch{res.writeHead(404).end();}}).listen(9093,'127.0.0.1');
