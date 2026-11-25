// SPDX-License-Identifier: AGPL-3.0-or-later
import {createServer} from 'node:http';
import {readFile} from 'node:fs/promises';
const root=new URL('../',import.meta.url);
const allowed=new Set(['/experiments/graph-validation.html','/internal/web/static/graphs.mjs']);
createServer(async(request,response)=>{
 const pathname=new URL(request.url,'http://localhost').pathname;
 if(request.method!=='GET' || !allowed.has(pathname)){response.writeHead(404).end();return;}
 try{const contents=await readFile(new URL(pathname.slice(1),root));response.writeHead(200,{'Content-Type':pathname.endsWith('.html')?'text/html':'text/javascript','Cache-Control':'no-store'}).end(contents);}catch{response.writeHead(404).end();}
}).listen(9096,'127.0.0.1',()=>console.log('http://127.0.0.1:9096/experiments/graph-validation.html'));
