// SPDX-License-Identifier: AGPL-3.0-or-later
export class WorkerClient {
  constructor(createWorker, onStatus = () => {}) {
    this.createWorker = createWorker;
    this.onStatus = onStatus;
    this.generation = 0;
    this.nextID = 0;
    this.pending = new Map();
    this.restart();
  }
  stop(reason = "Execution cancelled. Compile again after the runtime restarts.") {
	this.stopped = true;
    this.generation++;
    this.worker?.terminate();
    clearTimeout(this.bootTimer);
    this.rejectBoot?.(new Error(reason));
    for (const pending of this.pending.values()) {
      clearTimeout(pending.timer);
      pending.reject(new Error(reason));
    }
    this.pending.clear();
  }
  restart() {
    this.stop();
    const generation = this.generation;
    this.worker = this.createWorker();
	this.stopped = false;
    this.onStatus({ loading: true });
    this.ready = new Promise((resolve, reject) => { this.resolveBoot = resolve; this.rejectBoot = reject; });
    // Startup failures are surfaced through status even before a request awaits ready.
    this.ready.catch(() => {});
    this.bootTimer = setTimeout(() => this.fail("Runtime startup exceeded 8 seconds. Restart to retry."), 8000);
    this.worker.onerror = () => { if (generation === this.generation) this.fail("Worker failed. Restart to retry."); };
    this.worker.onmessage = ({ data }) => {
      if (generation !== this.generation) return;
      if (data.fatal) { this.fail(data.fatal); return; }
      if (data.ready) {
        clearTimeout(this.bootTimer);
        this.resolveBoot(data);
        this.onStatus(data);
        return;
      }
      const pending = this.pending.get(data.id);
      if (!pending) return;
      clearTimeout(pending.timer);
      this.pending.delete(data.id);
      if (data.error) pending.reject(new Error(data.error));
      else pending.resolve({ ...data, roundTripMs: performance.now() - pending.start });
    };
    return this.ready;
  }
  fail(message) { this.stop(message); this.onStatus({ error: message }); }
  async request(request) {
	if (this.stopped) throw new Error("Runtime stopped. Restart before compiling again.");
    const generation = this.generation;
    await this.ready;
    if (this.stopped || generation !== this.generation) throw new Error("Runtime restarted. Compile again.");
    if (this.pending.size) throw new Error("Wait for the current operation or cancel it.");
    const id = ++this.nextID;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => this.fail("Operation exceeded 5 seconds. Restart and use a smaller program or step budget."), 5000);
      this.pending.set(id, { resolve, reject, timer, start: performance.now() });
      this.worker.postMessage({ ...request, id });
    });
  }
}
