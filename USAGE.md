# OrbitJob Usage Guide

This document tracks the current `dev` branch. Deployment entry points, API requests and database migrations follow the code in the repository:

- Local stack: `Makefile`, `deploy/kind/`
- Kubernetes: `charts/orbitjob/`
- API schema: `api/openapi.yaml`
- Process configuration: `cmd/*/main.go`, `deploy/env/*.env.example`

## Quick start (5 minutes)

**Recommended**: deploy the full development environment with Kind + Helm.

### Prerequisites

- Docker Desktop (>= 24.0)
- kind (>= 0.20.0)
- kubectl (>= 1.28.0)
- Helm (>= 3.12.0)
- make

macOS install:

```bash
brew install kind kubectl helm
```

### One-shot start

```bash
# Option 1: use the script (recommended for first runs)
bash scripts/quickstart.sh

# Option 2: install the published v0.2.1 images manually
make kind-up                    # create the kind cluster
make kind-db                    # install PostgreSQL and generate the orbitjob-database Secret
helm upgrade --install orbitjob ./charts/orbitjob \
  --namespace orbitjob-system \
  -f deploy/kind/values-dev.yaml \
  --set-string global.imageTag=v0.2.1 \
  --set-string global.imagePullPolicy=IfNotPresent \
  --wait --timeout=10m

# Export the environment variables (recommended)
source <(make kind-env)

# Port-forward (separate terminal)
kubectl -n orbitjob-system port-forward svc/orbitjob-admin-api 18080:8080

# Verify the deployment
curl -H "Authorization: Bearer $ORBITJOB_API_KEY" http://localhost:18080/api/v1/tenants
```

**Expected output**: the bootstrap tenant list (`{"items":[...]}`) means the deployment succeeded.

The full development workflow is in [`docs/local-deployment.md`](docs/local-deployment.md).

## 1. Choosing a deployment method

| Method | Use case | Database | Process topology | Status |
|---|---|---|---|---|
| **Kind + Helm** | **development, test, production** | Existing PostgreSQL | Three Deployments: admin-api, scheduler, operator | **Recommended** |
| Standalone processes | VMs, bare metal, custom orchestration | Existing PostgreSQL | Standalone binaries | Production-ready |

> Historical note: the Docker Compose local stack was removed together with the worker execution path; use Kind + Helm for the local experience.

**Recommended development flow**: Kind + Helm for the full environment — see [`docs/local-deployment.md`](docs/local-deployment.md).

OrbitJob currently supports the ScheduledJob/JobRun and WorkflowJob/WorkflowRun CRs, cron/manual triggers, API-managed checks (checks are database rows that execute as Kubernetes Jobs through the run ledger — there is no Check or CheckRun CR), Functions and Workflows, SLI/SLO, API key authentication, tenant isolation, PostgreSQL roles/RLS, Prometheus metrics, Kubernetes Lease election (operator) and optional etcd election (scheduler).

Not yet delivered: function definition CRUD over the admin API — the API is deliberately read-only for function definitions (list/get) plus invoke and run queries; workflows are declared as `WorkflowJob` CRs.

## 2. Kind + Helm deployment (recommended)

The full guide is [`docs/local-deployment.md`](docs/local-deployment.md). Quick start:

```bash
# Create the kind cluster (cluster only, no database)
make kind-up

# Install PostgreSQL and generate the orbitjob-database Secret (must precede helm install)
make kind-db

# Build and load local development images
make docker-build TAG=dev
make kind-load TAG=dev

# Install OrbitJob; the values file carries imageTag=dev, pullPolicy=Never and
# the mandatory operator.namespaceTenants mapping — a missing mapping fails the install
helm upgrade --install orbitjob ./charts/orbitjob \
  --namespace orbitjob-system \
  -f deploy/kind/values-dev.yaml \
  --wait --timeout=10m

# For published images, add these after the values file instead of building/loading:
#   --set-string global.imageTag=v0.2.1 \
#   --set-string global.imagePullPolicy=IfNotPresent

# Export the environment variables (make kind-env)
source <(make kind-env)

# Port-forward
kubectl -n orbitjob-system port-forward svc/orbitjob-admin-api 18080:8080

# Verify
curl -H "Authorization: Bearer $ORBITJOB_API_KEY" http://localhost:18080/api/v1/tenants
```

