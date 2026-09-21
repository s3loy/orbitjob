# Local Development

How to run OrbitJob on your machine: kind for the cluster, Helm for the
workloads, a PostgreSQL pod for the database.

Every command here was run from a deleted cluster on a clean checkout. When one
of them stops working, fix the code or the script and then fix this file — a
guide that describes a flow nobody has executed is worse than no guide.

## Prerequisites

| Tool | Version used | Purpose |
|---|---|---|
| Go | 1.27.1 (`go.mod`) | build and test |
| Docker | 24+ | image builds, kind node |
| kind | 0.20+ | local cluster |
| kubectl | 1.28+ | cluster access |
| Helm | 3.12+ | install the chart |
| golangci-lint | v2.13.1 (CI) | lint gate |
| nc | any | the setup script waits on a port |

```bash
brew install go kind kubectl helm golangci-lint netcat
docker info >/dev/null && echo "docker is running"
```

## First run

Seven steps. Steps 1 and 2 are the ones people skip, and skipping them is why a
first install fails.

### 1. Cluster

```bash
make kind-up
```

Creates the `orbitjob-dev` cluster if it is missing and switches kubectl to it.
This builds a cluster and nothing else.

### 2. Database

```bash
make kind-db
```

The chart reads two things that step 1 does not provide: a PostgreSQL server,
and an `orbitjob-database` Secret holding the eight keys every pod reads. Run
this before `helm install`, not after. Without the Secret the Helm hooks fail
with `CreateContainerConfigError` and the release lands in `failed`.

What it does:

1. applies `deploy/kind/postgres-17.yaml` into the `orbitjob` namespace
2. port-forwards that pod to `127.0.0.1:15432`
3. runs `cmd/configure setup --mode external`, which generates the four role
   DSNs and three passwords into `.runtime/kind/database.json`
4. creates the `orbitjob-database` Secret in `orbitjob-system`, rewriting the
   DSN hosts from `127.0.0.1:15432` to the in-cluster Service

It is idempotent. Re-running keeps the existing bootstrap API key, so an
`ORBITJOB_API_KEY` you already exported stays valid. Override the namespaces
with `ORBITJOB_NAMESPACE` and `ORBITJOB_DB_NAMESPACE`, and the local port with
`ORBITJOB_DB_PORT`.

### 3. Images

```bash
make docker-build TAG=dev
make kind-load TAG=dev
```

Builds five images as `ghcr.io/s3loy/orbitjob-<component>:dev` — admin-api,
scheduler, operator, migrate and bootstrap — and loads them into the cluster.
`TAG=dev` keeps them distinct from released versions.

Skip this step when you only want to run a published version — the chart's
default registry is `ghcr.io/s3loy`, so `helm install` pulls from there.

### 4. Install

```bash
helm upgrade --install orbitjob ./charts/orbitjob \
  --namespace orbitjob-system \
  -f deploy/kind/values-dev.yaml \
  --wait --timeout=10m
```

The values file carries the two `global.*` values that cover all five
components. Overriding them pulls released images instead of the ones you just
built.

`operator.namespaceTenants` is required. The operator maps namespaces to
tenants from configuration rather than from a CR annotation, so that writing a
Custom Resource cannot claim another tenant. There is no default: guessing one
would bind some namespace to some tenant nobody chose. The value above maps the
`default` namespace to the bootstrap tenant.

Leaving it out fails the install before anything is created, and names the
value that is missing:

```
Error: UPGRADE FAILED: execution error at (orbitjob/templates/operator.yaml:22:24):
operator.namespaceTenants is required when operator.enabled is true. Set it to
"namespace=tenant[,namespace=tenant]", or set operator.enabled=false to install
without the operator.
```

Set `operator.enabled=false` to install the control plane without the operator
and without inventing a mapping.

### 5. Reach the API

```bash
kubectl port-forward -n orbitjob-system svc/orbitjob-admin-api 18080:8080 &
curl http://localhost:18080/healthz
# {"status":"ok"}
```

The health endpoint is `/healthz`. There is no `/health`.

### 6. Credentials

```bash
source <(make kind-env)
```

`kind-env` reads the plaintext key out of the `bootstrap-api-key` Secret and
exports `ORBITJOB_API_KEY` and `ORBITJOB_API`.

### 7. Check it

```bash
curl -H "Authorization: Bearer $ORBITJOB_API_KEY" \
  http://localhost:18080/api/v1/tenants
```

Expect the bootstrap tenant:

```json
{"items":[{"id":"00000000000000000000000001","slug":"default","name":"Default","status":"active"}]}
```

A request without the header returns 401.

## One command for all of it

```bash
bash scripts/quickstart.sh
```

Runs steps 1-7 in order and writes the exported key to `.kind/env.sh`. Use it
when you want a working installation and do not care how it got there; use the
steps above when something is broken and you need to see which part.

## Day to day

### Change, test, redeploy

```bash
make check              # lint, vet, race tests, openapi and tidy checks
make test-cover-check   # 60% coverage gate
make integration        # needs TEST_DATABASE_DSN, see below

make docker-build TAG=dev
make kind-load TAG=dev
helm upgrade orbitjob ./charts/orbitjob \
  --namespace orbitjob-system \
  -f deploy/kind/values-dev.yaml \
  --wait

kubectl rollout status -n orbitjob-system deployment/orbitjob-admin-api
```

`make integration` needs a PostgreSQL it can create schemas in. Run a
disposable instance for it — never reuse the cluster's database pod:

```bash
docker run -d --name orbitjob-test-pg -p 55432:5432 \
  -e POSTGRES_PASSWORD=postgres postgres:17-alpine
export TEST_DATABASE_DSN="postgres://postgres:postgres@127.0.0.1:55432/orbitjob_test?sslmode=disable"
make integration
```

