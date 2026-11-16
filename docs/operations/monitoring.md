# Monitor an installation

Monitor control plane health, database availability, execution uncertainty,
resource pressure and log storage together. A ready Pod or a successful scrape
does not establish that a submitted workload completed correctly.

The repository provides an optional kube-prometheus-stack configuration for the
local kind environment. It is a separate Helm release from OrbitJob. Production
installations can use an existing monitoring stack with equivalent scrape and
alert configuration.

## Before you begin

Obtain permission to install monitoring CRDs and cluster readers, a persistent
StorageClass, sufficient cluster capacity and kubectl access to the intended
context. The helper installs `kube-prometheus-stack` in `monitoring`, with a
10-GiB Prometheus PVC, 36-hour retention and an 8-GiB retention size. The
Makefile pins chart version 91.4.1.

The supplied monitors target control plane namespace `orbitjob-system` and
PostgreSQL namespace `orbitjob`. Adjust a maintained copy for other namespaces,
ports or workload labels before applying it. The helper values disable kind's
unavailable controller-manager, scheduler, etcd and proxy scrape endpoints.
They also configure Grafana `admin` / `admin` and anonymous Viewer access.
Restrict this profile to a trusted local environment; set the installation's
normal access policy before exposing a shared monitoring service.

## Install the local stack

From the repository root, inspect the target context and install:

```bash
kubectl config current-context
make monitoring-up
kubectl rollout status -n monitoring deployment/kube-prometheus-stack-grafana --timeout=5m
kubectl get pods,pvc -n monitoring
```

The helper installs kube-prometheus-stack, OrbitJob monitors, alert rules and
the run-ledger dashboard. It does not deploy PostgreSQL exporter. For the
repository's local PostgreSQL manifest, deploy the exporter separately:

```bash
kubectl apply -f deploy/monitoring/postgres-exporter.yaml
kubectl rollout status -n orbitjob deployment/orbitjob-postgres-exporter --timeout=3m
```

That exporter reads the local `orbitjob-postgres` Secret and endpoint. External
PostgreSQL needs an appropriately scoped monitoring credential and the external
service's exporter/monitoring procedure; do not apply the local credential
manifest unchanged.

Start port-forwards in separate terminals:

```bash
kubectl port-forward -n monitoring svc/kube-prometheus-stack-prometheus 9090:9090
```

```bash
kubectl port-forward -n monitoring svc/kube-prometheus-stack-grafana 3000:80
```

Open Prometheus at `http://localhost:9090` and Grafana at
`http://localhost:3000`. Keep them bound to loopback unless the installation's
access controls permit broader exposure.

## Check targets, rules and dashboard

With the forwards active, run:

```bash
make observability-status
```

Success requires the monitoring release, reachable ready Prometheus, declared
OrbitJob scrape pools with healthy targets, evaluating OrbitJob alert rules and
the specific dashboard ConfigMap. The dashboard must have sidecar label
`grafana_dashboard=1` and JSON UID `orbitjob-run-ledger`.

The script warns if the Prometheus PVC is absent. An unavailable optional
Grafana forward also produces a warning; inspect it before claiming a complete
monitoring setup. If Grafana responds with a missing dashboard, HTTP error,
invalid JSON or a different UID, the check fails. An unrelated dashboard does
not satisfy this check.

If Prometheus has a configured scrape pool with no target, compare namespace
selectors, Pod labels and port selection. If no OrbitJob pools or rules exist,
check the monitor/rule selectors and CRD discovery. A healthy Prometheus process
alone does not establish application monitoring.

## Component endpoints

Operational endpoints are internal installation interfaces. The default public
Ingress forwards Console/API paths and excludes health and metrics paths.
NodePort or LoadBalancer exposure includes the whole Admin listener.

