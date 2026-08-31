/* SPDX-License-Identifier: AGPL-3.0-or-later */

"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const {
  applyQuickFix,
  concatSources,
  mapDiagnosticLocation,
  buildArchiveFiles,
  buildCompileReport,
  buildZipBlob,
  mergeLightSnapshot,
  mergeRecentFiles,
  normalizeFileName,
  renderASTNode,
  runVMChunks,
  shouldAcceptResponse,
  shouldDisableRunButton,
  shouldStopAutoRun,
  toggleASTBranch,
  traceEnd,
} = require("./client-utils.js");

test("every AST branch has its own expanded disclosure control", () => {
  assert.equal(typeof renderASTNode, "function", "AST renderer must be available to the UI");

  const html = renderASTNode({
    kind: "Program",
    children: [{
      kind: "Class",
      children: [{
        kind: "Method",
        children: [{
          kind: "Block",
          children: [{ kind: "Int", text: "0" }],
        }],
      }],
    }],
  });
  const controls = html.match(/<button\b[^>]*class="tree-toggle"[^>]*>/g) || [];

  assert.equal(controls.length, 4);
  assert.ok(controls.every((button) => /aria-expanded="true"/.test(button)), html);
  assert.doesNotMatch(html, /<(?:details|summary)\b/);
});

test("the AST disclosure button toggles only its controlled child branch", () => {
  assert.equal(typeof toggleASTBranch, "function", "AST disclosure behavior must be available to the UI");

  const children = { hidden: false };
  const attributes = new Map([
    ["aria-controls", "ast-branch-0-0"],
    ["aria-expanded", "true"],
    ["aria-label", "Collapse Method"],
  ]);
  const button = {
    dataset: { nodeLabel: "Method" },
    ownerDocument: { getElementById: (id) => id === "ast-branch-0-0" ? children : null },
    getAttribute: (name) => attributes.get(name) ?? null,
    setAttribute: (name, value) => attributes.set(name, String(value)),
  };

  assert.equal(toggleASTBranch(button), false);
  assert.equal(children.hidden, true);
  assert.equal(attributes.get("aria-expanded"), "false");
  assert.equal(attributes.get("aria-label"), "Expand Method");

  assert.equal(toggleASTBranch(button), true);
  assert.equal(children.hidden, false);
  assert.equal(attributes.get("aria-expanded"), "true");
  assert.equal(attributes.get("aria-label"), "Collapse Method");
});

test("file names and recent documents are normalized, deduplicated, and limited", () => {
  assert.equal(normalizeFileName(" demo "), "demo.cl");
  assert.equal(normalizeFileName("Main.CL"), "Main.CL");
  const existing = Array.from({ length: 9 }, (_, index) => ({ name: `file-${index}.cl`, source: String(index) }));
  existing.unshift({ name: "DEMO.CL", source: "old" });
  const recent = mergeRecentFiles(existing, "demo", "new", "2026-08-20T12:00:00.000Z");
  assert.equal(recent.length, 8);
  assert.deepEqual(recent[0], { name: "demo.cl", source: "new", savedAt: "2026-08-20T12:00:00.000Z" });
  assert.equal(recent.filter((item) => item.name.toLowerCase() === "demo.cl").length, 1);
});

test("light snapshots merge overlapping trace deltas without losing static artifacts", () => {
  const result = {
    source: "class Main {};",
    tokens: [{ type: "CLASS" }],
    stages: [{ id: "lexer", status: "ok" }, { id: "runtime", status: "ok", summary: "0 executed instructions" }],
    diagnostics: [{ stage: "semantic", severity: "warning", code: "COL3000" }],
    runtime: { traceOffset: 4, trace: [{ op: "A" }, { op: "B" }], executedSteps: 1 },
  };
  const merged = mergeLightSnapshot(result, {
    runtime: { traceOffset: 5, trace: [{ op: "B2" }, { op: "C" }], executedSteps: 3, diagnostics: [] },
  });
  assert.deepEqual(merged.runtime.trace.map((entry) => entry.op), ["A", "B2", "C"]);
  assert.equal(merged.runtime.traceOffset, 4);
  assert.equal(traceEnd(merged.runtime), 7);
  assert.deepEqual(merged.tokens, [{ type: "CLASS" }]);
  assert.equal(merged.diagnostics[0].code, "COL3000");
  assert.deepEqual(merged.stages[1], { id: "runtime", status: "ready", summary: "3 executed instructions" });
});

