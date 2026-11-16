# Database configuration

OrbitJob uses one installation database with separate credentials for schema
maintenance, the Admin API and runtime components. PostgreSQL holds definitions,
execution metadata, authorization, accounts and audit history. Execution output
and lifecycle bodies are stored separately on the log archive PVC.

Complete this procedure before [installing the chart](setup/installation.md).
For the contributor kind environment, `make kind-db` prepares the local server
and Secret; do not apply its fixed development credentials to an external server.

## Database roles and administrator access

Provision an existing database and a bootstrap account with `LOGIN`,
`CREATEROLE`, database ownership and permission to install `pgcrypto` in
`public`. The local PostgreSQL 17 image allows its database owner to install
that trusted extension without superuser access. External services may impose
additional extension or schema restrictions; verify those before setup.

When OrbitJob roles already exist under another creator, the bootstrap account
also needs `ADMIN OPTION` on them. `CREATEROLE` alone does not confer management
of every existing role. A database administrator must repair unexpected
`SUPERUSER`, `BYPASSRLS`, `CREATEROLE`, `INHERIT` or `LOGIN` attributes. The setup
check rejects role drift rather than silently accepting a broader privilege.

Owner initialization creates these identities:

| Role | Login | Purpose |
|---|---|---|
| `orbitjob_table_owner` | No | Owns schema objects; assumed by the migrator |
| `orbitjob_migrator` | Yes | Runs ordered migrations after `SET ROLE orbitjob_table_owner` |
| `orbitjob_admin` | Yes | Admin application storage and permitted identity functions |
| `orbitjob_runtime` | Yes | Operator, Scheduler, admission and archive storage |
| `orbitjob_operator` | No | Existing schema privilege identity; not a connection user |
| `orbitjob_owner` | No | Existing schema identity; not a maintenance login |
| `orbitjob_reader` | No | Existing schema identity; not an issued read-only login |

Managed roles have no superuser, RLS bypass, role-creation or inherited-role
privilege. Only the migrator receives membership in the table-owner role.
Application processes do not receive the bootstrap DSN. Kubernetes hook Jobs
use the bootstrap account for role setup and the migrator for schema changes.
Keep the bootstrap account available for later owner-init hooks, with its
credential restricted to installation administration.

PostgreSQL roles are server-wide and these role names are fixed. Independently
generated installation passwords on two databases of the same server can
change credentials used by the other installation. Use a dedicated server or
an administrator-reviewed shared-role credential arrangement; a different
database name alone does not isolate role setup or rotation.

Tenant queries use transaction-local `app.tenant_id` and row level security
(RLS). A plain SELECT under an application login without tenant context may
return no rows. Do not interpret that empty result as an empty installation.
Cross-tenant maintenance requires an explicitly authorized maintenance identity.

## Choose a database mode

The `bundled` and `external` options configure credentials; neither installs a
PostgreSQL server through Helm.

| Mode | Initial setup behavior | Server provisioning |
|---|---|---|
| `bundled` | Generates installation state without contacting PostgreSQL | Administrator provides the server separately |
| `external` | Creates or validates roles, applies generated passwords and checks each login | Administrator or external service provides the server |

Both modes write `.runtime/database.json` and `.runtime/database.env` by
default. The files contain credentials, are created with mode `0600` and are
ignored by Git. Their directory is created privately. Keep a protected backup
of both files with the installation records.

An existing state file takes precedence over new connection options. Repeated
setup loads that file and refreshes the environment file without reconnecting
to PostgreSQL or rotating credentials. The message `Existing database
configuration is valid` describes the stored configuration, not a live server
health check.

`--reconfigure` replaces bundled state and generates new role passwords; it
does not change live database passwords by itself. External mode rejects it.
Deleting state or rerunning setup against a new endpoint is not a database
migration or a coordinated rotation.

## Bundled PostgreSQL

Provision the server and database before using these generated values. The
default endpoint is `postgres://pg:5432/orbitjob?sslmode=disable` and the default
bootstrap username is `orbitjob`. Supply an endpoint appropriate to the actual
network and use verified TLS settings where required.

In Bash, read the existing server owner password without echo:

