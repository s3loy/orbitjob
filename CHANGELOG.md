# Changelog

All notable changes to OrbitJob are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## [Unreleased]

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
  execution modes, Docker Compose deployment) — recorded in
  `docs/superpowers/adr/0003-two-execution-modes-and-ownership.md`.

### Fixed

- User workload Pods no longer receive a projected Kubernetes service-account
  token, and the operator's Kubernetes permissions are limited to the tenant
  namespaces it actually watches.
- Manual releases now resolve an existing tag to one immutable commit; all
  artifacts use that commit, and moving-image aliases update only after every
  versioned image and the Helm chart have succeeded.
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
