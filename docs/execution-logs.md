# Execution logs

The execution archive retains bounded lifecycle events and container
stdout/stderr across Attempts independently of Pod retention. The Console's
**Execution logs** panel provides attempt/stage filters, search, follow, copy
and download. [Read logs](tasks/read-logs.md) covers the workload-user procedure.
This page covers installation capacity, collection and recovery.

Command evidence records the reviewed image/argv, emitted output and observed
exit code or signal. Shell commands executed inside the container appear only
when the workload prints or traces them. Treat retained output as sensitive.
Log availability is separate from the Run's confirmed business outcome.

## Installation and capacity

The chart enables a Fluent Bit node collector and one
`orbitjob-log-archive` StatefulSet. The archive verifies JobRun, Job and Pod
UIDs and the pinned ledger Attempt before binding a source. It writes gzip
segments, bounded manifests and lifecycle events to a dedicated PVC.
PostgreSQL stores the tenant-scoped identity directory and cleanup requests.

The archive uses the trusted Runtime DB login. Admin uses its separate Admin
login for directory reads and audited cleanup requests, plus internal reader
mTLS. Collector and reader identities have separate internal capabilities.
The collector image is pinned to Fluent Bit 4.2.3 and a digest in chart values.

Use [local deployment](local-deployment.md) for the source-development kind
installation and its tool and resource prerequisites. Database PVC migration
requires a separate installation-specific plan; archive capacity settings do
not provide that procedure. Follow [backup and recovery](operations/backup-and-recovery.md)
before changing retained database, archive or collector storage.
The default archive PVC is 10 GiB. Review `logs` values before installing;
filesystem capacity does not increase automatically with a larger host disk.

| Budget | Default and behavior |
|---|---|
| Logical execution output | 32 MiB uncompressed, shared by Attempts, Pods, restarts and streams |
| Over-limit execution window | First 8 MiB and recent 24 MiB; omitted ranges are visible |
| Workflow output | 128 MiB across child executions; each child also has its own cap |
| Tenant output | 2 GiB retained uncompressed output |
| Installation allocation | 7 GiB charged filesystem allocation within a declared 10-GiB PVC, plus free-space reserve |
| Lifecycle events | 1,024 events and 64 KiB of event text; excess evidence records a gap |
| Manifest | 256 KiB; bounded source/replay metadata can refuse further admission |
| Directory admission | 128-MiB logical identity budget and 256-MiB physical table/index/TOAST guard |

Volume admission uses configured limits, the actual PVC request and filesystem
headroom. Managed writes reserve space, but another process or a backing-device
failure can still exhaust storage. Monitor archive usage, collector health and
database size. A capacity refusal does not stop the task; its collected output
can become partial or unavailable.

Filesystems that report a finite inode total must also retain more than 64 free
inodes. A zero inode total means that this count is unavailable; admission still
checks available bytes, the configured allocation and the free-space reserve.
A failed filesystem-stat call refuses admission. Actual write failures remain
visible even after a successful capacity check.

## Collector state and node capacity

The default collector keeps its tail database and filesystem queue under
`/var/lib/orbitjob-collectors/<namespace>/<release>` on each node. Namespace and
release isolate installation paths. Replacing a Pod on the same node retains
that directory; node replacement or loss does not preserve it. Normal upgrades
must retain the node directory.

`logs.collectorBuffer` bounds the Fluent Bit output queue, defaulting to
128 MiB. It does not impose a physical quota on the directory, SQLite database
or other node files. Provision and monitor a directory quota on every execution
node before selecting the production collector profile. After provisioning a
256-MiB quota for the default queue, declare it:

```yaml
logs:
  collectorPersistence:
    mode: hostPath
    profile: production
    hostPathBase: /var/lib/orbitjob-collectors
    quotaVerified: true
    quotaBytes: 268435456
```

