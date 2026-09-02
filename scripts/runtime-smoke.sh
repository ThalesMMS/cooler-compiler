#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail

if [[ "$#" -ne 2 ]]; then
  printf 'usage: %s PACKAGE_ARCHIVE_OR_LEGACY_DIRECTORY SHA256SUMS\n' "$0" >&2
  exit 2
fi

package_input="$1"
checksum_manifest="$(cd "$(dirname "$2")" && pwd)/$(basename "$2")"
platform="$(uname -s)"
architecture="$(uname -m)"
smoke_port="${COOLER_SMOKE_PORT:-0}"
smoke_root="$(mktemp -d "${TMPDIR:-/tmp}/cooler-runtime-smoke.XXXXXX")"
gui_pid=""
native_pid=""

cleanup() {
  if [[ -n "$gui_pid" ]] && kill -0 "$gui_pid" 2>/dev/null; then
    kill "$gui_pid" 2>/dev/null || true
    wait "$gui_pid" 2>/dev/null || true
  fi
  if [[ -n "$native_pid" ]] && kill -0 "$native_pid" 2>/dev/null; then
    kill "$native_pid" 2>/dev/null || true
  fi
  if [[ -d "$smoke_root" && "$(basename "$smoke_root")" == cooler-runtime-smoke.* ]]; then
    find "$smoke_root" -depth -delete
  fi
}
trap cleanup EXIT

case "$platform/$architecture" in
  Darwin/arm64)
    legacy_coolerc_name="coolerc-macos-arm64"
    legacy_coolergui_name="coolergui-macos-arm64-browser"
    legacy_lsp_name="cooler-lsp-macos-arm64"
    packaged_coolerc_name="coolerc"
    packaged_coolergui_name="coolergui-browser"
    packaged_lsp_name="cooler-lsp"
    package_pattern='cooler-compiler-v*-macos-arm64.zip'
    ;;
  Linux/x86_64)
    legacy_coolerc_name="coolerc-linux-amd64"
    legacy_coolergui_name="coolergui-linux-amd64"
    legacy_lsp_name="cooler-lsp-linux-amd64"
    packaged_coolerc_name="coolerc"
    packaged_coolergui_name="coolergui"
    packaged_lsp_name="cooler-lsp"
    package_pattern='cooler-compiler-v*-linux-amd64.tar.gz'
    ;;
  Linux/aarch64|Linux/arm64)
    legacy_coolerc_name="coolerc-linux-arm64"
    legacy_coolergui_name="coolergui-linux-arm64"
    legacy_lsp_name="cooler-lsp-linux-arm64"
    packaged_coolerc_name="coolerc"
    packaged_coolergui_name="coolergui"
    packaged_lsp_name="cooler-lsp"
    package_pattern='cooler-compiler-v*-linux-arm64.tar.gz'
    ;;
  *)
    printf '[FAIL] unsupported smoke host: %s/%s\n' "$platform" "$architecture" >&2
    exit 1
    ;;
esac

if [[ ! -f "$checksum_manifest" ]]; then
  printf '[FAIL] missing checksum manifest: %s\n' "$checksum_manifest" >&2
  exit 1
fi

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

verify_checksum() {
  local artifact="$1"
  local name expected actual
  name="$(basename "$artifact")"
  expected="$(awk -v name="$name" '$2 == name || $2 == "*" name { print $1; exit }' "$checksum_manifest")"
  if [[ -z "$expected" ]]; then
    printf '[FAIL] %s is absent from %s\n' "$name" "$checksum_manifest" >&2
    return 1
  fi
  actual="$(sha256_file "$artifact")"
  if [[ "$actual" != "$expected" ]]; then
    printf '[FAIL] checksum mismatch for %s\n' "$name" >&2
    return 1
  fi
  printf '[PASS] checksum %s %s\n' "$actual" "$name"
}

if [[ -d "$package_input" ]]; then
  package_dir="$(cd "$package_input" && pwd)"
  coolerc="$package_dir/$legacy_coolerc_name"
  coolergui="$package_dir/$legacy_coolergui_name"
  cooler_lsp="$package_dir/$legacy_lsp_name"
  for artifact in "$coolerc" "$coolergui" "$cooler_lsp"; do
    if [[ ! -f "$artifact" ]]; then
      printf '[FAIL] missing legacy development artifact: %s\n' "$artifact" >&2
      exit 1
    fi
    verify_checksum "$artifact"
  done
  printf '[INFO] qualified legacy raw-binary mode; release evidence must use a package archive\n'
