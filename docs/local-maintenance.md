# Local installation maintenance

Prepare a retained `orbitjob-dev` installation for a source upgrade, verified
execution networking and physically bounded collector storage. Preserve the
cluster, CNI, namespace mapping, resource identities, credentials and migration
input channel. This procedure requires a maintenance window and administrator
access to the Docker host, kind node, Kubernetes installation and database.

Use [upgrading](operations/upgrading.md) for the common submission, drain,
writer-cutover and resume procedure, and
[backup and recovery](operations/backup-and-recovery.md) for coordinated data
protection. This page adds the local node/storage work; it is not a production
multi-node storage or availability design.

## Historical inspection and current prerequisites

The maintenance record dated **2026-11-15** described one Kubernetes v1.37.0
kind node, `kindest/kindnetd:v20260820-69b56db7`, PostgreSQL 17, a 5-GiB database
claim and a 10-GiB archive claim. It recorded collector state at
`/var/lib/orbitjob-collectors/orbitjob-system/orbitjob` and insufficient host free
space for the 40-GiB prerequisite. These are dated observations, not a current
inventory or completed maintenance acceptance. Reinspect them before any outage.

The current local preflight requires at least 4 vCPU, 8 GiB Docker memory,
40 GiB free in the Docker filesystem and, for Docker Desktop, 40 GiB free on
the host backing filesystem. It also requires kubectl to match the cluster's
major/minor and the active database claim to request at least 5 GiB:

```bash
bash scripts/preflight-local.sh
bash scripts/preflight-local.sh --cluster
```

Resolve failures before scheduling downtime. Do not infer host capacity from
the larger guest filesystem. Keep a checksum-verified matching kubectl on PATH
if another bundled client is older. An environment passing preflight still
needs the actual drain, storage and execution checks below.

Before the window, complete source/coverage/PostgreSQL/Helm checks, build and
import all seven candidate images and record their digests. Save current Helm
values/manifests, component specifications and replica counts, CRDs, workload
CRs, source controls, Secrets, PVC/PV identities and the original migration
ConfigMap in private storage. Include manually changed image identities and
collector node selectors. Keep build/download work outside the outage.

The previous local plan reserved 90 minutes: 15 for pause/drain, 20 for backup,
25 for network/filesystem work, 20 for migration/verification and 10 for resume
or a recovery decision. This was an operator planning budget, not measured
downtime. Set the actual window and recovery decision points from the current
installation and tested restoration time. Unreadable backups or filesystems can
require longer recovery.

## Pause, drain and stop writers

