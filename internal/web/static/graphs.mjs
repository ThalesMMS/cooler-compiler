// SPDX-License-Identifier: AGPL-3.0-or-later
const LIMIT=200;
const escape=value=>String(value??'').replaceAll('&','&amp;').replaceAll('<','&lt;').replaceAll('>','&gt;').replaceAll('"','&quot;').replaceAll("'",'&#39;');
export function layoutGraph(result,kind){
 const nodes=[],edges=[];let truncated=false;
 if(kind==='ast' && result?.ast){
  const queue=[{node:result.ast,depth:0,parent:null}];let at=0;
  while(at<queue.length && nodes.length<LIMIT){
   const {node,depth,parent}=queue[at++],id=nodes.length;
   nodes.push({id,label:[node.kind,node.name || node.text,node.type].filter(Boolean).join(' · ').slice(0,160),span:node.span,depth});
   if(parent!==null)edges.push({from:parent,to:id});
   for(const child of (node.children || [])){if(queue.length>=LIMIT+1){truncated=true;break;}queue.push({node:child,depth:depth+1,parent:id});}
  }
  truncated ||= at<queue.length;
 }else if(kind==='cfg'){
  const blocks=result?.controlFlow?.blocks || [];truncated=blocks.length>LIMIT;
  const ids=new Map();
  for(const block of blocks.slice(0,LIMIT)){const id=nodes.length;ids.set(block.name,id);nodes.push({id,label:`${block.method || 'program'} · ${block.name} · IR ${block.range || ''}`.slice(0,160),span:block.span,depth:0});}
  const sourceEdges=result?.controlFlow?.edges || [];
  for(const edge of sourceEdges.slice(0,1000)){if(ids.has(edge.from) && ids.has(edge.to))edges.push({from:ids.get(edge.from),to:ids.get(edge.to),label:String(edge.kind || '').slice(0,40)});}
  truncated ||= sourceEdges.length>1000;
 }
 // Bounded breadth-first order for AST; stable source order for cyclic CFG.
 const columns=Math.min(5,Math.max(1,Math.ceil(Math.sqrt(nodes.length))));
 nodes.forEach((node,i)=>{node.x=30+(i%columns)*250;node.y=30+Math.floor(i/columns)*100;});
 return {kind,nodes,edges,truncated,width:columns*250+30,height:Math.max(150,Math.ceil(nodes.length/columns)*100+30)};
}
export function graphSVG(graph,dark=false){
 const bg=dark?'#171b25':'#ffffff',fg=dark?'#eef1f7':'#172033',edge=dark?'#a0a9be':'#59647a',fill=dark?'#252c3e':'#eef2f9';
 const paths=graph.edges.map((item,i)=>{
  const a=graph.nodes[item.from],b=graph.nodes[item.to];if(!a || !b)return '';
  const startX=a.x+110,startY=a.y+56,endX=b.x+110,endY=b.y;
  const d=item.from===item.to?`M ${a.x+220} ${a.y+15} C ${a.x+245} ${a.y-30}, ${a.x+245} ${a.y+85}, ${a.x+220} ${a.y+40}`:`M ${startX} ${startY} C ${startX+((i%3)-1)*20} ${startY+40}, ${endX} ${endY-35}, ${endX} ${endY}`;
  return `<path d="${d}" fill="none" stroke="${edge}" stroke-width="1.5" marker-end="url(#arrow-${graph.kind})"><title>${escape(item.label || 'child')}</title></path>`;
 }).join('');
 const boxes=graph.nodes.map(node=>`<g data-graph-node="${node.id}" tabindex="0" role="button" aria-label="${escape(node.label)}"><title>${escape(node.label)}</title><rect x="${node.x}" y="${node.y}" width="220" height="56" rx="6" fill="${fill}" stroke="${edge}"/><text x="${node.x+10}" y="${node.y+23}" fill="${fg}" font-family="sans-serif" font-size="12">${escape(node.label.slice(0,28))}</text><text x="${node.x+10}" y="${node.y+42}" fill="${fg}" font-family="sans-serif" font-size="11">${escape(node.label.slice(28,58))}</text></g>`).join('');
 return `<svg xmlns="http://www.w3.org/2000/svg" width="${graph.width}" height="${graph.height}" viewBox="0 0 ${graph.width} ${graph.height}" role="group" aria-label="${graph.kind.toUpperCase()} graph"><title>${graph.kind.toUpperCase()} graph</title><defs><marker id="arrow-${graph.kind}" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z" fill="${edge}"/></marker></defs><rect width="100%" height="100%" fill="${bg}"/>${paths}${boxes}</svg>`;
}
export function graphHTML(result,kind,dark=false){
 const graph=layoutGraph(result,kind);
 return `<section class="graph-view" data-graph-kind="${kind}" aria-label="${kind.toUpperCase()} visualization"><div class="graph-controls"><button type="button" data-graph-action="in" aria-label="Zoom in">+</button><button type="button" data-graph-action="out" aria-label="Zoom out">−</button><button type="button" data-graph-action="fit">Fit graph</button><span>Drag empty space to pan. Arrow keys pan; +/− zoom; Home fits. Enter selects source.</span></div><p role="status">${graph.nodes.length} nodes${graph.truncated?' · Display limited to 200 nodes / 1,000 edges. Export uses this same bounded graph.':''}</p><div class="graph-viewport" tabindex="0" aria-label="Pan and zoom graph">${graphSVG(graph,dark)}</div><details><summary>Accessible node table</summary><table><thead><tr><th>Node</th><th>Source</th></tr></thead><tbody>${graph.nodes.map(node=>`<tr><td><button type="button" data-graph-node="${node.id}">${escape(node.label)}</button></td><td>${escape(node.span?.sourceId || 'generated')}:${escape(node.span?.startLine || 1)}</td></tr>`).join('')}</tbody></table></details></section>`;
}
const cameras=new Map();
export function bindGraphs(root,result,onSelect){
 for(const panel of root.querySelectorAll('.graph-view')){
  const graph=layoutGraph(result,panel.dataset.graphKind),viewport=panel.querySelector('.graph-viewport'),svg=viewport.querySelector('svg');
  const camera=cameras.get(graph.kind) || {x:0,y:0,zoom:1};cameras.set(graph.kind,camera);
  const render=()=>svg.setAttribute('viewBox',`${camera.x} ${camera.y} ${graph.width/camera.zoom} ${graph.height/camera.zoom}`);
  const action=key=>{if(key==='fit'){camera.x=0;camera.y=0;camera.zoom=1;}else camera.zoom=Math.max(0.25,Math.min(8,camera.zoom*(key==='in'?1.25:0.8)));render();};
  panel.querySelectorAll('[data-graph-action]').forEach(button=>button.addEventListener('click',()=>action(button.dataset.graphAction)));
  panel.querySelectorAll('[data-graph-node]').forEach(node=>{
   const select=event=>{event.stopPropagation();const span=graph.nodes[Number(node.dataset.graphNode)]?.span;if(span)onSelect(span);};
   node.addEventListener('click',select);if(node.tagName.toLowerCase()==='g')node.addEventListener('keydown',event=>{if(event.key==='Enter' || event.key===' '){event.preventDefault();select(event);}});
  });
  viewport.addEventListener('keydown',event=>{
   const key=event.key;if(!['ArrowLeft','ArrowRight','ArrowUp','ArrowDown','+','=','-','Home'].includes(key))return;event.preventDefault();
   if(key==='Home')action('fit');else if(['+','=','-'].includes(key))action(key==='-'?'out':'in');else{camera.x+=(key==='ArrowRight'?60:key==='ArrowLeft'?-60:0)/camera.zoom;camera.y+=(key==='ArrowDown'?60:key==='ArrowUp'?-60:0)/camera.zoom;render();}
  });
  let drag=null;
  viewport.addEventListener('pointerdown',event=>{if(event.target.closest('[data-graph-node]'))return;const matrix=svg.getScreenCTM();drag={x:event.clientX,y:event.clientY,cx:camera.x,cy:camera.y,sx:matrix.a,sy:matrix.d};viewport.setPointerCapture(event.pointerId);});
  viewport.addEventListener('pointermove',event=>{if(!drag)return;camera.x=drag.cx-(event.clientX-drag.x)/drag.sx;camera.y=drag.cy-(event.clientY-drag.y)/drag.sy;render();});
  viewport.addEventListener('pointerup',()=>{drag=null;});viewport.addEventListener('pointercancel',()=>{drag=null;});render();
 }
}
export function rasterSize(graph,scale){
 if(![1,2,3,4].includes(scale))throw new Error('PNG scale must be 1, 2, 3 or 4.');
 const width=Math.ceil(graph.width*scale),height=Math.ceil(graph.height*scale);
 if(width>8192 || height>8192 || width*height>16777216)throw new Error('PNG exceeds 8192 pixels per side or 16 megapixels. Use a smaller scale or SVG.');
 return {width,height};
}
export async function graphPNG(graph,scale,dark=false){
 const {width,height}=rasterSize(graph,scale),url=`data:image/svg+xml;charset=utf-8,${encodeURIComponent(graphSVG(graph,dark))}`;
  const image=new Image();image.src=url;await image.decode();const canvas=document.createElement('canvas');canvas.width=width;canvas.height=height;
  canvas.getContext('2d').drawImage(image,0,0,width,height);return await new Promise((resolve,reject)=>canvas.toBlob(blob=>blob?resolve(blob):reject(new Error('PNG export failed')),'image/png'));
}
