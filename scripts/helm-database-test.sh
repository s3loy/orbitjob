#!/usr/bin/env bash
set -euo pipefail

chart=${1:-charts/orbitjob}
rendered=$(mktemp)
trap 'rm -f "$rendered"' EXIT
# operator.namespaceTenants is required; this script only inspects the
# rendered database wiring, so any valid mapping will do.
helm template orbitjob "$chart" \
  --set operator.namespaceTenants="default=00000000000000000000000001" >"$rendered"

if grep -q 'operator-password' "$chart/values.schema.json"; then
  echo 'values schema still requires operator/password duplication' >&2
  exit 1
fi
for key in bootstrap-owner-dsn migrator-dsn admin-dsn runtime-dsn; do
  grep -q "key: $key" "$rendered"
done
runtime=$(grep -A80 'kind: Deployment' "$rendered")
if grep -q 'bootstrap-owner-dsn' <<<"$runtime"; then
  echo 'runtime deployment references bootstrap owner credential' >&2
  exit 1
fi
