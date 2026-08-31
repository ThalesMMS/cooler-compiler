/* SPDX-License-Identifier: AGPL-3.0-or-later */


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
    elements.sessionStatus.textContent = text;
    elements.sessionStatus.className = `status-pill ${kind}`;
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
    $$(".tab", elements.tabs).forEach((tab) => tab.classList.toggle("active", tab.dataset.tab === state.activeTab));
    const diagnostics = state.result?.diagnostics || [];
    elements.diagnosticCount.textContent = String(diagnostics.length);
    elements.diagnosticCount.classList.toggle("has-errors", hasErrors(diagnostics));

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
      cfg: renderCFG,
      vm: renderVM,
    };
    elements.content.innerHTML = (renderers[state.activeTab] || renderDiagnostics)();
    wireArtifactInteractions();
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
    const diagnostics = state.result?.diagnostics || [];
    if (diagnostics.length === 0) {
      return `${metricsHTML()}<div class="success-card"><div><strong>No diagnostics</strong><br><span>The current source passed every completed stage.</span></div></div>`;
    }
    const files = state.concatFiles.length ? state.concatFiles : currentConcat().files;
    return `${metricsHTML()}<div class="diagnostic-list">${diagnostics.map((d) => {
      const symbol = d.severity === "error" ? "\u00d7" : d.severity === "warning" ? "!" : "i";
      const related = (d.related || []).map((item) => `${escapeHTML(item.label)} at ${item.line}:${item.column}`).join(" \u00b7 ");
      const fixes = (d.quickFixes || []).map((fix) => `
        <button class="text-button quick-fix" type="button" data-fix-start="${fix.span?.startOffset || 0}" data-fix-end="${fix.span?.endOffset || 0}" data-fix-value="${encodeURIComponent(fix.replacement || "")}">${escapeHTML(fix.title)}</button>
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
          <div class="diagnostic-meta"><span class="code-pill">${escapeHTML(d.code || d.stage)}</span><span>${escapeHTML(diagnosticLocationLabel(d, files))}</span></div>
        </article>`;
    }).join("")}</div>`;
  }

  function renderTokens() {
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
    const ast = state.result?.ast;
    if (!ast) return emptyInline("No AST was produced because parsing did not complete.");
    return `${metricsHTML()}<div class="tree">${client.renderASTNode(ast)}</div>`;
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
          <td><span class="op-pill">${escapeHTML(instruction.op)}</span></td>
          <td><code>${escapeHTML((instruction.args || []).join(", "))}</code></td>
          <td>${escapeHTML(instruction.comment || "")}</td>
          <td class="muted-cell">${instruction.line || 1}:${instruction.column || 1}</td>
        </tr>`).join("")}</tbody>
    </table></div>`;
  }

  function renderAssembly() {
    const assembly = state.result?.assembly || [];
    if (assembly.length === 0) return emptyInline("Pseudo-assembly is unavailable.");
    return `${metricsHTML()}<pre class="code-block">${escapeHTML(assembly.join("\n"))}</pre>`;
  }

  function renderCFG() {
    const graph = state.result?.controlFlow || {};
    const blocks = graph.blocks || [];
    const edges = graph.edges || [];
    if (blocks.length === 0) return emptyInline("No control-flow graph is available.");
    const byMethod = new Map();
    blocks.forEach((block) => {
      const list = byMethod.get(block.method) || [];
      list.push(block);
      byMethod.set(block.method, list);
    });
    return `<div class="cfg-layout">
      <div class="cfg-canvas">${Array.from(byMethod.entries()).map(([method, methodBlocks]) => `
        <section class="cfg-method">
          <div class="cfg-method-title">${escapeHTML(method || "program")}</div>
          <div class="cfg-blocks">${methodBlocks.map((block) => `
            <div class="cfg-block clickable-span" ${spanData(block.span)}><strong>${escapeHTML(shortBlockName(block.name))}</strong><span>IR ${escapeHTML(block.range)}</span></div>
          `).join("")}</div>
        </section>`).join("")}</div>
      <div class="cfg-edge-list">${edges.map((edge) => `
        <div class="cfg-edge"><span>${escapeHTML(shortBlockName(edge.from))}</span><span class="cfg-edge-kind">${escapeHTML(edge.kind)} \u2192</span><span>${escapeHTML(shortBlockName(edge.to))}</span></div>
      `).join("")}</div>
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
          <div class="table-wrap" style="border:0;border-radius:0;max-height:440px"><table class="data-table trace-table">
            <thead><tr><th>Step</th><th>Method</th><th>IP</th><th>Op</th><th>Note</th><th>Source</th></tr></thead>
            <tbody>${trace.map((item, index) => `
              <tr class="clickable clickable-span ${index === trace.length - 1 ? "trace-current" : ""}" ${spanData(item.span)}>
                <td>${item.step}</td><td>${escapeHTML(item.method || "")}</td><td>${item.ip}</td><td><span class="op-pill">${escapeHTML(item.op)}</span></td><td>${escapeHTML(item.note || "")}</td><td>${item.span?.startLine || 1}:${item.span?.startColumn || 1}</td>
              </tr>`).join("")}</tbody>
          </table></div>
        </section>
      </div>
      <div class="vm-column">
        <section class="section-card">
          <div class="section-title">Operand stack <span>${stack.length}</span></div>
          <div class="stack-list">${stack.length ? stack.slice().reverse().map((value, index) => `
            <div class="stack-item"><span class="stack-index">${stack.length - index - 1}</span><span class="value-type">${escapeHTML(value.type)}</span><span class="value-display">${escapeHTML(value.display)}</span></div>
          `).join("") : '<div class="diagnostic-hint">Empty</div>'}</div>
        </section>
        <section class="section-card">
          <div class="section-title">Call frames <span>${frames.length}</span></div>
          <div class="frame-list">${frames.slice().reverse().map((frame) => `
            <div class="frame-card">
              <div class="frame-head"><span>${escapeHTML(`${frame.owner || ""}.${frame.functionName || ""}`)}</span><span>IP ${frame.ip}</span></div>
              <div class="frame-locals">
                <div class="local-item"><span>self</span><span class="value-type">${escapeHTML(frame.self?.type || "Void")}</span><span class="value-display">${escapeHTML(frame.self?.display || "void")}</span></div>
                ${Object.entries(frame.locals || {}).map(([name, value]) => `<div class="local-item"><span>${escapeHTML(name)}</span><span class="value-type">${escapeHTML(value.type)}</span><span class="value-display">${escapeHTML(value.display)}</span></div>`).join("")}
              </div>
            </div>`).join("") || '<div class="diagnostic-hint">No active frames</div>'}</div>
        </section>
      </div>
    </div>`;
  }
