// SPDX-License-Identifier: AGPL-3.0-or-later
importScripts("wasm_exec.js");
let instance;
const ready = (async () => {
  const start = performance.now();
  const go = new Go();
  const response = await fetch("cooler.wasm");
  if (!response.ok) throw new Error("Unable to load cooler.wasm. Rebuild the local assets.");
  const bytes = await response.arrayBuffer();
  ({ instance } = await WebAssembly.instantiate(bytes, go.importObject));
  go.run(instance).catch((error) => postMessage({ fatal: String(error) }));
  if (typeof coolerHandle !== "function") throw new Error("Go adapter did not initialize.");
  postMessage({ ready: true, initMs: performance.now() - start, wasmBytes: bytes.byteLength, linearMemoryBytes: instance.exports.mem.buffer.byteLength });
})();
ready.catch((error) => postMessage({ fatal: String(error) }));
self.onmessage = async ({ data }) => {
  try {
    await ready;
    const start = performance.now();
    const result = JSON.parse(coolerHandle(JSON.stringify(data)));
    postMessage({ ...result, id: data.id, workerMs: performance.now() - start, linearMemoryBytes: instance.exports.mem.buffer.byteLength });
  } catch (error) { postMessage({ id: data.id, error: String(error) }); }
};