The suite truncates the database it runs against and its role provisioning
rewrites shared login-role passwords, so a name-based guard is not enough: on
2026-09-18 a run pointed at the cluster's PostgreSQL through a port-forward
and rotated the installation's credentials out from under the operator. The
test helper refuses a database whose name does not contain `test` — keep that
guard, and point the DSN at a container whose loss costs nothing.

### Restart one component

```bash
kubectl rollout restart -n orbitjob-system deployment/orbitjob-scheduler
```

Component names are `orbitjob-admin-api`, `-scheduler`, `-operator`.

### Logs

```bash
kubectl logs -n orbitjob-system deployment/orbitjob-admin-api -f
kubectl logs -n orbitjob-system deployment/orbitjob-operator --tail=100
```

The label `app.kubernetes.io/name=orbitjob-<component>` also works with `-l`,
and is what the chart sets.

## Database

PostgreSQL runs as a Deployment named `orbitjob-postgres` in the `orbitjob`
namespace — a different namespace from the workloads, which live in
`orbitjob-system`.

```bash
kubectl exec -n orbitjob deployment/orbitjob-postgres -- \
  env PGPASSWORD=orbitjob-owner psql -U postgres -d orbitjob
```

The `postgres` superuser bypasses row level security, which is why the command
above uses it: it is the only account that can see every tenant's rows. No
`orbitjob_owner` login role exists. Only `orbitjob_migrator`,
`orbitjob_admin` and `orbitjob_runtime` can log in
(`internal/platform/migrate/roles.go:21`); `orbitjob_operator` is a NOLOGIN
identity reserved for a future dedicated operator credential, and the login
roles are subject to RLS.

Useful queries:

```sql
SELECT id, slug, status FROM tenants;
SELECT version, name FROM schema_migrations ORDER BY version;
SELECT tenant_id, phase, count(*) FROM job_run_control_plane GROUP BY tenant_id, phase;
```

The run ledger lives in `job_run_control_plane` and
`job_run_attempts_control_plane`; materialized definitions live in
`job_definition_revisions`. The `phase` column is the run state machine
(`Pending`, `CreatingAttempt`, `Running`, `RetryWaiting`, `Succeeded`,
`Failed`, `CancelRequested`, `Canceled`, `CancelUnknown`); `trigger` says
whether a run was scheduled, manual, a retry, a check, or a workflow step,
and `actor` records who asked for it — `orbitjob-scheduler` for the schedule
loop, `orbitjob-check-scheduler` for checks, the API key for a manual
trigger. Check the phase values first.

### Migrations

The chart runs migrations as a Helm hook, so a normal install already applied
them. To read the current version:

```bash
kubectl exec -n orbitjob deployment/orbitjob-postgres -- \
  env PGPASSWORD=orbitjob-owner psql -U postgres -d orbitjob \
  -Atqc "SELECT max(version) FROM schema_migrations"
```

Migrations run forward only. There is no down migration to roll back with;
recover by fixing forward or by recreating the database.

The repository has not published a release. Its earlier development schemas
are intentionally unsupported by the Kubernetes-only baseline, and their data
is disposable: delete and recreate the development database instead of trying
to preserve or edit its `schema_migrations` history.

## Load testing

The load test tool has its own guide: [`loadtest-guide.md`](loadtest-guide.md).

Three profiles exist — `smoke`, `standard`, `long` — and the config file has to
match the profile name:

```bash
make loadtest-smoke    # 30 minutes, preflight check only, not a qualification run
make loadtest-long     # 8 hour soak
```

`make loadtest-smoke` verifies configuration and image lock without touching the
cluster:

```
preflight: profile=smoke Docker capacity >= 6 CPU / 8 GiB
preflight: configuration and image lock valid
```

Profiles live under `test/load/config/` (`smoke`, `smoke-faults`,
`standard`, `standard-dynamic`, `long`); tenant lists are 26-character ULIDs.
Profiles and qualification runs are described in
[`loadtest-guide.md`](loadtest-guide.md).

## Monitoring

The monitoring stack is a separate kube-prometheus-stack release, not part
of the load tool:

```bash
make monitoring-up
kubectl port-forward -n monitoring svc/kube-prometheus-stack-grafana 3000:80 &
kubectl port-forward -n monitoring svc/kube-prometheus-stack-prometheus 9090:9090 &
```

Grafana is at http://localhost:3000, `admin` / `admin`.

## Troubleshooting

### The release is `failed` and a hook pod is in `CreateContainerConfigError`

The `orbitjob-database` Secret is missing. Run `make kind-db` and reinstall.

### The operator is in `CrashLoopBackOff`

```bash
kubectl logs -n orbitjob-system deployment/orbitjob-operator --tail=5
```

An `OPERATOR_NAMESPACE_TENANTS is required` line means the mapping reached the
pod empty. That should not be possible through `helm install`, which refuses to
render without it — check for a hand-edited Deployment or an older release.

### Pods cannot pull their image

`global.imagePullPolicy=Never` means every image has to be in the node already.
Run `make kind-load TAG=<tag>` for the tag you set in `global.imageTag`.

### The API returns 401

The key comes from the `bootstrap-api-key` Secret, not from
`orbitjob-database`. `source <(make kind-env)` reads the right one.

### Resetting

```bash
kubectl delete namespace orbitjob-system    # workloads
kubectl delete namespace orbitjob           # PostgreSQL and its data
```

Then start again from step 2. Deleting the whole cluster is
`kind delete cluster --name orbitjob-dev`, followed by step 1.
