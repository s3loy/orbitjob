# Security Policy

## Reporting vulnerabilities

Please do not disclose security vulnerabilities in public Issues, Discussions or PRs. Email <justs3loy@gmail.com> with the subject `[OrbitJob Security]`.

A report should include:

- the affected commit, version or deployment method
- required privileges and attack path
- minimal reproduction steps
- the tenant, database role, Kubernetes namespace or network boundary involved
- actual impact and a suggested fix

Testing in unauthorized environments is prohibited. Never send real API keys, database passwords, Secret contents or production data; use placeholder values.

## Response and disclosure

Once maintainers acknowledge a report, fix and disclosure are coordinated on
the basis of a reproduction. Version `0.2.1` has no fixed response-time SLA.
Please do not publish exploit details before the fix commit is public.

## Current security model

OrbitJob's security boundary has four layers:

1. Bearer API key authentication
2. Admin API tenant checks and unified input validation
3. PostgreSQL least-privilege roles and RLS
4. Kubernetes Secrets, ServiceAccounts, RBAC and Pod Security

No layer substitutes for another. RLS can reduce the blast radius of unauthorized queries but cannot fix wrong grants; Kubernetes RBAC can constrain the operator's ability to create Jobs but cannot validate business tenants.

### API keys

API keys use the `otj_` prefix. The server stores a bcrypt digest, never recoverable plaintext. The plaintext is returned or written exactly once, at key creation or bootstrap.

The authentication function must look up candidate keys when the tenant is unknown. `orbitjob_auth_api_key` is `SECURITY DEFINER`, with a fixed `search_path`, and `EXECUTE` is granted only to `orbitjob_admin`. After the function returns candidate digests, the application performs the bcrypt comparison.

Revoked, expired, unknown keys, suspended tenants and wrong passwords all return the same `401`, so the existence of a key or tenant is never revealed.

### PostgreSQL roles

The deployment uses these roles:

| Role | Purpose |
|---|---|
| `orbitjob_table_owner` | NOLOGIN; owns tables, sequences and the security functions |
| `orbitjob_migrator` | migration runner; can `SET ROLE orbitjob_table_owner` |
| `orbitjob_admin` | Admin API and bootstrap; SELECT-only on the run-history tables — the baseline's three ledger tables plus `workflow_run_control_plane` and `function_runs` |
| `orbitjob_runtime` | login identity of scheduler and operator (`RUNTIME_DSN`, operator prefers `OPERATOR_DSN`); holds run-ledger write privileges |
| `orbitjob_operator` | NOLOGIN; run-ledger write only, reserved for a future dedicated operator credential |

Runtime processes must never use the bootstrap owner, table owner or PostgreSQL superuser DSN. Credentials live in separate keys of the installation-level `orbitjob-database` Secret, shared by all replicas.

The `baseline` migration revokes `CREATE` on the `public` schema from `PUBLIC`, and the security functions explicitly revoke `PUBLIC EXECUTE`. New functions must repeat both constraints; never rely on database defaults.

### RLS

The migrations run `ALTER TABLE ... ENABLE ROW LEVEL SECURITY` on these 20 relations:

```text
tenants
api_keys
key_policies
policies
resource_groups
audit_events
audit_events_default
job_definition_revisions
job_run_control_plane
job_run_attempts_control_plane
checks
check_runs
slis
slos
sli_snapshots
budgets
budget_alerts
functions            (0004)
function_runs        (0004)
workflow_run_control_plane  (0005)
```

The baseline only `ENABLE`s RLS, it does not `FORCE` it. `orbitjob_table_owner` is NOLOGIN, so no process ever logs in as the owner; `orbitjob_admin`, `orbitjob_runtime` and `orbitjob_operator` are not table owners, and `ENABLE` already enforces RLS against them. `FORCE` only matters when the owner itself must be subject to RLS, which the current identity-separation model does not need.

Policies read the transaction-local variable `app.tenant_id`. Repositories must set the tenant in the same transaction as the query. Never use session-level `SET app.tenant_id`; connection pools reuse connections.

Cross-tenant operations are allowed only through reviewed `SECURITY DEFINER` functions:

- `orbitjob_auth_api_key`
- `orbitjob_list_active_tenant_ids`
- `orbitjob_bootstrap_default`
- `orbitjob_find_key_tenant`

Before adding a cross-tenant query, adjust the data model or use a tenant-scoped transaction instead. When a function is truly needed: fix the `search_path`, narrow the returned columns, revoke from `PUBLIC`, grant only to specific roles, and add a real PostgreSQL integration test.

### Kubernetes workloads

The operator renders each `JobRun` into a `batch/v1` Job in the Kubernetes
namespace mapped to its tenant by `operator.namespaceTenants`, from the
container image, command and args declared in the `ScheduledJob` job template.
Pods run with `restartPolicy: Never` — a failed pod is one failed attempt, and
retry is owned by the platform, never a silent in-process restart. Every
rendered Pod sets `automountServiceAccountToken: false`, so workload containers
receive no Kubernetes API credential even if the namespace's default
ServiceAccount later gains permissions.

Workload Pods must remain tokenless. Kubernetes API access is not part of the
current job-template contract; adding it requires a reviewed API extension and
a dedicated, narrowly scoped ServiceAccount rather than widening the default
or operator identity.

A container image runs with whatever privileges its own manifest requests,
inside the boundary the mapped tenant namespace and its admission policies define. Treat job
images as untrusted inputs: pin them to an immutable digest at the registry or
admission layer if mutable tags are a concern.

### Secrets and logs

Never commit:

- `.runtime/` installation state files (DSNs and role passwords)
- the bootstrap API key
- PostgreSQL role passwords and DSNs
- Kubernetes Secret plaintext
- a `smoke-test-results.md` containing real identifiers or errors

Database credentials are generated by `cmd/configure setup` into `.runtime/` with `0600` permissions, then injected into the cluster as the
installation Secret. The bootstrap key is written by the bootstrap job into the
`bootstrap-api-key` Secret; read it with:

```bash
kubectl -n orbitjob-system get secret bootstrap-api-key \
  -o jsonpath='{.data.api-key}' | base64 --decode
```

Never paste that command's output into a CI log or a public Issue.

The application logs structurally with trace IDs. New log lines must never include the `Authorization` header, API keys, DSNs or full request bodies.

## Known boundaries

- `0.2.1` is the first supported release line; no long-term-support line is promised.
- The operator elects a singleton via Kubernetes Lease; the scheduler's etcd election is optional. PG epoch fencing (writer epoch) was removed together with the legacy execution path.
- The API has no full management RBAC yet. Never expose the tenant/API-key management endpoints directly to untrusted networks; add access control at the ingress or API gateway layer.
- OpenAPI does not yet fully express the Bearer security scheme and some DELETE version bodies. The middleware is authoritative for authentication; deleting a Check/SLI/SLO requires the current `version`.
- Runs execute as Kubernetes Jobs with `restartPolicy: Never`; a workload can therefore be started more than once across attempts. Job images and anything they call must tolerate repeated execution.
- Historical note: the in-process HTTP/webhook/`pg_notify` handlers and their SSRF and signature protections were removed with the worker execution path. The things that make outbound connections now are the user's own container image and the check probes — a digest-pinned curl image requesting the URL each check configures; constrain task-namespace egress with a NetworkPolicy or an egress proxy.

## Supported versions

| Version | Security updates |
|---|---|
| 0.2.1 | Security fixes until the next minor release; no long-term support promised |

## Credits

Reporters are listed after a fix is public, unless they ask to remain anonymous.
