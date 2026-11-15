# Execution logs

The archive retains execution output and lifecycle evidence across attempts.
Command evidence contains the reviewed image/argv, emitted output and observed
exit code/signal. Internal shell commands appear only if the workload prints or
traces them.

## Installation and capacity

Use kind + Helm through the [development deployment guide](local-deployment.md).
The development installation allocates at least **4 vCPU, 8 GiB RAM
and 40 GiB initially free backing storage** to Docker/Kubernetes. That storage
includes a 5-GiB database volume and a separate 10-GiB log archive volume.
These are minimum supported resources; they do not guarantee throughput or concurrency. Tool and
image versions come from the repository manifests. Run
`bash scripts/preflight-local.sh --cluster` to check an existing kind installation.

An existing kind database claim below 5 GiB needs migration. The kind-specific
`bash scripts/upgrade-local-db-volume.sh` stops application writers, copies the
database offline into a new claim and retains the old claim for rollback.
Back up an important installation before storage maintenance. Do not delete
the retained claim until recovery has been checked.

Helm enables a Fluent Bit node collector and one `orbitjob-log-archive`
StatefulSet. The archive service verifies Pod/Job/JobRun UIDs and the pinned
ledger attempt before binding a source. It writes gzip segments, bounded
manifests and lifecycle events to its dedicated PVC; PostgreSQL stores a compact
tenant-scoped identity directory and cleanup requests. The archive service
uses the trusted Runtime DB identity. The Admin API uses its separate Admin
identity and an internal reader mTLS certificate.

Configure archive limits through `charts/orbitjob/values.yaml`; `internal/logarchive` implements the following default limits:

| Budget | Behavior |
|---|---|
| Logical execution | 32 MiB uncompressed stdout/stderr shared by retries, Pods, restarts and streams |
| Window above the execution limit | First 8 MiB and recent 24 MiB, in bounded segments; omitted ranges are visible |
| Workflow | 128 MiB across its child executions; each child also has its own limit |
| Tenant | 2 GiB retained uncompressed output |
| Installation archive | At most 7 GiB charged filesystem allocation inside a 10-GiB PVC, with additional free-space reserve |
| Lifecycle/manifest | 1,024 events / 64 KiB event text; 256-KiB manifest; excess facts get a visible gap |
| Replay metadata | All admitted deterministic source fingerprints remain in the bounded manifest; exhaustion rejects new output with a persistent loss marker |
| Directory | 128-MiB logical identity budget and 256-MiB physical table/index/TOAST admission guard |

Volume admission uses the smaller of configured limits, the actual PVC request
and filesystem headroom. It does not infer unlimited capacity from a large kind
hostPath filesystem or automatically expand volumes. Disk guards cover managed
archive writes; other processes and storage failures can still exhaust a
shared backing device. Monitor the archive/collector metrics and database size.

### Collector state and node capacity

The default collector stores its tail database and filesystem chunks in
`/var/lib/orbitjob-collectors/<namespace>/<release>` on each node. The namespace
and Helm release isolate installations. Replacing a collector Pod on the same
node retains this directory; replacing or losing the node does not preserve it.
The database and archive are separate commits. Replayed records use persisted
source fingerprints, rather than a collector-generated request ID, for deduplication.

The `logs.collectorBuffer` setting limits the Fluent Bit output queue, initially
128 MiB. It does not impose a physical quota on the host directory, its SQLite
database or other node files. Before using the production collector profile,
provision and monitor a directory quota on every execution node. For example,
after provisioning a 256-MiB quota for the default queue, set:

```yaml
logs:
  collectorPersistence:
    mode: hostPath
    profile: production
    hostPathBase: /var/lib/orbitjob-collectors
    quotaVerified: true
    quotaBytes: 268435456
```

`quotaVerified` is an administrator declaration. Helm does not create or
independently measure the node quota. Production rendering rejects an ephemeral
directory, an absent declaration or a declared quota smaller than the configured
queue plus 64 MiB of metadata headroom. The development profile reports the
unverified quota in Helm notes and Pod annotations. An explicit
`logs.collectorPersistence.mode=ephemeral` uses an `emptyDir` size limit for disposable
development and loses offsets and unexported chunks when that Pod is replaced.
Do not remove a retained node directory as part of a normal upgrade.

