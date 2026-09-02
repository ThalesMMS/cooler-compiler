#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version="$(tr -d '[:space:]' < "$repo_root/tools/golangci-lint.version")"
destination="${GOLANGCI_LINT_INSTALL_DIR:-$repo_root/bin/tools}"

case "$(uname -s)" in
  Darwin) platform="darwin"; extension="tar.gz"; binary="golangci-lint" ;;
  Linux) platform="linux"; extension="tar.gz"; binary="golangci-lint" ;;
  MINGW*|MSYS*|CYGWIN*) platform="windows"; extension="zip"; binary="golangci-lint.exe" ;;
  *) printf 'unsupported golangci-lint host: %s\n' "$(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64|amd64) architecture="amd64" ;;
  arm64|aarch64) architecture="arm64" ;;
  *) printf 'unsupported golangci-lint architecture: %s\n' "$(uname -m)" >&2; exit 1 ;;
esac

if ! command -v curl >/dev/null 2>&1; then
  printf 'curl is required to install golangci-lint.\n' >&2
  exit 1
fi
if [[ "$extension" == "zip" ]] && ! command -v unzip >/dev/null 2>&1; then
  printf 'unzip is required to install golangci-lint on Windows.\n' >&2
  exit 1
fi

asset="golangci-lint-${version}-${platform}-${architecture}.${extension}"
base_url="https://github.com/golangci/golangci-lint/releases/download/v${version}"
temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT

curl --fail --location --silent --show-error "$base_url/$asset" --output "$temporary/$asset"

expected="$(awk -v asset="$asset" '$2 == asset { print $1 }' "$repo_root/tools/golangci-lint.checksums")"
if [[ -z "$expected" ]]; then
	printf 'pinned checksum list does not contain %s.\n' "$asset" >&2
  exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$temporary/$asset" | awk '{ print $1 }')"
else
  actual="$(shasum -a 256 "$temporary/$asset" | awk '{ print $1 }')"
fi
if [[ "$actual" != "$expected" ]]; then
  printf 'checksum mismatch for %s: expected %s, got %s.\n' "$asset" "$expected" "$actual" >&2
  exit 1
fi

mkdir -p "$destination"
if [[ "$extension" == "zip" ]]; then
  unzip -q "$temporary/$asset" -d "$temporary/extracted"
  extracted="$temporary/extracted/golangci-lint-${version}-${platform}-${architecture}/$binary"
else
  tar -xzf "$temporary/$asset" -C "$temporary"
  extracted="$temporary/golangci-lint-${version}-${platform}-${architecture}/$binary"
fi
cp "$extracted" "$destination/$binary"
chmod +x "$destination/$binary"
printf 'installed golangci-lint v%s at %s\n' "$version" "$destination/$binary"
