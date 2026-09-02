#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

target="${1:-}"
duration="${2:-}"
workers="${FUZZ_WORKERS:-4}"
targets=(
  FuzzLex
  FuzzParse
  FuzzCompile
  FuzzCompileProject
  FuzzBytecodeVerifier
  FuzzProgramInput
  FuzzOptimizationEquivalence
  FuzzJSONRoundTrip
)

if [[ ! "$duration" =~ ^[1-9][0-9]*(ms|s|m|h)$ ]]; then
  printf 'invalid fuzz duration %q; use a positive duration such as 30s or 10m\n' "$duration" >&2
  exit 2
fi
if [[ ! "$workers" =~ ^[1-9][0-9]*$ ]]; then
  printf 'FUZZ_WORKERS must be a positive integer\n' >&2
  exit 2
fi

run_target() {
  local name="$1"
  printf '\n==> Fuzz target: %s (%s, %s workers)\n' "$name" "$duration" "$workers"
  go test ./internal/compiler -run '^$' -fuzz "^${name}$" -fuzztime "$duration" -parallel "$workers"
}

if [[ "$target" == "all" ]]; then
  for name in "${targets[@]}"; do
    run_target "$name"
  done
  exit 0
fi

for name in "${targets[@]}"; do
  if [[ "$target" == "$name" ]]; then
    run_target "$name"
    exit 0
  fi
done

printf 'unknown fuzz target %q; choose one of:\n' "$target" >&2
printf '  %s\n' "${targets[@]}" >&2
exit 2
