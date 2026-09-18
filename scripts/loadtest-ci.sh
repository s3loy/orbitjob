#!/usr/bin/env bash
#
# Runs one load-test profile against the local kind cluster, end to end.
#
# This is the sequence docs/loadtest-guide.md walks a human through, with the
# port-forwards and the settling wait that a person does by hand. It is here
# rather than inline in the workflow so the same script can be run on a
# developer's machine, and so the workflow stays plumbing.
#
# It assumes the installation already exists: run scripts/quickstart.sh first.
#
# Usage: scripts/loadtest-ci.sh [smoke|standard]
#
# A profile is a wall-clock commitment. smoke is about 40 minutes including
# setup and verify. standard is four hours of schedule plus setup, settle and
# verify, and needs a runner that will not be reclaimed underneath it.

set -euo pipefail

readonly PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly RELEASE_NAMESPACE="${ORBITJOB_NAMESPACE:-orbitjob-system}"
readonly DB_NAMESPACE="${ORBITJOB_DB_NAMESPACE:-orbitjob}"
readonly LOAD_NAMESPACE="orbitjob-load"
# Every namespace the load tool creates carries this label, so the grant step
# below and reset's cleanup discover the same set without hardcoding names.
readonly LOAD_NAMESPACE_LABEL="orbitjob.io/managed-by=orbitjob-loadtest"
readonly ADMIN_PORT=18080
readonly FIXTURE_PORT=18081
readonly DB_PORT=15432
readonly DB_DEPLOYMENT="orbitjob-postgres"
readonly SECRET_NAME="orbitjob-database"

log() { printf '[loadtest-ci] %s\n' "$*" >&2; }
die() { printf '[loadtest-ci] error: %s\n' "$*" >&2; exit 1; }

profile="${1:-smoke}"
case "$profile" in
    smoke) config="test/load/config/smoke.yaml" ;;
    standard) config="test/load/config/standard.yaml" ;;
    *) die "unknown profile $profile: smoke|standard" ;;
esac

cd "$PROJECT_ROOT"

run_id="ci-${profile}-$(date -u +%Y%m%dT%H%M%SZ)"
run_dir="test/load/runs/${run_id}"
log "run $run_id using $config"

# --- port-forwards -----------------------------------------------------------
#
# The workloads reach the fixture over its in-cluster Service. These forwards
# exist for the two things that run on this host: the load generator talking to
# the Admin API, and the verifier reading the fixture ledger and the database.

forward_pids=()
cleanup() {
    for pid in "${forward_pids[@]:-}"; do
        [[ -n $pid ]] && kill "$pid" 2>/dev/null || true
    done
}
trap cleanup EXIT

forward() {
    local description="$1" namespace="$2" target="$3" mapping="$4"
    kubectl port-forward -n "$namespace" "$target" "$mapping" >"/tmp/pf-${mapping%%:*}.log" 2>&1 &
    forward_pids+=("$!")
    log "forwarding $description on ${mapping%%:*}"
}

wait_for_port() {
    local port="$1" name="$2"
    for _ in $(seq 1 30); do
        if nc -z 127.0.0.1 "$port" 2>/dev/null; then return 0; fi
        sleep 1
    done
    cat "/tmp/pf-${port}.log" >&2 || true
    die "$name port-forward on $port never became ready"
}

# --- credentials -------------------------------------------------------------

log "reading installation credentials"
admin_dsn="$(kubectl get secret "$SECRET_NAME" -n "$RELEASE_NAMESPACE" \
    -o jsonpath='{.data.admin-dsn}' | base64 -d)"
# The Secret holds the in-cluster Service address; this host reaches PostgreSQL
# through a port-forward instead.
admin_dsn="${admin_dsn/orbitjob-postgres.orbitjob.svc:5432/127.0.0.1:${DB_PORT}}"
bootstrap_key="$(kubectl get secret "$SECRET_NAME" -n "$RELEASE_NAMESPACE" \
    -o jsonpath='{.data.bootstrap-api-key}' | base64 -d)"
[[ -n $admin_dsn && -n $bootstrap_key ]] || die "the installation Secret is missing its DSN or API key"
export ORBITJOB_API_KEY="$bootstrap_key"
# The verifier and the settle wait read the ledger as the SELECT-only admin
# identity, which is exactly the read they perform. Roles are NOINHERIT and
# orbitjob_bootstrap holds no table grants, so the owner DSN this script used
# before got a 42501 on the first evidence query and the settle wait reported
# "unknown" for every iteration (CI run 35380354290). Keeping the DSN out of
# argv means its password is not in the process table.
export ORBITJOB_DSN="$admin_dsn"

# --- fixtures ----------------------------------------------------------------

log "deploying load fixtures"
kubectl apply -f deploy/load/namespace.yaml >/dev/null
kubectl apply -f deploy/load/operations-rbac.yaml >/dev/null
kubectl apply -f deploy/load/fixture-configmap.yaml >/dev/null
kubectl apply -f deploy/load/fixture-tls-secret.yaml >/dev/null
kubectl apply -f deploy/load/fixture.yaml >/dev/null
kubectl apply -f deploy/load/postgres.yaml >/dev/null

