#!/usr/bin/env bash
set -euo pipefail

mode=${1:-}
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
source_dir="$root/db/migrations"
target_dir="$root/charts/orbitjob/migrations"

list_names() {
  find "$1" -maxdepth 1 -type f -name '*.up.sql' -exec basename {} \; | LC_ALL=C sort
}

sync_migrations() {
  count=$(list_names "$source_dir" | wc -l | tr -d ' ')
  if [[ $count == 0 ]]; then
    printf 'no up migrations found in %s\n' "$source_dir" >&2
    exit 1
  fi
  find "$target_dir" -maxdepth 1 -type f -name '*.up.sql' -delete
  list_names "$source_dir" | while IFS= read -r name; do
    cp "$source_dir/$name" "$target_dir/$name"
  done
}

check_migrations() {
  source_list=$(mktemp)
  target_list=$(mktemp)
  trap 'rm -f "$source_list" "$target_list"' EXIT
  list_names "$source_dir" >"$source_list"
  list_names "$target_dir" >"$target_list"
  if [[ ! -s $source_list ]]; then
    printf 'no up migrations found in %s\n' "$source_dir" >&2
    exit 1
  fi
  if ! diff -u "$source_list" "$target_list"; then
    printf 'Chart migration file set differs from db/migrations.\n' >&2
    exit 1
  fi
  while IFS= read -r name; do
    if ! cmp -s "$source_dir/$name" "$target_dir/$name"; then
      printf 'migration content mismatch: %s\n' "$name" >&2
      exit 1
    fi
  done <"$source_list"
  printf 'Chart migrations match db/migrations (%s files).\n' "$(wc -l <"$source_list" | tr -d ' ')"
}

case "$mode" in
  sync) sync_migrations ;;
  check) check_migrations ;;
  *) printf 'usage: %s {sync|check}\n' "$0" >&2; exit 2 ;;
esac
