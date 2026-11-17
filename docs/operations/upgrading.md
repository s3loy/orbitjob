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

Review how maintenance writes will pass the candidate's admission policy.
`admission.nativeWriters` defaults to `[]`; a principal that previously used
`kubectl` may be unable to restore a workload CR after the upgrade. Use an
authorized checked-definition API where it supports the resource, or review
and configure the actual native-writer identity before the outage. Do not
broadly allow `kubernetes-admin` as a native writer to make restoration pass.
Legacy v0 execution-profile compatibility does not establish that the checked
v1 definition API can replace every historical resource.

## Stop submissions and drain

Stop and block manual trigger, Function invoke, webhook and other submission
clients, including native workload CR submitters. Choose the procedure for the
installed version. v0.2.1 has no `execution-controls` API, `ManageExecution`
permission or source-control pause columns.

### Installations with execution controls

Use this procedure only when the installed version provides
`GET /api/v1/<kind>/<id>/execution-controls`. For every affected Job, Workflow,
Function and Check:

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
Save each successful PATCH response as the maintenance write receipt.
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

### Upgrades from v0.2.1

Keep the old Operator available while accepted work drains. Complete the
following steps before stopping all old writers:

1. Save each ScheduledJob and WorkflowJob's original `spec`, UID,
   `resourceVersion`, generation and `spec.suspend` value. Verify the UID and
   set only `spec.suspend: true` with the saved Kubernetes `resourceVersion`
   as a write precondition. Preserve an already suspended CR. Record the
   acknowledged object and confirm its `status.observedGeneration` and actual
   projected definition before treating the suspension as applied.
2. Save Function identities and status. v0.2.1 has read and invoke interfaces
   for Functions, but no pause or resume write API. Keep callers blocked;
   do not change Function status directly in PostgreSQL.
3. Read each Check with `GET /api/v1/checks/<id>` and pause only an active
   definition with `POST /api/v1/checks/<id>/pause` and `{"version":N}`, where
   `N >= 1` is its current definition version. This requires `check:Get` and
   `check:Pause`. Save tenant, ID, `created_at`, original status and version,
   and the successful response's new version. Leave an already paused Check
   paused. A 409 or timed-out write requires review; equal status after a
   timeout does not establish an acknowledged maintenance write.
4. Inventory all accepted receipts against the run ledger, JobRun CRs and
   physical Kubernetes Jobs and Pods. Page through `GET /api/v1/instances`,
   then read each run's `GET /api/v1/instances/<run_id>` and
   `GET /api/v1/instances/<run_id>/attempts`. These reads need `instance:List`
   and `instance:Get`; `POST /api/v1/instances/<run_id>/cancel` needs
   `instance:Cancel`. Function-specific run history contains only terminal
   runs, so an empty history does not prove a drain. Use filters supported by
   v0.2.1; do not send the newer `ExecutionUnknown` phase to its instance API.
5. Check Workflow runs separately with paginated
   `GET /api/v1/workflows/<id>/runs` and
   `GET /api/v1/workflows/<id>/runs/<run_id>`, including each run's steps.
   These reads need `workflow:List` and `workflow:Get`. The v0.2.1 cancel
   endpoint, `POST /api/v1/workflows/<id>/runs/<run_id>/cancel`, needs
   `workflow:Trigger`. Keep the old Operator until existing Workflow runs
   reach confirmed terminal results or cancellation completes: its walker
   advances existing runs without checking the task Job's `spec.suspend`.

If clients that submit through the Admin API cannot be isolated separately,
save its specification and replica count and stop its entry point after the
preceding API work. Block native CR submitters independently; if that is not
possible, stop the upgrade.
Keep the Operator running for the drain. Once the Admin API is stopped,
repeat and maintain the complete run and Workflow inventory through read-only
database, CR and physical workload inspection; do not depend on its HTTP
interfaces for subsequent observation.

The old ScheduledJob scheduler returns on suspension before `repairOpenRuns`;
the old paused Check loop may also leave accepted but unpublished work
unrepaired. If any accepted but unpublished or otherwise unknown work remains,
stop the upgrade and preserve the receipts, ledger, CRs and physical evidence.
Quiet intervals and suspension alone do not establish a drain. Only after the
complete inventory is drained should all old writers stop, followed by the
final recheck in the next section.

A JobRun CR can temporarily retain an active phase after v0.2.1's ledger and
bound Attempt become terminal. Keep the old Operator available while checking
its normal status reconciliation against the ledger and physical workloads.
Do not mark a CR terminal manually to satisfy the drain check.

