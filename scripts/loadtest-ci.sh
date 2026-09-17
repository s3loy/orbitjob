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
orbitjob_dsn="$(kubectl get secret "$SECRET_NAME" -n "$RELEASE_NAMESPACE" \
    -o jsonpath='{.data.bootstrap-owner-dsn}' | base64 -d)"
# The Secret holds the in-cluster Service address; this host reaches PostgreSQL
# through a port-forward instead.
orbitjob_dsn="${orbitjob_dsn/orbitjob-postgres.orbitjob.svc:5432/127.0.0.1:${DB_PORT}}"
bootstrap_key="$(kubectl get secret "$SECRET_NAME" -n "$RELEASE_NAMESPACE" \
    -o jsonpath='{.data.bootstrap-api-key}' | base64 -d)"
[[ -n $orbitjob_dsn && -n $bootstrap_key ]] || die "the installation Secret is missing its DSN or API key"
export ORBITJOB_API_KEY="$bootstrap_key"
# The verifier reads this when --orbitjob-dsn is absent. Keeping the DSN out of
# argv means its password is not in the process table.
export ORBITJOB_DSN="$orbitjob_dsn"

# --- fixtures ----------------------------------------------------------------

log "deploying load fixtures"
kubectl apply -f deploy/load/namespace.yaml >/dev/null
kubectl apply -f deploy/load/operations-rbac.yaml >/dev/null
kubectl apply -f deploy/load/fixture-configmap.yaml >/dev/null
# The TLS secret is generated rather than committed, so the private key never
# enters git history.
bash deploy/load/make-tls-cert.sh >/dev/null
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
    in_flight="$(psql "$orbitjob_dsn" -Atqc \
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