**Common commands**:

```bash
# Print the exported environment variables (ORBITJOB_API_KEY, ORBITJOB_API)
make kind-env

# Pod status
kubectl -n orbitjob-system get pods

# Logs
kubectl -n orbitjob-system logs deployment/orbitjob-admin-api -f

# Delete the cluster
make kind-down
```

## 3. Docker Compose (removed)

The Docker Compose local stack (`make docker-up`, `make setup`, the Prometheus/Grafana/Loki containers) was removed together with the worker execution path in this refactor; `docker compose` commands from older documents no longer work. Replacements:

- Full local environment: Kind + Helm — section 2 and [`docs/local-deployment.md`](docs/local-deployment.md).
- Monitoring stack: `make monitoring-up` installs kube-prometheus-stack (Prometheus, Grafana, alerting) into the `monitoring` namespace.
- Database configuration: [`docs/database-setup.md`](docs/database-setup.md).

## 4. Local Go development

To debug a single process, run the binary directly against a migrated, bootstrapped PostgreSQL. The most convenient database is the one `make kind-db` installed into the kind cluster: port-forward it, then use the generated installation configuration.

```bash
# Port-forward the in-cluster PostgreSQL (installed by make kind-db)
kubectl -n orbitjob port-forward deployment/orbitjob-postgres 15432:5432 &

# Use the generated installation state (make kind-db writes it to
# .runtime/kind/database.env; the admin DSN is ADMIN_DSN)
set -a
. ./.runtime/kind/database.env
set +a

PORT=8081 ./bin/admin-api
RUNTIME_DSN="$RUNTIME_DSN" ./bin/scheduler
```

`make migrate-up` runs the repository's migration runner (`cmd/migrate`) against `db/migrations`. New environments should use Helm's owner-init + migrate flow (executed automatically at chart install) or run the baseline manually with `cmd/migrate`.

- The Admin API listens on `8080` by default; change it with `PORT`
- Running a binary directly does not create the database, run migrations or bootstrap; those are done by the Helm hooks or `cmd/bootstrap`
- The API requires a Bearer key; the bootstrap key is in section 6

## 5. Standalone processes

Compile all Go packages for the current platform:

```bash
make build
```

Cross-compile Linux/amd64 binaries:

```bash
make build-all
```

The binaries land in `bin/`: `admin-api`, `scheduler`, `operator`, `configure`, `bootstrap`.

Standalone processes should reference the same installation configuration. admin-api uses `ADMIN_DSN`; scheduler and operator share `RUNTIME_DSN` (the operator reads `OPERATOR_DSN` first):

```bash
set -a
. /opt/orbitjob/etc/database.env
set +a
PORT=8080 ./bin/admin-api
SCHEDULER_HEALTH_PORT=6060 ./bin/scheduler
OPERATOR_DSN="$RUNTIME_DSN" ./bin/operator
```

The legacy `DATABASE_DSN` and `SCHEDULER_DSN` remain only as compatibility fallbacks.

Recommended start order:

```text
owner-init -> migrate -> bootstrap -> admin-api/scheduler/operator
```

Never let runtime processes share the owner or superuser DSN in production. Helm separates the admin/runtime/migrator roles; custom deployments should reuse the same boundary.

Process environment variable examples:

```text
deploy/env/admin-api.env.example
deploy/env/scheduler.env.example
```

Common variables:

| Process | Variable | Default / meaning |
|---|---|---|
| admin-api | `PORT` | `8080` |
| admin-api | `RATELIMIT_READ_RPS` / `RATELIMIT_WRITE_RPS` / `RATELIMIT_TRIGGER_RPS` | code defaults `100`/`10`/`5` |
| scheduler | `RUNTIME_DSN` | installation runtime connection |
| scheduler | `SCHEDULER_HEALTH_PORT` | `6060` |
| operator | `OPERATOR_DSN` (fallback `RUNTIME_DSN`) | installation runtime connection |
| operator | `OPERATOR_NAMESPACE_TENANTS` | namespace=tenant mapping (injected by Helm via `operator.namespaceTenants`) |

