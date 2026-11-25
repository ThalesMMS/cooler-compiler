// SPDX-License-Identifier: AGPL-3.0-or-later
const vscode=require('vscode');
const assert=require('node:assert/strict');
const path=require('node:path');
async function until(check){const end=Date.now()+10000;while(Date.now()<end){const value=await check();if(value)return value;await new Promise(resolve=>setTimeout(resolve,100));}throw new Error('Timed out waiting for editor feature');}
exports.run=async()=>{
 let phase='editor features';
 let deadline=setTimeout(()=>{console.error('Extension '+phase+' phase exceeded 30 seconds');process.exit(1);},30000);
 try{
  const doc=await vscode.workspace.openTextDocument(vscode.Uri.file(path.join(process.env.COOLER_TEST_WORKSPACE,'main.cl')));
  await vscode.window.showTextDocument(doc);
  const extension=vscode.extensions.getExtension('cooler-compiler.cooler-local');assert(extension,'installed VSIX not found');
  const api=await extension.activate();await until(()=>api.getClient()?.isRunning());
  const text=doc.getText(),call=doc.positionAt(text.indexOf('out_int'));
  const hover=await vscode.commands.executeCommand('vscode.executeHoverProvider',doc.uri,call);assert(hover.length,'hover');
  const completion=await vscode.commands.executeCommand('vscode.executeCompletionItemProvider',doc.uri,call);assert(completion.items.length,'completion');
  const symbols=await vscode.commands.executeCommand('vscode.executeDocumentSymbolProvider',doc.uri);assert(symbols.length,'symbols');
  const definition=await vscode.commands.executeCommand('vscode.executeDefinitionProvider',doc.uri,doc.positionAt(text.indexOf('Main')));assert(definition.length,'definition');
  const tokens=await vscode.commands.executeCommand('vscode.provideDocumentSemanticTokens',doc.uri);assert(tokens.data.length,'semantic tokens');
  await Promise.all([vscode.commands.executeCommand('cooler.restart'),vscode.commands.executeCommand('cooler.restart')]);await until(()=>api.getClient()?.isRunning());
  assert.equal(await vscode.commands.executeCommand('cooler.check'),0);
  assert.equal(await vscode.commands.executeCommand('cooler.run'),0);
  const bad=text.replace('out_int','out_it');const edit=new vscode.WorkspaceEdit();edit.replace(doc.uri,new vscode.Range(doc.positionAt(0),doc.positionAt(text.length)),bad);assert(await vscode.workspace.applyEdit(edit));
  const diagnostic=await until(()=>vscode.languages.getDiagnostics(doc.uri).find(item=>item.severity===vscode.DiagnosticSeverity.Error));
  assert.equal(await vscode.commands.executeCommand('cooler.check'),null,'unsaved file should be refused');await doc.save();
  assert.equal(await vscode.commands.executeCommand('cooler.check'),1,'compiler error exit status');
  const actions=await vscode.commands.executeCommand('vscode.executeCodeActionProvider',doc.uri,diagnostic.range,vscode.CodeActionKind.QuickFix.value);assert(actions.some(action=>action.edit),'quick fix: '+JSON.stringify({diagnostics:vscode.languages.getDiagnostics(doc.uri),actions}));
  const action=actions.find(action=>action.edit);assert(await vscode.workspace.applyEdit(action.edit));
  await until(()=>vscode.languages.getDiagnostics(doc.uri).every(item=>item.severity!==vscode.DiagnosticSeverity.Error));
  assert.match(doc.getText(),/out_int/);await doc.save();
  clearTimeout(deadline);phase='debugger';
  deadline=setTimeout(()=>{console.error('Extension '+phase+' phase exceeded 45 seconds');process.exit(1);},45000);
  await require("./debug-suite.cjs").runDebug();
  await api.getClient().stop();assert(!api.getClient().isRunning(),'LSP did not stop');
  console.log(`VS Code ${vscode.version}: installed VSIX, startup/stop, diagnostics, completion, hover, definition, symbols, semantic tokens, QuickFix application, check/run passed.`);
 }finally{clearTimeout(deadline);}
};
