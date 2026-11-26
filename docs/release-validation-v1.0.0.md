# v1.0.0 platform support

This table describes the platforms and application modes offered with the
v1.0.0 release. Cross-built packages are available for the listed targets;
runtime execution was confirmed only where stated.

| Target | Package | Included modes | Runtime status |
| --- | --- | --- | --- |
| Linux amd64 | `cooler-compiler-v1.0.0-linux-amd64.tar.gz` | CLI, LSP, browser GUI | Cross-built; target runtime not claimed |
| Linux arm64 | `cooler-compiler-v1.0.0-linux-arm64.tar.gz` | CLI, LSP, browser GUI | Cross-built; target runtime not claimed |
| Windows amd64 | `cooler-compiler-v1.0.0-windows-amd64.zip` | CLI, LSP, browser GUI, native WebView2 shell | Packaged CLI, LSP, browser GUI, and native shell exercised |
| macOS arm64 | `cooler-compiler-v1.0.0-macos-arm64.zip` | CLI, LSP, browser GUI, app bundle | Cross-built; native runtime, signing, notarization, and Gatekeeper not claimed |

The packages are checksum-verifiable with the accompanying `SHA256SUMS` file.
The release manifest identifies the source commit, toolchain, build flags, and
package digests. Use the latest release page for currently available assets.
