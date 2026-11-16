# Back up and recover an installation

A recoverable OrbitJob backup includes PostgreSQL, retained output, collector
state, installation credentials and Kubernetes configuration from a coordinated
maintenance boundary. A database dump alone cannot restore queued logs or prove
that an earlier Kubernetes Job stopped.

The chart has no backup controller or one-command restore workflow. Choose and
qualify database and volume backup tools for the actual storage system. The
commands below provide the PostgreSQL and configuration parts; archive and
collector copies must use a storage method that preserves their contents,
ownership and permissions.

## Before you begin

The operator needs authorized database maintenance access, Kubernetes read/write
access and access to every archive and collector volume. Establish a destination
outside the cluster/node storage being protected. Use encryption and access
controls appropriate for Secrets, account data and raw workload output.

Define the required recovery point, restoration window and original-workload
reconciliation plan. The project does not establish an RPO or RTO for this
procedure. A readable archive listing or a successful snapshot request is not
a tested restoration.

Choose a new backup directory under an existing directory on separate storage.
The create command refuses to reuse an existing backup directory:

```bash
umask 077
export RELEASE_NAMESPACE=orbitjob-system
export RELEASE_NAME=orbitjob
export BACKUP_DIR=/absolute/path/on/separate-storage/orbitjob-backup
mkdir "$BACKUP_DIR"
chmod 0700 "$BACKUP_DIR"
kubectl config current-context
```

## Establish a consistent boundary

