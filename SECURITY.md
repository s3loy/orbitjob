# Security policy

OrbitJob uses personal-account sessions for the Console and named API keys for
automation. Workspace membership, action/resource policies, PostgreSQL roles/RLS
and Kubernetes namespace controls provide separate authorization boundaries.
This policy describes the current source; use the policy at the corresponding
tag when operating a published version.

## Reporting vulnerabilities

Report vulnerabilities privately to <justs3loy@gmail.com> with the subject
`[OrbitJob Security]`. Do not disclose a working exploit or sensitive installation
data in a public Issue, Discussion or pull request.

Include the affected version/commit, required privileges, a minimal reproduction,
the crossed tenant/database-role/namespace boundary, observed impact and any
suggested correction. Test only in an authorized environment. Exclude real keys,
passwords, DSNs, Secret contents and production output.

Maintainers coordinate a fix and disclosure after acknowledging a reproducible
report. There is no fixed response-time SLA or promised long-term-support line.
Security updates are identified by their release in [CHANGELOG](CHANGELOG.md).
Reporters are credited after public disclosure unless they request anonymity.

## API keys and authorization

API keys begin with `otj_`. The database stores a bcrypt digest; normal creation
returns plaintext once and later reads expose metadata. A key with no grants
can authenticate while every protected capability remains denied. Unknown,
revoked, expired and inactive-tenant credentials receive the same authentication
failure response. Authentication/permission-store outages can instead return
503 and do not establish that the credential is invalid.

Use dedicated named keys with the actions and resources needed by each
caller. Keep bootstrap credentials out of ordinary clients and workloads.
Revocation authorizes the target key's persisted owner/group and audits the
actual authenticated actor. Delegation and boundary propagation cannot expand
the caller's effective grants. Explicit Deny wins over Allow; unsupported or
unprovable containment fails closed.

Authenticated identity introspection returns credential metadata and exact
policy decisions, without credentials, policy documents or resource-existence
probes.

Tenant-authored policies use known exact actions and the supported whole-segment
resource wildcards. Platform presets own wildcard-action capability. Every
protected route declares its action and target. Trusted tenant, group, actor
and path identity come from server/authentication context; client binding cannot
replace them. Platform requests select one authorized tenant for both policy
and data scope. Ordinary tenant credentials cannot select another business tenant.

Group-aware reads and mutations resolve the persisted parent group. Routes
without a supported group restriction refuse group-bound callers. API keys,
policies, resource groups, definition authoring, trigger, ordinary history and
logs have independent grants. A resource ID or request reference grants no
access. [Manage access](docs/tasks/manage-access.md) lists task prerequisites.

Bootstrap can retry delivery of a configured plaintext key after a committed
key and failed delivery. It verifies the existing digest and refuses conflicting
file/Secret contents; it cannot recover plaintext from the database or silently
rotate it. Its Role permits namespaced Secret creation and a name-restricted
get of `bootstrap-api-key`, without list/update/patch. The deprecated
`bootstrap.secretName` value does not change that fixed destination.

## Browser Console

Users obtain personal accounts through single-use installation capabilities or
workspace invitations. API keys do not identify account owners and cannot sign
in to the Console. Initial setup requires `/orbitjob-identity` with the
installation's Admin DSN. A persistent claimed marker prevents setup from
reopening automatically; lost installation administration requires an operator
recovery capability. See [accounts and recovery](docs/accounts.md).

Passwords use Argon2id with 64 MiB memory, three iterations and parallelism one.
They require at least 15 characters and at most 1,024 UTF-8 bytes, with a local
common-password blocklist and rejection of short repeated patterns. This source
provides no email reset service, OIDC, passkeys or multifactor authentication.
Existing-account invitation acceptance requires that account's live session and
never changes its password. Only installation administrators issue global reset
links. Password changes/resets revoke all sessions for the affected account.

