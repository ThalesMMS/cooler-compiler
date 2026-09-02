/* SPDX-License-Identifier: AGPL-3.0-or-later */

import assert from "node:assert/strict";
import test from "node:test";

import { bootstrapCoolerApp } from "./app-bootstrap.mjs";
import {
  beginAutoRun,
  createAppState,
  finishAutoRun,
  invalidateActiveSession,
  requestRunPause,
	shouldAcceptTimeline,
} from "./app-state.mjs";

function fakeDocument() {
  const children = [];
  const status = { textContent: "Ready" };
  const region = { appendChild: (child) => children.push(child) };
  return {
    children,
    status,
    getElementById: (id) => ({ globalStatus: status, toastRegion: region }[id] || null),
    createElement: () => ({
      className: "",
      attributes: {},
      setAttribute(name, value) { this.attributes[name] = value; },
      textContent: "",
    }),
  };
}

test("bootstrap loads and starts the application module", async () => {
  let started = 0;
  const result = await bootstrapCoolerApp({
    loadApp: async () => ({ startCoolerApp: async () => { started += 1; } }),
    documentRef: fakeDocument(),
  });
  assert.equal(result, true);
  assert.equal(started, 1);
});

test("module or initialization failures are visible in the interface", async () => {
  const documentRef = fakeDocument();
  const result = await bootstrapCoolerApp({
    loadApp: async () => { throw new Error("module unavailable"); },
    documentRef,
  });
  assert.equal(result, false);
  assert.match(documentRef.status.textContent, /module unavailable/);
  assert.equal(documentRef.children.length, 1);
  assert.equal(documentRef.children[0].attributes.role, "alert");
  assert.match(documentRef.children[0].textContent, /Could not start Cooler Compiler Lab/);
});

test("session invalidation defers disposal while Run is active", () => {
  const state = createAppState();
  state.pipeline = { id: "session-1", snapshot: { complete: true } };
  state.result = { source: "class Main {};" };
  state.runtime = { executedSteps: 25 };
  beginAutoRun(state);

  const invalidated = invalidateActiveSession(state);

  assert.deepEqual(invalidated, { id: "session-1", disposeImmediately: false });
  assert.equal(state.sessionEpoch, 1);
  assert.equal(state.pauseRequested, true);
  assert.equal(state.deferredDisposals.has("session-1"), true);
  assert.deepEqual(state.pipeline, { id: "", snapshot: null });
  assert.equal(state.result, null);
  assert.equal(state.runtime, null);
});

test("session invalidation disposes immediately while Run is inactive", () => {
  const state = createAppState();
  state.pipeline = { id: "session-1", snapshot: { complete: true } };

  const invalidated = invalidateActiveSession(state);

  assert.deepEqual(invalidated, { id: "session-1", disposeImmediately: true });
  assert.equal(state.sessionEpoch, 1);
  assert.equal(state.pauseRequested, false);
  assert.equal(state.deferredDisposals.size, 0);
});

test("Run and Pause state transitions are deterministic", () => {
  const state = createAppState();
  assert.equal(requestRunPause(state), false);
  beginAutoRun(state);
  assert.equal(state.autoRunning, true);
  assert.equal(state.pauseRequested, false);
  assert.equal(requestRunPause(state), true);
  assert.equal(state.pauseRequested, true);
  finishAutoRun(state);
  assert.equal(state.autoRunning, false);
  assert.equal(state.pauseRequested, false);
});

test("timeline revisions reject stale reverse responses", () => {
	assert.equal(shouldAcceptTimeline(null, { revision: 1 }), true);
	assert.equal(shouldAcceptTimeline({ revision: 3 }, { revision: 3 }), true);
	assert.equal(shouldAcceptTimeline({ revision: 3 }, { revision: 4 }), true);
	assert.equal(shouldAcceptTimeline({ revision: 3 }, { revision: 2 }), false);
	assert.equal(shouldAcceptTimeline({ revision: 3 }, null), true);
});

test("session invalidation preserves debugger configuration but clears derived values", () => {
  const state = createAppState();
  state.breakpoints.add("source-1:4");
  state.breakpointConfigs.set("source-1:4", { condition: "i = 1" });
  state.watches.push({ id: "watch-1", expression: "i", enabled: true });
  state.breakpointStates.set("source-1:4", { hits: 2 });
  state.watchResults = [{ id: "watch-1", value: { display: "1" } }];
  state.debugLog = [{ order: 1, message: "i=1" }];
	state.heap = { mode: "graph", objects: [{ id: "Main#1" }] };
	state.heapLoading = true;
	state.timeline = { revision: 3, currentStep: 12 };

  invalidateActiveSession(state);

  assert.equal(state.breakpoints.has("source-1:4"), true);
  assert.equal(state.breakpointConfigs.get("source-1:4").condition, "i = 1");
  assert.equal(state.watches.length, 1);
  assert.equal(state.breakpointStates.size, 0);
  assert.deepEqual(state.watchResults, []);
  assert.deepEqual(state.debugLog, []);
	assert.equal(state.heap, null);
	assert.equal(state.heapLoading, false);
	assert.equal(state.timeline, null);
});

test("rapid compile/edit/run replacement never restores an obsolete client epoch", () => {
  const state = createAppState();
  const retired = [];
  for (let index = 0; index < 1000; index += 1) {
    const id = `session-${index}`;
    state.pipeline = { id, snapshot: { complete: true } };
    state.result = { source: `revision-${index}` };
    state.runtime = { executedSteps: index };
    if (index % 2 === 0) beginAutoRun(state);
    const epoch = state.sessionEpoch;
    const invalidated = invalidateActiveSession(state);
    retired.push({ id, epoch, disposeImmediately: invalidated.disposeImmediately });
    finishAutoRun(state);
  }

  assert.equal(state.sessionEpoch, 1000);
  assert.deepEqual(state.pipeline, { id: "", snapshot: null });
  assert.equal(state.result, null);
  assert.equal(state.runtime, null);
  assert.equal(state.deferredDisposals.size, 500);
  assert.equal(retired.every(({ id }, index) => id === `session-${index}`), true);
  assert.equal(retired.every(({ disposeImmediately }, index) => disposeImmediately === (index % 2 === 1)), true);
});
