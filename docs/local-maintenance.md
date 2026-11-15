# Local installation maintenance

This procedure prepares the existing `orbitjob-dev` installation for a source
upgrade, verified execution networking and bounded collector storage. It preserves
the cluster, CNI, namespaces, resource UIDs, credentials and migration input
channel. Applying it requires a scheduled maintenance window. Preparation and
disposable-cluster tests do not perform the change on the working installation.
The inventory and preparation findings below reflect the 2026-11-15 inspection.

## Scope and prerequisites

The reviewed installation uses one Kubernetes v1.37.0 kind node,
`kindest/kindnetd:v20260820-69b56db7`, PostgreSQL 17, a 5-GiB database claim and a
10-GiB archive claim. The collector uses
`/var/lib/orbitjob-collectors/orbitjob-system/orbitjob` on the node. Reinspect these
values immediately before maintenance; they are an installation inventory, not
chart defaults for every environment.

Use kubectl 1.37.x for this installation. Keep a separately checksum-verified
binary on the maintenance shell's PATH when Docker's bundled client is older.
The [local preflight](local-deployment.md#prerequisites) requires 4 vCPU, 8 GiB
Docker memory and 40 GiB free on both the Docker filesystem and its host backing
filesystem. Do not infer host capacity from the larger guest filesystem.

Before the outage:

1. Pass the current source's Go, coverage, PostgreSQL and Helm gates. Build and
   import candidate images before stopping services. Record source input hashes,
   image digests and the currently running image identities, including image
   changes made outside Helm. Do not select images solely by a mutable local tag.
2. Save the current Helm values and manifests, Deployment/StatefulSet/DaemonSet
   specifications, CRDs, workload CRs, namespace mapping, source controls and
   PVC/PV identities in a private directory. Preserve actual replica counts and
   collector node selectors. Keep credentials and raw execution output out of
   reports and shell tracing.
3. List required task destinations by namespace, CIDR and port. DNS permission
   does not authorize application traffic. Review policy-v0 compatibility paths
   separately; the restricted-v1 selector does not retroactively protect every
   historical workload.
4. Check every configured execution namespace for nonterminal runs, retained
   evidence finalizers and unknown execution. Confirm the remaining collector
   queue fits the destination filesystem. Abort the window when physical stop
   evidence is missing; do not force-delete evidence finalizers or release slots.
5. Reserve a maintenance window and define who can resume submissions. Image
   builds, downloads and disk-space remediation belong outside the outage. No
   measured downtime guarantee has been established for this procedure.

For the reviewed development installation, reserve a 90-minute window after all
preparation has passed. This is an operator time budget, not measured service
downtime. Start the outage by stopping clients and the Admin API's write traffic;
end it only after the final enabled-source check and controlled client resumption.

| Phase | Reserved time | Stop condition |
|---|---|---|
| Pause submissions and drain | 15 minutes | Any unresolved run, unknown execution or conflicting control write |
| Stop writers and verify coordinated backups | 20 minutes | Incomplete queue inventory, unreadable dump or mismatched checksums |
| Verify networking and provision collector filesystem | 25 minutes | Traffic mismatch, insufficient capacity, failed restart or mount dependency |
| Migrate, start and check the candidate | 20 minutes | Migration, identity, historical data, logs or test-source verification fails |
| Resume or begin rollback | 10 minutes | Source ownership cannot be confirmed or the remaining window cannot accommodate recovery |

Reaching a phase budget is a decision point: keep submissions paused and choose
rollback or extend the window explicitly. Restoration can exceed the reserved
time when a backup or filesystem is damaged. The current workstation's available
host space is below the 40-GiB prerequisite; resolve that before scheduling this
window, without deleting installation data or unrelated services.

## Stop submissions, drain and back up

Stop trigger/invoke clients. For each Job, Workflow, Function and Check, read
`GET /api/v1/<kind>/<id>/execution-controls`, save the response, then pause its
schedule with `PATCH` and the current CAS version. Leave execution enabled while
existing work drains only if it was already enabled; preserve an existing
execution pause. If that pause prevents a DAG from draining, stop preparation for
the affected source and obtain its owner's decision. Each PATCH includes
`version`, `schedule_paused` and the preserved `execution_paused` value. Reads need
the resource's `Get` grant; writes also require its independent `ManageExecution`
grant. Resolve 409 responses with a fresh read and ownership comparison; never
overwrite another administrator's intervening change.

For an older image that does not enforce source schedule pause for Check triggers,
also pause each active Check definition before draining. Save
`GET /api/v1/checks/<id>` and send `POST /api/v1/checks/<id>/pause` with only
`{"version":N}`, using the definition version from that read. Save its tenant,
ID, creation time, original status/version and acknowledged pause result. These
requests require `check:Get` and `check:Pause`, independently of
`ManageExecution`. Use an existing credential covering the target tenant/group;
verify the running image's route and tenant-selection contract first. Preserve
definitions that were already paused and each source's existing execution pause.

Definition pause excludes later timer scans; a batch that already read the
definition can still accept an occurrence. Include late accepted occurrences in
the drain inventory. The old loop has no fixed batch completion deadline, so
waiting for a few intervals does not prove completion. A paused definition may
also stop old Check-specific repair of accepted but unpublished runs. Abort the
drain if an accepted ledger row lacks its JobRun or remains unresolved; do not
temporarily resume the Check to try to repair it.

After all accepted work has settled, set execution pause as well. Confirm that no
new submissions were accepted between the drain inspection and the pause. Save
the resulting control versions for later comparison. Source controls are not
definition deletion and must not set a tombstone.

Stop the Admin API, Scheduler and Operator using their saved replica counts for
recovery. Let the collector flush into the archive, verify its queue and archive
state, then stop the collector with a temporary, recorded node-selector hold.
Stop the archive only after collection has stopped. Keep PostgreSQL running for
the dump. A collector readiness response alone does not establish an empty queue.
Repeat the ledger, original-occurrence and physical-execution inventory after
the old writers have stopped. A late nonterminal run, missing CR or unknown
execution fails the drain even if an earlier inspection was empty.

Create a coordinated backup after these writers have stopped:

- Database custom-format dump, role definitions and migration ledger/checksums.
- Archive PVC contents and collector state, including `tail.db`, its related
  files and queued chunks. Derive archive paths from the actual PV; do not copy
  the obsolete 1-GiB database claim instead of the mounted 5-GiB claim.
- Installation Secrets, current component specifications, source-control
  inventory, CRDs, workload CRs and the original Helm migration ConfigMap.

Use `umask 077`, a mode-0700 backup directory and mode-0600 sensitive files. Dump
passwords through the container's environment or an existing protected credential
channel; do not place them in command arguments or reports. Validate dump listings
and file checksums without printing their contents. Keep the pre-maintenance
backup until the restored installation and retained logs have been checked.

## Verify the existing network provider

The reference kindnetd image includes NetworkPolicy execution. Establish this
with real traffic before changing providers. Installing a second policy engine
is not part of the recommended change, and no CNI or Pod-CIDR replacement is
required by this procedure.

The verifier creates and removes a uniquely named namespace, three non-root
probe Pods and the chart's rendered NetworkPolicy. It checks baseline reachability,
DNS, denied ingress/egress, a permitted CIDR and port, and a denied second port:

```bash
python3 scripts/verify-execution-network.py \
  --kubeconfig /absolute/path/to/maintenance.kubeconfig \
  --context kind-orbitjob-dev \
  --kubectl /absolute/path/to/kubectl \
  > /private/maintenance/network-verification.json
```

Require `status: PASS`, all traffic checks and `cleanup: complete`. An API error,
failed exec or unavailable target must not be treated as proof of a denied
connection. Retain the actual probe image IDs and rendered-policy hashes.

Review and apply explicit workload egress in the installation values. Exercise
the real managed namespace and intended workload destinations as well as the
isolated probe namespace. Only then set
`executionNetwork.enforcementVerified: true`. Reverify after changes to CNI,
nodes, policy values or destination rules. Keep the CNI and current cluster intact
if verification fails.

## Bound the collector filesystem

For this single-node kind profile, use a dedicated 256-MiB ext4 image mounted at
the collector state directory. The filesystem provides a physical upper bound
for that directory; a `hostPath` declaration alone does not. Its usable capacity
must accommodate `collectorBuffer` plus 64 MiB and the preserved state. A
256-MiB image is smaller than 256 MiB after filesystem metadata, so inspect actual
capacity and free space before copying data.

With the collector stopped, keep its original directory as a rollback copy.
Create the image outside that directory, format it once, mount it at a staging
path, and copy the original state with ownership and permissions preserved. Check
content checksums, synchronize writes, unmount staging, then mount the same image
at the original collector path. Never format an existing image during restart.

Persist the mount inside the kind node with a systemd oneshot service. Substitute
the installation's paths in this template:

```ini
[Unit]
Description=Bounded OrbitJob collector filesystem
Before=kubelet.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/bin/mount -t ext4 -o loop,noatime /var/lib/orbitjob-collector-volumes/orbitjob-system-orbitjob.ext4 /var/lib/orbitjob-collectors/orbitjob-system/orbitjob
ExecStop=/usr/bin/umount /var/lib/orbitjob-collectors/orbitjob-system/orbitjob

[Install]
RequiredBy=kubelet.service
```

Enable the unit after `systemctl daemon-reload`. The kubelet dependency must fail
closed if mounting fails; the collector must never fall back to an unbounded
directory after node restart. Before resuming service, verify the filesystem with
`findmnt` and `df`, perform a bounded `ENOSPC` test in a temporary file, delete only
that test file, and confirm the preserved state remains readable. Restart the node
within the maintenance window, verify the mount before kubelet and collector use,
and check the preserved files again.

Keep the image on the node's durable kind filesystem. Recreating or deleting the
kind node still requires restoring this image and the other installation data
from backup. This local profile is not a multi-node production storage design.
After physical verification, set `logs.collectorPersistence.quotaVerified: true`
and keep `quotaBytes` consistent with the provisioned upper bound.

## Start the candidate and verify

While old writers remain stopped, apply the reviewed CRDs separately and deploy
the reviewed values and fixed candidate image identities. Keep application
replicas at zero and preserve the collector hold in that rendered deployment;
default Helm values must not restart writers before the cutover checks. Verify
the migration hook result and the actual migration ledger before starting any
component. The reliability changes add no migration beyond 0026. This candidate
also includes personal-account migration 0027; include that reviewed forward
migration and its identity/setup compatibility checks in the coordinated cutover. Preserve
all earlier SQL bytes and the installation's Helm-mounted migration input channel.

Start the archive, restore the collector's original node selector, then start the
Operator, Scheduler and Admin API at their saved replica counts with the verified
candidate images. Keep source execution paused during initial checks. Preserve
existing Secrets and migration checksums; Helm rollback is not database downgrade.

Verify the following before admitting new work:

1. Component readiness, Lease renewal, expected image identities and unchanged
   credentials, source UIDs and historical execution reads.
2. Collector mount/capacity, mTLS ingestion and retained archive reads after
   archive and collector restart.
3. Current execution-isolation metrics, their observation timestamp and the
   absence of unresolved evidence obligations. Use `max` across replicas of one
   installation; do not sum replica snapshots.
4. SLI maintenance scanning and retention reasons, with no missing-plan or
   duplicate-count errors. Its tenant scope is the Operator mapping, including
   suspended tenants still in that mapping.
5. One deliberately enabled test source: validate/save, trigger, correct final
   ledger phase, attempt identity, retained output and exact permitted/denied
   network destinations. Verify idempotent replay and cancellation separately.

Before restoring a source's saved pause state, compare its current source UID,
version and both pause values with the last acknowledged maintenance write. Any
identity change, intervening version or unexpected state requires administrator
review. A fresh read alone does not authorize overwriting a concurrent change.
After that comparison, restore the saved values with the verified CAS version.
Treat an ambiguous write response as an unresolved operation; do not infer
ownership of a pause merely because a later read has equal values. Resume clients
only after the real API/Operator/database/log path succeeds.

Restore Check definition status separately. Resume only a definition that this
maintenance changed from active to paused, after confirming the same tenant, ID,
creation time, `paused` status and exact acknowledged definition version. Send
`POST /api/v1/checks/<id>/resume` with `{"version":P}` and the independent
`check:Resume` permission. Keep definition versions separate from source-control
versions. Cursor changes can update timestamps without changing definition
version, and the old status audit actor does not identify the maintenance caller.
A changed identity/version or ambiguous response requires review; an originally
paused Check remains paused.

The existing monitoring stack can collect the new metrics after its maintained
manifests are applied. `kubectl top` additionally needs a working Metrics API;
application readiness does not establish resource-metrics availability. Verify
scrape targets and rule health with the documented observability procedure.

## Failure and rollback

Keep submissions paused on any failed identity, storage, execution or evidence
check. For a mount failure, stop the collector and take a complete snapshot of its
current state, including `tail.db`, related files and every queued chunk. Use that
stopped snapshot to populate an empty rollback directory with ownership and
permissions preserved. Verify its checksums before unmounting and putting it at
the original path. Keep the pre-maintenance copy separately; do not combine its
old offsets with current chunks or resume an obsolete `tail.db`. Remove the
kubelet dependency only as part of this controlled rollback, and verify the
restored path before starting the collector. If the current filesystem cannot be
read, preserve the image and keep collection stopped for explicit recovery.

Restore saved component specifications and image digests if the candidate fails.
When no schema migration has occurred, do not restore an older database merely to
change binaries. If data restoration is required, stop all writers and restore the
coordinated database, archive and collector backup together. Verify credentials,
history and retained output before resuming. Do not delete PVCs, rewrite migration
checksums, remove evidence finalizers or replace the working cluster as a shortcut.

Full released-version upgrade qualification, installed fault/recovery suites and
production throughput or HA remain separate from this local maintenance window.
