# v1.1.0 platform support

The release packages provide the targets and application modes listed below.
Successful cross-building confirms that the package was produced; it does not
establish runtime behavior on the target platform. The release manifest records
the build host and any packaging checks skipped there.

| Target | Package | Included modes |
| --- | --- | --- |
| Linux amd64 | `cooler-compiler-v1.1.0-linux-amd64.tar.gz` | CLI, LSP, browser GUI |
| Linux arm64 | `cooler-compiler-v1.1.0-linux-arm64.tar.gz` | CLI, LSP, browser GUI |
| Windows amd64 | `cooler-compiler-v1.1.0-windows-amd64.zip` | CLI, LSP, browser GUI; native WebView2 GUI when included in the manifest |
| macOS arm64 | `cooler-compiler-v1.1.0-macos-arm64.zip` | CLI, LSP, browser GUI, macOS app bundle |

Verify downloaded archives with `SHA256SUMS` before extraction. See the
[release guide](release.md) for package creation and platform runtime checks.
