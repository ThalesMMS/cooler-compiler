# v1.1.0 platform support

The release packages provide the targets, application modes, and runtime
coverage listed below. Cross-building confirms that a package was produced; it
does not establish runtime behavior on the target platform. The release
manifest records the build host and packaging checks skipped there.

| Target | Package | Included modes | Runtime coverage |
| --- | --- | --- |
| Linux amd64 | `cooler-compiler-v1.1.0-linux-amd64.tar.gz` | CLI, LSP, browser GUI | Cross-built and checksum-qualified; target runtime not claimed |
| Linux arm64 | `cooler-compiler-v1.1.0-linux-arm64.tar.gz` | CLI, LSP, browser GUI | Cross-built and checksum-qualified; target runtime not claimed |
| Windows amd64 | `cooler-compiler-v1.1.0-windows-amd64.zip` | CLI, LSP, browser GUI | Cross-built and checksum-qualified; native WebView2 build skipped because MinGW-w64 was unavailable; target runtime not claimed |
| macOS arm64 | `cooler-compiler-v1.1.0-macos-arm64.zip` | CLI, LSP, browser GUI, native WKWebView app bundle | Archive checksum, CLI success/error paths, multifile input, artifacts, LSP version, browser GUI health/security headers, and app launch/quit passed on macOS arm64 |

Verify downloaded archives with `SHA256SUMS` before extraction. See the
[release guide](release.md) for package creation and platform runtime checks.
The macOS app bundle is ad hoc signed; notarization and Gatekeeper behavior are
not claimed.
