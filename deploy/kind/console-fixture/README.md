# Installed Console account and Function fixtures

This helper provisions test-only accounts, API keys and Function definitions
for the dedicated `orbitjob-console-e2e` kind installation. Credential setup
uses guarded owner SQL in this disposable database. Function seeding calls the
existing `FunctionRepository.CreateForTenant`; it inserts no execution revision
or invocation history. The real operator must project the active revision and
execute every invocation used as evidence.

Run commands from the repository root. All commands require:

- `ORBITJOB_CONSOLE_FIXTURE=1` set explicitly.
- One explicit `KUBECONFIG` file whose current context is
  `kind-orbitjob-console-e2e`.
- The local kind cluster with that name, matching node names, namespace
  `orbitjob`, and the installed `orbitjob-database` Secret.
- A verified loopback kind API endpoint using embedded client certificates.
- Explicit native-writer authorization for the fixture's Kubernetes identity in
  this disposable installation. The helper creates definition CRs directly;
  the default admission configuration rejects those writes. Follow the local
  development guide's dedicated-fixture setup before preparing accounts.

The helper reads `admin-dsn` from that installation Secret into memory and
connects through its own loopback port-forward to the same cluster's PostgreSQL
Service. It refuses another host, database or role. DSNs, API keys and raw
kubeconfig data are not printed. Credential provisioning writes secrets only to
its private credential manifest; the Function ownership manifest contains no
credentials. The helper does not invoke Functions itself.

## Provision accounts

Use [the installed Console harness](../../../docs/local-deployment.md#isolated-installation-and-console-checks)
for normal preparation, execution and cleanup. Its `prepare` command calls
`credentials` with a private manifest and the existing Default tenant ID. It
requires a fresh, unclaimed installation with no `account_users`; production
setup or a previous credential seed makes this guard fail. Use a fresh dedicated
database, rather than clearing account state to bypass it.

The helper creates personal accounts with Argon2id passwords and explicit
memberships matching the supported tenant API permission profiles. It marks the
fixture installation claimed in the same transaction. The private JSON contains
`username`, `password` and `userId` for the primary browser account and
`accountsByKey[rawKey] = { username, password, userId }` for the other supported
profiles. API keys remain available for direct HTTP probes. Group, platform,
revoked and expired key profiles are API-only. These test fixtures do not imply
that a production API key can sign in or prove account ownership.

The manifest must remain private (0600) in a private directory (0700). Do not
print its contents or record browser sessions. The credential command requires
PostgreSQL Pod exec permission for guarded owner SQL in addition to the cluster
checks above. It is separate from the normal
[account setup and invitation procedures](../../../docs/accounts.md).

## Provision Functions and wait

After credential preparation, the manifest supplies the primary tenant, group
and foreign-tenant IDs. The harness passes them automatically to `seed`.
The helper accepts the non-secret IDs separately:

```bash
export KUBECONFIG="$PWD/.runtime/console-kind/kubeconfig"
export ORBITJOB_CONSOLE_FIXTURE=1
go run ./deploy/kind/console-fixture seed \
  --tenant "$PRIMARY_TENANT_ID" \
  --group "$PRIMARY_GROUP_ID" \
  --foreign-tenant "$FOREIGN_TENANT_ID"
```

The image comes from the existing `deploy/kind/fixtures/console-jobs.json`
unless `--image` is provided. Load that image into this kind cluster before
invoking; the helper does not pull images, start a cluster, install Helm or
modify M3 fixtures.

The private ownership manifest defaults to
`.runtime/console-kind/m4-functions.json` (0600). It records the unique owner,
image and exact Function IDs. Names include an owner prefix. A repeated `seed`
uses the existing manifest instead of inserting another set; it can recover a
definition committed immediately before an interrupted manifest update by its
prewritten owner and case labels. Keep the manifest until cleanup completes.
Only one process may use it at a time. Remove a stale `.lock` only after
confirming no helper process is running.

| Manifest case | Definition and intended evidence |
| --- | --- |
| `success` | Exit zero after a short real Pod execution. |
| `failed` | Exit one; no platform retries. |
| `retry` | First attempt exits one, second exits zero; `retry_limit=1`. |
| `long` | Sleep for five minutes, with a longer timeout; use for real cancellation. |
| `paused` | Paused definition; invocation should return a conflict. |
| `grouped` | Active definition in the supplied primary resource group. |
| `foreign` | Active definition owned by the second tenant, for isolation checks. |

`seed` waits up to its bounded `--timeout` for the five active primary-tenant
definitions to acquire real active revisions in namespace `orbitjob`, generation
one. A paused Function is intentionally excluded from projection. The foreign
tenant may have no operator namespace mapping, so its definition supports
isolation checks without promising an executable revision. To execute it as its
own tenant, explicitly provision the corresponding namespace mapping first.

```bash
go run ./deploy/kind/console-fixture wait --timeout 2m
go run ./deploy/kind/console-fixture status
```

Output contains only fixture identities, observed active revision IDs,
namespaces/generations and the actual recorded terminal-outcome count. A missing
revision is a failure to observe readiness, never permission to insert one.
Check operator leadership, namespace mapping, sync logs and DB grants when the
wait expires. Only the operator runs Function sync; `cmd/scheduler` evaluates
SLOs and cannot substitute for it.

Use personal fixture accounts in the installed Console, or dedicated API keys
with the Function HTTP endpoints, to invoke. Keep one idempotency key per intent
and reuse it after ambiguous responses. Observe JobRun status, Kubernetes Jobs and terminal history. Never
insert a `function_runs` row to make an installed test pass. A history UUID is
not the ordinary numeric ledger Run ID.

## Retire and clean up

```bash
go run ./deploy/kind/console-fixture cleanup --timeout 2m
```

Cleanup verifies every manifest ID against its tenant, owner, case, name and
group before making changes. It then soft-deletes only those definitions to
prevent new invocations, records the owned JobRun UIDs/revisions, requests real
cancellation for unfinished owned JobRuns, and waits for observed terminal
states. It removes only execution Jobs/Pods and JobRuns with the recorded
ownership, using UID preconditions for object deletion. It refuses a mismatched
revision or an execution whose JobRun ownership cannot be verified. A timeout or
`CancelUnknown` keeps the cleanup incomplete; inspect and rerun it rather than
declaring cancellation successful.

Database revisions, ledger rows, terminal history and audit records are
retained as test evidence. Cleanup is retirement and execution-resource removal,
not a database purge. Function revisions participate in the ordinary retention
enumeration; retained runs remain subject to projection and log-protection
constraints. Remove the dedicated cluster when all installed checks are finished
to discard that database. The Function cleanup command removes no tenant, group,
key, M3 definition or unrelated run. The harness subsequently uses `revoke-credentials` to disable
owned fixture accounts, revoke their sessions and keys, and consume outstanding
capabilities while retaining their historical audit evidence. A cleaned manifest
cannot be seeded again; use a fresh manifest for another test session.

The executing Kubernetes identity needs node/namespace reads, this installation's
Secret read and PostgreSQL Pod port-forward; cleanup additionally needs JobRun
list/patch/delete, Job list/delete and Pod list in `orbitjob`. The installed admin
API needs JobRun create/get/patch, while the operator needs its mapped-namespace
execution grants and leader-election Lease access. Keep these separate from
the browser account membership's `function:List`, `function:Get` and
`function:Invoke` grants.
