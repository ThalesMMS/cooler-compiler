/* SPDX-License-Identifier: AGPL-3.0-or-later */

  function renderConsole() {
    const runtime = state.runtime || state.result?.runtime;
    const steps = runtime?.executedSteps || 0;
    elements.executionSummary.textContent = `${steps} instruction${steps === 1 ? "" : "s"}`;
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
    const diagnosticLines = new Set((state.result?.diagnostics || []).map((d) => localLineFromJoined(d.line)).filter(Boolean));
    const activeLine = localLineFromJoined(lastTraceState()?.span?.startLine || 0);
    const fragments = [];
    for (let line = 1; line <= lines; line++) {
      const classes = ["gutter-line"];
      if (state.breakpoints.has(`${state.activeTabIndex}:${line}`)) classes.push("breakpoint");
      if (diagnosticLines.has(line)) classes.push("diagnostic");
      if (line === activeLine) classes.push("active");
      fragments.push(`<div class="${classes.join(" ")}" data-line="${line}"><span class="marker"></span><span class="number">${line}</span></div>`);
    }
    elements.gutter.innerHTML = fragments.join("");
    elements.gutter.scrollTop = elements.editor.scrollTop;
  }

  function wireArtifactInteractions() {
    $$(".tree-toggle", elements.content).forEach((button) => {
      button.addEventListener("click", () => client.toggleASTBranch(button));
    });
    $$(".clickable-span", elements.content).forEach((element) => {
      element.addEventListener("click", (event) => {
        if (event.target.closest(".quick-fix")) return;
        const span = readSpanData(element);
        if (span) selectSpan(span, true);
      });
    });
    $$(".quick-fix", elements.content).forEach((button) => {
      button.addEventListener("click", (event) => {
        event.stopPropagation();
        const joinedStart = Number(button.dataset.fixStart || 0);
        const files = state.concatFiles.length ? state.concatFiles : currentConcat().files;
        let fileIndex = 0;
        for (let index = 0; index < files.length; index++) {
          if (files[index].startOffset <= joinedStart) fileIndex = index;
        }
        if (fileIndex !== state.activeTabIndex) switchSourceTab(fileIndex);
        const origin = files[fileIndex]?.startOffset || 0;
        const applied = client.applyQuickFix(elements.editor.value, {
          span: {
            startOffset: Math.max(0, joinedStart - origin),
            endOffset: Math.max(0, Number(button.dataset.fixEnd || button.dataset.fixStart || 0) - origin),
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
    const files = state.concatFiles.length ? state.concatFiles : [];
    const origin = files[state.activeTabIndex]?.startOffset || 0;
    const limit = origin + source.length;
    const tokens = (state.result?.tokens || [])
      .filter((token) => token.type !== "EOF" && token.span)
      .filter((token) => {
        if (!files.length) return true;
        const start = Number(token.span.startOffset || 0);
        return start >= origin && start <= limit;
      })
      .sort((left, right) => left.span.startOffset - right.span.startOffset);
    let cursor = 0;
    const parts = [];
    for (const token of tokens) {
      const start = clamp(Number(token.span.startOffset || 0) - origin, cursor, source.length);
      const end = clamp(Number(token.span.endOffset || (start + origin)) - origin, start, source.length);
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
    return `data-start="${Number(span.startOffset || 0)}" data-end="${Number(span.endOffset || span.startOffset || 0)}" data-line="${Number(span.startLine || 1)}" data-column="${Number(span.startColumn || 1)}"`;
  }

  function readSpanData(element) {
    if (element.dataset.start === undefined) return null;
    return {
      startOffset: Number(element.dataset.start || 0),
      endOffset: Number(element.dataset.end || element.dataset.start || 0),
      startLine: Number(element.dataset.line || 1),
      startColumn: Number(element.dataset.column || 1),
    };
  }

  function selectSpan(span, focus = true) {
    if (!span) return;
    const files = state.concatFiles.length ? state.concatFiles : currentConcat().files;
    const loc = client.mapDiagnosticLocation(files, state.sourceTabs[0]?.name || "untitled.cl", Number(span.startLine || 1));
    if (loc.index >= 0 && loc.index !== state.activeTabIndex) switchSourceTab(loc.index);
    const origin = files[loc.index >= 0 ? loc.index : state.activeTabIndex]?.startOffset || 0;
    const sourceLength = elements.editor.value.length;
    const start = clamp(Number(span.startOffset || 0) - origin, 0, sourceLength);
    let end = clamp(Number(span.endOffset || span.startOffset || 0) - origin, start, sourceLength);
    if (end === start && start < sourceLength) end++;
    elements.editor.setSelectionRange(start, end);
    if (focus) elements.editor.focus({ preventScroll: true });
    scrollEditorToLine(loc.line || Number(span.startLine || 1));
    updateEditorMetadata();
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

  function sourceChanged() {
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
    elements.runButton.disabled = client.shouldDisableRunButton({
      busy: state.busy,
      autoRunning: state.autoRunning,
      pauseRequested: state.pauseRequested,
      runtimeDone,
    });
    elements.stepButton.disabled = state.busy || runtimeDone;
    elements.resetButton.disabled = state.busy || !state.pipeline.id || !snapshot?.complete || state.stale;
    elements.exportButton.disabled = !state.result;
  }

  function lastTraceState() {
    const trace = state.runtime?.trace || [];
    return trace.length ? trace[trace.length - 1] : null;
  }

  function setActiveTab(tab) {
    state.activeTab = tab;
    renderStageGuide();
    renderArtifact();
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
          state.sourceTabs.push({ name: item.name, source: item.source, savedSource: item.source });
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
      state.stale = false;
      state.sourceTabs = loaded.map((item) => ({
        name: item.name,
        source: item.source,
        savedSource: item.source,
      }));
      state.activeTabIndex = 0;
      state.concatFiles = [];
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
    flushActiveTab();
    const tab = state.sourceTabs[state.activeTabIndex];
    const name = client.normalizeFileName((tab && tab.name) || state.documentName || "program.cl");
    if (tab) {
      tab.name = name;
      tab.savedSource = tab.source;
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

  function exportSelectedArtifact() {
    if (!state.result) return;
    const stem = client.stripExtension(state.documentName || "program.cl");
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
      case "zip":
        downloadBlob(`${stem}.cooler-report.zip`, client.buildZipBlob(
          client.buildArchiveFiles(state.result, state.documentName, currentConcat().source),
        ));
        break;
      case "report":
      default:
        download(`${stem}.cooler-report.json`, JSON.stringify(
          client.buildCompileReport(state.result, state.documentName),
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

  function downloadBlob(filename, blob) {
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = filename;
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }

  function toggleTheme() {
    const current = document.documentElement.dataset.theme || "dark";
    const next = current === "dark" ? "light" : "dark";
    document.documentElement.dataset.theme = next;
    localStorage.setItem("cooler-theme", next);
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
    elements.nextPhaseButton.addEventListener("click", nextPipelinePhase);
    elements.runButton.addEventListener("click", runSession);
    elements.stepButton.addEventListener("click", stepSession);
    elements.resetButton.addEventListener("click", resetSession);
    window.CoolerAbout.createAboutController({
      dialog: elements.aboutDialog,
      trigger: elements.aboutButton,
      closeButton: elements.aboutCloseButton,
    });
    elements.exportButton.addEventListener("click", exportSelectedArtifact);
    elements.themeButton.addEventListener("click", toggleTheme);
    elements.clearConsoleButton.addEventListener("click", () => {
      state.consoleCleared = true;
      renderConsole();
    });

    elements.editor.addEventListener("input", sourceChanged);
    elements.editor.addEventListener("scroll", syncEditorScroll);
    elements.editor.addEventListener("click", updateEditorMetadata);
    elements.editor.addEventListener("keyup", updateEditorMetadata);
    elements.editor.addEventListener("keydown", (event) => {
      if (event.key === "Tab") {
        event.preventDefault();
        const start = elements.editor.selectionStart;
        const end = elements.editor.selectionEnd;
        elements.editor.setRangeText("  ", start, end, "end");
        sourceChanged();
      }
    });

    elements.gutter.addEventListener("click", (event) => {
      const row = event.target.closest(".gutter-line");
      if (!row) return;
      const line = Number(row.dataset.line);
      const key = `${state.activeTabIndex}:${line}`;
      if (state.breakpoints.has(key)) state.breakpoints.delete(key);
      else state.breakpoints.add(key);
      renderGutter();
    });

    elements.tabs.addEventListener("click", (event) => {
      const tab = event.target.closest(".tab");
      if (tab) setActiveTab(tab.dataset.tab);
    });
    elements.pipeline.addEventListener("click", (event) => {
      const stage = event.target.closest(".stage-card")?.dataset.stage;
      if (stage && stageTabs[stage]) setActiveTab(stageTabs[stage]);
    });

    document.addEventListener("keydown", (event) => {
      const command = event.metaKey || event.ctrlKey;
      if (command && event.key === "Enter") {
        event.preventDefault();
        compilePipeline();
      } else if (event.shiftKey && event.key === "Enter" && event.target !== elements.editor) {
        event.preventDefault();
        stepSession();
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
        navigator.sendBeacon("/api/pipeline/dispose", new Blob([JSON.stringify({ id: state.pipeline.id })], { type: "application/json" }));
      }
    });
  }

  async function init() {
    restoreTheme();
    bindEvents();
    populateRecentFiles();
    state.sourceTabs = [{ name: "hello.cl", source: fallbackSource, savedSource: fallbackSource }];
    state.activeTabIndex = 0;
    setSource(fallbackSource, "hello.cl");
    renderAll();
    await Promise.all([loadExamples(), loadHealth()]);
  }

  init();
})();
