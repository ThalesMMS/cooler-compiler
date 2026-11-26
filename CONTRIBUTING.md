# Contributing

Cooler Compiler is a local-first educational compiler. Contributions should
preserve deterministic behavior, explicit compatibility boundaries, and the
absence of hosted build or test dependencies.

## Contributor License Agreement

Every outside contribution requires agreement to the
[Contributor License Agreement](CLA.md). You keep the copyright in your work;
the agreement lets the maintainer keep offering Cooler Compiler under the AGPL
and under commercial terms, including in app stores whose terms conflict with
copyleft licenses. In return, every accepted contribution stays available under
the AGPL or another OSI-approved license.

To agree, include the acceptance statement in [`CLA.md`](CLA.md#7-how-to-agree)
with your contribution. If you contribute for a company or other organization,
contact the maintainer first so the organization can sign an entity agreement.

## Development setup

Install the Go version declared in [`go.mod`](go.mod) and Node.js 20 or newer, then clone
the repository and run:

```bash
go mod verify
./scripts/install-golangci-lint.sh
make test
make vet
make lint
make docs-check
go run ./cmd/coolergui --browser
```

The GUI is served locally. No account, database, cloud service, or CI runner is
required. Platform-specific prerequisites and the full gate are documented in
[`docs/release.md`](docs/release.md).

Keep commits reviewable and include tests with behavior changes. Do not commit
`bin/`, `dist/`, downloaded toolchains, credentials, or machine-specific paths.

## Code and tests

Go code is formatted with `gofmt`. JavaScript uses the existing module and test
style. The repository pins `golangci-lint` in
[`tools/golangci-lint.version`](tools/golangci-lint.version), verifies downloads
against the repository-owned SHA-256 list, and rejects any other installed
version. Installation is the only networked step; `make lint` and the release
gate run the pinned binary without downloading tools. Before proposing a
change, run:

```bash
make check
make lint
make docs-check
```

For release-affecting work, run `make release-check`. It performs local tests,
vetting, syntax checks, documentation checks, cross-builds, and two independent
byte-identical package builds. Optional platform checks are clearly reported as
passed or skipped; a cross-build is never described as a runtime test.

Compiler robustness work also uses `make fuzz-smoke`, `make property-test`, and
targeted campaigns such as `make fuzz TARGET=FuzzCompile DURATION=10m`. Inputs
are bounded and fuzzing stays local. Minimize a reproduced failure, add it to
`internal/compiler/testdata/fuzz-regressions/`, and record its bug class and
expected invariant in the manifest.

If a user-supplied Stanford-compatible toolchain is available, `make
differential` compares the original corpus locally. Do not download or commit
reference binaries or third-party course fixtures. Record exact tool hashes and
host metadata, and add an allowlist entry only for a reproduced, rule-linked
semantic difference. A normal missing-tool skip is expected; use `STRICT=1`
when the comparison is mandatory for the local task.

## Adding a COOL conformance fixture

Add normative fixtures below
[`internal/compiler/testdata/conformance/`](internal/compiler/testdata/conformance/)
and link them reciprocally from `matrix.json`. Each directory has a strict
`fixture.json` declaring stable rule IDs, validity, last successful stage,
diagnostic codes, output or runtime status, ordered source files, optional input,
and provenance. Use an existing fixture only when it proves the entire stated
rule; otherwise add the smallest original program that does.

Debugger rules use a bounded `debuggerScenario` in the same metadata file.
Prefer declarative action sequences and stable public snapshot expectations;
the harness records a compact replay prefix for a failing debugger action.

The older `valid/`, `invalid/`, and `golden/` trees remain useful implementation
regressions, but they do not establish a normative coverage claim. Examples are
teaching material rather than conformance evidence.

Use minimal executable COOL. For example:

```cool
class Main inherits IO {
  main() : Object { out_string("fixture ok\n") };
};
```

Run `make conformance`, inspect `dist/conformance-report.json`, and then run
`go test ./...`. The full format, status vocabulary, source precedence, and
licensing rules are in [`docs/conformance.md`](docs/conformance.md).

## Updating golden files intentionally

Goldens live in
[`internal/compiler/testdata/golden/`](internal/compiler/testdata/golden/) and
are LF-normalized by [`.gitattributes`](.gitattributes). There is deliberately
no blanket regeneration switch.

Run the focused failing test, copy only the reviewed `got` representation into
the named golden, rerun that test, and inspect `git diff --check` plus the full
diff. Explain semantic changes to diagnostics, IR, tokens, or AST in the commit.
Never update a golden only to silence an unexplained failure.

## Documentation changes

Use stable headings because other documents link to them. Mark statements as
normative contract, current implementation detail, or future direction where
the distinction matters. Link to canonical code and tests instead of copying
large inventories. `make docs-check` rejects broken relative links, missing
anchors, invalid schema identities, missing document-index entries, and a stale
diagnostic code catalog.

## Reporting a vulnerability

Do not post vulnerability details publicly. Follow
[`SECURITY.md`](SECURITY.md#reporting-a-vulnerability).
