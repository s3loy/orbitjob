#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
SITE_ORIGIN=${SITE_ORIGIN-${CF_PAGES_URL-}}
if [[ -z $SITE_ORIGIN ]]; then
  echo 'Set SITE_ORIGIN to the production HTTPS origin or provide CF_PAGES_URL.' >&2
  exit 1
fi
export SITE_ORIGIN
export SITE_BASE=/

history_dir=$(mktemp -d)
trap 'rm -rf -- "$history_dir"' EXIT
BENCHMARK_ARCHIVE=$(bash scripts/fetch-benchmark-history.sh "$history_dir")
export BENCHMARK_ARCHIVE
npm run build
