/* SPDX-License-Identifier: AGPL-3.0-or-later */

(() => {
  "use strict";

  const capabilityMeta = document.querySelector('meta[name="cooler-capability"]');
  const capability = capabilityMeta?.content || "";
  capabilityMeta?.remove();
  Object.defineProperty(window, "CoolerCapability", {
    value: capability,
    configurable: false,
    enumerable: false,
    writable: false,
  });

  function presentEntrypointFailure(error) {
    const detail = error instanceof Error ? error.message : String(error || "unknown error");
    const message = `Could not load Cooler Compiler Lab: ${detail}`;
    const status = document.getElementById("globalStatus");
    if (status) status.textContent = message;
    const failure = document.createElement("div");
    failure.className = "toast error bootstrap-error";
    failure.setAttribute("role", "alert");
    failure.textContent = message;
    const region = document.getElementById("toastRegion");
    if (region) region.appendChild(failure);
    else document.body.prepend(failure);
  }

  if (!capability) {
    presentEntrypointFailure(new Error("missing process capability; reopen the application from the current Cooler process"));
    return;
  }

  import("./app-bootstrap.mjs").then(({ bootstrapCoolerApp }) => bootstrapCoolerApp()).catch((error) => {
    console.error("Could not load Cooler GUI app:", error);
    presentEntrypointFailure(error);
  });
})();
