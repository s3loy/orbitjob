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
# A deployment can grow beyond any fixed grep context window. Inspect each
# complete Deployment document so later env entries cannot escape the gate.
runtime=$(awk '/^kind: / { deployment = ($0 == "kind: Deployment") } deployment' "$rendered")
[[ -n $runtime ]] || { echo 'no runtime deployments rendered' >&2; exit 1; }
if grep -q 'bootstrap-owner-dsn' <<<"$runtime"; then
  echo 'runtime deployment references bootstrap owner credential' >&2
  exit 1
fi

helm template orbitjob "$chart" --show-only templates/operator.yaml \
  --set operator.namespaceTenants="default=00000000000000000000000001" \
  --set database.secretName=custom-database \
  --set database.keys.runtime=custom-runtime >"$rendered"
grep -q 'name: custom-database' "$rendered"
grep -q 'key: custom-runtime' "$rendered"
