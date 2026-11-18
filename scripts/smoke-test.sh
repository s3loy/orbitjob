#!/usr/bin/env bash
# OrbitJob end-to-end smoke test
# Usage: ./scripts/smoke-test.sh [API_BASE_URL]
# Default API_BASE_URL=http://localhost:8080 (for a kind cluster, port-forward first:
#   kubectl -n orbitjob-system port-forward svc/orbitjob-admin-api 8080:8080)
#
# The script submits two temporary ScheduledJobs through the checked definition
# API in $SMOKE_JOB_NAMESPACE (default "default"), which must be mapped to the
# bootstrap credential's tenant. The current kubeconfig must be able to read
# and delete those ScheduledJobs in that namespace on the same cluster.
# Use a disposable installation. The smoke tenant is retained after the run.

set -euo pipefail

# ── Colors ─────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# ── Configuration ──────────────────────────────────────────────
API="${1:-http://localhost:8080}"
SMOKE_TENANT_SLUG="${ORBITJOB_TENANT_SLUG:-smoke-$(date +%s)-$$}"
JOB_NAMESPACE="${SMOKE_JOB_NAMESPACE:-default}"
CRS_TO_DELETE=()
# Digest pinned to the busybox entry in test/load/config/images.lock.yaml:
# echo runs `true` (exits instantly), sleep runs 30s to leave a cancel window.
CR_IMAGE="docker.io/library/busybox:1.37@sha256:9532d8c39891ca2ecde4d30d7710e01fb739c87a8b9299685c63704296b16028"
PASS=0
FAIL=0
SKIP=0
RESULTS_FILE="smoke-test-results.md"

# ── Helpers ────────────────────────────────────────────────────
log_pass() { echo -e "${GREEN}[PASS]${NC} $1"; PASS=$((PASS+1)); echo "- [x] $1" >> "$RESULTS_FILE"; }
log_fail() { echo -e "${RED}[FAIL]${NC} $1"; FAIL=$((FAIL+1)); echo "- [ ] $1" >> "$RESULTS_FILE"; }
log_skip() { echo -e "${YELLOW}[SKIP]${NC} $1"; SKIP=$((SKIP+1)); echo "- [-] $1 (skipped)" >> "$RESULTS_FILE"; }
log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_section() { echo ""; echo -e "${BLUE}═══ $1 ═══${NC}"; echo "" >> "$RESULTS_FILE"; echo "## $1" >> "$RESULTS_FILE"; }

# jq-free field extraction (tolerant)
jget() { echo "$1" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('$2',''))" 2>/dev/null || echo ""; }

# HTTP request with status-code capture
http() {
  local method="$1" path="$2"
  shift 2
  local code body
  if ! body=$(curl -s --connect-timeout 5 --max-time 30 -w "\n%{http_code}" -X "$method" "$API$path" "$@" 2>/dev/null); then
    # curl can print HTTP 200 and still fail while reading the response body.
    # Preserve transport failure instead of treating that partial body as proof.
    LAST_CODE="000"
    LAST_BODY="HTTP transfer failed"
    return 0
  fi
  code=$(echo "$body" | tail -1)
  body=$(echo "$body" | sed '$d')
  LAST_CODE="$code"
  LAST_BODY="$body"
}

# Assert the HTTP status code of the last request
assert_code() {
  local expected="$1" name="$2"
  if [ "$LAST_CODE" = "$expected" ]; then
    log_pass "$name (HTTP $LAST_CODE)"
  else
    log_fail "$name (expected $expected, got $LAST_CODE): $LAST_BODY"
  fi
}

