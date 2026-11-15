# Load testing

Run controlled workloads against an isolated Kubernetes installation, from
preflight through verification and cleanup.

The tooling drives the Kubernetes control plane: definitions are ScheduledJob
custom resources and every run is a ledger row under
`job_run_control_plane`. Load tests are operator-run, advisory evidence. They
do not block PRs or merges, and a completed workflow only means the harness
produced its evidence; the operator reads the verdict in `result.json` and
decides how to use it.

## Profiles

| Profile | Config | Schedule duration | Qualification requested |
|---|---|---|---|
| `smoke` | `test/load/config/smoke.yaml` | 30m | no |
| `standard` | `test/load/config/standard.yaml` | 4h | yes |
| `long` | `test/load/config/long.yaml` | 8h | no |

The config's internal `profile` and the command's profile flag must agree:
`smoke.yaml` declares `profile: smoke`, and passing `--profile standard` with
it is rejected. Filenames are not profile identifiers: `smoke-faults.yaml`
also declares `smoke`, and `standard-dynamic.yaml` declares `standard`.

`smoke.yaml` has no faults inside its thirty-minute schedule;
`smoke-faults.yaml` adds a custom in-range plan. Dynamic pacing can extend
wall-clock duration beyond the configured virtual schedule. Fault-bearing
profiles require the recovery canaries and independent evidence described below.

There is one execution path — the Kubernetes control plane — so profiles no
longer declare execution modes. The `*-dual.yaml` and `smoke-k8s.yaml` configs
that exercised a legacy-versus-kubernetes split are deleted: the execution paths they
were designed to compare no longer coexist. Profiles declare `tenant_count: 4`. The generator
uses the project's ULID generator and persists the mapping in each run's
`tenant-ids.json`; prepare, run and verify read that same mapping. Different
runs receive different tenants. A slug such as `default` is a name, never a
tenant identifier. Custom profiles with explicit tenant IDs remain supported,
but generated declarations still include a full run-identity digest so their
revisions and evidence stay separate between runs.

`standard` is called a qualification profile because it applies the full
four-hour workload and minimum-instance rule. It describes the requested
workload, not a successful qualification. Missing recovery canaries, source
observations or continuous journal evidence produce `INCONCLUSIVE` even when
deployments become ready. See the evidence rules below and
[verification](verification.md). Use a
specific run's artifacts as evidence, not the profile name or workflow status.

## Before you start

The load test modifies an installation: it creates tenants/namespaces, extends
the operator mapping, restarts components and may inject database/component
faults. Use an isolated development installation and run one profile at a time;
the fixture's state and the reset scope are shared by load runs.

Follow [local deployment](local-deployment.md) for prerequisites, a source
installation and the separate stop-old-writers upgrade boundary. For a new
development installation, its shortcut is:

```bash
bash scripts/quickstart.sh
```

Then the fixtures and three port-forwards:

```bash
make loadtest-prepare     # deploys shared fixture/task infrastructure
kubectl rollout status -n orbitjob-load deployment/load-fixture --timeout=5m
kubectl rollout status -n orbitjob-load deployment/load-postgres --timeout=5m

kubectl port-forward -n orbitjob-system svc/orbitjob-admin-api 18080:8080 &
kubectl port-forward -n orbitjob-load  svc/load-fixture        18081:8080 &
kubectl port-forward -n monitoring     svc/kube-prometheus-stack-prometheus 9090:9090 &
```

The Prometheus forward is only needed by the dynamic profile
(`standard-dynamic.yaml`): its feedback controller reads live pressure metrics
from Prometheus at `prometheus_url`, which defaults to `localhost:9090`. When
Prometheus is unreachable the controller does not fail the run — it freezes
the pace multiplier and logs warnings. A missing port-forward therefore leaves
the last multiplier unchanged and does not fail setup. The static profiles never contact
Prometheus and ignore the forward.

