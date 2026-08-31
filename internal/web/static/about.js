/* SPDX-License-Identifier: AGPL-3.0-or-later */

(function (root, factory) {
  "use strict";

  const api = factory();
  if (typeof module === "object" && module.exports) module.exports = api;
  else root.CoolerAbout = api;
})(typeof globalThis !== "undefined" ? globalThis : this, () => {
  "use strict";

  function createAboutController({ dialog, trigger, closeButton }) {
    if (!dialog || !trigger || !closeButton) {
      throw new Error("About dialog, trigger, and close button are required");
    }

    function isOpen() {
      return Boolean(dialog.open || dialog.hasAttribute?.("open"));
    }

    function open() {
      if (isOpen()) return;
      if (typeof dialog.showModal === "function") dialog.showModal();
      else dialog.setAttribute("open", "");
      closeButton.focus();
    }

    function close() {
      if (!isOpen()) return;
      if (typeof dialog.close === "function") dialog.close();
      else {
        dialog.removeAttribute("open");
        trigger.focus();
      }
    }

    trigger.addEventListener("click", open);
    closeButton.addEventListener("click", close);
    dialog.addEventListener("click", (event) => {
      if (event.target === dialog) close();
    });
    dialog.addEventListener("keydown", (event) => {
      if (event.key !== "Escape" || !isOpen()) return;
      event.preventDefault();
      close();
    });
    dialog.addEventListener("close", () => trigger.focus());

    return { open, close };
  }

  return { createAboutController };
});
