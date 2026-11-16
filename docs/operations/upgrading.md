# Upgrade OrbitJob

Upgrade a retained installation within a maintenance window. Keep the existing
database, archive, collector state, resource identities and credentials until
the candidate has passed the required checks. An image update, a schema
migration and a recovery from backup have different compatibility requirements.

For the historically inspected kind installation and its storage work, use
[local installation maintenance](../local-maintenance.md) in addition to this
procedure. That record does not establish the current state of another cluster.

## Before you begin

The installation administrator needs Kubernetes write access, database
maintenance access and authority to pause every affected workload. Obtain:

- The currently deployed Helm values, rendered manifests, image digests,
  component replica counts, CRDs, workload CRs and namespace-to-tenant mapping.
- A candidate chart, CRDs, ordered migrations and all seven component images
  from one reviewed version. Confirm registry availability on every node.
- A coordinated [backup and recovery plan](backup-and-recovery.md), including
  PostgreSQL, archive, collectors, Secrets and the migration input channel.
- A defined maintenance window, test workload and decision point for recovery.

Verify the candidate in an isolated environment before changing a retained
installation. Read [database compatibility](../database-setup.md#migration-execution-and-compatibility)
and the applicable [known limitations](../known-issues.md). Neither chart
rendering nor a previous source test establishes an installed upgrade path.

Set and inspect the actual installation context:

```bash
export RELEASE_NAMESPACE=orbitjob-system
export RELEASE_NAME=orbitjob
kubectl config current-context
helm status "$RELEASE_NAME" -n "$RELEASE_NAMESPACE"
```

## Prepare the candidate

Build or obtain all component images before the outage. Use a unique tag and
record digests; a rebuilt mutable tag may leave existing Pods unchanged. Review
candidate values against the installation's current values. Preserve tenant
mappings, storage classes, capacity budgets, TLS origin, Secrets and allowed
network destinations. Do not replace existing values with a fresh example file.

Check the candidate chart and migration packaging without changing the cluster:

```bash
make helm-migrations-check
helm lint ./charts/orbitjob -f /absolute/path/to/candidate-values.yaml
```

Render into a private directory for review. Rendering can include TLS private
keys. Check image identities, namespace RBAC, PVC behavior, admission settings
and network rules. The chart's pre-upgrade hooks run migrations before updating
Deployments; they do not stop old processes. A successful Helm rollback cannot
undo committed migrations or external workload effects.

## Stop submissions and drain

Stop manual trigger, Function invoke, webhook and other submission clients.
For every affected Job, Workflow, Function and Check:

1. Read its `GET /api/v1/<kind>/<id>/execution-controls` response using the
   required resource Get permission. Save its tenant, source identity, version
   and both pause values in a private maintenance record.
2. PATCH the controls with the current `version`, `schedule_paused: true` and
   the original `execution_paused` value. The write needs the independent
   `ManageExecution` permission. Preserve an existing execution pause.
3. Wait for accepted work to finish or cancel it through the supported API.
   Include accepted but unpublished runs, workflow steps and retained Pods in
   the inventory. `ExecutionUnknown` and `CancelUnknown` remain unresolved.
4. After the drain, set `execution_paused: true` using the acknowledged current
   version. Confirm that no submission was accepted during the final boundary.

A 409 requires a fresh read and comparison with the last acknowledged write.
Do not overwrite an intervening administrator change. A timed-out write has an
unknown outcome; observing equal values later does not prove ownership of it.
If an existing execution pause prevents a Workflow from draining, stop the
affected procedure and obtain that source owner's decision.

For an older image whose Check loop does not enforce source schedule pause,
also pause each active Check definition with
`POST /api/v1/checks/<id>/pause` and `{"version":N}` from its definition read.
This needs `check:Get` and `check:Pause`. Preserve the original status, tenant,
ID, creation time, definition version and acknowledged pause response. Keep
Check definition versions separate from source-control versions. An already
read batch can still accept a late occurrence, and an older paused Check loop
may not repair an accepted but unpublished run. A few quiet intervals do not
prove that such work is drained.

Inspect retained evidence Pods in every execution namespace:

```bash
kubectl get pods -n YOUR_EXECUTION_NAMESPACE \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.finalizers}{"\n"}{end}'
```

Any remaining `orbitjob.io/termination-evidence` obligation requires a compatible
Operator and PostgreSQL until proof is committed and the Operator releases its
own finalizer. Do not force-delete retained Pods, clear finalizers or release a
Forbid slot from the absence of an API object.

## Stop old writers and back up

Save actual component specifications and replica counts before stopping them.
Stop the Admin API, Scheduler and Operator; verify their old Pods have
terminated. With their saved defaults, the stop commands are:

```bash
kubectl scale -n "$RELEASE_NAMESPACE" \
  deployment/orbitjob-admin-api deployment/orbitjob-scheduler deployment/orbitjob-operator \
  --replicas=0
```

Do not use these example counts as the recovery record for a customized
installation. Recheck the run ledger and physical workloads after writers stop;
a late or unresolved run fails the drain.

Let collectors flush while the archive is available. Inspect offsets, queued
chunks and archive state; readiness does not establish an empty queue. Stop the
collector with a recorded DaemonSet scheduling hold, then stop the archive.
Preserve each node's collector directory as a unit. Keep PostgreSQL available
for the coordinated backup. Follow [backup and recovery](backup-and-recovery.md)
and verify the backup before schema changes.

## Apply CRDs and upgrade

With old publishers stopped, apply the reviewed candidate CRDs separately:

```bash
kubectl apply -f charts/orbitjob/crds/
```

Helm does not update CRDs in a chart's `crds/` directory during an ordinary
upgrade. Review CRD changes before this step; their compatibility is part of
the maintenance boundary.

Run the candidate upgrade with the same release namespace, Secret and migration
input channel:

```bash
helm upgrade "$RELEASE_NAME" ./charts/orbitjob \
  --namespace "$RELEASE_NAMESPACE" \
  -f /absolute/path/to/candidate-values.yaml \
  --wait --wait-for-jobs --timeout 10m
```

The chart restores its configured replica counts and collector scheduling when
it applies the new manifests. Keep source pauses and submission clients in
place during this startup. If manual component-by-component startup is required,
prepare and review a manifest transformation that holds the relevant workloads
before using Helm; the chart has no common replica-hold setting.

On any hook failure, keep submissions paused. Inspect the complete migration
ledger before retrying: earlier migrations can already be committed. Preserve
the original Helm ConfigMap byte representation. Never bypass a checksum
failure by editing the ledger or historical SQL.

## Verify and resume

Require all of the following before normal submissions resume:

1. The expected component images are running, rollout checks succeed, database
   migration rows match the candidate inputs and credentials remain consistent.
2. Historical definitions, run and attempt identity, account login and retained
   output remain readable through the authorized interfaces.
3. Collector storage and archive budgets match real capacity; collection and
   reads survive their intended restarts. Check internal certificate validity.
4. Execution-isolation inventory is fresh and complete, with no unresolved
   obligation from the drain. Use the observation timestamp and collection
   success alongside counts. Replica snapshots must not be summed.
5. One deliberately enabled test source validates, saves, projects, triggers
   and reaches the expected terminal result with correct identity and logs.
   Check idempotent replay, cancellation and allowed/denied destinations.
6. [Monitoring](monitoring.md) has healthy targets and rules, and any missing
   monitor or evidence source is understood.

Restore source pauses only when the source UID, control version and both pause
values still match the last acknowledged maintenance write. Apply restoration
with the verified CAS version. A changed identity/version, concurrent edit or
ambiguous response requires review. Restore an originally paused source to its
original pause state.

Resume a Check definition separately and only if this maintenance changed it
from active to paused. Confirm tenant, ID, creation time, status and exact
acknowledged definition version, then use
`POST /api/v1/checks/<id>/resume` with that version and `check:Resume`.
A cursor timestamp or old audit actor does not prove ownership of a pause.

Record actual candidate, environment, checks and remaining limits in the
installation's maintenance records. A local upgrade does not qualify production
throughput, released-version compatibility or high availability.

## Recover from a failed upgrade

Keep submissions paused and retain failed hooks, application logs and data.
Prefer a compatible corrective version when it preserves the recorded schema
and execution facts. Restoring old Deployment specifications is safe only when
the old binaries still honor the new database, resource-address and execution
contracts.

If data restoration is required, stop all writers and restore the coordinated
database, archive and collector backup. Validate it in isolation first where
possible. Do not pair an old offset database with newer collector chunks, remove
uncertainty facts or roll accounts back independently of their audit history.
See [backup and recovery](backup-and-recovery.md).

## Uninstall a retained installation

Complete the same submission pause, drain and evidence-finalizer checks before
uninstalling. Back up the installation while compatible components and database
access remain available. Only after those checks:

```bash
helm uninstall "$RELEASE_NAME" --namespace "$RELEASE_NAMESPACE"
```

Inspect remaining PVCs, Secrets, CRDs, workload CRs and collector directories.
Helm uninstall is not a complete data-erasure procedure and does not prove that
all physical workloads stopped. Delete retained data only under the installation
owner's explicit retention decision. Do not remove a shared namespace to clean
up OrbitJob resources.
