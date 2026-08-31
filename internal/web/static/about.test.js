/* SPDX-License-Identifier: AGPL-3.0-or-later */

"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const { createAboutController } = require("./about.js");

class FakeElement extends EventTarget {
  constructor() {
    super();
    this.focusCount = 0;
  }

  focus() {
    this.focusCount += 1;
  }
}

class FakeDialog extends FakeElement {
  constructor() {
    super();
    this.open = false;
  }

  showModal() {
    this.open = true;
  }

  close() {
    this.open = false;
    this.dispatchEvent(new Event("close"));
  }
}

test("Escape closes the About dialog and restores focus to its trigger", () => {
  const dialog = new FakeDialog();
  const trigger = new FakeElement();
  const closeButton = new FakeElement();
  createAboutController({ dialog, trigger, closeButton });

  trigger.dispatchEvent(new Event("click"));
  assert.equal(dialog.open, true);
  assert.equal(closeButton.focusCount, 1);

  const escape = new Event("keydown", { cancelable: true });
  Object.defineProperty(escape, "key", { value: "Escape" });
  dialog.dispatchEvent(escape);

  assert.equal(escape.defaultPrevented, true);
  assert.equal(dialog.open, false);
  assert.equal(trigger.focusCount, 1);
});
