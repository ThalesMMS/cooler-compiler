// SPDX-License-Identifier: AGPL-3.0-or-later
const vscode=require('vscode');const assert=require('node:assert/strict');const path=require('node:path');
async function until(check){const end=Date.now()+10000;while(Date.now()<end){const value=check();if(value)return value;await new Promise(r=>setTimeout(r,30));}throw Error('DAP event timed out');}
exports.runDebug=async()=>{
 const messages=[],errors=[];const tracker=vscode.debug.registerDebugAdapterTrackerFactory('cooler',{createDebugAdapterTracker(){return {onDidSendMessage:message=>messages.push(message),onError:error=>errors.push({message:error.message,afterDisconnect:messages.some(m=>m.command==="disconnect"&&m.success)})}}});
 const root=process.env.COOLER_TEST_WORKSPACE,helper=path.join(root,'helper-debug.cl'),main=path.join(root,'main-debug.cl');
 const bp=new vscode.SourceBreakpoint(new vscode.Location(vscode.Uri.file(helper),new vscode.Position(1,0)));vscode.debug.addBreakpoints([bp]);
 let stopCount=0;const stop=async()=>{const event=await until(()=>messages.filter(m=>m.event==='stopped')[stopCount]);stopCount++;return event.body;};
 try{
 assert(await vscode.debug.startDebugging(vscode.workspace.workspaceFolders[0],{type:'cooler',request:'launch',name:'Cooler DAP validation',sources:[main,helper],stopOnEntry:true}));
 const session=await until(()=>vscode.debug.activeDebugSession);assert.equal((await stop()).reason,'entry');
 await session.customRequest('continue',{threadId:1});assert.equal((await stop()).reason,'breakpoint');
 const stack=await session.customRequest('stackTrace',{threadId:1});assert(stack.stackFrames[0].source.path.toLowerCase()===helper.toLowerCase());
 const scopes=await session.customRequest('scopes',{frameId:stack.stackFrames[0].id});const variables=await session.customRequest('variables',{variablesReference:scopes.scopes[0].variablesReference});assert(variables.variables.some(v=>v.name==='n'&&v.value==='7'));
 await session.customRequest('stepIn',{threadId:1});await stop();await session.customRequest('stepOut',{threadId:1});await stop();
 vscode.debug.removeBreakpoints([bp]);await session.customRequest('setBreakpoints',{source:{path:helper},breakpoints:[]});
 await session.customRequest('continue',{threadId:1});assert.match((await stop()).text,/Input required/);
 await session.customRequest('evaluate',{expression:':input 42',context:'repl'});await stop();await session.customRequest('continue',{threadId:1});await until(()=>messages.some(m=>m.event==='terminated'));
 const output=messages.filter(m=>m.event==='output'&&m.body.category==='stdout').map(m=>m.body.output).join('');assert.equal(output,'842');
 await until(()=>!vscode.debug.activeDebugSession);
 // VS Code 1.136.1 fires a literal read error on stdout close, including its own normal teardown.
 assert(errors.every(e=>e.message==='read error'&&e.afterDisconnect),'unexpected adapter transport errors: '+JSON.stringify(errors));
 const before=messages.filter(m=>m.event==='terminated').length;
 assert(await vscode.debug.startDebugging(vscode.workspace.workspaceFolders[0],{type:'cooler',request:'launch',name:'Selected source set',stopOnEntry:false}));
 await until(()=>messages.filter(m=>m.event==='terminated').length>before);
 await until(()=>!vscode.debug.activeDebugSession);
 stopCount=messages.filter(m=>m.event==='stopped').length;
 assert(await vscode.debug.startDebugging(vscode.workspace.workspaceFolders[0],{type:'cooler',request:'launch',name:'Pause/resume validation',program:path.join(root,'loop-debug.cl'),stopOnEntry:true}));
 await stop();const loopSession=vscode.debug.activeDebugSession;
 for(let i=0;i<2;i++){await loopSession.customRequest('continue',{threadId:1});await loopSession.customRequest('pause',{threadId:1});assert.equal((await stop()).reason,'pause');}
 assert((await loopSession.customRequest('stackTrace',{threadId:1})).stackFrames.length>0);await vscode.debug.stopDebugging(loopSession);
 console.log(`VS Code ${vscode.version}: real DAP launch, multifile breakpoint, frames/n=7, stepIn/stepOut, live input, output 842, termination passed.`);
 }finally{vscode.debug.removeBreakpoints([bp]);await vscode.debug.stopDebugging();tracker.dispose();}
};
