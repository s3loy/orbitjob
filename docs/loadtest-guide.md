# Load testing

Run a controlled ScheduledJob corpus against an isolated Kubernetes installation.
The load tool generates declarations, prepares tenants and namespaces, drives
manual triggers, injects configured faults and verifies independent execution
evidence. Read the verdict for the exact run; a workflow completing or a profile
being named `standard` does not establish qualification.

Load tests are operator-run evidence and do not themselves block merges. They
modify installation state and can stop control plane or database components.
Use a dedicated installation, run one profile at a time and preserve its
artifacts before cleanup.

## Profiles and configuration

| Config under `test/load/config/` | `--profile` | Virtual schedule | Qualification requested | In-range faults |
|---|---|---|---|---|
| `smoke.yaml` | `smoke` | 30 minutes | No | None |
| `smoke-faults.yaml` | `smoke` | 30 minutes | No | Operator at 25m, PostgreSQL at 27m |
| `standard.yaml` | `standard` | 4 hours | Yes, at least 10,000 qualified runs | Scheduler at 180m, Operator at 190m, Admin at 210m, PostgreSQL at 220m |
| `standard-dynamic.yaml` | `standard` | 4 hours before pacing extensions | Yes, at least 10,000 qualified runs | Standard plan |
| `long.yaml` | `long` | 8 hours | No | Scheduler at 370m, Operator at 390m, Admin at 400m, PostgreSQL at 410m |

The flag must match the config's internal `profile`; the filename is not the
profile identifier. Standard requires a clean source/configuration tree and
Docker capacity of at least 10 CPU / 8 GiB. Smoke requires 6 CPU / 8 GiB; long
uses 10 CPU / 8 GiB. These checks inspect configured Docker capacity rather
than measuring the resources available throughout a run.

All supplied configs declare 1,200 definitions across four generated tenants:
280 data-processing, 220 database, 240 HTTP/webhook, 140 operations, 220 failure
and 100 external-probe definitions. Product trigger types are 840 manual and
360 cron; the origin labels include 120 webhook cases within that manual
surface. The ScheduledJob workload does not qualify Function or Workflow
execution merely because the installation supports them.

The generator persists real tenant ULIDs in `tenant-ids.json`; prepare, run and
verify must reuse that mapping. New run IDs get separate tenants and declaration
identity digests. A slug such as `default` is not a tenant ID. Explicit tenant-ID
configs remain subject to the tool's validation and ownership checks.

Static standard uses ten-minute cron intervals. Smoke and long use hourly cron.
Dynamic standard computes rates and active limits from inspected resources,
then adjusts trigger pacing from Prometheus. The current `run` flag
`--prometheus-url` defaults to `http://localhost:9090`; set that flag explicitly
for another endpoint. Prometheus failures freeze pacing with warnings rather
than failing the run. Do not infer live feedback from the config file alone.

## Before you begin

Install the current source using [local development](local-deployment.md) in a
dedicated cluster. The harness assumes the repository's local PostgreSQL
Deployment named `orbitjob-postgres`, database/user `orbitjob` / `postgres` and
`psql` access inside that Pod. Its prepare/reset/fault paths are not a general
external-database harness.

Obtain cluster administration access and the installation bootstrap API key.
The tool creates tenants/namespaces, extends the Operator mapping, applies CRs,
creates tenant keys and may restart or fault components. It updates the
Operator's mapping directly; it does not establish a complete production tenant
installation with matching Admin/admission/archive mappings and policies.
The additional fixture grants below supply its manual-trigger CR surface.
Do not use this procedure as production tenant onboarding or network-isolation
qualification.

Install Docker, kind, kubectl, Helm, Go, curl, Git, OpenSSL and Bash. Keep the
host awake for the full run. On macOS the engine uses a process-bound
`caffeinate -i -s` assertion, and standard preflight requires AC power. Other
platforms need their own no-suspend arrangement. Suspension stretches schedules
and invalidates latency measurements. Dynamic pacing and fault restoration can
extend wall time beyond the configured virtual schedule.

Run all commands from the selected repository root. Set one configuration and
run identity for the complete sequence:

