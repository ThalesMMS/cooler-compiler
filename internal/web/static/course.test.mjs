/* SPDX-License-Identifier: AGPL-3.0-or-later */

import assert from "node:assert/strict";
import test from "node:test";

import { COURSE_ASSIGNMENTS, courseCacheKey, createCourseController, referenceViewOptions, runStatusLabel, shareOrDownload, simulatorInput, toggleGCOption } from "./course.mjs";

function fakeEnv({ coarse = false, canShare = true, shareError = null } = {}) {
  const calls = { shared: [], clicked: [], revoked: [] };
  const body = { appendChild() {} };
  return {
    calls,
    matchMedia: () => ({ matches: coarse }),
    navigator: {
      canShare: () => canShare,
      share: async (data) => { if (shareError) throw shareError; calls.shared.push(data); },
    },
    File: class { constructor(parts, name, options) { this.parts = parts; this.name = name; this.type = options.type; } },
    Blob: class { constructor(parts, options) { this.parts = parts; this.type = options.type; } },
    URL: { createObjectURL: () => "blob:x", revokeObjectURL: (url) => calls.revoked.push(url) },
    setTimeout: (fn) => fn(),
    document: {
      body,
      createElement: () => ({ click() { calls.clicked.push(this.download); }, remove() {} }),
    },
  };
}

test("desktop export downloads a blob", async () => {
  const env = fakeEnv();
  assert.equal(await shareOrDownload("a.tokens.ref.txt", "#name", undefined, env), "downloaded");
  assert.deepEqual(env.calls.clicked, ["a.tokens.ref.txt"]);
  assert.equal(env.calls.shared.length, 0);
});

test("touch builds share a file through the platform share sheet", async () => {
  const env = fakeEnv({ coarse: true });
  assert.equal(await shareOrDownload("a.s", "main:", undefined, env), "shared");
  assert.equal(env.calls.shared[0].files[0].name, "a.s");
  assert.equal(env.calls.clicked.length, 0);
});

test("a cancelled share does not fall back to a download", async () => {
  const error = new Error("cancel");
  error.name = "AbortError";
  const env = fakeEnv({ coarse: true, shareError: error });
  assert.equal(await shareOrDownload("a.s", "x", undefined, env), "cancelled");
  assert.equal(env.calls.clicked.length, 0);
});

test("unsupported file sharing falls back to a download", async () => {
  const env = fakeEnv({ coarse: true, canShare: false });
  assert.equal(await shareOrDownload("a.s", "x", undefined, env), "downloaded");
});

test("cache keys change with sources, input, options, and artifact", () => {
  const sources = [{ id: "a", name: "a.cl", content: "class A {};" }];
  const base = courseCacheKey(sources, "", {}, "tokens");
  assert.notEqual(base, courseCacheKey([{ ...sources[0], content: "x" }], "", {}, "tokens"));
  assert.notEqual(base, courseCacheKey(sources, "1", {}, "tokens"));
  assert.notEqual(base, courseCacheKey(sources, "", { frontend: "generated" }, "tokens"));
  assert.notEqual(base, courseCacheKey(sources, "", {}, "ast"));
});

test("panels default to the Cooler view and load reference text on demand", async () => {
  const requests = [];
  let renders = 0;
  const controller = createCourseController({
    api: async (path, body) => { requests.push([path, body]); return { tokens: { text: "#name \"a.cl\"\n", ok: true } }; },
    sources: () => [{ id: "a", name: "a.cl", content: "class A {};" }],
    input: () => "",
    escapeHTML: (value) => String(value).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/"/g, "&quot;"),
    rerender: () => { renders += 1; },
    toast: () => {},
    stem: () => "a",
  });
  assert.ok(referenceViewOptions("tokens").some((choice) => choice.id === "reference"));
  assert.equal(controller.render("tokens"), null);
  assert.equal(requests.length, 0);
  controller.handleClick({ closest: (selector) => (selector === "[data-course-view]" ? { dataset: { courseView: "tokens:reference" } } : null) });
  assert.match(controller.render("tokens"), /Building/);
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.equal(requests[0][0], "/api/course");
  assert.deepEqual(requests[0][1].artifacts, ["tokens"]);
  assert.match(controller.render("tokens"), /#name &quot;a.cl&quot;/);
  assert.ok(renders >= 2);
});

test("simulator input appends live lines after the preloaded input", () => {
  assert.equal(simulatorInput("", []), "");
  assert.equal(simulatorInput("a\n", ["b"]), "a\nb\n");
  assert.equal(simulatorInput("a", ["b", "c"]), "a\nb\nc\n");
  assert.equal(simulatorInput("", ["x"]), "x\n");
  assert.equal(runStatusLabel("waiting-input"), "waiting for input");
  assert.equal(runStatusLabel("halted"), "finished");
});

test("collector options toggle like coolc flags", () => {
  let gc = toggleGCOption(undefined, "generational");
  assert.deepEqual(gc, { generational: true });
  gc = toggleGCOption(gc, "test");
  assert.deepEqual(gc, { generational: true, test: true });
  gc = toggleGCOption(toggleGCOption(gc, "generational"), "test");
  assert.equal(gc, undefined);
});

test("course mode maps PA1 to PA5 and wires the app hooks", async () => {
  const calls = [];
  let frontend = "handwritten";
  const controller = createCourseController({
    api: async (path, body) => ({ [body.artifacts[0]]: body.artifacts[0] === "mips" ? { ok: true, assembly: "main:" } : { text: `${body.artifacts[0]} text`, ok: true } }),
    sources: () => [{ id: "a", name: "a.cl", content: "class Main {};" }],
    input: () => "",
    escapeHTML: (value) => String(value),
    rerender: () => {},
    toast: () => {},
    stem: () => "a",
    hooks: {
      openStackMachine: () => calls.push("open"),
      runVM: () => calls.push("run"),
      frontend: () => frontend,
      setFrontend: (value) => { frontend = value; calls.push(`frontend:${value}`); },
    },
  });
  assert.deepEqual(COURSE_ASSIGNMENTS.map((item) => item.label), ["PA1", "PA2", "PA3", "PA4", "PA5"]);
  const click = (attribute, value) => controller.handleClick({ closest: (selector) => (selector === `[${attribute}]` ? { dataset: value } : null) });
  assert.match(controller.courseModeHTML(), /stack-machine example/);
  click("data-course-action", { courseAction: "pa1-open" });
  click("data-course-action", { courseAction: "pa1-run" });
  click("data-course-pa", { coursePa: "pa2" });
  assert.equal(controller.assignment, "pa2");
  assert.match(controller.courseModeHTML(), /Reference lexer token stream/);
  click("data-course-frontend", { courseFrontend: "generated" });
  assert.deepEqual(calls, ["open", "run", "frontend:generated"]);
  for (const pa of ["pa3", "pa4", "pa5"]) {
    click("data-course-pa", { coursePa: pa });
    controller.courseModeHTML();
  }
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.match(controller.courseModeHTML(), /Run in simulator/);
});

test("binary exports share the Blob itself", async () => {
  const env = fakeEnv({ coarse: true });
  const blob = { size: 4 };
  assert.equal(await shareOrDownload("a.png", blob, "image/png", env), "shared");
  assert.deepEqual(env.calls.shared[0].files[0].parts, [blob]);
  assert.equal(env.calls.shared[0].files[0].type, "image/png");
});
