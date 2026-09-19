# Load testing

Running a load test against a local cluster, from preflight to report.

Every command here was executed against a freshly built `orbitjob-dev` cluster.
The numbers quoted are from that run. The tooling has since moved onto the
Kubernetes control plane — definitions are now ScheduledJob custom resources and
every run is a ledger row under `job_run_control_plane` — and the rewritten
steps have not yet had a full live pass on the rebuilt cluster; where the text
quotes numbers from the previous architecture they are marked as such.

## Profiles

| Profile | Config | Wall clock | Qualification |
|---|---|---|---|
| `smoke` | `test/load/config/smoke.yaml` | 30m | no |
| `standard` | `test/load/config/standard.yaml` | 4h | yes |
| `long` | `test/load/config/long.yaml` | 8h | no |

The config file and the profile name must agree: `smoke.yaml` declares
`profile: smoke`, and passing `--profile standard` with it is rejected.

There is one execution path — the Kubernetes control plane — so profiles no
longer declare execution modes. The `*-dual.yaml` and `smoke-k8s.yaml` configs
that exercised a legacy-versus-kubernetes split are deleted: the split they
existed to test is gone. Every surviving profile lists its tenants as
26-character ULIDs (the `tenants.id` values the schema's foreign keys require);
a slug such as `default` is a name, never a tenant identifier, and validation
refuses one before any cluster is touched.

Only `smoke` has been run end to end on the previous architecture. Treat the
other two as untested here, and `standard` as the one whose verdict carries
qualification weight.

## Before you start

The load test drives a running installation. It needs:

```bash
make kind-up && make kind-db
make docker-build TAG=dev && make kind-load TAG=dev
helm upgrade --install orbitjob ./charts/orbitjob -n orbitjob-system \
  --set global.imageTag=dev --set global.imagePullPolicy=Never \
  --set operator.namespaceTenants="default=00000000000000000000000001" --wait
```

Then the fixtures and three port-forwards:

```bash
make loadtest-prepare     # applies deploy/load/*.yaml into orbitjob-load

kubectl port-forward -n orbitjob-system svc/orbitjob-admin-api 18080:8080 &
kubectl port-forward -n orbitjob-load  svc/load-fixture        18081:8080 &
kubectl port-forward -n monitoring     svc/kube-prometheus-stack-prometheus 9090:9090 &
```

The Prometheus forward is only needed by the dynamic profile
(`standard-dynamic.yaml`): its feedback controller reads live pressure metrics
from Prometheus at `prometheus_url`, which defaults to `localhost:9090`. When
Prometheus is unreachable the controller does not fail the run — it freezes
the pace multiplier and logs warnings, so a missing forward looks like a slow
cluster rather than a broken setup. The static profiles never contact
Prometheus and ignore the forward.

`make loadtest-prepare` deploys manifests. It does not seed anything. The
tenants, namespaces, ScheduledJob definitions and API keys are created later by
the tool's own `prepare` command, which is a different step with a confusingly
similar name.

Check the fixtures are up before continuing:

```bash
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:18080/healthz   # 200
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:18081/ledger    # 200
```

### The host has to stay awake

The engine paces the schedule against the monotonic clock, and that clock
stops while a machine is suspended. A host that sleeps between maintenance
windows does not finish a 30-minute profile in 30 minutes — the schedule needs
30 minutes of *awake* time, however many hours that stretches across. Nothing
is hung and nothing is slow, but the timings are meaningless and the last
events may never be reached. On a laptop sleeping 900s out of every 960s, the
first 11 minutes of the smoke schedule took 1h46m of wall clock, and the run
never finished.

`run` holds a sleep assertion itself on macOS, through `caffeinate`, for as
long as the run lasts. Linux and CI runners do not need one. If the host is
suspended anyway — the assertion needs AC power on macOS — the run says so on
the way out rather than leaving you to read a stretched timeline as a slow
system:

```
loadtest: the host was suspended for about 1h35m0s; the schedule stretched to
1h46m12s of wall clock and the latency numbers are not usable
```

## Running

```bash
export RUN_ID="smoke-$(date -u +%Y%m%dT%H%M%SZ)"
export ORBITJOB_API_KEY=$(kubectl get secret bootstrap-api-key \
  -n orbitjob-system -o jsonpath='{.data.api-key}' | base64 -d)
CONFIG=test/load/config/smoke.yaml
```

`bootstrap-api-key` is the Secret holding the plaintext key.
`orbitjob-database` holds the database credentials and is a different Secret.

### 1. Preflight

```bash
go run ./scripts/loadtest preflight --config "$CONFIG" \
  --images test/load/config/images.lock.yaml --profile smoke
```

```
preflight: profile=smoke Docker capacity >= 6 CPU / 8 GiB
preflight: configuration and image lock valid
```

Without `--check-only` it also verifies the images exist, which needs Docker.

### 2. Generate

```bash
go run ./scripts/loadtest generate --config "$CONFIG" \
  --images test/load/config/images.lock.yaml --run-id "$RUN_ID"
```

Writes `test/load/runs/$RUN_ID/generated/` — 1200 ScheduledJob declarations
for the smoke profile, plus a summary of how they break down by category.

A declaration is a full ScheduledJob spec. Cron definitions carry the interval
the config sets; manual-only definitions carry `0 0 30 2 *` — February 30th,
which parses as a standard cron expression but matches no instant, so the
CRD's non-empty-schedule rule is satisfied while the scheduler never fires the
definition. Every declaration raises its history limits to the corpus size so
the retainer cannot prune evidence while the run is still going. Because the
operator renders Kubernetes Jobs with image, command and args only — no env
block, no service account — each workload body has its case id, the fixture
addresses and the database DSN baked into its arguments as literals.

### 3. Prepare

```bash
go run ./scripts/loadtest prepare --config "$CONFIG" --profile smoke \
  --run-id "$RUN_ID" --api-url http://localhost:18080
```

```
prepare: declared 1200 definitions across 4 tenants
```

Prepare provisions everything the run depends on:

1. Tenant rows with the config's fixed ULIDs are seeded into the installation
   database (`INSERT ... ON CONFLICT DO NOTHING` through `kubectl exec psql`;
   the API mints its own ids, and the namespace table needs these exact ids).
2. One namespace per tenant is created — `orbitjob-tasks-<tenant-ULID>`,
   labelled `orbitjob.io/managed-by=orbitjob-loadtest` — carrying the same
   ResourceQuota and LimitRange shape as the shared task namespace, because
   this is now where the rendered Kubernetes Jobs execute.
3. The operator's namespace table (`OPERATOR_NAMESPACE_TENANTS`) is extended
   with one `namespace=tenant` entry per load tenant and the operator is
   restarted only if the table changed. Existing mappings are preserved.
4. The corpus is declared: one ScheduledJob custom resource per definition,
   batched into one multi-document manifest per tenant (kept under
   `cr-manifests/` in the run directory as evidence).
5. Prepare waits until every declaration has an active revision — read from
   each CR's `status.activeRevision` — and records the case-to-revision
   mapping in `created-definitions.json`. The trigger route addresses
   revisions, so this mapping is what the run engine fires.
6. A tenant API key is minted per tenant, bound to the `TenantAdminAccess`
   policy, into `tenant-keys.json` (plaintext, which is why
   `test/load/runs/` is gitignored), alongside `tenant-namespaces.json` for
   the verifier.

Tenancy comes from the API key on every call; there is no tenant header. The
Admin API has no create route for definitions — they are declared as custom
resources and the operator materializes the revisions.

Prepare is idempotent rather than resume-capable: reapplying an unchanged
ScheduledJob keeps its revision, so a rerun re-reads the same ids. After a
`reset`, run `prepare` again from scratch — reset deletes the load tenants'
ledger rows and tenants, so the CRs get fresh revision ids, and stale run
directories will not match them.

### 3.5 Grant the admin API its trigger surface

`prepare` creates the tenants' namespaces after the chart was installed, and
the chart grants the admin API's CR surface one scheduling namespace at a
time (`operator.namespaceTenants`). A namespace with no Role denies every
manual trigger's JobRun publish: the run rejects with 500 and nothing
executes. Apply the chart's own Role and binding to the namespaces this tool
created — they carry the `orbitjob.io/managed-by=orbitjob-loadtest` label,
the same one `reset` cleans by:

```bash
for namespace in $(kubectl get namespace \
    -l orbitjob.io/managed-by=orbitjob-loadtest \
    -o jsonpath='{.items[*].metadata.name}'); do
  kubectl apply -n "$namespace" -f - <<'MANIFEST'
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
    namespace: orbitjob-system
MANIFEST
done
```

`scripts/loadtest-ci.sh` performs this step between prepare and the run.
Widening a real installation stays a helm upgrade of
`operator.namespaceTenants`; this loop is fixture plumbing for namespaces
the tool itself created and deletes again.

### 4. Run

```bash
go run ./scripts/loadtest run --config "$CONFIG" --profile smoke \
  --run-id "$RUN_ID" --api-url http://localhost:18080
```

Thirty minutes for the smoke profile. It prints progress every few seconds:

```
loadtest progress: triggered=290 accepted=289 rejected=0 skipped=0 pace=1.00
```

and finishes with

```
run: triggered=500 accepted=500 rejected=0 skipped=0
```

`pace` is the feedback controller's multiplier against the profile's target
rate; `1.00` means it tracked the plan without throttling.

A scenario that expects `canceled` is cancelled by the generator itself: after
the trigger, the engine resolves the run's ledger id from the response's
occurrence key and calls `POST /api/v1/instances/{run_id}/cancel`. The API
patches `spec.cancelRequested` on the run's JobRun; the operator deletes the
Kubernetes Job; the run ends `Canceled`. The workload body sleeps long enough
that the cancel lands while the job is still running.

### 5. Verify

The verifier runs on this machine, so it needs a DSN it can reach. The Secret
stores an in-cluster host, which has to be rewritten to the port-forward:

```bash
kubectl port-forward -n orbitjob deploy/orbitjob-postgres 15432:5432 &

DSN=$(kubectl get secret orbitjob-database -n orbitjob-system \
  -o jsonpath='{.data.bootstrap-owner-dsn}' | base64 -d \
  | sed 's|orbitjob-postgres\.orbitjob\.svc:5432|127.0.0.1:15432|')

go run ./scripts/loadtest verify --run-id "$RUN_ID" --config "$CONFIG" \
  --orbitjob-dsn "$DSN" \
  --api-url http://localhost:18080 \
  --fixture-url http://localhost:18081
```

**Pass `--config`.** It defaults to `standard.yaml`, and the verifier refuses to
run when the config's profile disagrees with the profile the run recorded:

```
run smoke-... recorded profile "smoke" but --config is "standard"; pass the config the run used
```

Evidence comes from three places. The ledger: expected runs are rows in
`job_run_control_plane`, with their attempt trail in
`job_run_attempts_control_plane`, scoped to the revisions this run created.
The cluster: every terminal run's JobRun custom resource — named
`<definition>-<first 8 chars of the occurrence key>`, in the tenant's
namespace — must be present. The API: cross-tenant probes where each tenant's
key tries to read another tenant's definitions.

The ledger is row-level-security protected, so every per-tenant query runs in
a transaction that first sets `app.tenant_id` to that tenant's ULID; the
setting is transaction-local and rolled back with the read. The queries also
filter by this run's revision ids, so leftovers from earlier runs can never
feed the gates.

Six correctness checks run:

| Check | What it catches |
|---|---|
| `duplicate-succeeded-attempt` | one run with two succeeded attempts — the same occurrence executing twice |
| `tenant-leak` | one tenant reading another's rows |
| `duplicate-idempotent-side-effect` | the same idempotency key doing work twice |
| `terminal-regression` | a run leaving a terminal phase (`job_run.status_changed` audit events) |
| `expected-terminal-state` | a run ending in a phase other than the one its scenario declares |
| `jobrun-cr-present` | a terminal ledger run whose JobRun custom resource is gone |

**Verify straight after `run` returns fails spuriously.** The last Kubernetes
Jobs can take a few seconds to be observed after the schedule ends, and those
runs are not terminal yet. Wait for the ledger to settle:

```bash
psql "$DSN" -c "SELECT phase, count(*) FROM job_run_control_plane
                WHERE phase NOT IN ('Succeeded','Failed','Canceled')
                GROUP BY phase"
```

### 6. Report

```bash
go run ./scripts/loadtest report --run-id "$RUN_ID"
```

Writes `report.md` and `checksums.txt` into the run directory. A non-qualifying
run says so on the first line:

```
NON-STANDARD RUN - NOT A RELEASE QUALIFICATION
```

## Reading the output

Run counts exceed trigger counts. The smoke profile contains cron definitions
that fire on their own schedule, before, during and after the phases the `run`
command drives, and every occurrence is a ledger row. In the reference run on
the previous architecture the tool triggered 500 while the database
accumulated 726.

In the smoke profile the `failure` category is 220 of the 1200 definitions, so
a substantial `Failed` count is the test working, not the system breaking.

### Scenario families

A scenario family's name is a promise about what the workload does. Every
family runs a body that keeps its promise, one family per distinct body, and
a test fails if a family is added without one
(`TestCurlFamiliesReachTheirOwnEndpoint`, `TestNonCurlFamiliesMatchTheirBody`).

| Family | What it runs |
|---|---|
| `sha256-json-document` | python, hashes a 128-element JSON document |
| `sha256-text` | alpine, hashes the case id |
| `select-one` | postgres, `SELECT 1` over a connection URI in the args |
| `invalid-sql` | postgres, a statement that does not parse — fails |
| `dns-resolution` | busybox, resolves the load PostgreSQL by DNS |
| `secret-read-denied` | kubectl, reads a Secret the default identity is denied — fails |
| `forced-exit-nonzero`, `exit-nonzero-message` | a message then a non-zero exit — fails |
| `cancel-while-running` | python, runs long enough for the cancel to land |
| `plain-success` | python, the ordinary hash body |
| `dns-lookup` | busybox, `nslookup` of the load PostgreSQL |

`invalid-sql` appears in both `database` and `failure`: the same body, counted
under each category's own total because the categories test different things
around it — one is doing database work, the other is producing a failure.

Operator-rendered Jobs run as the namespace's default service account — the
CRD's job template has no service account field — so the old `pods-list`
family, which needed the operations Role, became the `dns-resolution` family
above. `secret-read-denied` stays meaningful: the default identity holds no
grants, so the API server denies the read and the job fails for exactly the
least-privilege reason `deploy/load/operations-rbac.yaml` documents.

The `curl` families reach the fixture at `deploy/load/fixture-configmap.yaml`,
one endpoint each:

| Family | Endpoint | Behaviour |
|---|---|---|
| `paginated-api` | `/api/pages?page=1` | 200 |
| `webhook-delivery` | `POST /webhook-origin` | 201, records an idempotency key |
| `idempotent-webhook` | `POST /ledger` twice | 201 then `duplicate: true`, one ledger entry |
| `file-download` | `/fixtures/json/events.json` | 200 |
| `rate-limit-then-success` | `/api/rate-limit-then-success` | 429 twice, then 200 |
| `server-error-then-success`, `retry-until-success` | `/api/fail-then-succeed` | 500 twice, then 200 |
| `retry-exhausted` | `/api/always-fail` | 500 every time |
| `slow-response-timeout` | `/api/slow` | holds the response for 30s |
| `invalid-json-content-type` | `POST /api/require-json` | 415 unless `Content-Type: application/json` |
| `tls-handshake` | `GET https://…:8443/healthz` | TLS handshake, no body |
| `tls-json-body` | `GET https://…:8443/api/pages?page=1` | TLS handshake plus a body |
| `fixture-json-download` | `/fixtures/json/events.json` | 200 |

The fixture answers HTTPS on 8443 with a self-signed certificate generated by
`deploy/load/make-tls-cert.sh`. The clients pass `--insecure`, so the `tls-*`
families prove a handshake completes, not that a chain validates.

The fixture runs a single replica because that state — the attempt counters and
the ledger — lives in the process. Two replicas put a coin flip in front of
every retry, and the families that need a third attempt failed about half the
time.

## Resetting

```bash
go run ./scripts/loadtest reset --confirm
```

Truncates the control-plane ledger (`job_run_control_plane`,
`job_run_attempts_control_plane`, `job_definition_revisions`), deletes every
tenant except `default`, deletes the labelled per-tenant load namespaces —
which garbage-collects the ScheduledJob and JobRun custom resources and the
rendered Kubernetes Jobs inside them — and clears the operator's ability to
resolve CRs into load tenants along with the namespaces themselves. Reset
is scoped to state the load tool created; it never touches the `monitoring`
namespace. That stack is long-lived and is installed and removed with
`make monitoring-up` and `make monitoring-down`, not by this tool.

The operator deployment's namespace table keeps entries for the deleted
namespaces. They are harmless — nothing is scheduled into a namespace that
does not exist — and the next `prepare` recreates the namespaces under the
same names, which makes the table right again.

It does not touch `test/load/runs/`. Remove a run directory by hand when you
want the disk back.
