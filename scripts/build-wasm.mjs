// SPDX-License-Identifier: AGPL-3.0-or-later
import { execFileSync } from "node:child_process";
import { copyFile, mkdir, readFile, stat, writeFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { gzipSync } from "node:zlib";
import { fileURLToPath } from "node:url";
import path from "node:path";

const root = fileURLToPath(new URL("../", import.meta.url));
const output = path.join(root, "dist", "browser-wasm");
await mkdir(output, { recursive: true });
const go = (...args) => execFileSync("go", args, { cwd: root, encoding: "utf8", maxBuffer: 16 << 20 });
const goroot = go("env", "GOROOT").trim();
execFileSync("go", ["build", "-trimpath", "-ldflags=-s -w", "-o", path.join(output, "cooler.wasm"), "./cmd/cooler-wasm"], {
  cwd: root, stdio: "inherit", env: { ...process.env, CGO_ENABLED: "0", GOOS: "js", GOARCH: "wasm" },
});
for (const name of ["index.html", "app.mjs", "client.mjs", "worker.js"]) {
  await copyFile(path.join(root, "experiments", "browser-wasm", name), path.join(output, name));
}
await copyFile(path.join(goroot, "lib", "wasm", "wasm_exec.js"), path.join(output, "wasm_exec.js"));
await copyFile(path.join(goroot, "LICENSE"), path.join(output, "GO-LICENSE.txt"));
await writeFile(path.join(output, "native-fixtures.json"), go("run", "./cmd/cooler-wasm"));
const wasm = await readFile(path.join(output, "cooler.wasm"));
const metadata = {
  toolchain: go("version").trim(),
  commit: execFileSync("git", ["rev-parse", "HEAD"], { cwd: root, encoding: "utf8" }).trim(),
	workingTreeDirty: Boolean(execFileSync("git", ["status", "--porcelain"], { cwd: root, encoding: "utf8" }).trim()),
  wasmBytes: (await stat(path.join(output, "cooler.wasm"))).size,
  gzipBytes: gzipSync(wasm).byteLength,
  sha256: createHash("sha256").update(wasm).digest("hex"),
};
await writeFile(path.join(output, "build.json"), JSON.stringify(metadata, null, 2) + "\n");
console.log(JSON.stringify({ output, ...metadata }, null, 2));
