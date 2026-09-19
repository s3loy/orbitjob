#!/usr/bin/env bash
# chart-gate gate: any chart change must still lint, render and satisfy the
# chart assertions before it can be committed. `make helm-check` chains the
# migrations sync check, `helm lint`, `helm template` and chart-assert.sh.
set -euo pipefail

staged=$(git -c core.quotePath=false diff --cached --name-only --diff-filter=ACM)
if [ -z "$staged" ]; then
	exit 0
fi

triggered=$(printf '%s\n' "$staged" | grep -E '^charts/' || true)
if [ -z "$triggered" ]; then
	exit 0
fi

echo "Chart touched; running the Helm gate:"
printf '%s\n' "$triggered"
exec make helm-check
