// SPDX-License-Identifier: AGPL-3.0-or-later
import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";

const directory = new URL("../dist/browser-wasm/", import.meta.url);
const allowed = new Set(["index.html", "app.mjs", "client.mjs", "worker.js", "cooler.wasm", "wasm_exec.js", "native-fixtures.json", "build.json", "GO-LICENSE.txt"]);
const types = { html: "text/html", mjs: "text/javascript", js: "text/javascript", wasm: "application/wasm", json: "application/json", txt: "text/plain" };
const server = createServer(async (request, response) => {
  const name = new URL(request.url, "http://localhost").pathname.slice(1) || "index.html";
  if (request.method !== "GET" || !allowed.has(name)) { response.writeHead(404).end(); return; }
  try {
    const bytes = await readFile(new URL(name, directory));
    response.writeHead(200, { "Content-Type": types[name.split(".").at(-1)], "Cache-Control": "no-store" }).end(bytes);
  } catch { response.writeHead(404).end(); }
});
server.listen(9092, "127.0.0.1", () => console.log(`Static files only: http://127.0.0.1:9092 (${fileURLToPath(directory)})`));
