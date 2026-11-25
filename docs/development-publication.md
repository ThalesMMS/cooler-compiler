# Development and public repository contract

Cooler uses two repositories with deliberately different histories:

- `ThalesMMS/cooler-compiler-dev` is the development source. Implementation,
  experiments, issue work, release preparation, and internal publication tools
  live here. The final v1.0.0 sequence is performed directly and sequentially
  on `main` under the [completion contract](v1.0-completion-contract.md).
- `ThalesMMS/cooler-compiler` contains reviewed public snapshots, public tags,
  and release artifacts. It never receives the private repository's Git history,
  internal branches, issues, pull requests, or development-only publication
  files.

The historical `issues` branch was aligned to the exact public `v0.1.0` source
tree before v0.1.1 work began. The preceding development `main` remains
recoverable as `legacy/pre-publication-v0.1`. Confirm both facts locally with:

```bash
git rev-parse legacy/pre-publication-v0.1
git diff --exit-code v0.1.0^{tree} 43c82cc^{tree}
```

The second command is intentionally empty: private commit `43c82cc` and public
tag `v0.1.0` have identical trees even though their histories are unrelated.

## Prepare a public snapshot

All release verification is local; this project does not use GitHub Actions,
hosted CI, or CI badges.

1. Start from the clean, immutable v1.0.0 candidate commit on `main` established
   by Final 09/11, after its complete local gate and Final 10/11 target-runtime
   qualification have passed. Do not change tracked candidate bytes during
   export or publication.
2. Create a new candidate directory from that exact commit:

   ```bash
   candidate_parent="$(mktemp -d)"
   ./scripts/publication-sync.sh export HEAD "$candidate_parent/cooler-compiler"
   ```

   The exporter reads committed objects with `git archive`; it never copies the
   working tree or `.git`. It also removes the internal publication document,
   exporter, and exporter test. It refuses to overwrite an existing destination
   and writes an adjacent deterministic `.publication-source` record containing
   the full development commit and archive object IDs.

3. Clone the public repository into another temporary directory. Remove its
   tracked files, copy only the contents of the generated candidate directory,
   run `git add -A`, review `git diff --cached`, and create a normal public snapshot commit. Do not
   add the development repository as a remote and do not merge, rebase, or
   cherry-pick its commits.
4. Before pushing or tagging, prove that the public commit contains exactly the
   filtered tree produced by the reviewed development commit:

   ```bash
   ./scripts/publication-sync.sh compare \
     <full-development-commit> <local-public-clone> <public-candidate-commit>
   ```

   Keep the adjacent `.publication-source` record with the local release
   evidence. The reported development commit plus a successful exact comparison
   establishes which private commit produced the public snapshot. The sidecar is
   outside the candidate directory and therefore cannot be copied accidentally
   with the snapshot.
5. Push the reviewed public commit, create its public tag, and build release
   artifacts from that tag. Never force-push the public repository.

To validate the original alignment, clone `ThalesMMS/cooler-compiler`, check out
`v0.1.0`, and run:

```bash
./scripts/publication-sync.sh compare 43c82cc <local-public-clone> v0.1.0
```

## Reconcile a public hotfix

A public hotfix must flow back into development before the next snapshot. Since
the repositories intentionally have unrelated histories, do not merge or
cherry-pick the public commit. In a clean public clone, generate a binary patch
for the hotfix range with `git diff --binary <previous-public-ref>..<hotfix-ref>`.
Apply and review that patch on `cooler-compiler-dev/main`, adapt it if private
development has moved, run `make release-check`, and commit it with the public
hotfix commit ID in the message or body. Only then prepare the next public
snapshot through the export and comparison procedure above.

If a hotfix cannot be applied cleanly, reproduce the correction manually and add
a regression test that fails on the pre-hotfix development code. The public fix
is not considered reconciled until the development commit passes the local gate.
