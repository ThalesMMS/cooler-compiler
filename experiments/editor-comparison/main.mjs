// SPDX-License-Identifier: AGPL-3.0-or-later
import {EditorState,EditorSelection} from '@codemirror/state';
import {EditorView,keymap} from '@codemirror/view';
import {history,historyKeymap,undo,redo} from '@codemirror/commands';
import {foldEffect,foldedRanges,foldGutter} from '@codemirror/language';
import {setDiagnostics,nextDiagnostic} from '@codemirror/lint';
const assert=(v,m)=>{if(!v)throw Error(m)};
try {
const source='class Main {\n  main(): Int { 0 };\n};\n'+'-- large source 😀\n'.repeat(5000);
const before=performance.memory?.usedJSHeapSize;const start=performance.now();
const view=new EditorView({state:EditorState.create({doc:source,extensions:[history(),keymap.of(historyKeymap),foldGutter(),EditorState.allowMultipleSelections.of(true)]}),parent:document.querySelector('#cm')});
const create=performance.now()-start;const editStart=performance.now();
for(let i=0;i<50;i++)view.dispatch({changes:{from:source.length,insert:'x'}});
const edit=performance.now()-editStart;
view.dispatch({effects:foldEffect.of({from:12,to:32})});assert(foldedRanges(view.state).size===1,'fold');
view.dispatch(setDiagnostics(view.state,[{from:source.length-5,to:source.length-3,severity:'warning',message:'Fixture diagnostic'}]));assert(nextDiagnostic(view),'diagnostic navigation');
view.dispatch({selection:EditorSelection.create([EditorSelection.cursor(0),EditorSelection.cursor(10)])});
view.dispatch(view.state.replaceSelection('X'));assert(view.state.doc.sliceString(0,1)==='X','multiple edit');assert(undo(view)&&redo(view),'undo redo');
const cmHeap=performance.memory?.usedJSHeapSize;
const textarea=document.querySelector('textarea');const t0=performance.now();textarea.value=source;const textCreate=performance.now()-t0;
const t1=performance.now();for(let i=0;i<50;i++)textarea.setRangeText('x',source.length,source.length,'end');const textEdit=performance.now()-t1;
document.querySelector('#result').textContent=JSON.stringify({pass:true,sourceUTF16:source.length,cmCreateMs:create,cm50EditsMs:edit,textareaCreateMs:textCreate,textarea50EditsMs:textEdit,heapBefore:before,heapAfterCM:cmHeap,heapAfterBoth:performance.memory?.usedJSHeapSize,folding:true,multipleEdit:true,diagnosticNavigation:true,undoRedo:true});

} catch(error) { document.querySelector('#result').textContent=JSON.stringify({pass:false,error:error.message}); }
