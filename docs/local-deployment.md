# Local development

Build the current source and run it in a single-node kind cluster. This path is
for contributors testing an installation, including the Console, Operator,
PostgreSQL and retained execution logs. For an administrator-managed cluster,
use [installation](setup/installation.md).

The commands create or reuse `orbitjob-dev`. If that cluster contains retained
data or an older installation, complete [upgrade preparation](operations/upgrading.md)
before running commands that install or upgrade the chart. A quickstart helper
does not stop old writers before migration hooks.

## Prerequisites

Use a Docker host with at least 4 vCPU, 8 GiB memory and 40 GiB initially free
backing storage. The database requests a 5-GiB PVC; execution logs use a separate
10-GiB archive PVC and node collector state. These requirements cover the local
combined profile, not a production sizing promise.

Install Go 1.27.3+, Docker with BuildKit, kind, Helm 4, kubectl matching the
cluster's Kubernetes minor, Bash, GNU make, netcat, OpenSSL and `shasum`.
The hosted installation workflow pins kind 0.32.0 and Helm 4.2.4 as reference
inputs. Host Console development also requires npm and a Node.js version supported
by the toolchain: 22.13.0 or later in 22.x, 24.x, or 26.0.0 and later.
Database integration checks need PostgreSQL client tools and Python 3 is needed
for installed Console and network checks. See
[installation prerequisites](setup/prerequisites.md) for storage, networking,
permissions and certificate constraints.

## Obtain the source

```bash
git clone --branch dev https://github.com/s3loy/orbitjob.git
cd orbitjob
```

Run repository commands from this directory. The current chart's version does
not prove that published images contain this checkout's Console or migrations;
the following procedure builds all seven component images from this source.

Start Docker, then run:

```bash
docker info
bash scripts/preflight-local.sh
```

The preflight can start a temporary read-only PostgreSQL-image container to
inspect Docker filesystem capacity. On Docker Desktop it also checks the host
filesystem containing the virtual disk. If that disk is outside its standard
macOS location, set `ORBITJOB_DOCKER_STORAGE_PATH` to an existing path on the
same filesystem. Run the check on the Docker host; a remote client filesystem
cannot establish the remote disk's free space.

Stop if preflight fails. Do not reduce storage budgets or delete installation
data to bypass the capacity check.

## Create the cluster and database

```bash
make kind-up
make kind-db
bash scripts/preflight-local.sh --cluster
```

`kind-up` creates `orbitjob-dev` when absent, selects `kind-orbitjob-dev` and
checks node readiness. It does not provision PostgreSQL.

`kind-db` deploys the local PostgreSQL 17 manifest when its Deployment is absent,
waits for it, uses a temporary host port-forward and initializes a restricted
bootstrap account. It stores generated credentials under `.runtime/kind/` and
creates `orbitjob-database` in `orbitjob-system` with in-cluster DSNs. PostgreSQL
is in namespace `orbitjob`. A rerun reuses existing state and the bootstrap API
key; it is not a general rotation or database-switch procedure.

