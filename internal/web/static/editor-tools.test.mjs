// SPDX-License-Identifier: AGPL-3.0-or-later
import test from 'node:test';import assert from 'node:assert/strict';
import {indentEdit,mapBreakpointLine,nextDiagnosticIndex} from './editor-tools.mjs';
test('multiline indent preserves UTF-16 selection and excludes end-line boundary',()=>{
 assert.deepEqual(indentEdit('😀a\nb\nc',0,6,false),{from:0,to:6,text:'  😀a\n  b\n',start:2,end:10});
 assert.deepEqual(indentEdit('  a\n b',0,6,true),{from:0,to:6,text:'a\nb',start:0,end:3});
});
test('breakpoints follow unchanged text, deleted lines disappear',()=>{
 assert.equal(mapBreakpointLine('a\nb\nc','new\na\nb\nc',2),3);
 assert.equal(mapBreakpointLine('a\nb\nc','a\nc',2),null);
 assert.equal(mapBreakpointLine('a\nb\nc','a\nc',3),2);
 assert.equal(mapBreakpointLine('a\nb','a\nb!',2),2);
});
test('diagnostic navigation wraps in either direction',()=>{
 assert.equal(nextDiagnosticIndex(3,-1,1),0);assert.equal(nextDiagnosticIndex(3,-1,-1),2);assert.equal(nextDiagnosticIndex(0,0,1),-1);
});

test('history isolates source identities, invalidates redo and bounds total text',async()=>{
 const {EditorHistory}=await import('./editor-tools.mjs');const h=new EditorHistory(60);const snap=text=>({text,start:0,end:0});
 h.record('a',snap('😀'),snap('😀a'));h.record('b',snap('B'),snap('BB'));assert.equal(h.move('a').text,'😀');assert.equal(h.move('b').text,'B');assert.equal(h.move('a',true).text,'😀a');
 h.record('a',snap('😀a'),snap('next'));assert.equal(h.move('a',true),null);h.record('c',snap('x'.repeat(50)),snap('y'.repeat(50)));assert.ok(h.bytes()<=60);
});

test('history counts redo entries and isolates identical public source IDs',async()=>{
 const {EditorHistory,breakpointLineForSource}=await import('./editor-tools.mjs');const h=new EditorHistory();const a=Symbol('source-1'),b=Symbol('source-1'),snap=text=>({text,start:0,end:0});
 for(let i=0;i<100;i++)h.record(a,snap('a'+i),snap('a'+(i+1)));for(let i=0;i<100;i++)h.move(a);
 for(let i=0;i<100;i++)h.record(b,snap('b'+i),snap('b'+(i+1)));assert.equal(h.past.length+h.future.length,100);assert.equal(h.move(a),null);assert.equal(h.move(b).text,'b99');
 assert.equal(breakpointLineForSource('a:b:2','a'),null);assert.equal(breakpointLineForSource('a:b:2','a:b'),2);assert.equal(breakpointLineForSource('a:NaN','a'),null);
});