```bash
export CONFIG=test/load/config/smoke.yaml
export PROFILE=smoke
export RUN_ID="smoke-$(date -u +%Y%m%dT%H%M%SZ)"
export ORBITJOB_NAMESPACE=orbitjob-system
export ORBITJOB_DB_NAMESPACE=orbitjob
export ORBITJOB_API_URL=http://localhost:18080
kubectl config current-context
```

Use `standard` / `test/load/config/standard.yaml` together for qualification.
Never reuse a run ID after execution has started. Run artifacts include
plaintext API keys; `test/load/runs/` is ignored by Git, and generated files
containing secrets use private permissions. Keep artifacts out of public logs.

## 1. Validate and generate

```bash
go run ./scripts/loadtest preflight --config "$CONFIG" \
  --images test/load/config/images.lock.yaml --profile "$PROFILE" --check-only
go run ./scripts/loadtest generate --config "$CONFIG" \
  --images test/load/config/images.lock.yaml --profile "$PROFILE" --run-id "$RUN_ID"
```

Preflight checks configuration, the six-entry digest image lock, required tools,
Docker capacity, profile rules, source cleanliness where required and static
minimum-instance feasibility. The current implementation does not pull images
or mutate the cluster even without `--check-only`; independently ensure that
nodes can obtain the locked images. `--images-only` validates only the lock and
does not establish environment readiness.

Generation writes `generated/definitions.json`, a category summary, tenant IDs
and separate fault-recovery canaries. Review the generated workload before
preparation. Canaries are outside the offered workload, coverage and minimum
instance count. A config/image/corpus error stops here before cluster mutation.

## 2. Prepare shared fixtures and access

For default namespaces, the Make target deploys fixture infrastructure:

```bash
make loadtest-prepare
kubectl rollout status -n orbitjob-load deployment/load-fixture --timeout=5m
kubectl rollout status -n orbitjob-load deployment/load-postgres --timeout=5m
```

The target creates the shared load/task namespaces, operations RBAC, HTTP/TLS
fixture and separate fixture PostgreSQL. It does not create the run's tenants.
For a custom control plane namespace, use
`scripts/render-local-manifest.sh load-operations "$ORBITJOB_NAMESPACE"` for
the operations RBAC instead of applying the default binding unchanged. The
Go prepare command also renders that override and applies the infrastructure.

When fixture code or its generated certificate changed between runs, restart
`deployment/load-fixture` and wait before starting the next run. Applying a
ConfigMap/Secret does not restart an existing fixture Pod. Never restart the
fixture during a run: its counters, dedupe set and append-only journal are
process memory. The single-replica fixture's boot identity and journal sequence
are continuity evidence.

Start the following forwards in separate terminals:

```bash
kubectl port-forward -n "$ORBITJOB_NAMESPACE" svc/orbitjob-admin-api 18080:8080
```

```bash
kubectl port-forward -n orbitjob-load svc/load-fixture 18081:8080
```

```bash
kubectl port-forward -n "$ORBITJOB_DB_NAMESPACE" deployment/orbitjob-postgres 15432:5432
```

Confirm that each forward bound its port, and stop if a different process is
already listening. Check `http://localhost:18080/healthz` and
`http://localhost:18081/ledger` return HTTP 200. Those checks are prerequisites,
not execution evidence.

Load credentials without putting their values in argv:

```bash
export ORBITJOB_API_KEY="$(kubectl get secret bootstrap-api-key \
  -n "$ORBITJOB_NAMESPACE" -o jsonpath='{.data.api-key}' | base64 -d)"
ORBITJOB_DSN="$(kubectl get secret orbitjob-database \
  -n "$ORBITJOB_NAMESPACE" -o jsonpath='{.data.admin-dsn}' | base64 -d)"
export ORBITJOB_DSN="${ORBITJOB_DSN/orbitjob-postgres.${ORBITJOB_DB_NAMESPACE}.svc:5432/127.0.0.1:15432}"
```

This endpoint substitution matches the local installation's DSN. Inspect the
actual endpoint privately if customized. The admin login supplies the
revision-scoped evidence reads with transaction-local tenant context; the
bootstrap owner is not an interchangeable read identity.

For dynamic standard, install and check [monitoring](operations/monitoring.md),
then keep its Prometheus forward active on 9090. Static profiles do not use
Prometheus feedback.

## 3. Prepare the run

