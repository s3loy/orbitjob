#!/usr/bin/env bash
#
# Retired. This benchmark measured the removed POST /api/v1/jobs creation
# flow and the old dispatcher/worker pipeline (trigger -> dispatch -> worker
# claim latency, status=pending dispatch rates). None of that exists on the
# Kubernetes control plane: jobs are declared as ScheduledJob CRs, the API
# only lists/gets/triggers them, and the operator runs them as Kubernetes
# Jobs. It also queried instances?status= filters and a tenant DELETE
# endpoint that no longer exist.
#
# The useful remaining measurements (trigger latency, list-query latency,
# multi-tenant concurrency, full run lifecycle) are covered by the Go load
# test tool on the ScheduledJob-CR flow:
#
#   go run ./scripts/loadtest --help
#   scripts/loadtest-ci.sh smoke
#   See docs/loadtest-guide.md.
#
# This stub exits non-zero on purpose so scripts or habits cannot quietly
# exercise dead endpoints.

echo "[FAIL] scripts/bench-load.sh is retired: it benchmarks the removed POST /api/v1/jobs flow and the dispatcher/worker pipeline." >&2
echo "       Use the ScheduledJob-CR load test instead: go run ./scripts/loadtest --help (or scripts/loadtest-ci.sh smoke; see docs/loadtest-guide.md)." >&2
exit 1
