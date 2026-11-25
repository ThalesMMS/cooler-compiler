// SPDX-License-Identifier: AGPL-3.0-or-later
// Local validation proxy: adds a DOM-driven test to the real GUI, without a production test hook.
import {createServer} from 'node:http';import {readFile} from 'node:fs/promises';
createServer(async(req,res)=>{try{
 if(req.url==='/editor-integration.mjs'){const contents=await readFile(new URL('../experiments/editor-integration.mjs',import.meta.url));res.writeHead(200,{'Content-Type':'text/javascript'}).end(contents);return;}
 const chunks=[];for await(const c of req)chunks.push(c);
 const headers={...req.headers};delete headers.host;delete headers.connection;delete headers['content-length'];if(headers.origin)headers.origin='http://127.0.0.1:9094';
 const upstream=await fetch('http://127.0.0.1:9094'+req.url,{method:req.method,headers,body:chunks.length?Buffer.concat(chunks):undefined});
 const type=upstream.headers.get('content-type')||'';let body=Buffer.from(await upstream.arrayBuffer());if(type.includes('text/html'))body=Buffer.from(body.toString().replace('</body>','<script type="module" src="/editor-integration.mjs"></script></body>'));
 const responseHeaders=Object.fromEntries(upstream.headers);delete responseHeaders['content-length'];delete responseHeaders['content-encoding'];res.writeHead(upstream.status,responseHeaders).end(body);
 }catch(e){res.writeHead(500).end(String(e));}}).listen(9095,'127.0.0.1');