```bash
read -r -s -p 'PostgreSQL owner password: ' PG_PASSWORD
printf '\n'
export PG_PASSWORD
go run ./cmd/configure setup --mode bundled \
  --bundled-endpoint 'postgres://pg:5432/orbitjob?sslmode=disable'
unset PG_PASSWORD
```

This command does not test connectivity, create roles or apply migrations. The
chart hooks perform those operations after the installation Secret exists.
Do not continue if the server, bootstrap account or in-cluster endpoint is
unavailable.

## External PostgreSQL

Use a PostgreSQL URI with a database name, username and nonempty password.
Percent-encode reserved characters in a manually written URI. `configure`
preserves connection/TLS query parameters when deriving role DSNs, and removes
query login overrides `user`, `password`, `passfile`, `service` and `servicefile`.
Generated usernames and passwords are encoded with URL rules.

Create a private DSN file using a secret editor or protected credential channel:

```bash
umask 077
install -m 0600 /dev/null /tmp/orbitjob-bootstrap-dsn
```

Write a URI such as this into that file, replacing the example values:

```text
postgres://dbadmin:PERCENT_ENCODED_PASSWORD@db.example.com:5432/orbitjob?sslmode=verify-full
```

Run from the selected source root:

```bash
go run ./cmd/configure setup --mode external \
  --database-dsn-file /tmp/orbitjob-bootstrap-dsn \
  --state .runtime/database.json \
  --runtime-env .runtime/database.env
```

The input must be a regular private file with no group or other permission.
Initial external setup connects with a five-second ping timeout, initializes
roles and passwords, then checks login connectivity as migrator, admin and
runtime. It does not apply numbered migrations or create the Default workspace.
Stop on an authentication, TLS, role or connectivity error. Preserve any partial
role setup and investigate before retrying; the database can have changed even
when local state was not written.

A setup machine's port-forward is not an address cluster Pods can use. Configure
a DNS endpoint reachable by all consumers or derive a separate in-cluster Secret
with reviewed endpoint replacement, as the kind helper does. Replacing an address
must preserve the intended database, TLS identity and credential mapping.

### TLS files

A DSN can contain `sslrootcert`, `sslcert` or `sslkey`, but each referenced path
must exist for every consumer. Host files do not appear in Pods. The current
chart has no general database TLS volume-mount settings; provide a maintained
customization for owner-init, migrate, bootstrap and all application consumers
when private CA or client-certificate files are needed. Verify that rendered
Pods receive those files before installation. Do not weaken `sslmode` to bypass
a missing trust configuration.

### Create the installation Secret

For a first installation, load the generated shell environment and create one
Secret in the release namespace. This example uses the chart's default Secret
name and keys. Set the namespace to the intended installation, then check the
context:

```bash
export RELEASE_NAMESPACE=orbitjob-system
export DATABASE_SECRET_NAME=orbitjob-database
kubectl config current-context
kubectl create namespace "$RELEASE_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -
```

The following command creates a private temporary manifest and does not place
credentials in command arguments. It requires Bash, Python 3, OpenSSL and
kubectl. It deliberately refuses an existing Secret:

