// SPDX-License-Identifier: AGPL-3.0-or-later
import assert from 'node:assert/strict';
import { connect } from './lsp-client-harness.mjs';
const {lsp,connection,close}=connect(process.argv[2]);
try {
  const initialized = await connection.sendRequest(lsp.InitializeRequest.type, { processId: process.pid, rootUri: null, capabilities: { textDocument: { signatureHelp: { signatureInformation: { documentationFormat: ['plaintext'] } } } } });
  assert.deepEqual(initialized.capabilities.signatureHelpProvider.triggerCharacters, ['(', ',']);
  await connection.sendNotification(lsp.InitializedNotification.type, {});
  const uri = 'file:///signature-client/main.cl';
  const prefix = 'class Main inherits IO { main():Object { out_int(';
  await connection.sendNotification(lsp.DidOpenTextDocumentNotification.type, { textDocument: { uri, languageId: 'cool', version: 1, text: prefix } });
  const first = await connection.sendRequest(lsp.SignatureHelpRequest.type, { textDocument: { uri }, position: { line: 0, character: prefix.length }, context: { triggerKind: 2, triggerCharacter: '(', isRetrigger: false } });
  assert.equal(first.signatures[0].label, 'IO.out_int(x: Int): Main');
  assert.equal(first.activeParameter, 0);
  assert.match(first.signatures[0].documentation, /signed 32-bit/);
  await connection.sendNotification(lsp.DidChangeTextDocumentNotification.type, { textDocument: { uri, version: 2 }, contentChanges: [{ text: prefix + '1)' }] });
  const closed = await connection.sendRequest(lsp.SignatureHelpRequest.type, { textDocument: { uri }, position: { line: 0, character: prefix.length + 2 }, context: { triggerKind: 2, triggerCharacter: ')', isRetrigger: true } });
  assert.equal(closed, null);
  await connection.sendRequest(lsp.ShutdownRequest.type);
  await connection.sendNotification(lsp.ExitNotification.type);
  process.stdout.write('Real LSP protocol client: capability, incomplete call, builtin documentation, version update and close passed.\n');
} finally {
  close();
}