```bash
go run ./scripts/loadtest prepare --config "$CONFIG" --profile "$PROFILE" \
  --run-id "$RUN_ID" --api-url "$ORBITJOB_API_URL"
```

Preparation performs these mutations:

1. Seeds owned tenant rows with the persisted IDs in the installation database.
2. Creates one labelled `orbitjob-tasks-<tenant-id>` namespace per tenant, with
   LimitRange and quota for the generated corpus.
3. Extends `OPERATOR_NAMESPACE_TENANTS` without replacing prior mappings and
   restarts the Operator when that table changes.
4. Applies ScheduledJob declarations, waits for each actual active revision
   and saves case-to-revision/namespace records.
5. Creates or reuses a tenant key with the `TenantAdminAccess` preset and
   resolves independent recovery canary identities.

An unstarted preparation can be retried: unchanged declarations retain their
revisions and valid recorded keys are reused. Once execution starts, preparation
refuses that run ID. If reset removed an unstarted run's cluster state,
preparation must refresh its revisions and key records before use.

The generated Operator Role includes Pod `patch` so the Operator can release
the protected finalizer after recording durable termination evidence. Without
that grant, terminal projection and cleanup can remain blocked.

### Grant fixture trigger publication

The namespaces were created after chart installation. Give the Admin API only
the fixture CR publication surface in those tool-owned namespaces:

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
    resources: ["jobruns", "workflowruns"]
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
    namespace: ${ORBITJOB_NAMESPACE}
MANIFEST
done
```

Without these grants, manual publication can fail after the API receives a
trigger. This loop is fixture infrastructure restricted by the ownership label;
normal installation namespace changes belong in reviewed Helm values.

## 4. Execute

```bash
go run ./scripts/loadtest run --config "$CONFIG" --profile "$PROFILE" \
  --run-id "$RUN_ID" --api-url "$ORBITJOB_API_URL" \
  --fixture-url http://localhost:18081
```

Fault recovery uses the exported `ORBITJOB_DSN`. Dynamic tuning additionally
uses `--prometheus-url`; `--tune-deployments` explicitly patches the Admin API's
trigger-rate environment and restarts it. That flag requires a dynamic config.
Review `--dry-run-tuning` output before opting into deployment changes.

The engine persists its execution config hash, source commit/dirty state,
planned faults, final counters and one identity receipt per accepted manual
trigger. HTTP 200/201 must contain a complete run identity; HTTP 409 is a
rejection. Idempotent replay returns the same accepted identity.

A scenario expecting cancellation is canceled by the generator through the API,
not by its workload body. The fixture's workload runs long enough for the
request to arrive. An accepted cancellation still requires the Operator's
physical stop and terminal ledger evidence.

Faults follow the same virtual phase clock as pacing. The injector performs
bounded restoration before final stats are written. The PostgreSQL fault
observes an in-flight owned canary across the outage and probes fresh work
later. Scheduler recovery requires actual SLI/SLO evaluation evidence, while
Admin and Operator recovery require the independent canary's exact source,
revision, ledger, Kubernetes objects and journal effect. Deployment readiness
alone does not establish business recovery. Lease loss and arbitrary network
partitions are outside these four fault types.

Interrupting a run preserves truncated evidence. Inspect the actual component
state and fault restoration records before cleanup or another run. Do not reuse
the run ID to overwrite an interrupted execution.

## 5. Settle and verify

The last accepted Jobs can still be executing when the schedule ends. Sample
the current run's projected ledger:

```bash
go run ./scripts/loadtest settle --run-id "$RUN_ID"
```

`settle` performs one bounded sample and prints a nonterminal count. It is not
a waiting command. Failed/malformed samples are unknown, never zero. A tenant
with no observed matching rows is an evidence gap. A zero sample does not
establish that all CRs projected or no future cron occurrence can start; collect
verification promptly while preserving the run's sources.

Run the verifier with the original config:

```bash
go run ./scripts/loadtest verify --run-id "$RUN_ID" --config "$CONFIG" \
  --api-url "$ORBITJOB_API_URL" --fixture-url http://localhost:18081
