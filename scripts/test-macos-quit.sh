#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APP="${1:-$ROOT/dist/Cooler Compiler.app}"

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "macOS quit integration test requires macOS" >&2
  exit 1
fi
if [[ ! -d "$APP" ]]; then
  echo "application bundle not found: $APP" >&2
  exit 1
fi

bundle_id="thalesmms.coolercompiler"
read_app_pid() {
  local info
  info="$(lsappinfo info -only pid -app "$bundle_id" 2>/dev/null || true)"
  sed -nE \
    -e 's/.*"pid"=([0-9]+).*/\1/p' \
    -e 's/.*pid[[:space:]]*=[[:space:]]*([0-9]+).*/\1/p' \
    -e 's/^ *([0-9]+)[[:space:]].*/\1/p' <<<"$info" | head -n 1
}

app_pid="$(read_app_pid)"
if [[ -n "$app_pid" ]]; then
  echo "an application with bundle identifier $bundle_id is already running" >&2
  exit 1
fi

open -n "$APP"
for _ in {1..100}; do
  app_pid="$(read_app_pid)"
  [[ -n "$app_pid" ]] && break
  sleep 0.1
done
if [[ -z "$app_pid" ]]; then
  echo "Cooler Compiler did not launch" >&2
  exit 1
fi

cleanup() {
  if kill -0 "$app_pid" 2>/dev/null; then
    kill "$app_pid" 2>/dev/null || true
  fi
}
trap cleanup EXIT

# A direct Apple Quit event exercises the application's normal termination path
# without requiring the test runner to have Accessibility permission. The menu's
# Command-Q binding is separately guarded by a source-level regression test.
osascript -e "tell application id \"$bundle_id\" to quit"

for _ in {1..100}; do
  if ! kill -0 "$app_pid" 2>/dev/null; then
    trap - EXIT
    echo "Apple Quit event terminated Cooler Compiler"
    exit 0
  fi
  sleep 0.1
done

echo "Cooler Compiler remained running after the Apple Quit event" >&2
exit 1
