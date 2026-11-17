#!/usr/bin/env bash
set -euo pipefail

if [[ $# != 1 || -z $1 ]]; then
  echo 'Usage: fetch-benchmark-history.sh ARCHIVE_DIRECTORY' >&2
  exit 2
fi

cd "$(dirname "${BASH_SOURCE[0]}")/../.."
ref=refs/heads/gh-pages
if git ls-remote --exit-code --heads origin "$ref" >/dev/null; then
  git fetch --depth=1 --no-tags origin "$ref" >&2
else
  status=$?
  if [[ $status == 2 ]]; then
    echo 'Benchmark history branch does not exist.' >&2
    exit 0
  fi
  exit "$status"
fi

index=$(git ls-tree --name-only FETCH_HEAD -- dev/bench/index.html)
if [[ -z $index ]]; then
  echo 'Benchmark history has no archive index yet.' >&2
  exit 0
fi

mkdir -p -- "$1"
destination=$(cd -- "$1" && pwd -P)
if [[ -e $destination/dev || -L $destination/dev ]]; then
  echo 'Benchmark extraction requires a fresh destination.' >&2
  exit 1
fi
git archive FETCH_HEAD dev/bench | tar -xf - -C "$destination"
printf '%s\n' "$destination/dev/bench"
