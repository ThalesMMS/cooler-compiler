/* SPDX-License-Identifier: AGPL-3.0-or-later */
import test from 'node:test';
import assert from 'node:assert/strict';
import { createHistorySeeker } from './history-seek.mjs';

test('coalesces drag destinations and rejects older in-flight responses', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const calls = [], applied = [], busy = [], complete = [];
  const submit = createHistorySeeker({
    request(value) { calls.push(value); return new Promise(resolve => complete.push(resolve)); },
    apply(value) { applied.push(value); },
    fail(error) { throw error; },
    busy(value) { busy.push(value); },
  });
  submit(1); submit(2); submit(3);
  t.mock.timers.tick(80);
  assert.deepEqual(calls, [3]);
  submit(4); submit(9);
  complete.shift()('old');
  await Promise.resolve(); await Promise.resolve();
  assert.deepEqual(applied, []);
  t.mock.timers.tick(80);
  assert.deepEqual(calls, [3, 9]);
  complete.shift()('latest');
  await Promise.resolve(); await Promise.resolve();
  assert.deepEqual(applied, ['latest']);
  assert.deepEqual(busy, [true, false]);
});
