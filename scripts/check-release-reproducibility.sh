#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail

# Git for Windows may resolve FIND.EXE before the POSIX find used below.
case "$(uname -s)" in
  MSYS* | MINGW* | CYGWIN*)
    export PATH="$(dirname "$BASH"):$PATH"
    ;;
esac

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp_base="${TMPDIR:-/tmp}"
tmp_base="${tmp_base%/}"
work_dir="$(mktemp -d "$tmp_base/cooler-reproducibility.XXXXXX")"

cleanup() {
  if [[ -d "$work_dir" && "$work_dir" == "$tmp_base"/cooler-reproducibility.* ]]; then
    find "$work_dir" -depth -delete
  fi
}
trap cleanup EXIT

cd "$ROOT"
commit="$(git rev-parse --verify HEAD)"
epoch="$(git show -s --format=%ct "$commit")"
for run in first second; do
  RELEASE_ALLOW_DIRTY=1 VERSION="${VERSION:-1.1.0}" COMMIT="$commit" SOURCE_DATE_EPOCH="$epoch" \
    ./scripts/build-release.sh "$work_dir/$run" >/dev/null
done

(cd "$work_dir/first" && find . -type f -print | LC_ALL=C sort) >"$work_dir/first-files.txt"
(cd "$work_dir/second" && find . -type f -print | LC_ALL=C sort) >"$work_dir/second-files.txt"
if ! cmp -s "$work_dir/first-files.txt" "$work_dir/second-files.txt"; then
  printf 'release builds produced different file lists\n' >&2
  diff -u "$work_dir/first-files.txt" "$work_dir/second-files.txt" >&2 || true
  exit 1
fi
while IFS= read -r relative; do
  if ! cmp -s "$work_dir/first/$relative" "$work_dir/second/$relative"; then
    printf 'release artifact is not reproducible: %s\n' "$relative" >&2
    exit 1
  fi
done <"$work_dir/first-files.txt"
printf '[PASS] two consecutive local release builds are byte-for-byte identical\n'
