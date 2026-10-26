#!/usr/bin/env bash
# Publish a fresh benchmark artifact only after a successful measured run.
set -euo pipefail

if [[ $# -eq 0 ]]; then
    printf 'at least one benchmark package is required\n' >&2
    exit 2
fi

# A failed run must not leave a stale result for a later upload/comparison.
rm -f bench.txt
measurement=$(mktemp)
trap 'rm -f "$measurement"' EXIT
go test -run='^$' -bench=. -benchmem "$@" | tee "$measurement"
if ! LC_ALL=C grep -Eq '^Benchmark[^[:space:]]+[[:space:]]+[0-9]+[[:space:]]+[0-9]+([.][0-9]+)?[[:space:]]+ns/op([[:space:]]|$)' "$measurement"; then
    printf 'benchmark run produced no measurements; bench.txt was not published\n' >&2
    exit 1
fi
mv "$measurement" bench.txt