Follow [stop submissions and drain](upgrading.md#stop-submissions-and-drain).
Save original source controls and component specifications, stop incompatible
writers and verify their termination. Keep PostgreSQL running for its dump.

Let collectors flush while the archive is available. Capture each collector's
current `tail.db`, any related SQLite files and every queued chunk after the
collector has stopped. Stop archive writes before taking its volume copy.
Readiness does not prove queue emptiness. Do not combine offsets and chunks
from different points in time.

Record nonterminal runs, uncertain attempts, original source/JobRun/Job UIDs,
resource versions, retained Pods and evidence finalizers. An unresolved run
requires an explicit recovery plan; backing it up does not make its physical
execution known. Never remove evidence finalizers to obtain an apparently
quiet backup boundary.

## Save Kubernetes configuration and credentials

Save Helm state and actual component configurations. The actual Kubernetes
objects can contain changes made outside Helm:

```bash
helm get values "$RELEASE_NAME" -n "$RELEASE_NAMESPACE" -a > "$BACKUP_DIR/helm-values.yaml"
helm get manifest "$RELEASE_NAME" -n "$RELEASE_NAMESPACE" > "$BACKUP_DIR/helm-manifest.yaml"
kubectl get deployment,statefulset,daemonset,service,configmap,secret \
  -n "$RELEASE_NAMESPACE" -o yaml > "$BACKUP_DIR/control-plane.yaml"
kubectl get pvc -n "$RELEASE_NAMESPACE" -o yaml > "$BACKUP_DIR/archive-claims.yaml"
kubectl get crd -o yaml > "$BACKUP_DIR/crds-inventory.yaml"
```

Also save the original migration ConfigMap, relevant PV definitions, release
metadata, namespaces, RBAC, admission webhook, source-control inventory and all
OrbitJob workload CRs in each mapped execution namespace. Record actual image
IDs/digests and saved replica counts. Copy the private generated database state
and runtime environment from the administrator's protected storage.

The configuration export contains plaintext or base64-encoded secrets; base64
is not encryption. Keep reports and ordinary source control separate. Do not
blindly apply this export to another cluster: it contains server-assigned
fields, stored credentials and resource identities that require review.

## Back up PostgreSQL

Use PostgreSQL client tools appropriate for the server version. Select an
existing protected libpq service/password-file configuration; do not put a
password or credential-bearing DSN in argv. The maintenance account must be
able to read all required tables and role definitions. Application RLS logins
are not general backup identities.

For an external server with an administrator-provisioned service named
`orbitjob-backup`:

```bash
export PGSERVICE=orbitjob-backup
pg_dump --format=custom --file="$BACKUP_DIR/database.dump"
pg_dumpall --roles-only > "$BACKUP_DIR/roles.sql"
psql -X -v ON_ERROR_STOP=1 -At \
  -c 'SELECT version, name, checksum, applied_at FROM schema_migrations ORDER BY version' \
  > "$BACKUP_DIR/schema-migrations.tsv"
pg_restore --list "$BACKUP_DIR/database.dump" > "$BACKUP_DIR/database-list.txt"
```

The libpq service selects the intended database and TLS settings; validate its
target before these commands. Role dumps can contain password hashes and
cluster-wide roles. Restrict them and review which roles belong to this
installation before restoration. If a managed service denies role export,
obtain an administrator-approved role reconstruction plan and matching
credentials; do not call the backup complete without it.

For the repository's local PostgreSQL Deployment, the container environment
already supplies its development credentials:

```bash
export DB_NAMESPACE=orbitjob
kubectl exec -n "$DB_NAMESPACE" deployment/orbitjob-postgres -- \
  sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" exec pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' \
  > "$BACKUP_DIR/database.dump"
kubectl exec -n "$DB_NAMESPACE" deployment/orbitjob-postgres -- \
  sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" exec pg_dumpall -U "$POSTGRES_USER" --roles-only' \
  > "$BACKUP_DIR/roles.sql"
pg_restore --list "$BACKUP_DIR/database.dump" > "$BACKUP_DIR/database-list.txt"
```

These commands are specific to the local manifest; do not substitute its
superuser credential for an external installation's maintenance policy.

## Back up archive and collector storage

Identify the actual mounted PVC/PV and node paths from the running installation.
The default archive claim is `data-orbitjob-log-archive-0`; a local database may
have moved to a larger claim. Back up the mounted volume, not a similarly named
obsolete claim.

For the archive, use a storage snapshot with the required consistency guarantees
or copy the stopped filesystem. Preserve the entire archive directory and its
metadata. PostgreSQL's execution-log directory and the archive contents must
refer to the same retained identities.

For each eligible collector node, copy its entire stopped state directory,
including offsets, journal files and queued chunks. The default path is
`/var/lib/orbitjob-collectors/<release-namespace>/<release-name>` on the node.
A queue's configured size does not cover the SQLite offset database and
filesystem overhead. Include the physical quota/mount configuration and any
persistent bounded-filesystem image used by the local maintenance procedure.

A kind node's filesystem disappears when the cluster/node container is deleted.
A second copy inside that same node is not an independent backup. Move the
verified backup to the separate destination before cluster removal.

## Verify and retain the backup

Record file checksums after all copies finish. For a Unix system with
`shasum -a 256`, run from the backup directory and select the files actually
created:

```bash
(
  cd "$BACKUP_DIR"
  shasum -a 256 database.dump roles.sql helm-values.yaml helm-manifest.yaml \
    control-plane.yaml archive-claims.yaml > checksums.txt
  shasum -a 256 -c checksums.txt
)
```

Include archive and collector copy manifests/checksums in the same record.
Verify ownership, permissions, file counts and copy completion. A missing
volume, unreadable dump, incomplete queue or inconsistent migration input
stops the maintenance operation. Keep the installation paused until the gap
is resolved or a reviewed recovery decision is made.

Perform an isolated restore before relying on the backup. Retain the original
installation and backup until account access, history, permissions and retained
output have been checked in the restored environment.

## Restore in isolation

Use a new isolated database/server and storage destination with compatible
PostgreSQL and application versions. Block ordinary clients and scheduling.
Do not connect a restored Operator to the original execution namespaces while
both installations could publish or adopt work.

With a protected libpq service selecting the empty recovery database, restore
reviewed role definitions first, then the custom-format dump:

```bash
export PGSERVICE=orbitjob-recovery
psql -X -v ON_ERROR_STOP=1 -f "$BACKUP_DIR/roles-reviewed.sql"
pg_restore --exit-on-error --dbname="service=$PGSERVICE" "$BACKUP_DIR/database.dump"
```

`roles-reviewed.sql` is the database administrator's installation-specific
subset of the saved role definitions. `service=$PGSERVICE` selects the recovery
service's connection settings, including its database and TLS parameters.
Provision any service-required database ownership and extension
permissions before restoration. A partial restore requires cleanup of the
isolated destination or an administrator-reviewed retry; do not continue from
an unexplained error.

Restore the archive and each collector state from the same backup boundary.
Keep offsets and chunks together. Restore Secrets and configuration under
review, preserving the original migration input representation and required
credential identities. Validate all recorded checksums before starting readers.

Kubernetes object recreation assigns new UIDs. A YAML reapply does not restore
an original source, JobRun or Job identity. Preserve original cluster state
where its recovery mechanism supports it; otherwise reconcile historical and
possibly still-running workloads explicitly. Do not rewrite historical identity
to make restored objects appear to be the originals, and do not let an isolated
restore execute replacement attempts on uncertain work.

## Validate and switch

Before enabling publishers or users, verify:

- The complete migration ledger and mounted migration checksums match the saved
  installation channel; no new baseline or down migration was introduced.
- Accounts, sessions/capabilities, memberships, policies, keys and audit history
  have the expected state. Use supported operator recovery for lost access.
- Historical definitions, run/attempt facts, uncertain execution and retained
  output remain readable under the correct authorization scope.
- Archive capacity, collector offsets/chunks, certificates and quotas are
  consistent. Missing output remains visible as missing evidence.
- Exactly one intended control plane can publish work to the execution
  namespaces, and original physical execution has been reconciled.

Use a controlled test source after those checks, then restore only the source
pause states owned by the maintenance operation with their CAS versions. Switch
clients and endpoints under the installation's rollout plan. Keep the old
installation stopped and preserved until recovery acceptance is complete.

A failed verification keeps publishers and submissions paused. Preserve the
restored destination and diagnostic evidence; choose a compatible corrective
version or repeat a clean isolated restoration. Restoring an older database to
change a binary alone can discard newer execution or account facts.