`make loadtest-prepare` deploys manifests into `orbitjob-load`, `orbitjob-tasks`
and the default control-plane namespace. It does not seed tenants. The
tenants, namespaces, ScheduledJob definitions and API keys are created later by
the tool's own `prepare` command, which also applies fixture infrastructure.
That Go command and `scripts/loadtest-ci.sh` render operations RBAC for
`ORBITJOB_NAMESPACE`; the Makefile fixture target applies the default manifest
directly. For a namespace override, use those rendered paths.

Reapplying a ConfigMap/TLS Secret is not a fixture-code rollout. If reusing an
installation after fixture changes, restart `deployment/load-fixture` and wait
for it before starting a run. A restart during a run loses its in-memory journal.
The versioned `/journal` envelope includes a boot identity and complete sequence.
Verification rejects a changed boot identity or a sequence gap even when the
restarted fixture has produced new entries for this run.
The CI script performs this restart itself. Host-side settling and verification
read PostgreSQL through the load tool's Go driver. Tenant setup/reset use `psql`
inside the database Pod.

Check the fixtures are up before continuing:

```bash
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:18080/healthz   # 200
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:18081/ledger    # 200
```

### The host has to stay awake

The engine paces the schedule against the monotonic clock, and that clock
stops while a machine is suspended. A 30-minute profile requires 30 minutes of
awake time. Suspension stretches the wall-clock schedule and invalidates its
latency measurements.

`run` holds a sleep assertion itself on macOS, through `caffeinate`, for as
long as the run lasts. Keep the host on AC power; qualification preflight
rejects battery power on macOS. Other hosts still need an environment that
will remain awake and available. If the host is
suspended anyway — the assertion needs AC power on macOS — the run reports suspension on
exit so the extended wall-clock duration can be distinguished from system
latency:

```
loadtest: the host was suspended for about 1h35m0s; the schedule stretched to
1h46m12s of wall clock and the latency numbers are not usable
```

## Running

```bash
export RUN_ID="smoke-$(date -u +%Y%m%dT%H%M%SZ)"
RELEASE_NAMESPACE="${ORBITJOB_NAMESPACE:-orbitjob-system}"
DB_NAMESPACE="${ORBITJOB_DB_NAMESPACE:-orbitjob}"
export ORBITJOB_API_KEY=$(kubectl get secret bootstrap-api-key \
  -n "$RELEASE_NAMESPACE" -o jsonpath='{.data.api-key}' | base64 -d)
CONFIG=test/load/config/smoke.yaml
PROFILE=smoke
```

`bootstrap-api-key` is the Secret holding the plaintext key.
`orbitjob-database` holds the database credentials and is a different Secret.

### 1. Preflight

```bash
go run ./scripts/loadtest preflight --config "$CONFIG" \
  --images test/load/config/images.lock.yaml --profile "$PROFILE"
```

```
preflight: profile=smoke Docker capacity >= 6 CPU / 8 GiB
preflight: configuration and image lock valid
```

Preflight validates config/image-lock structure, required tools, Docker
capacity, and profile-specific workspace/power/static-volume constraints. The
current implementation does not pull images or check their availability,
including without `--check-only`. It does not prove the cluster is ready or
that any workload ran. `--images-only` checks only the lock file without Docker.

The Makefile shortcuts `loadtest-smoke` and `loadtest-long` invoke preflight
only. `loadtest-full` invokes preflight and generation, then prints a manual
review instruction; `loadtest-run` prints guidance. None executes a full profile.

### 2. Generate

```bash
go run ./scripts/loadtest generate --config "$CONFIG" \
  --images test/load/config/images.lock.yaml --profile "$PROFILE" \
  --run-id "$RUN_ID"
```

Writes `test/load/runs/$RUN_ID/generated/` — 1200 ScheduledJob declarations
for the smoke profile, plus a summary of their distribution by category.

Repeating generation reuses the recorded tenant mapping and requires an
identical complete definition corpus, including images and workload arguments.
Changes to image/scenario inputs that alter the generated definitions require
a new run ID, even before execution, so prepared revision IDs cannot silently
address an older workload.
A run's
first execution exclusively creates `run-started.json`; generation, prepare
and a second execution refuse to overwrite evidence after that point. Use a
new run ID for another attempt. Trigger idempotency keys also include the run
ID, so retries within one run converge without reusing another run's work.
The tenant record also binds the full resolved input config from generation.
Prepare, run and verify reject a changed corpus, cron interval, rate or other
config field even when profile/seed/tenant count are unchanged. Changing the
workload requires a new run ID and generation; explicit tenant IDs cannot be
silently replaced by an older mapping.

