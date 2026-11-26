/* SPDX-License-Identifier: AGPL-3.0-or-later */

export function createAppState() {
  return {
    examples: [],
    result: null,
    runtime: null,
	heap: null,
	heapLoading: false,
	heapQuery: { mode: "graph" },
	timeline: null,
    pipeline: { id: "", snapshot: null },
    deferredDisposals: new Set(),
    activeTab: "diagnostics",
    breakpoints: new Set(),
	breakpointConfigs: new Map(),
	breakpointStates: new Map(),
	watches: [],
	watchResults: [],
	debugLog: [],
	restoredDebuggerSources: new Set(),
	debuggerWatchesRestored: false,
    documentName: "untitled.cl",
    sourceTabs: [{ name: "untitled.cl", source: "", savedSource: "" }],
    activeTabIndex: 0,
    busy: false,
    stale: false,
    consoleCleared: false,
    savedSource: "",
    sessionEpoch: 0,
    fileReadEpoch: 0,
    autoRunning: false,
    pauseRequested: false,
    tokenFilter: "",
	exploreFilter: "",
	exploreClass: "",
	selectedSpan: null,
	optimization: "none",
	frontend: "handwritten",
    inputHistory: [],
    debugLocation: null,
    stopReason: "ready",
  };
}

export function beginAutoRun(state) {
  state.autoRunning = true;
  state.pauseRequested = false;
}

export function requestRunPause(state) {
  if (!state.autoRunning) return false;
  state.pauseRequested = true;
  return true;
}

export function finishAutoRun(state) {
  state.autoRunning = false;
  state.pauseRequested = false;
}

export function shouldAcceptTimeline(current, incoming) {
	if (!incoming) return true;
	if (!current) return Number(incoming.revision || 0) > 0;
	return Number(incoming.revision || 0) >= Number(current.revision || 0);
}

export function invalidateActiveSession(state) {
  const id = state.pipeline.id;
  state.sessionEpoch += 1;
  if (state.autoRunning) {
    state.pauseRequested = true;
    if (id) state.deferredDisposals.add(id);
  }
  state.pipeline = { id: "", snapshot: null };
  state.result = null;
  state.runtime = null;
	state.heap = null;
	state.heapLoading = false;
	state.heapQuery = { mode: "graph" };
	state.timeline = null;
	state.exploreClass = "";
	state.selectedSpan = null;
  state.inputHistory = [];
  state.debugLocation = null;
  state.stopReason = "ready";
	state.breakpointStates = new Map();
	state.watchResults = [];
	state.debugLog = [];
  return {
    id,
    disposeImmediately: Boolean(id && !state.deferredDisposals.has(id)),
  };
}