The runtime processes expose health endpoints:

```text
GET /healthz   process alive
GET /readyz    readiness probe
GET /metrics   Prometheus metrics
```

Stopping processes: with a process in the foreground, press `Ctrl+C` and wait for the graceful shutdown. In the background, use `kill -TERM <pid>`. The three processes can be stopped in any order.

## 6. Bootstrap and the API key

Bootstrap creates the fixed default tenant and the initial API key. It is idempotent; existing records are not recreated.

Development may use the built-in key `otj_devkey_2026`; production must set an `ADMIN_BOOTSTRAP_API_KEY` of at least 12 characters.

Local file secret backend:

```bash
ADMIN_BOOTSTRAP_SECRET_BACKEND=local \
ADMIN_BOOTSTRAP_SECRET_ROOT="$PWD/run/secrets/orbitjob" \
ADMIN_BOOTSTRAP_API_KEY='otj_replace_with_local_secret' \
ADMIN_DSN="$ADMIN_DSN" \
./bin/bootstrap
```

The new key is written to:

```text
<ADMIN_BOOTSTRAP_SECRET_ROOT>/bootstrap-api-key/api-key
```

If the database already has a bootstrap key, the CLI does not recover or re-print the plaintext. The Kubernetes backend writes the key to the `bootstrap-api-key` Secret in the release namespace (done automatically at Helm install):

```bash
kubectl -n orbitjob-system get secret bootstrap-api-key \
  -o jsonpath='{.data.api-key}' | base64 --decode
```

## 7. Helm and Kubernetes

The chart lives in `charts/orbitjob`, current version `0.2.1`. This section assumes an existing PostgreSQL instance.

### 7.1 Namespace topology

Namespace layout after install:

| Namespace | Contents |
|---|---|
| `orbitjob-system` | Control plane: the admin-api, scheduler and operator Deployments and the install hook Jobs |
| Namespaces configured in `operator.namespaceTenants` | ScheduledJob and JobRun CRs, plus the container Jobs rendered from them |
| `monitoring` | kube-prometheus-stack: Prometheus, Grafana, alerting (optional, installed via `make monitoring-up`) |

One installation uses one `orbitjob-database` Secret, shared by all replicas.

### 7.2 First install

**Step 1: prepare the database Secret.** Generated from a bootstrap DSN:

```bash
install -m 0600 /dev/null /tmp/orbitjob-bootstrap-dsn
printf '%s\n' 'postgres://<owner>:<password>@<host>:5432/orbitjob?sslmode=require' \
  > /tmp/orbitjob-bootstrap-dsn

go run ./cmd/configure setup \
  --mode external \
  --database-dsn-file /tmp/orbitjob-bootstrap-dsn \
  --state .runtime/database.json \
  --runtime-env .runtime/database.env
```

The Kubernetes installer creates the Secret from the generated state with these fixed keys:

```text
bootstrap-owner-dsn  migrator-dsn  admin-dsn  runtime-dsn
migrator-password    admin-password  runtime-password  bootstrap-api-key
```

Full constraints: [`docs/database-setup.md`](docs/database-setup.md).

**Step 2: override the images and install.** The default values use local development image names and must be overridden before deploying:

```yaml
global:
  imageRegistry: registry.example.com
  imageTag: "v0.2.1"

images:
  admin: {repository: orbitjob-admin-api}
  scheduler: {repository: orbitjob-scheduler}
  bootstrap: {repository: orbitjob-bootstrap}
  migrate: {repository: orbitjob-migrate}

operator:
  image: {repository: orbitjob-operator}
  # Mandatory: the namespace-to-tenant mapping. There is no default — a default
  # would bind some namespace to a tenant nobody chose. A missing value fails
  # the install and names this value.
  namespaceTenants: "team-a=00000000000000000000000001,team-b=00000000000000000000000002"

taskNamespace:
  # Optional namespace bootstrap. Jobs run here only when this name is also a
  # key in operator.namespaceTenants.
  create: false
```

Note: comma-carrying values must go in a values file (as above), never through `--set`.

Install:

