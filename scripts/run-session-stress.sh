#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

count="${1:-20}"
timeout="${2:-5m}"
if [[ ! "$count" =~ ^[1-9][0-9]*$ ]] || (( count > 1000 )); then
  printf 'session stress count must be an integer from 1 through 1000\n' >&2
  exit 2
fi
if [[ ! "$timeout" =~ ^[1-9][0-9]*(s|m|h)$ ]]; then
  printf 'session stress timeout must be a positive duration such as 30s or 5m\n' >&2
  exit 2
fi

printf 'Session stress: %s race-enabled repetitions (timeout %s)\n' "$count" "$timeout"
go test -race -count="$count" -timeout "$timeout" ./cmd/coolergui \
  -run '^(TestSession|TestCompleteRacingNext|TestRunRaces|TestPipelinePromotion|TestServerShutdown)'
