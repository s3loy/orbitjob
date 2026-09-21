#!/usr/bin/env bash
# no-artifacts gate: build outputs and load-test debris never get committed.
# .gitignore already covers these names; this is defense in depth for
# `git add -f` and for names .gitignore forgot.
set -euo pipefail

staged=$(git -c core.quotePath=false diff --cached --name-only --diff-filter=ACM)
if [ -z "$staged" ]; then
	exit 0
fi

hits=$(printf '%s\n' "$staged" |
	grep -E '^(operator|orbitjob|smoke-test-results\.md|bench\.txt|bench-[^/]*\.txt)$' || true)
if [ -z "$hits" ]; then
	exit 0
fi

echo "Refusing to commit build or load-test artifacts:"
printf '%s\n' "$hits"
echo "Unstage them: git rm --cached <file>"
exit 1