```bash
helm upgrade --install orbitjob charts/orbitjob \
  --namespace orbitjob-system \
  --create-namespace \
  -f values.production.yaml \
  --wait --wait-for-jobs
```

**Step 3: verify the deployment.** All pods ready:

```bash
kubectl get pods -n orbitjob-system
helm status orbitjob -n orbitjob-system
```

The chart currently creates only a ClusterIP Service for the Admin API, with no built-in Ingress. Access via port-forward:

```bash
kubectl -n orbitjob-system port-forward svc/orbitjob-admin-api 8080:8080
```

Read the bootstrap key:

```bash
kubectl -n orbitjob-system get secret bootstrap-api-key \
  -o jsonpath='{.data.api-key}' | base64 --decode
printf '\n'
```

Verify the API:

```bash
export ORBITJOB_API_KEY="<bootstrap key>"
curl -fsS http://localhost:8080/healthz
curl -fsS -H "Authorization: Bearer $ORBITJOB_API_KEY" http://localhost:8080/api/v1/tenants
```

### 7.3 Day-to-day operations

All commands assume the `orbitjob-system` namespace. Setting the default namespace reduces typing:

```bash
kubectl config set-context --current --namespace=orbitjob-system
```

**Observing:**

| Command | Purpose |
|---|---|
| `kubectl get pods -A` | Cluster-wide overview, quickly locate abnormal pods |
| `kubectl get pods,svc,deploy -o wide` | Control-plane resources + IPs + nodes |
| `kubectl get jobs -n <tenant-namespace>` | Container Job executions in a namespace configured by `operator.namespaceTenants` |
| `kubectl get endpoints` | Confirm a Service has pods behind it — empty endpoints are a common failure source |
| `helm list -A` | Installed releases and revisions |

**Diagnosing:**

| Command | Purpose |
|---|---|
| `kubectl describe pod <pod>` | Events, probe configuration, image, environment variables |
| `kubectl logs <pod> --tail=50` | Recent logs |
| `kubectl logs -l app.kubernetes.io/name=orbitjob-operator --tail=50` | Logs by label, without hardcoding a pod name |
| `kubectl logs <pod> --previous` | Logs from before the last crash (for restart loops) |
| `kubectl get events --sort-by=.lastTimestamp` | Cluster events sorted by time |

OrbitJob images are distroless and contain no shell. `kubectl exec -it <pod> -- sh` fails with `executable file not found`. To enter a container, use an ephemeral debug container:

```bash
kubectl debug -it <pod> --image=busybox --target=<container>
```

In most cases `kubectl logs` + `kubectl get endpoints` are enough; exec is rarely needed.

**Port-forwards** (each occupies a terminal; one per port):

```bash
kubectl port-forward svc/orbitjob-admin-api 8080:8080
kubectl -n monitoring port-forward svc/grafana 3000:3000
kubectl -n monitoring port-forward svc/prometheus 9090:9090
```

**Restart and scale:**

```bash
kubectl rollout restart deploy/orbitjob-scheduler  # rolling restart
kubectl rollout status deploy/orbitjob-scheduler   # watch the rollout
kubectl scale deploy/orbitjob-scheduler --replicas=2 # scale (the operator is a singleton elected via Lease)
kubectl delete pod <pod>                            # delete a pod to force recreation
```

### 7.4 Upgrade and uninstall

After changing the chart or values:

```bash
# If images changed, load them into the kind nodes first
kind load docker-image --name orbitjob-dev <image>:<tag>

# Upgrade the release
helm upgrade orbitjob charts/orbitjob -n orbitjob-system -f values.production.yaml --wait
```

After editing `db/migrations/*.up.sql`, sync the chart copy:

```bash
make helm-migrations-sync
make helm-migrations-check
```

Uninstall:

```bash
helm uninstall orbitjob -n orbitjob-system
```

The chart does not delete external PostgreSQL data and does not clean up leftover Jobs in tenant namespaces configured through `operator.namespaceTenants`.

### 7.5 Common troubleshooting

**Pod Running but not Ready.** Three steps:

1. `kubectl describe pod` — why the probe fails (`Readiness probe failed: ...`)
2. `kubectl logs` — application errors
3. `kubectl get endpoints` — confirm the dependent Service has backends

