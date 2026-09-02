// SPDX-License-Identifier: AGPL-3.0-or-later

import { createRequire } from "node:module";
import { performance } from "node:perf_hooks";

const require = createRequire(import.meta.url);
const client = require("../internal/web/static/client-utils.js");

function parseIterations(argv) {
  const index = argv.indexOf("--iterations");
  if (index < 0 || !/^\d+$/.test(argv[index + 1] || "")) throw new Error("use --iterations <1..100000>");
  const value = Number(argv[index + 1]);
  if (value < 1 || value > 100000) throw new Error("iterations must be between 1 and 100000");
  return value;
}

function ast(depth, breadth, sequence = { value: 0 }) {
  const value = sequence.value++;
  return {
    kind: depth ? "Dispatch" : "Integer",
    text: String(value),
    type: "Int",
    span: { sourceId: "main", startOffset: value * 2, endOffset: value * 2 + 1, startLine: value + 1, startColumn: 1 },
    children: depth ? Array.from({ length: breadth }, () => ast(depth - 1, breadth, sequence)) : [],
  };
}

function trace(length, offset = 0) {
  return Array.from({ length }, (_, index) => ({ step: offset + index, op: "ADD", stack: [index, index + 1] }));
}

function measure(name, iterations, operation, metrics = {}) {
  for (let index = 0; index < Math.min(5, iterations); index += 1) operation();
  const start = performance.now();
  let value;
  for (let index = 0; index < iterations; index += 1) value = operation();
  const elapsed = performance.now() - start;
  globalThis.__coolerBenchmarkSink = value;
  return { benchmark: name, metrics: { "ns/op": elapsed * 1e6 / iterations, ...metrics } };
}

const iterations = parseIterations(process.argv.slice(2));
const tree = ast(5, 4);
const previous = { traceOffset: 0, trace: trace(2000), executedSteps: 2000 };
const delta = { traceOffset: 1750, trace: trace(1000, 1750), executedSteps: 2750 };
const report = {
  tokens: Array.from({ length: 5000 }, (_, index) => ({ kind: "identifier", lexeme: `value${index}`, line: index + 1 })),
  ir: Array.from({ length: 5000 }, (_, index) => ({ index, op: "add", operands: [index, index + 1] })),
  cfg: Array.from({ length: 1000 }, (_, index) => ({ id: index, successors: [index + 1] })),
  runtime: previous,
};
const reportBytes = Buffer.byteLength(JSON.stringify(report));

const results = [
  measure("WebRenderAST/large", iterations, () => client.renderASTNode(tree), { "ast-nodes/op": 1365 }),
  measure("WebMergeTrace/heavy", iterations, () => client.mergeLightSnapshot({ runtime: previous, diagnostics: [], stages: [] }, { runtime: delta }), { "trace-entries/op": 2750 }),
  measure("WebSerializeReport/large", iterations, () => JSON.stringify(report), { "payload-bytes/op": reportBytes }),
  measure("WebBuildCompileReport/large", iterations, () => client.buildCompileReport(report, "benchmark.cl", "benchmark"), { "payload-bytes/op": reportBytes }),
];

process.stdout.write(`${JSON.stringify(results)}\n`);
