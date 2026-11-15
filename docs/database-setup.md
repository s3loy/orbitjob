# Database configuration

OrbitJob stores database configuration per installation. Each process and replica
uses the connection assigned to its database role.

Migration 0014 adds the tenant-scoped execution-log identity directory and
audited cleanup requests. The archive service reuses `orbitjob_runtime`;
`orbitjob_admin` can read the directory and update only its cleanup request
column. RLS remains ENABLE-only under the existing owner boundary. Output and
lifecycle bodies stay on the separate log PVC. Metadata admission guards and
bloat recovery are documented in [execution logs](execution-logs.md).

## What the bootstrap account needs

One account is used once, to create the roles the workloads run as. It needs:

- `LOGIN` and `CREATEROLE`
- ownership of the target database

The PostgreSQL image used by the kind deployment does not require a `SUPERUSER` bootstrap
account: its database owner can install the trusted `pgcrypto` extension and
perform the schema grants in `owner-init`. For an external server, also check
its extension policy, connectivity and schema ownership before setup.

If the roles already exist and were created by a different account, the
bootstrap account also needs `ADMIN OPTION` on them — `CREATEROLE` alone only
covers roles the account created. `scripts/kind-db.sh` grants this when
switching an existing local installation over.

`owner-init` creates and manages:

```text
orbitjob_table_owner   orbitjob_migrator   orbitjob_admin   orbitjob_runtime
orbitjob_operator      orbitjob_owner      orbitjob_reader
```

Only `orbitjob_migrator`, `orbitjob_admin` and `orbitjob_runtime` have `LOGIN`;
`orbitjob_table_owner`, `orbitjob_operator`, `orbitjob_owner` and
`orbitjob_reader` are `NOLOGIN` identities. Business processes never receive
the bootstrap DSN. The migration hooks use owner/migrator credentials;
application processes use admin/runtime credentials. Admin and runtime tenant
queries enforce row level security; the migration identity can assume the
table-owner role for schema maintenance.

## Bundled PostgreSQL

`cmd/configure setup` has two modes. Bundled mode generates installation state
without contacting a server; it does not provision PostgreSQL, roles or a
schema. Supply the existing bundled server's owner password as `PG_PASSWORD`
and select its endpoint with `--bundled-endpoint` if needed. See
`go run ./cmd/configure setup -h` for flag defaults.

For Bash, with the owner password read without echo:

```bash
read -r -s -p 'Bundled PostgreSQL owner password: ' PG_PASSWORD
printf '\n'
export PG_PASSWORD
go run ./cmd/configure setup --mode bundled
```

`configure setup` writes:

- `.runtime/database.env` — the derived DSNs for the bootstrap owner, migrator,
  admin and runtime roles, plus the generated passwords
- `.runtime/database.json` — installation state, so a repeated setup reuses the
  passwords

Both paths are gitignored and both files are `0600`.

With an existing state file, `configure setup` refreshes the runtime env from
that state and does not change passwords or reconnect to validate the server.
Its message describes the local configuration:

```text
Existing database configuration is valid. Runtime configuration refreshed.
```

`--reconfigure` replaces bundled installation state and generates new role
credentials. It does not rotate live database passwords by itself. External
mode rejects this flag. Keep state, database role passwords and deployed Secrets
consistent through an explicit maintenance procedure; deleting state is not a
credential-rotation workflow.

## External PostgreSQL

Put the bootstrap DSN in a file only you can read:

```bash
install -m 0600 /dev/null /tmp/orbitjob-bootstrap-dsn
printf '%s\n' 'postgres://dbadmin:<password>@db.example.com:5432/orbitjob?sslmode=verify-full' \
  > /tmp/orbitjob-bootstrap-dsn
```

Then:

```bash
go run ./cmd/configure setup \
  --mode external \
  --database-dsn-file /tmp/orbitjob-bootstrap-dsn \
  --state .runtime/database.json \
  --runtime-env .runtime/database.env
```