Probe failures usually mean a dependency is not ready (database, downstream Service), not a crash. For crashes, use `--previous` logs.

**PostgreSQL connection failures.** Pod `0/1` Running, not Ready, `kubectl logs` shows `connection refused`. Check:

```bash
kubectl get endpoints orbitjob-postgres -n orbitjob
```

`<none>` means no pod behind the Service — the deployment has 0 replicas or PostgreSQL is not deployed to that namespace.

**Control-plane "false health".** Pods `1/1 Running` does not mean the database is reachable. To judge real component health, look for database connection errors in the logs and check whether `kubectl get jobrun` produces new runs.

**Container Job not running:**

```bash
kubectl -n <tenant-namespace> get jobs,pods
kubectl -n <tenant-namespace> describe job <name>
kubectl auth can-i create jobs \
  --as system:serviceaccount:orbitjob-system:orbitjob-operator \
  -n <tenant-namespace>
```

Use the namespace key mapped to the JobRun's tenant in `operator.namespaceTenants`. Check the image and command, operator RBAC, quotas and Pod Security restrictions; then compare with the status conditions of the corresponding JobRun CR. Rendered workload Pods have `automountServiceAccountToken: false`, so they receive no Kubernetes API token.

**ImagePullBackOff.** Forgetting `kind load docker-image` after rebuilding an image, so the kind node cannot find the locally built image. After rebuilding, run `kind load docker-image --name <cluster> <image>:<tag>`, then delete the pod to force recreation.

### 7.6 kind verification

Requires Docker, kind, kubectl, Helm and OpenSSL:

```bash
make kind-verify
```

The script creates or reuses the `orbitjob-verify` cluster, builds and loads images, installs PostgreSQL and the chart, and verifies repeat upgrades, migration-failure rollback and data retention. On success it deletes the temporary cluster; on failure it keeps it for investigation.

Daily development uses the `orbitjob-dev` cluster:

```bash
make kind-up         # create the cluster
make kind-status     # nodes and pods
make kind-down       # delete the cluster after typing delete-kind to confirm
```

## 8. Authentication and request conventions

Public endpoints:

```text
GET /healthz
GET /metrics
GET /openapi.json
```

All `/api/v1/*` requests require:

```http
Authorization: Bearer otj_...
```

The server resolves the tenant from the API key and stores only a bcrypt hash. Unknown, expired and revoked keys all return `401`.

`tenant_id` still appears in some Job/Check queries or payloads, mainly for management paths on the bootstrap tenant. A normal tenant key must not treat it as a cross-tenant authorization mechanism; the authenticated key is the tenant boundary.

Optional trace header:

```http
X-Trace-ID: request-20260713-001
```

The service echoes the value back; one is generated when absent.

List APIs use `limit`/`offset`. `limit` caps at 100; when omitted the use-case layer applies a default (usually 50). Responses are `{"items": [...]}`; do not rely on a `total` field.

## 9. Tenants and API keys

Create a tenant:

```bash
curl -sS -X POST "$ORBITJOB_API/api/v1/tenants" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"slug":"payments","name":"Payments","status":"active"}'
```

Query:

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/tenants?limit=50&offset=0"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/tenants/<tenant_id>"
```

Create an API key (an empty request still sends `{}`):

```bash
curl -sS -X POST "$ORBITJOB_API/api/v1/tenants/<tenant_id>/api_keys" \
  -H "$AUTH" -H 'Content-Type: application/json' -d '{}'
```

The `key` in the create response is shown once in plaintext; save it immediately. List APIs return metadata only.

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/tenants/<tenant_id>/api_keys"
curl -sS -X POST -H "$AUTH" "$ORBITJOB_API/api/v1/api_keys/<key_id>/revoke"
```

Management endpoints currently have no fine-grained RBAC. Never expose the bootstrap key to ordinary business callers; put a gateway ACL in front of untrusted networks.

## 10. Jobs and instances

### 10.1 Job definitions (the ScheduledJob CR)

