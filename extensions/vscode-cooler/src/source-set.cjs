// SPDX-License-Identifier: AGPL-3.0-or-later
function selected(path,set){
 if(!set)return true;
 const matches=entries=>(entries || []).some(entry=>path===entry || entry.endsWith('/') && path.startsWith(entry));
 return !matches(set.exclude) && (!set.include?.length || matches(set.include));
}
function searchPatterns(set){
 const entries=[...(set?.include || []),...(set?.exclude || [])];
 if(entries.length>256 || entries.some(entry=>typeof entry!=='string' || !entry || entry.startsWith('/') || entry.split('/').some(part=>part==='..' || part==='.') || /[\\:*?\[\]{},]/u.test(entry)))throw new Error('Source set entries must be exact relative files or directory prefixes.');
 const includes=set?.include?.length ? set.include.map(entry=>entry.endsWith('/')?entry+'**/*.{cl,cool}':entry) : ['**/*.{cl,cool}'];
 const excludes=['**/{.git,node_modules,dist,bin,vendor}/**',...(set?.exclude || []).map(entry=>entry.endsWith('/')?entry+'**':entry)];
 return {includes,exclude:excludes.length===1?excludes[0]:'{'+excludes.join(',')+'}'};
}
module.exports={selected,searchPatterns};