test("auto-run ignores chunk budget pauses but stops for breakpoints, errors, halt, or no progress", () => {
  assert.equal(shouldStopAutoRun({ paused: true, executedSteps: 25, trace: [{ op: "PAUSE" }] }, 0), false);
  assert.equal(shouldStopAutoRun({ paused: true, executedSteps: 25, trace: [{ op: "BREAKPOINT" }] }, 24), true);
  assert.equal(shouldStopAutoRun({ halted: true, executedSteps: 25, trace: [] }, 24), true);
  assert.equal(shouldStopAutoRun({ executedSteps: 24, trace: [] }, 24), true);
  assert.equal(shouldStopAutoRun({ executedSteps: 25, diagnostics: [{ severity: "error" }] }, 24), true);
});

test("the client Run loop batches execution and honors a pending Pause", async () => {
  let runtime = null;
  let pauseRequested = false;
  const requests = [];
  const snapshots = [
    { runtime: { executedSteps: 25, traceOffset: 0, trace: [{ op: "PAUSE" }] } },
    { runtime: { executedSteps: 50, traceOffset: 1, trace: [{ op: "PAUSE" }] } },
  ];
  const result = await runVMChunks({
    sessionID: "session-1",
    epoch: 4,
    getSessionID: () => "session-1",
    getEpoch: () => 4,
    getRuntime: () => runtime,
    getBreakpoints: () => new Set([9, 2]),
    isPauseRequested: () => pauseRequested,
    requestChunk: async (request) => {
      requests.push(request);
      const snapshot = snapshots.shift();
      if (requests.length === 2) pauseRequested = true;
      return { snapshot };
    },
    applySnapshot: (snapshot) => { runtime = snapshot.runtime; },
    wait: async () => {},
  });
  assert.equal(result.reason, "paused");
  assert.equal(requests.length, 2);
  assert.deepEqual(requests.map((request) => request.breakpoints), [[2, 9], [2, 9]]);
  assert.deepEqual(requests.map((request) => request.traceFrom), [0, 1]);
  assert.equal(runtime.executedSteps, 50);
});

test("the client Run loop discards a response after the session is replaced", async () => {
  let currentID = "session-1";
  let applied = 0;
  const result = await runVMChunks({
    sessionID: "session-1",
    epoch: 4,
    getSessionID: () => currentID,
    getEpoch: () => 4,
    getRuntime: () => null,
    getBreakpoints: () => new Set(),
    isPauseRequested: () => false,
    requestChunk: async () => {
      currentID = "session-2";
      return { snapshot: { runtime: { executedSteps: 25 } } };
    },
    applySnapshot: () => { applied += 1; },
    wait: async () => {},
  });
  assert.equal(result.reason, "stale");
  assert.equal(applied, 0);
});

test("session epochs reject responses from replaced or edited sessions", () => {
  assert.equal(shouldAcceptResponse("session-1", "session-1", 4, 4), true);
  assert.equal(shouldAcceptResponse("session-1", "session-2", 4, 4), false);
  assert.equal(shouldAcceptResponse("session-1", "session-1", 4, 5), false);
});

test("the Run button stays disabled while a pause is pending", () => {
  assert.equal(shouldDisableRunButton({ busy: true, autoRunning: true, pauseRequested: true }), true);
  assert.equal(shouldDisableRunButton({ busy: true, autoRunning: true, pauseRequested: false }), false);
  assert.equal(shouldDisableRunButton({ busy: true, autoRunning: false, pauseRequested: false }), true);
  assert.equal(shouldDisableRunButton({ runtimeDone: true }), true);
});

