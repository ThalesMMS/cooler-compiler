/* SPDX-License-Identifier: AGPL-3.0-or-later */

// Course-assignment views (PA1–PA5). Everything here is opt-in: the default
// Cooler panels stay unchanged until the user switches a panel to a
// reference view or opens the MIPS / Course tabs. All artifacts are computed
// in-process by /api/course; nothing shells out to a course toolchain.

// Panels that offer a "Reference format" view and the artifact each needs.
export const REFERENCE_VIEWS = {
  tokens: [
    { id: "cooler", label: "Cooler JSON" },
    { id: "reference", label: "Reference format", artifact: "tokens", file: "tokens.ref.txt", title: "Reference lexer token stream (PA2)" },
  ],
  ast: [
    { id: "cooler", label: "Cooler AST" },
    { id: "parser", label: "Reference (parser)", artifact: "ast", file: "ast.ref.txt", title: "Reference parser AST (PA3)" },
    { id: "semant", label: "Reference typed (semant)", artifact: "semant", file: "typed-ast.ref.txt", title: "Reference typed AST (PA4)" },
  ],
  diagnostics: [
    { id: "cooler", label: "Cooler diagnostics" },
    { id: "reference", label: "Reference format", artifact: "diagnostics", file: "diagnostics.ref.txt", title: "Reference diagnostics (PA2–PA4)", emptyText: "No errors: lexer, parser, and semant accept the program." },
  ],
};

// Elements inside the artifact panel whose clicks the controller handles.
export const COURSE_CLICK_SELECTOR = "[data-course-view],[data-course-export],[data-course-action],[data-course-gc],[data-course-pa],[data-course-frontend]";

// Course mode presents each compiler phase as its practical assignment.
export const COURSE_ASSIGNMENTS = [
  { id: "pa1", label: "PA1", title: "Stack machine", summary: "A COOL program: the stack-machine interpreter run on the default Go VM with the course stack.test script." },
  { id: "pa2", label: "PA2", title: "Lexical analysis", summary: "The course lexer's token stream, with ERROR tokens for lexical errors." },
  { id: "pa3", label: "PA3", title: "Parsing", summary: "The course parser's untyped AST, or its syntax-error report." },
  { id: "pa4", label: "PA4", title: "Semantic analysis", summary: "The course semant's typed AST and its error report." },
  { id: "pa5", label: "PA5", title: "Code generation", summary: "MIPS assembly for SPIM and the Cool runtime, run in the embedded simulator." },
];

// Collector options of the MIPS backend, mirroring coolc's -g, -t, and -T.
export const GC_OPTIONS = [
  { key: "generational", flag: "-g", label: "generational collector" },
  { key: "test", flag: "-t", label: "collect at every allocation" },
  { key: "debug", flag: "-T", label: "check object headers" },
];

export function toggleGCOption(gc, key) {
  const next = { ...(gc || {}) };
  if (next[key]) delete next[key];
  else next[key] = true;
  return Object.keys(next).length ? next : undefined;
}

// simulatorInput joins the preloaded program input with the lines typed
// while the simulator waited, one line per read.
export function simulatorInput(preloaded, liveLines) {
  const base = String(preloaded || "");
  if (!liveLines || liveLines.length === 0) return base;
  const prefix = base === "" || base.endsWith("\n") ? base : `${base}\n`;
  return `${prefix}${liveLines.join("\n")}\n`;
}

export function runStatusLabel(status) {
  return {
    halted: "finished",
    "runtime-error": "runtime error",
    "step-limit": "step limit reached",
    "waiting-input": "waiting for input",
    cancelled: "cancelled",
    "assembly-error": "assembly error",
    "compile-error": "compile errors",
  }[status] || status || "not run";
}

export function referenceViewOptions(panel) {
  return REFERENCE_VIEWS[panel] || [];
}

