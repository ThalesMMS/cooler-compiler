# Security policy

## Supported versions

| Version | Security support |
| --- | --- |
| Latest released `0.1.x` | Supported |
| Development branches | Best-effort pre-release fixes; not a supported distribution |
| Older minor or pre-release builds | Unsupported; upgrade first |

This table changes when a new minor line becomes the supported release. The
[changelog](CHANGELOG.md) records security-relevant user-visible changes.

## Reporting a vulnerability

Please use the repository's
[private security advisory form](https://github.com/ThalesMMS/cooler-compiler-dev/security/advisories/new).
Do not include exploit details, tokens, private source programs, or unpatched
vulnerabilities in a public issue.

Include the affected version/commit and platform, a minimal reproduction, the
security impact, whether remote access or `--allow-remote` was involved, and any
suggested mitigation. Maintainers will acknowledge and triage reports on a
best-effort basis; this project does not promise a response or disclosure SLA.

## Local-server threat model

Cooler is designed as a single-user desktop/browser companion bound to a
loopback address. Its default trust boundary is one local OS account and one
fresh process:

- the server binds to loopback and refuses non-loopback addresses by default;
- a random per-process capability is injected into the uncached initial page,
  kept in browser memory, and required by state-changing compiler/session calls;
- Host, Origin, HTTP method, JSON media type, body size, fields, breakpoints,
  trace offsets, and execution steps are bounded or validated;
- API responses are `no-store`, and the page uses a restrictive content security
  policy;
- compiler execution, session count/lifetime, VM stack/frames/objects/trace, and
  exported archive paths are bounded.

The implementation and regression tests are in
[`cmd/coolergui/main.go`](cmd/coolergui/main.go) and
[`cmd/coolergui/main_test.go`](cmd/coolergui/main_test.go). Runtime limits are
documented in [`docs/vm-runtime.md`](docs/vm-runtime.md#deterministic-limits).

## Non-loopback mode

`--allow-remote` is an explicit escape hatch for a trusted, controlled network.
It is **not** a supported deployment mode. Cooler provides no user
authentication, authorization, TLS termination, tenancy isolation, CSRF token
rotation, audit log, sandbox for compiled programs, or reverse-proxy hardening.
Never expose it to the public Internet or an untrusted LAN, and do not place it
behind a public proxy.

COOL source, program input, diagnostics, output, and exported artifacts can be
sensitive. A user with access to the local page and process capability can read
or execute that session's data. Native WebViews rely on the host OS and bundled
platform WebView security updates.

## Dependency and release integrity

Verify release archives with the adjacent `SHA256SUMS` before execution. The
release manifest distinguishes built artifacts from target-runtime-tested ones;
see the [artifact contract](docs/artifact-contract.md#release-provenance) and
[manual evidence ledger](docs/release-validation-v0.1.1.md). Dependency licenses
and provenance are listed in
[`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md).