The cluster preflight checks kubectl/server minor equality and the database's
actual mounted claim of at least 5 GiB. The project does not document a supported
in-place migration for an older, smaller database claim. If the database contains
retained data, stop here and prepare a storage migration plan for the installation.
Follow the [backup and recovery prerequisites](operations/backup-and-recovery.md#before-you-begin):
obtain database maintenance and Kubernetes access, establish a coordinated stop
boundary, save a complete backup on separate storage and verify an isolated
restore. Define the destination storage, data transfer, writer cutover, validation
and recovery steps before resizing or replacing the claim or resuming installation.

## Build and load images

```bash
make docker-build TAG=dev
make kind-load TAG=dev
```

These targets build/load Admin API, Scheduler, Operator, migrator, bootstrap,
archive and admission images. The Admin image includes the Console and
`orbitjob-identity`. `kind-load` also imports the pinned Fluent Bit collector.
Every kind node must have the exact image reference selected by the chart.
Host Node.js is not required for this Docker build.

## Install the development chart

For a new installation:

```bash
helm install orbitjob ./charts/orbitjob \
  --namespace orbitjob-system \
  -f deploy/kind/values-dev.yaml \
  --wait --wait-for-jobs --timeout 10m
```

The values file selects `global.imageTag: dev`, pull policy `Never` and
`default=00000000000000000000000001` as the namespace-to-tenant mapping. The
chart requires an explicit mapping when the Operator is enabled. A workload
cannot select another tenant by writing its own CR annotation. Disabling the
Operator removes definition reconciliation and execution; it is not a complete
workload installation.

The development values explicitly allow HTTP browser sessions on loopback.
They do not configure a public HTTP Console. The default log collector quota
and NetworkPolicy settings are development declarations; qualify actual
filesystem bounds and traffic before selecting production settings.

Check all long-running resources:

```bash
for component in admin-api scheduler operator admission; do
  kubectl rollout status -n orbitjob-system "deployment/orbitjob-$component" --timeout=5m
done
kubectl rollout status -n orbitjob-system statefulset/orbitjob-log-archive --timeout=5m
kubectl rollout status -n orbitjob-system daemonset/orbitjob-log-collector --timeout=5m
kubectl get pods,jobs,pvc -n orbitjob-system
```

## Access the API and Console

Keep this port-forward running in a separate terminal:

```bash
kubectl port-forward -n orbitjob-system svc/orbitjob-admin-api 18080:8080
```

In another terminal, load the bootstrap integration key and query the API:

```bash
source <(make kind-env)
curl --fail-with-body -H "Authorization: Bearer $ORBITJOB_API_KEY" \
  http://localhost:18080/api/v1/tenants
```

The tenant list includes the active Default workspace with ID
`00000000000000000000000001`. A request without authentication returns 401.
Keep the key private and issue narrower keys for ordinary scripts.
`kind-env` reads Secret `bootstrap-api-key`, field `api-key`; it does not sign
in to the Console.

For a new installation, follow
[first administrator setup](accounts.md#create-the-first-administrator) with
origin `http://localhost:18080`. Open the operator-issued one-time link, create
a personal username/password account and sign in at
`http://localhost:18080/console/`. An already claimed installation uses existing
account login or the documented operator recovery, not another initial setup.

Create and trigger a controlled first workload using the [user guide](../USAGE.md).
Confirm active revision, terminal run/attempt identity and retained logs.
`/healthz` indicates Admin process liveness; it does not check the database or
prove the workload path.

## Installation shortcut

For a new disposable environment, the helper runs the same cluster, database,
image and chart sequence:

```bash
bash scripts/quickstart.sh
```

It also restarts Admin API, Scheduler, Operator, archive and collector to pick
up rebuilt `:dev` images, then writes private credential exports to
`.kind/env.sh`. It prints API port-forward/check commands for the operator to
run. Complete the access and workload checks above after it finishes.

The helper reuses an existing cluster and performs `helm upgrade --install`.
For a retained installation, follow [upgrading](operations/upgrading.md) and
[local maintenance](local-maintenance.md) first. The helper's readiness message
does not establish compatibility with every older schema or resource format.

## Change, check and redeploy

Run the applicable quality checks from [contributing](../CONTRIBUTING.md).
The ordinary Go checks are:

```bash
make check
make test-cover-check
```

Frontend changes use `npm ci` and `npm run check` from `web/`; browser suites
have their own setup. Run database integration checks against a separate server
as described below.

When the installed schema and running writers already satisfy the compatibility
requirements, rebuild and reload the seven images, then upgrade:

```bash
make docker-build TAG=dev
make kind-load TAG=dev
helm upgrade orbitjob ./charts/orbitjob \
  --namespace orbitjob-system -f deploy/kind/values-dev.yaml \
  --wait --wait-for-jobs --timeout 10m
for component in admin-api scheduler operator admission; do
  kubectl rollout restart -n orbitjob-system "deployment/orbitjob-$component"
  kubectl rollout status -n orbitjob-system "deployment/orbitjob-$component" --timeout=5m
done
kubectl rollout restart -n orbitjob-system statefulset/orbitjob-log-archive daemonset/orbitjob-log-collector
kubectl rollout status -n orbitjob-system statefulset/orbitjob-log-archive --timeout=5m
kubectl rollout status -n orbitjob-system daemonset/orbitjob-log-collector --timeout=5m
```

Reusing `:dev` does not change a Pod template. A Helm upgrade alone can retain
old image content. Restart the admission Deployment as well as other image
consumers after rebuilding it. Check a real affected user flow after rollout.
Use the maintenance procedure when schema, CRDs, identity or writer contracts
change.

## Isolated PostgreSQL checks

`make integration` requires `TEST_DATABASE_DSN` and runs its packages serially,
uncached. Tests can rebuild schemas, truncate rows and change cluster-wide role
passwords. A different database name on the installation's server does not
isolate those role changes. Use a dedicated disposable PostgreSQL server.

For the repository's PostgreSQL 17 integration image:

```bash
TEST_PG_IMAGE="$(sed -n 's/^        image: //p' .github/workflows/integration.yml)"
docker run -d --name orbitjob-test-pg -p 127.0.0.1:55432:5432 \
  -e POSTGRES_DB=orbitjob_test -e POSTGRES_PASSWORD=postgres "$TEST_PG_IMAGE"
docker exec orbitjob-test-pg pg_isready -U postgres -d orbitjob_test
```

Wait for `pg_isready` to report accepting connections, then run:

```bash
export TEST_DATABASE_DSN='postgres://postgres:postgres@127.0.0.1:55432/orbitjob_test?sslmode=disable'
go run ./scripts/provision-test-roles
make integration
```

An empty DSN makes the Make target fail before tests start. A direct tagged Go
command may skip database tests without that variable; skipped tests are not
database evidence. After inspecting results, remove only this owned disposable
container with `docker rm -f orbitjob-test-pg`.

## Isolated installation and Console checks

`make kind-verify TAG=dev` runs `deploy/kind/verify-install.sh`. It creates or
reuses the selected verification cluster, builds the images rendered by the
chart, installs PostgreSQL and checks migration/role/bootstrap behavior,
repeat upgrades and failure handling. It can change the active context and
retains a newly created cluster on failure. Use an isolated kubeconfig and a
unique cluster name; it is not a read-only check of a working installation.

The installed Console harness is restricted to `orbitjob-console-e2e` and
validates its local cluster identity. Build/install that dedicated environment
with a separate kubeconfig before using it:

For a new dedicated environment:

```bash
export KUBECONFIG="$PWD/.runtime/console-kind/kubeconfig"
umask 077
mkdir -p "$(dirname "$KUBECONFIG")"
kind create cluster --name orbitjob-console-e2e --kubeconfig "$KUBECONFIG" --wait 5m
KIND_CLUSTER_NAME=orbitjob-console-e2e make kind-verify TAG=dev
```

If the named cluster already exists, inspect it and export its private
kubeconfig instead of creating it again. The verification script keeps a
pre-existing cluster on success but deletes a cluster it created itself on
success. The explicit create step preserves the installation for browser tests.
With that retained cluster installed:

The fixture writes definition CRs directly. Authorize only this dedicated
cluster's administrator as an explicit native writer before preparation; this
is a test-fixture prerequisite, not a configuration for ordinary application
users. Confirm the identity with `kubectl auth whoami` and use its exact
username in the list (the standard kind identity is `kubernetes-admin`):

```bash
helm upgrade orbitjob ./charts/orbitjob -n orbitjob --reuse-values \
  --set-json 'admission.nativeWriters=["kubernetes-admin"]' \
  --wait --wait-for-jobs --timeout 8m
```

```bash
python3 scripts/console-installed.py prepare \
  --kubeconfig "$KUBECONFIG" --state-dir "$PWD/.runtime/console-kind/fixtures" --port 0
python3 scripts/console-installed.py run \
  --kubeconfig "$KUBECONFIG" --state-dir "$PWD/.runtime/console-kind/fixtures" --port 0
```

The harness creates private account/API profiles and owned workload fixtures.
It exercises the installed API, Operator, Kubernetes Jobs and database rather
than substituting route mocks or seeded run outcomes. Use `--spec` for a
specific supported suite; `python3 scripts/console-installed.py --help` lists
them. Monitoring checks can take a five-minute SLO evaluation interval; unknown
cancellation checks intentionally wait for the stop-confirmation boundary.
The harness runs every selected spec and records each result. Any failed,
skipped, retried or missing test keeps the overall receipt unsuccessful; a
passing subset does not establish complete-suite acceptance.

Read the private results before cleanup. If interrupted while a test finalizer
is held, inspect its recorded ownership and release only that test's finalizer
after verifying its recovery conditions. Retire the owned definitions and revoke
fixture credentials before removing the dedicated cluster:

```bash
python3 scripts/console-installed.py cleanup \
  --kubeconfig "$KUBECONFIG" --state-dir "$PWD/.runtime/console-kind/fixtures" --port 0
kind delete cluster --name orbitjob-console-e2e
```

Cleanup retains historical execution evidence and validates fixture identities.
Keep installed evidence separate from browser fixture and Demo results.

Large installed selections and repeated runs retain protected archives and can
exhaust the test tenant's logical log budget. Start a fresh owned installation,
review capacity for the selected workload, or use the independently authorized
log-cleanup API for completed fixture archives. A `capacity` archive is explicit
missing evidence, even when the workload succeeds; do not count an output
assertion as passed. Keep unresolved execution evidence until the owned test
installation is retired.

## Inspect, monitor and clean up

Read component logs with the actual workload name:

```bash
kubectl logs -n orbitjob-system deployment/orbitjob-operator --tail=100
kubectl logs -n orbitjob-system deployment/orbitjob-admin-api --tail=100
```

Use [monitoring](operations/monitoring.md) for the separate Prometheus/Grafana
stack, [load testing](loadtest-guide.md) for workload profiles and
[troubleshooting](operations/troubleshooting.md) for failure symptoms.

A kind node stores database, archive and collector data inside its storage.
Before removing a retained environment, drain execution evidence and create a
verified [coordinated backup](operations/backup-and-recovery.md). For a disposable
environment whose evidence is no longer needed, `make kind-down` asks for
`delete-kind` before deleting `orbitjob-dev`. Do not delete the shared `default`
namespace to remove test tasks. Do not delete PVCs or remove evidence finalizers
to bypass a failed drain.