export function courseCacheKey(sources, input, options, artifact) {
  return JSON.stringify([artifact, options, input || "", (sources || []).map((source) => [source.id, source.name, source.content])]);
}

// The store-sandbox-friendly export path: on touch/mobile builds use the
// platform share sheet with a File (Web Share Level 2), which hands the file
// to the OS without assuming filesystem access; elsewhere, or when sharing
// files is unsupported, fall back to a browser download of a Blob URL.
// content is text or a Blob (for binary exports such as PNG and ZIP).
export async function shareOrDownload(filename, content, type = "text/plain;charset=utf-8", env = globalThis) {
  const nav = env.navigator;
  const coarse = typeof env.matchMedia === "function" && env.matchMedia("(pointer: coarse)").matches;
  if (coarse && nav && typeof nav.share === "function" && typeof nav.canShare === "function" && typeof env.File === "function") {
    const file = new env.File([content], filename, { type });
    if (nav.canShare({ files: [file] })) {
      try {
        await nav.share({ files: [file], title: filename });
        return "shared";
      } catch (error) {
        if (error && error.name === "AbortError") return "cancelled";
      }
    }
  }
  const doc = env.document;
  const blob = new env.Blob([content], { type });
  const url = env.URL.createObjectURL(blob);
  const anchor = doc.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  anchor.rel = "noopener";
  doc.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  env.setTimeout?.(() => env.URL.revokeObjectURL(url), 1000);
  return "downloaded";
}