For a local kind cluster, `make kind-db` does all of this and creates the
Secret, using an `orbitjob_bootstrap` account that owns the database rather than
the image's superuser.

External setup creates/hardens roles, sets their passwords and checks login
connectivity. It does not apply application migrations or run application
bootstrap. The chart's owner-init, migration and bootstrap hooks perform those
steps during installation. Reusing a local state file also reuses its endpoint;
do not assume a different `--database-dsn-file` changes an existing installation.

### Create the installation Secret

For a first installation, create the release namespace in the intended cluster,
then install the generated credentials before running Helm. This example uses
the default `database.keys` mapping from chart values. `RELEASE_NAMESPACE` and
`DATABASE_SECRET_NAME` are variables for this example; match the Helm namespace
and `database.secretName`, and adapt the mapping if those keys were customized.
It requires Python, OpenSSL and kubectl in addition to the generated env file.

```bash
kubectl config current-context
kubectl create namespace "${RELEASE_NAMESPACE:-orbitjob-system}" \
  --dry-run=client -o yaml | kubectl apply -f -
```

The DSNs must be reachable from Pods. A temporary host port-forward or a TLS
file available only on the setup machine is not a usable in-cluster endpoint.

```bash
(
  set -euo pipefail
  umask 077
  set -a
  . .runtime/database.env
  set +a
  export RELEASE_NAMESPACE="${RELEASE_NAMESPACE:-orbitjob-system}"
  export DATABASE_SECRET_NAME="${DATABASE_SECRET_NAME:-orbitjob-database}"
  INSTALL_BOOTSTRAP_KEY="otj_$(openssl rand -hex 24)"
  export INSTALL_BOOTSTRAP_KEY
  manifest="$(mktemp "${TMPDIR:-/tmp}/orbitjob-database-secret.XXXXXX")"
  trap 'rm -f "$manifest"' EXIT
  python3 >"$manifest" <<'PY'
import json
import os
import sys

fields = {
    "bootstrap-owner-dsn": "BOOTSTRAP_OWNER_DSN",
    "migrator-dsn": "MIGRATOR_DSN",
    "admin-dsn": "ADMIN_DSN",
    "runtime-dsn": "RUNTIME_DSN",
    "migrator-password": "MIGRATOR_PASSWORD",
    "admin-password": "ADMIN_PASSWORD",
    "runtime-password": "RUNTIME_PASSWORD",
    "bootstrap-api-key": "INSTALL_BOOTSTRAP_KEY",
}
values = {key: os.environ[variable] for key, variable in fields.items()}
if not all(values.values()):
    raise SystemExit("a required database Secret value is empty")
json.dump({
    "apiVersion": "v1",
    "kind": "Secret",
    "metadata": {
        "name": os.environ["DATABASE_SECRET_NAME"],
        "namespace": os.environ["RELEASE_NAMESPACE"],
    },
    "type": "Opaque",
    "stringData": values,
}, sys.stdout)
PY
  kubectl create -f "$manifest"
)
```

The temporary file is private and removed on exit; credentials are not passed
as command arguments. `kubectl create` refuses an existing Secret. This is not
a rotation or recovery procedure: do not generate a replacement bootstrap key
for an already initialized installation. The chart hooks still perform schema
migration and application bootstrap after this first-installation prerequisite.

## Kubernetes and replicas

One installation uses one database Secret. Components read the keys for their
own role; replicas inherit the same configuration. The Secret name and complete
key mapping are defined in [`charts/orbitjob/values.yaml`](../charts/orbitjob/values.yaml)
under `database`. It contains role DSNs and role passwords because the
owner-init hook applies passwords before migration/runtime processes use their
DSNs.

Shared credentials do not establish high availability. The shipped chart sets
one replica per component, and the scheduler's optional etcd election requires
separate configuration. Do not infer that scaling its Deployment is sufficient
to coordinate singleton loops.

Changing the Secret does not restart the pods that read it, and the chart cannot
hash a Secret it does not create. `make kind-db` rolls matching local workloads
when the Secret contents change; it is a development helper, not a general
rotation protocol. A manual rotation must coordinate live role passwords,
Secret values and workload restarts; a restart alone does not change PostgreSQL
credentials.

