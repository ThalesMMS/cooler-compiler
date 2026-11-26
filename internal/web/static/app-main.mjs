import { applyIndent, mapBreakpointLine, nextDiagnosticIndex, composing, EditorHistory, breakpointLineForSource } from "./editor-tools.mjs";
import { graphHTML, layoutGraph, graphSVG, graphPNG, bindGraphs } from "./graphs.mjs";
import { reportSources } from "./report-sources.mjs";
/* SPDX-License-Identifier: AGPL-3.0-or-later */

import {
  beginAutoRun,
  createAppState,
  finishAutoRun,
  invalidateActiveSession,
  requestRunPause,
	shouldAcceptTimeline,
} from "./app-state.mjs";
import { createHistorySeeker } from "./history-seek.mjs";
import { createDraftWriter, draftWorkspace } from "./drafts.mjs";
import { createCourseController, COURSE_CLICK_SELECTOR, shareOrDownload } from "./course.mjs";

export async function startCoolerApp() {

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
	formatButton: $("#formatButton"),
	optimizationMode: $("#optimizationMode"),
	frontendMode: $("#frontendMode"),
    nextPhaseButton: $("#nextPhaseButton"),
    runButton: $("#runButton"),
    stepButton: $("#stepButton"),
    stepIntoButton: $("#stepIntoButton"),
    stepOverButton: $("#stepOverButton"),
    stepOutButton: $("#stepOutButton"),
    runToCursorButton: $("#runToCursorButton"),
	reverseMode: $("#reverseMode"),
	stepBackButton: $("#stepBackButton"),
	reverseContinueButton: $("#reverseContinueButton"),
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
    liveInputForm: $("#liveInputForm"),
    liveInputLabel: $("#liveInputLabel"),
    liveInputLine: $("#liveInputLine"),
    sendInputButton: $("#sendInputButton"),
    inputHistory: $("#inputHistory"),
    documentName: $("#documentName"),
    sourcePosition: $("#sourcePosition"),
    sourceSize: $("#sourceSize"),
    pipeline: $("#pipeline"),
    stageGuide: $("#stageGuide"),
    tabs: $("#artifactTabs"),
    content: $("#artifactContent"),
    diagnosticCount: $("#diagnosticCount"),
    sessionStatus: $("#sessionStatus"),
    debugLocation: $("#debugLocation"),
	timelineStatus: $("#timelineStatus"),
	historySlider: $("#historySlider"),
	historyStep: $("#historyStep"),
    console: $("#consoleOutput"),
    executionSummary: $("#executionSummary"),
    clearConsoleButton: $("#clearConsoleButton"),
	breakpointEditor: $("#breakpointEditor"),
	watchForm: $("#watchForm"),
	watchExpression: $("#watchExpression"),
	watchList: $("#watchList"),
	debugLog: $("#debugLog"),
	saveDebuggerButton: $("#saveDebuggerButton"),
    globalStatus: $("#globalStatus"),
    version: $("#versionLabel"),
    toastRegion: $("#toastRegion"),
    emptyTemplate: $("#emptyTemplate"),
  };

  const state = createAppState();
  const course = createCourseController({
    api: (path, body) => api(path, body),
    sources: () => { flushActiveTab(); return state.sourceTabs.map((tab) => ({ id: tab.id || tab.name, name: tab.name, content: tab.source })); },
    input: () => programInput(),
    escapeHTML: (value) => escapeHTML(value),
    rerender: () => renderArtifact(),
    toast: (message, kind) => toast(message, kind),
    stem: () => client.stripExtension(state.documentName || "program.cl"),
    hooks: {
      openStackMachine: () => {
        const example = state.examples.find((item) => item.id === "13_stack_machine");
        if (example) loadExample(example);
        else toast("The stack-machine example is not available in this build.", "error");
      },
      runVM: () => { void runSession(); },
      frontend: () => state.frontend,
      setFrontend: (value) => {
        if (!elements.frontendMode || elements.frontendMode.value === value) return;
        elements.frontendMode.value = value;
        elements.frontendMode.dispatchEvent(new Event("change"));
      },
    },
  });

  const recentStorageKey = "coolerRecentFiles";
	const debuggerStorageKey = "coolerDebuggerWorkspace";
  const vmRunChunkSteps = 25;
  const vmRunChunkDelayMs = 25;
  let sourceIDCounter = 0;
	let offeredDrafts = null;
	let reportWorkspace = null;
	const inspectingReport = () => Boolean(reportWorkspace?.readonly);
	let lastDraftJSON = "";
	const draftStatus = $("#draftStatus");
	const draftWriter = createDraftWriter({
	  save(body) { return api("/api/drafts", body, new TextEncoder().encode(JSON.stringify(body)).length < 60 * 1024); },
	  status(message, error) { draftStatus.textContent = message; draftStatus.classList.toggle("draft-error", error); },
	});
	function scheduleDrafts() {
	  if (reportWorkspace) return;
	  flushActiveTab();
	  const workspace = draftWorkspace(state.sourceTabs, state.activeTabIndex);
	  const encoded = JSON.stringify(workspace);
	  if (encoded !== lastDraftJSON && draftWriter.schedule(workspace)) lastDraftJSON = encoded;
	}
	async function readDrafts() {
	  flushActiveTab();
	  const beforeRead = JSON.stringify(draftWorkspace(state.sourceTabs, state.activeTabIndex));
	  try {
	    const data = await api("/api/drafts", { action: "read" });
	    lastDraftJSON = beforeRead;
	    draftWriter.start(data.revision);
	    if (data.workspace) {
	      draftWriter.pause();
	      offeredDrafts = data.workspace;
	      draftStatus.textContent = `Recover ${data.workspace.tabs.length} saved draft tabs? These copies are not files saved to disk.`;
	      $("#restoreDrafts").hidden = false;
	      $("#discardDrafts").hidden = false;
	    } else {
	      draftStatus.textContent = "Recovery copies are stored locally as you edit.";
	      if (state.sourceTabs.some(tab => tab.source !== tab.savedSource)) lastDraftJSON = "";
	      scheduleDrafts();
	    }
	  } catch (error) { draftStatus.textContent = error.message; draftStatus.classList.add("draft-error"); }
	}
	async function clearDrafts() {
	  if (draftWriter.active()) { toast("Wait for the recovery save to finish, then clear it.", "error"); return; }
	  flushActiveTab();
	  const beforeClear = JSON.stringify(draftWorkspace(state.sourceTabs, state.activeTabIndex));
	  draftWriter.pause();
	  try {
	    const data = await api("/api/drafts", { action: "clear", revision: draftWriter.revision() });
	    offeredDrafts = null;
	    $("#restoreDrafts").hidden = true;
	    $("#discardDrafts").hidden = true;
	    draftWriter.start(data.revision);
	    draftStatus.textContent = "Recovery data cleared. Your open editor is unchanged; new edits create new recovery copies.";
	    flushActiveTab();
	    lastDraftJSON = beforeClear;
	    draftStatus.classList.remove("draft-error");
	    scheduleDrafts();
	  } catch (error) { draftStatus.textContent = error.message; draftStatus.classList.add("draft-error"); }
	}
	function restoreDrafts() {
	  if(reportWorkspace)return;
	  if (!offeredDrafts) return;
	  const tabs = Array.isArray(offeredDrafts.tabs) ? offeredDrafts.tabs : [];
	  if (!tabs.length) { offeredDrafts = null; return; }
	  invalidateSession();
	  state.sourceTabs = tabs.map(tab => ({ ...makeSourceTab(tab.name, tab.content, null), recovered: true }));
	  state.activeTabIndex = Math.min(Math.max(0, Math.trunc(Number(offeredDrafts.active)) || 0), state.sourceTabs.length - 1);
	  offeredDrafts = null;
	  state.breakpoints.clear(); state.breakpointConfigs.clear();
	  loadActiveTabIntoEditor(); renderAll();
	  $("#restoreDrafts").hidden = true; $("#discardDrafts").hidden = true;
	  draftWriter.start(draftWriter.revision());
	  lastDraftJSON = JSON.stringify(draftWorkspace(state.sourceTabs, state.activeTabIndex));
	  draftStatus.textContent = "Drafts restored. Save each file to keep a disk copy.";
	}

  function newSourceID() {
    do { sourceIDCounter += 1; } while ([...state.sourceTabs,...(reportWorkspace?.backup.sourceTabs || [])].some(tab=>tab.id===`source-${sourceIDCounter}`));
    return `source-${sourceIDCounter}`;
  }

  function makeSourceTab(name, source, savedSource = source, id = newSourceID()) {
    return { id, name: client.normalizeFileName(name), source, savedSource };
  }

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
    course: ["Course mode (PA1–PA5)", "Opt-in view of each phase as its practical assignment, with reference formats, MIPS, the simulator, and exports."],
    mips: ["MIPS backend (PA5)", "Optional SPIM assembly for the Cool runtime: object layout, tables, init methods, and code; export the .s file."],
    cfg: ["Control flow", "See basic blocks and the branches connecting each compiled method."],
	explore: ["Relationship explorer", "Navigate inheritance, logical layouts, dispatch, and many-to-many lowering provenance."],
	optimization: ["Educational optimization", "Compare verified baseline and optimized IR, CFG, source mappings, transformations, and refusals."],
    vm: ["Educational VM", "Follow stack, frames, output, and trace state as the COOL program executes."],
	heap: ["Heap inspector", "Explore bounded object graphs, roots, fields, and stable allocation identities without changing execution state."],
  };

  const fallbackSource = `class Main inherits IO {
  main() : Object {
    out_string("Hello, COOL!\\n")
  };
};
`;

  async function api(path, body, keepalive = false) {
	if (inspectingReport() && (/^\/api\/vm\//u.test(path) || /^\/api\/pipeline\/(start|next|complete)$/u.test(path) || path === '/api/compile')) throw new Error('Report inspection does not run code. Use Recompile imported sources.');
    const headers = {
      Accept: "application/json",
      "X-Cooler-Capability": window.CoolerCapability || "",
    };
    const options = body === undefined
      ? { method: "GET", headers }
      : {
          method: "POST",
		  keepalive,
          headers: { ...headers, "Content-Type": "application/json" },
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
      const error = new Error(payload.error || `${response.status} ${response.statusText}`);
      error.status = response.status;
      throw error;
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
	if(inspectingReport())return;
    if (!skipConfirmation && !confirmDiscard()) {
      elements.exampleSelect.value = "";
      return;
    }
    const files = Array.isArray(example.files) ? example.files.filter((file) => file && typeof file.source === "string") : [];
    if (files.length >= 1) {
      invalidateSession();
      state.breakpoints.clear();
	  state.breakpointConfigs.clear();
      state.stale = false;
      state.sourceTabs = files.map((file) => {
        const name = client.normalizeFileName(file.name || example.path || `${example.id}.cl`);
        const source = String(file.source ?? "");
        return makeSourceTab(name, source);
      });
      state.activeTabIndex = 0;
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
	if(inspectingReport())return;
    invalidateSession();
    state.breakpoints.clear();
	state.breakpointConfigs.clear();
    state.stale = false;
    const normalized = client.normalizeFileName(name);
    state.sourceTabs = [makeSourceTab(normalized, source, saved ? source : null)];
    state.activeTabIndex = 0;
    setSource(source, normalized, saved);
    elements.exampleSelect.value = "";
    renderAll();
  }

  function flushActiveTab() {
    const tab = state.sourceTabs[state.activeTabIndex];
    if (!tab) return;
    tab.source = elements.editor.value;
    tab.selectionStart=elements.editor.selectionStart;tab.selectionEnd=elements.editor.selectionEnd;tab.scrollTop=elements.editor.scrollTop;
  }

  function programInput() {
    return elements.programInput ? String(elements.programInput.value ?? "") : "";
  }

  function setProgramInput(text) {
    if (elements.programInput) elements.programInput.value = String(text ?? "");
  }

  function compilePayload() {
    flushActiveTab();
    return {
      sources: state.sourceTabs.map((tab) => ({ id: tab.id, name: tab.name, content: tab.source })),
      input: programInput(),
      interactive: true,
	  optimization: state.optimization,
	  ...(state.frontend === "generated" ? { frontend: "generated" } : {}),
    };
  }

  function sessionInputPayload(extra = {}, { includeInput = true } = {}) {
	const payload = { ...extra, watches: projectWatches() };
	if (!includeInput) return payload;
	return { ...payload, input: programInput() };
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
          <span class="tab-name">${escapeHTML(tab.name)}${tab.recovered ? " · recovered" : ""}</span>
        </button>
        <button type="button" class="tab-close" ${inspectingReport() ? "disabled" : ""} data-close-index="${index}" title="Close" aria-label="Close ${escapeHTML(tab.name)}">×</button>
      </div>`;
    }).join("");
	scheduleDrafts();
  }

  function loadActiveTabIntoEditor() {
    const tab = state.sourceTabs[state.activeTabIndex];
    if (!tab) return;
    elements.editor.value = tab.source;
    elements.editor.setSelectionRange(tab.selectionStart||0,tab.selectionEnd||0);elements.editor.scrollTop=tab.scrollTop||0;
    state.documentName = tab.name;
    state.savedSource = tab.savedSource;
    updateDocumentTitle();
    updateEditorMetadata();
    renderSourceTabs();
    renderGutter();
    renderSourceHighlights();
	if(!reportWorkspace)restoreDebuggerWorkspaceForOpenTabs();
  }

  function switchSourceTab(index) {
    if (index < 0 || index >= state.sourceTabs.length) return;
    flushActiveTab();
    state.activeTabIndex = index;
    loadActiveTabIntoEditor();
  }

  function closeSourceTab(index) {
	if(inspectingReport())return;
    if (index < 0 || index >= state.sourceTabs.length) return;
    flushActiveTab();
    if (state.sourceTabs.length <= 1) {
      const tab = state.sourceTabs[0];
      tab.source = "";
      tab.savedSource = "";
      tab.name = "untitled.cl";
      state.breakpoints.clear();
	  state.breakpointConfigs.clear();
      invalidateSession();
      state.stale = false;
      loadActiveTabIntoEditor();
      renderAll();
      setGlobalStatus("Editor cleared", "idle");
      return;
    }
    const tab = state.sourceTabs[index];
    if (tab.source !== tab.savedSource && !window.confirm("Discard unsaved changes to the current COOL source?")) return;
    for (const key of state.breakpoints) {
	  if (String(key).startsWith(`${tab.id}:`)) {
		state.breakpoints.delete(key);
		state.breakpointConfigs.delete(key);
	  }
    }
    state.sourceTabs.splice(index, 1);
    if (state.activeTabIndex >= state.sourceTabs.length) state.activeTabIndex = state.sourceTabs.length - 1;
    else if (index < state.activeTabIndex) state.activeTabIndex -= 1;
    invalidateSession();
    state.stale = false;
    loadActiveTabIntoEditor();
    renderAll();
    setGlobalStatus(`Closed ${tab.name}`, "idle");
  }

  function projectBreakpoints() {
    const breakpoints = [];
    for (const key of state.breakpoints) {
      if (typeof key === "number" || /^\d+$/.test(String(key))) {
        breakpoints.push({ line: Number(key) });
        continue;
      }
      const separator = String(key).lastIndexOf(":");
      if (separator < 1) continue;
      const sourceId = String(key).slice(0, separator);
      const line = Number(String(key).slice(separator + 1));
      if (!sourceId || !line) continue;
      const resolution = resolveBreakpoint(sourceId, line);
	  const config = state.breakpointConfigs.get(key) || {};
	  breakpoints.push({
		id: String(key), sourceId, line: resolution.resolvedLine || line,
		column: resolution.resolvedColumn || 0,
		condition: config.condition || "", hitCount: Number(config.hitCount || 0),
		logMessage: config.logMessage || "", pause: Boolean(config.pause), enabled: config.enabled !== false,
	  });
    }
    return breakpoints.sort((left, right) => String(left.sourceId || "").localeCompare(String(right.sourceId || "")) || left.line - right.line);
  }

  function executableLines(sourceID) {
    const lines = new Set();
    for (const instruction of state.result?.ir || []) {
      const span = instruction.span;
      if (!span || span.sourceId !== sourceID || !span.startLine) continue;
      if (instruction.method === "$program.$entry") continue;
      if (String(instruction.method || "").endsWith(".$init") && span.endLine > span.startLine) continue;
      lines.add(Number(span.startLine));
    }
    return Array.from(lines).sort((left, right) => left - right);
  }

	function executablePositions(sourceID) {
	  const positions = [];
	  for (const instruction of state.result?.ir || []) {
		const span = instruction.span;
		if (!span || span.sourceId !== sourceID || !span.startLine) continue;
		if (instruction.method === "$program.$entry") continue;
		if (String(instruction.method || "").endsWith(".$init") && span.endLine > span.startLine) continue;
		positions.push({ line: Number(span.startLine), column: Number(span.startColumn || 1), width: Math.max(1, Number(span.endOffset || 0) - Number(span.startOffset || 0)) });
	  }
	  return positions.sort((left, right) => left.line - right.line || left.column - right.column || left.width - right.width);
	}

  function resolveBreakpoint(sourceID, line) {
	if (!state.pipeline.snapshot?.complete || !state.result) return { status: "pending", resolvedLine: line, resolvedColumn: 0 };
	const positions = executablePositions(sourceID);
	const lines = Array.from(new Set(positions.map((position) => position.line)));
	if (lines.includes(line)) {
	  const exact = positions.filter((position) => position.line === line)[0];
	  return { status: "verified", resolvedLine: line, resolvedColumn: exact?.column || 0 };
	}
    if (!lines.length) return { status: "unresolved", resolvedLine: 0 };
    const resolvedLine = lines.reduce((best, candidate) => Math.abs(candidate - line) < Math.abs(best - line) ? candidate : best, lines[0]);
	const moved = positions.find((position) => position.line === resolvedLine);
	return { status: "moved", resolvedLine, resolvedColumn: moved?.column || 0 };
  }

	function projectWatches() {
	  return state.watches.map((watch) => ({ id: watch.id, expression: watch.expression, enabled: watch.enabled !== false }));
	}

  function sourceTabIndex(sourceID) {
    if (!sourceID && state.sourceTabs.length === 1) return 0;
    return state.sourceTabs.findIndex((tab) => tab.id === sourceID);
  }

  function localLineFromSpan(span) {
    if (!span?.startLine) return 0;
    const index = sourceTabIndex(span.sourceId);
    if (index >= 0 && index !== state.activeTabIndex) return 0;
    return Number(span.startLine);
  }

  function diagnosticLocationLabel(diagnostic) {
    const span = diagnostic.span || {};
    const index = sourceTabIndex(span.sourceId);
    const line = span.startLine || diagnostic.line || 1;
    const column = span.startColumn || diagnostic.column || 1;
    if (state.sourceTabs.length <= 1) return `${line}:${column}`;
    return `${state.sourceTabs[index]?.name || span.sourceId || "unknown"}:${line}:${column}`;
  }

  function invalidateSession() {
    const invalidated = invalidateActiveSession(state);
    if (invalidated.disposeImmediately) void disposeSession(invalidated.id);
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

  async function formatSource() {
    if (state.busy) return;
    const source = elements.editor.value;
    const tab = state.sourceTabs[state.activeTabIndex];
    const start = elements.editor.selectionStart, end = elements.editor.selectionEnd;
    const direction = elements.editor.selectionDirection;
    setBusy(true, "Formatting…");
    try {
      const result = await api("/api/format", { source });
      if (tab !== state.sourceTabs[state.activeTabIndex] || elements.editor.value !== source) return;
      if (result.source !== source) {
        elements.editor.value = result.source;
        elements.editor.setSelectionRange(mapFormatSelection(source, result.source, start), mapFormatSelection(source, result.source, end, start === end ? 'right' : 'left'), direction);
        sourceChanged();
      }
      elements.editor.focus();
      setGlobalStatus("Source formatted", "ok");
    } catch (error) {
      toast(error.message, "error");
      setGlobalStatus("Formatting failed — source preserved", "error");
    } finally { setBusy(false); }
  }

  async function inspectReport() {
    if(state.busy || reportWorkspace)return;
    const text=$('#reportJSON').value;
    if(new TextEncoder().encode(text).length>1048576){$('#reportError').textContent='Report must be at most 1 MiB.';return;}
    setBusy(true,'Opening report…');
    try {
      const payload=await api('/api/report/import',JSON.parse(text));
      if (!payload?.report || typeof payload.report !== 'object' || Array.isArray(payload.report)) throw new Error('The response contains no report.');
      flushActiveTab();
      const backup={};
      for(const key of ['sourceTabs','activeTabIndex','breakpoints','breakpointConfigs','documentName','savedSource'])backup[key]=state[key];
      backup.selection=[elements.editor.selectionStart,elements.editor.selectionEnd,elements.editor.selectionDirection];
      reportWorkspace={backup,sources:payload.sources || [],readonly:true};
      invalidateSession();state.stale=false;state.breakpoints=new Set();state.breakpointConfigs=new Map();
      state.sourceTabs=reportWorkspace.sources.length ? reportWorkspace.sources.map(source=>makeSourceTab(source.name,source.content,null,source.id)) : [makeSourceTab('sources-unavailable.cl','-- This report contains source metadata but no recoverable source contents.\n',null)];
      state.activeTabIndex=0;loadActiveTabIntoEditor();
      state.result=payload.report;state.runtime=payload.report.runtime;state.activeTab='diagnostics';
      $('#inspectionNotice').hidden=false;
      const missing=['tokens','ast','symbols','ir','assembly'].filter(key=>!payload.report[key] || Array.isArray(payload.report[key]) && !payload.report[key].length);
      $('#inspectionStatus').textContent=`Report inspection — no code is running. ${reportWorkspace.sources.length ? 'Sources available.' : 'Source contents unavailable.'} ${missing.length ? 'Artifacts absent: '+missing.join(', ')+'.' : ''}`;
      $('#reportDialog').close();renderAll();setGlobalStatus('Report opened for inspection','ok');
    } catch(error){$('#reportError').textContent=error.message;}
    finally{setBusy(false);}
  }

  function closeReport() {
    if(!reportWorkspace || state.busy)return;
	if(!reportWorkspace.readonly){
	  flushActiveTab();
	  const changed=state.sourceTabs.length!==reportWorkspace.sources.length || state.sourceTabs.some((tab,i)=>tab.source!==reportWorkspace.sources[i]?.content);
	  if(changed && !window.confirm('Discard edits to imported sources and return to your original documents?'))return;
	}
    const backup=reportWorkspace.backup;
    invalidateSession();
    for(const key of ['sourceTabs','activeTabIndex','breakpoints','breakpointConfigs','documentName','savedSource'])state[key]=backup[key];
    reportWorkspace=null;state.stale=true;loadActiveTabIntoEditor();
    elements.editor.setSelectionRange(...backup.selection);
    $('#inspectionNotice').hidden=true;renderAll();setGlobalStatus('Original documents restored','ok');
  }

  async function runSession() {
    if (requestRunPause(state)) {
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
      beginAutoRun(state);
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
        getBreakpoints: () => projectBreakpoints(),
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
      } else if (runtime.waitingInput) {
        markRuntimeStage("waiting-input");
        setGlobalStatus(`Waiting for ${runtime.waitingInput.builtin}`, "idle");
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
      finishAutoRun(state);
      disposeDeferredSessions();
      setBusy(false);
      if (epoch === state.sessionEpoch) {
        updateSessionStatus(state.runtime?.waitingInput ? "waiting-input" : state.runtime?.paused ? "paused" : "ready");
        if (state.runtime?.waitingInput) elements.liveInputLine?.focus();
      }
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

  async function sourceCommand(operation) {
    if (state.busy || state.autoRunning || state.runtime?.waitingInput) return;
    const epoch = state.sessionEpoch;
    setBusy(true, "Preparing source debugger…");
    try {
      const compilation = await completePipeline(epoch);
      if (!compilation || epoch !== state.sessionEpoch || compilation.blocked) return;
      const sessionID = state.pipeline.id;
      const request = {
        id: sessionID,
        maxSteps: 100_000,
        breakpoints: projectBreakpoints(),
        light: true,
        traceFrom: client.traceEnd(state.runtime),
      };
      if (operation === "run-to-cursor") request.cursor = editorSourcePosition();
      const payload = await api(`/api/vm/${operation}`, sessionInputPayload(request, { includeInput: !vmHasStarted() }));
      if (!client.shouldAcceptResponse(sessionID, state.pipeline.id, epoch, state.sessionEpoch)) return;
      applySnapshot(payload.snapshot, true);
      state.consoleCleared = false;
      state.activeTab = "vm";
      if (payload.snapshot.location?.span) selectSpan(payload.snapshot.location.span, false);
      const reason = String(payload.snapshot.stopReason || operation).replaceAll("-", " ");
      setGlobalStatus(`Stopped: ${reason}`, state.runtime?.halted ? "ok" : "idle");
    } catch (error) {
      if (epoch !== state.sessionEpoch) return;
      handleMissingSession(error);
      toast(error.message, "error");
      setGlobalStatus("Source debugger request failed", "error");
    } finally {
      setBusy(false);
    }
  }

  function editorSourcePosition() {
    const before = elements.editor.value.slice(0, elements.editor.selectionStart || 0);
    const lines = before.split("\n");
    return {
      sourceId: state.sourceTabs[state.activeTabIndex]?.id || "",
      line: lines.length,
      column: (lines[lines.length - 1]?.length || 0) + 1,
    };
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
      state.inputHistory = [];
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

  async function sendLiveInput(event) {
    event?.preventDefault();
    const request = state.runtime?.waitingInput;
    if (!request || state.busy || !state.pipeline.id) return;
    const line = String(elements.liveInputLine?.value ?? "");
    const sessionID = state.pipeline.id;
    const epoch = state.sessionEpoch;
    const sequence = Number(request.sequence || 0);
    let resume = false;
    setBusy(true, `Sending ${request.builtin} input…`);
    try {
      const payload = await api("/api/vm/input", {
        id: sessionID,
        line,
        inputSequence: sequence,
        light: true,
        traceFrom: client.traceEnd(state.runtime),
		watches: projectWatches(),
      });
      if (!client.shouldAcceptResponse(sessionID, state.pipeline.id, epoch, state.sessionEpoch)) return;
      if (Number(state.runtime?.waitingInput?.sequence || 0) !== sequence) return;
      applySnapshot(payload.snapshot, true);
	  state.inputHistory.push({ sequence, builtin: request.builtin, line, step: Number(payload.snapshot?.timeline?.currentStep || 0) });
      if (elements.liveInputLine) elements.liveInputLine.value = "";
      state.consoleCleared = false;
      resume = !state.runtime?.halted && !state.runtime?.waitingInput;
    } catch (error) {
      if (epoch !== state.sessionEpoch) return;
      handleMissingSession(error);
      toast(error.message, "error");
      setGlobalStatus("Program input was rejected", "error");
    } finally {
      setBusy(false);
    }
    if (resume && client.shouldAcceptResponse(sessionID, state.pipeline.id, epoch, state.sessionEpoch)) await runSession();
  }

	function applySnapshot(snapshot, light = false, replaceRuntime = false) {
    if (!snapshot) return;
	if (!shouldAcceptTimeline(state.timeline, snapshot.timeline)) return false;
	state.result = light
	  ? (replaceRuntime ? { ...(state.result || {}), runtime: snapshot.runtime } : client.mergeLightSnapshot(state.result, snapshot))
	  : (snapshot.result || state.result);
    state.runtime = state.result?.runtime || snapshot.runtime || null;
	state.heap = null;
    if (state.result && snapshot.runtime) state.result.runtime = state.runtime;
    state.debugLocation = snapshot.location || null;
    state.stopReason = snapshot.stopReason || "ready";
	state.breakpointStates = new Map((snapshot.breakpoints || []).map((item) => [item.breakpoint?.id || `${item.breakpoint?.sourceId || ""}:${item.breakpoint?.line || 0}`, item]));
	state.watchResults = snapshot.watches || [];
	state.debugLog = snapshot.debugLog || [];
	if (snapshot.timeline && state.timeline && snapshot.timeline.revision > state.timeline.revision) {
	  state.inputHistory = state.inputHistory.filter((entry) => Number(entry.step || 0) <= Number(state.timeline.currentStep || 0));
	}
	state.timeline = snapshot.timeline || state.timeline;
    if (light) renderRuntimeSnapshot();
    else renderAll();
    updateSessionStatus(snapshot.status || "ready");
	return true;
  }

	async function reverseCommand(operation) {
	  if (state.busy || state.autoRunning || !state.pipeline.id || !state.timeline) return;
	  const epoch = state.sessionEpoch;
	  const sessionID = state.pipeline.id;
	  const revision = Number(state.timeline.revision || 0);
	  setBusy(true, operation === "step-back" ? "Stepping backward…" : "Reversing to the previous stop…");
	  try {
		const request = {
		  id: sessionID,
		  timelineRevision: revision,
		  light: true,
		  traceFrom: 0,
		  watches: projectWatches(),
		};
		if (operation === "step-back") request.reverseMode = elements.reverseMode?.value || "instruction";
		const payload = await api(`/api/vm/${operation}`, request);
		if (!client.shouldAcceptResponse(sessionID, state.pipeline.id, epoch, state.sessionEpoch)) return;
		if (!shouldAcceptTimeline(state.timeline, payload.snapshot?.timeline)) return;
		state.activeTab = "vm";
		applySnapshot(payload.snapshot, true, true);
		state.consoleCleared = false;
		if (payload.snapshot.location?.span) selectSpan(payload.snapshot.location.span, false);
		setGlobalStatus(operation === "step-back" ? "Stepped backward" : "Reverse stop reached", "idle");
	  } catch (error) {
		if (epoch !== state.sessionEpoch) return;
		handleMissingSession(error);
		toast(error.message, "error");
		setGlobalStatus("Reverse debugger request failed", "error");
	  } finally {
		setBusy(false);
	  }
	}

	let historySeeking = false;
	const submitHistorySeek = createHistorySeeker({
	  busy(value) { historySeeking = value; setBusy(value, "Loading retained step…"); updateSessionStatus(); },
	  request(value) { return api("/api/vm/seek", { id: value.id, targetStep: value.step, timelineRevision: value.revision, light: true, traceFrom: 0, watches: projectWatches() }); },
	  apply(payload, value) {
	    if (!client.shouldAcceptResponse(value.id, state.pipeline.id, value.epoch, state.sessionEpoch)) return;
	    if (!shouldAcceptTimeline(state.timeline, payload.snapshot?.timeline)) return;
	    state.activeTab = "vm";
	    applySnapshot(payload.snapshot, true, true);
	    state.consoleCleared = false;
	    if (payload.snapshot.location?.span) selectSpan(payload.snapshot.location.span, false);
	    setGlobalStatus("Retained step loaded", "idle");
	  },
	  fail(error, value) { if (value.epoch === state.sessionEpoch) { handleMissingSession(error); toast(error.message, "error"); } },
	});
	function seekHistory(value) {
	  if ((state.busy && !historySeeking) || state.autoRunning || !state.timeline || !state.pipeline.id) return;
	  const step = Number(value);
	  if (!Number.isSafeInteger(step) || step < state.timeline.earliestReversibleStep || step > state.timeline.latestStep) return;
	  elements.historySlider.value = String(step);
	  elements.historyStep.value = String(step);
	  submitHistorySeek({ step, id: state.pipeline.id, epoch: state.sessionEpoch, revision: state.timeline.revision });
	}

  function handleMissingSession(error) {
    if (error?.status !== 404) return false;
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
	renderDebuggerTools();
    updateButtons();
  }

  function markRuntimeStage(status) {
    if (!state.result) return;
    state.result.stages = (state.result.stages || []).map((stage) => stage.id === "runtime" ? { ...stage, status } : stage);
    renderPipeline();
  }

  function setRunButtonMode(mode) {
	const presentation = client.runButtonPresentation(mode);
    if (mode === "pause") {
      elements.runButton.disabled = false;
    } else if (mode === "pausing") {
      elements.runButton.disabled = true;
    }
	const label = elements.runButton.querySelector(".button-label");
	if (label) label.textContent = presentation.label;
	elements.runButton.dataset.actionIcon = presentation.icon;
	elements.runButton.setAttribute("aria-label", presentation.label);
	elements.runButton.title = presentation.title;
  }

  function setBusy(busy, label = "Working…", active = "") {
    state.busy = busy;
    elements.compileButton.disabled = busy;
	elements.formatButton.disabled = busy;
    elements.nextPhaseButton.disabled = busy;
    elements.runButton.disabled = busy && active !== "run";
    elements.stepButton.disabled = busy;
    elements.stepIntoButton.disabled = busy;
    elements.stepOverButton.disabled = busy;
    elements.stepOutButton.disabled = busy;
    elements.runToCursorButton.disabled = busy;
	elements.stepBackButton.disabled = busy;
	elements.reverseContinueButton.disabled = busy;
	elements.reverseMode.disabled = busy;
    elements.resetButton.disabled = busy || !state.pipeline.id || !state.pipeline.snapshot?.complete || state.stale;
    const waitingInput = Boolean(state.runtime?.waitingInput);
    if (elements.liveInputLine) elements.liveInputLine.disabled = busy || !waitingInput;
    if (elements.sendInputButton) elements.sendInputButton.disabled = busy || !waitingInput;
    setRunButtonMode(busy && active === "run" ? "pause" : "idle");
    if (busy) setGlobalStatus(label, "busy");
    if (!busy) { updateButtons(); updateSessionStatus(); }
  }


  function setGlobalStatus(text, kind = "idle") {
    elements.globalStatus.innerHTML = `<i class="status-light ${escapeHTML(kind)}"></i>${escapeHTML(text)}`;
  }

  function updateSessionStatus(status) {
    const diagnostics = state.result?.diagnostics || [];
    const runtimeErrors = state.runtime?.diagnostics || [];
    let text = status;
    let kind = "neutral";
    if (state.stale) {
      text = "source changed";
      kind = "paused";
    } else if (hasErrors(diagnostics) || hasErrors(runtimeErrors)) {
      text = "errors";
      kind = "error";
    } else if (state.runtime?.waitingInput) {
      text = "waiting input";
      kind = "paused";
    } else if (state.autoRunning && !client.isBreakpointPause(state.runtime)) {
      text = "running";
      kind = "neutral";
    } else if (state.runtime?.paused) {
      text = "paused";
      kind = "paused";
    } else if (state.runtime?.halted && state.runtime?.executedSteps > 0) {
      text = "halted";
      kind = "ok";
    } else if (state.pipeline.id) {
      text = "ready";
      kind = "ok";
    }
    elements.sessionStatus.textContent = inspectingReport() ? 'report inspection' : text;
    elements.sessionStatus.className = `status-pill ${kind}`;
    if (elements.debugLocation) {
      const location = state.debugLocation;
      elements.debugLocation.textContent = location
        ? `${location.sourceId || "generated"}:${location.span?.startLine || "-"} · ${location.method} · frame ${location.frameDepth} · ${state.stopReason}`
        : `no source location · ${state.stopReason}`;
    }
	if (elements.timelineStatus) {
	  const timeline = state.timeline;
	  if (!timeline) {
		elements.timelineStatus.textContent = "timeline unavailable";
		elements.timelineStatus.classList.remove("historical");
	  } else {
		const historical = Number(timeline.currentStep || 0) < Number(timeline.latestStep || 0);
		const discarded = Boolean(timeline.futureDiscarded);
		elements.timelineStatus.textContent = `step ${timeline.currentStep} · earliest ${timeline.earliestReversibleStep} · latest ${timeline.latestStep} · rev ${timeline.revision}${historical ? " · historical output" : ""}${discarded ? " · future discarded" : ""}`;
		elements.timelineStatus.classList.toggle("historical", historical || discarded);
	  }
	}
	for (const control of [elements.historySlider, elements.historyStep]) {
	  if (!control) continue;
	  control.disabled = !state.timeline || state.autoRunning || (state.busy && !historySeeking) || state.timeline.checkpointCount === 0;
	  control.min = String(state.timeline?.earliestReversibleStep || 0);
	  control.max = String(state.timeline?.latestStep || 0);
	  if (!historySeeking) control.value = String(state.timeline?.currentStep || 0);
	}
  }

  function hasErrors(diagnostics) {
    return diagnostics.some((diagnostic) => diagnostic.severity === "error");
  }

  function renderAll() {
    renderPipeline();
    renderStageGuide();
    renderArtifact();
    renderConsole();
    renderGutter();
    renderSourceHighlights();
	renderDebuggerTools();
    updateButtons();
  }

  function renderStageGuide() {
    const guide = stageGuides[state.activeTab] || stageGuides.diagnostics;
    elements.stageGuide.innerHTML = `<strong>${escapeHTML(guide[0])}</strong><span>${escapeHTML(guide[1])}</span>`;
  }

  function renderPipeline() {
    const stages = state.result?.stages || [
      { id: "lexer", name: "Lexer", status: "ready", summary: "ready" },
      { id: "parser", name: "Parser", status: "waiting", summary: "waiting" },
      { id: "semantic", name: "Semantic analysis", status: "waiting", summary: "waiting" },
      { id: "ir", name: "Stack IR", status: "waiting", summary: "waiting" },
      { id: "codegen", name: "Pseudo-assembly", status: "waiting", summary: "waiting" },
      { id: "runtime", name: "Educational VM", status: "waiting", summary: "waiting" },
    ];
    const nextPhase = state.pipeline.snapshot?.nextPhase || (!state.pipeline.id ? "lexer" : "");
    elements.pipeline.innerHTML = stages.map((stage, index) => `
      <button class="stage-card ${escapeHTML(stage.status || "waiting")}" data-stage="${escapeHTML(stage.id)}" type="button" ${stage.id === nextPhase ? 'aria-current="step"' : ""}>
        <span class="stage-head"><span class="stage-index">0${index + 1}</span><i class="stage-status"></i></span>
        <span class="stage-name">${escapeHTML(stage.name)}</span>
        <span class="stage-summary">${escapeHTML(stage.summary || "")}</span>
      </button>
    `).join("");
  }

  function renderArtifact() {
	$$(".tab", elements.tabs).forEach((tab) => {
	  const active = tab.dataset.tab === state.activeTab;
	  tab.classList.toggle("active", active);
	  tab.setAttribute("aria-selected", String(active));
	  tab.tabIndex = active ? 0 : -1;
	});
    const diagnostics = state.result?.diagnostics || [];
    elements.diagnosticCount.textContent = String(diagnostics.length);
    elements.diagnosticCount.classList.toggle("has-errors", hasErrors(diagnostics));

    if (!state.result && (state.activeTab === "course" || state.activeTab === "mips")) {
      // Course mode and the MIPS panel build their artifacts from the
      // current sources, so they work before a Cooler compilation.
      elements.content.innerHTML = state.activeTab === "course" ? course.courseModeHTML() : course.mipsHTML();
      wireArtifactInteractions();
      return;
    }
    if (!state.result) {
      elements.content.replaceChildren(elements.emptyTemplate.content.cloneNode(true));
      updateSessionStatus("not compiled");
      return;
    }

    const renderers = {
      diagnostics: renderDiagnostics,
      tokens: renderTokens,
      ast: renderAST,
      symbols: renderSymbols,
      ir: renderIR,
      assembly: renderAssembly,
      mips: () => course.mipsHTML(),
      course: () => course.courseModeHTML(),
      cfg: renderCFG,
	  explore: renderExplore,
	  optimization: renderOptimization,
      vm: renderVM,
	  heap: renderHeap,
    };
    elements.content.innerHTML = provenanceBreadcrumbsHTML() + (renderers[state.activeTab] || renderDiagnostics)();
    wireArtifactInteractions(); bindGraphs(elements.content,state.result,span=>selectSpan(span));
	markRelatedArtifacts();
    updateSessionStatus(state.runtime?.paused ? "paused" : "ready");
  }

  function metricsHTML() {
    const metrics = state.result?.metrics || {};
    const entries = [
      [metrics.usefulTokens || 0, "tokens"],
      [metrics.astNodes || 0, "AST nodes"],
      [metrics.classes || 0, "classes"],
      [metrics.methods || 0, "methods"],
      [metrics.symbols || 0, "symbols"],
      [metrics.irInstructions || 0, "IR instructions"],
    ];
    return `<div class="metrics-strip">${entries.map(([value, label]) => `
      <div class="metric-card"><strong>${escapeHTML(String(value))}</strong><span>${escapeHTML(label)}</span></div>
    `).join("")}</div>`;
  }

  function renderDiagnostics() {
    const reference = course.render("diagnostics");
    if (reference) return reference;
    return course.viewSwitchHTML("diagnostics") + renderCoolerDiagnostics();
  }

  function renderCoolerDiagnostics() {
    const diagnostics = state.result?.diagnostics || [];
    if (diagnostics.length === 0) {
      return `${metricsHTML()}<div class="success-card"><div><strong>No diagnostics</strong><br><span>The current source passed every completed stage.</span></div></div>`;
    }
    return `${metricsHTML()}<div class="diagnostic-list">${diagnostics.map((d) => {
      const symbol = d.severity === "error" ? "\u00d7" : d.severity === "warning" ? "!" : "i";
      const related = (d.related || []).map((item) => `${escapeHTML(item.label)} at ${escapeHTML(diagnosticLocationLabel({ ...item, span: item.span || {} }))}`).join(" \u00b7 ");
      const fixes = (d.quickFixes || []).map((fix) => `
        <button class="text-button quick-fix" type="button" data-fix-source="${escapeHTML(fix.span?.sourceId || "")}" data-fix-start="${fix.span?.startOffset || 0}" data-fix-end="${fix.span?.endOffset || 0}" data-fix-value="${encodeURIComponent(fix.replacement || "")}">${escapeHTML(fix.title)}</button>
      `).join("");
      return `
        <article class="diagnostic-card ${escapeHTML(d.severity)} clickable-span" ${spanData(d.span)}>
          <div class="diagnostic-symbol">${symbol}</div>
          <div>
            <div class="diagnostic-message">${escapeHTML(d.message)}</div>
            ${d.hint ? `<div class="diagnostic-hint">${escapeHTML(d.hint)}</div>` : ""}
            ${related ? `<div class="diagnostic-related">${related}</div>` : ""}
            ${fixes ? `<div class="diagnostic-related">${fixes}</div>` : ""}
          </div>
          <div class="diagnostic-meta"><span class="code-pill">${escapeHTML(d.code || d.stage)}</span><span>${escapeHTML(diagnosticLocationLabel(d))}</span></div>
        </article>`;
    }).join("")}</div>`;
  }

  function renderTokens() {
    const reference = course.render("tokens");
    if (reference) return reference;
    return course.viewSwitchHTML("tokens") + renderCoolerTokens();
  }

  function renderCoolerTokens() {
    const tokens = (state.result?.tokens || []).filter((token) => token.type !== "EOF");
    return `${metricsHTML()}<input id="tokenFilter" class="token-filter" type="search" value="${escapeHTML(state.tokenFilter)}" placeholder="Filter tokens" aria-label="Filter tokens"><div class="table-wrap"><table class="data-table">
      <thead><tr><th>#</th><th>Type</th><th>Lexeme</th><th>Literal</th><th>Location</th></tr></thead>
      <tbody>${tokens.map((token, index) => `
        <tr class="clickable clickable-span token-row" data-token-index="${index}" ${spanData(token.span)}>
          <td class="muted-cell">${index}</td>
          <td><span class="token-type">${escapeHTML(token.type)}</span></td>
          <td><code>${escapeHTML(visibleWhitespace(token.lexeme))}</code></td>
          <td>${escapeHTML(token.literal || "")}</td>
          <td class="muted-cell">${token.line}:${token.column}</td>
        </tr>`).join("")}</tbody>
    </table></div>`;
  }

  function renderAST() {
    const reference = course.render("ast");
    if (reference) return reference;
    return course.viewSwitchHTML("ast") + renderCoolerAST();
  }

  function renderCoolerAST() {
    const ast = state.result?.ast;
    if (!ast) return emptyInline("No AST was produced because parsing did not complete.");
    return `${metricsHTML()}${graphHTML(state.result,"ast",document.documentElement.dataset.theme!=="light")}${layoutGraph(state.result,"ast").truncated ? "" : `<details><summary>AST tree</summary><div class="tree">${client.renderASTNode(ast)}</div></details>`}`;
  }

  function renderSymbols() {
    const symbols = state.result?.symbols || [];
    if (symbols.length === 0) return emptyInline("No symbol table is available.");
    return `${metricsHTML()}<div class="table-wrap"><table class="data-table">
      <thead><tr><th>Name</th><th>Kind</th><th>Type / signature</th><th>Owner</th><th>Scope</th><th>State</th><th>Location</th></tr></thead>
      <tbody>${symbols.map((symbol) => `
        <tr class="clickable clickable-span" ${spanData(symbol.span)}>
          <td><code>${escapeHTML(symbol.name)}</code></td>
          <td>${escapeHTML(symbol.kind)}</td>
          <td><code>${escapeHTML(symbol.type)}</code></td>
          <td>${escapeHTML(symbol.owner || "")}</td>
          <td class="muted-cell">${escapeHTML(symbol.scope || "")}</td>
          <td>${symbol.initialized ? "initialized" : "default"}${symbol.mutable ? " \u00b7 mutable" : ""}</td>
          <td class="muted-cell">${symbol.line}:${symbol.column}</td>
        </tr>`).join("")}</tbody>
    </table></div>`;
  }

  function renderIR() {
    const instructions = state.result?.ir || [];
    if (instructions.length === 0) return emptyInline("IR generation is blocked until the source is valid.");
    return `${metricsHTML()}<div class="table-wrap"><table class="data-table">
      <thead><tr><th>#</th><th>Method</th><th>Op</th><th>Arguments</th><th>Comment</th><th>Source</th></tr></thead>
      <tbody>${instructions.map((instruction) => `
        <tr class="clickable clickable-span" ${spanData(instruction.span)}>
          <td class="muted-cell">${instruction.index}</td>
          <td><code>${escapeHTML(instruction.method || "")}</code></td>
          <td><span class="op-pill">${escapeHTML(instruction.op)}</span>${instruction.synthetic ? '<span class="synthetic-badge">generated</span>' : ""}</td>
          <td><code>${escapeHTML((instruction.args || []).join(", "))}</code></td>
          <td>${escapeHTML(instruction.comment || "")}</td>
          <td class="muted-cell">${instruction.line || 1}:${instruction.column || 1}</td>
        </tr>`).join("")}</tbody>
    </table></div>`;
  }

  function renderAssembly() {
    const assembly = state.result?.assembly || [];
    const spans = state.result?.assemblySpans || [];
    if (assembly.length === 0) return emptyInline("Pseudo-assembly is unavailable.");
    return `${metricsHTML()}<pre class="code-block">${assembly.map((line, index) => `<span class="clickable-span" ${spanData(spans[index])}>${escapeHTML(line) || " "}</span>`).join("\n")}</pre>`;
  }

  function renderCFG() {
    const graph = state.result?.controlFlow || {};
    const blocks = graph.blocks || [];
    const edges = graph.edges || [];
    if (blocks.length === 0 || blocks.length > 200 || edges.length > 1000) return graphHTML(state.result,"cfg",document.documentElement.dataset.theme!=="light");
    const byMethod = new Map();
    blocks.forEach((block) => {
      const list = byMethod.get(block.method) || [];
      list.push(block);
      byMethod.set(block.method, list);
    });
    return `${graphHTML(state.result,"cfg",document.documentElement.dataset.theme!=="light")}<details><summary>Control-flow blocks and edges</summary><div class="cfg-layout">
      <div class="cfg-canvas">${Array.from(byMethod.entries()).map(([method, methodBlocks]) => `
        <section class="cfg-method">
          <div class="cfg-method-title">${escapeHTML(method || "program")}</div>
          <div class="cfg-blocks">${methodBlocks.map((block) => `
            <div class="cfg-block clickable-span" ${spanData(block.span)}><strong>${escapeHTML(shortBlockName(block.name))}</strong><span>IR ${escapeHTML(block.range)}</span></div>
          `).join("")}</div>
        </section>`).join("")}</div>
      <div class="cfg-edge-list">${edges.map((edge) => `
        <div class="cfg-edge clickable-span" ${spanData(edge.span)}><span>${escapeHTML(shortBlockName(edge.from))}</span><span class="cfg-edge-kind">${escapeHTML(edge.kind)} \u2192</span><span>${escapeHTML(shortBlockName(edge.to))}</span></div>
      `).join("")}</div>
    </div></details>`;
  }

	function spansOverlap(left, right) {
	  if (!left || !right || !left.sourceId || left.sourceId !== right.sourceId) return false;
	  return Number(left.startOffset || 0) <= Number(right.endOffset ?? right.startOffset ?? 0)
		&& Number(right.startOffset || 0) <= Number(left.endOffset ?? left.startOffset ?? 0);
	}

	function selectedProvenance() {
	  const visualization = state.result?.visualizations;
	  const selected = state.selectedSpan;
	  if (!visualization || !selected) return { groups: [], artifacts: [], counts: new Map() };
	  const groups = (visualization.groups || []).filter((group) => spansOverlap(group.span, selected));
	  const ids = new Set(groups.flatMap((group) => group.artifactIds || []));
	  const artifacts = (visualization.artifacts || []).filter((artifact) => ids.has(artifact.id));
	  const counts = new Map();
	  artifacts.forEach((artifact) => counts.set(artifact.phase, (counts.get(artifact.phase) || 0) + 1));
	  const runtimeMatches = (state.runtime?.trace || []).filter((item) => spansOverlap(item.span, selected)).length;
	  if (runtimeMatches) counts.set("vm", (counts.get("vm") || 0) + runtimeMatches);
	  return { groups, artifacts, counts };
	}

	function provenanceBreadcrumbsHTML() {
	  if (!state.selectedSpan) return "";
	  const related = selectedProvenance();
	  const typed = related.groups
		.filter((group) => group.staticType)
		.sort((left, right) => (left.span.endOffset - left.span.startOffset) - (right.span.endOffset - right.span.startOffset))[0];
	  const phaseOrder = ["tokens", "ast", "symbols", "ir", "cfg", "assembly", "vm", "diagnostics"];
	  const crumbs = phaseOrder.filter((phase) => related.counts.has(phase)).map((phase) =>
		`<button type="button" class="provenance-crumb" data-related-phase="${phase}">${escapeHTML(phase)} <span>${related.counts.get(phase)}</span></button>`).join('<span aria-hidden="true">↔</span>');
	  return `<nav class="provenance-bar" aria-label="Related compiler artifacts">
		<span class="provenance-source">${escapeHTML(state.selectedSpan.sourceId || "generated")}:${Number(state.selectedSpan.startLine || 1)}</span>
		${typed ? `<span class="type-evidence">Recorded type: <code>${escapeHTML(typed.staticType)}</code> · ${escapeHTML(typed.astKind)}</span>` : ""}
		<div class="provenance-crumbs">${crumbs || '<span class="muted-cell">No source-linked artifacts in completed phases</span>'}</div>
	  </nav>`;
	}

	function markRelatedArtifacts() {
	  const related = selectedProvenance();
	  const spans = related.artifacts.map((artifact) => artifact.span).concat(related.groups.map((group) => group.span));
	  $$(".clickable-span", elements.content).forEach((element) => {
		const candidate = readSpanData(element);
		const active = candidate && spans.some((span) => spansOverlap(candidate, span));
		element.classList.toggle("related-artifact", Boolean(active));
		if (active) element.setAttribute("aria-current", "location");
		else element.removeAttribute("aria-current");
	  });
	}

	function currentRuntimeClass() {
	  const frames = state.runtime?.frames || [];
	  return frames.length ? (frames[frames.length - 1].self?.type || "") : "";
	}

	function inheritancePositions(nodes) {
	  const included = new Map(nodes.map((node) => [node.name, node]));
	  const depthMemo = new Map();
	  const depthOf = (name, visiting = new Set()) => {
		if (depthMemo.has(name)) return depthMemo.get(name);
		if (visiting.has(name)) return 0;
		visiting.add(name);
		const parent = included.get(name)?.parent;
		const depth = parent && included.has(parent) ? depthOf(parent, visiting) + 1 : 0;
		visiting.delete(name);
		depthMemo.set(name, depth);
		return depth;
	  };
	  const rows = new Map();
	  nodes.forEach((node) => {
		const depth = depthOf(node.name);
		if (!rows.has(depth)) rows.set(depth, []);
		rows.get(depth).push(node.name);
	  });
	  rows.forEach((names) => names.sort((a, b) => a.localeCompare(b)));
	  const positions = new Map();
	  rows.forEach((names, depth) => names.forEach((name, index) => positions.set(name, { x: 75 + depth * 170, y: 34 + index * 58 })));
	  return positions;
	}

	function inheritanceSVG(visualization, nodes = visualization?.inheritance || [], includeMetadata = false) {
	  const bounded = nodes.slice(0, 120);
	  const positions = inheritancePositions(bounded);
	  const width = Math.max(360, ...Array.from(positions.values()).map((point) => point.x + 90));
	  const height = Math.max(120, ...Array.from(positions.values()).map((point) => point.y + 34));
	  const current = currentRuntimeClass();
	  const edges = (visualization?.edges || []).filter((edge) => positions.has(edge.from) && positions.has(edge.to));
	  const metadata = includeMetadata ? `<metadata>${escapeHTML(JSON.stringify({ schemaVersion: visualization.schemaVersion, compilerVersion: visualization.compilerVersion, sourceUnitIds: visualization.sourceUnitIds }))}</metadata>` : "";
	  return `<svg class="inheritance-svg" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${width} ${height}" role="img" aria-label="Class inheritance graph">${metadata}<style>.inheritance-edges line{stroke:#727c90;stroke-width:1.5}.inheritance-edges .cycle{stroke:#ff6b7a;stroke-dasharray:5 3}.inheritance-edges .recovered{stroke:#f2b85b;stroke-dasharray:3 3}.inheritance-nodes rect{fill:#171b25;stroke:#727c90}.inheritance-nodes text{fill:#eef1f7;font:12px sans-serif}.inheritance-nodes .basic rect{stroke:#41d6b4}.inheritance-nodes .invalid-recovered rect{stroke:#ff6b7a;stroke-width:2}.inheritance-nodes .current-runtime rect{fill:#5140c7;stroke:#a095ff;stroke-width:2}</style>
		<g class="inheritance-edges">${edges.map((edge) => { const from = positions.get(edge.from); const to = positions.get(edge.to); return `<line class="${edge.cycle ? "cycle" : edge.recovered ? "recovered" : ""}" x1="${to.x + 55}" y1="${to.y}" x2="${from.x - 55}" y2="${from.y}"/>`; }).join("")}</g>
		<g class="inheritance-nodes">${bounded.map((node) => { const point = positions.get(node.name); const classes = [node.kind, node.status, node.name === current ? "current-runtime" : ""].filter(Boolean).join(" "); return `<g class="${escapeHTML(classes)}"><rect x="${point.x - 54}" y="${point.y - 17}" width="108" height="34" rx="7"/><text x="${point.x}" y="${point.y + 4}" text-anchor="middle">${escapeHTML(node.name)}</text></g>`; }).join("")}</g>
	  </svg>`;
	}

	function inheritanceDOT(visualization) {
	  const lines = [
		`// Cooler visualization ${visualization?.schemaVersion || "unknown"}`,
		`// compiler ${visualization?.compilerVersion || "unknown"}`,
		`// sources ${(visualization?.sourceUnitIds || []).join(", ")}`,
		"digraph CoolerInheritance {",
	  ];
	  (visualization?.inheritance || []).forEach((node) => lines.push(`  "${String(node.name).replaceAll('"', '\\"')}" [kind="${node.kind}", status="${node.status}"];`));
	  (visualization?.edges || []).forEach((edge) => lines.push(`  "${String(edge.to).replaceAll('"', '\\"')}" -> "${String(edge.from).replaceAll('"', '\\"')}" [status="${edge.status}"];`));
	  lines.push("}");
	  return lines.join("\n");
	}

	function controlFlowDOT(result) {
	  const lines = [
		`// Cooler report ${result?.schemaVersion || "unknown"}`,
		`// compiler ${result?.compilerVersion || "unknown"}`,
		`// sources ${(result?.sourceUnits || []).map((unit) => unit.id).join(", ")}`,
		"digraph CoolerControlFlow {",
	  ];
	  (result?.controlFlow?.blocks || []).forEach((block) => lines.push(`  "${String(block.name).replaceAll('"', '\\"')}" [method="${String(block.method || "").replaceAll('"', '\\"')}", range="${block.range}"];`));
	  (result?.controlFlow?.edges || []).forEach((edge) => lines.push(`  "${String(edge.from).replaceAll('"', '\\"')}" -> "${String(edge.to).replaceAll('"', '\\"')}" [kind="${String(edge.kind).replaceAll('"', '\\"')}"];`));
	  lines.push("}");
	  return lines.join("\n");
	}

	function renderExplore() {
	  const visualization = state.result?.visualizations;
	  if (!visualization) return emptyInline("Complete semantic analysis to build the relationship model.");
	  const allNodes = visualization.inheritance || [];
	  const query = String(state.exploreFilter || "").trim().toLowerCase();
	  const nodes = allNodes.filter((node) => !query || `${node.name} ${node.parent} ${node.kind} ${node.status}`.toLowerCase().includes(query)).slice(0, 120);
	  if (!state.exploreClass || !(visualization.classes || []).some((model) => model.class === state.exploreClass)) {
		state.exploreClass = (visualization.classes || []).find((model) => model.class === "Main")?.class || (visualization.classes || []).find((model) => model.status !== "valid" || model.span?.sourceId)?.class || visualization.classes?.[0]?.class || "";
	  }
	  const model = (visualization.classes || []).find((candidate) => candidate.class === state.exploreClass);
	  const runtimeClass = currentRuntimeClass();
	  const heapObjects = (state.heap?.objects || []).filter((object) => object.class === model?.class);
	  const sharedReferences = new Map();
	  (state.heap?.objects || []).forEach((object) => (object.fields || []).forEach((field) => {
		if (field.value?.kind === "reference" && field.value.objectId) sharedReferences.set(field.value.objectId, (sharedReferences.get(field.value.objectId) || 0) + 1);
	  }));
	  return `<div class="explore-layout">
		<div class="explore-toolbar">
		  <label for="exploreSearch">Filter classes</label>
		  <input id="exploreSearch" type="search" value="${escapeHTML(state.exploreFilter || "")}" placeholder="Class, parent, status" autocomplete="off">
		  <label for="exploreClass">Selected class</label>
		  <select id="exploreClass">${(visualization.classes || []).map((candidate) => `<option value="${escapeHTML(candidate.class)}" ${candidate.class === model?.class ? "selected" : ""}>${escapeHTML(candidate.class)}</option>`).join("")}</select>
		  <span role="status">Showing ${nodes.length} of ${allNodes.length} classes${visualization.truncated ? " · compiler model truncated at safety limits" : ""}</span>
		</div>
		<section class="section-card inheritance-view" aria-labelledby="inheritanceTitle">
		  <div class="section-title"><span id="inheritanceTitle">Inheritance</span><span>deterministic · ${escapeHTML(runtimeClass || "no active runtime class")}</span></div>
		  <div class="inheritance-canvas" aria-hidden="true">${inheritanceSVG(visualization, nodes)}</div>
		  <details open><summary>Accessible inheritance table</summary><div class="table-wrap"><table class="data-table"><caption class="sr-only">Class inheritance graph alternative</caption><thead><tr><th>Class</th><th>Parent</th><th>Kind</th><th>Status</th><th>Diagnostics</th></tr></thead><tbody>${nodes.map((node) => `<tr class="${node.name === runtimeClass ? "runtime-class" : ""}"><td><button type="button" class="text-button clickable-span" data-explore-class="${escapeHTML(node.name)}" ${spanData(node.span)}>${escapeHTML(node.name)}</button></td><td>${escapeHTML(node.parent || "root")}${node.recoveredParent ? ` → recovered as ${escapeHTML(node.recoveredParent)}` : ""}</td><td>${escapeHTML(node.kind)}</td><td>${escapeHTML(node.status)}</td><td>${escapeHTML((node.diagnostics || []).join(", "))}</td></tr>`).join("")}</tbody></table></div></details>
		</section>
		${model ? `<section class="logical-model" aria-labelledby="logicalModelTitle">
		  <div class="model-notice" role="note"><strong id="logicalModelTitle">${escapeHTML(model.class)} logical layout</strong>Educational slot order only — not a native/reference ABI memory layout.</div>
		  <div class="logical-columns">
			<div><h3>Object fields</h3><div class="table-wrap"><table class="data-table"><thead><tr><th>Slot</th><th>Field</th><th>Type</th><th>Origin</th></tr></thead><tbody>${(model.attributes || []).length ? model.attributes.map((attribute) => `<tr class="clickable clickable-span" ${spanData(attribute.span)}><td>${attribute.slot}</td><td><code>${escapeHTML(attribute.name)}</code></td><td>${escapeHTML(attribute.type)}</td><td>${escapeHTML(attribute.owner)} · ${attribute.inherited ? "inherited" : "declared"}</td></tr>`).join("") : '<tr><td colspan="4">No fields</td></tr>'}</tbody></table></div></div>
			<div><h3>Effective dynamic dispatch</h3><div class="table-wrap"><table class="data-table"><thead><tr><th>Slot</th><th>Implementation</th><th>Signature</th><th>Lookup / override</th></tr></thead><tbody>${(model.dispatch || []).map((method) => `<tr class="clickable clickable-span" ${spanData(method.span)}><td>${method.slot}</td><td><code>${escapeHTML(`${method.owner}.${method.name}`)}</code>${method.returnsSelfType ? '<span class="self-type-badge">SELF_TYPE</span>' : ""}</td><td>${escapeHTML(method.signature)}</td><td>${escapeHTML((method.lookupStarts || []).join(" → "))}${method.overrides ? `<br>replaces ${escapeHTML(method.overrides)}` : ""}</td></tr>`).join("")}</tbody></table></div></div>
		  </div>
		  <p class="lookup-order">Static-dispatch lookup starts: ${escapeHTML((model.lookupOrder || []).join(" · "))}</p>
		</section>
		<section class="section-card runtime-layout" aria-labelledby="runtimeInstancesTitle"><div class="section-title"><span id="runtimeInstancesTitle">Runtime instances of ${escapeHTML(model.class)}</span><span>${heapObjects.length}</span></div>
		${heapObjects.length ? heapObjects.map((object) => `<details><summary>${escapeHTML(object.id)} · allocation #${Number(object.allocationOrder || 0)}</summary><div class="table-wrap"><table class="data-table"><thead><tr><th>Field</th><th>Kind</th><th>Value</th></tr></thead><tbody>${(object.fields || []).map((field) => `<tr class="clickable-span" ${spanData(field.span)}><td>${escapeHTML(`${field.owner}.${field.name}`)}</td><td>${escapeHTML(field.value?.kind || "void")}</td><td>${heapValueHTML(field.value)}${sharedReferences.get(field.value?.objectId) > 1 ? '<span class="shared-reference">shared reference · shallow copies preserve this identity</span>' : ""}</td></tr>`).join("")}</tbody></table></div></details>`).join("") : '<p class="diagnostic-hint">Open the Heap tab and inspect a live object to link bounded runtime instances here.</p>'}
		</section>` : ""}
	  </div>`;
	}

	function renderOptimization() {
	  const report = state.result?.optimization;
	  if (!report || !(report.beforeIR || []).length) return emptyInline("Complete IR generation to inspect optimization artifacts.");
	  const before = report.beforeIR || [];
	  const after = report.afterIR || [];
	  const passes = report.passes || [];
	  const irTable = (label, instructions) => `<section><h3>${escapeHTML(label)} <span>${instructions.length} instructions</span></h3><div class="table-wrap optimization-ir-table"><table class="data-table"><caption class="sr-only">${escapeHTML(label)}</caption><thead><tr><th>#</th><th>Method</th><th>Instruction</th><th>Original IR</th><th>Source</th></tr></thead><tbody>${instructions.slice(0, 500).map((instruction) => `<tr class="clickable clickable-span" tabindex="0" ${spanData(instruction.span)}><td>${instruction.index}</td><td><code>${escapeHTML(instruction.method || "")}</code></td><td><span class="op-pill">${escapeHTML(instruction.op)}</span> <code>${escapeHTML((instruction.args || []).join(", "))}</code></td><td>${escapeHTML((instruction.originalIndexes || []).join(", "))}</td><td>${escapeHTML(diagnosticLocationLabel({ span: instruction.span || {} }))}</td></tr>`).join("")}</tbody></table></div>${instructions.length > 500 ? '<p class="optimization-bound" role="status">Showing the first 500 instructions; export the report for the complete artifact.</p>' : ""}</section>`;
	  const cfgTable = (label, graph) => `<section><h3>${escapeHTML(label)} <span>${(graph?.blocks || []).length} blocks · ${(graph?.edges || []).length} edges</span></h3><div class="table-wrap"><table class="data-table"><caption class="sr-only">${escapeHTML(label)}</caption><thead><tr><th>Block</th><th>Method</th><th>IR range</th><th>Outgoing edges</th></tr></thead><tbody>${(graph?.blocks || []).slice(0, 300).map((block) => `<tr class="clickable clickable-span" tabindex="0" ${spanData(block.span)}><td><code>${escapeHTML(block.name)}</code></td><td>${escapeHTML(block.method || "")}</td><td>${escapeHTML(block.range)}</td><td>${(graph?.edges || []).filter((edge) => edge.from === block.name).map((edge) => escapeHTML(`${edge.kind} → ${edge.to}`)).join("<br>") || "terminal"}</td></tr>`).join("")}</tbody></table></div></section>`;
	  return `<div class="optimization-layout">
		<div class="optimization-summary">
		  <div><span>Mode</span><strong>${escapeHTML(report.mode || "none")}</strong></div>
		  <div><span>Execution</span><strong>${escapeHTML(report.executionIR || "unoptimized")}</strong></div>
		  <div><span>Baseline</span><strong>${report.baselineVerified ? "verified" : "not verified"}</strong></div>
		  <div><span>Optimized</span><strong>${report.enabled ? (report.optimizedVerified ? "verified" : "not verified") : "not run"}</strong></div>
		  <div><span>Difference</span><strong>${before.length - after.length} instructions</strong></div>
		</div>
		${report.enabled ? '<div class="optimization-warning" role="note"><strong>Instruction stepping differs from baseline.</strong> Source spans and original IR mappings are retained; choose Opt: none for baseline debugger instruction order.</div>' : '<div class="model-notice" role="note"><strong>Baseline path active.</strong> Select Opt: basic and compile again to run the conservative pass pipeline.</div>'}
		<section class="optimization-passes" aria-labelledby="optimizationPassesTitle"><h3 id="optimizationPassesTitle">Pass order and evidence</h3>
		${passes.length ? passes.map((pass, index) => `<details ${index === 0 ? "open" : ""}><summary><strong>${index + 1}. ${escapeHTML(pass.contract?.name || "pass")}</strong><span>${Number(pass.beforeInstructions || 0)} → ${Number(pass.afterInstructions || 0)} · ${Number(pass.transformations || 0)} changes · ${Number(pass.rejected || 0)} kept · ${pass.verified ? "verified" : "failed"}</span></summary>
		  <dl class="pass-contract"><div><dt>Preconditions</dt><dd>${escapeHTML((pass.contract?.preconditions || []).join(" · "))}</dd></div><div><dt>Preserves</dt><dd>${escapeHTML((pass.contract?.preserves || []).join(" · "))}</dd></div><div><dt>Analysis</dt><dd>${escapeHTML(pass.contract?.analysis || "")}</dd></div><div><dt>May alter</dt><dd>${[pass.contract?.canAlterInstructions ? "instruction count" : "", pass.contract?.canAlterCFG ? "CFG" : "", pass.contract?.canAlterAllocation ? "allocation" : "", pass.contract?.canAlterSourceMapping ? "source mapping" : ""].filter(Boolean).join(" · ") || "no published dimension"}</dd></div></dl>
		  ${(pass.changes || []).length ? `<div class="table-wrap"><table class="data-table"><caption>Transformations</caption><thead><tr><th>Method</th><th>Kind</th><th>Before</th><th>After</th><th>Why safe</th></tr></thead><tbody>${pass.changes.map((change) => `<tr class="clickable clickable-span" tabindex="0" ${spanData(change.span)}><td>${escapeHTML(change.method)}</td><td>${escapeHTML(change.kind)}</td><td><code>${escapeHTML(change.before)}</code></td><td><code>${escapeHTML(change.after)}</code></td><td>${escapeHTML(change.explanation)}</td></tr>`).join("")}</tbody></table></div>` : ""}
		  ${(pass.rejections || []).length ? `<div class="table-wrap"><table class="data-table"><caption>Opportunities deliberately kept</caption><thead><tr><th>Method</th><th>Opportunity</th><th>Reason</th></tr></thead><tbody>${pass.rejections.map((rejection) => `<tr class="clickable clickable-span" tabindex="0" ${spanData(rejection.span)}><td>${escapeHTML(rejection.method)}</td><td><code>${escapeHTML(rejection.opportunity)}</code></td><td>${escapeHTML(rejection.reason)}</td></tr>`).join("")}</tbody></table></div>` : ""}
		</details>`).join("") : '<p class="diagnostic-hint">No optimization passes ran.</p>'}</section>
		<div class="optimization-compare">${irTable("Before · verified unoptimized IR", before)}${irTable("After · execution IR", after)}</div>
		<div class="optimization-compare">${cfgTable("Before CFG", report.beforeCFG)}${cfgTable("After CFG", report.afterCFG)}</div>
	  </div>`;
	}

  function renderVM() {
    const runtime = state.runtime || state.result?.runtime;
    if (!runtime) return emptyInline("Start a debug session to inspect VM state.");
    const stack = runtime.stack || [];
    const frames = runtime.frames || [];
    const trace = (runtime.trace || []).slice(-300);
    return `<div class="vm-layout">
      <div class="vm-column">
        <section class="section-card">
          <div class="section-title">Execution trace <span>${runtime.executedSteps || 0} instructions</span></div>
          <div class="table-wrap trace-table-wrap"><table class="data-table trace-table">
            <thead><tr><th>Step</th><th>Method</th><th>IP</th><th>Op</th><th>Note</th><th>Source</th></tr></thead>
            <tbody>${trace.map((item, index) => `
              <tr class="clickable clickable-span ${index === trace.length - 1 ? "trace-current" : ""}" ${spanData(item.span)}>
                <td>${item.step}</td><td>${escapeHTML(item.method || "")}</td><td>${item.ip}</td><td><span class="op-pill">${escapeHTML(item.op)}</span></td><td>${escapeHTML(item.note || "")}</td><td>${escapeHTML(diagnosticLocationLabel({ span: item.span || {} }))}</td>
              </tr>`).join("")}</tbody>
          </table></div>
        </section>
      </div>
      <div class="vm-column">
        <section class="section-card">
          <div class="section-title">Operand stack <span>${stack.length}</span></div>
          <div class="stack-list">${stack.length ? stack.slice().reverse().map((value, index) => `
			<div class="stack-item"><span class="stack-index">${stack.length - index - 1}</span><span class="value-type">${escapeHTML(value.type)}</span><span class="value-display">${runtimeValueHTML(value)}</span></div>
          `).join("") : '<div class="diagnostic-hint">Empty</div>'}</div>
        </section>
        <section class="section-card">
          <div class="section-title">Call frames <span>${frames.length}</span></div>
          <div class="frame-list">${frames.slice().reverse().map((frame) => `
            <div class="frame-card clickable-span" ${spanData(frame.span)}>
              <div class="frame-head"><span>${escapeHTML(`${frame.owner || ""}.${frame.functionName || ""}`)}</span><span>IP ${frame.ip}</span></div>
              <div class="frame-locals">
				<div class="local-item"><span>self</span><span class="value-type">${escapeHTML(frame.self?.type || "Void")}</span><span class="value-display">${runtimeValueHTML(frame.self)}</span></div>
				${Object.entries(frame.locals || {}).map(([name, value]) => `<div class="local-item"><span>${escapeHTML(name)}</span><span class="value-type">${escapeHTML(value.type)}</span><span class="value-display">${runtimeValueHTML(value)}</span></div>`).join("")}
              </div>
            </div>`).join("") || '<div class="diagnostic-hint">No active frames</div>'}</div>
        </section>
      </div>
    </div>`;
  }

	function runtimeValueHTML(value) {
	  if (!value) return "void";
	  if (!value.object) return escapeHTML(value.display || "void");
	  return `<button class="heap-object-link" type="button" data-heap-object="${escapeHTML(encodeURIComponent(value.object))}" aria-label="Inspect heap object ${escapeHTML(value.object)}">${escapeHTML(value.display || value.object)}</button>`;
	}

	function heapValueHTML(value) {
	  if (!value) return '<span class="heap-unavailable">unavailable</span>';
	  const suffix = value.truncated ? ` <span class="heap-note">(${Number(value.fullLength || 0)} total)</span>` : "";
	  if (value.kind === "reference" && value.objectId && !value.unavailable) {
		return `<button class="heap-object-link" type="button" data-heap-object="${escapeHTML(encodeURIComponent(value.objectId))}" aria-label="Inspect heap object ${escapeHTML(value.objectId)}">${escapeHTML(value.display || value.objectId)}</button>${suffix}`;
	  }
	  return `<span class="${value.unavailable ? "heap-unavailable" : ""}">${escapeHTML(value.display || "void")}</span>${suffix}`;
	}

	function renderHeap() {
	  if (state.heapLoading) return '<div class="heap-loading" role="status">Loading a bounded heap snapshot…</div>';
	  const heap = state.heap;
	  if (!heap) return emptyInline("Open or compile a program, then refresh the heap inspector.");
	  const roots = heap.roots || [];
	  const objects = heap.objects || [];
	  const rootGroups = new Map();
	  for (const root of roots) {
		const match = String(root.label || "").match(/^frame\[(\d+)\]/);
		const label = match ? `Frame ${match[1]}` : root.kind === "stack" ? "Operand stack" : "VM roots";
		if (!rootGroups.has(label)) rootGroups.set(label, []);
		rootGroups.get(label).push(root);
	  }
	  const queryClass = state.heapQuery?.class || "";
	  return `<div class="heap-layout">
		<div class="heap-toolbar">
		  <form class="heap-search" data-heap-search>
			<label for="heapClassSearch">Class or object ID</label>
			<div><input id="heapClassSearch" name="heapSearch" type="search" maxlength="1024" value="${escapeHTML(queryClass)}" placeholder="Node or Node#2"><button class="button secondary" type="submit">Search</button></div>
		  </form>
		  <button class="button secondary" data-heap-refresh type="button">Refresh graph</button>
		</div>
		<div class="metrics-strip heap-metrics">
		  <div class="metric-card"><strong>${Number(heap.totalObjects || 0)}</strong><span>allocated</span></div>
		  <div class="metric-card"><strong>${Number(heap.reachableObjects || 0)}</strong><span>root-reachable</span></div>
		  <div class="metric-card"><strong>${Number(heap.serializedBytes || 0)}</strong><span>JSON bytes</span></div>
		  <div class="metric-card"><strong>${Number(heap.limits?.maxDepth || 0)}</strong><span>depth limit</span></div>
		</div>
		${heap.truncated ? '<div class="heap-warning" role="status">This view was truncated at a configured safety limit. References outside the response are marked unavailable.</div>' : ""}
		<section class="section-card heap-roots" aria-labelledby="heapRootsTitle">
		  <div class="section-title"><span id="heapRootsTitle">Debugger roots</span><span>${roots.length}</span></div>
		  ${roots.length ? `<div class="heap-root-groups">${Array.from(rootGroups.entries()).map(([label, group]) => `<section><h4>${escapeHTML(label)}</h4><ul>${group.map((root) => `<li><code>${escapeHTML(root.label)}</code><span class="value-type">${escapeHTML(root.value?.type || root.value?.kind || "Void")}</span><span>${heapValueHTML(root.value)}</span></li>`).join("")}</ul></section>`).join("")}</div>` : '<div class="diagnostic-hint">No roots returned for this query.</div>'}
		</section>
		<section class="heap-objects" aria-labelledby="heapObjectsTitle" aria-label="Accessible object graph">
		  <h3 id="heapObjectsTitle">Objects <span>${objects.length}</span></h3>
		  ${objects.length ? objects.map((object) => `<article class="heap-object${object.reachable ? " reachable" : ""}">
			<header><button class="heap-object-link heap-object-title" type="button" data-heap-object="${escapeHTML(encodeURIComponent(object.id))}" aria-label="Center graph on ${escapeHTML(object.id)}">${escapeHTML(object.id)}</button><span>allocation #${Number(object.allocationOrder || 0)} · ${object.reachable ? "root-reachable" : "not root-reachable"}</span><button class="text-button" type="button" data-class-layout="${escapeHTML(object.class)}">Class layout</button></header>
			<p>${escapeHTML(object.summary || object.class || "Object")}</p>
			${object.fields?.length ? `<details class="heap-field-tree" open><summary>Fields (${object.fields.length})</summary><div class="table-wrap"><table class="data-table heap-fields"><caption class="sr-only">Fields of ${escapeHTML(object.id)}</caption><thead><tr><th>Field</th><th>Declared</th><th>Value</th></tr></thead><tbody>${object.fields.map((field) => `<tr class="clickable-span" ${spanData(field.span)}><td><code>${escapeHTML(field.name)}</code></td><td>${escapeHTML(`${field.owner || "?"}.${field.staticType || "Object"}`)}</td><td>${heapValueHTML(field.value)}</td></tr>`).join("")}</tbody></table></div></details>` : '<div class="diagnostic-hint">No fields</div>'}
			${object.truncated ? `<div class="heap-note">${Number(object.omittedFields || 0)} additional fields omitted.</div>` : ""}
		  </article>`).join("") : '<div class="diagnostic-hint">No objects match this query.</div>'}
		</section>
		${heap.nextOffset ? `<button class="button secondary heap-more" data-heap-more type="button">Load next ${Number(heap.limits?.maxObjects || 0)} objects</button>` : ""}
	  </div>`;
	}

	async function loadHeap(query = { mode: "graph" }) {
	  if (state.heapLoading || state.busy) return;
	  const epoch = state.sessionEpoch;
	  state.heapLoading = true;
	  if (state.activeTab === "heap") renderArtifact();
	  try {
		const compilation = await completePipeline(epoch);
		if (!compilation || compilation.blocked || epoch !== state.sessionEpoch) return;
		state.activeTab = "heap";
		const normalized = { mode: query.mode || "graph", ...(query.id ? { id: query.id } : {}), ...(query.class ? { class: query.class } : {}), ...(query.offset ? { offset: query.offset } : {}) };
		const payload = await api("/api/vm/heap", { id: state.pipeline.id, heap: normalized });
		if (epoch !== state.sessionEpoch) return;
		state.heap = payload.heap;
		state.heapQuery = normalized;
		setGlobalStatus("Heap snapshot ready", "ok");
	  } catch (error) {
		if (epoch !== state.sessionEpoch) return;
		handleMissingSession(error);
		toast(error.message, "error");
		setGlobalStatus("Heap query failed", "error");
	  } finally {
		state.heapLoading = false;
		if (epoch === state.sessionEpoch && state.activeTab === "heap") {
		  renderStageGuide();
		  renderArtifact();
		}
	  }
	}

	function openHeapObject(id) {
	  if (!id) return;
	  state.activeTab = "heap";
	  renderStageGuide();
	  renderArtifact();
	  void loadHeap({ mode: "graph", id });
	}

	function openClassLayout(className) {
	  if (!className) return;
	  state.exploreClass = className;
	  state.activeTab = "explore";
	  renderStageGuide();
	  renderArtifact();
	}

	function ensureBreakpointConfig(key) {
	  if (!state.breakpointConfigs.has(key)) {
		state.breakpointConfigs.set(key, { enabled: true, condition: "", hitCount: 0, logMessage: "", pause: false });
	  }
	  return state.breakpointConfigs.get(key);
	}

	function renderDebuggerTools() {
	  if (elements.breakpointEditor) {
		const keys = Array.from(state.breakpoints).filter((key) => String(key).includes(":"));
		elements.breakpointEditor.innerHTML = keys.length ? keys.map((key, index) => {
		  const textKey = String(key);
		  const separator = textKey.lastIndexOf(":");
		  const sourceID = textKey.slice(0, separator);
		  const line = Number(textKey.slice(separator + 1));
		  const tab = state.sourceTabs.find((item) => item.id === sourceID);
		  const config = ensureBreakpointConfig(key);
		  const backend = state.breakpointStates.get(textKey);
		  const resolution = resolveBreakpoint(sourceID, line);
		  const status = backend ? (backend.verified ? "verified" : "unresolved") : resolution.status;
		  const diagnostic = backend?.diagnostic || (resolution.status === "unresolved" ? "No executable instruction in this source file." : "");
		  const fieldID = `breakpoint-${index}`;
		  return `<article class="breakpoint-editor-row" data-breakpoint-key="${encodeURIComponent(textKey)}">
			<div class="breakpoint-editor-head">
			  <label class="debugger-check"><input type="checkbox" data-breakpoint-field="enabled"${config.enabled !== false ? " checked" : ""}> Enabled</label>
			  <code>${escapeHTML(tab?.name || sourceID)}:${line}</code>
			  <button class="debugger-remove" type="button" data-remove-breakpoint aria-label="Remove breakpoint at ${escapeHTML(tab?.name || sourceID)} line ${line}">×</button>
			</div>
			<div class="breakpoint-fields">
			  <label class="wide" for="${fieldID}-condition">Condition<input id="${fieldID}-condition" type="text" maxlength="1024" data-breakpoint-field="condition" value="${escapeHTML(config.condition || "")}" placeholder="i = 10"></label>
			  <label for="${fieldID}-hits">Stop/log after hit<input id="${fieldID}-hits" type="number" min="0" max="10000000" data-breakpoint-field="hitCount" value="${Number(config.hitCount || 0)}"></label>
			  <label class="debugger-check"><input type="checkbox" data-breakpoint-field="pause"${config.pause ? " checked" : ""}> Pause after log</label>
			  <label class="wide" for="${fieldID}-log">Log message<input id="${fieldID}-log" type="text" maxlength="1024" data-breakpoint-field="logMessage" value="${escapeHTML(config.logMessage || "")}" placeholder="value = {value}"></label>
			</div>
			<div class="debugger-meta">${escapeHTML(status)}${backend ? ` · ${backend.hits || 0} hits` : ""}</div>
			${diagnostic ? `<div class="debugger-diagnostic" role="status">${escapeHTML(diagnostic)}</div>` : ""}
		  </article>`;
		}).join("") : '<div class="debugger-empty">No breakpoints. Choose a line number in the source gutter.</div>';
	  }

	  if (elements.watchList) {
		const results = new Map(state.watchResults.map((result) => [result.id, result]));
		elements.watchList.innerHTML = state.watches.length ? state.watches.map((watch, index) => {
		  const result = results.get(watch.id);
		  const value = result?.value?.display;
		  return `<article class="watch-row" data-watch-id="${encodeURIComponent(watch.id)}">
			<div class="watch-head">
			  <label class="debugger-check"><input type="checkbox" data-watch-field="enabled"${watch.enabled !== false ? " checked" : ""}> Enabled</label>
			  <span class="watch-value">${result?.value ? runtimeValueHTML(result.value) : escapeHTML(value ?? "not evaluated")}</span>
			  <button class="debugger-remove" type="button" data-remove-watch aria-label="Remove watch ${index + 1}">×</button>
			</div>
			<label class="sr-only" for="watch-${index}">Watch expression ${index + 1}</label>
			<input id="watch-${index}" type="text" maxlength="1024" data-watch-field="expression" value="${escapeHTML(watch.expression)}">
			${result?.dynamicType ? `<div class="debugger-meta">${escapeHTML(result.dynamicType)}${result.dependencies?.length ? ` · reads ${escapeHTML(result.dependencies.join(", "))}` : ""}</div>` : ""}
			${result?.diagnostic ? `<div class="debugger-diagnostic" role="status">${escapeHTML(result.diagnostic)}</div>` : ""}
		  </article>`;
		}).join("") : '<div class="debugger-empty">No watches.</div>';
	  }

	  if (elements.debugLog) {
		elements.debugLog.innerHTML = state.debugLog.length ? state.debugLog.map((entry) => `<li><span>${escapeHTML(entry.message)}</span><small>#${entry.order} · ${escapeHTML(entry.method || "program")} · ${escapeHTML(entry.sourceId || "generated")}:${entry.line} · step ${entry.step}</small></li>`).join("") : '<li class="debugger-empty">No debugger log entries.</li>';
	  }
	}

	function saveDebuggerWorkspace() {
	  const breakpoints = Array.from(state.breakpoints).map((key) => {
		const textKey = String(key);
		const separator = textKey.lastIndexOf(":");
		const sourceID = textKey.slice(0, separator);
		const tab = state.sourceTabs.find((item) => item.id === sourceID);
		return { sourceName: tab?.name || sourceID, line: Number(textKey.slice(separator + 1)), config: ensureBreakpointConfig(key) };
	  });
	  localStorage.setItem(debuggerStorageKey, JSON.stringify({ breakpoints, watches: state.watches }));
	  toast("Debugger breakpoints and watches saved locally.");
	}

	function restoreDebuggerWorkspaceForOpenTabs() {
	  let saved;
	  try {
		saved = JSON.parse(localStorage.getItem(debuggerStorageKey) || "null");
	  } catch {
		return;
	  }
	  if (!saved || typeof saved !== "object") return;
	  for (const item of Array.isArray(saved.breakpoints) ? saved.breakpoints : []) {
		const tab = state.sourceTabs.find((candidate) => candidate.name === item.sourceName);
		const line = Number(item.line || 0);
		if (!tab || line < 1 || state.restoredDebuggerSources.has(tab.name)) continue;
		const key = `${tab.id}:${line}`;
		state.breakpoints.add(key);
		state.breakpointConfigs.set(key, {
		  enabled: item.config?.enabled !== false,
		  condition: String(item.config?.condition || ""),
		  hitCount: Number(item.config?.hitCount || 0),
		  logMessage: String(item.config?.logMessage || ""),
		  pause: Boolean(item.config?.pause),
		});
	  }
	  for (const tab of state.sourceTabs) state.restoredDebuggerSources.add(tab.name);
	  if (!state.debuggerWatchesRestored) {
		for (const watch of Array.isArray(saved.watches) ? saved.watches : []) {
		  if (!watch?.id || state.watches.some((candidate) => candidate.id === watch.id)) continue;
		  state.watches.push({ id: String(watch.id), expression: String(watch.expression || ""), enabled: watch.enabled !== false });
		}
		state.debuggerWatchesRestored = true;
	  }
	  renderDebuggerTools();
	}

  function renderConsole() {
    const runtime = state.runtime || state.result?.runtime;
    const steps = runtime?.executedSteps || 0;
    elements.executionSummary.textContent = `${steps} instruction${steps === 1 ? "" : "s"}`;
    const inputRequest = runtime?.waitingInput;
    if (elements.liveInputForm) elements.liveInputForm.hidden = !inputRequest;
    if (elements.liveInputLabel) {
      elements.liveInputLabel.textContent = inputRequest
        ? `Waiting for ${inputRequest.builtin} in ${inputRequest.method}`
        : "Waiting for program input";
    }
    if (elements.liveInputLine) elements.liveInputLine.disabled = !inputRequest || state.busy;
    if (elements.sendInputButton) elements.sendInputButton.disabled = !inputRequest || state.busy;
    if (elements.inputHistory) {
      elements.inputHistory.innerHTML = state.inputHistory.filter((entry) => !state.timeline || Number(entry.step || 0) <= state.timeline.currentStep)
        .map((entry) => `<li><span class="sr-only">Submitted to ${escapeHTML(entry.builtin)}: </span>${escapeHTML(entry.line || "[empty line]")}</li>`)
        .join("");
    }
    if (state.consoleCleared) {
      elements.console.innerHTML = '<span class="console-muted">Console view cleared. VM output is unchanged.</span>';
      return;
    }
    if (!runtime) {
      elements.console.innerHTML = '<span class="console-muted">Compile or advance a phase to begin.</span>';
      return;
    }
    const output = (runtime.output || []).join("");
    const runtimeErrors = runtime.diagnostics || [];
    let text = output;
    if (runtimeErrors.length) {
      if (text && !text.endsWith("\n")) text += "\n";
      text += runtimeErrors.map((d) => `[${d.code || "runtime"}] ${d.message}`).join("\n");
    }
    if (!text) {
      text = runtime.halted ? "[program produced no output]" : "[no output yet]";
      elements.console.innerHTML = `<span class="console-muted">${escapeHTML(text)}</span>`;
    } else {
      elements.console.textContent = text;
    }
    elements.console.scrollTop = elements.console.scrollHeight;
  }

  function renderGutter() {
    const source = elements.editor.value;
    const lines = Math.max(1, source.split("\n").length);
    const diagnosticLines = new Set((state.result?.diagnostics || []).map((d) => localLineFromSpan(d.span)).filter(Boolean));
    const activeLine = localLineFromSpan(state.debugLocation?.span || lastTraceState()?.span);
    const tab = state.sourceTabs[state.activeTabIndex];
    const fragments = [];
    for (let line = 1; line <= lines; line++) {
      const classes = ["gutter-line"];
      let breakpointLabel = "Toggle breakpoint";
      if (tab && state.breakpoints.has(`${tab.id}:${line}`)) {
        const resolution = resolveBreakpoint(tab.id, line);
        classes.push("breakpoint", `breakpoint-${resolution.status}`);
        breakpointLabel = resolution.status === "moved"
          ? `Breakpoint moved from line ${line} to executable line ${resolution.resolvedLine}`
          : `Breakpoint ${resolution.status} at line ${line}`;
      }
      if (diagnosticLines.has(line)) classes.push("diagnostic");
      if (line === activeLine) classes.push("active");
      fragments.push(`<button class="${classes.join(" ")}" data-line="${line}" type="button" aria-label="${breakpointLabel}"><span class="marker" aria-hidden="true"></span><span class="number">${line}</span></button>`);
    }
    elements.gutter.innerHTML = fragments.join("");
    elements.gutter.scrollTop = elements.editor.scrollTop;
  }

  function wireArtifactInteractions() {
	$$(COURSE_CLICK_SELECTOR, elements.content).forEach((element) => element.addEventListener("click", (event) => {
	  event.stopPropagation();
	  course.handleClick(element);
	}));
	$$("[data-course-input]", elements.content).forEach((field) => {
	  field.addEventListener("keydown", (event) => {
	    if (event.key !== "Enter") return;
	    event.preventDefault();
	    const send = field.closest(".course-live-input")?.querySelector('[data-course-action="send-input"]');
	    if (send) course.handleClick(send);
	  });
	  field.focus();
	});
	$$('[data-related-phase]', elements.content).forEach((button) => button.addEventListener("click", () => {
	  const phaseTabs = { tokens: "tokens", ast: "ast", symbols: "symbols", ir: "ir", cfg: "cfg", assembly: "assembly", vm: "vm", diagnostics: "diagnostics" };
	  setActiveTab(phaseTabs[button.dataset.relatedPhase] || "explore");
	}));
	$$('[data-class-layout]', elements.content).forEach((button) => button.addEventListener("click", (event) => {
	  event.stopPropagation();
	  openClassLayout(button.dataset.classLayout || "");
	}));
	$$('[data-explore-class]', elements.content).forEach((button) => button.addEventListener("click", (event) => {
	  event.stopPropagation();
	  state.exploreClass = button.dataset.exploreClass || "";
	  const span = readSpanData(button);
	  if (span) selectSpan(span, false);
	  renderArtifact();
	}));
	const exploreSearch = elements.content.querySelector("#exploreSearch");
	if (exploreSearch) exploreSearch.addEventListener("input", () => {
	  state.exploreFilter = exploreSearch.value;
	  const position = exploreSearch.selectionStart;
	  renderArtifact();
	  const replacement = elements.content.querySelector("#exploreSearch");
	  replacement?.focus();
	  replacement?.setSelectionRange(position, position);
	});
	const exploreClass = elements.content.querySelector("#exploreClass");
	if (exploreClass) exploreClass.addEventListener("change", () => {
	  state.exploreClass = exploreClass.value;
	  renderArtifact();
	  elements.content.querySelector("#exploreClass")?.focus();
	});
	$$('[data-heap-object]', elements.content).forEach((button) => button.addEventListener("click", (event) => {
	  event.stopPropagation();
	  openHeapObject(decodeURIComponent(button.dataset.heapObject || ""));
	}));
	elements.content.querySelector("[data-heap-refresh]")?.addEventListener("click", () => void loadHeap({ mode: "graph" }));
	elements.content.querySelector("[data-heap-search]")?.addEventListener("submit", (event) => {
	  event.preventDefault();
	  const value = String(new FormData(event.currentTarget).get("heapSearch") || "").trim();
	  void loadHeap(value.includes("#") ? { mode: "graph", id: value } : { mode: "search", class: value });
	});
	elements.content.querySelector("[data-heap-more]")?.addEventListener("click", () => void loadHeap({ ...state.heapQuery, offset: state.heap?.nextOffset || 0 }));
    $$(".tree-toggle", elements.content).forEach((button) => {
      button.addEventListener("click", () => client.toggleASTBranch(button));
    });
    $$(".clickable-span", elements.content).forEach((element) => {
      element.addEventListener("click", (event) => {
        if (event.target.closest(".quick-fix")) return;
        const span = readSpanData(element);
        if (span) selectSpan(span, true);
      });
	  if (element.tabIndex === 0) element.addEventListener("keydown", (event) => {
		if (event.key !== "Enter" && event.key !== " ") return;
		event.preventDefault();
		element.click();
	  });
    });
    $$(".quick-fix", elements.content).forEach((button) => {
      button.addEventListener("click", (event) => {
        event.stopPropagation();
        if(inspectingReport())return; const sourceID = button.dataset.fixSource || "";
        const fileIndex = sourceTabIndex(sourceID);
        if (fileIndex < 0) return;
        if (fileIndex !== state.activeTabIndex) switchSourceTab(fileIndex);
        const applied = client.applyQuickFix(elements.editor.value, {
          span: {
            startOffset: Math.max(0, Number(button.dataset.fixStart || 0)),
            endOffset: Math.max(0, Number(button.dataset.fixEnd || button.dataset.fixStart || 0)),
          },
          replacement: decodeURIComponent(button.dataset.fixValue || ""),
        });
        elements.editor.value = applied.source;
        elements.editor.focus();
        elements.editor.setSelectionRange(applied.selectionStart, applied.selectionEnd);
        sourceChanged();
      });
    });
    const tokenFilter = $("#tokenFilter", elements.content);
    if (tokenFilter) {
      const applyFilter = () => {
        state.tokenFilter = tokenFilter.value;
        const query = state.tokenFilter.trim().toLowerCase();
        const tokens = (state.result?.tokens || []).filter((token) => token.type !== "EOF");
        $$(".token-row", elements.content).forEach((row) => {
          const token = tokens[Number(row.dataset.tokenIndex)];
          const text = `${token?.type || ""} ${token?.lexeme || ""} ${token?.literal || ""}`.toLowerCase();
          row.hidden = Boolean(query && !text.includes(query));
        });
      };
      tokenFilter.addEventListener("input", applyFilter);
      applyFilter();
    }
  }

  function renderSourceHighlights() {
    const source = elements.editor.value;
    const sourceID = state.sourceTabs[state.activeTabIndex]?.id || "";
    const tokens = (state.result?.tokens || [])
      .filter((token) => token.type !== "EOF" && token.span)
      .filter((token) => !token.span.sourceId || token.span.sourceId === sourceID)
      .sort((left, right) => left.span.startOffset - right.span.startOffset);
    let cursor = 0;
    const parts = [];
    for (const token of tokens) {
      const start = clamp(Number(token.span.startOffset || 0), cursor, source.length);
      const end = clamp(Number(token.span.endOffset || start), start, source.length);
      parts.push(escapeHTML(source.slice(cursor, start)));
      parts.push(`<span class="source-token ${sourceTokenClass(token.type)}">${escapeHTML(source.slice(start, end))}</span>`);
      cursor = end;
    }
    parts.push(escapeHTML(source.slice(cursor)) || " ");
    elements.sourceHighlights.innerHTML = parts.join("");
    syncEditorScroll();
  }

  function sourceTokenClass(type) {
    if (["CLASS", "ELSE", "FI", "IF", "IN", "INHERITS", "ISVOID", "LET", "LOOP", "POOL", "THEN", "WHILE", "CASE", "ESAC", "NEW", "OF", "NOT"].includes(type)) return "keyword";
    if (type === "TYPE_ID") return "type";
    if (type === "STR_CONST") return "string";
    if (type === "INT_CONST") return "number";
    if (type === "BOOL_CONST") return "boolean";
    if (["ASSIGN", "CASE_ARROW", "PLUS", "MINUS", "STAR", "SLASH", "TILDE", "LESS", "LESS_EQUAL", "EQUAL"].includes(type)) return "operator";
    return "identifier";
  }

  function syncEditorScroll() {
    elements.gutter.scrollTop = elements.editor.scrollTop;
    elements.sourceHighlights.scrollTop = elements.editor.scrollTop;
    elements.sourceHighlights.scrollLeft = elements.editor.scrollLeft;
  }

  function spanData(span) {
    if (!span) return "";
    return `data-source-id="${escapeHTML(span.sourceId || "")}" data-start="${Number(span.startOffset || 0)}" data-end="${Number(span.endOffset || span.startOffset || 0)}" data-line="${Number(span.startLine || 1)}" data-column="${Number(span.startColumn || 1)}"`;
  }

  function readSpanData(element) {
    if (element.dataset.start === undefined) return null;
    return {
      sourceId: element.dataset.sourceId || "",
      startOffset: Number(element.dataset.start || 0),
      endOffset: Number(element.dataset.end || element.dataset.start || 0),
      startLine: Number(element.dataset.line || 1),
      startColumn: Number(element.dataset.column || 1),
    };
  }

  function selectSpan(span, focus = true) {
    if (!span) return;
	state.selectedSpan = { ...span };
    const index = sourceTabIndex(span.sourceId);
    if (index >= 0 && index !== state.activeTabIndex) switchSourceTab(index);
    const sourceLength = elements.editor.value.length;
    const start = clamp(Number(span.startOffset || 0), 0, sourceLength);
    let end = clamp(Number(span.endOffset || span.startOffset || 0), start, sourceLength);
    if (end === start && start < sourceLength) end++;
    elements.editor.setSelectionRange(start, end);
    if (focus) elements.editor.focus({ preventScroll: true });
    scrollEditorToLine(Number(span.startLine || 1));
    updateEditorMetadata();
	markRelatedArtifacts();
  }

  function scrollEditorToLine(line) {
    const lineHeight = parseFloat(getComputedStyle(elements.editor).lineHeight) || 20;
    const target = Math.max(0, (line - 4) * lineHeight);
    elements.editor.scrollTop = target;
    syncEditorScroll();
  }

  function updateEditorMetadata() {
    const source = elements.editor.value;
    const position = elements.editor.selectionStart || 0;
    const before = source.slice(0, position);
    const line = before.split("\n").length;
    const lastBreak = before.lastIndexOf("\n");
    const column = position - lastBreak;
    const lineCount = Math.max(1, source.split("\n").length);
    elements.sourcePosition.textContent = `Ln ${line}, Col ${column}`;
    elements.sourceSize.textContent = `${lineCount} line${lineCount === 1 ? "" : "s"}`;
  }

	function updateSourceProvenance() {
	  if (!state.result) return;
	  const tab = state.sourceTabs[state.activeTabIndex];
	  if (!tab) return;
	  const start = Number(elements.editor.selectionStart || 0);
	  const end = Math.max(start, Number(elements.editor.selectionEnd || start));
	  const before = elements.editor.value.slice(0, start);
	  state.selectedSpan = {
		sourceId: tab.id, startOffset: start, endOffset: end,
		startLine: before.split("\n").length, startColumn: start - before.lastIndexOf("\n"),
	  };
	  renderArtifact();
	}

  const editorKeys=new WeakMap();
  function historyKey(tab){if(!editorKeys.has(tab))editorKeys.set(tab,Symbol());return editorKeys.get(tab);}
  const editorHistory=new EditorHistory();let historyReplay=false, beforeInputSelection=null;
  function undoEditor(redo=false){
    if(inspectingReport())return;const tab=state.sourceTabs[state.activeTabIndex];if(!tab)return;
    const stack=redo?editorHistory.future:editorHistory.past;const entry=stack.findLast(e=>e.key===historyKey(tab));
    if(entry && (redo?entry.before.text:entry.after.text)!==tab.source){editorHistory.forget(historyKey(tab));return;}
    const snapshot=editorHistory.move(historyKey(tab),redo);if(!snapshot)return;
    historyReplay=true;try{elements.editor.value=snapshot.text;elements.editor.setSelectionRange(snapshot.start,snapshot.end);sourceChanged();}finally{historyReplay=false;}
  }
  let diagnosticCursor=-1, diagnosticResult=null;
  function navigateDiagnostic(direction) {
    if(state.stale || !state.result){setGlobalStatus("Compile to navigate diagnostics", "idle");return;}
    if(diagnosticResult!==state.result){diagnosticCursor=-1;diagnosticResult=state.result;}
    const items=(state.result.diagnostics||[]).filter(d=>d.span && sourceTabIndex(d.span.sourceId)>=0);
    diagnosticCursor=nextDiagnosticIndex(items.length,diagnosticCursor,direction);
    if(diagnosticCursor<0){setGlobalStatus("No source diagnostics", "ok");return;}
    const d=items[diagnosticCursor];selectSpan(d.span);setGlobalStatus(`${diagnosticCursor+1}/${items.length}: ${d.message}`,d.severity==="error"?"error":"idle");
  }

  function sourceChanged() {
	if(inspectingReport()){elements.editor.value=state.sourceTabs[state.activeTabIndex]?.source || '';return;}
    const tab=state.sourceTabs[state.activeTabIndex];
    if(tab && tab.source!==elements.editor.value){
      if(!historyReplay)editorHistory.record(historyKey(tab),{text:tab.source,start:elements.editor.coolerSelection?.before.start??beforeInputSelection?.start??tab.selectionStart??0,end:elements.editor.coolerSelection?.before.end??beforeInputSelection?.end??tab.selectionEnd??0},{text:elements.editor.value,start:elements.editor.coolerSelection?.after.start??elements.editor.selectionStart,end:elements.editor.coolerSelection?.after.end??elements.editor.selectionEnd});
      beforeInputSelection=null;
      const mapped=[];
      for(const key of state.breakpoints){const oldLine=breakpointLineForSource(key,tab.id);if(oldLine===null)continue;const line=mapBreakpointLine(tab.source,elements.editor.value,oldLine);mapped.push({key,line,config:state.breakpointConfigs.get(key)});}
      for(const {key} of mapped){state.breakpoints.delete(key);state.breakpointConfigs.delete(key);state.breakpointStates.delete(key);}
      for(const {line,config} of mapped){if(line!==null){const key=`${tab.id}:${line}`;state.breakpoints.add(key);if(config)state.breakpointConfigs.set(key,config);}}
    }
    flushActiveTab();
    invalidateSession();
    state.stale = true;
    state.consoleCleared = false;
    updateEditorMetadata();
    renderSourceTabs();
    renderAll();
    setGlobalStatus("Source changed — pipeline reset", "idle");
  }

  function updateButtons() {
    const snapshot = state.pipeline.snapshot;
    const terminal = Boolean(snapshot?.complete || snapshot?.blocked);
    const next = phaseLabels[snapshot?.nextPhase] || "Lexer";
    const runtimeDone = Boolean(state.pipeline.id && (
      state.runtime?.halted || (state.runtime?.diagnostics || []).some((diagnostic) => diagnostic.severity === "error")
    ));
    elements.nextPhaseButton.textContent = snapshot?.blocked
      ? "Pipeline Blocked"
      : snapshot?.complete
        ? "Pipeline Ready"
        : `Next: ${next}`;
    elements.nextPhaseButton.disabled = state.busy || terminal;
    elements.compileButton.disabled = state.busy;
    if (elements.optimizationMode) elements.optimizationMode.disabled = state.busy;
    if (elements.frontendMode) elements.frontendMode.disabled = state.busy;
    elements.runButton.disabled = client.shouldDisableRunButton({
      busy: state.busy,
      autoRunning: state.autoRunning,
      pauseRequested: state.pauseRequested,
      runtimeDone,
      waitingInput: Boolean(state.runtime?.waitingInput),
    });
    elements.stepButton.disabled = state.busy || runtimeDone || Boolean(state.runtime?.waitingInput);
    const sourceCommandDisabled = state.busy || runtimeDone || Boolean(state.runtime?.waitingInput);
    elements.stepIntoButton.disabled = sourceCommandDisabled;
    elements.stepOverButton.disabled = sourceCommandDisabled;
    elements.stepOutButton.disabled = sourceCommandDisabled || !state.debugLocation || Number(state.debugLocation.frameDepth) <= 0;
    elements.runToCursorButton.disabled = sourceCommandDisabled;
	elements.stepBackButton.disabled = state.busy || !state.timeline?.canStepBack;
	elements.reverseContinueButton.disabled = state.busy || !state.timeline?.canReverseContinue;
	elements.reverseMode.disabled = state.busy || !state.timeline?.canStepBack;
    elements.resetButton.disabled = state.busy || !state.pipeline.id || !snapshot?.complete || state.stale;
    elements.exportButton.disabled = !state.result;
	const locked = inspectingReport();
	$('.debugger-panel').hidden=locked;
	if(elements.optimizationMode)elements.optimizationMode.disabled=state.busy || locked;
	elements.programInput.readOnly=locked;
	for(const id of ['restoreDrafts','discardDrafts','clearDrafts'])$('#'+id).disabled=Boolean(reportWorkspace);
	if(locked){if(elements.liveInputLine)elements.liveInputLine.disabled=true;if(elements.sendInputButton)elements.sendInputButton.disabled=true;}
	elements.editor.readOnly = locked;
	for (const control of [elements.openButton,elements.addFileButton,elements.clearSourceButton,elements.exampleSelect,elements.recentSelect,elements.saveButton,elements.saveAsButton,elements.formatButton]) control.disabled = state.busy || locked;
	if (locked) for (const control of [elements.compileButton,elements.nextPhaseButton,elements.runButton,elements.stepButton,elements.stepIntoButton,elements.stepOverButton,elements.stepOutButton,elements.runToCursorButton,elements.resetButton]) control.disabled=true;
	$('#openReportButton').disabled = state.busy || Boolean(reportWorkspace);
	$('#recompileReport').disabled = state.busy || !reportWorkspace?.sources?.length || !locked;
	$('#closeReport').disabled = state.busy;
	$$('.tab-close',elements.sourceTabs).forEach(control => {control.disabled=locked;});
  }

  function lastTraceState() {
    const trace = state.runtime?.trace || [];
    return trace.length ? trace[trace.length - 1] : null;
  }

  function setActiveTab(tab) {
    state.activeTab = tab;
    renderStageGuide();
    renderArtifact();
	if (tab === "heap" && !state.heap && !state.heapLoading) void loadHeap({ mode: "graph" });
  }

  function readFileAsText(file) {
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve({
        name: client.normalizeFileName(file.name || "program.cl"),
        source: String(reader.result || ""),
      });
      reader.onerror = () => reject(reader.error || new Error("read failed"));
      reader.readAsText(file);
    });
  }

  async function openFiles(fileList, { append = false } = {}) {
	if(inspectingReport())return;
    const files = Array.from(fileList || []).filter(Boolean);
    if (files.length === 0) return;
    if (!append && !confirmDiscard()) return;
    const epoch = state.sessionEpoch;
    const readEpoch = ++state.fileReadEpoch;
    try {
      const loaded = await Promise.all(files.map(readFileAsText));
      if (readEpoch !== state.fileReadEpoch || epoch !== state.sessionEpoch) return;
      if (append) {
        flushActiveTab();
        for (const item of loaded) {
          state.sourceTabs.push(makeSourceTab(item.name, item.source));
          rememberRecentFile(item.name, item.source);
        }
        state.activeTabIndex = state.sourceTabs.length - 1;
        invalidateSession();
        state.stale = false;
        loadActiveTabIntoEditor();
        renderAll();
        setGlobalStatus(`Added ${loaded.map((item) => item.name).join(" + ")}`, "idle");
        return;
      }
      invalidateSession();
      state.breakpoints.clear();
	  state.breakpointConfigs.clear();
      state.stale = false;
      state.sourceTabs = loaded.map((item) => makeSourceTab(item.name, item.source));
      state.activeTabIndex = 0;
      for (const item of loaded) rememberRecentFile(item.name, item.source);
      elements.exampleSelect.value = "";
      loadActiveTabIntoEditor();
      renderAll();
      setGlobalStatus(`Opened ${loaded.map((item) => item.name).join(" + ")}`, "idle");
    } catch {
      toast("Could not read the selected file.", "error");
    }
  }

  function saveSource() {
	if(inspectingReport())return;
    flushActiveTab();
    const tab = state.sourceTabs[state.activeTabIndex];
    const name = client.normalizeFileName((tab && tab.name) || state.documentName || "program.cl");
    if (tab) {
      tab.name = name;
      tab.savedSource = tab.source;
	  tab.recovered = false;
    }
    state.documentName = name;
    state.savedSource = tab ? tab.source : elements.editor.value;
    rememberRecentFile(name, tab ? tab.source : elements.editor.value);
    download(name, tab ? tab.source : elements.editor.value, "text/plain;charset=utf-8");
    updateDocumentTitle();
    renderSourceTabs();
    toast(`Saved ${name}`);
  }

  function saveSourceAs() {
	if(inspectingReport())return;
    const tab = state.sourceTabs[state.activeTabIndex];
    const requested = window.prompt(".cl file name", (tab && tab.name) || state.documentName || "program.cl");
    if (!requested) return;
    const name = client.normalizeFileName(requested);
    if (tab) tab.name = name;
    state.documentName = name;
    saveSource();
  }

  function readRecentFiles() {
    try {
      const parsed = JSON.parse(localStorage.getItem(recentStorageKey) || "[]");
      return Array.isArray(parsed) ? parsed.filter((item) => item?.name && typeof item.source === "string") : [];
    } catch {
      return [];
    }
  }

  function rememberRecentFile(name, source) {
    try {
      const files = client.mergeRecentFiles(readRecentFiles(), name, source);
      localStorage.setItem(recentStorageKey, JSON.stringify(files));
      populateRecentFiles();
    } catch {
      toast("Could not update recent files in local storage.", "error");
    }
  }

  function populateRecentFiles() {
    const files = readRecentFiles();
    elements.recentSelect.innerHTML = [
      '<option value="">Recent files…</option>',
      ...files.map((file, index) => `<option value="${index}">${escapeHTML(file.name)}</option>`),
    ].join("");
  }

  function loadRecentFile() {
    const selected = elements.recentSelect.value;
    elements.recentSelect.value = "";
    if (selected === "") return;
    const file = readRecentFiles()[Number(selected)];
    if (!file || !confirmDiscard()) return;
    replaceDocument(file.source, file.name);
    setGlobalStatus(`Opened recent file ${file.name}`, "idle");
  }

  function confirmDiscard() {
    flushActiveTab();
    const dirty = state.sourceTabs.some((tab) => tab.source !== tab.savedSource);
    if (!dirty) return true;
    return window.confirm("Discard unsaved changes to the current COOL source?");
  }

  async function exportSelectedArtifact() {
    const courseExports = { "ref-tokens": ["tokens", "tokens.ref.txt"], "ref-ast": ["ast", "ast.ref.txt"], "ref-semant": ["semant", "typed-ast.ref.txt"], "ref-diagnostics": ["diagnostics", "diagnostics.ref.txt"], "mips-s": ["mips", "s"] };
    if (courseExports[elements.exportSelect.value]) {
      const [artifact, file] = courseExports[elements.exportSelect.value];
      await course.exportArtifact(artifact, file);
      return;
    }
    if (!state.result) return;
    const stem = client.stripExtension(state.documentName || "program.cl");
    if (/^(ast|cfg)-(svg|png)$/.test(elements.exportSelect.value)) { const [kind,format]=elements.exportSelect.value.split("-"); const graph=layoutGraph(state.result,kind); try { if(format==="svg") download(`${stem}.${kind}.svg`,graphSVG(graph,document.documentElement.dataset.theme!=="light"),"image/svg+xml"); else downloadBlob(`${stem}.${kind}.png`,await graphPNG(graph,Number($("#pngScale").value),document.documentElement.dataset.theme!=="light")); setGlobalStatus("Graph exported", "ok"); toast("Graph exported"); } catch(error){setGlobalStatus(error.message,"error"); toast(error.message,"error");} return; }
    switch (elements.exportSelect.value) {
      case "tokens":
        download(`${stem}.tokens.json`, JSON.stringify(state.result.tokens || [], null, 2), "application/json;charset=utf-8");
        break;
      case "ast":
        download(`${stem}.ast.json`, JSON.stringify(state.result.ast ?? null, null, 2), "application/json;charset=utf-8");
        break;
      case "symbols":
        download(`${stem}.symbols.json`, JSON.stringify(state.result.symbols || [], null, 2), "application/json;charset=utf-8");
        break;
      case "ir":
        download(`${stem}.ir.txt`, (state.result.ir || []).map(client.formatIRLine).join("\n"), "text/plain;charset=utf-8");
        break;
      case "assembly":
        download(`${stem}.assembly.txt`, (state.result.assembly || []).join("\n"), "text/plain;charset=utf-8");
        break;
      case "trace":
        download(`${stem}.vm-trace.json`, JSON.stringify(state.runtime?.trace || [], null, 2), "application/json;charset=utf-8");
        break;
	  case "visualizations":
		download(`${stem}.visualizations.json`, JSON.stringify({ ...(state.result.visualizations || {}), selection: { class: state.exploreClass || "" } }, null, 2), "application/json;charset=utf-8");
		break;
	  case "inheritance-svg":
		download(`${stem}.inheritance.svg`, inheritanceSVG(state.result.visualizations, state.result.visualizations?.inheritance || [], true), "image/svg+xml;charset=utf-8");
		break;
	  case "inheritance-dot":
		download(`${stem}.inheritance.dot`, inheritanceDOT(state.result.visualizations), "text/vnd.graphviz;charset=utf-8");
		break;
	  case "cfg-dot":
		download(`${stem}.control-flow.dot`, controlFlowDOT(state.result), "text/vnd.graphviz;charset=utf-8");
		break;
      case "zip":
        flushActiveTab();
        downloadBlob(`${stem}.cooler-report.zip`, client.buildZipBlob(
          await client.buildArchiveFiles(
            state.result,
            state.documentName,
            state.sourceTabs.map((tab) => ({ id: tab.id, name: tab.name, content: tab.source })),
          ),
        ));
        break;
      case "report":
      default:
        download(`${stem}.cooler-report.json`, JSON.stringify(
          { ...client.buildCompileReport(state.result, state.documentName), ...reportSources(state.result,state.sourceTabs) },
          null,
          2,
        ), "application/json;charset=utf-8");
        break;
    }
    toast("Selected compiler artifact exported.");
  }

  function download(filename, content, type) {
    downloadBlob(filename, new Blob([content], { type }));
  }

  // Every export goes through the store-sandbox-friendly path: the share
  // sheet on touch builds, a Blob download elsewhere.
  function downloadBlob(filename, blob) {
    void shareOrDownload(filename, blob, blob.type || "application/octet-stream");
  }

  function toggleTheme() {
    const current = document.documentElement.dataset.theme || "dark";
    const next = current === "dark" ? "light" : "dark";
    document.documentElement.dataset.theme = next;
    localStorage.setItem("cooler-theme", next); renderArtifact();
  }

  function restoreTheme() {
    const saved = localStorage.getItem("cooler-theme");
    if (saved === "light" || saved === "dark") document.documentElement.dataset.theme = saved;
  }

  function toast(message, kind = "normal") {
    const item = document.createElement("div");
    item.className = `toast ${kind}`;
    item.textContent = message;
    elements.toastRegion.appendChild(item);
    setTimeout(() => item.remove(), 3600);
  }

  function emptyInline(message) {
    return `<div class="empty-state"><div class="empty-icon">C</div><h2>Artifact unavailable</h2><p>${escapeHTML(message)}</p></div>`;
  }

  function shortBlockName(name) {
    const parts = String(name || "").split(":");
    return parts[parts.length - 1] || name;
  }

  function visibleWhitespace(value) {
    return String(value || "").replace(/\n/g, "↵").replace(/\t/g, "⇥");
  }

  function clamp(value, min, max) {
    return Math.min(max, Math.max(min, value));
  }

  function escapeHTML(value) {
    return String(value ?? "")
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;")
      .replace(/'/g, "&#039;");
  }

  function bindEvents() {
	$("#restoreDrafts").addEventListener("click", restoreDrafts);
	$("#discardDrafts").addEventListener("click", clearDrafts);
	$("#clearDrafts").addEventListener("click", clearDrafts);
	window.addEventListener("pagehide", () => { void draftWriter.flush(); });
	document.addEventListener("visibilitychange", () => { if (document.visibilityState === "hidden") void draftWriter.flush(); });
    elements.exampleSelect.addEventListener("change", () => {
      const example = state.examples.find((item) => item.id === elements.exampleSelect.value);
      if (example) loadExample(example);
    });
    elements.recentSelect.addEventListener("change", loadRecentFile);
    elements.clearSourceButton.addEventListener("click", () => {
      if (!confirmDiscard()) return;
      replaceDocument("", "untitled.cl", false);
      setProgramInput("");
      elements.editor.focus();
      setGlobalStatus("Editor cleared", "idle");
    });
    elements.openButton.addEventListener("click", () => elements.fileInput.click());
    elements.addFileButton.addEventListener("click", () => elements.addFileInput.click());
    elements.fileInput.addEventListener("change", () => {
      void openFiles(elements.fileInput.files, { append: false });
      elements.fileInput.value = "";
    });
    elements.addFileInput.addEventListener("change", () => {
      void openFiles(elements.addFileInput.files, { append: true });
      elements.addFileInput.value = "";
    });
    elements.sourceTabs.addEventListener("click", (event) => {
      const closer = event.target.closest(".tab-close");
      if (closer) {
        event.preventDefault();
        event.stopPropagation();
        closeSourceTab(Number(closer.dataset.closeIndex));
        return;
      }
      const tab = event.target.closest("[data-file-index]");
      if (tab) switchSourceTab(Number(tab.dataset.fileIndex));
    });
    elements.saveButton.addEventListener("click", saveSource);
    elements.saveAsButton.addEventListener("click", saveSourceAs);
    elements.compileButton.addEventListener("click", compilePipeline);
	$('#openReportButton').addEventListener('click', () => {$('#reportError').textContent='';$('#reportDialog').showModal();});
	$('#cancelReport').addEventListener('click', () => $('#reportDialog').close());
	$('#reportFile').addEventListener('change', async event => {
	  const file=event.target.files?.[0];if(!file)return;
	  if(file.size>1048576){$('#reportError').textContent='Report must be at most 1 MiB.';return;}
	  try {$('#reportJSON').value=await file.text();}catch(error){$('#reportError').textContent=error.message;}
	});
	$('#inspectReport').addEventListener('click',inspectReport);
	$('#closeReport').addEventListener('click',closeReport);
	$('#recompileReport').addEventListener('click',async () => {
	  if(!reportWorkspace?.sources.length || state.busy)return;
	  reportWorkspace.readonly=false;invalidateSession();state.stale=true;
	  $('#inspectionStatus').textContent='Imported sources workspace. Your original documents remain preserved until you return.';
	  renderAll();await compilePipeline();
	});
	elements.formatButton.addEventListener("click", formatSource);
	elements.frontendMode?.addEventListener("change", () => {
	  if(inspectingReport())return;
	  state.frontend = elements.frontendMode.value === "generated" ? "generated" : "handwritten";
	  course.options.frontend = state.frontend === "generated" ? "generated" : undefined;
	  invalidateSession();
	  state.stale = true;
	  renderAll();
	  setGlobalStatus(`Front-end: ${state.frontend === "generated" ? "generated (spec-driven lexer + LALR parser)" : "hand-written"} — pipeline reset`, "idle");
	});
	elements.optimizationMode?.addEventListener("change", () => {
	  if(inspectingReport())return;
	  state.optimization = elements.optimizationMode.value === "basic" ? "basic" : "none";
	  invalidateSession();
	  state.stale = true;
	  renderAll();
	  setGlobalStatus(`Optimization: ${state.optimization} — pipeline reset`, "idle");
	});
    elements.nextPhaseButton.addEventListener("click", nextPipelinePhase);
    elements.runButton.addEventListener("click", runSession);
    elements.stepButton.addEventListener("click", stepSession);
    elements.stepIntoButton.addEventListener("click", () => sourceCommand("step-into"));
    elements.stepOverButton.addEventListener("click", () => sourceCommand("step-over"));
    elements.stepOutButton.addEventListener("click", () => sourceCommand("step-out"));
    elements.runToCursorButton.addEventListener("click", () => sourceCommand("run-to-cursor"));
	elements.stepBackButton.addEventListener("click", () => reverseCommand("step-back"));
	elements.reverseContinueButton.addEventListener("click", () => reverseCommand("reverse-continue"));
	elements.historySlider.addEventListener("input", () => seekHistory(elements.historySlider.value));
	elements.historyStep.addEventListener("change", () => seekHistory(elements.historyStep.value));
	elements.historyStep.addEventListener("keydown", (event) => { if (event.key === "Enter") seekHistory(elements.historyStep.value); });
    elements.resetButton.addEventListener("click", resetSession);
    elements.liveInputForm?.addEventListener("submit", sendLiveInput);
    window.CoolerAbout.createAboutController({
      dialog: elements.aboutDialog,
      trigger: elements.aboutButton,
      closeButton: elements.aboutCloseButton,
    });
    elements.exportButton.addEventListener("click", () => {
      void exportSelectedArtifact().catch((error) => {
        toast(`Could not export artifact: ${error.message}`, "error");
      });
    });
    elements.themeButton.addEventListener("click", toggleTheme);
    elements.clearConsoleButton.addEventListener("click", () => {
      state.consoleCleared = true;
      renderConsole();
    });
	elements.saveDebuggerButton?.addEventListener("click", saveDebuggerWorkspace);
	elements.watchForm?.addEventListener("submit", (event) => {
	  event.preventDefault();
	  const expression = String(elements.watchExpression?.value || "").trim();
	  if (!expression) return;
	  let sequence = state.watches.length + 1;
	  while (state.watches.some((watch) => watch.id === `watch-${sequence}`)) sequence++;
	  state.watches.push({ id: `watch-${sequence}`, expression, enabled: true });
	  elements.watchExpression.value = "";
	  renderDebuggerTools();
	});
	elements.breakpointEditor?.addEventListener("click", (event) => {
	  const row = event.target.closest("[data-breakpoint-key]");
	  if (!row || !event.target.closest("[data-remove-breakpoint]")) return;
	  const key = decodeURIComponent(row.dataset.breakpointKey || "");
	  state.breakpoints.delete(key);
	  state.breakpointConfigs.delete(key);
	  renderGutter();
	  renderDebuggerTools();
	});
	const updateBreakpointConfig = (event) => {
	  const row = event.target.closest("[data-breakpoint-key]");
	  const field = event.target.dataset.breakpointField;
	  if (!row || !field) return;
	  const key = decodeURIComponent(row.dataset.breakpointKey || "");
	  const config = ensureBreakpointConfig(key);
	  config[field] = event.target.type === "checkbox" ? event.target.checked : field === "hitCount" ? Math.max(0, Number(event.target.value || 0)) : event.target.value;
	  state.breakpointStates.delete(key);
	  renderGutter();
	};
	elements.breakpointEditor?.addEventListener("input", updateBreakpointConfig);
	elements.breakpointEditor?.addEventListener("change", updateBreakpointConfig);
	elements.watchList?.addEventListener("click", (event) => {
	  const objectButton = event.target.closest("[data-heap-object]");
	  if (objectButton) {
		openHeapObject(decodeURIComponent(objectButton.dataset.heapObject || ""));
		return;
	  }
	  const row = event.target.closest("[data-watch-id]");
	  if (!row || !event.target.closest("[data-remove-watch]")) return;
	  const id = decodeURIComponent(row.dataset.watchId || "");
	  state.watches = state.watches.filter((watch) => watch.id !== id);
	  renderDebuggerTools();
	});
	const updateWatch = (event) => {
	  const row = event.target.closest("[data-watch-id]");
	  const field = event.target.dataset.watchField;
	  if (!row || !field) return;
	  const id = decodeURIComponent(row.dataset.watchId || "");
	  const watch = state.watches.find((item) => item.id === id);
	  if (!watch) return;
	  watch[field] = event.target.type === "checkbox" ? event.target.checked : event.target.value;
	  state.watchResults = state.watchResults.filter((result) => result.id !== id);
	};
	elements.watchList?.addEventListener("input", updateWatch);
	elements.watchList?.addEventListener("change", updateWatch);

    elements.editor.addEventListener("beforeinput",event=>{
      if(event.inputType==="historyUndo"||event.inputType==="historyRedo"){event.preventDefault();undoEditor(event.inputType==="historyRedo");return;}
      beforeInputSelection=elements.editor.coolerSelection?.before || {start:elements.editor.selectionStart,end:elements.editor.selectionEnd};
    });
    elements.editor.addEventListener("input", sourceChanged);
    elements.editor.addEventListener("scroll", syncEditorScroll);
    elements.editor.addEventListener("click", () => { updateEditorMetadata(); updateSourceProvenance(); });
    elements.editor.addEventListener("keyup", () => { updateEditorMetadata(); updateSourceProvenance(); });
    elements.editor.addEventListener("keydown", (event) => {
      if(inspectingReport() || composing(event))return;
      if((event.ctrlKey||event.metaKey) && ["z","y"].includes(event.key.toLowerCase())){event.preventDefault();undoEditor(event.shiftKey||event.key.toLowerCase()==="y");return;}
      if((event.ctrlKey||event.metaKey) && ["[","]"].includes(event.key)){event.preventDefault();applyIndent(elements.editor,event.key==="[");updateEditorMetadata();}
    });
    $('#indentSource').addEventListener('click',()=>{if(!inspectingReport()){applyIndent(elements.editor);updateEditorMetadata();}});
    $('#outdentSource').addEventListener('click',()=>{if(!inspectingReport()){applyIndent(elements.editor,true);updateEditorMetadata();}});
    $('#previousDiagnostic').addEventListener('click',()=>navigateDiagnostic(-1));
    $('#nextDiagnostic').addEventListener('click',()=>navigateDiagnostic(1));

    const toggleBreakpoint = (row) => {
      if (!row) return;
      const line = Number(row.dataset.line);
      const tab = state.sourceTabs[state.activeTabIndex];
      if (!tab) return;
      const key = `${tab.id}:${line}`;
	  if (state.breakpoints.has(key)) {
		state.breakpoints.delete(key);
		state.breakpointConfigs.delete(key);
	  } else {
		state.breakpoints.add(key);
		ensureBreakpointConfig(key);
	  }
      renderGutter();
	  renderDebuggerTools();
      requestAnimationFrame(() => elements.gutter.querySelector(`[data-line="${line}"]`)?.focus({ preventScroll: true }));
    };
    elements.gutter.addEventListener("click", (event) => {
      const row = event.target.closest(".gutter-line");
      toggleBreakpoint(row);
    });
    elements.tabs.addEventListener("click", (event) => {
      const tab = event.target.closest(".tab");
      if (tab) setActiveTab(tab.dataset.tab);
    });
	elements.tabs.addEventListener("keydown", (event) => {
	  if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
	  const tabs = $$(".tab", elements.tabs);
	  const current = tabs.indexOf(event.target.closest(".tab"));
	  if (current < 0) return;
	  event.preventDefault();
	  const next = event.key === "Home" ? 0 : event.key === "End" ? tabs.length - 1 : (current + (event.key === "ArrowRight" ? 1 : -1) + tabs.length) % tabs.length;
	  setActiveTab(tabs[next].dataset.tab);
	  tabs[next].focus();
	});
    elements.pipeline.addEventListener("click", (event) => {
      const stage = event.target.closest(".stage-card")?.dataset.stage;
      if (stage && stageTabs[stage]) setActiveTab(stageTabs[stage]);
    });

    document.addEventListener("keydown", (event) => {
      if(composing(event))return;
      if(event.key==="F8"){event.preventDefault();navigateDiagnostic(event.shiftKey?-1:1);return;}
	  if(inspectingReport())return;
      const command = event.metaKey || event.ctrlKey;
      if (command && event.key === "Enter") {
        event.preventDefault();
        compilePipeline();
      } else if (event.shiftKey && event.key === "Enter" && event.target !== elements.editor) {
        event.preventDefault();
        stepSession();
      } else if (event.key === "F11" && event.shiftKey) {
        event.preventDefault();
        sourceCommand("step-out");
      } else if (event.key === "F11") {
        event.preventDefault();
        sourceCommand("step-into");
      } else if (event.key === "F10" && command) {
        event.preventDefault();
        sourceCommand("run-to-cursor");
      } else if (event.key === "F10") {
        event.preventDefault();
        sourceCommand("step-over");
      } else if (command && event.key.toLowerCase() === "s") {
        event.preventDefault();
        if (event.shiftKey) saveSourceAs();
        else saveSource();
      } else if (command && event.key.toLowerCase() === "o") {
        event.preventDefault();
        elements.fileInput.click();
      }
    });

    window.addEventListener("beforeunload", () => {
      if (state.pipeline.id) {
        void fetch("/api/pipeline/dispose", {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-Cooler-Capability": window.CoolerCapability || "",
          },
          body: JSON.stringify({ id: state.pipeline.id }),
          keepalive: true,
        });
      }
    });
  }

  async function init() {
    restoreTheme();
    bindEvents();
    populateRecentFiles();
    state.sourceTabs = [makeSourceTab("hello.cl", fallbackSource)];
    state.activeTabIndex = 0;
    setSource(fallbackSource, "hello.cl");
    renderAll();
    await Promise.all([loadExamples(), loadHealth()]);
	await readDrafts();
  }

  await init();
}
import { mapFormatSelection } from "./format-selection.mjs";
