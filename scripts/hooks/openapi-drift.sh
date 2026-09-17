#!/usr/bin/env bash
# openapi-drift gate: when anything on the admin API surface moves, the
# generated spec must still match the tree. `make openapi-check` regenerates
# api/openapi.yaml from the code and fails on a diff.
set -euo pipefail

staged=$(git -c core.quotePath=false diff --cached --name-only --diff-filter=ACM)
if [ -z "$staged" ]; then
	exit 0
fi

triggered=$(printf '%s\n' "$staged" |
	grep -E '^api/openapi\.yaml$|^internal/admin/http/|^internal/core/domain/[^/]+/create_input\.go$' || true)
if [ -z "$triggered" ]; then
	exit 0
fi

echo "Admin API surface touched; checking the generated spec:"
printf '%s\n' "$triggered"
exec make openapi-check