export function createCourseController({ api, sources, input, escapeHTML, rerender, toast, stem, hooks = {} }) {
  const views = { tokens: "cooler", ast: "cooler", diagnostics: "cooler" };
  const options = {};
  const cache = new Map();
  const simulator = { result: null, running: false, live: [], sourcesKey: "" };
  let assignment = "pa1";
  const pending = new Set();
  const errors = new Map();

  function key(artifact) {
    return courseCacheKey(sources(), input(), options, artifact);
  }

  async function load(artifact) {
    const cacheKey = key(artifact);
    if (cache.has(cacheKey) || pending.has(cacheKey)) return cache.get(cacheKey);
    if (cache.size > 32) cache.clear();
    pending.add(cacheKey);
    errors.delete(cacheKey);
    try {
      const response = await api("/api/course", { sources: sources(), input: input(), artifacts: [artifact], ...options });
      cache.set(cacheKey, response[artifact] || null);
      return cache.get(cacheKey);
    } catch (error) {
      errors.set(cacheKey, error.message || String(error));
      return null;
    } finally {
      pending.delete(cacheKey);
      rerender();
    }
  }

  function viewSwitchHTML(panel) {
    const choices = referenceViewOptions(panel);
    if (choices.length === 0) return "";
    return `<div class="course-view-switch" role="group" aria-label="${escapeHTML(panel)} view">${choices.map((choice) => {
      const active = views[panel] === choice.id;
      return `<button type="button" class="course-view-button${active ? " active" : ""}" aria-pressed="${active}" data-course-view="${escapeHTML(panel)}:${escapeHTML(choice.id)}">${escapeHTML(choice.label)}</button>`;
    }).join("")}</div>`;
  }

  function textArtifactHTML(artifact, { title, file, emptyText }) {
    const cacheKey = key(artifact);
    if (errors.has(cacheKey)) return `<p class="course-error" role="alert">${escapeHTML(errors.get(cacheKey))}</p>`;
    if (!cache.has(cacheKey)) {
      void load(artifact);
      return `<p class="course-loading" role="status">Building ${escapeHTML(title)}…</p>`;
    }
    const value = cache.get(cacheKey);
    if (!value) return `<p class="course-error" role="alert">No ${escapeHTML(title)} was produced.</p>`;
    const status = value.ok ? "" : `<span class="course-badge error">halted · exit ${escapeHTML(String(value.exitCode))}</span>`;
    return `<section class="course-artifact" aria-label="${escapeHTML(title)}">
      <header class="course-artifact-header"><h3>${escapeHTML(title)}</h3>${status}
        <button type="button" class="button secondary course-export" data-course-export="${escapeHTML(artifact)}" data-course-file="${escapeHTML(file)}">Export / share</button></header>
      ${!value.text && emptyText ? `<p class="course-empty" role="status">${escapeHTML(emptyText)}</p>` : `<pre class="code-block course-text">${escapeHTML(value.text || "") || " "}</pre>`}</section>`;
  }

  // render returns HTML for a panel's reference view, or null for the default view.
  function render(panel) {
    const choice = referenceViewOptions(panel).find((item) => item.id === views[panel]);
    if (!choice || !choice.artifact) return null;
    return viewSwitchHTML(panel) + textArtifactHTML(choice.artifact, choice);
  }

  function artifactText(artifact, value) {
    if (artifact === "mips") return value.ok ? value.assembly : "";
    return value.text || "";
  }

  async function exportArtifact(artifact, file) {
    const value = cache.get(key(artifact)) || await load(artifact);
    if (!value || (artifact === "mips" && !value.ok)) {
      toast(artifact === "mips" ? "MIPS output needs a program without errors." : "The artifact is not available yet.", "error");
      return;
    }
    const outcome = await shareOrDownload(`${stem()}.${file}`, artifactText(artifact, value));
    if (outcome !== "cancelled") toast(outcome === "shared" ? "Artifact shared." : "Artifact exported.");
  }

  function loadingOrError(artifact, title) {
    const cacheKey = key(artifact);
    if (errors.has(cacheKey)) return `<p class="course-error" role="alert">${escapeHTML(errors.get(cacheKey))}</p>`;
    if (!cache.has(cacheKey)) {
      void load(artifact);
      return `<p class="course-loading" role="status">Building ${escapeHTML(title)}…</p>`;
    }
    return "";
  }

  // mipsHTML renders the MIPS panel: the generated SPIM assembly with a .s
  // export, or the diagnostics that prevent generation.
  function mipsHTML() {
    const title = "MIPS assembly (PA5)";
    const pending = loadingOrError("mips", title);
    if (pending) return pending;
    const value = cache.get(key("mips"));
    const gc = options.gc || {};
    const gcControls = `<fieldset class="course-gc"><legend>Garbage collector</legend>${GC_OPTIONS.map((option) => `<label><input type="checkbox" data-course-gc="${option.key}" ${gc[option.key] ? "checked" : ""}> <code>${option.flag}</code> ${escapeHTML(option.label)}</label>`).join("")}</fieldset>`;
    const header = `${gcControls}<header class="course-artifact-header"><h3>${title}</h3><span class="course-badge">SPIM · Cool runtime</span>
      <button type="button" class="button secondary course-export" data-course-export="mips" data-course-file="s" ${value?.ok ? "" : "disabled"}>Export .s / share</button></header>`;
    if (!value?.ok) {
      const items = (value?.diagnostics || []).filter((d) => d.severity === "error").map((d) => `<li>${escapeHTML(`${d.code || d.stage}: ${d.message}`)}</li>`).join("");
      return `<section class="course-artifact">${header}<p class="course-error" role="alert">MIPS generation needs a program without errors.</p><ul class="course-diagnostics">${items}</ul></section>`;
    }
    return `<section class="course-artifact" aria-label="${title}">${header}${simulatorHTML()}<details class="course-assembly"><summary>Generated assembly (${escapeHTML(String(value.assembly.split("\n").length))} lines)</summary><pre class="code-block course-text">${escapeHTML(value.assembly)}</pre></details></section>`;
  }

  function currentSourcesKey() {
    return courseCacheKey(sources(), "", options, "run");
  }

  async function runSimulator() {
    if (simulator.running) return;
    simulator.running = true;
    rerender();
    try {
      const response = await api("/api/course", {
        sources: sources(), input: simulatorInput(input(), simulator.live), artifacts: ["run"], interactive: true, ...options,
      });
      simulator.result = response.run || null;
      simulator.sourcesKey = currentSourcesKey();
    } catch (error) {
      simulator.result = { status: "error", error: error.message || String(error), output: "" };
    } finally {
      simulator.running = false;
      rerender();
    }
  }

  // simulatorHTML renders the embedded simulator: run controls, program
  // output, status, collector statistics, and a live input line while the
  // program waits for a read.
  function simulatorHTML() {
    if (simulator.result && simulator.sourcesKey !== currentSourcesKey()) {
      simulator.result = null;
      simulator.live = [];
    }
    const result = simulator.result;
    const controls = `<div class="course-run-controls">
      <button type="button" class="button primary" data-course-action="run" ${simulator.running ? "disabled" : ""}>${simulator.running ? "Running…" : "Run in simulator"}</button>
      <span class="course-hint">Embedded SPIM-compatible simulator with the Cool runtime; uses the Console's preloaded input.</span></div>`;
    if (!result) return `<section class="course-run" aria-label="MIPS simulator">${controls}</section>`;
    const ok = result.status === "halted";
    const gc = result.gc ? `<span class="course-badge">${escapeHTML(result.gc.collector || "NoGC")} · ${escapeHTML(String(result.gc.allocations))} allocations · ${escapeHTML(String(result.gc.collections))} collections</span>` : "";
    const waiting = result.status === "waiting-input"
      ? `<div class="course-live-input"><label for="courseLiveInput">Program input</label><input id="courseLiveInput" type="text" data-course-input autocomplete="off" spellcheck="false"><button type="button" class="button primary" data-course-action="send-input">Send</button></div>`
      : "";
    const diagnostics = (result.diagnostics || []).filter((d) => d.severity === "error").map((d) => `<li>${escapeHTML(`${d.code || d.stage}: ${d.message}`)}</li>`).join("");
    return `<section class="course-run" aria-label="MIPS simulator">${controls}
      <div class="course-run-status"><span class="course-badge ${ok ? "ok" : result.status === "waiting-input" ? "" : "error"}" role="status">${escapeHTML(runStatusLabel(result.status))}</span>
      <span class="course-badge">${escapeHTML(String(result.steps || 0))} instructions</span>${gc}</div>
      ${result.error && !ok && result.status !== "waiting-input" ? `<p class="course-error" role="alert">${escapeHTML(result.error)}</p>` : ""}
      ${diagnostics ? `<ul class="course-diagnostics">${diagnostics}</ul>` : ""}
      <pre class="console-output course-run-output" aria-live="polite">${escapeHTML(result.output || "") || '<span class="console-muted">(no output)</span>'}</pre>
      ${waiting}</section>`;
  }

  function frontendSwitchHTML() {
    const current = hooks.frontend?.() === "generated" ? "generated" : "handwritten";
    const choices = [["handwritten", "Hand-written front-end"], ["generated", "Generated (Flex/Bison-style)"]];
    return `<div class="course-view-switch" role="group" aria-label="Front-end">${choices.map(([id, label]) => `<button type="button" class="course-view-button${current === id ? " active" : ""}" aria-pressed="${current === id}" data-course-frontend="${id}">${escapeHTML(label)}</button>`).join("")}</div>`;
  }

  // courseModeHTML renders Course mode: a PA1–PA5 navigator and the selected
  // assignment's artifacts, actions, and exports, stacked for narrow screens.
  function courseModeHTML() {
    const current = COURSE_ASSIGNMENTS.find((item) => item.id === assignment) || COURSE_ASSIGNMENTS[0];
    const nav = `<nav class="course-nav" aria-label="Practical assignments">${COURSE_ASSIGNMENTS.map((item) => `<button type="button" class="course-pa${item.id === current.id ? " active" : ""}" aria-pressed="${item.id === current.id}" data-course-pa="${item.id}"><strong>${item.label}</strong><span>${escapeHTML(item.title)}</span></button>`).join("")}</nav>`;
    let body = "";
    switch (current.id) {
      case "pa1":
        body = `<div class="course-run-controls"><button type="button" class="button secondary" data-course-action="pa1-open">Open the stack-machine example</button>
          <button type="button" class="button primary" data-course-action="pa1-run">Run on the Go VM</button></div>
          <p class="course-hint">Opening the example loads <code>13_stack_machine.cl</code> and <code>atoi.cl</code> with the course <code>stack.test</code> script as preloaded input; the run's output appears in the Console. <code>TestStackMachineMatchesCourseScript</code> pins the expected output.</p>`;
        break;
      case "pa2":
        body = frontendSwitchHTML() + textArtifactHTML("tokens", { title: "Reference lexer token stream (PA2)", file: "tokens.ref.txt" });
        break;
      case "pa3":
        body = frontendSwitchHTML() + textArtifactHTML("ast", { title: "Reference parser AST (PA3)", file: "ast.ref.txt" });
        break;
      case "pa4":
        body = textArtifactHTML("diagnostics", { title: "Reference diagnostics (PA4)", file: "diagnostics.ref.txt", emptyText: "No errors: lexer, parser, and semant accept the program." })
          + textArtifactHTML("semant", { title: "Reference typed AST (PA4)", file: "typed-ast.ref.txt" });
        break;
      case "pa5":
        body = mipsHTML();
        break;
    }
    return `<section class="course-mode" aria-label="Course mode">
      <header class="course-mode-header"><h2>Course mode</h2><p>Each compiler phase as its practical assignment. Everything runs in the app; no course toolchain is needed.</p></header>
      ${nav}<section class="course-assignment" aria-labelledby="courseAssignmentTitle"><h3 id="courseAssignmentTitle">${current.label} · ${escapeHTML(current.title)}</h3><p class="course-hint">${escapeHTML(current.summary)}</p>${body}</section></section>`;
  }

  function handleClick(target) {
    const pa = target.closest?.("[data-course-pa]");
    if (pa) {
      assignment = pa.dataset.coursePa;
      rerender();
      return true;
    }
    const frontendButton = target.closest?.("[data-course-frontend]");
    if (frontendButton) {
      hooks.setFrontend?.(frontendButton.dataset.courseFrontend);
      rerender();
      return true;
    }
    const viewButton = target.closest?.("[data-course-view]");
    if (viewButton) {
      const [panel, view] = viewButton.dataset.courseView.split(":");
      views[panel] = view;
      rerender();
      return true;
    }
    const gcToggle = target.closest?.("[data-course-gc]");
    if (gcToggle) {
      options.gc = toggleGCOption(options.gc, gcToggle.dataset.courseGc);
      if (!options.gc) delete options.gc;
      rerender();
      return true;
    }
    const action = target.closest?.("[data-course-action]");
    if (action) {
      if (action.dataset.courseAction === "pa1-open") {
        hooks.openStackMachine?.();
      } else if (action.dataset.courseAction === "pa1-run") {
        hooks.runVM?.();
      } else if (action.dataset.courseAction === "run") {
        simulator.live = [];
        void runSimulator();
      } else if (action.dataset.courseAction === "send-input") {
        const field = action.closest?.(".course-live-input")?.querySelector?.("[data-course-input]");
        simulator.live.push(field ? String(field.value) : "");
        void runSimulator();
      }
      return true;
    }
    const exportButton = target.closest?.("[data-course-export]");
    if (exportButton) {
      void exportArtifact(exportButton.dataset.courseExport, exportButton.dataset.courseFile);
      return true;
    }
    return false;
  }

  function invalidate() {
    cache.clear();
    errors.clear();
  }

  return { views, options, render, viewSwitchHTML, handleClick, invalidate, exportArtifact, load, mipsHTML, courseModeHTML, get assignment() { return assignment; } };
}
