# Changelog

All notable changes to OrbitJob are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## [Unreleased]

Changes prepared for 0.3.0. See [project status](docs/status.md) for
implementation scope and [verification](docs/verification.md) for test results.

### Added

- Same-origin Console for workload authoring, execution inspection, reliability
  monitoring and administration of workspaces, policies and API keys.
- Typed Job and Workflow definition validation/apply with immutable identity,
  version checks and request reconciliation.
- Versioned Function creation, editing, pause/resume and revision-pinned invocation.
- Timezone-aware automatic Workflow scheduling with durable occurrence records
  and concurrency admission shared with manual starts.
- Durable execution logs with bounded output, protected retention, cursor reads,
  downloads, visible collection gaps and independently authorized cleanup.

### Changed

- Reorganize development documentation into complete English and reviewed
  Simplified Chinese tasks, tutorials, concepts and references, with structured
  routes, localized navigation and search, and same-page language switching.
  Keep commands and contract data consistent and reject unreviewed input changes.

- Release and pre-release publication require backend, PostgreSQL, vulnerability,
  Console and installed-cluster verification against the selected source revision.
- Upgrading execution recovery requires a maintenance window: stop older writers,
  preserve the existing migration input channel, apply forward migrations and
  start the new components. See [migration requirements](docs/database-setup.md#migration-execution-and-compatibility).
- Database and execution archives require separate durable volumes. Preserve both
  in a coordinated backup and restore; see [execution logs](docs/execution-logs.md).

### Fixed

- Require Go 1.27.4 and pin the Docker builder to the same patch version to
  resolve the standard-library HTTP/1 header race reported as GO-2026-6859.
- Provide English and Chinese recovery in the shared static-host 404 page,
  remove nonexistent language metadata targets and validate same-site absolute
  links alongside relative links.
- Validate the same member grants that are persisted, and bind JSON API writes
  to the tenant authorized for the request regardless of Content-Type.
- Preserve verified historical Function invocation addresses during replay and
  retain the last observed phase when waiting ends without a newer observation.
  Finish Workflow cancellation without requiring unused duration operands.
- Include collection changes and gaps from every Console log export page and
  label unavailable archives as incomplete downloads.
- Refresh newly created workspace memberships, isolate unconfirmed controls by
  authenticated subject, accept zero Check retries and manage API keys in an
  independently selected authorized workspace.
- Publish the admission image in pre-releases and restrict webhook matching to
  the installation's mapped execution namespaces.
- Report failed control-plane recovery during local volume maintenance and
  retire interrupted fixture credentials and paginated workload definitions.
- Clarify volatile authoring drafts and Workflow reference-readiness checks in
  the maintained English and Chinese guides.
- Resolve Console development dependency advisories in minimatch and fast-uri.
- Preserve log copy, download and cleanup feedback during archive polling while
  clearing reconnection progress when reads recover.
- Correct generated account request constraints and authorization descriptions,
  archive pagination defaults, Workflow creation guidance, cancellation evidence
  and Function invocation timing; align the English and Chinese references.
- Admit execution archives on filesystems that do not report inode counts while
  preserving byte budgets, free-space reserves and finite-inode limits.
- Preserve complete namespaced definition identities in audit records with
  migration 0028, removing the former 64-character storage limit.
- Advance stale Check plans rejected before revision activation, with conditional
  cursor updates that preserve concurrent version and pause changes.
- Preserve the selected management workspace on Console API-key rename and
  revocation requests without changing the workload connection.
- Enforce tenant and resource-group boundaries during request binding, derived
  reads, policy delegation and API-key revocation. Preserve the actual actor in
  audit records and reject ambiguous permission intersections.
- Retry bootstrap credential delivery after a committed key without replacing
  existing credentials or accepting conflicting Secret/file contents.
- Enforce append-only audit permissions, same-tenant references and scoped
  reliability-source validation through forward migrations.
- Preserve uncertain execution creation and immutable Kubernetes identities.
  Missing bound Jobs remain `ExecutionUnknown`; cancellation requires owned
  termination evidence, and stopped Workflows cannot start further work.
- Retain new workload Pods until verified termination evidence is persisted,
  release only OrbitJob's evidence finalizer, and isolate missing evidence
  atomically. Cancellation waits for controller stop acknowledgement before
  collecting proof and deleting the original Job; its confirmation window
  remains bounded across Operator restarts.
- Serialize scheduled, manual and Workflow-task admission for the same source.
  Client-go Lease sessions stop singleton work after leadership loss and support
  reacquisition under a new term.
- Recover pending terminal outcomes without duplicate SLI increments, including
  fair scans past failed batches and the documented historical cutover.
- Correct Function, Check and Workflow recovery and retention, probe transport
  handling, supported SLI criteria and zero-budget SLO calculations.
- Preserve Console drafts and uncertain operation results across navigation,
  and keep monitoring searches and Administration refresh behavior consistent.
- Reject incomplete or contradictory load, benchmark and integration evidence.
  Check scheduler database readiness, rendered credentials, generated contracts,
  image-build inputs and dependency consistency.

### Deprecated

- `bootstrap.secretName` is retained as an ignored chart value. Remove custom
  overrides; bootstrap delivery uses the fixed `bootstrap-api-key` Secret.

## [0.2.1] - 2026-09-21

### Added

- Workflow execution: `WorkflowJob`/`WorkflowRun` custom resources, a DAG
  walker with dependency and conditional task gating, fail policies and
  cancel fan-out (`internal/operator/workflow.go`,
  `internal/core/domain/workflow/`, migration 0005).
- Functions (the Serverless surface): HTTP-invoked one-shot runs.
  `POST /api/v1/functions/{id}/invoke` with run reads under
  `/functions/{id}/runs`; definitions sync to immutable revisions on an
  operator loop (`internal/core/app/functionsync/`, migrations 0004 and
  0006).
- Pre-release image workflow: branch builds pushed to GHCR as
  `rc-<run>-<sha>` tags with a full-success `rc` alias
  (`.github/workflows/prerelease.yml`).

### Removed

- The legacy in-process pipeline (dispatcher and worker processes, tenant
  execution modes and Docker Compose deployment).

### Fixed

- User workload Pods no longer receive a projected Kubernetes service-account
  token, and the operator's Kubernetes permissions are limited to the tenant
  namespaces it actually watches.
- Formal releases require a new tag on the current `main` commit and refuse to
  overwrite any existing versioned image tag; mutable aliases are not
  published.
- Attempt observations require the immutable Kubernetes Job UID recorded when
  the attempt was created, so a replacement Job cannot mutate another
  workload's ledger entry.
- Load-test manifest generation now enforces the selected profile before
  writing output.
- The operator no longer loses its apiserver watches to silent connection
  stalls: the shared client carries TCP keepalive and HTTP/2 read-idle health
  checks, reconciles read their objects from the informer store under a
  per-pass budget instead of one throttled apiserver read per event, and the
  client states its rate (50 QPS, burst 150) instead of client-go's silent
  default of five (`internal/operator/factory.go`,
  `internal/operator/controller.go`, `internal/operator/dynamic_handler.go`).
- The smoke test's SLI payload names the `job_run` source the ledger cutover
  kept, not the retired `check_run` (`scripts/smoke-test.sh`).
