# OrbitJob

[![License](https://img.shields.io/github/license/s3loy/orbitjob)](./LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/s3loy/orbitjob)](https://goreportcard.com/report/github.com/s3loy/orbitjob)
[![Build Status](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml)
[![Coverage Status](https://codecov.io/gh/s3loy/orbitjob/graph/badge.svg)](https://codecov.io/gh/s3loy/orbitjob)

[中文](./README.md)

A background job scheduler for Go applications.
PostgreSQL is the only dependency.

Use it as a library in your Go app, or deploy as a standalone service.

## Quick Start

```bash
# 1. Start PostgreSQL, then start the API server
DATABASE_DSN="postgres://postgres:password@127.0.0.1:5432/orbitjob?sslmode=disable" \
  go run ./cmd/admin-api

# 2. Create your first job
curl -X POST http://localhost:8080/api/v1/jobs \
  -H "Content-Type: application/json" \
  -d '{"name":"hello-world","trigger_type":"manual"}'

# 3. Start the background components
go run ./cmd/scheduler &
DISPATCHER_WORKER_ID=worker-1 go run ./cmd/dispatcher &
WORKER_ID=worker-1 go run ./cmd/worker &
```

## Overview

OrbitJob handles reliable single-task scheduling — cron triggering, dispatching, execution, retry, and result tracking.

- **scheduler** — generates instances from cron expressions
- **dispatcher** — advances instance state, recovers orphans, enforces concurrency policy
- **worker** — claims and executes tasks autonomously (HTTP callback or subprocess)
- **admin-api** — REST API for job management and execution visibility

Components communicate through PostgreSQL — no direct RPC between them.

## API

RESTful HTTP with auto-generated OpenAPI spec:

| Method | Path | Description |
| --- | --- | --- |
| `POST` | `/api/v1/jobs` | Create job |
| `GET` | `/api/v1/jobs` | List jobs |
| `GET` | `/api/v1/jobs/:id` | Get job detail |
| `PUT` | `/api/v1/jobs/:id` | Update job |
| `POST` | `/api/v1/jobs/:id/pause` | Pause job |
| `POST` | `/api/v1/jobs/:id/resume` | Resume job |

Mutation endpoints require `X-Actor-ID` header. Updates use optimistic locking via `version`. Full contract at [`api/openapi.yaml`](./api/openapi.yaml) or `/openapi.json` on a running instance.

## Development

### Requirements

Go 1.26+, PostgreSQL.

### Running

```bash
# Admin API
go run ./cmd/admin-api

# Scheduler
go run ./cmd/scheduler

# Dispatcher (DISPATCHER_WORKER_ID will be deprecated in Mode B)
DISPATCHER_WORKER_ID=worker-1 go run ./cmd/dispatcher

# Worker
WORKER_ID=worker-1 go run ./cmd/worker
```

### Environment Variables

| Variable | Purpose | Default |
| --- | --- | --- |
| `DATABASE_DSN` | Database connection string | -- |
| `TEST_DATABASE_DSN` | Integration test connection string | -- |
| `APP_ENV` | Log environment (development/production) | -- |
| `SCHEDULER_BATCH_SIZE` | Max jobs per tick | `100` |
| `SCHEDULER_TICK_INTERVAL_SEC` | Tick interval in seconds | `5` |
| `DISPATCHER_BATCH_SIZE` | Max claims per tick | `50` |
| `DISPATCHER_TICK_INTERVAL_SEC` | Tick interval in seconds | `2` |
| `DISPATCHER_LEASE_DURATION_SEC` | Lease duration in seconds | `30` |
| `WORKER_POLL_INTERVAL_SEC` | Poll interval in seconds | `2` |
| `WORKER_CAPACITY` | Max concurrent executions | `10` |

### Testing

```bash
go test ./...                                                        # Unit tests
go test -tags integration ./internal/platform/postgrestest            # Integration tests
go test -tags integration ./internal/admin/store/postgres ./internal/core/store/postgres
golangci-lint run                                                    # Lint
go run ./cmd/openapi-gen -check -out api/openapi.yaml                # OpenAPI drift check
```

## Repository Structure

| Path | Description |
| --- | --- |
| `cmd/admin-api` | Control plane HTTP service |
| `cmd/scheduler` | Scheduler |
| `cmd/dispatcher` | Dispatcher |
| `cmd/worker` | Worker executor |
| `cmd/openapi-gen` | OpenAPI generation and drift check |
| `internal/admin/` | HTTP handlers, use cases, read-side store |
| `internal/core/` | Domain rules, use cases, write-side store |
| `internal/platform/` | Config, logger, metrics, test helpers |
| `db/migrations/` | PostgreSQL schema |

## Project Status

| Area | Status |
| --- | --- |
| Control plane HTTP API | ✅ |
| Scheduler + Dispatcher runtime | ✅ |
| Worker executor MVP | ✅ |
| Multi-tenant RLS security model | 🔧 In design |
| Instance query / Manual trigger API | 🔧 Planned |

## Docs

| Document | Description |
| --- | --- |
| [`docs/job-lifecycle.md`](./docs/job-lifecycle.md) | Job state machine |
| [`docs/execution-plane.md`](./docs/execution-plane.md) | Execution plane contract |
| [`./CONTRIBUTING.md`](./CONTRIBUTING.md) | Contributing guide |
| [`./SECURITY.md`](./SECURITY.md) | Security policy |

## License

[BSD 3-Clause](./LICENSE)
