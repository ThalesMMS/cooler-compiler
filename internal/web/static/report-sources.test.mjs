// SPDX-License-Identifier: AGPL-3.0-or-later
import test from 'node:test';
import assert from 'node:assert/strict';
import { reportSources } from './report-sources.mjs';
test('export preserves compiled identity after Save As or imported name normalization',()=>{
  const report={schemaVersion:"1.5.0",sourceUnits:[{id:'source-2',name:'original'}]};
  assert.deepEqual(reportSources(report,[{id:'source-2',name:'new-name.cl',source:'text'}]),{sources:[{id:'source-2',name:'original',content:'text'}]});
  assert.deepEqual(reportSources(report,[{id:'placeholder',name:'unavailable.cl',source:''}]),{});
});

test("older reports keep their advertised schema without source extensions",()=>{assert.deepEqual(reportSources({schemaVersion:"1.4.0",sourceUnits:[{id:"a",name:"a.cl"}]},[{id:"a",source:"text"}]),{});});
