/* SPDX-License-Identifier: AGPL-3.0-or-later */

(() => {
  "use strict";

  const $ = (selector, root = document) => root.querySelector(selector);
  const $$ = (selector, root = document) => Array.from(root.querySelectorAll(selector));
  const client = window.CoolerClient;

  const elements = {
    editor: $("#sourceEditor"),
    sourceHighlights: $("#sourceHighlights"),
    gutter: $("#lineGutter"),
    exampleSelect: $("#exampleSelect"),
    recentSelect: $("#recentSelect"),
    clearSourceButton: $("#clearSourceButton"),
    openButton: $("#openButton"),
    addFileButton: $("#addFileButton"),
    saveButton: $("#saveButton"),
    saveAsButton: $("#saveAsButton"),
    compileButton: $("#compileButton"),
    nextPhaseButton: $("#nextPhaseButton"),
    runButton: $("#runButton"),
    stepButton: $("#stepButton"),
    resetButton: $("#resetButton"),
    aboutButton: $("#aboutButton"),
    aboutDialog: $("#aboutDialog"),
    aboutCloseButton: $("#aboutCloseButton"),
    aboutVersion: $("#aboutVersion"),
    exportSelect: $("#exportSelect"),
    exportButton: $("#exportButton"),
    themeButton: $("#themeButton"),
    fileInput: $("#fileInput"),
    addFileInput: $("#addFileInput"),
    sourceTabs: $("#sourceTabs"),
    programInput: $("#programInput"),
    documentName: $("#documentName"),
    sourcePosition: $("#sourcePosition"),
    sourceSize: $("#sourceSize"),
    pipeline: $("#pipeline"),
    stageGuide: $("#stageGuide"),
    tabs: $("#artifactTabs"),
    content: $("#artifactContent"),
    diagnosticCount: $("#diagnosticCount"),
    sessionStatus: $("#sessionStatus"),
    console: $("#consoleOutput"),
    executionSummary: $("#executionSummary"),
    clearConsoleButton: $("#clearConsoleButton"),
    globalStatus: $("#globalStatus"),
    version: $("#versionLabel"),
    toastRegion: $("#toastRegion"),
    emptyTemplate: $("#emptyTemplate"),
  };

  const state = {
    examples: [],
    result: null,
    runtime: null,
    pipeline: { id: "", snapshot: null },
    deferredDisposals: new Set(),
    activeTab: "diagnostics",
    breakpoints: new Set(),
    documentName: "untitled.cl",
    sourceTabs: [{ name: "untitled.cl", source: "", savedSource: "" }],
    activeTabIndex: 0,
    concatFiles: [],
    busy: false,
    stale: false,
    consoleCleared: false,
    savedSource: "",
    sessionEpoch: 0,
    fileReadEpoch: 0,
    autoRunning: false,
    pauseRequested: false,
    tokenFilter: "",
  };

  const recentStorageKey = "coolerRecentFiles";
  const vmRunChunkSteps = 25;
  const vmRunChunkDelayMs = 25;

  const stageTabs = {
    lexer: "tokens",
    parser: "ast",
    semantic: "symbols",
    ir: "ir",
    codegen: "assembly",
    runtime: "vm",
  };

  const phaseLabels = {
    lexer: "Lexer",
    parser: "Parser",
    semantic: "Semantic",
    ir: "Stack IR",
    codegen: "Pseudo-assembly",
    runtime: "Educational VM",
  };

  const stageGuides = {
    diagnostics: ["Diagnostics", "Review lexer, parser, semantic, runtime, and interface feedback before continuing."],
    tokens: ["Lexical analysis", "Inspect how COOL source becomes typed tokens with browser-compatible source spans."],
    ast: ["Syntax analysis", "Explore the typed tree produced from COOL classes, features, and expressions."],
    symbols: ["Semantic analysis", "Inspect declarations, inheritance ownership, scopes, mutability, and inferred types."],
    ir: ["Stack IR", "Follow the language-neutral stack instructions generated for each COOL method."],
    assembly: ["Pseudo-assembly", "Inspect the readable target form emitted after IR generation."],
    cfg: ["Control flow", "See basic blocks and the branches connecting each compiled method."],
    vm: ["Educational VM", "Follow stack, frames, output, and trace state as the COOL program executes."],
  };

  const fallbackSource = `class Main inherits IO {
  main() : Object {
    out_string("Hello, COOL!\\n")
  };
};
`;

  async function api(path, body) {
    const options = body === undefined
      ? { method: "GET", headers: { Accept: "application/json" } }
      : {
          method: "POST",
          headers: { "Content-Type": "application/json", Accept: "application/json" },
          body: JSON.stringify(body),
        };
    const response = await fetch(path, options);
    let payload;
    try {
      payload = await response.json();
    } catch {
      payload = { error: `${response.status} ${response.statusText}` };
    }
    if (!response.ok) {
      throw new Error(payload.error || `${response.status} ${response.statusText}`);
    }
    return payload;
  }

  async function loadExamples() {
    const startupEpoch = state.sessionEpoch;
    try {
      const payload = await api("/api/examples");
      state.examples = payload.examples || [];
      elements.exampleSelect.innerHTML = '<option value="">Examples…</option>' + state.examples
        .map((example) => `<option value="${escapeHTML(example.id)}">${escapeHTML(example.title)}</option>`)
        .join("");
      if (state.examples.length > 0 && startupEpoch === state.sessionEpoch) {
        loadExample(state.examples[0], true);
      } else if (state.examples.length === 0 && startupEpoch === state.sessionEpoch) {
        replaceDocument(fallbackSource, "hello.cl");
      }
    } catch (error) {
      if (startupEpoch === state.sessionEpoch) replaceDocument(fallbackSource, "hello.cl");
      toast(`Could not load examples: ${error.message}`, "error");
    }
  }

  async function loadHealth() {
    try {
      const health = await api("/api/health");
      const version = health.version || "dev";
      elements.version.textContent = `cooler-compiler ${version}`;
      elements.aboutVersion.textContent = version;
    } catch {
      elements.version.textContent = "local educational compiler";
      elements.aboutVersion.textContent = "local";
    }
  }

  function loadExample(example, skipConfirmation = false) {
    if (!skipConfirmation && !confirmDiscard()) {
      elements.exampleSelect.value = "";
      return;
    }
    const files = Array.isArray(example.files) ? example.files.filter((file) => file && typeof file.source === "string") : [];
    if (files.length >= 1) {
      invalidateSession();
      state.breakpoints.clear();
      state.stale = false;
      state.sourceTabs = files.map((file) => {
        const name = client.normalizeFileName(file.name || example.path || `${example.id}.cl`);
        const source = String(file.source ?? "");
        return { name, source, savedSource: source };
      });
      state.activeTabIndex = 0;
      state.concatFiles = [];
      state.consoleCleared = false;
      loadActiveTabIntoEditor();
      renderAll();
    } else {
      replaceDocument(example.source, example.path || `${example.id}.cl`);
    }
    setProgramInput(example.input || "");
    elements.exampleSelect.value = example.id;
    setGlobalStatus(`Loaded ${example.title}`, "idle");
  }

  function setSource(source, name = "untitled.cl", saved = true) {
    elements.editor.value = source;
    state.documentName = client.normalizeFileName(name);
    state.savedSource = saved ? source : null;
    state.consoleCleared = false;
    updateDocumentTitle();
    updateEditorMetadata();
    renderGutter();
    renderSourceTabs();
  }

  function replaceDocument(source, name, saved = true) {
    invalidateSession();
    state.breakpoints.clear();
    state.stale = false;
    const normalized = client.normalizeFileName(name);
    state.sourceTabs = [{
      name: normalized,
      source,
      savedSource: saved ? source : null,
    }];
    state.activeTabIndex = 0;
    state.concatFiles = [];
    setSource(source, normalized, saved);
    elements.exampleSelect.value = "";
    renderAll();
  }

  function flushActiveTab() {
    const tab = state.sourceTabs[state.activeTabIndex];
    if (!tab) return;
    tab.source = elements.editor.value;
  }

  function currentConcat() {
    flushActiveTab();
    const concat = client.concatSources(state.sourceTabs.map((tab) => ({ name: tab.name, source: tab.source })));
    state.concatFiles = concat.files;
    return concat;
  }

  function programInput() {
    return elements.programInput ? String(elements.programInput.value ?? "") : "";
  }

  function setProgramInput(text) {
    if (elements.programInput) elements.programInput.value = String(text ?? "");
  }

  function compilePayload() {
    const concat = currentConcat();
    return { source: concat.source, input: programInput() };
  }

  function sessionInputPayload(extra = {}, { includeInput = true } = {}) {
    if (!includeInput) return extra;
    return { ...extra, input: programInput() };
  }

  function vmHasStarted() {
    return Number(state.runtime?.executedSteps || 0) > 0;
  }

  function updateDocumentTitle() {
    const names = state.sourceTabs.map((tab) => tab.name);
    elements.documentName.textContent = names.join(" + ");
    const current = state.sourceTabs[state.activeTabIndex];
    if (current) state.documentName = current.name;
  }

  function renderSourceTabs() {
    if (!elements.sourceTabs) return;
    elements.sourceTabs.innerHTML = state.sourceTabs.map((tab, index) => {
      const active = index === state.activeTabIndex;
      const dirty = tab.source !== tab.savedSource;
      return `<div class="source-tab" role="presentation">
        <button class="tab${active ? " active" : ""}${dirty ? " dirty" : ""}" type="button" data-file-index="${index}" role="tab" aria-selected="${active}" aria-controls="sourcePanel" title="${escapeHTML(tab.name)}">
          <span class="tab-name">${escapeHTML(tab.name)}</span>
        </button>
        <button type="button" class="tab-close" data-close-index="${index}" title="Close" aria-label="Close ${escapeHTML(tab.name)}">×</button>
      </div>`;
    }).join("");
  }

  function loadActiveTabIntoEditor() {
    const tab = state.sourceTabs[state.activeTabIndex];
    if (!tab) return;
    elements.editor.value = tab.source;
    state.documentName = tab.name;
    state.savedSource = tab.savedSource;
    updateDocumentTitle();
    updateEditorMetadata();
    renderSourceTabs();
    renderGutter();
    renderSourceHighlights();
  }

  function switchSourceTab(index) {
    if (index < 0 || index >= state.sourceTabs.length) return;
    flushActiveTab();
    state.activeTabIndex = index;
    loadActiveTabIntoEditor();
  }

  function closeSourceTab(index) {
    if (index < 0 || index >= state.sourceTabs.length) return;
    flushActiveTab();
    if (state.sourceTabs.length <= 1) {
      const tab = state.sourceTabs[0];
      tab.source = "";
      tab.savedSource = "";
      tab.name = "untitled.cl";
      state.breakpoints.clear();
      invalidateSession();
      state.stale = false;
      loadActiveTabIntoEditor();
      renderAll();
      setGlobalStatus("Editor cleared", "idle");
      return;
    }
    const tab = state.sourceTabs[index];
    if (tab.source !== tab.savedSource && !window.confirm("Discard unsaved changes to the current COOL source?")) return;
    const nextBreakpoints = new Set();
    for (const key of state.breakpoints) {
      if (typeof key === "number" || /^\d+$/.test(String(key))) {
        nextBreakpoints.add(Number(key));
        continue;
      }
      const [tabIndex, line] = String(key).split(":").map(Number);
      if (Number.isNaN(tabIndex) || tabIndex === index || Number.isNaN(line) || !line) continue;
      nextBreakpoints.add(`${tabIndex > index ? tabIndex - 1 : tabIndex}:${line}`);
    }
    state.breakpoints = nextBreakpoints;
    state.sourceTabs.splice(index, 1);
    if (state.activeTabIndex >= state.sourceTabs.length) state.activeTabIndex = state.sourceTabs.length - 1;
    else if (index < state.activeTabIndex) state.activeTabIndex -= 1;
    invalidateSession();
    state.stale = false;
    loadActiveTabIntoEditor();
    renderAll();
    setGlobalStatus(`Closed ${tab.name}`, "idle");
  }

  function joinedBreakpointLines() {
    const concat = currentConcat();
    const lines = [];
    for (const key of state.breakpoints) {
      if (typeof key === "number" || /^\d+$/.test(String(key))) {
        lines.push(Number(key));
        continue;
      }
      const [tabIndex, line] = String(key).split(":").map(Number);
      const file = concat.files[tabIndex];
      if (!file || !line) continue;
      lines.push(file.startLine + line - 1);
    }
    return lines.sort((left, right) => left - right);
  }

  function localLineFromJoined(joinedLine) {
    if (!joinedLine) return 0;
    const files = state.concatFiles.length ? state.concatFiles : currentConcat().files;
    const loc = client.mapDiagnosticLocation(files, state.sourceTabs[0]?.name || "untitled.cl", joinedLine);
    if (loc.index >= 0 && loc.index !== state.activeTabIndex) return 0;
    return loc.line;
  }

  function diagnosticLocationLabel(diagnostic, files) {
    const mapped = files || (state.concatFiles.length ? state.concatFiles : currentConcat().files);
    if (mapped.length <= 1) return `${diagnostic.line || 1}:${diagnostic.column || 1}`;
    const loc = client.mapDiagnosticLocation(mapped, state.sourceTabs[0]?.name || "untitled.cl", diagnostic.line || 1);
    return `${loc.name}:${loc.line || 1}:${diagnostic.column || 1}`;
  }

  function invalidateSession() {
    const id = state.pipeline.id;
    state.sessionEpoch += 1;
    if (state.autoRunning) {
      state.pauseRequested = true;
      if (id) state.deferredDisposals.add(id);
    }
    state.pipeline = { id: "", snapshot: null };
    state.result = null;
    state.runtime = null;
    if (id && !state.deferredDisposals.has(id)) void disposeSession(id);
  }

  async function disposeSession(id) {
    if (!id) return;
    try {
      await api("/api/pipeline/dispose", { id });
    } catch {
      // Expired sessions are already effectively disposed.
    }
  }

  function disposeDeferredSessions() {
    for (const id of state.deferredDisposals) void disposeSession(id);
    state.deferredDisposals.clear();
  }

  function applyPipelineSnapshot(payload, expectedEpoch = state.sessionEpoch) {
    if (expectedEpoch !== state.sessionEpoch) {
      if (payload?.id) void disposeSession(payload.id);
      return false;
    }
    if (!payload?.snapshot) return null;
    state.pipeline.id = payload.id || state.pipeline.id;
    state.pipeline.snapshot = payload.snapshot;
    state.result = payload.snapshot.result || state.result;
    state.runtime = state.result?.runtime || null;
    state.stale = false;
    state.consoleCleared = false;
    const tab = stageTabs[payload.snapshot.currentPhase];
    if (tab) state.activeTab = tab;
    renderAll();
    const label = phaseLabels[payload.snapshot.currentPhase] || "Compiler";
    updateSessionStatus(payload.snapshot.blocked ? "blocked" : payload.snapshot.complete ? "ready" : label.toLowerCase());
    return true;
  }

  async function nextPipelinePhase() {
    if (state.busy) return;
    const epoch = state.sessionEpoch;
    setBusy(true, "Executing next compiler phase…");
    try {
      const payload = state.pipeline.id
        ? await api("/api/pipeline/next", sessionInputPayload({ id: state.pipeline.id }))
        : await api("/api/pipeline/start", compilePayload());
      const applied = applyPipelineSnapshot(payload, epoch);
      if (applied === false) return;
      if (applied !== true) throw new Error("Pipeline response returned no snapshot");
      const snapshot = payload.snapshot;
      const label = phaseLabels[snapshot.currentPhase] || "Compiler phase";
      setGlobalStatus(snapshot.blocked ? `${label} blocked the pipeline` : `${label} completed`, snapshot.blocked ? "error" : "ok");
    } catch (error) {
      if (epoch !== state.sessionEpoch) return;
      handleMissingSession(error);
      toast(error.message, "error");
      setGlobalStatus("Compiler phase request failed", "error");
    } finally {
      setBusy(false);
    }
  }

  async function completePipeline(epoch = state.sessionEpoch) {
    if (!state.pipeline.id) {
      const payload = await api("/api/pipeline/start", compilePayload());
      const applied = applyPipelineSnapshot(payload, epoch);
      if (applied === false) return null;
      if (applied !== true) throw new Error("Pipeline start returned no snapshot");
    }
    if (epoch !== state.sessionEpoch) return null;
    const snapshot = state.pipeline.snapshot;
    if (!snapshot.complete && !snapshot.blocked) {
      const payload = await api("/api/pipeline/complete", sessionInputPayload({ id: state.pipeline.id }));
      const applied = applyPipelineSnapshot(payload, epoch);
      if (applied === false) return null;
      if (applied !== true) throw new Error("Pipeline complete returned no snapshot");
    }
    return state.pipeline.snapshot;
  }

  async function compilePipeline() {
    if (state.busy) return;
    const epoch = state.sessionEpoch;
    setBusy(true, "Compiling…");
    try {
      const snapshot = await completePipeline(epoch);
      if (!snapshot) return;
      setGlobalStatus(snapshot.blocked ? "Compilation completed with errors" : "Compilation ready", snapshot.blocked ? "error" : "ok");
    } catch (error) {
      if (epoch !== state.sessionEpoch) return;
      handleMissingSession(error);
      toast(error.message, "error");
      setGlobalStatus("Compilation request failed", "error");
    } finally {
      setBusy(false);
    }
  }

  async function runSession() {
    if (state.autoRunning) {
      state.pauseRequested = true;
      setRunButtonMode("pausing");
      setGlobalStatus("Pausing VM…", "busy");
      return;
    }
    if (state.busy) return;
    const epoch = state.sessionEpoch;
    setBusy(true, "Preparing compiler pipeline…");
    try {
      const compilation = await completePipeline(epoch);
      if (!compilation || epoch !== state.sessionEpoch) return;
      if (compilation.blocked) {
        setGlobalStatus("Compilation completed with errors", "error");
        return;
      }
      const sessionID = state.pipeline.id;
      state.autoRunning = true;
      state.pauseRequested = false;
      setBusy(true, "Running VM…", "run");
      state.activeTab = "vm";

      const runResult = await client.runVMChunks({
        sessionID,
        epoch,
        chunkSteps: vmRunChunkSteps,
        chunkDelayMs: vmRunChunkDelayMs,
        getSessionID: () => state.pipeline.id,
        getEpoch: () => state.sessionEpoch,
        getRuntime: () => state.runtime,
        getBreakpoints: () => new Set(joinedBreakpointLines()),
        isPauseRequested: () => state.pauseRequested,
        requestChunk: (request) => api("/api/vm/run", sessionInputPayload(request, { includeInput: !vmHasStarted() })),
        applySnapshot: (snapshot) => {
          applySnapshot(snapshot, true);
          state.consoleCleared = false;
        },
      });
      if (runResult.reason === "stale") return;

      const runtime = state.runtime || {};
      if ((runtime.diagnostics || []).some((d) => d.severity === "error")) {
        markRuntimeStage("error");
        setGlobalStatus("Runtime error", "error");
      } else if (state.pauseRequested && !client.isBreakpointPause(runtime)) {
        markRuntimeStage("paused");
        setGlobalStatus("Paused by user", "idle");
      } else if (runtime.paused) {
        markRuntimeStage("paused");
        setGlobalStatus("Paused at breakpoint", "idle");
      } else if (runtime.halted) {
        markRuntimeStage("ok");
        setGlobalStatus("Program halted", "ok");
      }
    } catch (error) {
      if (epoch !== state.sessionEpoch) return;
      handleMissingSession(error);
      toast(error.message, "error");
      setGlobalStatus("VM request failed", "error");
    } finally {
      state.autoRunning = false;
      state.pauseRequested = false;
      disposeDeferredSessions();
      setBusy(false);
      if (epoch === state.sessionEpoch) updateSessionStatus(state.runtime?.paused ? "paused" : "ready");
    }
  }

  async function stepSession() {
    if (state.busy || state.autoRunning) return;
    const epoch = state.sessionEpoch;
    setBusy(true, "Preparing compiler pipeline…");
    try {
      const compilation = await completePipeline(epoch);
      if (!compilation || epoch !== state.sessionEpoch) return;
      if (compilation.blocked) {
        setGlobalStatus("Compilation completed with errors", "error");
        return;
      }
      setGlobalStatus("Executing one instruction…", "busy");
      const sessionID = state.pipeline.id;
      const payload = await api("/api/vm/step", sessionInputPayload({ id: sessionID, light: true, traceFrom: client.traceEnd(state.runtime) }, { includeInput: !vmHasStarted() }));
      if (!client.shouldAcceptResponse(sessionID, state.pipeline.id, epoch, state.sessionEpoch)) return;
      applySnapshot(payload.snapshot, true);
      state.consoleCleared = false;
      const last = lastTraceState();
      if (last?.span) selectSpan(last.span, false);
      setGlobalStatus(state.runtime?.halted ? "Program halted" : "Stepped", state.runtime?.halted ? "ok" : "idle");
    } catch (error) {
      if (epoch !== state.sessionEpoch) return;
      handleMissingSession(error);
      toast(error.message, "error");
      setGlobalStatus("VM step failed", "error");
    } finally {
      setBusy(false);
    }
  }

  async function resetSession() {
    if (state.busy || state.autoRunning || !state.pipeline.id || !state.pipeline.snapshot?.complete || state.stale) return;
    const epoch = state.sessionEpoch;
    const sessionID = state.pipeline.id;
    setBusy(true, "Resetting VM…");
    try {
      const payload = await api("/api/vm/reset", sessionInputPayload({ id: sessionID, light: true, traceFrom: 0 }));
      if (!client.shouldAcceptResponse(sessionID, state.pipeline.id, epoch, state.sessionEpoch)) return;
      applySnapshot(payload.snapshot, true);
      state.consoleCleared = false;
      setGlobalStatus("VM reset", "idle");
    } catch (error) {
      if (epoch !== state.sessionEpoch) return;
      handleMissingSession(error);
      toast(error.message, "error");
    } finally {
      setBusy(false);
    }
  }

  function applySnapshot(snapshot, light = false) {
    if (!snapshot) return;
    state.result = light ? client.mergeLightSnapshot(state.result, snapshot) : (snapshot.result || state.result);
    state.runtime = state.result?.runtime || snapshot.runtime || null;
    if (state.result && snapshot.runtime) state.result.runtime = state.runtime;
    if (light) renderRuntimeSnapshot();
    else renderAll();
    updateSessionStatus(snapshot.status || "ready");
  }

  function handleMissingSession(error) {
    if (!/not found/i.test(error?.message || "")) return false;
    invalidateSession();
    state.stale = false;
    renderAll();
    return true;
  }

  function renderRuntimeSnapshot() {
    renderPipeline();
    renderStageGuide();
    if (state.activeTab === "vm") renderArtifact();
    renderConsole();
    renderGutter();
    updateButtons();
  }

  function markRuntimeStage(status) {
    if (!state.result) return;
    state.result.stages = (state.result.stages || []).map((stage) => stage.id === "runtime" ? { ...stage, status } : stage);
    renderPipeline();
  }

  function setRunButtonMode(mode) {
    if (mode === "pause") {
      elements.runButton.disabled = false;
      elements.runButton.textContent = "Pause";
      elements.runButton.title = "Pause after the current VM batch";
      return;
    }
    if (mode === "pausing") {
      elements.runButton.disabled = true;
      elements.runButton.textContent = "Pausing…";
      elements.runButton.title = "Waiting for the current VM batch";
      return;
    }
    elements.runButton.textContent = "Run";
    elements.runButton.title = "Run until halt or breakpoint";
  }

  function setBusy(busy, label = "Working…", active = "") {
    state.busy = busy;
    elements.compileButton.disabled = busy;
    elements.nextPhaseButton.disabled = busy;
    elements.runButton.disabled = busy && active !== "run";
    elements.stepButton.disabled = busy;
    elements.resetButton.disabled = busy || !state.pipeline.id || !state.pipeline.snapshot?.complete || state.stale;
    setRunButtonMode(busy && active === "run" ? "pause" : "idle");
    if (busy) setGlobalStatus(label, "busy");
    if (!busy) updateButtons();
  }

