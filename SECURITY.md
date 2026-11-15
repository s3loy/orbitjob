# Security policy

This policy describes the current source. When operating a published version,
use the security policy at that version's tag. Installation administrators manage
personal accounts, workspace memberships and access policies. Personal accounts use password-authenticated sessions; scripts receive separate
named API keys.

## Reporting vulnerabilities

Please do not disclose vulnerabilities in public Issues, Discussions or PRs.
Email <justs3loy@gmail.com> with the subject `[OrbitJob Security]`.

Include the affected commit/version, required privileges, minimal reproduction,
tenant/database-role/namespace boundary, actual impact and a suggested fix.
Test only in authorized environments. Do not send real API keys, passwords,
Secret contents or production data.

Maintainers coordinate a fix and disclosure after acknowledging a reproducible
report. No fixed response-time SLA or long-term-support line is promised here.
Recorded releases are in [CHANGELOG.md](CHANGELOG.md); the packaged chart/app
version is defined in [Chart.yaml](charts/orbitjob/Chart.yaml). Security updates
are identified by their release. Reporters are credited after a fix is
public unless they request anonymity.

## Current security model

The boundaries are personal-account sessions or automation Bearer authentication,
explicit workspace memberships and policy grants, trusted tenant/group handling,
PostgreSQL least-privilege roles/RLS, and Kubernetes
namespace RBAC/admission/Secret controls. Each addresses a different failure;
database RLS does not authorize an HTTP action or validate Kubernetes ownership.
[Verification](docs/verification.md) records the tested boundaries and remaining
qualification work.

### API keys and authorization

API keys use the `otj_` prefix. PostgreSQL stores a bcrypt digest, not recoverable
plaintext. Normal key creation returns plaintext once; list operations return
metadata. Bootstrap can retry delivery of the same configured plaintext after a
committed key and failed Secret write: it first verifies the existing bcrypt
digest, and refuses a conflicting file or Secret. It does not recover plaintext
from the database or rotate an existing credential silently.

Bootstrap Secret delivery grants namespaced create and a `resourceNames`-scoped
get of `bootstrap-api-key` for matching-data retries. It has no Secret list,
update or patch grant. The legacy `bootstrap.secretName` chart value is
deprecated and ignored; remove the override rather than treating it as a writer
destination setting. The application has always used the fixed delivery name.

`orbitjob_auth_api_key` resolves candidate digests before a tenant is known. It
is SECURITY DEFINER with a fixed search path and a restricted execution grant;
the application performs bcrypt comparison. Unknown, revoked, expired and
inactive-tenant credentials receive the same authentication failure response.