# A ConfigMap or Secret change does not restart the pods that mount it, so on a
# cluster that already ran a load test the fixture would keep serving the old
# server code and the old certificate. Applying is not deploying.
kubectl rollout restart deployment/load-fixture -n "$LOAD_NAMESPACE" >/dev/null
kubectl rollout status deployment/load-fixture -n "$LOAD_NAMESPACE" --timeout=5m
kubectl rollout status deployment/load-postgres -n "$LOAD_NAMESPACE" --timeout=5m

forward "admin api" "$RELEASE_NAMESPACE" "svc/orbitjob-admin-api" "${ADMIN_PORT}:8080"
forward "load fixture" "$LOAD_NAMESPACE" "svc/load-fixture" "${FIXTURE_PORT}:8080"
forward "postgres" "$DB_NAMESPACE" "deployment/$DB_DEPLOYMENT" "${DB_PORT}:5432"
for pair in "$ADMIN_PORT:admin api" "$FIXTURE_PORT:load fixture" "$DB_PORT:postgres"; do
    wait_for_port "${pair%%:*}" "${pair#*:}"
done

[[ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 "http://localhost:${ADMIN_PORT}/healthz")" == "200" ]] \
    || die "the Admin API is not answering on ${ADMIN_PORT}"
[[ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 "http://localhost:${FIXTURE_PORT}/ledger")" == "200" ]] \
    || die "the load fixture is not answering on ${FIXTURE_PORT}"

# --- the run -----------------------------------------------------------------

log "resetting previous run state"
go run ./scripts/loadtest reset --confirm

log "generating definitions"
go run ./scripts/loadtest generate --config "$config" \
    --images test/load/config/images.lock.yaml --run-id "$run_id"

log "preparing tenants and definitions"
go run ./scripts/loadtest prepare --config "$config" --profile "$profile" \
    --run-id "$run_id" --api-url "http://localhost:${ADMIN_PORT}"

# The chart grants the admin API its CR surface one scheduling namespace at a
# time, keyed by operator.namespaceTenants -- and prepare created the load
# tenants' namespaces after the installation was charted. Without a Role
# there, every manual trigger's JobRun publish is denied: CI run 35380354290
# rejected all 500 triggers with 500 and not one run executed. Mirror the
# chart's per-namespace Role and binding for the run's own namespaces (same
# rules, same identity), scoped to namespaces this tool created and labeled;
# reset deletes them again. Widening a real installation stays a helm
# upgrade; this is fixture plumbing, not a production grant.
log "granting the admin api its trigger surface in the load namespaces"
load_namespaces="$(kubectl get namespace \
    -l "$LOAD_NAMESPACE_LABEL" -o jsonpath='{.items[*].metadata.name}')"
[[ -n $load_namespaces ]] || die "prepare produced no load namespaces to grant"
for namespace in $load_namespaces; do
    kubectl apply -n "$namespace" -f - >/dev/null <<MANIFEST
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: orbitjob-admin-api
rules:
  - apiGroups: ["workloads.orbitjob.io"]
    resources: ["jobruns"]
    verbs: ["create", "get", "patch"]
  - apiGroups: ["workloads.orbitjob.io"]
    resources: ["workflowruns"]
    verbs: ["create", "get", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: orbitjob-admin-api
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: Role, name: orbitjob-admin-api}
subjects:
  - kind: ServiceAccount
    name: orbitjob-admin-api
    namespace: ${RELEASE_NAMESPACE}
MANIFEST
    log "granted $namespace"
done

log "running the schedule; this is the part that takes $profile's duration"
go run ./scripts/loadtest run --config "$config" --profile "$profile" \
    --run-id "$run_id" --api-url "http://localhost:${ADMIN_PORT}"

# Runs the operator has not yet carried to a terminal phase count as in flight,
# and verifying immediately after the run fails spuriously on them: the last
# Kubernetes Jobs can take a few seconds to be observed after the schedule
# ends.
log "waiting for runs to reach a terminal state"
settled=false
for _ in $(seq 1 60); do
    # Through the DSN, not `kubectl exec` with a hardcoded password: the
    # credential is already in the installation Secret and this reads it from
    # there rather than repeating a fixture password in a second place.
    #
    # Only an integer is allowed out of this. A psql failure can quote the
    # connection string back, and this value ends up in a log line, so anything
    # that is not a count is discarded rather than echoed.
    in_flight="$(psql "$admin_dsn" -Atqc \
        "SELECT count(*) FROM job_run_control_plane WHERE phase NOT IN ('Succeeded','Failed','Canceled')" \
        2>/dev/null)" || in_flight=""
    [[ $in_flight =~ ^[0-9]+$ ]] || in_flight="unknown"
    if [[ $in_flight == "0" ]]; then settled=true; break; fi
    log "in flight: $in_flight"
    sleep 15
done
$settled || log "WARN runs were still in flight when the wait ran out; the verdict will say so"

log "verifying"
go run ./scripts/loadtest verify --run-id "$run_id" --config "$config" \
    --api-url "http://localhost:${ADMIN_PORT}" \
    --fixture-url "http://localhost:${FIXTURE_PORT}"

log "writing the report"
go run ./scripts/loadtest report --run-id "$run_id"

log "artifacts in $run_dir"
