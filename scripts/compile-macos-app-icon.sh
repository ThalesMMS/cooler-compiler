#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail

app="${1:?usage: compile-macos-app-icon.sh APP MIN_MACOS_VERSION}"
min_version="${2:?minimum macOS version required}"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
resources="$app/Contents/Resources"
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/cooler-app-icon.XXXXXX")"
trap 'rm -r -- "$work_dir"' EXIT

# Let macOS mask the original artwork without adding a plate around it.
icon="$work_dir/AppIcon.icon"
compiled="$work_dir/compiled"
mkdir -p "$icon/Assets" "$compiled"
cp "$root/assets/AppIcon.icon.json" "$icon/icon.json"
sips -z 1024 1024 "$root/assets/AppIcon.png" --out "$icon/Assets/Cooler.png" >&2

xcrun actool --compile "$compiled" --platform macosx --target-device mac \
  --minimum-deployment-target "$min_version" --app-icon AppIcon \
  --output-partial-info-plist "$work_dir/IconInfo.plist" "$icon" >&2
cp "$compiled/Assets.car" "$compiled/AppIcon.icns" "$resources/"
icon_name="$(plutil -extract CFBundleIconName raw "$work_dir/IconInfo.plist")"
plutil -insert CFBundleIconName -string "$icon_name" "$app/Contents/Info.plist"