Job definitions are not created over HTTP: a definition is a `ScheduledJob` custom
resource, materialized into `job_definition_revisions` by the operator. The API
is read-only for definitions:

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/jobs?limit=50"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/jobs/<job_id>"
```

Declare a definition:

```yaml
apiVersion: workloads.orbitjob.io/v1alpha1
kind: ScheduledJob
metadata:
  name: reconcile-billing
spec:
  schedule: "17 3 * * *"
  concurrencyPolicy: Forbid
  misfirePolicy: Skip
  timeoutSeconds: 300
  retryPolicy:
    maxAttempts: 3
  history:
    successfulRuns: 10
    failedRuns: 10
  jobTemplate:
    image: registry.example.com/jobs/reconcile
    command: ["/app/reconcile"]
    args: ["--tenant", "payments"]
```

```bash
kubectl apply -f reconcile-billing.yaml
```

| Field | Values / meaning |
|---|---|
| `schedule` | standard five-field cron; every due occurrence produces one run |
| `concurrencyPolicy` | `Allow`, `Forbid`, `Replace` |
| `misfirePolicy` | `Skip`, `FireOnce`, `CatchUpBounded` |
| `timeoutSeconds` | per-run deadline, written to the K8s Job's `activeDeadlineSeconds` |
| `retryPolicy.maxAttempts` | total attempt budget, minimum 1 (unset means 1, not unlimited retries) |
| `history` | terminal runs retained by outcome (`successfulRuns`/`failedRuns`); 0 is the default, not unlimited |
| `suspend` | `true` pauses triggering |
| `jobTemplate` | `image`, `command`, `args`, `backoffLimit` |

### 10.2 Manual trigger

```bash
curl -sS -X POST "$ORBITJOB_API/api/v1/jobs/<job_id>/trigger" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -H 'X-OrbitJob-Idempotency-Key: billing-close-2026-07-13' \
  -d '{}'
```

A first trigger returns `201`; a repeated idempotency key returns the original run with `200`. The actor the API records is the authenticated
key, not a client-supplied header.

### 10.3 Runs and phases

Every run (scheduled or manual) is a `JobRun` CR; the operator renders it into a
Kubernetes Job and writes the ledger. Phase transitions:

```text
Pending -> CreatingAttempt -> Running -> Succeeded
                              |-> RetryWaiting -> CreatingAttempt (next attempt)
                              `-> Failed
Pending/Running/RetryWaiting -> CancelRequested -> Canceled
```

`CancelUnknown` means the cancel outcome could not be confirmed. The Kubernetes Job uses `restartPolicy:
Never` — a failed pod is one failed attempt; retry is owned by the platform, with no silent in-pod restarts.

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/instances?phase=Failed&limit=50"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/instances/<run_id>"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/instances/<run_id>/attempts"
```

Cancel takes no request body:

```bash
curl -sS -X POST -H "$AUTH" "$ORBITJOB_API/api/v1/instances/<run_id>/cancel"
```

Cancelling sets `spec.cancelRequested` to true on the JobRun CR; the operator deletes the corresponding
Kubernetes Job and the run becomes `Canceled`. When a ledger row exists but the CR is missing, the API returns
`409 Conflict`, not `404` — it distinguishes "once existed" from "never existed".

## 11. ScheduledJob reference and execution semantics

- Runs execute as `batch/v1` Jobs rendered from the definition's `jobTemplate`
  in the namespace mapped to their tenant. Their Pod spec disables service-account
  token mounting, including in namespaces created after chart installation.
- The platform owns retry: attempt counts live in the ledger (`attempt`/`max_attempts`),
  and `backoffLimit` is handed to Kubernetes for in-Job pod retries.
- History: the in-process `exec`/`http`/`webhook`/`pg_notify`/`container` handlers were removed
  with the worker execution path. The only thing that executes user code now is the user's container image.

## 12. Checks, SLIs and SLOs

### 12.1 Checks

Only `http_health` is currently supported. A check executes as a Kubernetes
Job — a digest-pinned `curlimages/curl` probe, methods GET/HEAD/OPTIONS only —
and interval schedules must be at least 30 seconds:

```bash
curl -sS -X POST "$ORBITJOB_API/api/v1/checks" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{
    "name":"public-api-health",
    "check_type":"http_health",
    "check_config":{"url":"https://example.com/health","method":"GET","expected_status":200},
    "assertion_rules":[{"metric":"response_time_ms","operator":">","threshold":500,"severity":"warning"}],
    "schedule_type":"interval",
    "interval_sec":60,
    "timeout_sec":10
  }'
