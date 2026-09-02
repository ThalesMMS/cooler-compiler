#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTPUT="${1:-$ROOT/dist}"
VERSION="${VERSION:-1.0.0}"
COMMIT="${COMMIT:-$(git -C "$ROOT" rev-parse --verify HEAD)}"
SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-$(git -C "$ROOT" show -s --format=%ct "$COMMIT")}"
tmp_base="${TMPDIR:-/tmp}"
tmp_base="${tmp_base%/}"
work_dir="$(mktemp -d "$tmp_base/cooler-release-build.XXXXXX")"

cleanup() {
  if [[ -d "$work_dir" && "$work_dir" == "$tmp_base"/cooler-release-build.* ]]; then
    rm -rf -- "$work_dir"
  fi
}
trap cleanup EXIT

cd "$ROOT"
if [[ "${RELEASE_ALLOW_DIRTY:-0}" != "1" && -n "$(git status --porcelain --untracked-files=normal)" ]]; then
  printf 'release packaging requires a clean working tree (set RELEASE_ALLOW_DIRTY=1 only for reproducibility tests)\n' >&2
  exit 1
fi
mkdir -p "$OUTPUT"
shopt -s nullglob dotglob
output_entries=("$OUTPUT"/*)
shopt -u nullglob dotglob
if (( ${#output_entries[@]} > 0 )); then
  printf 'release output directory must be empty: %s\n' "$OUTPUT" >&2
  exit 1
fi

raw="$work_dir/raw"
make --no-print-directory DIST="$raw" VERSION="$VERSION" COMMIT="$COMMIT" SOURCE_DATE_EPOCH="$SOURCE_DATE_EPOCH" build-all

require_macos_app=false
skipped_checks=()
if [[ "$(uname -s)" == "Darwin" ]] && command -v codesign >/dev/null 2>&1; then
  make --no-print-directory DIST="$raw" VERSION="$VERSION" COMMIT="$COMMIT" SOURCE_DATE_EPOCH="$SOURCE_DATE_EPOCH" macos-app
  require_macos_app=true
else
  skipped_checks+=("native macOS application package unavailable on this host")
fi

windows_cc="${WINDOWS_CC:-x86_64-w64-mingw32-gcc}"
windows_cxx="${WINDOWS_CXX:-x86_64-w64-mingw32-g++}"
if command -v "$windows_cc" >/dev/null 2>&1 && command -v "$windows_cxx" >/dev/null 2>&1; then
  make --no-print-directory DIST="$raw" VERSION="$VERSION" COMMIT="$COMMIT" SOURCE_DATE_EPOCH="$SOURCE_DATE_EPOCH" \
    WINDOWS_CC="$windows_cc" WINDOWS_CXX="$windows_cxx" build-windows-native
else
  skipped_checks+=("native Windows WebView2 build unavailable: MinGW-w64 toolchain not found")
fi
if [[ "${RELEASE_ALLOW_DIRTY:-0}" == "1" && -n "$(git status --porcelain --untracked-files=normal)" ]]; then
  skipped_checks+=("reproducibility qualification used a dirty development tree and output is not releasable")
fi

skipped_text=""
for check in "${skipped_checks[@]}"; do
  if [[ -n "$skipped_text" ]]; then
    skipped_text+=";"
  fi
  skipped_text+="$check"
done

go run ./cmd/cooler-release \
  -input "$raw" \
  -output "$OUTPUT" \
  -version "$VERSION" \
  -commit "$COMMIT" \
  -source-date-epoch "$SOURCE_DATE_EPOCH" \
  -require-macos-app="$require_macos_app" \
  -skipped-checks "$skipped_text"
