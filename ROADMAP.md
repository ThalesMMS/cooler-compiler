# Roadmap

This roadmap separates accepted, tracked work from ideas that are still being
explored. It describes direction, not a delivery-date promise. The canonical
status of any item is its linked GitHub issue. Development happens in this
repository and reviewed releases are published separately under the boundary
defined in [`docs/development-publication.md`](docs/development-publication.md).

## Supported release

The current supported public release is **v0.1.0**. The private development
tree is the **v1.0.0 candidate line**. Features previously grouped under
v0.1.x, v0.2, and v0.3 are incorporated into that one candidate; they are not
separate future releases.

The declared v1.0.0 scope is **implemented**. Release status is tracked as four
distinct states, and an earlier state never implies a later one:

1. **Implemented candidate** — the declared scope exists on `main`. Reached.
2. **Locally verified candidate** — one frozen commit passes the complete local
   verification gate
   ([Final 09/11](https://github.com/ThalesMMS/cooler-compiler-dev/issues/59)).
3. **Target-tested artifacts** — one unified artifact set is built and its exact
   hashes are runtime-tested on every supported target
   ([Final 10/11](https://github.com/ThalesMMS/cooler-compiler-dev/issues/60)).
4. **Public release** — the exact candidate is exported, tagged, packaged, and
   published through the one-way release process
   ([Final 11/11](https://github.com/ThalesMMS/cooler-compiler-dev/issues/61)).

v1.0.0 remains **unreleased** until Final 11/11 closes and the sequential
[v1.0.0 completion contract](docs/v1.0-completion-contract.md) is satisfied.
Completing an implementation milestone does not silently promote its
development tree to a supported public release, and no target may be described
as runtime-tested before Final 10/11 records that target's exact package hash.

The v0.1.1 target-runtime gate and evidence are complete. See the
[changelog](CHANGELOG.md), [security policy](SECURITY.md), and
[release validation record](docs/release-validation-v0.1.1.md).

## Incorporated implementation history

The following private-development milestones are complete and incorporated into
the v1.0.0 candidate. They are historical groupings, not pending v0.2 or v0.3
release promises. Public release status remains governed by the
[completion contract](docs/v1.0-completion-contract.md) and
[publication boundary](docs/development-publication.md).

### v0.1.x — hardening and distribution

The completed [v0.1.1 release-readiness epic](https://github.com/ThalesMMS/cooler-compiler-dev/issues/44)
delivered compatibility documentation, diagnostics and regression fixtures,
local release validation, security hardening, artifact contracts, project
documents, and final target evidence. Its work items were
[#14](https://github.com/ThalesMMS/cooler-compiler-dev/issues/14) through
[#20](https://github.com/ThalesMMS/cooler-compiler-dev/issues/20), all closed.

### v0.2 — complete the educational workbench

The completed [v0.2 epic](https://github.com/ThalesMMS/cooler-compiler-dev/issues/45)
and issues [#21](https://github.com/ThalesMMS/cooler-compiler-dev/issues/21)
through [#34](https://github.com/ThalesMMS/cooler-compiler-dev/issues/34)
delivered rule-level COOL conformance, native multifile compilation, bytecode
verification, deterministic resource bounds, fuzz/differential/race evidence,
and performance baselines. Breaking artifact changes must follow the migration
policy in the [changelog](CHANGELOG.md#versioning-and-migrations).

### v0.3 — extensibility and deeper tooling

The completed [v0.3 epic](https://github.com/ThalesMMS/cooler-compiler-dev/issues/46)
and issues [#35](https://github.com/ThalesMMS/cooler-compiler-dev/issues/35)
through [#43](https://github.com/ThalesMMS/cooler-compiler-dev/issues/43)
delivered interactive input, source/reverse debugging, bounded heap and watch
tools, compiler relationship views, verified educational optimization, a
stable public Go API, and a local COOL LSP. The
[master roadmap epic](https://github.com/ThalesMMS/cooler-compiler-dev/issues/47)
records the completed ordering and cross-release guarantees.

## Exploratory directions

These are research themes, not accepted release scope:

- native machine-code backends or reference-toolchain binary compatibility;
- a third-party extension or compiler-pass ecosystem;
- authenticated remote or multi-user hosting;
- additional desktop architectures and mobile packages.

An exploratory direction becomes committed only when a scoped issue is accepted
into a milestone and linked from this document.

None of these directions can enter v1.0.0 without explicitly reopening
[Final 01/11](https://github.com/ThalesMMS/cooler-compiler-dev/issues/51) and
revising the locked completion contract.

## Roadmap maintenance

Update this file when an epic is accepted, materially re-scoped, released, or
abandoned. Completed user-visible changes belong in [CHANGELOG.md](CHANGELOG.md);
implementation history belongs in commits and issues rather than duplicated
narrative here.