`quotaVerified` is an administrator declaration. Helm neither creates nor
independently measures the quota. Production rendering requires hostPath,
that declaration and a quota at least the queue plus 64 MiB of metadata
headroom. Development rendering reports an unverified quota in notes and
annotations. Explicit `mode: ephemeral` uses bounded `emptyDir` storage for
disposable development and loses offsets/unexported chunks on Pod replacement.

Each source file has a separate tag/chunk route, limiting the effect of a
permanently invalid source batch on unrelated files. Queue overflow can discard
old chunks. Oversized physical lines are skipped so later lines can proceed;
inspect collector warnings and dropped-record metrics. Node loss, queue eviction,
rotation before collection and local storage failure can lose output. When its
extent cannot be established, coverage remains explicitly unverified.

## Ingestion and replay

The archive and PostgreSQL commits are separate. Ingestion first validates an
entire JSON-lines batch and its structural bounds: 8 MiB, 20,000 records and
256 source files. Source verification completes before any output group is
appended. Malformed JSON or structural overflow returns 400/413 without an
accepted output prefix. Verification can register source metadata independently
of output acceptance.

A persistence failure can occur after earlier output groups commit. A 503
therefore requires replay of the original source records. Persisted per-record
source fingerprints deduplicate complete or regrouped replay; collector-generated
request IDs are not the replay authority. Replay metadata itself remains
bounded; exhaustion refuses new output and retains a loss marker where possible.

A successful ingestion response reports `received` and `rejected`. It does not
promise that every submitted record was retained. Permanent invalid sources
and capacity/expiry/quarantine refusals are counted separately. Known identities
can receive a durable loss marker; an unattributable source failure is counted
without inventing an execution identity. The collector queue remains bounded
during transient outages. Inspect both archive and collector evidence when
investigating collection loss.

## Retention and missing evidence

Terminal archives are protected for 15 days from the later of completion and
collection seal. Late accepted output extends protection by another 15 days
without replacing sealed output. A sealed archive at its cap refuses late output
and records a gap. Automatic cleanup does not evict another execution's protected
output. A running task can rotate its own output window at its cap.

After protection expires, output is reclaimed and an `expired` summary remains
for another 15 days. Directory maintenance continues after JobRun removal.
Count-based execution pruning respects protected archives. Normal terminal Job
cleanup waits for a completed archive with protection metadata (sealed or
partial), a five-minute update delay and settled termination evidence. It
preserves the ledger summary. Independently
deleting a Pod does not delete an already retained archive.

Archive retention, execution history and Function invocation history have
separate lifecycles. Log protection is not a Function-history TTL. Function log
routes also need their available parent definition to resolve current group
authorization; retained files alone do not preserve that URL after the parent
is unavailable.

A sealed archive can still have collection gaps. The archive records the range
of an omission when it can determine the bounds and reports an unknown range
otherwise. When the manifest is full, the reserved control block records loss
and retains confirmed completion and seal information for retention and cleanup.
The Run's confirmed success, failure or cancellation remains authoritative when
output is missing. The archive can return only output it collected.

**Delete retained logs** requires independent `instance:DeleteLogs` or
`function:DeleteLogs`. It commits an audited cleanup request and can remove a
terminal archive during protection. The receipt confirms a request, not immediate
deletion. Cleanup leaves the execution summary.

## API and permissions

Instance archive routes require credentials without a resource-group restriction:

```text
GET    /api/v1/instances/{run_id}/logs/archive
GET    /api/v1/instances/{run_id}/logs/archive/download
DELETE /api/v1/instances/{run_id}/logs/archive
```

Read/download require `instance:ReadLogs`; delete requires
`instance:DeleteLogs`. These grants are independent of ordinary run reads.
Function history uses
`/api/v1/functions/{id}/runs/{history_uuid}/logs/archive`, with `/download` for
export. It authorizes the persisted Function parent/group with
`function:ReadLogs` or `function:DeleteLogs`. Tenant isolation and explicit
Deny apply throughout. Browser callers provide no Pod or namespace locator.