```

Assertions describe failure conditions. The example records a warning when the response time exceeds 500ms. Supported operators: `>`, `<`, `==`, `!=`, `>=`, `<=`.

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/checks?status=active"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/check-runs?check_id=<id>"
```

Check pause, resume and delete all require the current `version`; delete also requires a JSON body:

```bash
curl -sS -X DELETE "$ORBITJOB_API/api/v1/checks/<id>" \
  -H "$AUTH" -H 'Content-Type: application/json' -d '{"version":2}'
```

### 12.2 SLIs

```bash
curl -sS -X POST "$ORBITJOB_API/api/v1/slis" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{
    "name":"public-api-availability",
    "sli_type":"availability",
    "source_type":"job_run",
    "source_config":{"source_uid":"<the check's or the ScheduledJob CR's UID>"},
    "aggregation":"ratio",
    "good_event_criteria":{"status":"success"}
  }'
```

SLIs generate five-minute UTC buckets from run-ledger events. `source_type` is
`job_run` and `source_config.source_uid` names the source definition — a
check's `source_uid`, or the ScheduledJob CR's UID. availability/quality
require an explicit `good_event_criteria`.

Deleting an SLI requires the current `version`:

```bash
curl -sS -X DELETE "$ORBITJOB_API/api/v1/slis/<id>" \
  -H "$AUTH" -H 'Content-Type: application/json' -d '{"version":2}'
```

### 12.3 SLOs, budgets and alerts

```bash
curl -sS -X POST "$ORBITJOB_API/api/v1/slos" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{
    "name":"public-api-99.9",
    "sli_id":9,
    "target":0.999,
    "window_type":"rolling",
    "window_duration":"720h",
    "alert_fast_burn_rate":14.4,
    "alert_slow_burn_rate":6
  }'
```

`window_duration` uses Go duration format, e.g. `168h`, `720h`, with a maximum of `8760h`. The slow burn rate must be smaller than the fast burn rate.

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/slos/<id>"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/slos/<id>/budget"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/slos/<id>/budgets"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/slo-alerts?slo_id=<id>&status=firing"
```

SLO pause, resume and delete all require the current `version`. With no Check/SLI samples the budget may be empty; that is not an API error.

## 13. Errors, rate limits and optimistic locking

Error structure:

```json
{"error":{"code":"VALIDATION_ERROR","message":"is required","field":"name"}}
```

| HTTP | code | Scenario |
|---:|---|---|
| 400 | `MALFORMED_REQUEST` | JSON or Content-Type cannot be parsed |
| 400 | `VALIDATION_ERROR` | field or domain rule failed |
| 401 | `UNAUTHORIZED` | invalid Bearer key |
| 403 | `FORBIDDEN` | insufficient permission |
| 404 | `NOT_FOUND` | resource does not exist |
| 409 | `CONFLICT` | stale version, state conflict, unique-key conflict |
| 429 | `RATE_LIMITED` | tenant token bucket exhausted |
| 500 | `INTERNAL_ERROR` | unmapped error |
| 503 | `SERVICE_UNAVAILABLE` | dependency unavailable |

Default per-tenant rate limits: read 100/s, write 10/s, trigger 5/s (trigger and run cancel share one bucket). `RATELIMIT_READ_RPS`, `RATELIMIT_WRITE_RPS` and `RATELIMIT_TRIGGER_RPS` set both the refill rate and the burst.

On `409`, re-GET the resource, read the latest `version`, then replay the operation. Never blindly increment a local version.

## 14. Monitoring and troubleshooting

```bash
make observability-status
curl -fsS http://localhost:8080/metrics
```

The Helm chart overrides the code defaults to read 100 / write 100 / trigger 10 (`adminapi.rateLimits`).

Grafana ships with the Prometheus datasource and the OrbitJob dashboard pre-provisioned (after `make monitoring-up`).

### Migration failures

```bash
kubectl -n orbitjob-system get pods | grep -E 'migrate|owner-init'
kubectl -n orbitjob-system logs <migrate-or-owner-init-pod>
```

- `unsupported pre-release schema history; recreate the database`: the database came from pre-release development migrations. Recreate the database (delete the PostgreSQL namespace or rebuild the cluster); never edit `schema_migrations`.
- `migration 0001 checksum mismatch` with a ledger entry named `baseline`: the released baseline file was modified. Restore the released file; never paper over history changes by rebuilding.
- Checksum mismatches on later migrations: likewise restore the original file, or fix the schema with a new, consecutively numbered migration.

### Bootstrap key retrieval fails

```bash
kubectl -n orbitjob-system get pods        # bootstrap Job status
kubectl -n orbitjob-system logs <bootstrap-pod>
```

If the bootstrap Job fails, read its logs first; on success the key is in the `bootstrap-api-key` Secret. Never write the key into logs or commit it to Git.

### Run stuck Pending

```bash
kubectl -n orbitjob-system logs deployment/orbitjob-operator --tail=100
kubectl -n orbitjob-system get scheduledjob,jobrun -A
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/instances/<run_id>"
```

Check whether the operator's schedule loop created the JobRun CR, reconcile errors in the operator logs,
whether `operator.namespaceTenants` covers the namespace that owns the tenant, and whether the K8s Job
hits quotas or admission restrictions.

### Container Job not running

```bash
kubectl -n <tenant-namespace> get jobs,pods
kubectl -n <tenant-namespace> describe job <name>
kubectl auth can-i create jobs \
  --as system:serviceaccount:orbitjob-system:orbitjob-operator \
  -n <tenant-namespace>
