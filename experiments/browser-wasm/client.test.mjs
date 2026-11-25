// SPDX-License-Identifier: AGPL-3.0-or-later
import test from "node:test";
import assert from "node:assert/strict";
import { WorkerClient } from "./client.mjs";

class FakeWorker {
  postMessage(message) { this.last = message; }
  terminate() { this.terminated = true; }
  emit(data) { this.onmessage({ data }); }
}
function setup(t) {
  const workers = [];
  const statuses = [];
  const client = new WorkerClient(() => { const worker = new FakeWorker(); workers.push(worker); return worker; }, (s) => statuses.push(s));
  t.after(() => client.stop());
  return { client, workers, statuses };
}
test("cancel rejects pending work and ignores the previous worker", async (t) => {
  const { client, workers } = setup(t);
  workers[0].emit({ ready: true });
  const old = client.request({ action: "run" });
  const cancelled = assert.rejects(old, /cancelled/);
  await Promise.resolve();
  client.restart();
  await cancelled;
  assert.equal(workers[0].terminated, true);
  workers[0].emit({ id: 1, state: "stale" });
  workers[1].emit({ ready: true });
  const current = client.request({ action: "compile" });
  await Promise.resolve();
  workers[1].emit({ id: workers[1].last.id, state: "fresh" });
  assert.equal((await current).state, "fresh");
});
test("watchdog terminates a worker that cannot process cancellation messages", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const { client, workers, statuses } = setup(t);
  workers[0].emit({ ready: true });
  const pending = client.request({ action: "run" });
  const rejected = assert.rejects(pending, /exceeded 5 seconds/);
  await Promise.resolve();
  t.mock.timers.tick(5001);
  await rejected;
  assert.equal(workers[0].terminated, true);
  assert.match(statuses.at(-1).error, /exceeded/);
	await assert.rejects(client.request({ action: "compile" }), /Runtime stopped/);
});
test("startup timeout rejects waiting calls and permits restart", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const { client, workers } = setup(t);
  const rejected = assert.rejects(client.request({ action: "compile" }), /startup exceeded/);
  t.mock.timers.tick(8001);
  await rejected;
  client.restart();
  workers[1].emit({ ready: true });
  await client.ready;
});