## Generated process environment

The generated `.runtime/database.env` describes the environment interface used
by process configuration. For a controlled launcher, its shell format can be
loaded as follows:

```bash
set -a
. .runtime/database.env
set +a
```

Loading this file is not a standalone installation procedure. The current
Admin API and operator construct clients with `rest.InClusterConfig`; they
require an in-cluster ServiceAccount and do not fall back to a host kubeconfig.
Building a native binary does not remove that requirement. The scheduler and
client tools may run on a host when their own dependencies are configured;
the supported installation path for the complete system remains Kubernetes.

`admin-api` reads `ADMIN_DSN`. The runtime processes — scheduler and operator —
share `RUNTIME_DSN`; the operator accepts `OPERATOR_DSN` first and falls back
to `RUNTIME_DSN`.

> **Renamed variables.** `DATABASE_DSN` and `SCHEDULER_DSN` are accepted only
> as legacy fallbacks for `ADMIN_DSN` and `RUNTIME_DSN`; new deployments must
> not use them. The old `DISPATCHER_DSN` and `WORKER_DSN` were removed together
> with the dispatcher and worker processes.

## Audit permissions and outcome recovery

<a id="consolidated-local-development-lineage"></a>

### Migration lineage and compatibility

The migration chain includes management and Console schema changes through
0011, storage integrity in 0012, execution observation in 0013, the execution
log directory in 0014 and subsequent execution repairs. Migration 0027 adds
personal Console accounts. The [migration directory](../db/migrations/) defines
the complete order. Recorded migration names and checksums are immutable.

A development database that already recorded `version 7 = storage_integrity`
belongs to the former backend-only lineage. It cannot upgrade using the
current chain: version 7 resolves to the Console platform-policy
reader, and checksum validation must reject that mismatch. Preserve its backup
and receipts. Recreate only disposable test databases; a database containing
needed data requires a separately reviewed export/restore or migration plan.
Do not edit `schema_migrations`, clear checksums or apply both version-7 files.

Execution observation adds original JobRun ownership, uncertainty metadata and
durable stop evidence to attempts. Unknown and CancelUnknown rows remain open
and retained. Stop old writers before applying 0013/CRDs; start only compatible
writers afterwards. Rollback retains those facts and cannot use binaries that
interpret API absence as permission to retry or release concurrency.

