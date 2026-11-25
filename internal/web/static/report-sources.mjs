// SPDX-License-Identifier: AGPL-3.0-or-later
export function reportSources(report, tabs) {
  if (!/^1\.(?:[5-9]|[1-9][0-9]+)\.[0-9]+$/.test(report?.schemaVersion || "")) return {};
  const sources=(report?.sourceUnits || []).map(unit => {
    const tab=tabs.find(item=>item.id===unit.id);
    return tab ? {id:unit.id,name:unit.name,content:tab.source} : null;
  });
  return sources.length && sources.every(Boolean) ? {sources} : {};
}
