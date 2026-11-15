# Local Development

Build and test the current source in a local kind environment with Helm and
PostgreSQL. This guide is for contributors and administrators testing a source
build. It creates `:dev` images and is separate from installing a published
version. For release installation, use the chart and instructions at that
release's tag. The backend image described here contains no compiled Console
bundle; the API remains available when `/console/` returns 503.
[Verification](verification.md) records completed installation and runtime checks.

## Prerequisites

The development installation requires 4 vCPU, 8 GiB RAM and 40 GiB initially free
Docker/Kubernetes backing storage, including database/log volume allocations.
For Docker Desktop, both the Docker filesystem and the host filesystem containing
the virtual disk must meet the free-space requirement. The preflight checks the
standard macOS `Docker.raw` location. If the disk was moved, or Docker Desktop
runs on another host platform, set `ORBITJOB_DOCKER_STORAGE_PATH` to an existing
path on the filesystem that contains that disk. Run the check on the Docker host;
a remote Desktop context cannot establish the backing filesystem capacity.
Run `bash scripts/preflight-local.sh` before installation and add `--cluster`
to check the existing database claim. Legacy claims below 5 GiB need the
offline migration described in [execution logs](execution-logs.md).
Helm also installs the archive StatefulSet with a separate 10-GiB PVC and the
Fluent Bit node collector. Do not skip their rollout checks after an upgrade.

The backend source and Docker build require **Go 1.27.3+**. They do not require
Node.js or frontend assets.

| Tool | Version or required capability | Purpose |
|---|---|---|
| Go | **1.27.3+** ([module requirement](../go.mod)) | Build Go tools and run installation helpers |
| Docker Engine | **29.5.2** reference environment, with BuildKit | Build images and run kind nodes |
| kind | **0.33.0** reference environment | Create the development cluster |
| Helm | **4.3.1** reference environment | Install the chart |
| kubectl | Match the cluster's Kubernetes minor version; **1.37.x** for a **1.37** cluster | Manage resources and port-forwards |
| Bash / GNU make | **3.2 / 3.81** reference environment | Run repository scripts and targets |
| netcat, OpenSSL, shasum | `nc -z`, `openssl rand -hex` and `shasum -a 256` | Check ports, generate credentials and compare Secret contents |
| golangci-lint | **2.15.0** ([CI pin](../.github/workflows/lint.yml)) | Run the lint gate |