Stop submission clients and follow the [drain procedure](operations/upgrading.md#stop-submissions-and-drain).
Preserve each source's tenant, UID, CAS version and original schedule/execution
pause values. Pause schedules first while already enabled execution drains.
An existing execution pause remains in place. If it prevents a Workflow from
draining, stop the window for that source and obtain its owner's decision.

For older images that do not enforce source schedule pause on Checks, also
pause active Check definitions with their independent definition versions and
`check:Pause` permission. Preserve originally paused Checks. Account for late
accepted occurrences and accepted-but-unpublished runs; elapsed polling time
does not prove an old Check batch finished.

After accepted work settles, set execution pause and check again for late work.
Save acknowledged control versions. A 409, changed identity or ambiguous write
requires review rather than overwriting another administrator's state.

Stop Admin API, Scheduler and Operator and verify old Pods terminated. Let the
collector flush to the archive, inspect its offsets/chunks, then apply a recorded
collector scheduling hold and wait for collector Pods to stop. Stop the archive
last. Keep PostgreSQL available for backup. Repeat ledger, original-occurrence,
physical-execution and evidence-finalizer inspection after old writers stop.
Any late nonterminal run, missing CR or execution uncertainty fails the drain.

Take and verify the [coordinated backup](operations/backup-and-recovery.md).
Include the actual mounted database claim, archive data and stopped collector
state as one recovery boundary. Do not copy an obsolete smaller database claim
in place of the mounted one. Readiness does not establish an empty collector
queue or a restorable archive.

## Verify the existing network provider

Test the installed CNI before considering a provider change. A version string
or a `NetworkPolicy` object does not establish traffic enforcement. The existing
probe creates a uniquely named namespace, three non-root Pods and the chart's
rendered policy, then checks baseline reachability, DNS, denied ingress/egress,
a permitted CIDR/port and a denied second port:

```bash
python3 scripts/verify-execution-network.py \
  --kubeconfig /absolute/path/to/maintenance.kubeconfig \
  --context kind-orbitjob-dev \
  --kubectl /absolute/path/to/matching-kubectl \
  > /absolute/private/maintenance/network-verification.json
```

Run from the repository root with Python 3 and Helm available. The node must
be able to obtain its probe image; use `--probe-image` to select a reviewed
available image with the required BusyBox commands. Require `status: PASS`,
all checks and `cleanup: complete`. Preserve actual probe image IDs and policy
hashes. An API/exec error or unavailable target cannot prove a denied connection.

Test intended application destinations in real managed namespaces as well.
Configure each namespace's required destination CIDRs and ports in the
installation values. DNS permission grants no business access. Existing
policy-v0 compatibility workloads may not carry the restricted-v1 selector.
Only after the checks set `executionNetwork.enforcementVerified: true`.
Reverify after CNI, node, policy or destination changes. Preserve the existing
CNI/cluster on failure; installing a second policy engine is a separate change.

## Bound the collector filesystem

For this single-node local profile, a dedicated 256-MiB ext4 image can bound the
collector state directory. The chart's `hostPath` and `quotaBytes` values do not
create that bound. The usable mounted capacity must accommodate the 128-MiB
queue plus at least 64 MiB and the preserved state. Filesystem metadata means a
256-MiB image has less than 256 MiB usable capacity; measure it.

Before starting, verify that the kind node supports loop mounts, ext4 tooling,
`findmnt`, `df`, `cp`, `sync` and systemd. Ensure the image's parent path is on
the node's persistent filesystem. Do not place the image inside its own mount
point. Keep a complete stopped backup of the original directory.

The node administrator performs these steps while the collector is stopped:

1. Create a new, uniquely named image file outside the collector directory and
   format it once as ext4. Refuse to overwrite an existing image.
2. Mount it at a temporary staging directory. Check the filesystem identity,
   usable capacity and free space with `findmnt` and `df`.
3. Copy the original stopped collector directory with ownership and permissions
   preserved. Include `tail.db`, its related files and every queued chunk.
   Compare a complete file manifest/checksums, then `sync`.
4. Unmount staging. Keep the original directory as a separate rollback copy and
   mount the same image at the original collector path. Recheck filesystem
   identity, file ownership and checksums.

For the historical path, a node-local systemd unit has this form:

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

Substitute the actual node paths, install the reviewed unit, run
`systemctl daemon-reload`, enable it and confirm kubelet's required dependency.
The dependency must prevent kubelet from starting if mounting fails; the
collector must not fall back to an unbounded directory after restart. Never run filesystem formatting
as part of a restart unit.

With the collector still stopped, perform a bounded ENOSPC test using only a
new temporary file inside this filesystem. Remove only that test file and
recheck preserved state. Restart the node within the maintenance window, verify
the mount precedes kubelet/collector use and compare the preserved files again.
A mount command succeeding once does not establish restart persistence.

After physical verification, set `logs.collectorPersistence.quotaVerified: true`
and record `quotaBytes` consistent with the physical upper bound. A kind node
recreation still needs restoration of the image and other installation data
from the independent backup. Do not interpret this filesystem recipe as a
qualified production node-replacement procedure.

## Start and verify the candidate

While old publishers remain stopped, apply reviewed CRDs and the matching
ordered migration inputs. The current chain ends at 0028 audit resource identities;
preserve all earlier SQL bytes and the installed Helm input channel.
Account setup requires the matching Admin image and origin settings.

If this window requires checking migrations before any application starts,
prepare and review a manifest transformation that holds application replicas
at zero and preserves the collector hold during Helm application. The current
chart has no common replica-hold value and its default upgrade starts workloads
after pre-upgrade hooks. Do not assume manually scaling to zero survives the
next Helm apply.

After migration verification, start archive, restore the saved collector
selector, then start the compatible Operator, Scheduler, admission and Admin
API at the intended saved configuration. Keep source execution paused during
initial checks. Verify:

- Images, credentials, migration checksums, leadership and component readiness.
- Historical source/run/attempt identity and account/API authorization.
- Collector mount/capacity, mTLS ingestion and retained reads after restarts.
- Complete fresh isolation inventory, no unresolved evidence obligation and
  SLI maintenance without missing-plan or duplicate-count errors.
- One enabled test source through validate/save, projection, trigger, terminal
  ledger result, attempts and logs; check replay, cancellation and destinations.
- Healthy [monitoring targets/rules](operations/monitoring.md), with Metrics API
  availability checked separately if `kubectl top` is required.

Restore source pause values only after comparing UID, version and both values
with the last acknowledged maintenance write. Use the verified CAS version.
Restore Check definitions separately using exact acknowledged definition
versions and `check:Resume`; creation time/identity must still match. An old
audit actor or cursor timestamp does not prove pause ownership. Resume clients
after the actual API/Operator/database/log path succeeds.

## Failure and recovery

Keep submissions paused when identity, storage, migration or execution checks
fail. For a collector mount rollback, stop collection and snapshot its complete
current offsets/chunks. Populate an empty rollback directory from that stopped
snapshot, preserving ownership and permissions, then verify checksums before
unmounting and restoring the original path. Keep the older pre-maintenance
copy separate. Mixing its offsets with newer chunks can lose or duplicate
collection. If the image is unreadable, preserve it and keep collection stopped
for explicit recovery.

Remove a kubelet mount dependency only within the controlled filesystem
rollback and verify the restored path before collection resumes. Restore saved
component specifications/images only if they remain compatible with the schema
and execution facts. Do not restore an older database merely to change binaries.
When data restoration is needed, stop all writers and restore coordinated
PostgreSQL, archive and collector state together.

Keep PVCs, source identity and evidence finalizers intact. A failed local window
does not justify replacing the working cluster or deleting unknown execution.
Full released-version upgrade, installed fault recovery, complete restore and
production throughput/HA require their own scoped acceptance evidence.
