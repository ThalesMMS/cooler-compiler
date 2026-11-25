/* SPDX-License-Identifier: AGPL-3.0-or-later */

// One request in flight, one latest destination retained. Suppress intermediate
// responses even when they share the same timeline revision.
export function createHistorySeeker({ request, apply, fail, busy, delay = 80 }) {
  let pending = null;
  let running = false;
  let timer = null;
  let sequence = 0;
  async function flush() {
    timer = null;
    const item = pending;
    pending = null;
    running = true;
    try {
      const result = await request(item.value);
      if (item.sequence === sequence) apply(result, item.value);
    } catch (error) {
      if (item.sequence === sequence) fail(error, item.value);
    } finally {
      running = false;
      if (pending) timer = setTimeout(flush, delay);
      else busy(false);
    }
  }
  return (value) => {
    pending = { value, sequence: ++sequence };
    if (running) return;
    if (timer !== null) clearTimeout(timer);
    else busy(true);
    timer = setTimeout(flush, delay);
  };
}
