/* SPDX-License-Identifier: AGPL-3.0-or-later */
import test from 'node:test';
import assert from 'node:assert/strict';
import { draftWorkspace, createDraftWriter } from './drafts.mjs';
test('a missing revision stops saves without losing the last known revision', async () => {
  const errors=[];
  const writer=createDraftWriter({save:async()=>({}),status:(message,error)=>{if(error)errors.push(message);}});
  writer.start(4);writer.schedule('draft');await writer.flush();
  assert.equal(writer.revision(),4);
  assert.equal(writer.schedule('next'),false);
  assert.equal(errors.length,1);
});
test('recovery stores only ordered names/content and active tab', () => {
  assert.deepEqual(draftWorkspace([{name:'a.cl',source:'unsaved',id:'session',token:'secret',savedSource:'old'}],0),
    {version:1,active:0,tabs:[{name:'a.cl',content:'unsaved'}]});
});
test('writes debounce, serialize revisions and stop after unavailable storage', async (t) => {
  t.mock.timers.enable({apis:['setTimeout']});
  const calls=[],done=[],errors=[],statuses=[];
  const writer=createDraftWriter({save(value){calls.push(value);return new Promise((resolve,reject)=>done.push({resolve,reject}));},status(message,error){statuses.push(message);if(error)errors.push(message);}});
  writer.schedule('blocked');t.mock.timers.tick(300);assert.equal(calls.length,0);
  writer.start(4);writer.schedule('a');writer.schedule('b');t.mock.timers.tick(300);
  writer.schedule('c');t.mock.timers.tick(300);assert.deepEqual(calls,[{action:'save',revision:4,workspace:'b'}]);
  done.shift().resolve({revision:5});await Promise.resolve();await Promise.resolve();
  assert.equal(statuses.at(-1),'Saving recovery copy…');
  t.mock.timers.tick(300);
  assert.deepEqual(calls[1],{action:'save',revision:5,workspace:'c'});
  done.shift().reject(new Error('quota'));await Promise.resolve();await Promise.resolve();
  writer.schedule('d');t.mock.timers.tick(300);assert.equal(calls.length,2);assert.deepEqual(errors,['quota']);
});