v0.2.1 exposes Workflow `schedule` and `suspend` fields but has no Workflow cron
scheduler. The candidate can schedule these Workflows. Keep their CRs suspended
through candidate verification; enabling a previously ineffective schedule
requires explicit review and is a behavior change.

### Retained execution evidence

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

For installations with collectors and an archive, let collectors flush while
the archive is available. Inspect offsets, queued chunks and archive state;
readiness does not establish an empty queue. Stop the collector with a recorded
DaemonSet scheduling hold, then stop the archive. Preserve each node's
collector directory as a unit. v0.2.1 has neither component: record their
absence and back up the installation's actual stores without a flush step for
nonexistent services. Review storage and capacity for any archive and
collectors introduced by the candidate. Keep PostgreSQL available for the
coordinated backup. Follow [backup and recovery](backup-and-recovery.md) and
verify the backup before schema changes.

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
it applies the new manifests. Keep source-control pauses, legacy CR suspension,
Check definition pauses and blocked submission clients in place during this
startup. If manual component-by-component startup is required,
prepare and review a manifest transformation that holds the relevant workloads
before using Helm; the chart has no common replica-hold setting.

On any hook failure, keep submissions paused. Inspect the complete migration
ledger before retrying: earlier migrations can already be committed. Preserve
the original Helm ConfigMap byte representation. Never bypass a checksum
failure by editing the ledger or historical SQL.

### Establish candidate execution controls

For retained Jobs and Workflows, rediscover each current active revision ID
through the candidate's authorized definition interface; Functions and Checks
use their retained definition IDs. Read
`GET /api/v1/<kind>/<id>/execution-controls` with the corresponding ID. Save tenant,
`source_mode`, `source_uid`, `version`, both pause values, `actor` and
`updated_at` as actually returned, including absent audit fields. Migration
`0022` does not backfill controls: an unset row reads as version `0` with both
pause values `false`. These values do not record ownership of a legacy CR or
Check pause.

If this maintenance adds a candidate control hold, PATCH with the actual
current version and both intended pause values using `ManageExecution`.
Preserve the acknowledged response separately from the legacy maintenance
record. Continue to hold legacy CRs, Check definitions and clients while
candidate checks run. Restore a candidate control only against this
maintenance's own acknowledged write, under the rules below.

A Workflow with `executionPolicyVersion: 1` requires each referenced Job's
active revision to have resolved execution policy version 1 settings and an
immutable image digest. Retained execution policy version 0 Jobs do not meet
this requirement automatically. Review and publish compliant Jobs through the
[Job authoring flow](../tasks/manage-jobs.md) before referencing them from
these Workflows.

The API can accept a trigger for a projected Workflow even when a referenced
Job is not ready. Its WorkflowRun can remain `Pending` with
`workflow_reference_not_ready` before a run ledger or steps exist. Preserve the
accepted WorkflowRun receipt and inspect its conditions and referenced Job
versions; trigger acceptance alone does not establish parent-run admission.

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

Restore source-control pauses only when the source UID, control version and
both pause values still match the last acknowledged maintenance write. Apply
restoration with the verified CAS version. A changed identity/version,
concurrent edit or ambiguous response requires review. Restore an originally
paused source to its original pause state. Do not use a legacy CR or Check
pause record as a candidate control write receipt.

Restore a legacy CR separately through the reviewed checked-definition API or
native writer. Verify the maintenance record, current UID, `resourceVersion`
and relevant `spec` against the acknowledged suspension; account for reviewed
candidate projection changes and stop on concurrent or unexplained edits.
Restore only the original `spec.suspend` value with the current verified
Kubernetes `resourceVersion` precondition. Preserve original suspensions.
For Workflow schedules that v0.2.1 did not execute, obtain the explicit schedule
review before unsuspending them.

Resume a Check definition separately and only if this maintenance changed it
from active to paused. Confirm tenant, ID, creation time, status and exact
acknowledged definition version, then use
`POST /api/v1/checks/<id>/resume` with that version and `check:Resume`.
A cursor timestamp or old audit actor does not prove ownership of a pause.

Record actual candidate, environment, checks and remaining limits in the
installation's maintenance records. Upgrade evidence applies to the recorded
versions and configuration. Production throughput and high availability require
separate qualification.

## Recover from a failed upgrade

Keep submissions paused and retain failed hooks, application logs and data.
Prefer a compatible corrective version when it preserves the recorded schema
and execution facts. Restoring old Deployment specifications is safe only when
the old binaries still honor the new database, resource-address and execution
contracts.

If data restoration is required, stop all writers and restore the coordinated
backup of the database and any archive or collector state present at the backup
boundary. Validate it in isolation first where possible. Do not pair an old
offset database with newer collector chunks, remove
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
