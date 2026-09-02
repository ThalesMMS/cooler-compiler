#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temp_paths=()
internal_only_paths=(
  docs/development-publication.md
  scripts/publication-sync.sh
  scripts/publication_sync_test.go
  examples/13_stack_machine.cl
  examples/stack-machine/atoi.cl
  examples/stack-machine/stack.test
)
internal_only_exclusions=()
for internal_only_path in "${internal_only_paths[@]}"; do
  internal_only_exclusions+=(":(exclude,top)$internal_only_path")
done
unset internal_only_path

cleanup() {
  local path
  for path in "${temp_paths[@]}"; do
    if [[ -d "$path" && "$(basename "$path")" == .cooler-public-* ]]; then
      find "$path" -depth -delete
    elif [[ -f "$path" && "$(basename "$path")" == .cooler-public-* ]]; then
      rm -f -- "$path"
    fi
  done
}
trap cleanup EXIT

usage() {
  cat <<'EOF'
Usage:
  scripts/publication-sync.sh export <development-commit> <new-output-directory>
  scripts/publication-sync.sh compare <development-commit> <public-repository> [public-ref]

The export command creates a public candidate plus an adjacent
<new-output-directory>.publication-source provenance file. The compare command
checks the filtered development snapshot against a commit in a local clone of
the public repository; public-ref defaults to HEAD.
EOF
}

resolve_development_commit() {
  git -C "$repo_root" rev-parse --verify "$1^{commit}"
}

write_filtered_archive() {
  local commit="$1"
  local archive="$2"

  # These files define/test the private publication process or contain teaching
  # material without a verified redistribution grant. They are intentionally
  # absent from the sanitized public snapshot.
  git -C "$repo_root" archive --format=tar "$commit" -- \
    . \
    "${internal_only_exclusions[@]}" \
    >"$archive"
}

extract_development_candidate() {
  local commit="$1"
  local destination="$2"
  local archive="$3"

  mkdir -p "$(dirname "$destination")"
  local staging
  staging="$(mktemp -d "$(dirname "$destination")/.cooler-public-export.XXXXXX")"
  temp_paths+=("$staging")

  tar -xf "$archive" -C "$staging"

  if find "$staging" -name .git -print -quit | grep -q .; then
    printf 'refusing export: candidate contains a .git path\n' >&2
    return 1
  fi
  local internal_path
  for internal_path in "${internal_only_paths[@]}"; do
    if [[ -e "$staging/$internal_path" ]]; then
      printf 'refusing export: candidate contains internal-only path %s\n' "$internal_path" >&2
      return 1
    fi
  done

  mv "$staging" "$destination"
}

export_candidate() {
  local requested_commit="$1"
  local destination="$2"
  local commit
  commit="$(resolve_development_commit "$requested_commit")"

  if [[ -e "$destination" ]]; then
    printf 'refusing export: destination already exists: %s\n' "$destination" >&2
    return 1
  fi
  if [[ -e "$destination.publication-source" ]]; then
    printf 'refusing export: provenance file already exists: %s\n' "$destination.publication-source" >&2
    return 1
  fi

  local archive
  archive="$(mktemp "${TMPDIR:-/tmp}/.cooler-public-archive.XXXXXX")"
  temp_paths+=("$archive")
  write_filtered_archive "$commit" "$archive"
  local archive_object
  archive_object="$(git -C "$repo_root" hash-object "$archive")"

  extract_development_candidate "$commit" "$destination" "$archive"
  {
    printf 'format=cooler-publication-source-v1\n'
    printf 'source_repository=https://github.com/ThalesMMS/cooler-compiler-dev.git\n'
    printf 'source_commit=%s\n' "$commit"
    printf 'archive_object=%s\n' "$archive_object"
  } >"$destination.publication-source"

  printf 'Exported public candidate: %s\n' "$destination"
  printf 'Provenance: %s\n' "$destination.publication-source"
  printf 'Development commit: %s\n' "$commit"
  printf 'Archive object: %s\n' "$archive_object"
}

compare_snapshot() {
  local requested_commit="$1"
  local public_repository="$2"
  local public_ref="${3:-HEAD}"
  local commit
  commit="$(resolve_development_commit "$requested_commit")"

  if ! git -C "$public_repository" rev-parse --git-dir >/dev/null 2>&1; then
    printf 'not a Git repository: %s\n' "$public_repository" >&2
    return 1
  fi
  local public_commit
  public_commit="$(git -C "$public_repository" rev-parse --verify "$public_ref^{commit}")"

  local comparison_root
  comparison_root="$(mktemp -d "${TMPDIR:-/tmp}/.cooler-public-compare.XXXXXX")"
  temp_paths+=("$comparison_root")
  mkdir "$comparison_root/development" "$comparison_root/public"

  local archive="$comparison_root/development.tar"
  write_filtered_archive "$commit" "$archive"
  tar -xf "$archive" -C "$comparison_root/development"
  git -C "$public_repository" archive --format=tar "$public_commit" | \
    tar -xf - -C "$comparison_root/public"

  if ! diff -ruN "$comparison_root/development" "$comparison_root/public"; then
    printf 'public snapshot does not match development commit %s\n' "$commit" >&2
    return 1
  fi

  git -C "$repo_root" ls-tree -r "$commit" -- . | awk -F '\t' '
    BEGIN {
      for (i = 1; i < ARGC; i++) {
        excluded[ARGV[i]] = 1
        delete ARGV[i]
      }
    }
    !($2 in excluded) {
      split($1, metadata, " "); print metadata[1] "\t" $2
    }
  ' "${internal_only_paths[@]}" >"$comparison_root/development-modes"
  git -C "$public_repository" ls-tree -r "$public_commit" -- . | awk -F '\t' '{
	split($1, metadata, " "); print metadata[1] "\t" $2
  }' >"$comparison_root/public-modes"
  if ! diff -u "$comparison_root/development-modes" "$comparison_root/public-modes"; then
	printf 'public snapshot file modes do not match development commit %s\n' "$commit" >&2
	return 1
  fi

  printf 'Public snapshot matches exactly.\n'
  printf 'Development commit: %s\n' "$commit"
  printf 'Public commit: %s\n' "$public_commit"
}

if [[ "$#" -lt 1 ]]; then
  usage >&2
  exit 2
fi

command_name="$1"
shift
case "$command_name" in
  export)
    if [[ "$#" -ne 2 ]]; then
      usage >&2
      exit 2
    fi
    export_candidate "$1" "$2"
    ;;
  compare)
    if [[ "$#" -lt 2 || "$#" -gt 3 ]]; then
      usage >&2
      exit 2
    fi
    compare_snapshot "$@"
    ;;
  *)
    usage >&2
    exit 2
    ;;
esac
