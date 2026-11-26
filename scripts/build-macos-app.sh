#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail

VERSION="${1:-1.1.0}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${DIST_DIR:-$ROOT/dist}"
COMMIT="${COMMIT:-$(git -C "$ROOT" rev-parse --verify HEAD 2>/dev/null || true)}"
SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-$(git -C "$ROOT" show -s --format=%ct HEAD 2>/dev/null || true)}"
APP_NAME="${APP_NAME:-Cooler Compiler}"
APP="$DIST_DIR/$APP_NAME.app"
MACOS="$APP/Contents/MacOS"
RESOURCES="$APP/Contents/Resources"

if [[ -z "$COMMIT" ]]; then
  printf '[WARN] Git source identity unavailable; embedding commit=unknown\n' >&2
  COMMIT="unknown"
fi
if [[ -z "$SOURCE_DATE_EPOCH" ]]; then
  printf '[WARN] Git source timestamp unavailable; embedding build date 0\n' >&2
  SOURCE_DATE_EPOCH="0"
fi

rm -rf "$APP"
mkdir -p "$MACOS" "$RESOURCES"

cd "$ROOT"
if [[ "$(uname -s)" == "Darwin" ]]; then
  CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -buildvcs=false -trimpath \
    -ldflags "-s -w -X main.version=$VERSION -X main.commit=$COMMIT -X main.buildDate=$SOURCE_DATE_EPOCH" \
    -o "$MACOS/coolergui" ./cmd/coolergui
else
  printf '[INFO] cross-building the macOS application bundle with the browser fallback\n' >&2
  GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -buildvcs=false -trimpath \
    -ldflags "-s -w -X main.version=$VERSION -X main.commit=$COMMIT -X main.buildDate=$SOURCE_DATE_EPOCH" \
    -o "$MACOS/coolergui" ./cmd/coolergui
fi
chmod +x "$MACOS/coolergui"
cp "$ROOT/assets/AppIcon.icns" "$RESOURCES/AppIcon.icns"
for notice_file in LICENSE NOTICE THIRD_PARTY_NOTICES.md; do
  cp "$ROOT/$notice_file" "$RESOURCES/$notice_file"
done

cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleDisplayName</key><string>Cooler Compiler</string>
  <key>CFBundleExecutable</key><string>coolergui</string>
  <key>CFBundleIdentifier</key><string>thalesmms.coolercompiler</string>
  <key>CFBundleInfoDictionaryVersion</key><string>6.0</string>
  <key>CFBundleIconFile</key><string>AppIcon</string>
  <key>CFBundleName</key><string>Cooler Compiler</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>$VERSION</string>
  <key>CFBundleVersion</key><string>$VERSION</string>
  <key>LSMinimumSystemVersion</key><string>12.0</string>
  <key>NSHumanReadableCopyright</key><string>Copyright © 2026 Thales Matheus M Santos</string>
  <key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
PLIST

if [[ "$(uname -s)" == "Darwin" ]] && command -v codesign >/dev/null 2>&1; then
  codesign --force --sign - "$APP"
fi

echo "$APP"
