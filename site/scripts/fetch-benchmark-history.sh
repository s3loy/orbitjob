#!/usr/bin/env bash
set -euo pipefail

if [[ $# != 1 || -z $1 ]]; then
  echo 'Usage: fetch-benchmark-history.sh ARCHIVE_DIRECTORY' >&2
  exit 2
fi

cd "$(dirname "${BASH_SOURCE[0]}")/../.."
ref=refs/heads/gh-pages
remote_ref=$(git ls-remote --heads origin "$ref")
if [[ -z $remote_ref ]]; then
  echo 'Benchmark history branch does not exist.' >&2
  exit 0
fi

# Use a private ref so other fetches in this checkout cannot change the archive.
ref_token=$(mktemp -d)
snapshot="refs/orbitjob/benchmark-history/${ref_token##*/}"
cleanup() {
  git update-ref -d "$snapshot"
  rmdir -- "$ref_token"
}
trap cleanup EXIT
git fetch --depth=1 --no-tags --no-write-fetch-head --refmap= origin "$ref:$snapshot" >&2
revision=$(git rev-parse --verify "$snapshot^{commit}")
archive=$(git ls-tree --name-only "$revision" -- dev/bench)
if [[ -z $archive ]]; then
  echo 'Benchmark history has no archive yet.' >&2
  exit 0
fi

# The history branch is independent of the site source. Verify the file types
# and names in its tree before copying anything out of it.
entries=$(git ls-tree "$revision" -- dev/bench/ | sed -E $'s/^100(644|755) blob [0-9a-f]+\t/100644 blob /')
if [[ $entries != $'100644 blob dev/bench/data.js\n100644 blob dev/bench/index.html' ]]; then
  echo 'Benchmark history must contain only regular files data.js and index.html.' >&2
  exit 1
fi

mkdir -p -- "$1"
destination=$(cd -- "$1" && pwd -P)
if [[ -e $destination/dev || -L $destination/dev ]]; then
  echo 'Benchmark extraction requires a fresh destination.' >&2
  exit 1
fi
mkdir -p -- "$destination/dev/bench"
for name in data.js index.html; do
  git cat-file blob "$revision:dev/bench/$name" > "$destination/dev/bench/$name"
done
printf '%s\n' "$destination/dev/bench"
