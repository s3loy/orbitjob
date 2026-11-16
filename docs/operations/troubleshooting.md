# Troubleshoot an installation

Start with the affected operation, its accepted identity and the component that
owns the next step. Preserve the original error, request/run identity, timestamps
and relevant events before retrying. Keep credentials and raw execution output
in private evidence storage.

## Collect an installation snapshot

Use the intended context and actual release namespace:

```bash
export RELEASE_NAMESPACE=orbitjob-system
export RELEASE_NAME=orbitjob
kubectl config current-context
helm status "$RELEASE_NAME" -n "$RELEASE_NAMESPACE"
kubectl get pods,jobs,pvc -n "$RELEASE_NAMESPACE"
kubectl get events -n "$RELEASE_NAMESPACE" --sort-by=.metadata.creationTimestamp
```

Inspect a specific failing Pod with `kubectl describe pod` and `kubectl logs`.
For a restarted container, `kubectl logs --previous` can retain its earlier
failure. Match the namespace, source UID, revision, occurrence and original
Job/Pod UID when investigating execution. Resource names alone can identify an
unrelated replacement.

Do not delete a database, clear migration checksums, remove finalizers or
retrigger uncertain work as a diagnostic step. An ambiguous write response
requires an identity/receipt lookup before another mutation.

## A hook Pod reports CreateContainerConfigError

