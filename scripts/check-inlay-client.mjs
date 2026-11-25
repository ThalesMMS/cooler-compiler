// SPDX-License-Identifier: AGPL-3.0-or-later
import assert from 'node:assert/strict';
import { connect } from './lsp-client-harness.mjs';
const {lsp,connection,close}=connect(process.argv[2]);
try {
  const initialized = await connection.sendRequest(lsp.InitializeRequest.type, { processId: process.pid, rootUri: null, capabilities: { textDocument: { inlayHint: {} } }, initializationOptions: { inlayHints: { parameterNames: true, selfType: true } } });
  assert.equal(initialized.capabilities.inlayHintProvider, true);
  await connection.sendNotification(lsp.InitializedNotification.type, {});
  const uri = 'file:///inlay-client/main.cl';
  const source = 'class Main inherits IO { main():Object {{ "😀"; out_int(42); }}; };';
  await connection.sendNotification(lsp.DidOpenTextDocumentNotification.type, { textDocument: { uri, languageId: 'cool', version: 1, text: source } });
  const params = { textDocument: { uri }, range: { start: { line: 0, character: 0 }, end: { line: 0, character: source.length } } };
  const first = await connection.sendRequest(lsp.InlayHintRequest.type, params);
  assert.deepEqual(first.map(hint => hint.label), ['x:', ': Main']);
  assert.equal(first[0].position.character, source.indexOf('42'));
  assert.equal(first[0].paddingRight, true);
  assert.match(first[1].tooltip, /not the runtime dynamic type/);
  await connection.sendNotification(lsp.DidChangeConfigurationNotification.type, { settings: { cooler: { inlayHints: { parameterNames: false } } } });
  const typesOnly = await connection.sendRequest(lsp.InlayHintRequest.type, params);
  assert.deepEqual(typesOnly.map(hint => hint.label), [': Main']);
  await connection.sendNotification(lsp.DidChangeConfigurationNotification.type, { settings: { cooler: { inlayHints: { selfType: false } } } });
  assert.deepEqual(await connection.sendRequest(lsp.InlayHintRequest.type, params), []);
  await connection.sendRequest(lsp.ShutdownRequest.type);
  await connection.sendNotification(lsp.ExitNotification.type);
  process.stdout.write('Real LSP protocol client: opt-in initialization, UTF-16, concise labels, padding, static tooltip and independent category settings passed.\n');
} finally {
  close();
}