```

Use the namespace key mapped to the tenant in `operator.namespaceTenants`. Check the image and command, operator RBAC, quotas and Pod Security restrictions. Rendered workload Pods do not mount ServiceAccount tokens.

## 15. Smoke test and sample data

The full-surface smoke test covers tenant, API key, trigger and run-query flows and fills in
`smoke-test-results.md`:

```bash
ADMIN_BOOTSTRAP_API_KEY="$ORBITJOB_API_KEY" \
  ./scripts/smoke-test.sh http://localhost:8080
```

The script creates data and triggers runs; it modifies the database. Never run it against production.

> Historical note: `scripts/import-jobs.sh` and `scripts/job-dataset.json` targeted the removed
> `POST /api/v1/jobs` endpoint (handler payload dataset) and do not work on this branch.

## 16. Development verification

```bash
make test
make test-cover
make test-race
TEST_DATABASE_DSN='postgres://postgres:<password>@127.0.0.1:5432/orbitjob_test?sslmode=disable' make integration
make check
make helm-check
```

`make check` runs golangci-lint, `go vet`, race tests, the OpenAPI sync check and the `go mod tidy` check.

`make integration` does not create PostgreSQL. A dedicated `TEST_DATABASE_DSN` is required; the tests modify schema and data.

## 17. API index

Full schema, request fields and response models:

- Repository file: `api/openapi.yaml`
- At runtime: `GET /openapi.json`

Main routes:

```text
/api/v1/tenants
/api/v1/tenants/{id}/api_keys
/api/v1/api_keys/{id}/revoke
/api/v1/jobs
/api/v1/jobs/{id}/trigger
/api/v1/instances
/api/v1/instances/{run_id}/{cancel,attempts}
/api/v1/functions
/api/v1/functions/{id}/{invoke,runs}
/api/v1/functions/{id}/runs/{run_id}
/api/v1/workflows
/api/v1/workflows/{id}/runs
/api/v1/workflows/{id}/runs/{run_id}/{cancel}
/api/v1/checks
/api/v1/checks/{id}/{pause,resume}
/api/v1/check-runs
/api/v1/slis
/api/v1/slos
/api/v1/slos/{id}/{pause,resume,budget,budgets}
/api/v1/slo-alerts
/api/v1/policies
/api/v1/policies/{id}
/api/v1/resource_groups
```

OpenAPI is generated from the HTTP registry by `cmd/openapi-gen`. After changing handler contracts, run:

```bash
make openapi-gen
make openapi-check
```