Queries support `after`, `limit` (maximum 1,000), a positive `attempt` and
`search` (maximum 256 characters). The default page limit is 500. Follow resumes
from the last cursor and searches only retained evidence. Responses include
collection state, gaps, output/retained/limit bytes and protection expiry.
Downloads include omissions, collection-state changes and an explicit end marker.
A partial transfer is not a complete export even when initial headers succeeded.

The separate `/attempts/{attempt}/logs` route reads a bounded snapshot from
retained Kubernetes resources, with a five-second deadline, at most 500 tail
lines and 65,536 bytes. `attempt` means the ledger Attempt number. It verifies
immutable ownership and has no durable retention promise; Kubernetes garbage
collection can make it unavailable.

Archive reads carry caller cancellation and a five-second work budget including
queueing. Each archive process allows four concurrent reads and two per tenant.
Its segment snapshot coordinates with append/deletion, while decompression and
search avoid holding installation metadata locks. Cancellation/exhaustion
returns unavailable instead of a complete empty page. Go cancellation cannot
forcibly interrupt a filesystem call already blocked in the operating system.

## Integrity and recovery

A missing or corrupt segment returns a partial page with its omitted sequence
range; remaining verified segments can still be read. A missing, malformed,
oversized or checksum-invalid manifest quarantines its hash-named directory,
preserves files and charged allocation, and refuses fresh registration over
that evidence. Healthy archives remain available. A symlink, unsafe root layout
or directory/manifest identity mismatch prevents opening an untrusted root.

New manifests contain a content checksum. Legacy manifests without one retain
structural and gzip-integrity checks, without a retroactive checksum guarantee.
A checksum detects corruption; it does not authenticate edits by an administrator
who controls the volume. Quarantined directories remain outside automatic
unreferenced-file cleanup and continue to consume capacity.

Inspect `orbitjob_archive_corruption_total` and preserve the affected directory.
Do not delete its evidence or manufacture an empty manifest to clear the error.
Validate a coordinated backup in an isolated environment before replacement.
With Go 1.27.4 or later from the matching source checkout, inspect a stopped
archive or a consistent isolated volume copy:

```sh
go run ./internal/logarchive/inspectcmd --directory /path/to/archive-copy --timeout 1m
```

The command prints JSON containing archive hashes and integrity findings,
without container output. The underlying program exits 0 for no findings,
1 for findings, and 2 for unsafe roots or incomplete inspection. `go run`
additionally reports a nonzero child exit. Inspection refuses an active writer
lock, creates no missing lock file, and stops at 10,000 archive directories or
the supplied time budget. An interrupted inspection does not establish integrity
for the remaining archives. The copy itself must be consistent.

## Operate and back up the archive

Inspect the archive StatefulSet, collector DaemonSet, readiness probes and
`/metrics` endpoints in the installation namespace. Metrics use bounded labels
without output text or credentials. Collector node-log access is a trusted
installation capability; workload/browser credentials receive no node, Secret
or exec permission. Output is served as inert text and remains sensitive.

At the PostgreSQL physical metadata guard, existing archives can update state
and accept cleanup while new registration is refused. Reclaim eligible rows,
then have the database owner reclaim relation bloat in a maintenance window
with `VACUUM FULL execution_log_archives` and `ANALYZE execution_log_archives`
before expecting admission to resume. `VACUUM FULL` requires an exclusive lock
and appropriate temporary storage; schedule it as database maintenance.

Back up PostgreSQL, the archive volume and installation configuration as one
recoverable installation. Verify restored directory identity, permissions and
files together in an isolated environment. A database-only restore does not
restore output; an archive-only restore does not restore authorized lookup.

Application rollback must preserve archive files, additive schema and retention
limits. Stopping collection does not authorize deletion of protected archives
or unbounded queueing. Unavailable output has no reconstruction guarantee.
[Verification](verification.md) records the scope of filesystem, retention,
PostgreSQL authorization and restart-recovery evidence. Those results do not
qualify another installation or establish gap-free collection.
