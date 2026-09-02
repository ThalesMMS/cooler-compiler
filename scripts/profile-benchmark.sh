#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

target="${1:-compile-large}"
duration="${2:-3s}"
output_dir="${3:-dist/profiles}"

case "$target" in
  compile-large)
    package="./internal/compiler"
    benchmark="^BenchmarkCompileComplete/large$"
    ;;
  vm-trace)
    package="./internal/compiler"
    benchmark="^BenchmarkVMTraceModes/retained$"
    ;;
  api-compile)
    package="./cmd/coolergui"
    benchmark="^BenchmarkCompileAPI/medium$"
    ;;
  *)
    printf 'unknown profile target %q; use compile-large, vm-trace, or api-compile\n' "$target" >&2
    exit 2
    ;;
esac

if [[ ! "$duration" =~ ^[1-9][0-9]*(ns|us|ms|s|m|h)$ ]]; then
  printf 'invalid profile duration %q\n' "$duration" >&2
  exit 2
fi

mkdir -p "$output_dir"
cpu_profile="$output_dir/$target.cpu.pprof"
memory_profile="$output_dir/$target.mem.pprof"
test_binary="$output_dir/$target.test"

go test "$package" -run '^$' -bench "$benchmark" -benchtime "$duration" \
  -cpuprofile "$cpu_profile" -memprofile "$memory_profile" -o "$test_binary"

printf 'CPU profile: %s\n' "$cpu_profile"
printf 'Memory profile: %s\n' "$memory_profile"
printf 'Profile binary: %s\n' "$test_binary"
printf 'Inspect with: go tool pprof -http=:0 %q\n' "$cpu_profile"