Each source file has its own collector tag and chunk route. This separates a
permanently invalid source batch from unrelated files. Queue overflow can still
discard the oldest queued chunk. An oversized physical line is skipped so that
later lines can continue; inspect collector warnings and dropped-record metrics.
Node loss, queue eviction, source rotation before collection and local database
or filesystem failure can lose output. These losses do not change the task's
business outcome. Source coverage remains unverified when its missing extent
cannot be established.

### Ingestion and replay

The internal endpoint checks a complete JSON-lines batch and its structural
limits before appending output: at most 8 MiB, 20,000 records and 256 source
files. Source verification completes before any output group is appended.
Malformed JSON or a structural limit returns 400 or 413 without committing an
output prefix. Verification can register source metadata; that is distinct from
accepting container output.

A persistence failure can occur after earlier groups have committed. A 503
therefore requires replay of the original batch, with the original source
records. Per-record fingerprints deduplicate both complete-batch replay and
regrouped replay. A successful response reports `received` and `rejected`;
success does not mean that every record was retained. Permanent invalid records
and capacity, expiry or quarantined-archive refusals are counted separately.
Known archive identities receive a durable loss marker where storage permits;
unattributable source failures remain visible in counters without inventing an
archive identity.

The pinned Fluent Bit 4.2.3 HTTP output does not retry ordinary 4xx responses,
including 400 and 413; it retries 408, 429, 5xx and transport failures. This makes
prevalidation necessary: a permanent batch rejection must not leave an accepted
output prefix. The filesystem retry queue remains bounded during transient
outages. These status rules are defined by the
[pinned output implementation](https://github.com/fluent/fluent-bit/blob/v4.2.3/plugins/out_http/http.c#L283-L316).

## Retention and missing evidence

Terminal archives are protected for **15 days from the later of completion and
collection seal**. Late accepted output extends protection by another 15 days
and does not replace sealed output. A sealed archive at its limit refuses late
output with a gap marker. Automatic cleanup never evicts another execution's
protected logs. A running task that exceeds its output limit can rotate only its own window.

After protection expires, the archive is reclaimed and a bounded `expired`
summary remains for another 15 days. Directory maintenance continues after
JobRun removal. Count-based execution pruning respects protected archives.
Normal terminal Job cleanup waits for a sealed archive and retains its ledger
summary; independently deleting Pods does not delete the durable archive.

Archive retention, execution-ledger retention and Function invocation history
are separate lifecycles. The archive's protection period is not a TTL for
Function history. Function log routes also require an available parent
definition to resolve its current group; retaining archive files alone does not
keep the original Function URL accessible after that definition is deleted.

If no budget remains, the task continues and collection becomes partial or
unavailable. Read responses and downloads disclose known omitted ranges, or explicitly
state that the range is unknown. A reserved control block records known loss
even if the main manifest budget is exhausted. Collection is observed evidence;
node loss, collector buffer loss and never-collected historical output cannot
be reconstructed. A sealed state is not a proof of gap-free source coverage.

Deleting retained logs requests audited deletion and requires an independent
`instance:DeleteLogs` or `function:DeleteLogs` grant. It can remove a terminal
archive inside its protection period, but does not delete the execution summary.
Cleanup is asynchronous; the receipt means requested, not already removed.

## API and permissions

Reading and downloading require `instance:ReadLogs`; deletion requires
`instance:DeleteLogs`. Each permission is independent of ordinary execution
read access:

```text
GET    /api/v1/instances/{run_id}/logs/archive
GET    /api/v1/instances/{run_id}/logs/archive/download
DELETE /api/v1/instances/{run_id}/logs/archive
```

Instance archive routes require credentials without a resource-group
restriction. Group-bound credentials use the Function archive routes, authorized
against the Function's persisted resource group.

Function history uses `/api/v1/functions/{id}/runs/{history_uuid}/logs/archive`
and the same `/download` suffix, with actual-parent group authorization and
`function:ReadLogs` / `function:DeleteLogs`. Explicit Deny and tenant isolation
apply to every operation. Browser callers cannot supply Pod locators.

Read queries support `after`, `limit` (at most 1,000), positive `attempt` and
`search` (at most 256 characters). Follow reconnects with the last cursor.
Search scans retained evidence only. Responses contain collection state, gaps,
output/retained/limit bytes and protection expiry. Download includes omission
markers and an explicit end marker; an interrupted export must not be treated
as complete. Automation keys belong in Authorization headers, never URLs. Account sessions
use the separate cookie and CSRF contract in [accounts](accounts.md).

The old `/attempts/{attempt}/logs` endpoint remains a bounded snapshot from
retained Kubernetes resources. It is useful for legacy executions, but has no
durable retention guarantee. Its availability is distinct from the archive.

Reads carry the caller's cancellation context and a five-second work budget,
including queueing. At most four reads run per archive process and two per
tenant; each archive coordinates its own segment snapshot with append and
deletion. Decompression and searching do not hold the installation metadata
lock. A canceled or exhausted read returns unavailable rather than a complete
empty page. Local filesystem operations already blocked in the operating system
cannot be forcibly interrupted by a Go context.

A missing or corrupt segment produces an explicit partial page with its omitted
sequence range; remaining verified segments can still be read. Downloads include
gaps and collection-state changes discovered on later pages. A malformed,
oversized, missing or checksum-invalid manifest isolates its hash-named archive
directory, preserves its files and charged allocation, and refuses fresh
registration over that evidence. Healthy archives remain available. Symlinks,
an unsafe directory layout or a manifest identity that disagrees with its
directory still prevent the service from opening the untrusted archive root.
Newly written manifests contain a content checksum; legacy manifests without
one retain structural checks and gzip integrity checks, without a retroactive
checksum guarantee. A checksum detects corruption and does not authenticate
changes made by an administrator with access to the volume.

Inspect `orbitjob_archive_corruption_total` and preserve an affected directory
for recovery. Do not remove its files or fabricate an empty manifest to restore
a healthy status. Validate a coordinated backup in an isolated copy before
replacing evidence. Quarantined manifest directories are excluded from automatic
unreferenced-file cleanup and continue to consume archive capacity.

From a source checkout with the Go version declared in `go.mod`, inspect a
stopped archive or an isolated volume copy without rewriting evidence:

```bash
go run ./internal/logarchive/inspectcmd --directory /path/to/archive-copy --timeout 1m
```

The command emits JSON containing archive hashes and integrity findings, without
container output. The underlying command exits 0 for no detected findings, 1
for archive findings, and 2 for an unsafe root or an incomplete inspection;
`go run` additionally reports the command's nonzero exit status. It refuses an
active writer lock and does not create a missing lock file. Inspection is bounded
to 10,000 archive directories and the supplied time budget. A backup copy must
itself be consistent; an interrupted inspection does not establish integrity of
the remaining archives.

## Operations and verification

Inspect `orbitjob-log-archive` and `orbitjob-log-collector` in the installation
namespace, their readiness probes and `/metrics` endpoints. Archive counters use
bounded labels and omit output text and credentials. Collector node-log access
is a trusted installation capability; workload and browser credentials do not
gain node, Secret or exec access. Output is inert text and remains sensitive.

At the database physical metadata guard, existing archives can still update
state and accept cleanup. Reclaim eligible rows, then have the database owner
reclaim relation bloat during a maintenance window (`VACUUM FULL
execution_log_archives` followed by `ANALYZE execution_log_archives`) before
expecting new registration to resume. The guard refuses growth that would exceed the allocation.

Back up the PostgreSQL database, archive volume and installation configuration
as one recoverable installation. Verify the restored identity directory and
archive files together in an isolated environment before relying on the backup.
Restoring only the database does not restore archived output; restoring only the
archive does not restore the identities and permissions needed to read it.

An application rollback must preserve the archive files, additive database
schema and retention limits. Disabling new collection does not authorize deletion
of protected archives or unbounded buffering. Historical snapshot output has no
retroactive completeness guarantee, and unavailable output cannot be recreated.

See [verification](verification.md) for filesystem-capacity, retention,
PostgreSQL authorization and restart-recovery results.
