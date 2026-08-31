/* SPDX-License-Identifier: AGPL-3.0-or-later */

(() => {
  "use strict";
  async function loadCoolerApp() {
    const parts = await Promise.all([
      "/app-a.js",
      "/app-b.js",
      "/app-c.js",
    ].map(async (path) => {
      const response = await fetch(path);
      if (!response.ok) {
        throw new Error(`${path} ${response.status} ${response.statusText}`);
      }
      return response.text();
    }));
    const script = document.createElement("script");
    script.textContent = parts.join("\n");
    document.head.appendChild(script);
  }
  loadCoolerApp().catch((error) => {
    console.error("Could not load Cooler GUI app:", error);
  });
})();
