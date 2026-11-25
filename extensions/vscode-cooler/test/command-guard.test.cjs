// SPDX-License-Identifier: AGPL-3.0-or-later
const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const {EventEmitter}=require('node:events');

test('commands reject concurrency during file discovery and recover after failure',async()=>{
 const commands=new Map(),errors=[];
 let finishSearch,spawned=0;
 const uri={fsPath:'/workspace/main.cl',toString:()=> 'file:///workspace/main.cl'};
 const vscode={
  workspace:{isTrusted:true,workspaceFolders:[{uri:{fsPath:'/workspace'}}],textDocuments:[],
   getConfiguration:()=>({get:key=>({sourceSet:null,warningRules:'all',compilerPath:'coolerc',lspPath:'cooler-lsp'})[key]}),
   findFiles:()=>new Promise((resolve,reject)=>{finishSearch={resolve,reject};}),asRelativePath:()=> 'main.cl',
   createFileSystemWatcher:()=>({}),onDidChangeConfiguration:()=>({}),onDidChangeWorkspaceFolders:()=>({})},
  window:{showErrorMessage:message=>errors.push(message),createOutputChannel:()=>({show(){},append(){},appendLine(){}})},
  commands:{registerCommand:(name,handler)=>{commands.set(name,handler);return {};}}
 };
 const module={exports:{}};
 vm.runInNewContext(fs.readFileSync(require.resolve('../src/extension.cjs'),'utf8'),{module,setTimeout,clearTimeout,Buffer,require:name=>{
  if(name==='vscode')return vscode;
  if(name==='vscode-languageclient/node')return {LanguageClient:class {async start(){} async stop(){}}};
  if(name==='./debug.cjs')return {registerDebugger(){}};
  if(name==='./source-set.cjs')return require('../src/source-set.cjs');
  if(name==='node:child_process')return {spawn(){spawned++;const child=new EventEmitter();child.stdout=new EventEmitter();child.stderr=new EventEmitter();child.stdout.setEncoding=child.stderr.setEncoding=()=>{};child.kill=()=>{};setImmediate(()=>child.emit('close',0));return child;}};
  return require(name);
 }});
 await module.exports.activate({subscriptions:[]});
 try {
  const first=commands.get('cooler.check')();
  assert.equal(await commands.get('cooler.run')(),null);
  assert.match(errors.at(-1),/already running/);
  assert.equal(spawned,0);
  finishSearch.reject(new Error('file discovery failed'));assert.equal(await first,null);
  const next=commands.get('cooler.run')();finishSearch.resolve([uri]);assert.equal(await next,0);
  const last=commands.get('cooler.check')();finishSearch.resolve([uri]);assert.equal(await last,0);
  assert.equal(spawned,2);
 }finally {await module.exports.deactivate();}
});
