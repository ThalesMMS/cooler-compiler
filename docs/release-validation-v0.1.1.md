# v0.1.1 target-runtime validation record

This document is the authoritative manual evidence ledger for v0.1.1. A build
is not called runtime-tested until the exact packaged checksum was verified and
executed on the named target. Cross-build success is recorded only as `built`.

**Release-set verdict: pending.** The evidence below was collected against
successive candidate commits and qualifies only the exact package/checksum named
in each target section. It does not qualify a single publishable v0.1.1 artifact
set. Before publication, all archives must be rebuilt from one tagged commit,
`SHA256SUMS` regenerated, and the exact packages rerun on every claimed target;
the candidate-specific entries below must then be replaced by that unified
evidence.

## Status vocabulary

- `built`: produced by the local release gate, not executed on the target;
- `runtime-tested`: checksum verified and automated smoke passed on the target;
- `native GUI tested`: native WKWebView/WebView2 package exercised manually;
- `source-build-only`: supported from source but no binary is distributed;
- `pending`: required target evidence has not been collected.

## Candidate package matrix

| Target | Package mode | Status | Evidence |
| --- | --- | --- | --- |
| Windows 11 amd64 | browser fallback | runtime-tested; browser GUI tested | Windows 11 Pro 25H2 full-system VM under QEMU x86_64 emulation; exact package smoke, debugger controls, and exports passed. See [Windows 11 amd64 evidence](#windows-11-amd64-evidence--pass). |
| Windows 11 amd64 | native WebView2 | source-build-only; not tested | The v0.1.1 package distributes the CGO-disabled browser fallback. The optional MinGW-w64/WebView2 build was unavailable and is not represented by the tested package. |
| Linux amd64 | browser fallback | runtime-tested; browser GUI tested | Ubuntu 24.04.4 LTS full-system VM under QEMU x86_64 emulation; exact package smoke, debugger controls, and exports passed. See [Linux amd64 evidence](#linux-amd64-evidence--pass). |
| Linux arm64 | browser fallback | runtime-tested; browser GUI tested | Ubuntu 24.04.4 LTS full-system VM on Apple Virtualization (`vz`); exact package smoke, debugger controls, and exports passed. See [Linux arm64 evidence](#linux-arm64-evidence--pass). |
| macOS arm64 | browser fallback | runtime-tested; browser GUI tested | macOS 26.6.2 on Apple M4 Pro; exact package smoke, debugger controls, and saved exports passed. See [macOS arm64 evidence](#macos-arm64-evidence--pass). |
| macOS arm64 | native WKWebView | runtime-tested; native GUI tested | The exact packaged app passed signature verification, launch, Compile/Step/Reset/Run, JSON viewing, and Command-Q. Saved artifact downloads use the packaged browser fallback. See [macOS arm64 evidence](#macos-arm64-evidence--pass). |
| macOS amd64 | none | unsupported | v0.1.1 does not distribute an Intel or universal macOS binary; source builds are not promised. |

Current host capability audit: macOS 26.6.2 (25G83), arm64, Apple M4 Pro.
Docker, Podman, Lima, Wine, and Multipass were not installed. A verified
portable Lima v2.2.0 distribution was therefore extracted under `/tmp`. It used
Apple's built-in `vz` driver for the isolated arm64 VM and Homebrew QEMU 11.1.1
for the isolated amd64 VM documented below. Hosted CI/cloud execution remains
intentionally out of scope.

## Windows 11 amd64 evidence — PASS

- **Candidate:** v0.1.1, commit
  `b4a146e8adf8b8560458f41b608d8af0b3b32830`.
- **Target:** Microsoft Windows 11 Pro 25H2, version `10.0.26200`, build
  `26200`, `amd64`; Windows PowerShell `5.1.26100.7920`.
- **Environment:** fresh full-system VM using Lima v2.2.0 and QEMU 11.1.1 TCG
  x86_64 emulation on an Apple M4 Pro; 4 logical processors, 8 GiB RAM, a
  64 GiB virtual disk, UEFI, and an emulated TPM provided by swtpm 0.10.2. The
  guest was installed from Microsoft's Windows 11 25H2 English x64 ISO,
  digest
  `768984706b909479417b2368438909440f2967ff05c6a9195ed2667254e465e3`,
  with virtio-win 0.1.271 installation media, digest
  `bbe6166ad86a490caefad438fef8aa494926cb0a1b37fa1212925cfd81656429`.
- **Package:** `cooler-compiler-v0.1.1-windows-amd64.zip`, SHA-256
  `52d1c1e7e95181074e95a82ed097170997ce37bcbfc64f1494e70449baf5d83d`.
  `runtime-smoke.ps1` verified it against the adjacent `SHA256SUMS` before
  extraction or execution.
- **Automated scenarios:** CLI version; successful execution; semantic and
  runtime errors; ordered multifile compilation; explicit program input;
  tokens, AST, symbols, CFG, IR, assembly, and report emission; browser GUI
  health, CSP, and application HTML. All passed. The first run exposed that
  Windows PowerShell 5.1 promotes expected native stderr to
  `NativeCommandError` under the script's `Stop` preference; the harness now
  scopes those two intentional failures to `Continue`, then asserts their exit
  codes and diagnostics explicitly. The amended harness passed the complete
  exact-package run.
- **Manual browser scenarios:** the packaged Windows server ran inside the VM
  and was reached through SSH local port forwarding. The page identified
  Cooler Compiler 0.1.1 and rendered without an overlay or console log.
  Compile completed all six phases; Step advanced from zero to one instruction;
  Reset returned to zero; Run halted after 19 instructions and printed
  `Hello, COOL!`.
- **Export inspection:** JSON report, IR text, and ZIP report downloads passed.
  JSON identified schema `1.0.0`, compiler `0.1.1`, the candidate commit, target
  `windows/amd64`, zero diagnostics, a halted VM, and `HALT` as the last
  operation. IR contained the expected string push, `out_string` call, and
  halt. The ZIP passed integrity testing, identified the same commit, and every
  byte size and SHA-256 matched its 11-entry manifest.
- **Verdict:** PASS (`runtime-tested`; browser GUI tested). The distributed
  `coolergui.exe` is the CGO-disabled browser fallback. The optional native
  WebView2 build requires a MinGW-w64 cross-toolchain, was not built into this
  release package, and therefore was neither advertised nor tested as native.
- **Tester/date:** local issue workflow on the ThalesMMS host, 2026-08-31 UTC.

## Linux amd64 evidence — PASS

- **Candidate:** v0.1.1, commit
  `0bf75fe317862d477682f4813d5992a0c701f772`.
- **Target:** Ubuntu 24.04.4 LTS (Noble), Linux
  `6.8.0-134-generic`, `x86_64`.
- **Environment:** fresh full-system VM using Lima v2.2.0 and QEMU 11.1.1 TCG
  x86_64 emulation on an Apple M4 Pro; 4 vCPUs, 4 GiB RAM, and a 12 GiB virtual
  disk. The VM had no host-directory mounts or container runtime and used
  Ubuntu's `ubuntu-24.04-server-cloudimg-amd64.img`, image digest
  `ffe6203da54deeb6db5d2a98a83f9ec8e55f149d3f7ba622e1abe5fa966ee3d6`.
  The portable Lima archive was verified as
  `bbdef91774885a0d05f7b048c4eb89ae2bcf3a0c252ae7ca7934e63df76d93c3`;
  its additional x86_64 guest-agent archive was verified as
  `3aff4453eb3c359eb4f3b458056db24f2c5c15019232531292f49e04050554ed`.
- **Package:** `cooler-compiler-v0.1.1-linux-amd64.tar.gz`, SHA-256
  `6fb7837588aedce16d1033262e10803fd76d2d68b45c4011c74cff33a940d93d`.
  `runtime-smoke.sh` verified it against the adjacent `SHA256SUMS` before
  extraction or execution.
- **Automated scenarios:** CLI version; successful execution; semantic and
  runtime errors; ordered multifile compilation; explicit program input;
  tokens, AST, symbols, CFG, IR, assembly, and report emission; browser GUI
  health, CSP, and application HTML. All passed. `ldd` reported the fallback
  `coolerc` binary as having no dynamic shared-library dependency.
- **Manual browser scenarios:** the packaged Linux server ran inside the VM
  and was reached through Lima's local port forwarding. Compile completed all
  six phases; Step advanced from zero to one instruction; Reset returned to
  zero; Run halted after 19 instructions and printed `Hello, COOL!`. Page
  identity, meaningful rendered content, absence of framework overlays, and
  browser console health all passed.
- **Export inspection:** JSON report, IR text, and ZIP report downloads passed.
  JSON identified compiler `0.1.1`, the candidate commit, and target
  `linux/amd64`. IR contained the expected string push, `out_string` call, and
  halt. The ZIP passed integrity testing, identified the same commit, and every
  byte size and SHA-256 matched its 11-entry manifest.
- **Verdict:** PASS (`runtime-tested`; browser GUI tested). This is a clean
  amd64 Linux kernel/userspace under documented full-system CPU emulation.
  There is no native Linux desktop package to test; the distributed GUI mode is
  browser-hosted.
- **Tester/date:** local issue workflow on the ThalesMMS host, 2026-08-31 UTC.

## Linux arm64 evidence — PASS

- **Candidate:** v0.1.1, commit
  `316daf48ed53840bf1105ee555cb4bc688c19348`.
- **Target:** Ubuntu 24.04.4 LTS (Noble), Linux
  `6.8.0-134-generic`, `aarch64`.
- **Environment:** full-system VM using Lima v2.2.0 and Apple Virtualization
  (`vz`) on an Apple M4 Pro; 4 vCPUs, 4 GiB RAM, and a 12 GiB virtual disk.
  The VM was created fresh without host-directory mounts from Ubuntu's
  `ubuntu-24.04-server-cloudimg-arm64.img`, image digest
  `7df0201546f75b8bcc1044594c806c35749421ad3c9bc1be2a3ab806cfae39cc`.
  The portable Lima archive was verified as
  `bbdef91774885a0d05f7b048c4eb89ae2bcf3a0c252ae7ca7934e63df76d93c3`.
- **Package:** `cooler-compiler-v0.1.1-linux-arm64.tar.gz`, SHA-256
  `a83b012a3d6acf1e9448ec2cb5fdf3d9114d5bc3bb3b79fd8939cee652d4ac4d`.
  `runtime-smoke.sh` verified it against the adjacent `SHA256SUMS` before
  extraction or execution.
- **Automated scenarios:** CLI version; successful execution; semantic and
  runtime errors; ordered multifile compilation; explicit program input;
  tokens, AST, symbols, CFG, IR, assembly, and report emission; browser GUI
  health, CSP, and application HTML. All passed. `ldd` reported the fallback
  `coolerc` binary as having no dynamic shared-library dependency.
- **Manual browser scenarios:** the packaged Linux server was run inside the
  VM and reached through Lima's local port forwarding. Compile, Step (one
  instruction), Reset (zero instructions), and Run all passed; Run halted after
  printing `Hello, COOL!`. JSON report, IR text, and ZIP report downloads opened
  successfully with no browser console warning or error.
- **Export inspection:** JSON identified schema `1.0.0`, compiler `0.1.1`, the
  candidate commit, target `linux/arm64`, and a halted VM. IR contained the
  expected string push, `out_string` call, and halt. The ZIP passed integrity
  testing, identified the same commit, and every byte size and SHA-256 matched
  its 11-entry manifest.
- **Verdict:** PASS (`runtime-tested`; browser GUI tested). This is a real arm64
  Linux kernel/userspace in a documented full-system VM. There is no native
  Linux desktop package to test; the distributed GUI mode is browser-hosted.
- **Tester/date:** local issue workflow on the ThalesMMS host, 2026-08-31 UTC.

## macOS arm64 evidence — PASS

- **Candidate:** v0.1.1, commit
  `4aa5fe1368dd16b747ab9cfce988d578e0973c3c`.
- **Target:** macOS 26.6.2 (25G83), `arm64`, Apple M4 Pro. Validation used
  the normal local account on the target machine; no claim of a clean-account
  or first-boot environment is made.
- **Package:** `cooler-compiler-v0.1.1-macos-arm64.zip`, SHA-256
  `65bcb34f3cbe6f092e0d7b9fa9a3a76f828f9c208a8872f2d3ddbe5e9f4631be`.
  Two consecutive release builds produced byte-identical output. The adjacent
  `SHA256SUMS` was verified before extraction or execution.
- **Automated scenarios:** `runtime-smoke.sh` passed CLI version; successful
  execution; semantic and runtime errors; ordered multifile compilation;
  explicit program input; tokens, AST, symbols, CFG, IR, assembly, and report
  emission; browser GUI health, CSP, and application HTML; packaged native app
  launch; and a normal Apple Quit event. `make release-check` passed on the
  clean candidate, including all required checks, race tests, reproducible
  packages, and native macOS packaging/Quit integration.
- **Signature:** `codesign --verify --deep --strict` passed. The app is an
  ad-hoc signed arm64 bundle with identifier
  `com.thalesmms.cooler-compiler`; as expected for an unnotarized ad-hoc build,
  `spctl` did not accept it as Developer ID distributed software.
- **Manual browser scenarios:** the exact packaged browser server completed
  Compile, Step (one instruction), Reset (zero instructions), and Run. Run
  halted after 19 instructions and printed `Hello, COOL!`; the browser log was
  empty. JSON report, IR text, and ZIP report downloads opened successfully.
- **Export inspection:** JSON identified schema `1.0.0`, compiler `0.1.1`, the
  candidate commit, target `darwin/arm64`, zero diagnostics, a halted VM, and
  `HALT` as the last operation. IR contained the expected string push,
  `out_string` call, and halt. The ZIP passed integrity testing, identified the
  same commit, and every byte size and SHA-256 matched its 11-entry manifest.
- **Manual native scenarios:** the packaged WKWebView app completed Compile,
  Step, Reset, and Run with the same 19-instruction halted result and output.
  Command-Q terminated the exact packaged process. Selecting JSON export opened
  the correct schema/commit report inside WKWebView. WKWebView does not persist
  the blob as a file; saved JSON/IR/ZIP downloads are provided by the browser
  fallback included in the same package and were verified above.
- **Verdict:** PASS (`runtime-tested`; browser GUI tested; native GUI tested).
  Known limitation: the package is ad-hoc signed and not notarized, and saved
  exports use browser mode rather than the native WKWebView.
- **Tester/date:** local issue workflow on the ThalesMMS host, 2026-08-31 UTC.

## Smoke-tool qualification (not release evidence)

On 2026-08-31, the POSIX smoke script completed on the current macOS arm64 host
against locally built development binaries. It verified these temporary
artifacts before executing them:

- `coolerc-macos-arm64`: `bcece6e01846ffe3540c8ffbcaa68a9d07ecec7c36b925adb6356e6b143428a4`;
- `coolergui-macos-arm64-browser`: `517b8dcb04e698e186b0b61c00e6b06f5b6723007a123a3eb578e2db28a8405b`.

The automated CLI success, semantic-error, runtime-error, multifile input,
artifact emission, browser health, CSP, and application HTML checks passed.
This qualifies the harness only. The binaries came from the working development
tree rather than a retained final v0.1.1 package, and the manual debugger,
export, and native GUI checks had not yet been performed. Consequently, no
package-matrix row is promoted by this earlier run; later candidate-specific
exact-package evidence is recorded above.

The development native application also launched and terminated through its
normal Apple Quit event without granting Accessibility permission. The release
gate now repeats that integration check and statically guards the menu's
Command-Q binding. This earlier development run did not replace observing the
later package's controls and Command-Q behavior; that observation is recorded
in the macOS candidate evidence above.

The deterministic-package workflow was also qualified on 2026-08-31 using a
dirty development tree whose embedded source identity was commit
`bc988410703aee8715444e3be67d69afe59b36e2`. The generated macOS ZIP had SHA-256
`e3e6a14cdcb085c2db1219463798b4032ee18696df7f625b89e9ab33c2faad95`.
The smoke script verified that archive before extraction, then passed the CLI
and browser checks and launched/quit the packaged native app. This proves the
package-to-smoke integration only: the manifest explicitly marks the dirty
output as non-releasable, and the final
commit, checksum, controls, exports, and Command-Q checks were pending. Later
candidate-specific checks are recorded above, but do not qualify a unified
release set.

## Required evidence per executed target

Record all fields below beneath the applicable matrix row:

- release/tag and full development commit;
- exact OS version, architecture, hardware, VM, or emulation method;
- package filename and SHA-256;
- account or clean-system preparation, where applicable;
- complete output from `runtime-smoke.sh` or `runtime-smoke.ps1`;
- successful, semantic-error, runtime-error, multifile, and input results;
- browser GUI launch, debugger actions, JSON/text/ZIP export inspection;
- native GUI launch and Quit behavior where applicable;
- shared-library report on Linux;
- pass/fail verdict and known limitations;
- tester and UTC date.

Copy the generated release package, its adjacent `SHA256SUMS`, and this
repository's matching smoke script to the target, then run one of the following
commands. The script verifies the distributed archive before extracting or
executing it. The POSIX
script requires Bash and `curl`; the Windows script supports the built-in
Windows PowerShell 5.1 or newer.

```bash
./runtime-smoke.sh /path/to/cooler-compiler-v0.1.1-TARGET.ARCHIVE /path/to/SHA256SUMS
```

```powershell
.\runtime-smoke.ps1 -PackagePath C:\path\to\cooler-compiler-v0.1.1-windows-amd64.zip -ChecksumManifest C:\path\to\SHA256SUMS
```

The scripts never turn their `[MANUAL]` lines into passes. A human must complete
and record the browser/native GUI and export checks before changing a matrix row.
