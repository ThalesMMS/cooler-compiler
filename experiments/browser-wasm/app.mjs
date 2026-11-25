// SPDX-License-Identifier: AGPL-3.0-or-later
import { WorkerClient } from "./client.mjs";
const $ = (id) => document.getElementById(id);
let ready = false, busy = false, state = null, revision = 0;
let frames = 0;
const tick = () => { frames++; requestAnimationFrame(tick); };
requestAnimationFrame(tick);
const measurements = { browser: navigator.userAgent, operations: [] };
const renderMeasurements = () => { $("measurements").textContent = JSON.stringify(measurements, null, 2); };
const controls = () => {
  $("compile").disabled = !ready || busy;
  $("compare").disabled = !ready || busy;
  $("run").disabled = !ready || busy || !state || state.blocked || state.runtime.halted || Boolean(state.runtime.waitingInput);
  $("line").disabled = !state?.runtime.waitingInput || busy;
  $("send").disabled = !ready || busy || !state?.runtime.waitingInput;
};
const client = new WorkerClient(() => new Worker("worker.js"), (status) => {
  ready = Boolean(status.ready);
  if (status.ready) { measurements.initialization = { ...status, generation: revision }; $("status").textContent = "Runtime ready. Compile a program."; renderMeasurements(); }
  else $("status").textContent = status.error || "Loading runtime…";
  controls();
});
fetch("build.json").then((r) => r.json()).then((data) => { measurements.build = data; renderMeasurements(); }).catch(() => {});
function show(reply) {
  state = reply.state;
  $("output").textContent = state.runtime.output?.join("") || "[no output]";
  $("diagnostics").textContent = state.diagnostics?.map((d) => `${d.code} · ${d.stage} · ${d.message}`).join("\n") || "No diagnostics.";
  $("status").textContent = state.blocked ? "Compilation has errors. Edit the source and compile again." : `Execution state: ${state.stopReason}.`;
  controls();
}
async function call(request) {
  const startFrames = frames;
  const reply = await client.request(request);
  measurements.operations.push({ generation: revision, action: request.action, workerMs: reply.workerMs, roundTripMs: reply.roundTripMs, heapBytes: reply.heapBytes, goSystemBytes: reply.goSystemBytes, linearMemoryBytes: reply.linearMemoryBytes, interfaceFrames: frames - startFrames });
  measurements.operations = measurements.operations.slice(-32);
  renderMeasurements();
  return reply;
}
async function action(work) {
  const current = revision;
  busy = true; controls();
  try { await work(); } catch (error) { if (current === revision) $("status").textContent = error.message; }
  finally { if (current === revision) { busy = false; controls(); } }
}
const steps = () => {
  const value = Number($("steps").value);
  if (!Number.isInteger(value) || value < 1 || value > 10000) throw new Error("Choose a step budget from 1 through 10,000.");
  return value;
};
$("compile").onclick = () => action(async () => show(await call({ action: "compile", sources: [{ id: "main", name: "main.cl", content: $("source").value }], input: $("input").value })));
$("run").onclick = () => action(async () => show(await call({ action: "run", steps: steps() })));
$("send").onclick = () => action(async () => {
  show(await call({ action: "input", line: $("line").value, sequence: state.runtime.waitingInput.sequence }));
  $("line").value = "";
  show(await call({ action: "run", steps: steps() }));
});
$("cancel").onclick = () => {
  revision++; busy = false; state = null; ready = false;
  $("output").textContent = "Execution cleared. Compile again when ready.";
  $("diagnostics").textContent = "No compilation yet.";
  client.restart(); controls();
};
for (const id of ["source", "input"]) $(id).oninput = () => {
  if (busy) $("cancel").click();
  state = null; controls();
  $("status").textContent = "Source or input changed. Compile again.";
};
$("compare").onclick = () => action(async () => {
  const current = revision;
  const response = await fetch("native-fixtures.json");
  const fixtures = await response.json();
  let passed = 0;
  const lines = [];
  for (const fixture of fixtures) {
    if (current !== revision) throw new Error("Comparison cancelled.");
    let matches = true;
    for (let index = 0; index < fixture.requests.length; index++) {
      const reply = await call(fixture.requests[index]);
      matches = matches && JSON.stringify(reply.state) === JSON.stringify(fixture.expected[index]);
    }
    if (matches) passed++;
    lines.push(`${matches ? "PASS" : "FAIL"} ${fixture.name}`);
    $("comparison").textContent = lines.join("\n");
  }
  state = null;
  $("comparison").textContent += `\n${passed}/${fixtures.length} matched native results.`;
  $("status").textContent = "Comparison complete. Compile your source to continue.";
});
