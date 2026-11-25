// SPDX-License-Identifier: AGPL-3.0-or-later
import { readFileSync } from 'node:fs';
const bytes = readFileSync(process.argv[2]);
if (!WebAssembly.validate(bytes)) throw new Error('invalid Wasm module');
const module = new WebAssembly.Module(bytes);
const spans = JSON.parse(new TextDecoder().decode(WebAssembly.Module.customSections(module, 'cooler.spans')[0]));
let output, ticks;
const instance = new WebAssembly.Instance(module, { env: {
  out_int(value, self) { output += String(value); return self; },
  tick(span) { if (++ticks > 1000) throw { code: 'budget', span: spans[span] }; },
  fail(code, span) { throw { code: `COL${code}`, span: spans[span] }; },
} });
function run() {
  output = ''; ticks = 0;
  try { return { result: instance.exports.run(), output, ticks }; }
  catch (error) {
    if (!error.code) throw error;
    return { error, output, ticks };
  }
}
const result = run();
const start = performance.now();
for (let i = 0; i < 100; i++) run();
process.stdout.write(JSON.stringify({ ...result, bytes: bytes.length, meanMs: (performance.now() - start) / 100 }));
