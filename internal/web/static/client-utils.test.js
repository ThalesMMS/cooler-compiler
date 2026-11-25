/* SPDX-License-Identifier: AGPL-3.0-or-later */

"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const { createHash } = require("node:crypto");
const fs = require("node:fs");
const path = require("node:path");
const {
  applyQuickFix,
  buildArchiveFiles,
  buildCompileReport,
  buildZipBlob,
  mergeLightSnapshot,
  mergeRecentFiles,
  normalizeFileName,
  renderASTNode,
  runButtonPresentation,
  runVMChunks,
  shouldAcceptResponse,
  shouldDisableRunButton,
  shouldStopAutoRun,
  toggleASTBranch,
  traceEnd,
  validateArchiveFiles,
} = require("./client-utils.js");

test("Run button presentation keeps its visual and accessible state in sync", () => {
  assert.deepEqual(runButtonPresentation("idle"), {
    label: "Run",
    title: "Run until halt or breakpoint",
    icon: "run",
  });
  assert.deepEqual(runButtonPresentation("pause"), {
    label: "Pause",
    title: "Pause after the current VM batch",
    icon: "pause",
  });
  assert.deepEqual(runButtonPresentation("pausing"), {
    label: "Pausing…",
    title: "Waiting for the current VM batch",
    icon: "pause",
  });
});

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
  assert.equal(shouldStopAutoRun({ executedSteps: 25, waitingInput: { sequence: 1 }, trace: [] }, 24), true);
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

test("the client sends source-aware breakpoints in deterministic order", async () => {
  let runtime = null;
  let request;
  await runVMChunks({
    sessionID: "session-project",
    epoch: 1,
    getSessionID: () => "session-project",
    getEpoch: () => 1,
    getRuntime: () => runtime,
    getBreakpoints: () => [
      { sourceId: "z-unit", line: 2 },
      { sourceId: "a-unit", line: 9 },
      { sourceId: "a-unit", line: 3 },
    ],
    isPauseRequested: () => false,
    requestChunk: async (payload) => {
      request = payload;
      return { snapshot: { runtime: { halted: true, executedSteps: 1, trace: [] } } };
    },
    applySnapshot: (snapshot) => { runtime = snapshot.runtime; },
    wait: async () => {},
  });
  assert.deepEqual(request.breakpoints, [
    { sourceId: "a-unit", line: 3 },
    { sourceId: "a-unit", line: 9 },
    { sourceId: "z-unit", line: 2 },
  ]);
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
  assert.equal(shouldDisableRunButton({ waitingInput: true }), true);
});

test("quick fixes use browser-compatible offsets", () => {
  assert.deepEqual(
    applyQuickFix("hello worlt", { span: { startOffset: 10, endOffset: 11 }, replacement: "d" }),
    { source: "hello world", selectionStart: 10, selectionEnd: 11 },
  );
});

test("the complete report preserves metrics and control-flow artifacts", () => {
  const report = buildCompileReport({
    schemaVersion: "1.1.0",
    compilerVersion: "0.1.1",
    compilerCommit: "abc123",
    target: { os: "linux", arch: "amd64", goVersion: "go1.25" },
    options: { execute: true, vmMaxSteps: 100000, inputProvided: false },
    sourceUnits: [{ id: "source-1", name: "main.cl", order: 0, byteSize: 14, sha256: "a".repeat(64) }],
    source: "class Main {};",
    metrics: { classes: 1 },
    controlFlow: { blocks: [{ id: "entry" }], edges: [] },
    visualizations: { schemaVersion: "1.3.0", classes: [{ name: "Main" }] },
    optimization: { mode: "basic", executionIR: "optimized", passes: [{ contract: { name: "constant-fold" } }] },
  }, "main.cl", "2026-08-20T12:00:00.000Z");
  assert.deepEqual(report.metrics, { classes: 1 });
  assert.deepEqual(report.controlFlow.blocks, [{ id: "entry" }]);
  assert.equal(report.sourceFile, "main.cl");
  assert.equal(report.schemaVersion, "1.1.0");
  assert.equal(report.compilerVersion, "0.1.1");
  assert.equal(report.compilerCommit, "abc123");
  assert.equal(report.visualizations.classes[0].name, "Main");
  assert.equal(report.optimization.mode, "basic");
  assert.equal(report.exportedAt, "2026-08-20T12:00:00.000Z");
  assert.equal(buildCompileReport({}, "main.cl").exportedAt, undefined);
  assert.equal(buildCompileReport({}, "main.cl").schemaVersion, "1.5.0");
  assert.equal(buildCompileReport({}, "main.cl").optimization.mode, "none");
});

