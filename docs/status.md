# Project status

Implementation inventory checked against the current source on 2026-11-16. This page describes the repository's
current capabilities. Release availability is recorded in the
[Changelog](../CHANGELOG.md); test results and their scope are maintained in
[verification](verification.md).

## Current capabilities

| Area | Implemented behavior |
|---|---|
| Execution | ScheduledJob cron and manual execution, immutable definition revisions, retries, deadlines, cancellation, run and attempt history |
| Workflows | WorkflowJob DAGs, automatic and manual starts, dependency conditions, failure policies and cancellation fan-out |
| Functions | Versioned definition management, invocation, active revision synchronization and invocation history |
| Reliability | Container-based HTTP checks, supported status/duration SLI criteria, SLO windows, error budgets and burn alerts |
| Administration | Personal accounts, workspace memberships, revocable Console sessions, API keys, explicit policy grants, resource groups and authorized workspace discovery |
| Personal accounts | Invite-only accounts, password authentication, revocable browser sessions, explicit workspace memberships and installation-administrator recovery |
| Console | Same-origin management UI with guided task templates, Job runtime presets, configuration reuse, schedule generation and live draft explanations; guided Workflow authoring, Function management, execution history, retained logs and Administration |
| Execution logs | Bounded stdout/stderr and lifecycle evidence retained independently of Job, Pod and run-resource cleanup; authorized reading and audited deletion |
| Execution policy | Version 1 pins accepted CPU/memory/ephemeral-storage resources and requires retry-safety declaration before repeated execution |
| Interfaces | Admin HTTP API, generated OpenAPI, Kubernetes CRDs and CLI |
| Delivery | Helm installation with configurable Service exposure and optional same-origin Ingress, source builds and development-cluster tooling |
| Maintenance | Bounded tenant-scoped SLI replay-detail retirement, ledger-based execution-isolation inventory, and fault-recovery canaries with independent evidence checks |

The [architecture](architecture.md) defines component responsibilities and
trust boundaries. [Usage](../USAGE.md) describes supported operations;
[execution logs](execution-logs.md) defines retention and capacity behavior.

The [account guide](accounts.md) defines first-administrator setup, invitation,
password and session handling. Global password reset requires an installation
administrator; workspace administrators manage their own memberships and grants.
Installation-administrator status does not imply workspace workload access.

## Execution and isolation guarantees

Kubernetes runs containers; PostgreSQL records execution facts. The Admin API
writes management state and publishes Kubernetes intent. The Operator owns
execution-ledger updates. The Runtime database identity is trusted; application
policy, database RLS and namespace RBAC enforce separate access boundaries.

Durable creation intent and immutable Job identities protect recovery after
ambiguous external operations. Missing execution evidence remains nonterminal
`ExecutionUnknown`; it neither permits a replacement attempt nor frees a
`Forbid` admission slot. Cancellation completes only with sufficient owned
termination evidence. Terminal projections retry idempotently, and unacknowledged
outcomes remain protected from retention.

New workload Pods retain container exit evidence until it is persisted. The
Operator requires controller stop acknowledgement before accepting a complete
inventory, releases only its own evidence finalizer, then deletes the original
Job. Recovery and retention preserve unfinished evidence obligations, including
historical attempts. A deletion delay may recover after trustworthy proof;
a definite evidence gap remains isolated.

Operator singleton work uses client-go Lease sessions. Source admission uses
transaction-scoped PostgreSQL locks shared with revision and lifecycle updates.
Leadership coordinates work; it does not provide universal fencing across
PostgreSQL and Kubernetes. Job backoff and platform retries can repeat external
workload effects; workloads must provide idempotency where repeated effects are
unsafe. Execution can fail before application code runs, so the platform does
not guarantee that a business effect occurs.

## Compatibility and operations

Use the chart and environment examples for configuration, the migration files
for database schema, and OpenAPI for HTTP contracts. The
[installation guide](setup/installation.md) and [database guide](database-setup.md)
define installation, credentials and migration procedures.

Personal Console accounts require migration 0027 and its matching Admin image.
Operators issue single-use setup or recovery links; ordinary users join through
workspace invitations and sign in with their own accounts. Remembered sessions
expire after seven days. Existing API keys retain their automation semantics and
do not become personal accounts. See [account setup and access](accounts.md).

The installed Console follows the installation's IP or domain and port. Optional
Ingress routes the Console, API and schema through one origin and can reference
an existing TLS Secret. The chart does not install an ingress controller or
provision certificates. See [installation access](setup/installation.md)
for external access and the [verification record](verification.md) for tested
origin and upgrade scenarios.

Older execution writers must stop before the recovery and resource-identity
migration cutover. Preserve recorded migration checksums and the installation's
migration input channel. The database and log archive require durable storage;
resource cleanup must preserve their data unless deletion is explicitly intended.
Migration 0027 introduces personal accounts. A working installation's account,
network-policy and storage changes require its own maintenance and verification;
this source inventory does not establish deployment of those changes. Follow the
[maintenance procedure](local-maintenance.md).

## Remaining limitations

Unknown execution can retain a concurrency slot indefinitely when termination
cannot be proven. SLI retirement preserves incomplete plans and queryable missing
evidence; it covers tenants still assigned to the Operator. Missing timestamps can
prevent automatic retirement.
Schedule-adherence SLI derivation, general workflow dataflow and cross-cluster
scheduling are outside the implemented scope.

The documented installation profile and controlled recovery tests do not define
production throughput or HA qualification. See [known issues](known-issues.md)
for operational constraints and [verification](verification.md) for the tested
behaviors, environments and outstanding evidence.
