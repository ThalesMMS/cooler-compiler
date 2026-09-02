/* SPDX-License-Identifier: AGPL-3.0-or-later */

(function (root, factory) {
  "use strict";

  const api = factory();
  if (typeof module === "object" && module.exports) module.exports = api;
  else root.CoolerClient = api;
})(typeof globalThis !== "undefined" ? globalThis : this, () => {
  "use strict";

  function normalizeFileName(name) {
    const trimmed = String(name || "program.cl").trim() || "program.cl";
    return trimmed.toLowerCase().endsWith(".cl") ? trimmed : `${trimmed}.cl`;
  }

  function stripExtension(name) {
    return String(name || "program.cl").replace(/\.[^.]+$/, "") || "program";
  }

  function safeArchiveName(name) {
    return String(name || "program")
      .replace(/[^a-zA-Z0-9._-]+/g, "-")
      .replace(/^-+|-+$/g, "") || "program";
  }

  function mergeRecentFiles(files, name, source, savedAt = new Date().toISOString(), limit = 8) {
    const normalizedName = normalizeFileName(name);
    const next = [{ name: normalizedName, source: String(source ?? ""), savedAt }];
    for (const item of Array.isArray(files) ? files : []) {
      if (!item?.name || typeof item.source !== "string") continue;
      if (item.name.toLowerCase() === normalizedName.toLowerCase()) continue;
      next.push(item);
    }
    return next.slice(0, Math.max(1, limit));
  }

  function applyQuickFix(source, fix) {
    const text = String(source ?? "");
    const start = clamp(Number(fix?.span?.startOffset || 0), 0, text.length);
    const end = clamp(Number(fix?.span?.endOffset ?? start), start, text.length);
    const replacement = String(fix?.replacement ?? "");
    return {
      source: text.slice(0, start) + replacement + text.slice(end),
      selectionStart: start,
      selectionEnd: start + replacement.length,
    };
  }

  function renderASTNode(node, path = "0") {
    const children = node?.children || [];
    const label = `
      <span class="tree-kind">${escapeASTHTML(node?.kind || "Node")}</span>
      ${node?.text ? `<span class="tree-text">${escapeASTHTML(node.text)}</span>` : ""}
      ${node?.type ? `<span class="tree-type">: ${escapeASTHTML(node.type)}</span>` : ""}
      <span class="tree-location">${node?.line || node?.span?.startLine || 1}:${node?.column || node?.span?.startColumn || 1}</span>`;
    if (children.length === 0) {
      return `<div class="tree-node"><div class="tree-row tree-leaf clickable-span" ${astSpanData(node?.span)}><span class="tree-toggle-spacer" aria-hidden="true"></span>${label}</div></div>`;
    }
    const childrenID = `ast-branch-${path}`;
    const nodeLabel = escapeASTHTML(node?.kind || "Node");
    return `<div class="tree-node"><div class="tree-row"><button class="tree-toggle" type="button" aria-expanded="true" aria-controls="${childrenID}" aria-label="Collapse ${nodeLabel}" data-node-label="${nodeLabel}"><span aria-hidden="true">›</span></button><div class="tree-label clickable-span" ${astSpanData(node?.span)}>${label}</div></div><div class="tree-children" id="${childrenID}">${children.map((child, index) => renderASTNode(child, `${path}-${index}`)).join("")}</div></div>`;
  }

  function toggleASTBranch(button) {
    const childrenID = button?.getAttribute("aria-controls");
    const children = childrenID ? button.ownerDocument?.getElementById(childrenID) : null;
    if (!children) return null;
    const expanded = button.getAttribute("aria-expanded") !== "true";
    button.setAttribute("aria-expanded", String(expanded));
    button.setAttribute("aria-label", `${expanded ? "Collapse" : "Expand"} ${button.dataset.nodeLabel || "AST node"}`);
    children.hidden = !expanded;
    return expanded;
  }

  function astSpanData(span) {
    if (!span) return "";
    return `data-source-id="${escapeASTHTML(span.sourceId || "")}" data-start="${Number(span.startOffset || 0)}" data-end="${Number(span.endOffset || span.startOffset || 0)}" data-line="${Number(span.startLine || 1)}" data-column="${Number(span.startColumn || 1)}"`;
  }

  function escapeASTHTML(value) {
    return String(value ?? "")
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;")
      .replace(/'/g, "&#039;");
  }

  function traceEnd(runtime) {
    return Number(runtime?.traceOffset || 0) + (runtime?.trace?.length || 0);
  }

  function mergeRuntime(previous, incoming) {
    const prior = previous || {};
    const delta = incoming || {};
    const priorTrace = Array.isArray(prior.trace) ? prior.trace : [];
    const deltaTrace = Array.isArray(delta.trace) ? delta.trace : [];
    const priorOffset = Number(prior.traceOffset || 0);
    const priorEnd = priorOffset + priorTrace.length;
    const deltaOffset = Number(delta.traceOffset || 0);
    let trace;
    let traceOffset;

    if (deltaOffset < priorOffset || deltaOffset > priorEnd) {
      trace = deltaTrace.slice();
      traceOffset = deltaOffset;
    } else {
      trace = priorTrace.slice(0, deltaOffset - priorOffset).concat(deltaTrace);
      traceOffset = priorOffset;
    }

    return { ...delta, trace, traceOffset };
  }

  function mergeLightSnapshot(result, snapshot) {
    if (!snapshot) return result;
    if (snapshot.result?.source) {
      return { ...snapshot.result, runtime: snapshot.runtime || snapshot.result.runtime || {} };
    }
    if (!result || !snapshot.runtime) return result;
    const runtime = mergeRuntime(result.runtime, snapshot.runtime);
    const staticDiagnostics = (result.diagnostics || []).filter((diagnostic) => diagnostic.stage !== "runtime");
    const stages = (result.stages || []).map((stage) => {
      if (stage.id !== "runtime") return stage;
      let status = Number(runtime.executedSteps || 0) > 0 ? "ready" : stage.status;
      if ((runtime.diagnostics || []).some((diagnostic) => diagnostic.severity === "error")) status = "error";
      else if (runtime.halted) status = "ok";
      else if (runtime.waitingInput) status = "waiting-input";
      else if (isBreakpointPause(runtime)) status = "paused";
      const count = Number(runtime.executedSteps || 0);
      return { ...stage, status, summary: `${count} executed instruction${count === 1 ? "" : "s"}` };
    });
    return {
      ...result,
      stages,
      runtime,
      diagnostics: staticDiagnostics.concat(runtime.diagnostics || []),
    };
  }

  function shouldAcceptResponse(expectedID, currentID, expectedEpoch, currentEpoch) {
    return Boolean(expectedID) && expectedID === currentID && expectedEpoch === currentEpoch;
  }

  function shouldDisableRunButton({ busy, autoRunning, pauseRequested, runtimeDone, waitingInput } = {}) {
    return Boolean(pauseRequested || (busy && !autoRunning) || runtimeDone || waitingInput);
  }

  function runButtonPresentation(mode) {
    if (mode === "pause") {
      return {
        label: "Pause",
        title: "Pause after the current VM batch",
        icon: "pause",
      };
    }
    if (mode === "pausing") {
      return {
        label: "Pausing…",
        title: "Waiting for the current VM batch",
        icon: "pause",
      };
    }
    return {
      label: "Run",
      title: "Run until halt or breakpoint",
      icon: "run",
    };
  }

  function isBreakpointPause(runtime) {
    const trace = runtime?.trace || [];
    return Boolean(runtime?.paused && trace.at(-1)?.op === "BREAKPOINT");
  }

  function shouldStopAutoRun(runtime, executedBefore) {
    if (!runtime) return true;
    if (runtime.halted || runtime.waitingInput || isBreakpointPause(runtime)) return true;
    if ((runtime.diagnostics || []).some((diagnostic) => diagnostic.severity === "error")) return true;
    return Number(runtime.executedSteps || 0) <= Number(executedBefore || 0);
  }

  async function runVMChunks({
    sessionID,
    epoch,
    chunkSteps = 25,
    chunkDelayMs = 25,
    getSessionID,
    getEpoch,
    getRuntime,
    getBreakpoints,
    isPauseRequested,
    requestChunk,
    applySnapshot,
    wait = (milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds)),
  }) {
    const compareBreakpoints = (left, right) => {
      if (typeof left === "number" && typeof right === "number") return left - right;
      const leftSource = String(left?.sourceId || "");
      const rightSource = String(right?.sourceId || "");
      return leftSource.localeCompare(rightSource) || Number(left?.line || left || 0) - Number(right?.line || right || 0);
    };
    while (!isPauseRequested()) {
      const runtimeBefore = getRuntime();
      const executedBefore = Number(runtimeBefore?.executedSteps || 0);
      const payload = await requestChunk({
        id: sessionID,
        maxSteps: chunkSteps,
        breakpoints: Array.from(getBreakpoints()).sort(compareBreakpoints),
        light: true,
        traceFrom: traceEnd(runtimeBefore),
      });
      if (!shouldAcceptResponse(sessionID, getSessionID(), epoch, getEpoch())) return { reason: "stale" };
      applySnapshot(payload.snapshot);
      if (shouldStopAutoRun(getRuntime(), executedBefore)) return { reason: "stopped" };
      await wait(chunkDelayMs);
      if (!shouldAcceptResponse(sessionID, getSessionID(), epoch, getEpoch())) return { reason: "stale" };
    }
    return { reason: "paused" };
  }

  function buildCompileReport(result, documentName, exportedAt = null) {
    const report = {
      project: "cooler-compiler",
      schemaVersion: result?.schemaVersion || "1.4.0",
      compilerVersion: result?.compilerVersion || "dev",
      ...(result?.compilerCommit ? { compilerCommit: result.compilerCommit } : {}),
      target: result?.target || {},
      options: result?.options || {},
      sourceUnits: result?.sourceUnits || [],
      sourceFile: normalizeFileName(documentName),
      source: result?.source ?? "",
      stages: result?.stages ?? [],
      diagnostics: result?.diagnostics ?? [],
      tokens: result?.tokens ?? [],
      ast: result?.ast ?? null,
      symbols: result?.symbols ?? [],
      ir: result?.ir ?? [],
      assembly: result?.assembly ?? [],
      assemblySpans: result?.assemblySpans ?? [],
      metrics: result?.metrics ?? {},
      controlFlow: result?.controlFlow ?? { blocks: [], edges: [] },
      runtime: result?.runtime ?? {},
      ...(result?.visualizations ? { visualizations: result.visualizations } : {}),
      optimization: result?.optimization ?? {
        mode: result?.options?.optimization || "none",
        enabled: false,
        executionIR: "unoptimized",
        passOrder: [],
        baselineVerified: false,
        optimizedVerified: false,
        beforeIR: [],
        afterIR: [],
        beforeCFG: { blocks: [], edges: [] },
        afterCFG: { blocks: [], edges: [] },
        passes: [],
      },
    };
    if (exportedAt) report.exportedAt = exportedAt;
    return report;
  }

  function formatDiagnosticsText(diagnostics) {
    if (!diagnostics?.length) return "No diagnostics.";
    return diagnostics.map((diagnostic) => {
      const code = diagnostic.code ? `${diagnostic.code} ` : "";
      const where = `${diagnostic.line || "-"}:${diagnostic.column || "-"}`;
      const suggestions = (diagnostic.suggestions || [])
        .map((suggestion) => `  suggestion: ${suggestion.title}${suggestion.detail ? ` - ${suggestion.detail}` : ""}`)
        .join("\n");
      const header = `${code}${String(diagnostic.severity || "info").toUpperCase()} ${diagnostic.stage || "compiler"} ${where}`;
      return `${header}\n${diagnostic.message || ""}${suggestions ? `\n${suggestions}` : ""}`;
    }).join("\n\n");
  }

  function formatIRLine(instruction) {
    const args = (instruction.args || []).join(", ");
    const comment = instruction.comment ? ` ; ${instruction.comment}` : "";
    return `${String(instruction.index ?? 0).padStart(4, "0")} ${instruction.op || ""}${args ? ` ${args}` : ""}${comment}`;
  }

  async function buildArchiveFiles(result, documentName, sourceFallback = "", exportedAt = null) {
    const baseName = stripExtension(documentName);
    const folder = safeArchiveName(baseName);
    const runtime = result?.runtime || {};
    const projectSources = Array.isArray(sourceFallback) ? sourceFallback : [];
    const legacySource = Array.isArray(sourceFallback) ? "" : sourceFallback;
    const report = buildCompileReport({ ...result, source: result?.source || legacySource }, documentName, exportedAt);
    const sourceFiles = projectSources.length > 0
      ? projectSources.map((unit, index) => ({
          name: `${folder}/sources/${String(index + 1).padStart(3, "0")}-${safeArchiveName(unit.name || unit.id || "source.cl")}`,
          content: String(unit.content ?? ""),
        }))
      : [{ name: `${folder}/source.cl`, content: result?.source || legacySource }];
    const files = [
      ...sourceFiles,
      { name: `${folder}/diagnostics.json`, content: jsonText(result?.diagnostics || []) },
      { name: `${folder}/diagnostics.txt`, content: formatDiagnosticsText(result?.diagnostics || []) },
      { name: `${folder}/tokens.json`, content: jsonText(result?.tokens || []) },
      { name: `${folder}/ast.json`, content: jsonText(result?.ast ?? null) },
      { name: `${folder}/symbols.json`, content: jsonText(result?.symbols || []) },
      { name: `${folder}/ir.txt`, content: (result?.ir || []).map(formatIRLine).join("\n") },
      { name: `${folder}/assembly.txt`, content: (result?.assembly || []).join("\n") },
      { name: `${folder}/vm-trace.json`, content: jsonText(runtime.trace || []) },
      { name: `${folder}/output.txt`, content: (runtime.output || []).join("") },
      { name: `${folder}/compile-report.json`, content: jsonText(report) },
    ];
    validateArchiveFiles(files);
    const encoder = new TextEncoder();
    const artifacts = [];
    for (const file of files) {
      const data = typeof file.content === "string" ? encoder.encode(file.content) : new Uint8Array(file.content || []);
      artifacts.push({ path: file.name, byteSize: data.length, sha256: await sha256Hex(data) });
    }
    const manifest = {
      schemaVersion: "1.0.0",
      manifestType: "cooler-report-archive",
      compilerVersion: report.compilerVersion,
      ...(report.compilerCommit ? { compilerCommit: report.compilerCommit } : {}),
      documentName: report.sourceFile,
      sourceUnits: (report.sourceUnits || []).map((unit, index) => ({
        ...unit,
        ...(sourceFiles[index] ? { path: sourceFiles[index].name } : {}),
      })),
      artifacts,
      creation: {
        timestampPolicy: "fixed-dos-epoch",
        generatedTextNewlines: "LF",
        sourceAndProgramOutput: "preserved",
      },
    };
    files.push({ name: `${folder}/manifest.json`, content: jsonText(manifest) });
    validateArchiveFiles(files);
    return files;
  }

  function jsonText(value) {
    return `${JSON.stringify(value, null, 2)}\n`;
  }

  async function sha256Hex(bytes) {
    if (!globalThis.crypto?.subtle) throw new Error("SHA-256 is unavailable in this runtime");
    const digest = new Uint8Array(await globalThis.crypto.subtle.digest("SHA-256", bytes));
    return Array.from(digest, (byte) => byte.toString(16).padStart(2, "0")).join("");
  }

  function validateArchiveFiles(files) {
    const names = new Set();
    for (const file of files || []) {
      const name = String(file?.name || "");
      if (!name || name.includes("\\") || name.includes("\0") || name.startsWith("/") || /^[a-zA-Z]:/.test(name)) {
        throw new Error(`unsafe ZIP entry path: ${name || "<empty>"}`);
      }
      const parts = name.split("/");
      if (parts.some((part) => !part || part === "." || part === "..")) {
        throw new Error(`unsafe ZIP entry path: ${name}`);
      }
      if (names.has(name)) throw new Error(`duplicate ZIP entry path: ${name}`);
      names.add(name);
    }
  }

  function buildZipBlob(files, date = new Date(Date.UTC(1980, 0, 1, 0, 0, 0))) {
    validateArchiveFiles(files);
    const encoder = new TextEncoder();
    const localParts = [];
    const centralParts = [];
    const timestamp = zipTimestamp(date);
    let offset = 0;

    for (const file of files) {
      const nameBytes = encoder.encode(String(file.name || "file").replace(/^\/+/, ""));
      const data = typeof file.content === "string" ? encoder.encode(file.content) : new Uint8Array(file.content || []);
      const crc = crc32(data);
      const localHeader = zipLocalHeader(nameBytes, data.length, crc, timestamp);
      const centralHeader = zipCentralHeader(nameBytes, data.length, crc, timestamp, offset);
      localParts.push(localHeader, data);
      centralParts.push(centralHeader);
      offset += localHeader.length + data.length;
    }

    const centralSize = centralParts.reduce((sum, part) => sum + part.length, 0);
    return new Blob([...localParts, ...centralParts, zipEndRecord(files.length, centralSize, offset)], { type: "application/zip" });
  }

  function zipLocalHeader(nameBytes, size, crc, timestamp) {
    const header = new Uint8Array(30 + nameBytes.length);
    const view = new DataView(header.buffer);
    view.setUint32(0, 0x04034b50, true);
    view.setUint16(4, 20, true);
    view.setUint16(6, 0x0800, true);
    view.setUint16(8, 0, true);
    view.setUint16(10, timestamp.time, true);
    view.setUint16(12, timestamp.date, true);
    view.setUint32(14, crc, true);
    view.setUint32(18, size, true);
    view.setUint32(22, size, true);
    view.setUint16(26, nameBytes.length, true);
    header.set(nameBytes, 30);
    return header;
  }

  function zipCentralHeader(nameBytes, size, crc, timestamp, localOffset) {
    const header = new Uint8Array(46 + nameBytes.length);
    const view = new DataView(header.buffer);
    view.setUint32(0, 0x02014b50, true);
    view.setUint16(4, 20, true);
    view.setUint16(6, 20, true);
    view.setUint16(8, 0x0800, true);
    view.setUint16(10, 0, true);
    view.setUint16(12, timestamp.time, true);
    view.setUint16(14, timestamp.date, true);
    view.setUint32(16, crc, true);
    view.setUint32(20, size, true);
    view.setUint32(24, size, true);
    view.setUint16(28, nameBytes.length, true);
    view.setUint32(42, localOffset, true);
    header.set(nameBytes, 46);
    return header;
  }

  function zipEndRecord(entryCount, centralSize, centralOffset) {
    const header = new Uint8Array(22);
    const view = new DataView(header.buffer);
    view.setUint32(0, 0x06054b50, true);
    view.setUint16(8, entryCount, true);
    view.setUint16(10, entryCount, true);
    view.setUint32(12, centralSize, true);
    view.setUint32(16, centralOffset, true);
    return header;
  }

  function zipTimestamp(date) {
    const year = Math.max(1980, Math.min(2107, date.getUTCFullYear()));
    return {
      time: (date.getUTCHours() << 11) | (date.getUTCMinutes() << 5) | Math.floor(date.getUTCSeconds() / 2),
      date: ((year - 1980) << 9) | ((date.getUTCMonth() + 1) << 5) | date.getUTCDate(),
    };
  }

  let crcTable;
  function crc32(bytes) {
    if (!crcTable) {
      crcTable = Array.from({ length: 256 }, (_, n) => {
        let value = n;
        for (let bit = 0; bit < 8; bit++) value = value & 1 ? 0xedb88320 ^ (value >>> 1) : value >>> 1;
        return value >>> 0;
      });
    }
    let crc = 0xffffffff;
    for (const byte of bytes) crc = crcTable[(crc ^ byte) & 0xff] ^ (crc >>> 8);
    return (crc ^ 0xffffffff) >>> 0;
  }

  function clamp(value, min, max) {
    return Math.min(max, Math.max(min, value));
  }

  return {
    applyQuickFix,
    buildArchiveFiles,
    buildCompileReport,
    buildZipBlob,
    formatDiagnosticsText,
    formatIRLine,
    isBreakpointPause,
    mergeLightSnapshot,
    mergeRecentFiles,
    normalizeFileName,
    renderASTNode,
    runButtonPresentation,
    safeArchiveName,
    validateArchiveFiles,
    runVMChunks,
    shouldAcceptResponse,
    shouldDisableRunButton,
    shouldStopAutoRun,
    stripExtension,
    toggleASTBranch,
    traceEnd,
  };
});