Read Pod events for the missing Secret or key. Owner-init, migrate and bootstrap
consume the installation Secret before ordinary application use. By default it
is `orbitjob-database` in the release namespace, with the eight keys documented
in [database configuration](../database-setup.md#create-the-installation-secret).

For a new local kind installation, `make kind-db` prepares that Secret and the
PostgreSQL server. For external PostgreSQL, complete database configuration
before retrying Helm. A Secret in the database namespace does not satisfy a Pod
in the release namespace. Restore existing credentials for an initialized
installation rather than generating a new bootstrap key.

## A migration hook fails

Inspect the failed Job/Pod and its logs before the hook TTL removes them:

```bash
kubectl get jobs -n "$RELEASE_NAMESPACE"
kubectl logs -n "$RELEASE_NAMESPACE" job/YOUR_FAILED_MIGRATION_JOB
```

Use maintenance access to inspect all migration rows. Each successful migration
commits separately; a later failure can leave the schema partially advanced.
Check database reachability, role setup, object ownership and the selected SQL
input channel before retrying.

For `checksum mismatch`, compare the original inputs and installed ConfigMap.
Helm's `|-` removes trailing line feeds, so raw-directory SQL is not an
interchangeable migration channel. For the old version-7 development lineage,
stop and use the [compatibility procedure](../database-setup.md#existing-development-lineages).
Do not edit the migration ledger or SQL to suppress the error.

An owner-init role-drift error names the role or attribute to repair. A database
administrator must restore the required role attributes and management grants.
An application connection is not a substitute for that authority.

## Database authentication or TLS fails

Check the selected database, URI encoding, network endpoint, bootstrap account
and certificate trust. An endpoint reachable on the configuration machine may
be unavailable inside Pods. A host-only port-forward or `sslrootcert` path is
not automatically usable by cluster components.

External setup requires a private DSN file and initial connectivity for each
generated role. Reusing state refreshes the local environment without checking
the server. A success message on that repeated path does not prove the DSNs
still work. Inspect the actual Secret key mapping and deployment environment
references without printing credentials into reports.

If a Secret was rotated, coordinate PostgreSQL passwords, local state, all
DSN/password keys and consumer restarts. A Secret update alone does not restart
the Pods, and restarting Pods does not rotate database credentials.

## The Operator exits or definitions have no active revision

Read its startup/reconciliation logs:

```bash
kubectl logs -n "$RELEASE_NAMESPACE" deployment/orbitjob-operator --tail=100
```

An empty or malformed `OPERATOR_NAMESPACE_TENANTS` prevents startup. The current
chart rejects a missing mapping when the Operator is enabled; inspect old
manifests or manual Deployment edits if the live value differs. A mapping must
name an existing intended tenant and the namespace containing the source CR.

For a nonprojected definition, inspect its CR conditions, admission errors,
revision status and the original source UID. Check webhook readiness/certificate
validity and native-writer policy. Ordinary clients should use the checked
definition API. An unready fail-closed webhook is not a reason to disable
admission and create unchecked sources.

## Pods cannot pull or start their image

Read Pod events to distinguish pull credentials, missing platform manifests,
missing image references, image startup errors and admission rejection.
With `global.imagePullPolicy: Never`, each node must already contain the exact
rendered name and tag. For the ordinary contributor cluster:

```bash
make kind-load TAG=dev
```

Use the tag selected by the installation values. A local Docker login does not
configure node image-pull credentials. After rebuilding `:dev`, restart every
affected image consumer; an unchanged Pod template can keep old code.

## The API rejects authentication or authorization

HTTP 401 indicates unavailable/invalid authentication; inspect the credential
used by that client. The delivered local bootstrap integration key is Secret
`bootstrap-api-key`, key `api-key`; `source <(make kind-env)` loads it for scripts.
That API key is not a Console password.

For Console login, use a personal account and the configured HTTPS origin, or
explicitly enabled HTTP loopback mode. Check that the browser's exact origin
matches `adminapi.console.publicOrigin`, including scheme and port. Forwarded
headers are not trusted to select that origin. Follow
[account recovery](../accounts.md#recover-installation-administration) for lost
installation administration.

HTTP 403 requires checking the exact action, tenant/group boundary, explicit
Deny and permission boundary. ReadLogs, DeleteLogs, execution controls and
cancellation are independent grants. Resource detail can return 404 outside an
authorized scope; installation administration does not grant workload access.
Do not broaden permissions merely to make one diagnostic request pass.

## A trigger was accepted but the run is not complete

An accepted trigger establishes its submitted identity, not terminal execution.
Inspect the returned run/occurrence, active revision, JobRun, attempt trail,
Kubernetes Job and Pod status. Check Operator leadership, namespace RBAC,
ResourceQuota, Pod Security, image pulls and execution network destinations.

The Admin Service's `/healthz` does not check the database. Scheduler and
Operator readiness include a database ping, but neither proves an accepted Job
has completed. A quiet control plane can still have retained physical execution.

For `ExecutionUnknown` or `CancelUnknown`, inspect original attempt identity,
unknown reason, first unknown time and stored stop evidence. Restore trustworthy
observation of the original objects. Missing Pods, elapsed time or cancellation
request acceptance do not prove termination. Keep concurrency held until the
supported recovery path resolves the evidence. See
[execution recovery](../architecture.md) and [execution logs](../execution-logs.md).

## Cancellation or uninstall is blocked

Keep PostgreSQL and a compatible Operator available until original Jobs are
confirmed stopped and required Pod termination evidence is committed. Inspect
`orbitjob.io/termination-evidence` finalizers and the attempt's pending release
state. A manual finalizer removal discards that recovery boundary.

Stop new submissions and follow [draining](upgrading.md#stop-submissions-and-drain).
Do not delete namespaces or PVCs while retained evidence obligations remain.
An uninstall command returning does not establish a recoverable backup or
physical stop proof.

## Execution logs are missing, partial or at capacity

Check the authorized run/attempt and archive identity first, then collector
queue/offset state, mTLS connectivity, archive readiness and the directory's
reported state. `partial`, `capacity`, `expired` and `deleted` describe different
evidence conditions. Empty output cannot substitute for missing collection.

Inspect archive allocation, actual PVC capacity, metadata storage and each
collector's physical quota separately. Helm's quota declaration does not create
a filesystem quota. Do not delete queued chunks or mix an older `tail.db` with
new chunks. Restore a consistent stopped collector snapshot when recovery is
needed. See [execution logs](../execution-logs.md) for cleanup permissions and
capacity recovery.

## Monitoring is absent or incomplete

Follow [monitoring checks](monitoring.md#check-targets-rules-and-dashboard).
Check monitor namespace/label/port selectors, discovered Prometheus targets,
rule health and the exact run-ledger dashboard UID. The local monitoring helper
does not deploy PostgreSQL exporter or archive/collector monitors.

Check execution-isolation collection success and timestamp before interpreting
its count. The supplied CR StateSet omits `ExecutionUnknown`; it cannot prove
the absence of execution uncertainty. `kubectl top` needs a separate Metrics API.

## A load test is inconclusive

Read the specific run's `result.json` and `report.md`. Confirm its original
configuration, generated corpus, source identity, accepted receipts, tenant
keys, namespaces, fixture boot identity and journal sequence. Rejected/skipped
work, unfinished runs and missing independent fault evidence prevent a complete
verdict. Restarting the fixture loses its in-memory evidence and cannot repair
that run's continuity. Follow [load testing](../loadtest-guide.md) and retain
the original artifacts before starting a new run ID.
