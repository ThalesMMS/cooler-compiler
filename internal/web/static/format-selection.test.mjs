// SPDX-License-Identifier: AGPL-3.0-or-later
import test from 'node:test';
import assert from 'node:assert/strict';
import { mapFormatSelection } from './format-selection.mjs';
test('format maps a selected Unicode token past changed whitespace', () => {
  const before='class Main{main():Object{"😀"};};';
  const after='class Main {\n  main(): Object {\n    "😀"\n  };\n};\n';
  const start=before.indexOf('"😀"'), end=start+'"😀"'.length;
  assert.equal(after.slice(mapFormatSelection(before,after,start),mapFormatSelection(before,after,end,'left')), '"😀"');
});
test('selection inside preserved string spaces remains intact', () => {
  const before='class Main{main():Object{"hello   world"};};';
  const after='class Main {\n  main(): Object {\n    "hello   world"\n  };\n};\n';
  const start=before.indexOf('   ');
  assert.equal(after.slice(mapFormatSelection(before,after,start),mapFormatSelection(before,after,start+3,'left')),'   ');
});
