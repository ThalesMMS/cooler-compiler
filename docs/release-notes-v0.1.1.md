# Cooler Compiler Lab v0.1.1 release notes (draft)

The final release notes must copy the verified statuses from
`release-validation-v0.1.1.md`; build success is not runtime evidence.

Current pre-release status:

- complete reports use schema v1 across CLI, GUI, and API;
- GUI report ZIPs carry digest-verified deterministic manifests;
- local release packages carry commit/tool/target provenance and are
  reproducible byte for byte on the same source and toolchain;

- Windows amd64 fallback: runtime-tested on Windows 11 Pro 25H2 in a documented
  QEMU full-system x86_64 VM; CLI, browser GUI, debugger actions, and exports
  passed for the exact checksummed package;
- Windows amd64 native WebView2: source-build-only and not tested; the v0.1.1
  package distributes the CGO-disabled browser fallback, and the optional
  MinGW-w64/WebView2 build was unavailable;
- Linux amd64 fallback: runtime-tested on Ubuntu 24.04.4 LTS in a documented
  QEMU full-system x86_64 VM; CLI, browser GUI, debugger actions, exports, and
  shared-library check passed for the exact checksummed package;
- Linux arm64 fallback: runtime-tested on Ubuntu 24.04.4 LTS in a documented
  `vz` full-system arm64 VM; CLI, browser GUI, debugger actions, exports, and
  shared-library check passed for the exact checksummed package;
- macOS arm64 fallback and WKWebView: runtime-tested on macOS 26.6.2 arm64 for
  the exact checksummed package; CLI, browser debugger and saved exports,
  native WKWebView controls, ad-hoc signature verification, and Command-Q
  passed. Native WKWebView displays exported JSON in-place; saved JSON/IR/ZIP
  downloads use the included browser fallback;
- macOS amd64: unsupported; no Intel or universal package is distributed.

Do not publish v0.1.1 with stronger platform claims until every distributed
package row has an exact checksum and target-runtime verdict. This project uses
local/manual validation and intentionally has no hosted CI matrix.
