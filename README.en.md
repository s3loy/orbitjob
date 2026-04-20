# OrbitJob

[![License](https://img.shields.io/github/license/s3loy/orbitjob)](./LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/s3loy/orbitjob)](https://goreportcard.com/report/github.com/s3loy/orbitjob)
[![Build Status](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml)
[![Coverage Status](https://codecov.io/gh/s3loy/orbitjob/graph/badge.svg)](https://codecov.io/gh/s3loy/orbitjob)

[中文](./README.md)

OrbitJob is a job scheduling system built with Go and PostgreSQL, following a context-first modular monolith architecture. The current implementation covers the control plane (full CRUD and lifecycle management for job definitions), the scheduler (batched due-job scheduling with misfire policies), and the dispatcher (atomic instance claiming, concurrency policies, priority aging, and lease recovery).

## Project Status

| Area | Status | Notes |
| --- | --- | --- |
| Control plane HTTP API | Implemented | `create / list / get / update / pause / resume` |
| Job domain validation | Implemented | trigger, status, retry, concurrency, and misfire rules under `internal/core/domain/job` |
| Write-side persistence | Implemented | PostgreSQL + optimistic locking + audit |
| Read-side query | Implemented | list and detail queries under `internal/admin/store/postgres` |
| Execution foundation domain | Implemented | `internal/core/domain/instance` and `internal/core/domain/worker` |
| Execution foundation persistence | Implemented | instance create/claim + worker heartbeat upsert |
| Scheduler runtime | Implemented | `cmd/scheduler` + bounded batch tick + misfire policy + atomic scheduling tx |
| Dispatcher runtime | Implemented | `cmd/dispatcher` + atomic claim (`FOR UPDATE SKIP LOCKED`) + concurrency policy + priority aging + lease expiry recovery + graceful shutdown |
| Worker executor | Not yet implemented | Worker executor and result write-back loop |
| Manual trigger API | Not yet implemented | On-demand job trigger endpoint |
| Instance query API | Not yet implemented | Instance listing and detail endpoint |

## Architecture

```mermaid
flowchart LR
    Client["Client / Automation"] --> Router["Gin Router<br/>/healthz<br/>/metrics<br/>/api/v1/jobs"]

    subgraph AdminAPI["cmd/admin-api + internal/admin/http"]
        Router --> QueryUC["Query use cases<br/>list / get"]
        Router --> CommandUC["Command use cases<br/>create / update / pause / resume"]
    end

    subgraph Storage["Persistence"]
        ReadRepo["admin/store/postgres<br/>read model"] --> DB[("PostgreSQL")]
        WriteRepo["core/store/postgres<br/>write model + audit"] --> DB
    end

    QueryUC --> ReadRepo
    CommandUC --> Domain["core/domain/job<br/>validation + status transition"]
    CommandUC --> WriteRepo

    Scheduler["cmd/scheduler"] --> ScheduleUC["core/app/schedule<br/>policy + tick"]
    ScheduleUC --> SchedulerRepo["core/store/postgres<br/>SchedulerRepository"]
    SchedulerRepo --> DB

    Dispatcher["cmd/dispatcher"] --> DispatchUC["core/app/dispatch<br/>tick use case"]
    DispatchUC --> DispatchRepo["core/store/postgres<br/>DispatchRepository"]
    DispatchRepo --> DB
```

Job lifecycle and state transitions are documented in [docs/job-lifecycle.en.md](./docs/job-lifecycle.en.md).

Execution-plane contracts and scope are documented in [docs/execution-plane.en.md](./docs/execution-plane.en.md).

## HTTP API

### Routes

| Method | Path | Function | Input | Notes |
| --- | --- | --- | --- | --- |
| `GET` | `/healthz` | Health check | none | Returns service liveness |
| `GET` | `/openapi.json` | OpenAPI document | none | Live code-generated API contract |
| `GET` | `/metrics` | Prometheus metrics | none | Exposes metrics handler |
| `POST` | `/api/v1/jobs` | Create a job | JSON body | Mutating endpoint |
| `GET` | `/api/v1/jobs` | List jobs | Query: `tenant_id`, `status`, `limit`, `offset` | `status` accepts `active` / `paused` only |
| `GET` | `/api/v1/jobs/:id` | Get a job | Path: `id`; Query: `tenant_id` | `id >= 1` |
| `PUT` | `/api/v1/jobs/:id` | Update job configuration | Path: `id`; Query: `tenant_id`; JSON body | Merge-style update; requires `X-Actor-ID` |
| `POST` | `/api/v1/jobs/:id/pause` | Pause a job | Path: `id`; Query: `tenant_id`; JSON body: `version` | Requires `X-Actor-ID` |
| `POST` | `/api/v1/jobs/:id/resume` | Resume a job | Path: `id`; Query: `tenant_id`; JSON body: `version` | Requires `X-Actor-ID` |

### Mutating Request Conventions

| Item | Notes |
| --- | --- |
| `X-Actor-ID` | Required for mutating endpoints; recorded in audit trail |
| `X-Trace-ID` | Optional; server generates one when omitted and returns it in the response header |
| `version` | Required for update, pause, and resume; used for optimistic locking |
| Error mapping | Validation errors return `400`; missing resources return `404`; version conflicts return `409`; unexpected errors return `500` |

### Update Semantics

`PUT /api/v1/jobs/:id` is implemented as a merge-style update:

| Rule | Notes |
| --- | --- |
| Unspecified fields | Retain the current job values |
| Specified fields | Overwrite the current job values |
| `cron -> manual` | When switching to `manual` without explicitly providing `cron_expr`, the existing cron expression is cleared |
| Persistence write | Uses `jobs.version` for optimistic locking |

### Core Field Conventions

| Field | Values |
| --- | --- |
| `trigger_type` | `cron` / `manual` |
| `status` | `active` / `paused` |
| `retry_backoff_strategy` | `fixed` / `exponential` |
| `concurrency_policy` | `allow` / `forbid` / `replace` |
| `misfire_policy` | `skip` / `fire_now` / `catch_up` |

## Development

### Prerequisites

- Go 1.26+
- PostgreSQL

### Processes

OrbitJob consists of multiple independent processes, each serving a distinct responsibility:

| Process | Entrypoint | Description |
| --- | --- | --- |
| Admin API | `cmd/admin-api` | Control plane HTTP service for job CRUD and lifecycle management |
| Scheduler | `cmd/scheduler` | Periodically scans for due jobs and creates instances (batch tick loop) |
| Dispatcher | `cmd/dispatcher` | Atomically claims pending instances and assigns them to workers (`FOR UPDATE SKIP LOCKED`) |
| OpenAPI Gen | `cmd/openapi-gen` | OpenAPI contract generation and drift detection tool |

### Environment Variables

The `.env.example` file contains:

```bash
DATABASE_DSN=postgres://postgres:password@127.0.0.1:5432/orbitjob?sslmode=disable
TEST_DATABASE_DSN=postgres://postgres:password@127.0.0.1:5432/orbitjob_test?sslmode=disable
```

Common variables:

| Variable | Purpose | Default |
| --- | --- | --- |
| `DATABASE_DSN` | Database connection string for `cmd/admin-api` | -- |
| `TEST_DATABASE_DSN` | Database connection string for integration tests | -- |
| `APP_ENV` | Runtime and logging environment identifier | -- |

Scheduler variables:

| Variable | Purpose | Default |
| --- | --- | --- |
| `SCHEDULER_BATCH_SIZE` | Maximum due jobs per scheduler tick | `100` |
| `SCHEDULER_TICK_INTERVAL_SEC` | Scheduler tick interval in seconds | `5` |

Dispatcher variables:

| Variable | Purpose | Default |
| --- | --- | --- |
| `DISPATCHER_WORKER_ID` | Target worker identifier (required) | -- |
| `DISPATCHER_TENANT_ID` | Tenant identifier | `default` |
| `DISPATCHER_BATCH_SIZE` | Maximum instances claimed per tick | `50` |
| `DISPATCHER_TICK_INTERVAL_SEC` | Tick interval in seconds | `2` |
| `DISPATCHER_LEASE_DURATION_SEC` | Lease duration in seconds | `30` |

### Running

Start the Admin API:

```bash
go run ./cmd/admin-api
```

Start the Scheduler:

```bash
go run ./cmd/scheduler
```

Start the Dispatcher:

```bash
DISPATCHER_WORKER_ID=worker-1 go run ./cmd/dispatcher
```

### OpenAPI (Code-first)

Committed contract file: `api/openapi.yaml`

Regenerate:

```bash
go run ./cmd/openapi-gen -out api/openapi.yaml
```

Check for drift (same command used in CI):

```bash
go run ./cmd/openapi-gen -check -out api/openapi.yaml
```

### Testing

Unit tests:

```bash
go test ./...
```

Integration tests:

```bash
go test -tags integration ./internal/platform/postgrestest
go test -tags integration ./internal/admin/store/postgres ./internal/core/store/postgres
```

## Repository Layout

| Path | Purpose |
| --- | --- |
| `cmd/admin-api` | Control plane service entrypoint, middleware, router wiring |
| `cmd/scheduler` | Scheduler entrypoint (bounded batch tick + misfire policy) |
| `cmd/dispatcher` | Dispatcher entrypoint (claim + dispatch + graceful shutdown) |
| `cmd/openapi-gen` | OpenAPI code-first generation and drift detection tool |
| `internal/admin/http` | HTTP handlers, request binding, error mapping |
| `internal/admin/app/job` | Control-plane query and command use cases |
| `internal/admin/store/postgres` | Read-side PostgreSQL repository |
| `internal/core/domain` | Job / instance / worker domain models and validation |
| `internal/core/app/schedule` | Scheduler tick use case (policy + tick) |
| `internal/core/app/dispatch` | Dispatcher tick use case |
| `internal/core/store/postgres` | Write-side repositories (job / instance / worker / scheduler / dispatch) |
| `internal/domain` | Shared validation and resource errors |
| `internal/platform` | Config, logger, metrics, and test helpers |
| `db/migrations` | PostgreSQL schema, constraints, and triggers |

## Documentation

| Path | Purpose |
| --- | --- |
| [`README.md`](./README.md) | Chinese overview and developer reference |
| [`README.en.md`](./README.en.md) | English overview |
| [`docs/job-lifecycle.md`](./docs/job-lifecycle.md) | Job lifecycle and endpoint rules (Chinese) |
| [`docs/job-lifecycle.en.md`](./docs/job-lifecycle.en.md) | Job lifecycle and endpoint rules |
| [`docs/execution-plane.md`](./docs/execution-plane.md) | Execution-plane contract and foundation semantics (Chinese) |
| [`docs/execution-plane.en.md`](./docs/execution-plane.en.md) | Execution-plane contract and foundation semantics |

## License

See [LICENSE](./LICENSE).
