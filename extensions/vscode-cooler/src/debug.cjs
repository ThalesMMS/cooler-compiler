// SPDX-License-Identifier: AGPL-3.0-or-later
const vscode=require('vscode');
const path=require('node:path');
const {selected,searchPatterns}=require('./source-set.cjs');
function registerDebugger(context){
 context.subscriptions.push(vscode.debug.registerDebugAdapterDescriptorFactory('cooler',{
  createDebugAdapterDescriptor(session){
   if(!vscode.workspace.isTrusted)throw new Error('Trust the workspace before debugging.');
   return new vscode.DebugAdapterExecutable(vscode.workspace.getConfiguration('cooler',session.workspaceFolder?.uri).get('dapPath'),[],{cwd:session.workspaceFolder?.uri.fsPath});
  }
 }));
 context.subscriptions.push(vscode.debug.registerDebugConfigurationProvider('cooler',{
  async resolveDebugConfigurationWithSubstitutedVariables(folder,config){
   if(!vscode.workspace.isTrusted||!folder||folder.uri.scheme!=='file')throw new Error('Open a trusted local workspace before debugging.');
   let sources=config.sources;
   if(!sources && config.program)sources=[config.program];
   if(!sources){
    const set=vscode.workspace.getConfiguration('cooler',folder.uri).get('sourceSet'),{includes,exclude}=searchPatterns(set);const found=new Map();
    for(const pattern of includes){for(const uri of await vscode.workspace.findFiles(new vscode.RelativePattern(folder,pattern),exclude,65)){const relative=path.relative(folder.uri.fsPath,uri.fsPath).replaceAll('\\','/');if(selected(relative,set))found.set(uri.fsPath,uri.fsPath);if(found.size>64)throw new Error('Debug projects support at most 64 selected files.');}}
    sources=[...found.values()].sort();
   }
   if(!Array.isArray(sources)||!sources.length||sources.length>64||sources.some(item=>typeof item!=='string'||!item))throw new Error('Debug launch requires 1–64 source paths.');
   sources=sources.map(file=>path.resolve(folder.uri.fsPath,file));const uris=new Set(sources.map(file=>vscode.Uri.file(file).toString()));
   if(vscode.workspace.textDocuments.some(doc=>doc.isDirty&&uris.has(doc.uri.toString())))throw new Error('Save modified COOL sources before debugging.');
   return {...config,cwd:folder.uri.fsPath,sources};
  }
 }));
}
module.exports={registerDebugger};