test("quick fixes use browser-compatible offsets", () => {
  assert.deepEqual(
    applyQuickFix("hello worlt", { span: { startOffset: 10, endOffset: 11 }, replacement: "d" }),
    { source: "hello world", selectionStart: 10, selectionEnd: 11 },
  );
});

test("the complete report preserves metrics and control-flow artifacts", () => {
  const report = buildCompileReport({
    source: "class Main {};",
    metrics: { classes: 1 },
    controlFlow: { blocks: [{ id: "entry" }], edges: [] },
  }, "main.cl", "2026-08-20T12:00:00.000Z");
  assert.deepEqual(report.metrics, { classes: 1 });
  assert.deepEqual(report.controlFlow.blocks, [{ id: "entry" }]);
  assert.equal(report.sourceFile, "main.cl");
});

test("multifile export produces a readable ZIP directory with every documented entry", async () => {
  const files = buildArchiveFiles({
    source: "class Main {};",
    diagnostics: [],
    tokens: [],
    ast: null,
    symbols: [],
    ir: [],
    assembly: [],
    runtime: { trace: [], output: ["ok\n"] },
  }, "hello.cl", "", "2026-08-20T12:00:00.000Z");
  const expected = [
    "hello/source.cl",
    "hello/diagnostics.json",
    "hello/diagnostics.txt",
    "hello/tokens.json",
    "hello/ast.json",
    "hello/symbols.json",
    "hello/ir.txt",
    "hello/assembly.txt",
    "hello/vm-trace.json",
    "hello/output.txt",
    "hello/compile-report.json",
  ];
  assert.deepEqual(files.map((file) => file.name), expected);

  const bytes = new Uint8Array(await buildZipBlob(files, new Date("2026-08-20T12:00:00Z")).arrayBuffer());
  assert.deepEqual(Array.from(bytes.slice(0, 4)), [0x50, 0x4b, 0x03, 0x04]);
  const text = new TextDecoder().decode(bytes);
  for (const name of expected) assert.ok(text.includes(name), `missing ZIP entry ${name}`);
  assert.deepEqual(Array.from(bytes.slice(-22, -18)), [0x50, 0x4b, 0x05, 0x06]);
});

test("concatSources joins files in order and inserts a newline between chunks", () => {
  const withoutNewline = concatSources([
    { name: "a.cl", source: "class A {};" },
    { name: "b.cl", source: "class B {};" },
  ]);
  assert.equal(withoutNewline.source, "class A {};\nclass B {};");
  assert.deepEqual(withoutNewline.files.map((file) => ({ name: file.name, startLine: file.startLine, startOffset: file.startOffset })), [
    { name: "a.cl", startLine: 1, startOffset: 0 },
    { name: "b.cl", startLine: 2, startOffset: 12 },
  ]);

  const withNewline = concatSources([
    { name: "a.cl", source: "class A {};\n" },
    { name: "b.cl", source: "class B {};\n" },
  ]);
  assert.equal(withNewline.source, "class A {};\nclass B {};\n");
  assert.equal(withNewline.files[1].startLine, 2);
  assert.equal(withNewline.files[1].startOffset, 12);
});

test("mapDiagnosticLocation maps joined lines back to the originating file", () => {
  const { files } = concatSources([
    { name: "stack.cl", source: "class Stack {};\nclass Main {};\n" },
    { name: "atoi.cl", source: "class A2I {};\n" },
  ]);
  assert.deepEqual(mapDiagnosticLocation(files, "fallback.cl", 1), { name: "stack.cl", line: 1, index: 0 });
  assert.deepEqual(mapDiagnosticLocation(files, "fallback.cl", 2), { name: "stack.cl", line: 2, index: 0 });
  assert.deepEqual(mapDiagnosticLocation(files, "fallback.cl", 3), { name: "atoi.cl", line: 1, index: 1 });
  assert.deepEqual(mapDiagnosticLocation([], "fallback.cl", 4), { name: "fallback.cl", line: 4, index: -1 });
  assert.equal(mapDiagnosticLocation(files, "fallback.cl", 0).name, "stack.cl");
});