A declaration is a full ScheduledJob spec. Cron definitions carry the interval
the config sets; manual-only definitions carry `0 0 30 2 *` — February 30th,
which parses as a standard cron expression but matches no instant, so the
CRD's non-empty-schedule rule is satisfied while the scheduler never fires the
definition. Every declaration raises its history limits to the corpus size so
the retainer cannot prune evidence while the run is still going. Because the
operator renders Kubernetes Jobs with image, command and args only — no env
block, no service account — each workload body has its case id, the fixture
addresses and the database DSN stored as literal arguments.

### 3. Prepare

```bash
go run ./scripts/loadtest prepare --config "$CONFIG" --profile "$PROFILE" \
  --run-id "$RUN_ID" --api-url http://localhost:18080
```

```
prepare: declared 1200 definitions across 4 tenants
```

Prepare provisions everything the run depends on:

`ORBITJOB_NAMESPACE` selects the control-plane namespace, and
`ORBITJOB_DB_NAMESPACE` selects the installation PostgreSQL namespace.
Prepare and the CI runner render the operations Role and RoleBinding for the
selected control plane; their task ServiceAccount stays in `orbitjob-tasks`.

1. Tenant rows with the run's persisted generated ULIDs are seeded into the installation
   database (`INSERT ... ON CONFLICT DO NOTHING` through `kubectl exec psql`;
   the API generates its own IDs, and the namespace table needs these exact ids).
2. One namespace per tenant is created — `orbitjob-tasks-<tenant-ULID>`,
   labelled `orbitjob.io/managed-by=orbitjob-loadtest` — carrying the same
   LimitRange and a run-specific ResourceQuota from
   `tenantNamespaceManifest` in `scripts/loadtest/prepare.go`, because this is
   where rendered Kubernetes Jobs execute.
3. The operator's namespace table (`OPERATOR_NAMESPACE_TENANTS`) is extended
   with one `namespace=tenant` entry per load tenant and the operator is
   restarted only if the table changed. Existing mappings are preserved.
4. The corpus is declared: one ScheduledJob custom resource per definition,
   batched into one multi-document manifest per tenant (kept under
   `cr-manifests/` in the run directory as evidence).
5. Prepare waits until every declaration has an active revision — read from
   each CR's `status.activeRevision` — and records the case-to-revision
   mapping in `created-definitions.json`. The trigger route addresses
   revisions, so this mapping is what the run engine triggers.
6. A tenant API key is created per tenant, bound to the `TenantAdminAccess`
   policy, into `tenant-keys.json` (plaintext, which is why
   `test/load/runs/` is gitignored), alongside `tenant-namespaces.json` for
   the verifier.

Tenancy comes from the API key on every call; there is no tenant header. The
load-test flow declares definitions as custom resources and waits for the
operator to materialize their revisions. The separate typed definition API
does not replace this fixture-generation path.

Before execution, prepare can be retried: unchanged ScheduledJobs keep their
revision and still-valid recorded tenant keys are reused. After execution has
started, prepare refuses that run ID. After `reset`, preserve started runs as
historical artifacts and generate/prepare a new run ID. An unstarted run can
reuse its tenant mapping, but prepare must refresh its CR revision/key records
because reset removed their database and namespace state.

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
  kubectl apply -n "$namespace" -f - <<MANIFEST
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
done
```

`scripts/loadtest-ci.sh` performs this step between prepare and the run.
Widening a real installation stays a helm upgrade of
`operator.namespaceTenants`; this loop configures fixtures in namespaces
the tool itself created and deletes again.

### 4. Run

```bash
go run ./scripts/loadtest run --config "$CONFIG" --profile "$PROFILE" \
  --run-id "$RUN_ID" --api-url http://localhost:18080
