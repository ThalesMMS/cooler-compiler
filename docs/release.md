# Local release verification

Cooler intentionally uses local release verification instead of CI or hosted automation. Run the complete release gate from any directory inside a clean clone with:

```bash
./scripts/install-golangci-lint.sh
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
- the exact `golangci-lint` version pinned in
  [`tools/golangci-lint.version`](../tools/golangci-lint.version), installed with
  `scripts/install-golangci-lint.sh` or supplied through `GOLANGCI_LINT`.

The required gate runs `go mod verify`, `git diff --check`, a read-only `gofmt -l` audit, uncached Go tests, `go vet`, the pinned `golangci-lint`, shell-script syntax checks, JavaScript syntax checks and tests, CGO-disabled CLI/GUI builds for the supported Linux, Windows, and macOS targets, a macOS `.app` bundle build, and two byte-for-byte identical packaged release builds. Tool acquisition is deliberately separate: release verification never downloads a linter or other dependency. The Go suite includes the embedded catalog/resource tests, every valid example's exact output, intentional-error examples, the stack-machine input fixture, multifile CLI/input behavior, schemas, archive safety, manifests, and package digests.

## Optional checks

The same command detects and runs these checks when their platform or tool is available:

- `go test -race ./...` when CGO and a C compiler are enabled;
- native macOS application packaging and Quit integration when running on macOS with `codesign`;
- the native Windows WebView2 cross-build when MinGW-w64's `x86_64-w64-mingw32-gcc` and `g++` are on `PATH`;

Override `WINDOWS_CC` and `WINDOWS_CXX` when the MinGW-w64 compiler names differ.

## Reading the result

Every check ends with one of three markers:

- `[PASS]` — the check completed successfully;
- `[SKIP]` — an optional platform or tool is unavailable;
- `[FAIL]` — the check ran and failed.

Any required failure exits immediately with a nonzero status. An optional check may be skipped, but if it is available and fails, the final status is also nonzero. A successful run ends with `[PASS] release verification completed`.

Gate artifacts are discarded with the temporary directory. `make build-all`
retains raw cross-built binaries. The release builder also creates the macOS
application bundle in every environment: it uses native WKWebView on macOS and
a darwin/arm64 browser-fallback executable elsewhere. From a clean reviewed
commit and an empty `dist/`, produce the actual deterministic release packages
and provenance with:

```bash
make release-artifacts VERSION=1.1.0
```

The four packages, `release-manifest.json`, and `SHA256SUMS` are described by
the versioned [artifact contract](artifact-contract.md). The builder refuses a
dirty tree so its recorded source commit cannot silently misidentify the code.

Cross-building an artifact does not establish that it runs on its target.
Runtime smoke scripts accept the distributed archive, verify it against
`SHA256SUMS`, extract it, and execute its contents on the target platform.
Release packages, checksums, and provenance are described in the
[artifact contract](artifact-contract.md#release-provenance).
