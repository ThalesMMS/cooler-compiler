# Local release verification

Cooler intentionally uses local release verification instead of CI or hosted automation. Run the complete release gate from any directory inside a clean clone with:

```bash
make release-check
```

The command resolves the repository root itself, writes build products to a temporary directory, and removes them when it finishes. It does not format or otherwise rewrite tracked files.

## Prerequisites

Required tools:

- Go 1.25 or a compatible newer toolchain;
- Git;
- GNU or BSD Make;
- Bash;
- Node.js 20 or newer, including the built-in `node:test` runner.

The required gate runs `go mod verify`, `git diff --check`, a read-only `gofmt -l` audit, uncached Go tests, `go vet`, JavaScript syntax checks and tests, and CGO-disabled CLI/GUI builds for the supported Linux, Windows, and macOS targets. The Go suite includes the embedded catalog/resource tests, every valid example's exact output, intentional-error examples, the stack-machine input fixture, and multifile CLI/input behavior.

## Optional checks

The same command detects and runs these checks when their platform or tool is available:

- `go test -race ./...` when CGO and a C compiler are enabled;
- native macOS application packaging when running on macOS with `codesign`;
- the native Windows WebView2 cross-build when MinGW-w64's `x86_64-w64-mingw32-gcc` and `g++` are on `PATH`;
- `golangci-lint run ./...` when `golangci-lint` is installed.

Override `WINDOWS_CC` and `WINDOWS_CXX` when the MinGW-w64 compiler names differ.

## Reading the result

Every check ends with one of three markers:

- `[PASS]` — the check completed successfully;
- `[SKIP]` — an optional platform or tool is unavailable;
- `[FAIL]` — the check ran and failed.

Any required failure exits immediately with a nonzero status. An optional check may be skipped, but if it is available and fails, the final status is also nonzero. A successful run ends with `[PASS] release verification completed`.

The fallback release artifacts are built only to prove reproducibility and are discarded with the temporary directory. Use `make build-all` when you want to retain them in `dist/`.

Release notes should link to the README's canonical [Scope and compatibility](../README.md#scope-and-compatibility) section so “complete within Cooler v0.1's educational scope” is not confused with drop-in reference-toolchain compatibility.