```

Fault recovery also requires the protected `ORBITJOB_DSN` environment variable
used by verification. Do not place credentials in command arguments. Generation
writes separate `generated/recovery.json` canaries; preparation writes their
resolved identities in `recovery-prepared.json`. These resources do not contribute
to the offered workload, coverage or minimum-instance count. Override the local
fixture endpoint with `--fixture-url` when it differs from the default.

The smoke profile has a thirty-minute virtual schedule. Throttling, capacity
waits and fault restoration can extend wall-clock time. It prints progress:

```
loadtest progress: triggered=290 accepted=289 rejected=0 skipped=0 pace=1.00
```

Example final counters (values depend on the run):

```
run: triggered=500 accepted=500 rejected=0 skipped=0
```

`pace` is the feedback controller's multiplier against the profile's target
rate; `1.00` means it tracked the plan without throttling.

At execution start the tool captures the resolved config's SHA-256 and the
checkout's commit/dirty flag. `run-stats.json` stores these alongside final
counters and one receipt per accepted manual trigger (tenant, revision and
occurrence key). HTTP 200/201 requires a complete manual-run identity; HTTP 409
is a rejection. Idempotent replay is the API's HTTP 200 with the same identity.

A scenario that expects `canceled` is cancelled by the generator itself: after
the trigger, the engine resolves the run's ledger id from the response's
occurrence key and calls `POST /api/v1/instances/{run_id}/cancel`. The API
patches `spec.cancelRequested` on the run's JobRun; the operator deletes the
Kubernetes Job; the run ends `Canceled`. The workload body sleeps long enough
that the cancellation request arrives while the Job is still running.

### 5. Verify

The verifier runs on this machine, so it needs a DSN it can reach. The Secret
stores an in-cluster host, which has to be rewritten to the port-forward:

```bash
kubectl port-forward -n "$DB_NAMESPACE" deploy/orbitjob-postgres 15432:5432 &

ORBITJOB_DSN=$(kubectl get secret orbitjob-database -n "$RELEASE_NAMESPACE" \
  -o jsonpath='{.data.admin-dsn}' | base64 -d)
export ORBITJOB_DSN="${ORBITJOB_DSN/orbitjob-postgres.${DB_NAMESPACE}.svc:5432/127.0.0.1:15432}"

go run ./scripts/loadtest verify --run-id "$RUN_ID" --config "$CONFIG" \
  --api-url http://localhost:18080 \
  --fixture-url http://localhost:18081
