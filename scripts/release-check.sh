#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

tmp_base="${TMPDIR:-/tmp}"
tmp_base="${tmp_base%/}"
work_dir=""
optional_failures=0

cleanup() {
  if [[ -n "$work_dir" && -d "$work_dir" && "$work_dir" == "$tmp_base"/cooler-release-check.* ]]; then
    find "$work_dir" -depth -delete
  fi
}
trap cleanup EXIT

pass() {
  printf '[PASS] %s\n' "$1"
}

fail() {
  printf '[FAIL] %s\n' "$1" >&2
}

skip() {
  printf '[SKIP] %s\n' "$1"
}

run_required() {
  local label="$1"
  shift
  printf '\n==> Required: %s\n' "$label"
  if "$@"; then
    pass "$label"
  else
    fail "$label"
    return 1
  fi
}

run_optional() {
  local label="$1"
  shift
  printf '\n==> Optional: %s\n' "$label"
  if "$@"; then
    pass "$label"
  else
    fail "$label"
    optional_failures=$((optional_failures + 1))
  fi
}

check_required_tools() {
  local missing=0
  local tool
  for tool in bash find git go gofmt make mktemp node uname xargs; do
    if ! command -v "$tool" >/dev/null 2>&1; then
      printf 'missing required tool: %s\n' "$tool" >&2
      missing=1
    fi
  done
  [[ "$missing" -eq 0 ]]
}

check_repository_diff() {
  git diff --check HEAD --
}

check_go_format() {
  local unformatted
  if ! unformatted="$(find . -type f -name '*.go' \
    -not -path './.git/*' \
    -not -path './bin/*' \
    -not -path './dist/*' \
    -print0 | xargs -0 gofmt -l)"; then
    return 1
  fi
  if [[ -n "$unformatted" ]]; then
    printf 'unformatted Go files:\n%s\n' "$unformatted" >&2
    return 1
  fi
}

check_go_tests() {
  CGO_ENABLED=0 go test -count=1 ./...
}

check_go_vet() {
  CGO_ENABLED=0 go vet ./...
}

check_javascript() {
  local source_file
  for source_file in \
    internal/web/static/about.js \
    internal/web/static/client-utils.js \
    internal/web/static/app.js; do
    node --check "$source_file" || return 1
  done
  node --test \
    internal/web/static/about.test.js \
    internal/web/static/client-utils.test.js
}

check_fallback_builds() {
  local artifact
  make --no-print-directory DIST="$work_dir/dist" build-all || return 1
  for artifact in \
    LICENSE \
    NOTICE \
    THIRD_PARTY_NOTICES.md \
    coolergui-linux-amd64 \
    coolerc-linux-amd64 \
    coolergui-linux-arm64 \
    coolerc-linux-arm64 \
    coolergui-windows-amd64.exe \
    coolerc-windows-amd64.exe \
    coolergui-macos-arm64-browser \
    coolerc-macos-arm64; do
    if [[ ! -f "$work_dir/dist/$artifact" ]]; then
      printf 'missing release artifact: %s\n' "$artifact" >&2
      return 1
    fi
  done
}

check_race() {
  CGO_ENABLED=1 go test -race -count=1 ./...
}

check_macos_package() {
  make --no-print-directory DIST="$work_dir/macos" macos-app || return 1
  local resources="$work_dir/macos/Cooler Compiler Lab.app/Contents/Resources"
  [[ -f "$resources/AppIcon.icns" ]]
  [[ -f "$resources/LICENSE" ]]
  [[ -f "$resources/NOTICE" ]]
  [[ -f "$resources/THIRD_PARTY_NOTICES.md" ]]
}

check_windows_native() {
  make --no-print-directory \
    DIST="$work_dir/windows-native" \
    WINDOWS_CC="$windows_cc" \
    WINDOWS_CXX="$windows_cxx" \
    build-windows-native
}

run_required "required tools" check_required_tools
work_dir="$(mktemp -d "$tmp_base/cooler-release-check.XXXXXX")"

printf 'Cooler release verification\n'
printf 'Repository: %s\n' "$ROOT"
printf 'Temporary output: %s\n' "$work_dir"

run_required "module checksums (go mod verify)" go mod verify
run_required "repository whitespace (git diff --check)" check_repository_diff
run_required "Go formatting (gofmt -l)" check_go_format
run_required "Go tests, examples, embedded resources, and multifile/input behavior" check_go_tests
run_required "Go vet" check_go_vet
run_required "JavaScript syntax and tests" check_javascript
run_required "CLI and GUI fallback release builds" check_fallback_builds

cgo_enabled="$(go env CGO_ENABLED 2>/dev/null || true)"
cc_value="$(go env CC 2>/dev/null || true)"
cc_command="${cc_value%% *}"
if [[ "$cgo_enabled" == "1" && -n "$cc_command" ]] && command -v "$cc_command" >/dev/null 2>&1; then
  run_optional "race-enabled Go tests" check_race
else
  skip "race-enabled Go tests (CGO or C compiler unavailable)"
fi

if [[ "$(uname -s)" == "Darwin" ]] && command -v codesign >/dev/null 2>&1; then
  run_optional "native macOS application packaging" check_macos_package
else
  skip "native macOS application packaging (requires macOS and codesign)"
fi

windows_cc="${WINDOWS_CC:-x86_64-w64-mingw32-gcc}"
windows_cxx="${WINDOWS_CXX:-x86_64-w64-mingw32-g++}"
if command -v "$windows_cc" >/dev/null 2>&1 && command -v "$windows_cxx" >/dev/null 2>&1; then
  run_optional "native Windows WebView2 cross-build" check_windows_native
else
  skip "native Windows WebView2 cross-build (MinGW-w64 toolchain unavailable)"
fi

if command -v golangci-lint >/dev/null 2>&1; then
  run_optional "golangci-lint" golangci-lint run ./...
else
  skip "golangci-lint (tool unavailable)"
fi

if [[ "$optional_failures" -ne 0 ]]; then
  fail "$optional_failures optional check(s) failed"
  exit 1
fi

printf '\n'
pass "release verification completed"