Production cookies use `__Host-orbitjob_session`, `HttpOnly`, `Secure`,
`SameSite=Strict`, `Path=/` and no `Domain`. Only the session-secret hash is stored
in PostgreSQL. Remembered sessions have a seven-day absolute lifetime; unchecked
remember creates a browser-session cookie with an eight-hour server lifetime.
Polling does not extend expiry. Browser restoration can retain session cookies;
shared-device users must sign out.

A separate CSRF token stays in page memory. Cookie-authenticated writes require
an exact allowed Origin and `X-CSRF-Token`; login and capability submission also
validate Origin. Explicit invalid Bearer authentication does not fall back to a
session cookie. `CONSOLE_PUBLIC_ORIGIN` can fix the exact HTTPS origin; otherwise
Admin expects HTTPS plus request Host. Reverse proxies must preserve public
Host. Forwarded headers do not relax this boundary.

`CONSOLE_ALLOW_INSECURE_LOCALHOST=false` is the default. Explicit local mode
permits HTTP loopback only and uses `orbitjob_session_local` without Secure.
It does not permit sign-in through node/public HTTP addresses. See
[origin configuration](docs/accounts.md#https-and-local-development).

Console assets/navigation are public. `/healthz`, `/openapi.json` and `/metrics`
are also unauthenticated; control their external exposure at the installation
network/gateway. Protected API routes retain authentication, policy, body limits,
rate limits and request correlation. Missing assets and
unknown API paths do not fall back to the browser application. Console responses
set a same-origin CSP, nosniff, frame exclusion and no-referrer. Scripts, fonts
and styles are local; inline styles are permitted for the component library.
The installed bundle excludes the demo client. Enter credentials only at the
trusted installation origin. Same-origin script execution can act through an
HttpOnly session; HTTPS, CSP and device protection remain necessary.

Each workspace request checks active account, membership and effective grants.
Installation administrators manage global accounts and workspace metadata with
no implicit workload access. Workspace creators receive explicit owner
membership. Account disablement revokes sessions; membership disablement and
role changes affect subsequent workspace requests. Privileged account changes
and browser API-key/policy/group writes require password confirmation within
15 minutes. User actor IDs remain separate from key IDs.

The browser stores the non-secret selected workspace ID and unconfirmed
source-control intent in `sessionStorage`. Retained source-control intent is
scoped to the authenticated subject, workspace and source; the same subject can
review it after reconnecting. Protected in-memory query and operation state
clears on sign-out, a 401 or identity/workspace change.
Setup/invitation/reset fragments are removed from the address bar and
held in page memory. One-time key results use a separate volatile store outside
query/mutation caches; discard/disconnect removes them, and disconnected clients
drop late creation responses. A lost creation result requires reconciliation,
without automatic replacement-key creation.

Console Job, Workflow and Function authoring drafts remain in page memory for
the current connection. Reloading, signing out, receiving a 401 or changing
identity/workspace clears them. Server-side private draft persistence and
recovery are unavailable. Save unfinished content outside the Console before
ending or changing the connection.

Accepted definition-save and Function-create operations retain server-side
actor/request identity for reconciliation. Their operation receipts identify
accepted writes; they do not recover unsaved authoring drafts. A request UUID
alone does not authorize access to an operation.

## Definitions, discovery and monitoring evidence

Online authoring accepts typed ScheduledJob/WorkflowJob specs in mapped tenant
namespaces, with independent Apply permission. Both checking and persistence
are authorized. Kubernetes dry-run checks the reviewed payload; persistence
requires its digest, stable request identity and expected UID/resourceVersion.
Concurrent replacement conflicts. Unknown writes retain identity and reconcile
against their operation history instead of automatically writing again. The
API does not provide arbitrary Kubernetes metadata/resource writes.

The default chart enables a fail-closed validating webhook for Job/Workflow
definitions in mapped namespaces. It verifies the final typed spec, reviewed
digest and live server operation. Explicitly configured native writers bypass
the checked-save receipt requirement while retaining typed-spec validation.
Treat native-writer configuration as installation authority. An unavailable
webhook can block definition persistence; disabling it removes that final-object
check.

Function Create/Update actions are independent of reads and invocation. Creates
bind tenant, actor, request key and normalized payload. Updates compare a
reviewed definition version atomically; version-pinned invocations reject an
unready or changed version. Definition changes and audit commit together.
Preset migrations extend only their named presets; custom policies do not
implicitly acquire new actions.

The Bearer workspace directory returns only authorized name, slug, status and
immutable ID. Tenant keys see their own labels; foreign rows require effective
`tenant:Get`, and group-bound credentials cannot discover foreign workspaces.
Platform credentials have no implicit own row. Visibility incorporates explicit
Deny and recursive boundaries before pagination. It returns visible `has_more`
without a global total. The Admin-only metadata reader does not widen ordinary
tenant-table RLS or grant workloads. Human sessions list active memberships
separately; metadata discovery never creates membership.

### Monitoring evidence

CheckRun, SLO budget/history and alert reads authorize their actual parent Check
or SLO group and ID, including group-specific Deny for ungrouped credentials.
Another group's parent appears unavailable. CheckRun/alert collection permission
must cover the entire requested query domain. A relevant per-ID/group Deny,
boundary restriction or exhausted work budget for the containment proof rejects
the request before reading items or counts; pagination cannot bypass it. Successful
collection responses do not silently omit denied rows or counts.

Each derived read requires its own action. Embedded SLO `current_budget` is
omitted for group keys; ungrouped access still requires `slo:GetBudget` on the
actual parent group/ID. SLO creation validates a live SLI in the same tenant and,
for scoped callers, the same group inside the write transaction. Missing or
inaccessible references return not-found.

## PostgreSQL roles and RLS

| Role | Purpose |
|---|---|
| `orbitjob_table_owner` | NOLOGIN owner of schema objects and reviewed security functions |
| `orbitjob_migrator` | Migration login allowed to assume the table owner |
| `orbitjob_admin` | Management writes, reviewed account-security calls and read-only execution history |
| `orbitjob_runtime` | Operator/scheduler/admission/archive login with required runtime writes |
| `orbitjob_operator` | NOLOGIN execution grants; not a separately deployed login |

Runtime processes must not use installation-owner, table-owner or superuser
credentials. Operator resolves `OPERATOR_DSN` before its Runtime fallback.
Use the installation's configured Secret names/keys and migration input channel;
see [database setup](docs/database-setup.md).

Application roles do not own tenant tables. ENABLE RLS applies without FORCE
RLS; the NOLOGIN owner remains the maintenance boundary. Repositories set
transaction-local `app.tenant_id` in the same transaction as queries. Session-level
tenant settings are unsafe with pooled connections. Audit parents and direct
partitions grant append/read as required, without application UPDATE, DELETE or
TRUNCATE. A domain mutation and its audit commit atomically.

Narrow SECURITY DEFINER operations cover authentication, bootstrap, active-tenant
enumeration, key-owner discovery, platform presets, workspace metadata and
account operations that precede ordinary tenant scoping. They fix search_path,
revoke PUBLIC execution and grant specific roles. The baseline also revokes
PUBLIC CREATE on schema public. A new cross-tenant operation needs equivalent
review and real PostgreSQL boundary tests.

Terminal projection acknowledgement follows successful idempotent derivatives.
SLI increment/deduplication facts commit together and outlive ordinary run pruning.
Migration 0012 preserves old aggregates instead of replaying old terminal rows;
old Operators must stop before cutover. Published migration bytes are immutable,
including comments. See the [migration procedure](docs/database-setup.md#migration-execution-and-compatibility).

## Kubernetes workloads and execution recovery

Namespace mappings determine tenant identity; a workload's own tenant claim
does not. Admin has namespaced definition get/create/patch, run create/get/patch,
Job get, Pod get/list and pods/log get. It has no Job/Pod lifecycle writes,
status writes, Secret, exec or attach access. Operator owns execution/status and
retention operations only in mapped namespaces; its Lease permissions are in
the installation namespace. JobRun actor attribution trusts the resource writer:
a principal allowed to write JobRun CRs can choose its actor field. Restrict
direct CR writes to the intended control-plane identities.

Workload Pods disable service-account token mounting and use
`restartPolicy: Never`. Execution policy version 1 adds non-root UID 65532,
RuntimeDefault seccomp, no privilege escalation and dropped capabilities, plus
pinned CPU/memory/ephemeral-storage requests and limits. Images must work under
those settings. Legacy version 0 does not acquire all version 1 protections
merely by upgrading the Operator. Never widen a default workload ServiceAccount
to compensate for an unsupported application requirement.

Containers still execute workload-supplied code. Apply namespace admission,
Pod Security, approved images and network controls for the installation's trust
model. The chart's restricted-v1 NetworkPolicy selects version 1 workload Pods,
denies ingress and limits egress to configured DNS and allowed destinations.
Enforcement depends on the CNI. Production rendering requires an administrator's
successful enforcement declaration; Helm rendering cannot verify packet denial.
Legacy Pods outside that selector need separate network policy. HTTP probes
also make outbound requests from their workload namespace; configure their
allowed destinations accordingly.

Run/Attempt adoption requires complete identity and immutable Job UID.
Cancellation patches and retention deletes use identity/version preconditions.
Workflow stops prevent new child creation. Current names preserve full occurrence
digests; matching historical names remain lookup-only compatibility candidates.
Retention verifies the original tenant/source/revision/occurrence before deleting
terminal resources and then pruning records.

A missing bound Job produces nonterminal `ExecutionUnknown`, retains its
concurrency slot and prevents automatic replacement. Cancellation can remain
`CancelUnknown` until complete stop evidence is available. An empty Pod list,
a deletion timestamp, node loss or external force removal cannot prove that
workload code stopped. There is no public force-resolution API.

New workload Pods hold termination evidence with an Operator-owned finalizer.
The Operator persists complete owned-container evidence before releasing its
hold with Pod UID/resourceVersion checks. Cancellation also requires controller
stop acknowledgement, then proof persistence/release before original-Job deletion.
Admin and collector have no Pod patch permission. This preserves available
observations across outages without proving termination after evidence loss.
Client-go leadership coordinates singleton work; transactional source admission
provides a separate concurrency boundary. See
[execution recovery](docs/architecture.md#execution-recovery-and-concurrent-admission).

Kubernetes backoff and platform Attempts can repeat external effects despite
Never restart policy. Version 1 requires retrySafe before enabling repetition.
Request idempotency does not make those external effects exactly once.

## Execution logs and installation secrets

Log reads use independent parent-specific ReadLogs grants and immutable recorded
JobRun/Job/Pod ownership. Instance routes refuse group-scoped credentials;
Function routes resolve the actual parent group and history UUID. The legacy
snapshot route has byte/line/time bounds and no durable retention promise.
Kubernetes garbage collection can remove that source evidence.

A trusted Fluent Bit collector reads mapped task CRI paths and forwards output
to the archive over collector mTLS; Admin uses reader mTLS. The single archive
writer uses Runtime database
credentials and a dedicated bounded PVC. Cursor reads, follow/search/export
and audited DeleteLogs requests remain independently authorized. Output is inert
text and may contain secrets. Capacity or collection failures produce gaps;
sealed archives do not prove complete coverage. See
[execution logs](docs/execution-logs.md) for retention, integrity and recovery.

Keep generated installation state, keys, passwords, DSNs, Secret plaintext and
sensitive smoke output out of source control and public reports. Protect
`.runtime/`, `.kind/`, local credential files, backups and administrator devices.
Use restrictive file permissions and an approved secret-delivery process. Do not
put keys in URLs, frontend build variables or public documentation-site inputs.
Back up the database, archive volume and installation configuration together and
exercise restoration in an isolated environment.

Recorded verification is scoped to its source and environment. It does not
prove an arbitrary deployment's security or outage recovery. Current evidence
and limits are recorded in [verification](docs/verification.md) and
[known issues](docs/known-issues.md).