```

**Pass the original `--config`.** It defaults to `standard.yaml`. Verification
rejects a different run ID or config fingerprint, including changes within the
same profile to rates, duration, seed, fault plan or minimum count. Source
identity comes from execution-time stats, rather than the checkout used later
to verify. The generated corpus, created revisions, tenant keys and namespace
mapping must agree before evidence collection starts; missing actors or duplicate
revision/case records cannot silently reduce the tested scope.

Evidence comes from the database, Kubernetes, fixture journal and API. The
ledger: expected runs are rows in
`job_run_control_plane`, with their attempt trail in
`job_run_attempts_control_plane`, scoped to the revisions this run created.
The cluster: every terminal run's JobRun custom resource — named
`<bounded-definition-prefix>-<complete occurrence digest>`, in the tenant's
namespace — must be present with the expected source UID, revision, occurrence
and trigger. Matching legacy names are accepted only with that complete identity. The API: cross-tenant probes where each tenant's
key tries to read another tenant's definitions.

The ledger is row-level-security protected, so every per-tenant query runs in
a transaction that first sets `app.tenant_id` to that tenant's ULID; the
setting is transaction-local and rolled back with the read. The queries also
filter by this run's revision ids, so leftovers from earlier runs can never
feed the gates.

The verifier runs these checks, plus the configured fault checks:

| Check | What it catches |
|---|---|
| `duplicate-succeeded-attempt` | one run with two succeeded attempts — the same occurrence executing twice |
| `tenant-leak` | one tenant reading another's definition through the API |
| `duplicate-idempotent-side-effect` | the same idempotency key doing work twice |
| `terminal-regression` | a run leaving a terminal phase (`job_run.status_changed` audit events) |
| `expected-terminal-state` | a run ending in a phase other than the one its scenario declares |
| `jobrun-cr-present` | a terminal ledger run whose JobRun is missing or lacks the expected namespace/full identity |
| `manual-trigger-accounting` | offered events, accepted receipts and observed manual ledger rows must reconcile by tenant, revision and occurrence |

Rejected or skipped triggers make the offered workload incomplete and produce
`INCONCLUSIVE`; no failure budget is inferred from a few healthy remaining rows.
Every accepted manual receipt requires exactly one matching ledger row. Cron
rows cannot substitute for missing manual work. Historical stats without the
execution config/source record or accepted receipts cannot establish a new
`PASS`; preserve their old reports as dated evidence and use a new run ID.

Empty attempts, phase-transition observations, tenant probes or fixture side-effect
journals are evidence gaps, including on smoke runs. They produce `INCONCLUSIVE`,
and the corresponding check does not print `PASS`. The transition scan reads this
run's revision-scoped audit facts before looking for terminal regressions. The
fixture exposes an append-only side-effect journal independently of its dedupe
set; a broken dedupe guard can therefore produce duplicate observations. Only
entries carrying this run's generated case IDs count, and fixture restart loses
this in-memory evidence rather than proving correctness.

Faults use the engine's same virtual phase clock under dynamic pacing. The run
persists its planned faults, cancels and joins the injector (including bounded
restoration) before writing final stats, and checks that every planned fault has
one successful injection/restore record. An injection failure or absent record is
`INCONCLUSIVE`; a readiness recovery timeout is `FAIL`, while a canceled
observation remains `INCONCLUSIVE`.

Recovery checks require the component's specific business result:

- Operator and Admin API faults require a separate canary's accepted identity,
  exact source/revision, ledger run, owned Kubernetes objects and journal effect.
  Admin replay must return the same accepted identity.
- PostgreSQL faults additionally require an original in-flight canary whose
  owned Job completes during the observed outage, subsequent terminal persistence,
  and a new canary after recovery.
- Scheduler faults require a real canary SLI event and a newly evaluated SLO
  budget with the expected calculation. Job success alone is insufficient.

Fixture-assigned, versioned checkpoints bind fault, restoration, readiness,
submission and observation to the same run, fault and canary nonce. Independent
database clock samples constrain cross-source timestamps; ambiguous timing
remains `INCONCLUSIVE`. Verification rereads source facts rather than accepting
`business_recovered` or free-text claims in stats. Missing or unsupported evidence
cannot enable `PASS`. Lease-loss and arbitrary network-partition behavior are
outside these four fault types.

Recovery deadlines use wall time while scheduled fault offsets use the engine's
virtual phase clock. Normal load completion permits bounded probe draining;
user cancellation stops observations while restoration retains its own bounded
cleanup deadline. Installed fault tests of the candidate and a complete standard
run remain necessary before claiming qualification.

The last Jobs may still be executing when `run` returns; verification reports
that incomplete evidence as `INCONCLUSIVE`. With `ORBITJOB_DSN` exported as above,
sample the current run's projected ledger with:

```bash
go run ./scripts/loadtest settle --run-id "$RUN_ID"
```

This command performs one sample and prints only the nonterminal count on success;
it does not wait. It loads `created-definitions.json` from `test/load/runs/$RUN_ID`
(or `--run-root`), groups revisions by tenant, and reads each group in a transaction
with transaction-local `app.tenant_id`, rolled back afterwards. Invalid or empty definitions,
a tenant with no matching ledger observations, and database/transaction errors
exit nonzero without printing a count. Connection details remain outside argv and
raw driver errors are not logged. Each sample has a bounded database timeout.

`scripts/loadtest-ci.sh` polls this command up to 60 times, sleeping 15 seconds
between nonzero or unknown samples; a failed command or malformed output is
`unknown`, never zero. On exhaustion it warns and still runs the independent
verifier to preserve the final missing-evidence verdict and report.

Settling is not qualification evidence by itself. It samples the ScheduledJob
corpus's projected JobRun ledger, not unprojected CRs or Workflow completion.
Cron definitions remain active after the generator finishes, so observe the
run-scoped ledger and collect verification promptly. A zero sample is not a
promise that no later occurrence can start. Empty sources stay inconclusive.

### 6. Report

```bash
go run ./scripts/loadtest report --run-id "$RUN_ID"
```

Writes `report.md` and `checksums.txt` into the run directory. A non-qualifying
run says so on the first line:

```
NON-STANDARD RUN - NOT A RELEASE QUALIFICATION
```

Reporting requires readable `run-record.json`, `run-stats.json` and `result.json`
with consistent run, qualification, profile, seed, start and source identities.
Missing/corrupt artifacts stop report generation. A saved PASS also requires
completed, reconciled execution stats and all required checks to be present and
PASS; it cannot coexist with `completed=false` or missing execution identity.
Docker CPU/memory values are
labeled as configured requirements, not an observed allocation. The hosted job
summary uses the exact attempted run directory recorded before setup; it never
selects another run's newest report after an early failure.

## Reading the output

Run counts can exceed trigger counts. The smoke profile contains cron definitions
that fire on their own schedule, before, during and after the phases the `run`
command drives, and every occurrence is a ledger row. Compare the particular
run's trigger counters with its revision-scoped database evidence; historical
counts from a different architecture do not establish a current baseline.

In the smoke profile the `failure` category is 220 of the 1200 definitions, so
a substantial `Failed` count can be an expected test outcome rather than a system failure.

### Scenario families

A scenario family's name describes the workload behavior. Every
family has a distinct workload body that implements that behavior, and
a test fails if a family is added without one
(`TestCurlFamiliesReachTheirOwnEndpoint`, `TestNonCurlFamiliesMatchTheirBody`).

| Family | What it runs |
|---|---|
| `sha256-json-document` | python, hashes a 128-element JSON document |
| `sha256-text` | alpine, hashes the case id |
| `select-one` | postgres, `SELECT 1` over a connection URI in the args |
| `invalid-sql` | postgres, a statement that does not parse — fails |
| `dns-resolution` | busybox, resolves the load PostgreSQL by DNS |
| `service-account-token-absent` | busybox, succeeds only when the projected ServiceAccount token path is absent |
| `forced-exit-nonzero`, `exit-nonzero-message` | a message then a non-zero exit — fails |
| `cancel-while-running` | python, runs long enough for cancellation to arrive |
| `plain-success` | python, the ordinary hash body |
| `dns-lookup` | busybox, `nslookup` of the load PostgreSQL |

`invalid-sql` appears in both `database` and `failure`: the same body, counted
under each category's own total because the categories test different things
around it — one is doing database work, the other is producing a failure.

Operator-rendered Jobs set `automountServiceAccountToken: false`. The old
`pods-list` family, which needed the operations Role, became the
`dns-resolution` family above. `service-account-token-absent` directly checks
that the standard projected token file does not exist, so the scenario proves
the tokenless workload boundary instead of inferring it from an API failure.

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

The fixture runs a single replica because its attempt counters and side-effect
journal are process memory. Multiple replicas would split that state and make
retry scenarios depend on which replica receives each request.

## Resetting

```bash
go run ./scripts/loadtest reset --confirm
```

Deletes only tenants with both a `load-` slug and the
`orbitjob.io/managed-by=orbitjob-loadtest` metadata marker written by prepare.
The dependent history, audit rows and SLI deduplication facts are removed
before those tenants in one database transaction. Unmarked tenants, including
older fixtures without ownership metadata, are preserved for explicit manual
review. A name or an ID alone is not proof of ownership.

Reset first deletes the labelled load namespaces and waits for their workloads
to stop, then purges the owned database rows. Shared fixture infrastructure and
the `monitoring` namespace remain under their installation/removal commands.

The operator deployment's namespace table keeps entries for the deleted
namespaces. They are harmless — nothing is scheduled into a namespace that
does not exist. Preparing the same unstarted run reuses its persisted tenant
identities; a new run adds new mappings for its new tenants.

It does not touch `test/load/runs/`. Use `clean --run-id "$RUN_ID" --confirm`
to remove only that run's local artifacts, or remove the directory manually when that storage is no longer required.
