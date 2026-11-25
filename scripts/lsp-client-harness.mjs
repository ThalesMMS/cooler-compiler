// SPDX-License-Identifier: AGPL-3.0-or-later
import { createRequire } from 'node:module';
import { resolve } from 'node:path';
import { spawn } from 'node:child_process';

// Use the pinned Microsoft protocol client in ignored dist/lsp-client.
// Running this harness never downloads dependencies.
const require = createRequire(resolve('dist/lsp-client/package.json'));
const lsp = require('vscode-languageserver-protocol/node');

export function connect(executablePath) {
  if (!executablePath) throw new Error('pass the cooler-lsp executable path');
  const child = spawn(resolve(executablePath), [], {stdio: ['pipe', 'pipe', 'pipe'], windowsHide: true});
  const connection = lsp.createProtocolConnection(new lsp.StreamMessageReader(child.stdout), new lsp.StreamMessageWriter(child.stdin));
  let closed = false;
  const watchdog = setTimeout(() => { console.error('LSP client validation exceeded 20 seconds'); process.exitCode = 1; close(); }, 20000);
  child.stderr.resume();
  child.once('error', error => { console.error(error.message); process.exitCode = 1; close(); });
  function close() {
    if (closed) return;
    closed = true;
    clearTimeout(watchdog);
    connection.dispose();
    child.kill();
  }
  connection.listen();
  return {lsp, connection, close};
}