```bash
(
  set -euo pipefail
  umask 077
  set -a
  . .runtime/database.env
  set +a
  export RELEASE_NAMESPACE DATABASE_SECRET_NAME
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

The Secret contains four role DSNs, three role passwords and the initial API
key. `database.secretName` and `database.keys` customize the mapping; update
all fields together when customizing it. Replicas share this installation
configuration, but that fact does not establish high availability.

This is a first-installation procedure. Do not generate a new bootstrap key
for an initialized database. Bootstrap commits the key identity in PostgreSQL
and delivers it to a separate fixed Secret named `bootstrap-api-key`, key
`api-key`. A conflicting existing delivery Secret is an error, not a reason to
replace the database key. `bootstrap.secretName` is deprecated and ignored.

After checking installation state and protecting its backup, remove the
temporary input DSN file. Keep generated state, the live Secret and PostgreSQL
role passwords consistent.

## Migration execution and compatibility

The chart runs owner-init at hook weight `-30`, then migrate at `-20`, before
install or upgrade. Application bootstrap runs after install/upgrade at weight
`-10`. Hook completion is separate from workload readiness.

The migrator requires `MIGRATION_MODE=migrate`, `MIGRATOR_DSN` and the migration
directory. It takes an installation advisory lock, validates recorded file
checksums and applies each unapplied `.up.sql` in its own transaction. Earlier
successful migrations remain committed if a later one fails. SQL files ending
in `.down.sql` exist for some historical changes, but the deployment migrator
has no down operation. `MIGRATIONS_BASELINE_VERSION` is rejected.

Inspect the complete ledger with an authorized maintenance connection:

```sql
SELECT version, name, checksum, applied_at
FROM schema_migrations ORDER BY version;
```

The current chain is `0001_baseline` through `0028_audit_resource_identity`. Verify
file identity and ordering as well as the maximum version. Published migration
bytes, including comments, and recorded checksums must remain unchanged.

### Preserve the migration input channel

The chart packages SQL in a ConfigMap with YAML `|-`, which removes trailing
line-feed bytes. The migrator hashes the bytes read from its mounted files.
Consequently, a Helm-initialized database and a raw-filesystem initialization
can record different checksums for identical SQL statements.

Keep an installation on the channel that initialized its ledger. For a chart
installation, preserve and verify the Helm-mounted ConfigMap representation.
For a raw-filesystem installation, keep its original raw inputs. Do not run
`make migrate-up` against a Helm-initialized installation without a separately
reviewed compatibility procedure. Restoring trailing newlines, editing
`schema_migrations` or changing historical SQL to make a checksum match discards
the compatibility check.

<a id="consolidated-local-development-lineage"></a>

### Existing development lineages

A database recording version 7 as `storage_integrity` belongs to an older
backend-only development chain. The current version 7 is
`platform_key_documents`; checksum validation must reject that mismatch.
Preserve its data and receipts. Recreate only a disposable database; retained
data requires a reviewed export/restore or migration plan. Do not apply two
version-7 files or clear the ledger to make it appear current.

### Writer cutover

For a retained installation, [stop submissions and drain](operations/upgrading.md#stop-submissions-and-drain)
before applying new CRDs and migrations. Stop older Admin API publishers,
Operators and other incompatible writers, verify their termination, migrate,
then start only compatible images. Helm pre-upgrade hooks do not stop the old
Deployments.

Preserve these compatibility constraints:

- Migration 0012 acknowledges existing terminal outcomes at cutover to preserve
  their statistics. Automatic recovery applies to later terminal outcomes.
  Per-run SLI deduplication facts survive run-history pruning; explicit SLI or
  tenant maintenance must remove their dependent facts in the required order.
- Migration 0013 and subsequent execution handling retain original JobRun/Job
  identity, execution uncertainty and durable stop evidence. `ExecutionUnknown`
  and `CancelUnknown` are unresolved states. Missing API objects or elapsed
  time do not authorize a retry, a terminal result or release of concurrency.
- Full-digest run addresses require compatible publishers. Existing matching
  historical resources remain readable without a bulk rename. Use the returned
  Kubernetes address instead of reconstructing a shortened suffix.
- Retained Pod evidence and pending release progress must be committed before
  attempt progression or pruning. Keep PostgreSQL and a compatible Operator
  available until evidence finalizers have been safely released. Follow the
  [upgrade and uninstall drain](operations/upgrading.md#stop-submissions-and-drain).

Before migration 0012, check for cross-tenant SLO references with the maintenance
identity:

```sql
SELECT s.id AS slo_id, s.tenant_id AS slo_tenant, i.tenant_id AS sli_tenant
FROM slos s JOIN slis i ON i.id = s.sli_id
WHERE s.tenant_id <> i.tenant_id;
```

Resolve any returned ownership inconsistency before migration. The new foreign
key rejects it; migration does not reassign or delete those rows.

## Schema privileges and account changes

Migration 0012 revokes application `UPDATE`, `DELETE` and `TRUNCATE` on the audit
parent and existing partitions. New partitions must preserve that append-only
application boundary. Offline audit retention uses an authorized table-owner
maintenance identity.

Migration 0014 adds an RLS-protected execution-log directory. Runtime can manage
its records; Admin can read records and update only `cleanup_requested_at` for
audited cleanup. Output remains on the separate archive PVC. Directory storage
and identity admission have independent capacity limits; see
[execution logs](execution-logs.md).

Bounded SECURITY DEFINER functions grant Admin only the required read/identity
surface while retaining tenant table RLS:

| Migration | Behavior |
|---|---|
| 0007 | Platform-key preset documents and permitted preset boundary for an already authenticated key; does not authenticate a caller or return tenant policies |
| 0011 | Workspace metadata directory using the application's authorized tenant scope and literal search before pagination |
| 0027 | Fixed identity operation set for accounts, memberships, sessions, capabilities and installation audit |

These functions are owned by `orbitjob_table_owner`, pin `search_path` and do
not grant runtime, operator or PUBLIC access to those privileged interfaces.
The platform policy reader preserves explicit Deny and boundary intersection;
a selected tenant does not resolve a platform preset's `self` scope.

Migrations 0008 and 0009 add Function creation receipts and Workflow schedule
metadata. Apply them before deploying their readers/writers. Existing custom
policy documents are not expanded merely because named built-in presets gain
actions. Historical planned Workflow times remain NULL; rejected rows require
a reason and recorded source UIDs retain their uniqueness.

Migration 0027 adds personal Console accounts without converting API keys into
users or memberships. Existing API keys keep their bcrypt authentication and
serve scripts; they cannot sign in to the Console. Deploy the matching Admin
image, configure HTTPS or explicit loopback development mode and follow
[first administrator setup](accounts.md#create-the-first-administrator).
The active Default workspace is required. Setup atomically consumes a one-time
capability and records the administrator, explicit workspace ownership and
permanent claim marker. Lost administration uses
[operator recovery](accounts.md#recover-installation-administration), not
account deletion or clearing that marker.

Migration 0028 widens `audit_events.resource_id` to `TEXT` on the partitioned
parent and its existing partitions. Checked-definition audit events retain the
complete `namespace/name`; valid Kubernetes identities can exceed the former
64-character column. Existing rows, resource indexes, tenant policies and
application privileges are preserved. Apply this migration with the table-owning
migrator before deploying the matching application. Existing audit partitions
must retain that ownership. Follow the writer cutover and input-channel rules
above; older installations can still reject a long identity until migrated.
Reverting to a 64-character column requires separately reviewed data handling
once longer identifiers have been recorded.

## Process configuration

A controlled launcher can load the generated file:

```bash
set -a
. .runtime/database.env
set +a
```

| Consumer | DSN selection |
|---|---|
| Admin API | `ADMIN_DSN`, then legacy `DATABASE_DSN` |
| Scheduler | `RUNTIME_DSN`, then legacy `SCHEDULER_DSN`, then `DATABASE_DSN` |
| Operator | `OPERATOR_DSN`, then `RUNTIME_DSN`, then legacy `DATABASE_DSN` |
| Log archive and admission | `RUNTIME_DSN` |
| Bootstrap | `ADMIN_DSN`, then legacy `DATABASE_DSN`; positional DSN also accepted |
| Owner-init | `BOOTSTRAP_OWNER_DSN` and the three role password variables |
| Migrate | `MIGRATOR_DSN` |

Use the primary variables for new deployments. Dispatcher and worker DSN
variables no longer configure any component. The generated JSON is not loaded
by application entrypoints as a replacement for environment configuration.
Admin, Operator and archive need in-cluster Kubernetes configuration; a native
binary and a host kubeconfig do not provide that environment automatically.

## Rotation, endpoint changes and recovery

A Secret update does not restart consumers, and a Pod restart does not rotate a
PostgreSQL password. A rotation must coordinate live role changes, DSNs,
password fields, local installation state, hooks and workload restarts. The
chart does not implement that protocol. `make kind-db` can restart local
Deployments after its Secret changes; it is a development helper rather than
a general rotation workflow.

Changing endpoints changes the installation's data source. Stop writers, back
up, restore or migrate data and validate the new server before switching
credentials. Keep the old database and volumes until the new installation's
history, accounts, policies, execution evidence and archive reads have been
verified. See [backup and recovery](operations/backup-and-recovery.md).

Application rollback must preserve additive schema, accounts, audit records,
deduplication and uncertainty evidence. An older binary that lacks those
contracts is not a compatible recovery version. Restore a coordinated backup
when a schema rollback is required. Do not run historical down files or edit
the migration ledger as an operational rollback procedure.

For permission, connectivity and migration failures, follow
[troubleshooting](operations/troubleshooting.md).
