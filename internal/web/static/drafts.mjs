/* SPDX-License-Identifier: AGPL-3.0-or-later */
export function draftWorkspace(tabs, active) {
  return { version: 1, active, tabs: tabs.map(tab => ({ name: tab.name, content: tab.source })) };
}

export function createDraftWriter({ save, status, delay = 300 }) {
  let timer = null, running = false, pending = null;
  let revision = 0, enabled = false;
  async function flush() {
    if (timer !== null) clearTimeout(timer);
    timer = null;
    if (running || !enabled || !pending) return;
    running = true;
    const workspace = pending;
    pending = null;
    try {
      const result = await save({ action: 'save', revision, workspace });
      if (result?.revision == null) throw new Error('Recovery save returned no revision. Reload before saving recovery copies again.');
      revision = result.revision;
      status(pending ? 'Saving recovery copy…' : 'Recovery copy saved', false);
    } catch (error) {
      enabled = false;
      pending = null;
      status(error.message, true);
    } finally {
      running = false;
      if (pending && enabled) timer = setTimeout(flush, 0);
    }
  }
  return {
    start(value) { revision = value; enabled = true; },
    pause() { enabled = false; pending = null; if (timer !== null) clearTimeout(timer); timer = null; },
    revision() { return revision; },
    active() { return running; },
    schedule(workspace) {
      if (!enabled) return false;
      pending = workspace;
      if (timer !== null) clearTimeout(timer);
      timer = setTimeout(flush, delay);
      status('Saving recovery copy…', false);
      return true;
    },
    flush,
  };
}
