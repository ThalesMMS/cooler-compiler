// SPDX-License-Identifier: AGPL-3.0-or-later
import test from 'node:test';
import assert from 'node:assert/strict';
import { layoutGraph, graphSVG, graphHTML, rasterSize } from './graphs.mjs';
test('accessible node table escapes source line values',()=>{
 const html=graphHTML({ast:{kind:'Class',span:{startLine:'<img src=x onerror=alert(1)>'}}},'ast');
 assert(!html.includes('<img'));
 assert(html.includes('&lt;img'));
 assert(graphHTML({ast:{kind:'Class'}},'ast').includes('generated:1'));
});
test('AST layout is deterministic, bounded and escapes labels',()=>{
 const result={ast:{kind:'Program',children:[{kind:'String',text:'<script>&"',span:{sourceId:'one'}},{kind:'Class',span:{sourceId:'two'}}]}};
 const graph=layoutGraph(result,'ast');assert.deepEqual(graph,layoutGraph(result,'ast'));assert.equal(graph.edges.length,2);
 const svg=graphSVG(graph);assert(!svg.includes('<script>'));assert(svg.includes('&lt;script&gt;'));assert(!svg.includes('http://') || svg.includes('http://www.w3.org/2000/svg'));
 const huge=layoutGraph({ast:{kind:'Program',children:Array.from({length:1000},()=>({kind:'Int'}))}},'ast');assert.equal(huge.nodes.length,200);assert(huge.truncated);
});
test('CFG cycles and multiple methods stay bounded without recursive traversal',()=>{
 const graph=layoutGraph({controlFlow:{blocks:[{name:'A',method:'one'},{name:'B',method:'two'}],edges:[{from:'A',to:'B'},{from:'B',to:'A'},{from:'B',to:'B'}]}},'cfg');assert.equal(graph.edges.length,3);assert(graphSVG(graph).includes('<path'));
 assert.equal(layoutGraph({},'cfg').nodes.length,0);
 assert.deepEqual(rasterSize({width:400,height:300},2),{width:800,height:600});
 assert.throws(()=>rasterSize({width:20000,height:20000},4));assert.throws(()=>rasterSize(graph,0));
});
