/* SPDX-License-Identifier: AGPL-3.0-or-later */

export function presentBootstrapFailure(error, documentRef = globalThis.document) {
  const detail = error instanceof Error ? error.message : String(error || "unknown error");
  const message = `Could not start Cooler Compiler: ${detail}`;
  if (!documentRef) return message;

  const status = documentRef.getElementById?.("globalStatus");
  if (status) status.textContent = message;

  const failure = documentRef.createElement("div");
  failure.className = "toast error bootstrap-error";
  failure.setAttribute("role", "alert");
  failure.textContent = message;
  const region = documentRef.getElementById?.("toastRegion");
  if (region) region.appendChild(failure);
  else documentRef.body?.prepend(failure);
  return message;
}

export async function bootstrapCoolerApp({
  loadApp = () => import("./app-main.mjs"),
  documentRef = globalThis.document,
} = {}) {
  try {
    const application = await loadApp();
    if (typeof application.startCoolerApp !== "function") {
      throw new TypeError("application module does not export startCoolerApp");
    }
    await application.startCoolerApp();
    return true;
  } catch (error) {
    presentBootstrapFailure(error, documentRef);
    return false;
  }
}