Admin API routes enforce explicit action/resource policy grants. A key without
grants authenticates but cannot access protected capabilities. Explicit Deny
wins; delegation and boundary intersection must not expand the caller's grants.
Tenant policies use the supported exact actions and resource-segment semantics;
platform presets own wildcard capabilities. The
[architecture](docs/architecture.md#tenant-and-authorization-boundaries)
describes policy evaluation and trusted request identity.

Policy documents accept only lowercase `version` and `statement` at the root,
and `effect`, `action` and `resource` in statements. Parsing rejects duplicate
members, case variants, unsupported fields such as `Condition` and `NotAction`,
and trailing JSON values. Boundary intersections retain explicit Deny from
both the grants and boundaries, including nested constraints; a boundary does
not grant access without a source grant.

Request bodies cannot replace server-owned tenant/group/path identities.
Group-aware checks, SLIs, SLOs and their derived reads retain group scope; a
surface unable to enforce a group restriction refuses a group-scoped key.
Cross-tenant management requires a policy grant reaching the actual target;
bootstrap tenant identity is not an implicit grant. Revoke authorizes the target
key's owner/group and audits the authenticated actor.

Keep bootstrap credentials out of ordinary business clients. Use dedicated keys
with required grants, and add ingress/gateway access control before exposing
management endpoints to untrusted networks.

### Browser console

Console users obtain a workspace invitation and use a personal account. The
installer issues the first administrator's single-use setup link through
`/orbitjob-identity` using the installation's Admin DSN. The claimed installation
marker never automatically reopens setup. Lost administration requires an
operator-issued recovery capability. Legacy API keys do not identify account
owners. See [accounts and recovery](docs/accounts.md).

Passwords use Argon2id with 64 MiB memory, three iterations and parallelism one.
The server requires at least 15 characters, at most 1,024 UTF-8 bytes, and rejects
its local common-password blocklist and short repeated patterns. It stores no
recoverable password. There is no email delivery or multifactor authentication
in this source. Invitation acceptance for an existing account requires that
account's live session and never changes its password. Only installation
administrators can issue global password-reset links. Password changes and
resets revoke all sessions for that account.

The backend Admin image provides the account routes without a compiled Console
bundle. When browser assets are absent, `/console/` returns 503 while API routes
remain available. The public navigation route does not bypass protected API
authentication, policy grants, tenant rate limits, body limits or trace/metrics
middleware. Enter passwords only through a trusted account client and origin.

Production sessions use `__Host-orbitjob_session` with `HttpOnly`, `Secure`,
`SameSite=Strict`, `Path=/` and no `Domain`. PostgreSQL stores only a hash of the
session secret. `remember: true` selects a seven-day absolute persistent
session; `remember: false` selects a browser-session cookie and an eight-hour
server lifetime. Requests and polling do not extend expiry. Browser session restoration
may retain session cookies, so shared-device users must sign out explicitly.
The separate CSRF token is returned in the session snapshot. Every cookie-authenticated write
requires exact Origin validation and `X-CSRF-Token`; login and capability
submission also check Origin. An explicit invalid Bearer credential never falls
back to cookie authentication.

`CONSOLE_PUBLIC_ORIGIN` optionally fixes the exact public HTTPS origin. Otherwise
the expected origin is `https://` plus request Host. Reverse proxies must preserve
the public Host; forwarded headers cannot relax origin or cookie security.
`CONSOLE_ALLOW_INSECURE_LOCALHOST=false` is the default. Explicit local mode
allows HTTP loopback only, using `orbitjob_session_local`; non-loopback HTTP
addresses cannot sign in. See [origin settings](docs/accounts.md#https-and-local-development)
for Helm and local development configuration.

Treat session cookies and capability results as credentials. Same-origin script
execution can act through an `HttpOnly` session; HTTPS and client/device
protection remain necessary.

Each request rechecks active user and workspace membership state and effective
policy grants. Installation-administrator status controls global accounts and
workspace metadata; it grants no implicit workload access. Workspace creators
receive explicit owner memberships. Account disablement revokes sessions, and
membership disablement or role changes affect subsequent workspace requests.
Privileged account changes and browser API-key, policy and resource-group writes
require password confirmation within 15 minutes. Stable user actor IDs are
separate from key IDs. Installation security events use a separate append-only
audit stream; workspace changes retain transactional tenant audit records.

Save one-time API key creation results in an approved secret store. Reconcile
unknown create results before retrying; list operations cannot recover key
plaintext. Identity introspection returns metadata and exact policy
decisions, never credentials, policy documents or resource-existence probes.
Platform requests use one explicitly selected tenant for both authorization
and data access; ordinary tenant credentials cannot select another tenant.

For Bearer clients, the authenticated `/workspaces` directory exposes only name,
slug, status and immutable ID. Tenant keys can read their own identity labels; foreign
rows require exact effective `tenant:Get` grants. Group-bound credentials cannot
discover foreign workspaces. Platform credentials have no implicit own row.
The policy package computes the exact finite/cofinite visibility set, including
recursive boundaries and explicit Deny. Migration 0011's Admin-only metadata
reader applies that trusted scope and literal name/slug search before pagination;
`has_more` describes visible results and no global total is returned. The reader
uses a fixed search_path and does not widen table RLS or grant workload access.
Directory requests cannot select a business tenant. Administration selection
changes only the existing explicit management target; ordinary tenant business
queries retain their credential-bound scope. Human sessions list their active
memberships separately; installation metadata discovery does not create one.

Online definition reads and writes use typed routes restricted to
installation-mapped tenant namespaces and ungrouped credentials. Independent
Apply grants protect both dry-run and persistence. Only spec and server request
annotations are written; UID/resourceVersion comparison prevents silent
replacement of concurrent edits. Reconcile unknown writes by reading their
recorded request identity before retrying.

Log reads require independent parent-specific grants and immutable recorded
JobRun/Job/Pod ownership. Admin has only namespaced definition get/create/patch,
Job get, Pod get/list and pods/log get in addition to its run publication grants.
There is no generic Kubernetes proxy or secret/exec/attach access. Independent
archive routes provide bounded cursor reads, cursor-based search/download
and audited DeleteLogs requests. A trusted Fluent Bit collector reads node CRI
paths in mapped namespaces and uses collector mTLS; Admin uses reader mTLS.
The single archive writer uses Runtime database credentials, not a new narrow
DB login. Archive output and events live on a dedicated bounded PVC and remain
sensitive. See [execution logs](docs/execution-logs.md).
Snapshots have byte/line/time bounds.
Legacy Jobs without a JobRun controller reference are unavailable, and
Kubernetes garbage collection can remove diagnostic logs with their owner.

Platform policy documents are loaded through the migration 0007 Admin-only
SECURITY DEFINER reader for the already authenticated key ID. It returns only
live tenant-NULL platform credentials' bound presets and boundary documents;
PUBLIC/runtime/operator cannot execute it. Invalid configured boundaries fail
closed. Policy boundaries are evaluated against each actual request, retaining
explicit Deny across grant documents. The selected tenant never expands preset
self semantics or changes ordinary tenant-key RLS.

Function management has independent Create/Update grants. Group-scoped writes
and reads resolve the actual persisted group; explicit Deny remains authoritative.
Create receipts bind tenant, actor, key and payload. Updates compare the reviewed
version atomically; version-pinned invocations also check the active revision.
Definition writes and audit records commit together. New actions update only the
named TenantAdminAccess preset; existing explicit tenant policies gain no writes.

Workflow automatic starts use a durable occurrence ledger, exact CR identity and
atomic overlap checks shared with manual starts. A WorkflowRun must be bound to
its original UID and observed live before steps begin. Publication repair rejects
foreign or replacement resources. Retention deletes only terminal resources after
full tenant/source/revision/occurrence identity checks, using UID/resourceVersion
preconditions, before pruning records. Operator `workflowruns` delete is granted
only in installation-mapped tenant namespaces. No new cluster-wide write grant
is added. Rejected scheduled starts retain a receipt through the trigger minute
and cannot be replayed from a stale Schedule CR after ledger pruning.

### Console monitoring evidence scope

Check results, SLO budgets/history and SLO alerts apply the authenticated
credential's group through their parent Check or SLO. Detail and single-SLO
budget/history reads authorize the actual parent group and ID even for ungrouped
credentials, preserving group-specific Deny. Other-group parents return 404,
including budget history. CheckRun/Alert collection permission must cover the
whole tenant or credential-group query domain. A relevant per-ID/group Deny,
boundary restriction or exhausted proof budget returns 403 before items/counts
are read; filters and pagination cannot bypass this proof. Denied items/counts
are not silently removed. A group ARN grant must not widen into tenant-wide
evidence. Each read still needs its independent grant. Embedded SLO
`current_budget` remains omitted for group keys; ungrouped callers require
`slo:GetBudget` on the actual SLO group and ID, including explicit Deny.
Check/SLI/SLO definitions retain their group filters. SLO creation validates its
SLI reference inside the write transaction: same tenant, live row and matching
caller group when scoped. Missing or inaccessible references return not-found.

### PostgreSQL roles and RLS

| Role | Purpose |
|---|---|
| `orbitjob_table_owner` | NOLOGIN owner of schema objects and security functions |
| `orbitjob_migrator` | Migration identity allowed to assume the table owner |
| `orbitjob_admin` | Admin API/bootstrap management writes, reviewed account-security functions and read-only access to execution history |
| `orbitjob_runtime` | Scheduler/operator login identity with required runtime writes |
| `orbitjob_operator` | NOLOGIN execution-role grants; not a separate deployed login credential |

Runtime processes must not use the installation owner, table-owner or superuser
DSN. The operator resolves `OPERATOR_DSN` before the runtime fallback; Secret
names/keys and connection examples are defined in [chart values](charts/orbitjob/values.yaml)
and [deploy/env/](deploy/env/). See [database setup](docs/database-setup.md).

Review the complete [migration directory](db/migrations/) for RLS,
grants and functions, including `sli_snapshot_events` added by migration 0012.
Application roles do not own tenant tables. ENABLE RLS therefore applies to
them without FORCE RLS; the NOLOGIN owner remains the maintenance boundary.
Repositories set transaction-local `app.tenant_id` in the same transaction as
queries. Session-level tenant settings are unsafe with pooled connections.

Audit parents and direct partitions allow application append/read as granted,
but not UPDATE, DELETE or TRUNCATE. Domain changes and their audit facts commit
together. SLI/SLO references enforce tenant ownership through constraints and
transactional source/group checks. Projection acknowledgement follows successful
idempotent derivatives; retained deduplication facts must not be pruned without preserving deduplication guarantees.

Narrow reviewed SECURITY DEFINER entrypoints handle authentication, active tenant
enumeration, bootstrap and revoke-owner discovery before a scoped transaction is
available. They fix search paths, revoke PUBLIC execution and grant only to
specific roles. The platform-preset and authorized workspace-metadata readers
have this same narrow contract. New cross-tenant paths require the same review
and real PostgreSQL tests; prefer a scoped transaction when it can answer the
query. The baseline revokes `CREATE` on schema `public` from `PUBLIC`; new
security functions must explicitly revoke public execution as well.

### Kubernetes workloads

The operator resolves the tenant from the configured namespace mapping, not a
tenant claim supplied in a workload resource. Admin's per-namespace Role permits
create/get/patch on JobRun and WorkflowRun resources. Typed authoring adds
namespaced definition get/create/patch; retained log reads add Job get, Pod
get/list and pods/log get. These grants provide no Job/Pod lifecycle writes,
status write, Secret, exec or attach access. Operator RBAC owns execution and
retention operations within configured namespaces.

Rendered workload Pods set `automountServiceAccountToken: false` and
`restartPolicy: Never`. Token mounting must remain disabled; Kubernetes API access
would require a reviewed template extension and a dedicated narrow identity.
Never widen the operator or default namespace ServiceAccount for workload use.

Images execute arbitrary container code within namespace admission, Pod Security
and network controls. Pin images at the registry/admission layer when immutable
content is required, and constrain workload/probe egress through NetworkPolicy or
an egress proxy. Check probes make outbound requests to their configured URLs;
the removed in-process HTTP/webhook handler protections do not apply to them.

Run/attempt adoption requires full identity and immutable Job UID. Cancellation
patches and retention deletes use UID/resourceVersion preconditions. A committed
workflow stop prevents new child creation; uncertain creation remains observable as
`CancelUnknown` until ownership and termination can be confirmed. A Job's backoff
or another platform attempt can repeat external effects despite Pod restartPolicy;
workloads must tolerate repeated execution.

New workload Pods retain termination evidence with an Operator-owned finalizer.
The Operator has Pod `patch` permission only in mapped workload namespaces and
removes only its own finalizer after durable proof, with UID/resourceVersion
checks. Admin and collector credentials receive no Pod mutation permission.
Cancellation requires controller stop acknowledgement before accepting a
complete Pod inventory; a Job deletion timestamp is insufficient. Proof
persistence and evidence release precede deletion of the original Job.
This protocol preserves observations across controller and database outages;
it does not prove termination after node loss or external removal of the hold.

Execution recovery retains a missing bound Job as
`ExecutionUnknown`, holding its concurrency slot and forbidding automatic
replacement. Full namespace Pod observation and immutable owner identities
are required for termination evidence; empty inventories, force deletion and
node loss do not prove that workload code stopped. Client-go Lease sessions
coordinate singleton work, while transactional source admission enforces Forbid.
See [execution recovery and concurrent admission](docs/architecture.md#execution-recovery-and-concurrent-admission)
for these guarantees and their limits.
Admin retains read-only execution-ledger access. There is no force-resolution API.

### Secrets, logs and test environments

Do not commit installation state under `.runtime/`, generated credentials under
`.kind/`, API keys, passwords/DSNs, Secret plaintext or smoke output containing
real identifiers/errors. Database installation files use restrictive permissions;
bootstrap uses a local secret file or the `bootstrap-api-key` Secret. For the
documented release namespace, retrieve it privately with:

```bash
kubectl -n orbitjob-system get secret bootstrap-api-key \
  -o jsonpath='{.data.api-key}' | base64 --decode
```

Never put that output in CI logs or public reports. Log messages must not include
Authorization headers, API keys, DSNs, full request bodies or Secret data. Rotate
role passwords, Secret values and workload environments as a coordinated operation;
updating one does not update the others.

Integration tests require a dedicated disposable PostgreSQL database. They may
rebuild schema and provision roles; never point `TEST_DATABASE_DSN` at an installed
environment. Test results and their applicable environments are recorded in
[verification](docs/verification.md).

## Known contract and compatibility limits

- OpenAPI declares automation Bearer authentication through `APIKey` and Console
  cookie authentication through `ConsoleSession`. Middleware enforces the
  corresponding authentication, CSRF and policy requirements. Check/SLI/SLO DELETE requests require
  the versioned JSON bodies declared in [OpenAPI](api/openapi.yaml).
- Migration 0027 adds human accounts without assigning account ownership from
  existing API keys. Deploy its matching Admin image and chart, then complete
  explicit account setup; see [migration guidance](docs/database-setup.md#personal-account-migration).
- Migration 0012 preserves historical aggregates rather than replaying old
  terminal rows. Stop old operators before migration and start the new version
  afterwards. See [migration compatibility](docs/database-setup.md#migration-lineage-and-compatibility).
- New run resource names preserve complete occurrence digests; matching historical
  names remain readable after full identity checks. See
  [the upgrade procedure](docs/local-deployment.md#upgrading-an-existing-installation).
- Current verification gaps, including independent fault recovery and a bounded
  deduplication archive policy, remain in [known issues](docs/known-issues.md).