Fixed reference versions describe a recorded local environment, not an inferred
minimum or a guarantee for every later version. Use them for reproducibility.
The hosted load workflow separately pins kind 0.32.0 and Helm 4.2.4; these are
workflow inputs, not a compatibility range. Keep kubectl within the upstream
[supported version skew](https://kubernetes.io/releases/version-skew-policy/).
The shell utilities above have capability requirements; this project has not
established separate minimum versions for them.

Start Docker and verify it with `docker info`. If the tools are installed through
a package manager, check their versions before running the setup. Load verification
also requires `psql` and `curl`; load-tool regressions use Python 3.

## First run

First obtain the repository and enter its root directory:

```bash
git clone --branch dev https://github.com/s3loy/orbitjob.git
cd orbitjob
```

Use the same directory for subsequent repository commands. Prepare the cluster
and database before installing the chart.

### 1. Cluster

```bash
make kind-up
```

Creates the `orbitjob-dev` cluster if it is missing and switches kubectl to it.
This step creates only the cluster.

### 2. Database

```bash
make kind-db
```

The chart needs two things that step 1 does not provide: a PostgreSQL server
and the configured database Secret. Components consume their own role keys;
the complete mapping is defined in `charts/orbitjob/values.yaml`. Run this
before `helm install`, not after. Without the Secret the Helm hooks fail
with `CreateContainerConfigError` and the release enters the `failed` state.

What it does:

1. when the database Deployment is absent, renders
   `deploy/kind/postgres-17.yaml` for the selected database namespace
   (`orbitjob` by default) with `scripts/render-local-manifest.sh` and applies it
2. port-forwards that pod to `127.0.0.1:15432`
3. ensures the local bootstrap role, then runs `cmd/configure setup --mode external`,
   which creates or reuses state in `.runtime/kind/database.json` and writes
   `.runtime/kind/database.env`
4. creates the `orbitjob-database` Secret in `orbitjob-system`, rewriting the
   DSN hosts from `127.0.0.1:15432` to the in-cluster Service

It is idempotent. Re-running keeps the existing bootstrap API key, so an
`ORBITJOB_API_KEY` you already exported stays valid. Override the namespaces
with `ORBITJOB_NAMESPACE` and `ORBITJOB_DB_NAMESPACE`, and the local port with
`ORBITJOB_DB_PORT`.
The database manifest and service DSNs follow the same resolved namespace.

### 3. Images

```bash
make docker-build TAG=dev
make kind-load TAG=dev
```

Builds the images defined by `DOCKER_COMPONENTS` in the Makefile as
`ghcr.io/s3loy/orbitjob-<component>:dev`, including the log archive, and loads
them into the cluster. The load target also prepares the Fluent Bit collector.
`TAG=dev` keeps development images distinct from released versions.

The backend Docker build uses Go without a Node.js build step. The Admin image
includes the account endpoints and identity tool without a compiled Console
bundle. The image does not build or copy `/console` assets.

The source-development path uses these freshly built images. Installing a
published version requires matching chart and component image tags and a
compatible upgrade path. Check the release before selecting its tag.

### 4. Install

```bash
helm upgrade --install orbitjob ./charts/orbitjob \
  --namespace orbitjob-system \
  -f deploy/kind/values-dev.yaml \
  --wait --timeout=10m
```

The values file configures the shared image tag and pull policy for OrbitJob
components. They must agree with the tag loaded in step 3. The local development
values explicitly enable HTTP loopback sessions. For an existing installation,
first follow the upgrade boundary below.

`operator.namespaceTenants` is required. The operator maps namespaces to
tenants from configuration rather than from a CR annotation, so that writing a
Custom Resource cannot claim another tenant. There is no default mapping; namespaces must be explicitly assigned to tenants. The value above maps the
`default` namespace to the bootstrap tenant.

Leaving it out fails chart rendering with a message naming the missing value:

```
operator.namespaceTenants is required when operator.enabled is true. Set it to
"namespace=tenant[,namespace=tenant]", or set operator.enabled=false to install
without the operator.
```

Set `operator.enabled=false` to install the control plane without the operator
and without a mapping. That configuration does not reconcile definitions or
execute runs.

### 5. Reach the API

```bash
kubectl port-forward -n orbitjob-system svc/orbitjob-admin-api 18080:8080 &
curl http://localhost:18080/healthz
# {"status":"ok"}
```

The health endpoint is `/healthz`. There is no `/health`.
This endpoint returns process liveness; its 200 response does not test a
workload or prove database-backed operations succeed.

### 6. Credentials

```bash
source <(make kind-env)
```

`kind-env` reads the bootstrap integration key from the `bootstrap-api-key`
Secret and exports `ORBITJOB_API_KEY` and `ORBITJOB_API` to this shell for API
calls. It does not sign in to the Console. Keep the key private and issue a
narrower dedicated key for ordinary scripts.

For personal accounts, follow
[first administrator setup](accounts.md#create-the-first-administrator) with
`-origin http://localhost:18080` and consume the capability through the account
HTTP endpoint. An already claimed installation does not reopen setup.

### 7. Check it

```bash
curl -H "Authorization: Bearer $ORBITJOB_API_KEY" \
  http://localhost:18080/api/v1/tenants
```

Expect the bootstrap tenant:

```json
{"items":[{"id":"00000000000000000000000001","slug":"default","name":"Default","status":"active"}]}
```

A request without the header returns 401. This checks one authenticated API query.
Use the [workload API procedures](../USAGE.md#10-jobs-runs-functions-and-workflows)
to check a task's execution phase and logs independently of API connectivity.

## Installation shortcut

This is a shortcut for the source-development steps above. It requires a checkout
of this repository and all prerequisites; run it from the repository root.

```bash
bash scripts/quickstart.sh
```

Checks prerequisites, ensures the cluster/database, builds and loads the dev
images, installs the chart, restarts the application Deployments, archive
StatefulSet and collector DaemonSet, and writes credential exports to
`.kind/env.sh`. It prints the port-forward and API
verification commands without executing them. Source the env
file and complete steps 5-7 above.

This helper reuses `orbitjob-dev` when it exists. It does not stop older
writers before pre-upgrade migration hooks, so it is not a migration procedure
for an existing installation that needs the boundary below.

## Upgrading an existing installation

For the existing local kind installation, use the
[maintenance procedure](local-maintenance.md) to prepare source controls,
coordinated backups, network verification, bounded collector storage and recovery.
`preflight-local.sh --cluster` also checks that kubectl matches the server's minor
version. Preparation does not change the running installation.

Migration `0012_storage_integrity.up.sql` and the full-digest run-address
format require stopping older operators and run publishers before applying the migration or
starting the repaired version. This includes older Admin API processes.
Prepare a backup and a maintenance window, stop and confirm termination of
those writers, then apply reviewed CRD changes and run the new chart's hooks
before starting only compatible binaries. See
[database setup](database-setup.md#audit-permissions-and-outcome-recovery) for
the cutover and rollback limits, and [verification](verification.md) for tested
upgrade scenarios. Migration
`0013_execution_observation.up.sql` adds durable recovery metadata for
Unknown executions. Old writers must also stay stopped while the Lease and
source-admission changes are deployed.
Earlier Console installations with migrations 0001-0011 retain their original
checksums; subsequent changes add forward migrations rather than editing
those recorded files.

Migration `0027_personal_accounts.up.sql` adds human identities, memberships,
capabilities and sessions. Install the matching Admin image, which contains
`/orbitjob-identity`, and chart migrations before account setup. Existing API
keys retain their API behavior and do not become Console accounts. Review
[account migration and rollback](database-setup.md#personal-account-migration)
and configure the intended account origin before issuing the setup link.
An existing service at a familiar local address may still run an older image;
source changes alone do not upgrade it.

Helm installs files under `crds/` on first installation; a normal chart upgrade
does not update existing CRDs. Review and apply the current schemas separately:

```bash
kubectl apply -f charts/orbitjob/crds/
```

Do not treat Helm rollback as a database rollback. Existing full-digest run
addresses and durable deduplication facts must remain compatible with the
version used for recovery.

New execution Pods carry `orbitjob.io/termination-evidence`. The Operator needs
namespaced Pod `patch` permission to release this finalizer after persisting
verified exits. Upgrade the matching Operator and RBAC together. Existing Pods
are not retrofitted, and an upgrade cannot reconstruct missing legacy evidence.
Before uninstalling or downgrading, stop new submissions and let the compatible
Operator finish evidence persistence and release in every mapped namespace.
Follow the [drain checks](../USAGE.md#74-upgrade-and-uninstall). Preserve retained
Pods and the database when recovery is incomplete; an older Operator may strand
them.

## Day to day

### Change, test, redeploy

The following dev-image loop assumes the database is already at the current
schema and all running writers satisfy the compatibility boundary above.

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

for component in admin-api scheduler operator; do
  kubectl rollout restart -n orbitjob-system "deployment/orbitjob-$component"
  kubectl rollout status -n orbitjob-system "deployment/orbitjob-$component" --timeout=5m
done

kubectl rollout restart -n orbitjob-system statefulset/orbitjob-log-archive daemonset/orbitjob-log-collector
kubectl rollout status -n orbitjob-system statefulset/orbitjob-log-archive --timeout=5m
kubectl rollout status -n orbitjob-system daemonset/orbitjob-log-collector --timeout=5m
```

Reusing `:dev` does not change a Deployment's pod template. Restarting picks up
the images just loaded into kind; a Helm upgrade alone can keep the old pods.

The tidy check uses `go mod tidy -diff`: it checks whether the current module
files need normalization without changing them or requiring an unmodified Git tree.

`make integration` runs seven packages serially and uncached, and refuses an
empty `TEST_DATABASE_DSN` before Go starts. It needs a dedicated PostgreSQL
instance with permission to rebuild schemas and manage the deployment roles.
Direct tagged Go invocations can still skip tests without that variable; skipped
tests are not database evidence. Use a disposable server, never the installation's
database pod:

```bash
TEST_PG_IMAGE="$(sed -n 's/^        image: //p' .github/workflows/integration.yml)"
docker run -d --name orbitjob-test-pg -p 127.0.0.1:55432:5432 \
  -e POSTGRES_DB=orbitjob_test -e POSTGRES_PASSWORD=postgres "$TEST_PG_IMAGE"
# Wait until this reports accepting connections before running the suite:
docker exec orbitjob-test-pg pg_isready -U postgres -d orbitjob_test
export TEST_DATABASE_DSN="postgres://postgres:postgres@127.0.0.1:55432/orbitjob_test?sslmode=disable"
go run ./scripts/provision-test-roles
make integration
```

The migration tests rebuild `public`; other helpers create/drop temporary
schemas and truncate their test data. Role provisioning can rewrite login-role
passwords across databases on the same PostgreSQL server. A database name
containing `test` is only a guard, not server isolation. After reviewing the
results, remove this disposable container with `docker rm -f orbitjob-test-pg`.

### Restart one component

```bash
kubectl rollout restart -n orbitjob-system deployment/orbitjob-scheduler
```

Deployment names are `orbitjob-admin-api`, `-scheduler` and `-operator`.
The archive and collector use `statefulset/orbitjob-log-archive` and
`daemonset/orbitjob-log-collector` respectively.
Scheduler readiness uses its existing `/readyz` database check; database loss
can make it unready while the process stays alive. Its liveness/startup probes
use `/healthz`. The Admin API has no `/readyz`; its `/healthz` readiness does not
prove database connectivity. A successful rollout alone is not a workload result.

### Logs

```bash
kubectl logs -n orbitjob-system deployment/orbitjob-admin-api -f
kubectl logs -n orbitjob-system deployment/orbitjob-operator --tail=100
```

The label `app.kubernetes.io/name=orbitjob-<component>` also works with `-l`,
and is what the chart sets.

## Database

PostgreSQL runs as a Deployment named `orbitjob-postgres` in the `orbitjob`
namespace. Control-plane workloads live in `orbitjob-system`; rendered Jobs
live in the namespace mapped to their tenant by `operator.namespaceTenants`
(`default` in `deploy/kind/values-dev.yaml`).

```bash
kubectl exec -n orbitjob deployment/orbitjob-postgres -- \
  sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" exec psql -U "$POSTGRES_USER" -d "$POSTGRES_DB"'
```

The `postgres` superuser bypasses row level security, which is why the command
above uses it for local maintenance across tenants. No
`orbitjob_owner` login role exists. Only `orbitjob_migrator`,
`orbitjob_admin` and `orbitjob_runtime` can log in
([role definitions](../internal/platform/migrate/roles.go)); `orbitjob_operator` is a NOLOGIN
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
`Failed`, `ExecutionUnknown`, `CancelRequested`, `Canceled`, `CancelUnknown`).
Unknown phases preserve unresolved execution and cancellation facts; see
[execution recovery](architecture.md#execution-recovery-and-concurrent-admission).
`trigger` says
whether a run was scheduled, manual, a retry, a check, or a workflow step,
and `actor` records who requested it — `orbitjob-scheduler` for the schedule
loop, `orbitjob-check-scheduler` for checks, the API key for a manual
trigger. Check the phase values first.

### Migrations

The chart runs migrations as pre-install/pre-upgrade hooks. Check their result
and the database's applied version; the presence of a migrations ConfigMap
alone does not prove application:

```bash
kubectl exec -n orbitjob deployment/orbitjob-postgres -- \
  sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" exec psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Atqc "SELECT max(version) FROM schema_migrations"'
```

Migrations run forward only. There is no down migration to roll back with;
recover by fixing forward or restoring a reviewed backup. Recreating the
database is appropriate only for disposable development data.

The current database schema is defined by [`db/migrations/`](../db/migrations/), including
the additive storage-integrity and execution-observation migrations. Do not edit
recorded migration checksums or assume an older
development schema is supported. Rebuild disposable data when no supported
upgrade path exists; preserving other data requires an explicit migration plan.

## Load testing

The load test tool has its own guide: [`loadtest-guide.md`](loadtest-guide.md).

Profile names are `smoke`, `standard` and `long`. Config variants may use a
different filename; their internal `profile` must match the command's flag:

```bash
make loadtest-smoke    # validates the smoke preflight; does not run its 30m schedule
make loadtest-long     # validates the long preflight; does not run its 8h schedule
```

`make loadtest-smoke` verifies configuration, image lock and local Docker
capacity without executing a workload or mutating the cluster:

```
preflight: profile=smoke Docker capacity >= 6 CPU / 8 GiB
preflight: configuration and image lock valid
```

Configs live under `test/load/config/` (`smoke`, `smoke-faults`,
`standard`, `standard-dynamic`, `long`); the tool generates and persists four
tenant ULIDs per run. `make loadtest-full` performs preflight and generation,
then prints a manual-review instruction; it does not execute qualification.
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

`make observability-status` requires the named OrbitJob dashboard ConfigMap,
its `grafana_dashboard=1` sidecar label and a JSON payload with UID
`orbitjob-run-ledger`. Unrelated stack dashboards cannot satisfy that check.
An unavailable optional Grafana port-forward produces WARN; a responding
Grafana API with HTTP 404/500, invalid JSON or the wrong UID fails. Prometheus
readiness, targets and rules remain required checks.

## Troubleshooting

### The release is `failed` and a hook pod is in `CreateContainerConfigError`

Inspect the Pod events for the named missing Secret or key. For the local
installation, `make kind-db` creates `orbitjob-database`; confirm it is in the
release namespace before retrying Helm. This error can also identify another
missing configuration reference.

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
`orbitjob-database`. `source <(make kind-env)` reads the bootstrap key.

### Resetting

The following operations are destructive. A kind cluster stores its PostgreSQL
and log-archive volumes inside the node's storage. Before removing namespaces,
PVCs or the cluster, preserve a consistent database backup, installation
credentials/configuration and the log archive, then verify restoration in a
separate environment. Keep the original data until that verification succeeds.
Use these commands only for a disposable environment or after that recovery
procedure is complete:

```bash
kubectl delete namespace orbitjob-system    # control plane
kubectl delete namespace orbitjob           # PostgreSQL and its data
```

Do not delete the shared `default` namespace just to remove tenant
workloads. For a complete reset, delete the isolated development cluster with
`kind delete cluster --name orbitjob-dev`, then restart from step 1.
