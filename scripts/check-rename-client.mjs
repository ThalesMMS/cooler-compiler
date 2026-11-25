// SPDX-License-Identifier: AGPL-3.0-or-later
import assert from 'node:assert/strict';
import { connect } from './lsp-client-harness.mjs';
const {lsp,connection,close}=connect(process.argv[2]);
try {
  const initialized = await connection.sendRequest(lsp.InitializeRequest.type, { processId: process.pid, rootUri: null, capabilities: { workspace: { workspaceEdit: { documentChanges: true } } } });
  assert.equal(initialized.capabilities.renameProvider.prepareProvider, true);
  await connection.sendNotification(lsp.InitializedNotification.type, {});
  const uri = 'file:///rename-client/main.cl';
  const source = 'class Main { main():Object {{ "😀"; let count:Int <- 42 in count; }}; };';
  await connection.sendNotification(lsp.DidOpenTextDocumentNotification.type, { textDocument: { uri, languageId: 'cool', version: 1, text: source } });
  const params = { textDocument: { uri }, position: { line: 0, character: source.indexOf('count') } };
  const prepared = await connection.sendRequest(lsp.PrepareRenameRequest.type, params);
  assert.equal(prepared.placeholder, 'count');
  const renamed = await connection.sendRequest(lsp.RenameRequest.type, { ...params, newName: 'amount' });
  assert.equal(renamed.documentChanges[0].textDocument.version, 1);
  assert.equal(renamed.documentChanges[0].edits.length, 2);
  let changed = source;
  for (const edit of renamed.documentChanges[0].edits.toReversed()) changed = changed.slice(0, edit.range.start.character) + edit.newText + changed.slice(edit.range.end.character);
  assert.equal(changed, source.replaceAll('count', 'amount'));
  await connection.sendNotification(lsp.DidChangeTextDocumentNotification.type, { textDocument: { uri, version: 2 }, contentChanges: [{ text: changed }] });
  const next = await connection.sendRequest(lsp.PrepareRenameRequest.type, params);
  assert.equal(next.placeholder, 'amount');
  await connection.sendRequest(lsp.ShutdownRequest.type);
  await connection.sendNotification(lsp.ExitNotification.type);
  process.stdout.write('Real LSP protocol client: prepare, versioned rename, UTF-16 application and changed-document reanalysis passed.\n');
} finally {
  close();
}