elif [[ -f "$package_input" ]]; then
  package_archive="$(cd "$(dirname "$package_input")" && pwd)/$(basename "$package_input")"
  package_name="$(basename "$package_archive")"
  if [[ "$package_name" != $package_pattern ]]; then
    printf '[FAIL] package %s does not match host %s/%s\n' "$package_name" "$platform" "$architecture" >&2
    exit 1
  fi
  verify_checksum "$package_archive"
  extraction_dir="$smoke_root/package"
  mkdir -p "$extraction_dir"
  case "$package_archive" in
    *.tar.gz) tar -xzf "$package_archive" -C "$extraction_dir" ;;
    *.zip)
      if ! command -v unzip >/dev/null 2>&1; then
        printf '[FAIL] unzip is required for %s\n' "$package_name" >&2
        exit 1
      fi
      unzip -q "$package_archive" -d "$extraction_dir"
      ;;
    *)
      printf '[FAIL] unsupported package archive: %s\n' "$package_name" >&2
      exit 1
      ;;
  esac
  top_level_count="$(find "$extraction_dir" -mindepth 1 -maxdepth 1 -type d -print | wc -l | tr -d '[:space:]')"
  package_dir="$(find "$extraction_dir" -mindepth 1 -maxdepth 1 -type d -print -quit)"
  if [[ "$top_level_count" != "1" || -z "$package_dir" ]]; then
    printf '[FAIL] package must contain exactly one top-level directory\n' >&2
    exit 1
  fi
  coolerc="$package_dir/$packaged_coolerc_name"
  coolergui="$package_dir/$packaged_coolergui_name"
  cooler_lsp="$package_dir/$packaged_lsp_name"
  for artifact in "$coolerc" "$coolergui" "$cooler_lsp"; do
    if [[ ! -f "$artifact" ]]; then
      printf '[FAIL] package omitted expected executable: %s\n' "$artifact" >&2
      exit 1
    fi
  done
else
  printf '[FAIL] package input does not exist: %s\n' "$package_input" >&2
  exit 1
fi
chmod +x "$coolerc" "$coolergui" "$cooler_lsp"

cat >"$smoke_root/success.cl" <<'COOL'
class Main inherits IO { main() : Object { out_string("smoke-ok\n") }; };
COOL
cat >"$smoke_root/semantic-error.cl" <<'COOL'
class Main { main() : Object { missing_identifier }; };
COOL
cat >"$smoke_root/runtime-error.cl" <<'COOL'
class Main { main() : Object { 1 / 0 }; };
COOL
cat >"$smoke_root/helper.cl" <<'COOL'
class Helper inherits IO { echo() : Object { out_string(in_string()) }; };
COOL
cat >"$smoke_root/main.cl" <<'COOL'
class Main { main() : Object { (new Helper).echo() }; };
COOL
printf 'multifile-input\n' >"$smoke_root/input.txt"

"$coolerc" --version | grep -Eq '^coolerc [0-9]+\.[0-9]+\.[0-9]+'
"$cooler_lsp" --version 2>&1 | grep -Eq '^cooler-lsp [0-9]+\.[0-9]+\.[0-9]+'
[[ "$("$coolerc" run "$smoke_root/success.cl")" == "smoke-ok" ]]
if "$coolerc" check "$smoke_root/semantic-error.cl" >"$smoke_root/semantic.out" 2>"$smoke_root/semantic.err"; then
  printf '[FAIL] semantic-error program unexpectedly succeeded\n' >&2
  exit 1
fi
grep -q 'COL3100' "$smoke_root/semantic.err"
if "$coolerc" run "$smoke_root/runtime-error.cl" >"$smoke_root/runtime.out" 2>"$smoke_root/runtime.err"; then
  printf '[FAIL] runtime-error program unexpectedly succeeded\n' >&2
  exit 1
fi
grep -qi 'division by zero' "$smoke_root/runtime.err"
[[ "$("$coolerc" run --input "$smoke_root/input.txt" "$smoke_root/helper.cl" "$smoke_root/main.cl")" == "multifile-input" ]]