test("report export produces a digest-verified deterministic ZIP manifest", async () => {
  const files = await buildArchiveFiles({
    schemaVersion: "1.1.0",
    compilerVersion: "0.1.1",
    compilerCommit: "abc123",
    target: { os: "linux", arch: "amd64", goVersion: "go1.25" },
    options: { execute: true, vmMaxSteps: 100000, inputProvided: false },
    sourceUnits: [{ id: "source-1", name: "hello.cl", order: 0, byteSize: 14, sha256: "a".repeat(64) }],
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
    "hello/manifest.json",
  ];
  assert.deepEqual(files.map((file) => file.name), expected);

  const manifest = JSON.parse(files.at(-1).content);
  const schema = JSON.parse(fs.readFileSync(path.join(__dirname, "../../../schemas/report-archive-manifest-v1.schema.json"), "utf8"));
  for (const field of schema.required) assert.ok(Object.hasOwn(manifest, field), `schema requires ${field}`);
  assert.equal(manifest.schemaVersion, "1.0.0");
  assert.equal(manifest.compilerVersion, "0.1.1");
  assert.deepEqual(manifest.artifacts.map((artifact) => artifact.path), expected.slice(0, -1));
  const byName = new Map(files.map((file) => [file.name, file.content]));
  for (const artifact of manifest.artifacts) {
    const bytes = new TextEncoder().encode(byName.get(artifact.path));
    assert.equal(artifact.byteSize, bytes.length);
    assert.equal(artifact.sha256, createHash("sha256").update(bytes).digest("hex"));
  }

  const bytes = new Uint8Array(await buildZipBlob(files, new Date("2026-08-20T12:00:00Z")).arrayBuffer());
  assert.deepEqual(Array.from(bytes.slice(0, 4)), [0x50, 0x4b, 0x03, 0x04]);
  const text = new TextDecoder().decode(bytes);
  for (const name of expected) assert.ok(text.includes(name), `missing ZIP entry ${name}`);
  assert.deepEqual(Array.from(bytes.slice(-22, -18)), [0x50, 0x4b, 0x05, 0x06]);

  const first = new Uint8Array(await buildZipBlob(files).arrayBuffer());
  const second = new Uint8Array(await buildZipBlob(files).arrayBuffer());
  assert.deepEqual(first, second);
});

test("multifile report archives preserve explicit source units", async () => {
  const files = await buildArchiveFiles({
    schemaVersion: "1.1.0",
    compilerVersion: "dev",
    sourceUnits: [
      { id: "left", name: "same.cl", order: 0, byteSize: 11, sha256: "a".repeat(64) },
      { id: "right", name: "same.cl", order: 1, byteSize: 11, sha256: "b".repeat(64) },
    ],
  }, "same.cl", [
    { id: "left", name: "same.cl", content: "class A {};" },
    { id: "right", name: "same.cl", content: "class B {};" },
  ]);
  assert.equal(files[0].name, "same/sources/001-same.cl");
  assert.equal(files[1].name, "same/sources/002-same.cl");
  assert.equal(files[0].content, "class A {};");
  assert.equal(files[1].content, "class B {};");
  const manifest = JSON.parse(files.at(-1).content);
  assert.deepEqual(manifest.sourceUnits.map((unit) => unit.id), ["left", "right"]);
  assert.deepEqual(manifest.sourceUnits.map((unit) => unit.path), [
    "same/sources/001-same.cl",
    "same/sources/002-same.cl",
  ]);
});

test("ZIP generation rejects duplicate and unsafe entry paths", () => {
  for (const files of [
    [{ name: "same.txt", content: "a" }, { name: "same.txt", content: "b" }],
    [{ name: "../escape.txt", content: "x" }],
    [{ name: "/absolute.txt", content: "x" }],
    [{ name: "folder\\windows.txt", content: "x" }],
    [{ name: "C:/drive.txt", content: "x" }],
  ]) {
    assert.throws(() => validateArchiveFiles(files), /ZIP entry path/);
    assert.throws(() => buildZipBlob(files), /ZIP entry path/);
  }
});

test("report archive manifest matches the v1 golden fixture", async () => {
  const files = await buildArchiveFiles({
    schemaVersion: "1.0.0",
    compilerVersion: "0.1.1",
    compilerCommit: "abc123",
    target: { os: "test", arch: "test", goVersion: "go-test" },
    options: { execute: false, vmMaxSteps: 100000, inputProvided: false },
    sourceUnits: [{ id: "source-1", name: "main.cl", order: 0, byteSize: 0, sha256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" }],
    source: "",
    diagnostics: [],
    tokens: [],
    ast: null,
    symbols: [],
    ir: [],
    assembly: [],
    assemblySpans: [],
    runtime: { trace: [], output: [] },
  }, "main.cl");
  const golden = fs.readFileSync(path.join(__dirname, "testdata/report-archive-manifest.golden.json"), "utf8").replace(/\r\n/g, "\n");
  assert.equal(files.at(-1).content, golden);
});
