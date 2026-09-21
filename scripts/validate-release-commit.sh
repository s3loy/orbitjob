#!/usr/bin/env bash

# A formal release must describe the exact commit currently selected by the
# repository's default branch. Accepting any ancestor would permit tagging an
# obsolete release; accepting an unrelated commit would bypass the main PR.

set -euo pipefail

release_ref=${1:-}
main_ref=${2:-}
if [[ -z $release_ref || -z $main_ref ]]; then
  printf 'usage: %s RELEASE_REF MAIN_REF\n' "$0" >&2
  exit 2
fi

release_sha=$(git rev-parse --verify "${release_ref}^{commit}") || exit 1
main_sha=$(git rev-parse --verify "${main_ref}^{commit}") || exit 1
if [[ $release_sha != "$main_sha" ]]; then
  printf 'release commit %s is not the current main commit %s\n' "$release_sha" "$main_sha" >&2
  exit 1
fi
