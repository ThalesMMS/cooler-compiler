#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
required_version="$(tr -d '[:space:]' < "$repo_root/tools/golangci-lint.version")"

if [[ -n "${GOLANGCI_LINT:-}" ]]; then
  lint_command="$GOLANGCI_LINT"
elif [[ -x "$repo_root/bin/tools/golangci-lint" ]]; then
  lint_command="$repo_root/bin/tools/golangci-lint"
elif [[ -x "$repo_root/bin/tools/golangci-lint.exe" ]]; then
  lint_command="$repo_root/bin/tools/golangci-lint.exe"
elif command -v golangci-lint >/dev/null 2>&1; then
  lint_command="$(command -v golangci-lint)"
else
  printf 'golangci-lint v%s is required but unavailable.\n' "$required_version" >&2
  printf 'Run scripts/install-golangci-lint.sh or set GOLANGCI_LINT to the pinned binary.\n' >&2
  exit 1
fi

version_output="$($lint_command version 2>&1)"
if [[ "$version_output" != *"version $required_version "* ]]; then
  printf 'golangci-lint v%s is required; found: %s\n' "$required_version" "$version_output" >&2
  exit 1
fi

cd "$repo_root"
"$lint_command" config verify
"$lint_command" run ./...