for artifact in tokens ast symbols cfg report; do
  "$coolerc" emit "$artifact" "$smoke_root/success.cl" >"$smoke_root/$artifact.json"
  grep -Eq '^[[:space:]]*[\[{]|^null$' "$smoke_root/$artifact.json"
done
for artifact in ir assembly; do
  "$coolerc" emit "$artifact" "$smoke_root/success.cl" >"$smoke_root/$artifact.txt"
  grep -q 'Main.main' "$smoke_root/$artifact.txt"
done
printf '[PASS] CLI success, compile error, runtime error, multifile input, and artifacts\n'
printf '[PASS] language-server executable and version output\n'

"$coolergui" --browser --open=false --addr="127.0.0.1:$smoke_port" >"$smoke_root/gui.log" 2>&1 &
gui_pid="$!"
gui_ready=false
for _ in {1..100}; do
  if ! kill -0 "$gui_pid" 2>/dev/null; then
    printf '[FAIL] browser GUI exited before becoming ready\n' >&2
    cat "$smoke_root/gui.log" >&2
    exit 1
  fi
  gui_url="$(sed -n 's/^Cooler Compiler at \(http:\/\/[^[:space:]]*\)$/\1/p' "$smoke_root/gui.log" | tail -n 1)"
  if [[ -n "$gui_url" ]] && curl --fail --silent "$gui_url/api/health" >"$smoke_root/health.json"; then
    gui_ready=true
    break
  fi
  sleep 0.1
done
if [[ "$gui_ready" != true ]]; then
  printf '[FAIL] browser GUI did not become ready\n' >&2
  exit 1
fi
curl --fail --silent --dump-header "$smoke_root/headers.txt" --output "$smoke_root/index.html" "$gui_url/"
grep -qi '^Content-Security-Policy:' "$smoke_root/headers.txt"
grep -q 'Cooler Compiler' "$smoke_root/index.html"
printf '[PASS] browser GUI served health and secured application HTML\n'

if [[ "$platform" == Darwin && -d "$package_dir/Cooler Compiler.app" ]]; then
  bundle_id="com.thalesmms.cooler-compiler"
  running_info="$(lsappinfo info -only pid -app "$bundle_id" 2>/dev/null || true)"
  if [[ "$running_info" == *'"pid"='* ]]; then
    printf '[FAIL] an application with bundle identifier %s is already running\n' "$bundle_id" >&2
    exit 1
  fi
  open -n "$package_dir/Cooler Compiler.app"
  for _ in {1..100}; do
    running_info="$(lsappinfo info -only pid -app "$bundle_id" 2>/dev/null || true)"
    native_pid="$(sed -n 's/.*"pid"=\([0-9][0-9]*\).*/\1/p' <<<"$running_info")"
    [[ -n "$native_pid" ]] && break
    sleep 0.1
  done
  if [[ -z "$native_pid" ]]; then
    printf '[FAIL] packaged native macOS app did not launch\n' >&2
    exit 1
  fi
  osascript -e "tell application id \"$bundle_id\" to quit"
  for _ in {1..100}; do
    if ! kill -0 "$native_pid" 2>/dev/null; then
      native_pid=""
      printf '[PASS] packaged native macOS app launch and Apple Quit event\n'
      break
    fi
    sleep 0.1
  done
  if [[ -n "$native_pid" ]]; then
    printf '[FAIL] packaged native macOS app remained running after Apple Quit event\n' >&2
    exit 1
  fi
fi

if [[ "$platform" == Linux ]] && command -v ldd >/dev/null 2>&1; then
  if ldd "$coolerc" >"$smoke_root/ldd-coolerc.txt" 2>&1; then
    printf '[INFO] coolerc shared-library report:\n'
    sed -n '1,40p' "$smoke_root/ldd-coolerc.txt"
  else
    grep -Eqi 'not a dynamic executable|statically linked' "$smoke_root/ldd-coolerc.txt"
    printf '[PASS] coolerc has no dynamic shared-library dependency\n'
  fi
fi

printf '[MANUAL] exercise debugger controls and JSON/text/ZIP downloads in a real browser\n'
printf '[MANUAL] exercise native GUI controls and Command-Q when this package includes an app\n'
printf '[PASS] automated runtime smoke completed on %s/%s\n' "$platform" "$architecture"
