#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APP="${1:-$ROOT/dist/Cooler Compiler Lab.app}"

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "macOS quit integration test requires macOS" >&2
  exit 1
fi
if [[ ! -d "$APP" ]]; then
  echo "application bundle not found: $APP" >&2
  exit 1
fi

open -n "$APP"
app_pid=""
for _ in {1..100}; do
  app_pid="$(pgrep -nx coolergui || true)"
  [[ -n "$app_pid" ]] && break
  sleep 0.1
done
if [[ -z "$app_pid" ]]; then
  echo "Cooler Compiler Lab did not launch" >&2
  exit 1
fi

cleanup() {
  if kill -0 "$app_pid" 2>/dev/null; then
    kill "$app_pid" 2>/dev/null || true
  fi
}
trap cleanup EXIT

menu_ready=false
for _ in {1..50}; do
  if osascript - "$app_pid" >/dev/null 2>&1 <<'APPLESCRIPT'
on run argv
  set targetPID to (item 1 of argv) as integer
  tell application "System Events"
    set appProcess to first application process whose unix id is targetPID
    if (count of menu bars of appProcess) is greater than 0 then return
    error "menu bar is not ready"
  end tell
end run
APPLESCRIPT
  then
    menu_ready=true
    break
  fi
  sleep 0.1
done
if [[ "$menu_ready" != true ]]; then
  echo "Cooler Compiler Lab did not expose a menu bar" >&2
  exit 1
fi

osascript - "$app_pid" <<'APPLESCRIPT'
on run argv
  set targetPID to (item 1 of argv) as integer
  tell application "System Events"
    set appProcess to first application process whose unix id is targetPID
    set frontmost of appProcess to true
    tell appProcess
      set appMenu to menu 1 of menu bar item "Cooler Compiler Lab" of menu bar 1
      set quitItem to first menu item of appMenu whose name is "Quit Cooler Compiler Lab"
      set shortcut to value of attribute "AXMenuItemCmdChar" of quitItem
      if shortcut is not "Q" and shortcut is not "q" then
        error "Quit menu item does not use Q as its key equivalent"
      end if
    end tell
    keystroke "q" using command down
  end tell
end run
APPLESCRIPT

for _ in {1..100}; do
  if ! kill -0 "$app_pid" 2>/dev/null; then
    trap - EXIT
    echo "Command-Q terminated Cooler Compiler Lab"
    exit 0
  fi
  sleep 0.1
done

echo "Cooler Compiler Lab remained running after Command-Q" >&2
exit 1
