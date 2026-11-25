// SPDX-License-Identifier: AGPL-3.0-or-later
export function indentEdit(source,start,end,outdent=false){
 const from=(start===0?0:source.lastIndexOf('\n',start-1)+1);
 const to=end>start && source[end-1]==='\n'?end:((source.indexOf('\n',end)+1)||source.length);
 const lines=source.slice(from,to).split('\n');let delta=0,firstDelta=0;
 const text=lines.map((line,index)=>{if(index===lines.length-1 && !line && to>from && source[to-1]==='\n')return line;const remove=outdent?(line.match(/^(?: {1,2}|\t)/)?.[0].length||0):0;const change=outdent?-remove:2;if(index===0)firstDelta=change;delta+=change;return outdent?line.slice(remove):'  '+line;}).join('\n');
 return {from,to,text,start:Math.max(from,start+firstDelta),end:Math.max(from,end+delta)};
}
export function applyIndent(editor,outdent=false){
 if(editor.readOnly)return false;
 const edit=indentEdit(editor.value,editor.selectionStart,editor.selectionEnd,outdent);const before=editor.value;
 editor.coolerSelection={before:{start:editor.selectionStart,end:editor.selectionEnd},after:{start:edit.start,end:edit.end}};
 editor.focus({preventScroll:true});editor.setSelectionRange(edit.from,edit.to);
 // Chromium's editing command retains native undo. The fallback stays functional
 // on WebViews without this legacy editing API, but cannot promise native undo.
 if(!document.execCommand('insertText',false,edit.text)){
  editor.setRangeText(edit.text,edit.from,edit.to,'end');editor.dispatchEvent(new Event('input',{bubbles:true}));
 }
 editor.setSelectionRange(edit.start,edit.end);delete editor.coolerSelection;return editor.value!==before;
}
export function mapBreakpointLine(before,after,line){
 const old=before.split('\n'),next=after.split('\n');let prefix=0,suffix=0;
 while(prefix<old.length && prefix<next.length && old[prefix]===next[prefix])prefix++;
 while(suffix<old.length-prefix && suffix<next.length-prefix && old[old.length-1-suffix]===next[next.length-1-suffix])suffix++;
 if(line<=prefix)return line;
 if(line>old.length-suffix)return line+next.length-old.length;
 // A same-height replacement keeps explicit line intent. Removed/ambiguous
 // lines lose their breakpoint instead of silently targeting unrelated code.
 return old.length===next.length?line:null;
}
export function nextDiagnosticIndex(length,current,direction){return length?(current<0?(direction>0?0:length-1):(current+direction+length)%length):-1;}
export function composing(event){return event.isComposing || event.keyCode===229;}

// A shared budget bounds history across all source tabs; entries stay keyed by
// source identity so undo cannot restore another file into the active tab.
export class EditorHistory {
 constructor(limit=4*1024*1024){this.limit=limit;this.past=[];this.future=[];}
 record(key,before,after){if(before.text===after.text)return;this.future=this.future.filter(e=>e.key!==key);this.past.push({key,before,after});while(this.past.length+this.future.length>100 || this.bytes()>this.limit){if(this.future.length){this.future.shift();}else this.past.shift();}}
 bytes(){return [...this.past,...this.future].reduce((n,e)=>n+2*(e.before.text.length+e.after.text.length),0);}
 move(key,redo=false){const from=redo?this.future:this.past,to=redo?this.past:this.future;const index=from.findLastIndex(e=>e.key===key);if(index<0)return null;const [entry]=from.splice(index,1);to.push(entry);return redo?entry.after:entry.before;}
 forget(key){this.past=this.past.filter(e=>e.key!==key);this.future=this.future.filter(e=>e.key!==key);}
}

export function breakpointLineForSource(key,id){const text=String(key),separator=text.lastIndexOf(":");if(text.slice(0,separator)!==id)return null;const line=Number(text.slice(separator+1));return Number.isSafeInteger(line)&&line>0?line:null;}