| Component | Listener | Health/readiness | Metrics |
|---|---|---|---|
| Admin API | 8080 | `/healthz`; no database-dependent `/readyz` | `/metrics` |
| Scheduler | 6060 | `/healthz`; `/readyz` checks its database | `/metrics` |
| Operator | 8082 by default | `/healthz`, `/readyz` | `/metrics` |
| Log archive | 8083, separate from mTLS archive port 8443 | `/healthz`, `/readyz` | `/metrics` |
| Admission | 8084, separate from webhook port 8443 | `/healthz`, `/readyz` | No application scrape defined by the supplied monitors |
| Fluent Bit collector | 2020 | `/api/v1/metrics` is used for readiness | Fluent Bit HTTP metrics interface |

The supplied PodMonitors cover Admin API, Scheduler and Operator. They do not
configure archive or collector scraping. Add installation-appropriate monitors
before relying on archive/collector metrics or alerts. `kubectl top` additionally
requires a working Kubernetes Metrics API; application Prometheus scraping does
not provide that API.

## Read execution and storage signals

Use low-level health with execution facts:

| Signal | Interpretation and next check |
|---|---|
| `orbitjob_operator_reconcile_errors_total` | Inspect affected resource reconciliation logs and original CR identity |
| `orbitjob_operator_job_creation_failures_total` | Inspect admission, RBAC, quota and Job create/adopt responses |
| Operator singleton Lease | Missing/stale renewal prevents singleton schedule/retention work; inspect Operator and API connectivity |
| `orbitjob_execution_isolated_runs` | Ledger snapshot of isolated execution phases; inspect authorized run attempts and evidence |
| `orbitjob_execution_isolation_collection_success` | Zero means the inventory could not be collected completely |
| `orbitjob_execution_isolation_observed_timestamp_seconds` | Check freshness before interpreting isolation counts |
| `orbitjob_archive_storage_bytes` / `orbitjob_archive_budget_bytes` | Allocated archive bytes versus configured allocation ceiling; scrape archive first |
| `orbitjob_http_requests_total` and `orbitjob_ratelimit_hits_total` | Request errors and admission pressure; compare route, status and tenant scope |

Isolation gauges are snapshots. Use `max` across replicas for the same
installation and phase, rather than summing duplicate inventories. A stale or
failed collection retains prior counts; it cannot establish zero unresolved
obligations. The snapshot covers the Operator's configured tenant mappings,
including suspended tenants in that mapping.

The supplied kube-state-metrics `jobrun_phase` is a StateSet: only series with
value `1` identify the current listed phase. Presence of a phase-labelled series
alone does not indicate a run is in that phase. Its current phase list omits
`ExecutionUnknown`; use the dedicated isolation inventory and authorized run
evidence for uncertainty. CR metrics and database ledger evidence can also
lag one another, so inspect identity and timestamps before concluding they
contradict.

Archive allocation metrics do not measure collector directory quotas or database
metadata capacity. Inspect all three separately. ReadLogs and DeleteLogs are
independent capabilities; a monitoring role should not gain cleanup authority
merely to inspect storage.

## Alert response

The supplied rules include scrape failures, Admin API availability, Operator
reconcile/creation errors, stale leadership, Pending runs, cancellation delay,
isolation backlog/staleness, exporter failures, node pressure and API error
rate. Review thresholds against the workload and avoid treating example rules
as a production service objective.

For an isolation or cancellation alert, retain the original Job and Pod evidence.
Do not clear finalizers or retry an uncertain attempt to remove an alert.
For storage pressure, preserve offsets/chunks and archive identity while
following [execution logs](../execution-logs.md) and
[troubleshooting](troubleshooting.md). Database audit/history cleanup requires
its own authorized maintenance procedure.

## Remove local monitoring

Stop only the port-forward processes started for this task. For a dedicated
local monitoring installation:

```bash
make monitoring-down
```

This removes the separate release and the `monitoring` namespace. The helper
must not be used when other applications share that namespace. Review PVC/PV
retention and preserve needed metrics before removal. It does not remove the
separately deployed PostgreSQL exporter; remove that owned manifest separately
when no longer needed. Removing monitoring does not uninstall OrbitJob.
