// SPDX-License-Identifier: AGPL-3.0-or-later
const vscode=require('vscode');
const {spawn}=require('node:child_process');
const {pathToFileURL}=require('node:url');
const {LanguageClient}=require('vscode-languageclient/node');
const {selected}=require('./source-set.cjs');
let client,output,watcher,operations=new Set(),restarting=Promise.resolve(),closing=false,commandBusy=false;

function start(){
 const next=restarting.catch(()=>{}).then(startSerial);restarting=next;return next;
}
async function startSerial(){
 if(closing)return;
 if(!vscode.workspace.isTrusted)return;
 if(client)await client.stop();
 const config=vscode.workspace.getConfiguration('cooler');
 client=new LanguageClient('cooler','Cooler language server',{command:config.get('lspPath'),options:{shell:false,windowsHide:true}}, {
  documentSelector:[{scheme:'file',language:'cool'}],
  uriConverters:{code2Protocol:uri=>uri.scheme==='file'?pathToFileURL(uri.fsPath).href:uri.toString(),protocol2Code:uri=>vscode.Uri.parse(uri)},
  initializationOptions:{sourceSet:config.get('sourceSet'),warningRules:config.get('warningRules'),inlayHints:{parameterNames:config.get('inlayHints.parameterNames'),selfType:config.get('inlayHints.selfType')}},
  synchronize:{configurationSection:'cooler',fileEvents:watcher}
 });
 try{await client.start();}catch(error){void vscode.window.showErrorMessage(`Cooler could not start its local language server: ${error.message}. Configure cooler.lspPath; no binary is downloaded.`);}
}

async function runProject(command){
 if(!vscode.workspace.isTrusted)throw new Error('Trust the workspace before running Cooler.');
 const roots=vscode.workspace.workspaceFolders || [];
 if(!roots.length)throw new Error('Open a local workspace folder first.');
 const config=vscode.workspace.getConfiguration('cooler');
 if(commandBusy || operations.size)throw new Error('A Cooler command is already running.');
 commandBusy=true;
 try {
 const set=config.get('sourceSet');
 const {searchPatterns}=require('./source-set.cjs');
 const {includes,exclude}=searchPatterns(set);
 const found=new Map();
 for(const include of includes){for(const uri of await vscode.workspace.findFiles(include,exclude,1025)){if(selected(vscode.workspace.asRelativePath(uri,false).replaceAll('\\','/'),set))found.set(uri.toString(),uri);if(found.size>1024)throw new Error('Selected project exceeds 1024 files.');}}
 const files=[...found.values()].sort((a,b)=>a.toString().localeCompare(b.toString()));
 if(!files.length)throw new Error('The selected source set contains no COOL files.');
 if(vscode.workspace.textDocuments.some(doc=>doc.isDirty && files.some(uri=>uri.toString()===doc.uri.toString())))throw new Error('Save modified COOL files before Check or Run.');
 output.show(true);output.appendLine(`Cooler ${command}: ${files.length} files`);
 return await new Promise((resolve,reject)=>{
  const child=spawn(config.get('compilerPath'),[command,'--warnings',config.get('warningRules'),...files.map(uri=>uri.fsPath)],{cwd:roots[0].uri.fsPath,shell:false,windowsHide:true,stdio:['ignore','pipe','pipe']});
  operations.add(child);let bytes=0,limited=false;
  const timer=setTimeout(()=>{limited=true;child.kill();},10000);
  child.stdout.setEncoding('utf8');child.stderr.setEncoding('utf8');
  const receive=data=>{bytes+=Buffer.byteLength(data);if(bytes>1048576){limited=true;child.kill();return;}output.append(data);};
  child.stdout.on('data',receive);child.stderr.on('data',receive);
  child.once('error',error=>{clearTimeout(timer);operations.delete(child);reject(new Error(`Could not launch local compiler: ${error.message}`));});
  child.once('close',code=>{clearTimeout(timer);operations.delete(child);output.appendLine(`\nExit code: ${code}${limited?' (time/output limit reached)':''}`);resolve(code);});
 });
 } finally { commandBusy=false; }
}

async function activate(context){
 require("./debug.cjs").registerDebugger(context);
 watcher=vscode.workspace.createFileSystemWatcher('**/*.{cl,cool}');context.subscriptions.push(watcher);
 output=vscode.window.createOutputChannel('Cooler commands');context.subscriptions.push(output);
 for(const command of ['check','run'])context.subscriptions.push(vscode.commands.registerCommand('cooler.'+command,async()=>{try{return await runProject(command);}catch(error){output.appendLine(error.message);void vscode.window.showErrorMessage(error.message);return null;}}));
 context.subscriptions.push(vscode.commands.registerCommand('cooler.restart',start));
 context.subscriptions.push(vscode.workspace.onDidChangeConfiguration(event=>{if(event.affectsConfiguration('cooler.lspPath'))void start();}));
 context.subscriptions.push(vscode.workspace.onDidChangeWorkspaceFolders(()=>void start()));
 await start();
 return {getClient:()=>client};
}
async function deactivate(){closing=true;await restarting.catch(()=>{});for(const child of operations)child.kill();operations.clear();if(client)await client.stop();}
module.exports={activate,deactivate};