Pod evidence retention stores its proof and release progress in the existing
attempt evidence field. Proof, evidence-gap isolation and cancellation
completion use the parent/run/attempt transaction order. Pending releases must
complete before attempt progression or history pruning. Keep PostgreSQL and a
compatible Operator available while draining retained Pods before uninstall or
downgrade; see [the operational drain procedure](../USAGE.md#74-upgrade-and-uninstall).

Keep an existing installation on its original migration input channel. The
chart's SQL files match `db/migrations/`, but its existing ConfigMap template
uses YAML `|-`, which removes trailing LF bytes. The migrator hashes the bytes
it reads: Helm-mounted SQL and raw filesystem SQL therefore have different
checksums even when their SQL statements are identical. A Helm-initialized
ledger cannot be treated as interchangeable with a raw-directory ledger.
Preserve the installed ConfigMap input and verify its projected checksums; do
not restore LF bytes, edit `schema_migrations` or change the historical template
to bypass a mismatch. This input-channel distinction is a compatibility
constraint, including when the SQL statements themselves are unchanged.

Apply the selected channel's migrations in order with the migration identity.
Migration
`0012_storage_integrity.up.sql` revokes application `UPDATE`, `DELETE` and
`TRUNCATE` privileges on the audit parent and all existing partitions. New
audit partitions must preserve the same permissions. Offline retention uses
the owner maintenance identity, not an application login.

The same migration adds durable terminal-outcome acknowledgement and
per-run SLI deduplication facts. Unacknowledged terminal runs remain available
for recovery. Deduplication facts survive run-history pruning, and explicit
tenant/SLI maintenance must remove those facts before their referenced rows.
Stop old operators before the
migration and start only the new version afterwards. Existing terminal runs
are excluded from recovery at cutover to preserve their statistics; missing
historical projections require offline review. Recovery applies to runs that
become terminal after the migration.

Stop older run publishers before starting the new version: they cannot safely
discover the new full-digest resource addresses.
This includes the Admin API and any other older writer alongside the operator.
Matching historical resources remain readable; no bulk rename is required.
Use the returned resource name rather than reconstructing a shortened suffix.

For an existing installation, plan a maintenance boundary: back up the
database and configuration, stop and verify termination of older writers,
apply reviewed CRD updates, run the new migration hooks, then start only
compatible binaries. The chart's pre-upgrade hooks do not stop old Deployments
for you. Review [verification](verification.md) for covered schema transitions,
installation checks and recovery tests before planning the maintenance window.

Migrations are forward-only. Rollback must retain the additive schema and
deduplication facts. Short-name-only writers require draining/reconciling new
addresses before reuse; an automatic Helm rollback cannot undo these data and
compatibility changes safely. Use a recovery version that preserves both the
resource-address and execution-observation requirements above.

Migration 0012 also enforces same-tenant SLI references for SLOs. Before an
upgrade, use the owner maintenance identity to check existing rows:

```sql
SELECT s.id AS slo_id, s.tenant_id AS slo_tenant, i.tenant_id AS sli_tenant
FROM slos s JOIN slis i ON i.id = s.sli_id
WHERE s.tenant_id <> i.tenant_id;
```

Resolve any returned ownership mismatch before migration. The new constraint
rejects inconsistent history; the migration does not silently reassign or delete it.

## TLS

An external DSN keeps its connection and TLS query parameters. Derivation
removes query credentials and login indirection (`user`, `password`, `passfile`,
`service`, `servicefile`) before applying the intended role credentials:

```text
postgres://dbadmin:<password>@db.example.com:5432/orbitjob?sslmode=verify-full&sslrootcert=/run/secrets/ca.crt
```

The tooling encodes generated credentials in derived DSNs with `net/url`.
A manually supplied bootstrap URI must itself be valid: percent-encode
reserved characters in its username/password before placing it in the input
file.

## Troubleshooting

### Local configuration is missing

The launcher must load the generated environment; process entrypoints do not
read the installation JSON as a substitute. Rerun `configure setup` with the
installation's existing mode/state paths to refresh its runtime env. Supply
the required owner password or external DSN file for initial setup.

### External authentication fails

Check the user, password, TLS parameters and database in the bootstrap DSN. The
tooling prints only a redacted endpoint and never the password.

### Permission denied

```text
ensure database roles: pq: permission denied to alter role (42501)
```

The bootstrap account cannot manage the OrbitJob roles. It needs `CREATEROLE`,
and `ADMIN OPTION` on roles it did not create itself. PostgreSQL lets only a
superuser change the `SUPERUSER`, `CREATEROLE` or `BYPASSRLS` attributes, so a
role that has drifted needs a superuser to repair it — `owner-init` names the
role and the attribute rather than doing it silently.

### Switching databases

A normal setup never overwrites existing installation state. Pointing at a
different endpoint changes the authoritative data source and is not a routine
configuration change: back up and migrate separately.

## Personal account migration

Migration `0027_personal_accounts.up.sql` adds installation state, personal users,
workspace memberships and policy bindings, hashed single-use capabilities,
hashed server sessions and an installation audit stream. Account operations use
a fixed-search-path SECURITY DEFINER function granted only to `orbitjob_admin`;
runtime, operator and PUBLIC do not gain access to account tables or functions.
Tenant business data keeps its existing RLS and explicit policy checks.

Back up the installation, preserve its migration input channel, and apply the
full ordered chain with the matching chart. Deploy an Admin image containing
the account endpoints and `/orbitjob-identity` before creating accounts. The
backend image does not include compiled browser assets; consume setup through
the account HTTP API described in [accounts](accounts.md).
Configure HTTPS or explicit loopback development mode, then follow
[first administrator setup](accounts.md#create-the-first-administrator).
The active Default workspace is a prerequisite. Setup consumes a one-time
operator-issued capability and creates the administrator, explicit Default
owner membership and permanent claim marker atomically.

The migration leaves existing API keys and their bcrypt verification unchanged.
It does not infer user ownership or memberships from those keys. They continue
to serve scripts; they cannot sign in to the new Console. Roll out invitations
for human users separately. Updating source files does not update an existing
Admin deployment or its database.

This migration is additive and has no destructive down procedure. Application
rollback must retain the new schema and audit history and assess whether the
chosen binary supports the installation's account state. An older image does
not provide account login or recovery. Restore a coordinated database backup
when schema rollback is required; editing recorded migrations, clearing the
claim marker or deleting accounts to reopen setup is not a recovery procedure.
Lost administration uses [operator recovery](accounts.md#recover-installation-administration).

## Platform credential policy reader

Migration `0007_platform_key_documents` implements ADR0011's bounded
`orbitjob_platform_key_documents(text)` SECURITY DEFINER helper. It is owned by
`orbitjob_table_owner`, pins `search_path` to `pg_catalog, public`, and grants
EXECUTE only to `orbitjob_admin`; PUBLIC, runtime and operator gain no access.
It reads preset documents bound to one live, unexpired, unrevoked platform key
with NULL tenant ownership, plus its configured preset permission boundary.
A configured non-preset or unavailable boundary fails closed. Tenant-owned
policy documents are never returned, and existing table RLS is unchanged.

The helper does not authenticate an API key. Admin middleware supplies only the
credential ID it has already authenticated; no HTTP target or key ID selector is
forwarded to this reader. Ordinary tenant credentials keep their existing RLS
queries. Platform preset `self` remains unresolved: selecting a target tenant
does not give a platform credential that tenant's self-scoped permissions.
Boundaries still intersect grants and explicit Deny remains authoritative.

Apply the migration before deploying the Admin policy-loader path. Application
rollback retains the additive schema and helper. Removing the helper requires
a reviewed forward migration after all callers have stopped using it; a down
SQL file is not an operational rollback mechanism.

## Function management and Workflow scheduling migrations

Migration `0008_function_management` adds actor/key/payload receipt metadata to
Functions with a tenant-scoped unique index. It grants no new table privilege
and keeps existing Function RLS. Only the named TenantAdminAccess preset gains
`function:Create` and `function:Update`; explicit tenant policies retain their
current actions. Creation receipts and audit records commit with the definition.

Migration `0009_workflow_scheduling` adds nullable planned trigger time, a bounded
rejection reason and original resource UID to the Workflow ledger. Historical
planned times remain NULL. Rejected rows require a reason; planned times require
a Schedule trigger, and bound resource UIDs are unique. Existing table roles/RLS
remain unchanged. Apply migrations before deploying their readers/writers.
Application rollback retains these fields and receipt identities. Any later
removal requires a reviewed forward migration and a data/replay compatibility
plan; do not execute down files against an installation's recorded lineage.

## Workspace directory metadata reader

Migration `0011_workspace_directory` adds the metadata-only
`orbitjob_workspace_directory` SECURITY DEFINER reader under ADR0021. It is owned
by `orbitjob_table_owner`, pins its search_path and grants EXECUTE only to
`orbitjob_admin`; PUBLIC, runtime and operator receive no execution privilege.
The Admin application supplies the authenticated credential's exact authorized
tenant scope, computed with the existing policy semantics. SQL applies that scope
and literal name/slug search before stable pagination. Returned columns contain
only workspace ID, name, slug and status; no keys, policy documents or workloads.

Ordinary tenant table RLS and the legacy tenant list are unchanged. Apply this
migration before deploying the new directory endpoint. Application rollback
retains the helper and existing data. Remove an unused helper only through a
reviewed forward migration after its callers have been retired.