```

The verifier reconciles generated definitions, revisions, tenant keys,
namespaces, execution stats and accepted receipts before collecting evidence.
It reads tenant-scoped database rows, JobRun identities, API isolation probes,
fixture journal and fault-specific sources. Every accepted manual receipt needs
exactly one corresponding manual ledger row; cron activity cannot fill that
gap. It checks duplicate successful attempts, duplicate idempotent effects,
terminal regression, expected terminal states and full JobRun identity.

The fixture journal is independent of its dedupe set. Its boot identity and
complete sequence interval must remain intact; a restart loses evidence rather
than proving no duplicate effect. Fault checkpoints bind injection,
restoration, readiness, submission and observation to the same run/fault/canary.
Independent database clock samples bound timing uncertainty. Unsupported or
missing sources cannot produce a PASS.

## 6. Read and retain the result

```bash
go run ./scripts/loadtest report --run-id "$RUN_ID"
```

The command writes `report.md` and `checksums.txt` alongside `result.json`,
`run-record.json` and `run-stats.json`. It rejects missing, corrupt or
inconsistent artifact identity. Nonqualification reports begin with
`NON-STANDARD RUN - NOT A RELEASE QUALIFICATION`.

| Verdict | Meaning |
|---|---|
| `PASS` | Required checks passed for this recorded run and scope; smoke/long remain nonqualification runs |
| `FAIL` | A checked correctness/recovery condition failed, or a qualification run did not meet its minimum qualified-run count |
| `INCONCLUSIVE` | Missing/incomplete evidence, rejected/skipped workload, truncation, in-flight runs, coverage gaps or unsupported recovery observations prevent the requested conclusion |

Qualification also requires every created definition to have observed runs and
at least 10,000 qualified terminal runs with their required CR evidence. Smoke
can leave some corpus definitions untriggered without failing its coverage
rule; empty attempts, transitions, tenant probes or journal evidence still
prevent a complete verdict.

Run totals can exceed offered manual trigger counts because cron declarations
run independently. Expected failure scenarios should end `Failed`; that count
alone is not a product failure. Compare actual terminal outcomes with each
scenario's declared expectation. Do not use an old report or a newer checkout
to relabel historical execution source or incomplete evidence.

The corpus includes JSON hashing, SQL success/invalid SQL, DNS, token absence,
HTTP pagination/downloads, webhooks, retries, timeouts, TLS handshakes and
explicit failure/cancellation. Rendered workload Pods disable projected
ServiceAccount tokens. The HTTPS fixture uses a generated self-signed
certificate and clients use `--insecure`; those cases test a handshake, not
certificate-chain validation. Keep these limits with reported results.

## Convenience commands

`scripts/loadtest-ci.sh smoke` or `standard` automates the local fixture,
forwarding, reset, generation, prepare, trigger grants, run, settle, verify and
report sequence. It resets all positively owned prior load tenants before
starting, so use it only in the dedicated installation after preserving needed
evidence. It accepts only those two profiles, not arbitrary config variants.
Its settle loop samples up to 60 times at 15-second intervals, then still runs
the independent verifier when the wait expires. It does not convert unknown
settling into success.

`make loadtest-smoke` and `make loadtest-long` run preflight only.
`make loadtest-full` runs standard preflight and generation, then requires
manual review; it does not execute four-hour qualification.

## Reset and cleanup

After retaining the report and required private artifacts, remove owned live
load state with:

```bash
go run ./scripts/loadtest reset --confirm
```

Reset discovers labelled load namespaces, deletes them and waits for their
workloads to stop before purging owned database state transactionally. Tenant
ownership requires both the `load-` slug and the metadata marker
`orbitjob.io/managed-by=orbitjob-loadtest`; names alone do not authorize deletion.
Unmarked historical fixtures require manual review. Any namespace deletion or
SQL failure stops the reset; do not force evidence finalizers to finish it.

Reset does not remove shared fixtures, monitoring or local artifacts. It leaves
Operator mapping entries for deleted namespaces; they no longer resolve live
work there. A new run adds its own persisted tenants. Keep that fixture behavior
separate from maintained production values.

Remove one run's local artifacts only when no longer needed:

```bash
go run ./scripts/loadtest clean --run-id "$RUN_ID" --confirm
```

`clean` removes that run directory only. Stop only the forwards started for
this run. Remove shared fixture manifests or the isolated cluster separately
after reviewing ownership and preserving any needed execution evidence.
