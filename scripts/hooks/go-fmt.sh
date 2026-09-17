#!/usr/bin/env bash
# go-fmt gate: gofmt exactly the staged Go files, then exit 1 so the committer
# re-stages them (pre-commit's fix-and-retry pattern). Enumerating the index
# keeps the gate staged-files-only — a whole-tree `find` would reformat files
# that belong to concurrent workstreams and dirty unstaged changes.
set -euo pipefail

staged=$(git -c core.quotePath=false diff --cached --name-only --diff-filter=ACM -- '*.go')
if [ -z "$staged" ]; then
	exit 0
fi

rewrote=0
while IFS= read -r f; do
	[ -f "$f" ] || continue
	if [ -n "$(gofmt -l "$f")" ]; then
		gofmt -w "$f"
		echo "gofmt reformatted: $f"
		rewrote=1
	fi
done <<<"$staged"

if [ "$rewrote" -eq 1 ]; then
	echo "Re-stage the files above (git add <file>) and commit again."
	exit 1
fi