apply_smoke_job() {
  local name="$1" path="/api/v1/definitions/jobs/$JOB_NAMESPACE/$1"
  shift
  local request_id request cleanup_index
  request_id=$(python3 -c 'import uuid; print(uuid.uuid4())') || return 1
  # February 30 never fires, leaving these jobs available for manual triggers.
  request=$(python3 -c '
import json, sys
request_id, image, *command = sys.argv[1:]
spec = {
    "schedule": "0 0 30 2 *",
    "executionPolicyVersion": 1,
    "history": {"successfulRuns": 10, "failedRuns": 10},
    "timeoutSeconds": 120,
    "jobTemplate": {"image": image, "command": command},
}
print(json.dumps({"request_id": request_id, "dry_run": True, "spec_json": json.dumps(spec)}))
' "$request_id" "$CR_IMAGE" "$@") || return 1
  http PUT "$path" -H "Authorization: Bearer $BOOTSTRAP_KEY" \
    -H "Content-Type: application/json" -d "$request"
  assert_code "200" "check ScheduledJob $name"
  [ "$LAST_CODE" = "200" ] || return 1

  if ! request=$(python3 -c '
import json, sys
request_id, namespace, name = sys.argv[1:]
receipt = json.load(sys.stdin)
identity = tuple(receipt.get(key) for key in ("request_id", "kind", "namespace", "name", "state"))
if identity != (request_id, "jobs", namespace, name, "validated"):
    raise SystemExit(1)
spec, digest = receipt.get("spec_json"), receipt.get("manifest_digest")
if not isinstance(spec, str) or not spec or not isinstance(digest, str) or not digest:
    raise SystemExit(1)
print(json.dumps({"request_id": request_id, "spec_json": spec, "manifest_digest": digest}))
' "$request_id" "$JOB_NAMESPACE" "$name" <<< "$LAST_BODY"); then
    log_fail "check response has no matching validated receipt for $name"
    return 1
  fi

  # Track the request so cleanup can identify an unresolved write for review.
  cleanup_index=${#CRS_TO_DELETE[@]}
  CRS_TO_DELETE+=("$name:$request_id:")
  http PUT "$path" -H "Authorization: Bearer $BOOTSTRAP_KEY" \
    -H "Content-Type: application/json" -d "$request"
  assert_code "200" "apply checked ScheduledJob $name (request $request_id)"
  [ "$LAST_CODE" = "200" ] || return 1
  if ! JOB_UID=$(python3 -c '
import json, sys, uuid
request_id, namespace, name = sys.argv[1:]
receipt = json.load(sys.stdin)
identity = tuple(receipt.get(key) for key in ("request_id", "kind", "namespace", "name"))
if identity != (request_id, "jobs", namespace, name) or receipt.get("state") not in ("accepted", "reconciled"):
    raise SystemExit(1)
print(uuid.UUID(receipt["uid"]))
' "$request_id" "$JOB_NAMESPACE" "$name" <<< "$LAST_BODY"); then
    log_fail "apply response has no matching acceptance receipt for $name"
    return 1
  fi
  CRS_TO_DELETE[$cleanup_index]="$name:$request_id:$JOB_UID"
}

# The definition receipt describes the admitted CR. Wait for the operator's
# independent projection of that exact source UID before triggering any runs.
wait_job_revision() {
  local uid="$1" revision
  for _ in $(seq 1 60); do
    http GET "/api/v1/jobs?source_uid=$uid&limit=1" -H "Authorization: Bearer $BOOTSTRAP_KEY"
    if [ "$LAST_CODE" = "200" ]; then
      revision=$(python3 -c '
import json, sys
item = next(iter(json.load(sys.stdin).get("items") or []), {})
if item.get("source_uid") == sys.argv[1]:
    print(item.get("id", ""))
' "$uid" <<< "$LAST_BODY" 2>/dev/null || true)
      if [[ $revision =~ ^[1-9][0-9]*$ ]]; then
        printf '%s\n' "$revision"
        return 0
      fi
    fi
    sleep 1
  done
  return 1
}

# Find a run by occurrence_key in the /instances list; prints "id phase" (empty if not found)
find_run_by_key() {
  http GET "/api/v1/instances?limit=100" -H "Authorization: Bearer $BOOTSTRAP_KEY"
  [ "$LAST_CODE" = "200" ] || { echo ""; return; }
  echo "$LAST_BODY" | python3 -c "
import sys, json
key = '$1'
try:
    d = json.load(sys.stdin)
except Exception:
    print(''); raise SystemExit
for it in d.get('items', []):
    if it.get('occurrence_key') == key:
        print(it.get('id', ''), it.get('phase', ''))
        break
else:
    print('')
" 2>/dev/null || echo ""
}

# Poll until the run for an occurrence_key shows up; prints "id phase"
wait_run_appear() {
  local key="$1" timeout_sec="${2:-60}"
  local found=""
  for _ in $(seq 1 "$timeout_sec"); do
    found=$(find_run_by_key "$key")
    [ -n "$found" ] && { echo "$found"; return 0; }
    sleep 1
  done
  echo ""
  return 1
}

# Poll a run id until its phase enters one of the given phases; prints the final phase
wait_run_phase() {
  local run_id="$1" timeout_sec="$2"
  shift 2
  local phases="$*" phase=""
  for _ in $(seq 1 "$timeout_sec"); do
    http GET "/api/v1/instances/$run_id" -H "Authorization: Bearer $BOOTSTRAP_KEY"
    if [ "$LAST_CODE" = "200" ]; then
      phase=$(jget "$LAST_BODY" "phase")
      for p in $phases; do
        [ "$phase" = "$p" ] && { echo "$phase"; return 0; }
      done
    fi
    sleep 1
  done
  echo "$phase"
  return 1
}

# ── Bootstrap key ─────────────────────────────────────────────
# Prefer the environment variable; otherwise read it from the kind cluster
# secret (the old "make bootstrap-key" target no longer exists).
BOOTSTRAP_KEY="${ADMIN_BOOTSTRAP_API_KEY:-}"
if [ -z "$BOOTSTRAP_KEY" ] && command -v kubectl >/dev/null 2>&1; then
  KEY_NS="${ORBITJOB_NAMESPACE:-orbitjob-system}"
  BOOTSTRAP_KEY=$(kubectl -n "$KEY_NS" get secret bootstrap-api-key -o jsonpath='{.data.api-key}' 2>/dev/null | base64 -d || true)
fi
if [ -z "$BOOTSTRAP_KEY" ]; then
  echo -e "${RED}[FAIL]${NC} ADMIN_BOOTSTRAP_API_KEY not set and the cluster secret could not be read." >&2
  echo "Run: export ADMIN_BOOTSTRAP_API_KEY=\"\$(kubectl -n orbitjob-system get secret bootstrap-api-key -o jsonpath='{.data.api-key}' | base64 -d)\"" >&2
  exit 1
fi

# ── Initialize the results file ───────────────────────────────
cat > "$RESULTS_FILE" << EOF
# OrbitJob smoke test report

> Generated $(date)
> Scope: core API endpoints, checked ScheduledJob submission, successful
> manual execution, and cancellation from the Running phase.

EOF

# The checked API writes definitions. Kubernetes deletes the temporary CRs and
# their owned JobRuns and Jobs; deletion is outside the definition webhook.
cleanup_crs() {
  [ "${#CRS_TO_DELETE[@]}" -gt 0 ] || return 0
  local entry name request_id expected_uid live options path status=0
  local -a kube=(kubectl --context "$KUBE_CONTEXT" -n "$JOB_NAMESPACE" --request-timeout=30s)
  for entry in "${CRS_TO_DELETE[@]}"; do
    IFS=: read -r name request_id expected_uid <<< "$entry"
    if ! live=$("${kube[@]}" get "scheduledjobs.workloads.orbitjob.io/$name" --ignore-not-found -o json); then
      status=1
      continue
    fi
    if [ -z "$expected_uid" ]; then
      if [ -n "$live" ]; then
        printf 'Cleanup retained ScheduledJob %s/%s in kube context %s. API operation %s has no confirmed UID.\n' \
          "$JOB_NAMESPACE" "$name" "$KUBE_CONTEXT" "$request_id" >&2
        status=1
      fi
      continue
    fi
    if ! options=$(python3 -c '
import json, sys
metadata = json.load(sys.stdin)["metadata"]
request_id, expected_uid = sys.argv[1:]
uid, version = metadata["uid"], metadata["resourceVersion"]
if uid != expected_uid or metadata.get("annotations", {}).get("orbitjob.io/admin-request-id") != request_id:
    raise SystemExit(1)
print(json.dumps({"apiVersion": "v1", "kind": "DeleteOptions", "preconditions": {"uid": uid, "resourceVersion": version}}))
' "$request_id" "$expected_uid" <<< "$live" 2>/dev/null); then
      printf 'Cleanup could not match ScheduledJob %s/%s in kube context %s to the API write.\n' \
        "$JOB_NAMESPACE" "$name" "$KUBE_CONTEXT" >&2
      status=1
      continue
    fi
    path="/apis/workloads.orbitjob.io/v1alpha1/namespaces/$JOB_NAMESPACE/scheduledjobs/$name"
    if ! "${kube[@]}" delete --raw "$path" -f - <<< "$options" >/dev/null; then
      printf 'Cleanup could not confirm the guarded delete of ScheduledJob %s/%s (UID %s). Verify its current identity before retrying.\n' \
        "$JOB_NAMESPACE" "$name" "$expected_uid" >&2
      status=1
      continue
    fi
    if ! "${kube[@]}" wait --for=delete "scheduledjobs.workloads.orbitjob.io/$name" --timeout=30s >/dev/null; then
      status=1
    fi
  done
  CRS_TO_DELETE=()
  return "$status"
}
trap cleanup_crs EXIT

# ═══════════════════════════════════════════════════════════════
log_section "0. Preconditions"
# ═══════════════════════════════════════════════════════════════

log_info "Checking curl / python3 / kubectl"
command -v curl >/dev/null && log_pass "curl available" || { log_fail "curl not available"; exit 1; }
command -v python3 >/dev/null && log_pass "python3 available" || { log_fail "python3 not available"; exit 1; }
command -v kubectl >/dev/null && log_pass "kubectl available" || { log_fail "kubectl not available (required for ScheduledJob cleanup)"; exit 1; }
if ! KUBE_CONTEXT=$(kubectl config current-context); then
  log_fail "kubectl has no current context for ScheduledJob cleanup"
  exit 1
fi
if [[ ! $JOB_NAMESPACE =~ ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$ ]]; then
  log_fail "SMOKE_JOB_NAMESPACE must be a valid Kubernetes namespace"
  exit 1
fi

FIXTURE_SUFFIX=$(python3 -c 'import uuid; print(uuid.uuid4().hex)')
CR_ECHO_NAME="smoke-echo-$FIXTURE_SUFFIX"
CR_SLEEP_NAME="smoke-sleep-$FIXTURE_SUFFIX"

log_info "Waiting for admin-api to become ready (up to 60s)"
for i in $(seq 1 60); do
  if curl -fsS --connect-timeout 2 --max-time 5 "$API/healthz" >/dev/null 2>&1; then
    log_pass "admin-api ready (${i}s)"
    break
  fi
  [ "$i" = "60" ] && { log_fail "admin-api not ready within 60s"; exit 1; }
  sleep 1
done

# ═══════════════════════════════════════════════════════════════
log_section "1. System endpoints"
# ═══════════════════════════════════════════════════════════════

http GET "/healthz"
assert_code "200" "GET /healthz health check"

http GET "/metrics"
assert_code "200" "GET /metrics Prometheus metrics"

http GET "/openapi.json"
assert_code "200" "GET /openapi.json OpenAPI spec"

# ═══════════════════════════════════════════════════════════════
log_section "2. Authentication"
# ═══════════════════════════════════════════════════════════════

# No credentials
http GET "/api/v1/tenants"
assert_code "401" "unauthenticated request rejected (401)"

# Wrong key
http GET "/api/v1/tenants" -H "Authorization: Bearer wrong-key"
assert_code "401" "wrong API key rejected (401)"

# Correct bootstrap key
http GET "/api/v1/tenants" -H "Authorization: Bearer $BOOTSTRAP_KEY"
assert_code "200" "bootstrap key accepted"
log_info "tenants visible to bootstrap key: $(echo "$LAST_BODY" | python3 -c "import sys,json; print(len(json.load(sys.stdin).get('items',[])))" 2>/dev/null || echo '?')"

# ═══════════════════════════════════════════════════════════════
log_section "3. Tenants"
# ═══════════════════════════════════════════════════════════════

# Create a tenant
http POST "/api/v1/tenants" \
  -H "Authorization: Bearer $BOOTSTRAP_KEY" \
  -H "Content-Type: application/json" \
  -d "{\"slug\":\"$SMOKE_TENANT_SLUG\",\"name\":\"Smoke Test Team\",\"status\":\"active\"}"
TENANT_ID=$(jget "$LAST_BODY" "id")
assert_code "201" "create tenant $SMOKE_TENANT_SLUG"

# Duplicate create (conflict)
http POST "/api/v1/tenants" \
  -H "Authorization: Bearer $BOOTSTRAP_KEY" \
  -H "Content-Type: application/json" \
  -d "{\"slug\":\"$SMOKE_TENANT_SLUG\",\"name\":\"Smoke Test Team\"}"
assert_code "409" "duplicate slug conflict (409)"

# Validation failure: empty slug
http POST "/api/v1/tenants" \
  -H "Authorization: Bearer $BOOTSTRAP_KEY" \
  -H "Content-Type: application/json" \
  -d '{"slug":"","name":"x"}'
assert_code "400" "empty slug validation failure (400)"

# List tenants. RLS restricts the list to the caller's own tenant, so the
# response has exactly one item: the tenant the bootstrap key is scoped to.
http GET "/api/v1/tenants?limit=20" -H "Authorization: Bearer $BOOTSTRAP_KEY"
assert_code "200" "list tenants"
TENANT_COUNT=$(echo "$LAST_BODY" | python3 -c "import sys,json; print(len(json.load(sys.stdin).get('items',[])))" 2>/dev/null || echo "0")
[ "$TENANT_COUNT" -ge 1 ] && log_pass "tenant list not empty ($TENANT_COUNT items)" || log_fail "tenant list is empty"
OWN_TENANT_ID=$(echo "$LAST_BODY" | python3 -c "import sys,json; print(json.load(sys.stdin)['items'][0]['id'])" 2>/dev/null || echo "")

# Get the caller's own tenant (in scope -> 200)
http GET "/api/v1/tenants/$OWN_TENANT_ID" -H "Authorization: Bearer $BOOTSTRAP_KEY"
assert_code "200" "get own tenant (in RLS scope)"

# Get the just-created tenant. Tenant reads are RLS-scoped to the caller, and
# the new tenant's scope is itself, so it is invisible to the bootstrap key by
# design (the duplicate-slug 409 above still works via the unique constraint).
http GET "/api/v1/tenants/$TENANT_ID" -H "Authorization: Bearer $BOOTSTRAP_KEY"
assert_code "404" "get foreign tenant rejected by tenant isolation (404)"

# ═══════════════════════════════════════════════════════════════
log_section "4. API keys"
# ═══════════════════════════════════════════════════════════════

# Create an API key for the smoke tenant
http POST "/api/v1/tenants/$TENANT_ID/api_keys" \
  -H "Authorization: Bearer $BOOTSTRAP_KEY" \
  -H "Content-Type: application/json" \
  -d '{}'
assert_code "201" "create API key for smoke tenant"
TEST_KEY=$(jget "$LAST_BODY" "key")
TEST_KEY_ID=$(jget "$LAST_BODY" "id")
[ -n "$TEST_KEY" ] && log_pass "API key plaintext returned" || log_fail "API key plaintext not returned"

# List keys
http GET "/api/v1/tenants/$TENANT_ID/api_keys" -H "Authorization: Bearer $BOOTSTRAP_KEY"
assert_code "200" "list API keys of smoke tenant"

# Revoke the key
http POST "/api/v1/api_keys/$TEST_KEY_ID/revoke" -H "Authorization: Bearer $BOOTSTRAP_KEY"
assert_code "200" "revoke API key"

# Access with the revoked key (must be 401)
http GET "/api/v1/jobs" -H "Authorization: Bearer $TEST_KEY"
assert_code "401" "revoked key rejected (401)"

# ═══════════════════════════════════════════════════════════════
log_section "5. Job definitions (declarative ScheduledJob CRs)"
# ═══════════════════════════════════════════════════════════════

apply_smoke_job "$CR_ECHO_NAME" true || exit 1
ECHO_UID=$JOB_UID
apply_smoke_job "$CR_SLEEP_NAME" sleep 30 || exit 1
SLEEP_UID=$JOB_UID

log_info "waiting for the operator to materialize both job revisions..."
if ! REVISION_ID=$(wait_job_revision "$ECHO_UID"); then
  log_fail "$CR_ECHO_NAME did not materialize after 60 job-list attempts"
  exit 1
fi
if ! SLEEP_REVISION_ID=$(wait_job_revision "$SLEEP_UID"); then
  log_fail "$CR_SLEEP_NAME did not materialize after 60 job-list attempts"
  exit 1
fi
log_pass "both ScheduledJobs materialized as job revisions ($REVISION_ID, $SLEEP_REVISION_ID)"

# Get a single revision
http GET "/api/v1/jobs/$REVISION_ID" -H "Authorization: Bearer $BOOTSTRAP_KEY"
assert_code "200" "get single job revision"

# Not found
http GET "/api/v1/jobs/999999" -H "Authorization: Bearer $BOOTSTRAP_KEY"
assert_code "404" "get missing job (404)"

# ═══════════════════════════════════════════════════════════════
log_section "6. Run lifecycle (trigger -> Succeeded)"
# ═══════════════════════════════════════════════════════════════

# Manual trigger: the API publishes a JobRun CR and the operator writes the
# ledger row. The response carries occurrence_key but no run id — the run row
# only appears after the operator reconciles, so look it up by occurrence_key.
http POST "/api/v1/jobs/$REVISION_ID/trigger" \
  -H "Authorization: Bearer $BOOTSTRAP_KEY" \
  -H "Content-Type: application/json" -d '{}'
assert_code "201" "trigger job (201 created)"
TRIGGER_KEY=$(jget "$LAST_BODY" "occurrence_key")
log_info "trigger occurrence_key=$TRIGGER_KEY phase=$(jget "$LAST_BODY" "phase")"

# Idempotent trigger: the same Idempotency-Key again returns the existing run (200)
http POST "/api/v1/jobs/$REVISION_ID/trigger" \
  -H "Authorization: Bearer $BOOTSTRAP_KEY" \
  -H "X-OrbitJob-Idempotency-Key: idem-smoke-1" \
  -H "Content-Type: application/json" -d '{}'
assert_code "201" "idempotent trigger (first call creates)"
http POST "/api/v1/jobs/$REVISION_ID/trigger" \
  -H "Authorization: Bearer $BOOTSTRAP_KEY" \
  -H "X-OrbitJob-Idempotency-Key: idem-smoke-1" \
  -H "Content-Type: application/json" -d '{}'
assert_code "200" "idempotent trigger (second call returns existing run)"

# Wait for the run to appear in the ledger
log_info "waiting for the run to enter the ledger..."
RUN_LINE=$(wait_run_appear "$TRIGGER_KEY" 60 || true)
RUN_ID="${RUN_LINE%% *}"
if [ -n "$RUN_ID" ]; then
  log_pass "run appeared in ledger (id=$RUN_ID)"
else
  log_fail "run never appeared in ledger (occurrence_key=$TRIGGER_KEY)"
fi

# Wait for successful execution (busybox true finishes in seconds; allow 120s
# for a first-time image pull)
log_info "waiting for the run to reach Succeeded..."
FINAL_PHASE=$(wait_run_phase "$RUN_ID" 120 Succeeded Failed Canceled || true)
if [ "$FINAL_PHASE" = "Succeeded" ]; then
  log_pass "run succeeded (phase=Succeeded)"
else
  log_fail "run did not reach Succeeded (phase=${FINAL_PHASE:-unknown})"
fi

# Get a single run (response includes the full attempt trail)
http GET "/api/v1/instances/$RUN_ID" -H "Authorization: Bearer $BOOTSTRAP_KEY"
assert_code "200" "get single run (with attempt trail)"

# Attempts endpoint
http GET "/api/v1/instances/$RUN_ID/attempts" -H "Authorization: Bearer $BOOTSTRAP_KEY"
assert_code "200" "list run attempts"

# Phase filter (the ledger vocabulary is phase, not status)
http GET "/api/v1/instances?phase=Succeeded&limit=10" -H "Authorization: Bearer $BOOTSTRAP_KEY"
assert_code "200" "list runs filtered by phase=Succeeded"

# ═══════════════════════════════════════════════════════════════
log_section "7. Cancel a run (body-less cancel)"
# ═══════════════════════════════════════════════════════════════

# The cancel request carries no body: the stop intent is patched onto the
# JobRun CR's spec.cancelRequested, and the actor is the authenticated key —
# there is no field to declare.
http POST "/api/v1/jobs/$SLEEP_REVISION_ID/trigger" \
  -H "Authorization: Bearer $BOOTSTRAP_KEY" \
  -H "Content-Type: application/json" -d '{}'
assert_code "201" "trigger sleep job (cancel target)"
CANCEL_KEY=$(jget "$LAST_BODY" "occurrence_key")

CANCEL_LINE=$(wait_run_appear "$CANCEL_KEY" 60 || true)
CANCEL_RUN_ID="${CANCEL_LINE%% *}"
CANCEL_PHASE="${CANCEL_LINE#* }"
if [ -z "$CANCEL_RUN_ID" ]; then
  log_fail "cancel run (run never entered the ledger)"
elif [ "$CANCEL_PHASE" = "Succeeded" ] || [ "$CANCEL_PHASE" = "Failed" ] || [ "$CANCEL_PHASE" = "Canceled" ]; then
  log_fail "cancel run (run already terminal before cancellation: $CANCEL_PHASE)"
else
  # Wait until the run is actually Running so we observe Canceled rather than
  # a finish-then-200 race.
  log_info "waiting for the run to enter Running..."
  RUNNING_PHASE=$(wait_run_phase "$CANCEL_RUN_ID" 60 Running Succeeded Failed Canceled || true)
  if [ "$RUNNING_PHASE" = "Running" ]; then
    http POST "/api/v1/instances/$CANCEL_RUN_ID/cancel" -H "Authorization: Bearer $BOOTSTRAP_KEY"
    assert_code "200" "cancel run (no body)"
    CANCEL_FINAL=$(wait_run_phase "$CANCEL_RUN_ID" 90 Canceled Succeeded Failed || true)
    if [ "$CANCEL_FINAL" = "Canceled" ]; then
      log_pass "run reached Canceled"
    else
      log_fail "run did not reach Canceled after cancel (phase=${CANCEL_FINAL:-unknown})"
    fi
  else
    log_fail "cancel run (did not observe Running before cancellation: ${RUNNING_PHASE:-unknown})"
  fi
fi

# ═══════════════════════════════════════════════════════════════
log_section "8. Checks"
# ═══════════════════════════════════════════════════════════════

# Create an interval check
http POST "/api/v1/checks" \
  -H "Authorization: Bearer $BOOTSTRAP_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name":"http-check",
    "check_type":"http_health",
    "check_config":{"url":"https://httpbin.org/get","method":"GET"},
    "assertion_rules":[{"metric":"status","operator":"==","threshold":200,"severity":"critical"}],
    "schedule_type":"interval",
    "interval_sec":30,
    "timeout_sec":10,
    "priority":5,
    "labels":{"env":"test"}
  }'
assert_code "201" "create interval check"
CHECK_ID=$(jget "$LAST_BODY" "id")
CHECK_VERSION=$(jget "$LAST_BODY" "version")

# Create a cron check
http POST "/api/v1/checks" \
  -H "Authorization: Bearer $BOOTSTRAP_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name":"cron-check",
    "check_type":"http_health",
    "check_config":{"url":"https://httpbin.org/status/200","method":"GET"},
    "schedule_type":"cron",
    "cron_expr":"*/10 * * * *",
    "timezone":"UTC"
  }'
assert_code "201" "create cron check"
CRON_CHECK_ID=$(jget "$LAST_BODY" "id")
CRON_CHECK_VERSION=$(jget "$LAST_BODY" "version")

# Validation: invalid check_type
http POST "/api/v1/checks" \
  -H "Authorization: Bearer $BOOTSTRAP_KEY" \
  -H "Content-Type: application/json" \
  -d '{"name":"bad","check_type":"invalid","check_config":{},"schedule_type":"interval","interval_sec":30}'
assert_code "400" "invalid check_type validation failure (400)"

# List
http GET "/api/v1/checks?status=active&limit=20" -H "Authorization: Bearer $BOOTSTRAP_KEY"
assert_code "200" "list checks"

# Get
http GET "/api/v1/checks/$CHECK_ID" -H "Authorization: Bearer $BOOTSTRAP_KEY"
assert_code "200" "get single check"

# Pause/resume
http POST "/api/v1/checks/$CHECK_ID/pause" \
  -H "Authorization: Bearer $BOOTSTRAP_KEY" -H "Content-Type: application/json" \
  -d "{\"version\":$CHECK_VERSION}"
assert_code "200" "pause check"

CHECK_VERSION=$(jget "$LAST_BODY" "version")

http POST "/api/v1/checks/$CHECK_ID/resume" \
  -H "Authorization: Bearer $BOOTSTRAP_KEY" -H "Content-Type: application/json" \
  -d "{\"version\":$CHECK_VERSION}"
assert_code "200" "resume check"

# ═══════════════════════════════════════════════════════════════
log_section "9. Check runs"
# ═══════════════════════════════════════════════════════════════

# Resuming a Check can skip an occurrence from before revision activation.
# The read model appears after a later occurrence completes and is projected.
CHECK_RUN_WAIT_SECONDS=120
log_info "waiting for a completed check run (up to ${CHECK_RUN_WAIT_SECONDS}s)..."
CHECK_RUN_ID=""
check_run_deadline=$((SECONDS + CHECK_RUN_WAIT_SECONDS))
while (( SECONDS < check_run_deadline )); do
  remaining=$((check_run_deadline - SECONDS))
  (( remaining > 0 )) || break
  http GET "/api/v1/check-runs?check_id=$CHECK_ID&limit=5" \
    -H "Authorization: Bearer $BOOTSTRAP_KEY" --max-time "$((remaining < 5 ? remaining : 5))"
  if [ "$LAST_CODE" = "200" ]; then
    CHECK_RUN_ID=$(python3 -c '
import json, sys
for item in json.load(sys.stdin).get("items") or []:
    if str(item.get("check_id")) == sys.argv[1] and isinstance(item.get("id"), int) and item["id"] > 0:
        print(item["id"])
        break
' "$CHECK_ID" <<< "$LAST_BODY" 2>/dev/null || true)
    [ -n "$CHECK_RUN_ID" ] && break
  fi
  (( SECONDS >= check_run_deadline )) || sleep 1
done

http GET "/api/v1/check-runs?limit=20" -H "Authorization: Bearer $BOOTSTRAP_KEY"
assert_code "200" "list check runs"

http GET "/api/v1/check-runs?status=pending&limit=10" -H "Authorization: Bearer $BOOTSTRAP_KEY"
assert_code "200" "list check runs filtered by status=pending"

if [ -n "$CHECK_RUN_ID" ]; then
  log_pass "check run produced (id=$CHECK_RUN_ID)"
  http GET "/api/v1/check-runs/$CHECK_RUN_ID" -H "Authorization: Bearer $BOOTSTRAP_KEY"
  assert_code "200" "get single check run"
else
  log_fail "no completed run for check $CHECK_ID within ${CHECK_RUN_WAIT_SECONDS}s"
fi

# ═══════════════════════════════════════════════════════════════
log_section "10. SLIs"
# ═══════════════════════════════════════════════════════════════

http POST "/api/v1/slis" \
  -H "Authorization: Bearer $BOOTSTRAP_KEY" \
  -H "Content-Type: application/json" \
  -d '{"name":"availability-sli","sli_type":"availability","source_type":"job_run","source_config":{"source_uid":"check-'"$CHECK_ID"'"},"aggregation":"ratio","good_event_criteria":{"status":"success"}}'
assert_code "201" "create SLI"
SLI_ID=$(jget "$LAST_BODY" "id")
SLI_VERSION=$(jget "$LAST_BODY" "version")

http GET "/api/v1/slis?limit=20" -H "Authorization: Bearer $BOOTSTRAP_KEY"
assert_code "200" "list SLIs"

http GET "/api/v1/slis/$SLI_ID" -H "Authorization: Bearer $BOOTSTRAP_KEY"
assert_code "200" "get single SLI"

# ═══════════════════════════════════════════════════════════════
log_section "11. SLOs"
# ═══════════════════════════════════════════════════════════════

http POST "/api/v1/slos" \
  -H "Authorization: Bearer $BOOTSTRAP_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name":"api-slo",
    "sli_id":'"$SLI_ID"',
    "target":0.999,
    "window_type":"rolling",
    "window_duration":"720h",
    "alert_fast_burn_rate":0.5,
    "alert_slow_burn_rate":0.1
  }'
if [ "$LAST_CODE" = "201" ]; then
  log_pass "create SLO (99.9% / 30d rolling) (HTTP 201)"
  SLO_ID=$(jget "$LAST_BODY" "id")
  SLO_VERSION=$(jget "$LAST_BODY" "version")
else
  SLO_ID=""
  SLO_VERSION=""
  log_fail "create SLO (expected 201, got $LAST_CODE): $LAST_BODY"
fi

http GET "/api/v1/slos?limit=20" -H "Authorization: Bearer $BOOTSTRAP_KEY"
assert_code "200" "list SLOs"

if [ -n "$SLO_ID" ]; then
  http GET "/api/v1/slos/$SLO_ID" -H "Authorization: Bearer $BOOTSTRAP_KEY"
  assert_code "200" "get single SLO"

  # Pause/resume
  http POST "/api/v1/slos/$SLO_ID/pause" \
    -H "Authorization: Bearer $BOOTSTRAP_KEY" -H "Content-Type: application/json" \
    -d "{\"version\":$SLO_VERSION}"
  assert_code "200" "pause SLO"

  http POST "/api/v1/slos/$SLO_ID/resume" \
    -H "Authorization: Bearer $BOOTSTRAP_KEY" -H "Content-Type: application/json" \
    -d "{\"version\":$((SLO_VERSION+1))}"
  assert_code "200" "resume SLO"
else
  log_skip "get/pause/resume SLO (no SLO)"
fi

# ═══════════════════════════════════════════════════════════════
log_section "12. SLO error budget"
# ═══════════════════════════════════════════════════════════════

if [ -n "$SLO_ID" ]; then
  http GET "/api/v1/slos/$SLO_ID/budget" -H "Authorization: Bearer $BOOTSTRAP_KEY"
  assert_code "200" "get current SLO budget"

  http GET "/api/v1/slos/$SLO_ID/budgets?limit=20" -H "Authorization: Bearer $BOOTSTRAP_KEY"
  assert_code "200" "list SLO budget history"
else
  log_skip "SLO budget (no SLO)"
fi

# ═══════════════════════════════════════════════════════════════
log_section "13. SLO alerts"
# ═══════════════════════════════════════════════════════════════

http GET "/api/v1/slo-alerts?limit=20" -H "Authorization: Bearer $BOOTSTRAP_KEY"
assert_code "200" "list SLO alerts"

if [ -n "$SLO_ID" ]; then
  http GET "/api/v1/slo-alerts?slo_id=$SLO_ID&limit=10" -H "Authorization: Bearer $BOOTSTRAP_KEY"
  assert_code "200" "list SLO alerts filtered by SLO"
else
  log_skip "list SLO alerts filtered by SLO (no SLO)"
fi

# ═══════════════════════════════════════════════════════════════
log_section "14. Rate limiting"
# ═══════════════════════════════════════════════════════════════

log_info "sending 60 trigger requests (10 concurrent) to probe the rate limit..."
# Trigger rate limiting is a token bucket (triggerRPS 10 by default in the
# chart); serial requests are absorbed by the refill rate, only a concurrent
# burst reliably drains the bucket and produces a 429.
CODES=$(seq 1 60 | xargs -P 10 -I{} curl -s -o /dev/null -w "%{http_code}\n" \
  -X POST "$API/api/v1/jobs/$REVISION_ID/trigger" \
  -H "Authorization: Bearer $BOOTSTRAP_KEY" \
  -H "Content-Type: application/json" -d '{}')
if echo "$CODES" | grep -q "429"; then
  log_pass "trigger rate limited (429)"
else
  log_skip "rate limit test (no 429; the bucket may be large enough)"
fi

# ═══════════════════════════════════════════════════════════════
log_section "15. Cleanup"
# ═══════════════════════════════════════════════════════════════

# Delete checks (GET the latest version first: pause/resume may have bumped it)
delete_check() {
  local id="$1"
  [ -n "$id" ] || { log_skip "delete check (no id)"; return; }
  http GET "/api/v1/checks/$id" -H "Authorization: Bearer $BOOTSTRAP_KEY"
  local v
  v=$(jget "$LAST_BODY" "version")
  if [ -n "$v" ]; then
    http DELETE "/api/v1/checks/$id" \
      -H "Authorization: Bearer $BOOTSTRAP_KEY" \
      -H "Content-Type: application/json" \
      -d "{\"version\":$v}"
    assert_code "200" "delete check (id=$id)"
  else
    log_skip "delete check (id=$id, no version)"
  fi
}
delete_check "$CHECK_ID"
delete_check "$CRON_CHECK_ID"

# Delete the SLO
if [ -n "$SLO_ID" ]; then
  http DELETE "/api/v1/slos/$SLO_ID" \
    -H "Authorization: Bearer $BOOTSTRAP_KEY" \
    -H "Content-Type: application/json" \
    -d "{\"version\":$((SLO_VERSION+2))}"
  assert_code "200" "delete SLO"
else
  log_skip "delete SLO (no SLO)"
fi

# Delete the SLI
if [ -n "$SLI_ID" ]; then
  http DELETE "/api/v1/slis/$SLI_ID" \
    -H "Authorization: Bearer $BOOTSTRAP_KEY" \
    -H "Content-Type: application/json" \
    -d "{\"version\":$SLI_VERSION}"
  assert_code "200" "delete SLI"
else
  log_skip "delete SLI (no SLI)"
fi

# Delete the temporary ScheduledJob CRs (cascades to JobRuns and Kubernetes Jobs)
if cleanup_crs; then
  log_pass "temporary ScheduledJob CRs deleted"
else
  log_fail "could not delete temporary ScheduledJobs $CR_ECHO_NAME and $CR_SLEEP_NAME in $JOB_NAMESPACE"
fi

# ═══════════════════════════════════════════════════════════════
log_section "Summary"
# ═══════════════════════════════════════════════════════════════

echo "" >> "$RESULTS_FILE"
echo "**Total: $PASS passed / $FAIL failed / $SKIP skipped**" >> "$RESULTS_FILE"

echo ""
echo -e "${BLUE}═══════════════════════════════════════${NC}"
echo -e "Total: ${GREEN}$PASS passed${NC} / ${RED}$FAIL failed${NC} / ${YELLOW}$SKIP skipped${NC}"
echo -e "Report written to: ${BLUE}$RESULTS_FILE${NC}"
echo ""

if [ "$FAIL" != "0" ]; then
  echo -e "${RED}$FAIL check(s) failed; see $RESULTS_FILE${NC}"
elif [ "$SKIP" != "0" ]; then
  echo -e "${YELLOW}Required checks passed; $SKIP optional check(s) remain unverified.${NC}"
else
  echo -e "${GREEN}All checks passed.${NC}"
fi

exit $FAIL
