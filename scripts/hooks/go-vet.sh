#!/usr/bin/env bash
# go-vet gate: vet only the packages that contain staged Go files. Scoping to
# those packages keeps unrelated in-flight work from failing your commit; the
# full `go vet ./...` stays in CI (make vet / make check).
set -euo pipefail

staged=$(git -c core.quotePath=false diff --cached --name-only --diff-filter=ACM -- '*.go')
if [ -z "$staged" ]; then
	exit 0
fi

# One package per staged file (its directory), de-duplicated. A file at the
# module root has no directory component, and dirname maps it to ".".
pkgs=$(printf '%s\n' "$staged" |
	while IFS= read -r f; do
		dirname "$f"
	done |
	sort -u)
if [ -z "$pkgs" ]; then
	exit 0
fi

args=()
while IFS= read -r d; do
	args+=("./$d")
done <<<"$pkgs"

echo "go vet ${args[*]}"
exec go vet "${args[@]}"
