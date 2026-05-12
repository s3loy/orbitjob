# OrbitJob

[![License](https://img.shields.io/github/license/s3loy/orbitjob)](./LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/s3loy/orbitjob)](https://goreportcard.com/report/github.com/s3loy/orbitjob)
[![Build Status](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml)
[![Coverage Status](https://codecov.io/gh/s3loy/orbitjob/graph/badge.svg)](https://codecov.io/gh/s3loy/orbitjob)

[中文](./README.md)

![Stone Badge](https://stone.professorlee.work/api/stone/s3loy/orbitjob)

A Go job scheduling library. PostgreSQL is the only external dependency.

Embed as a library, or deploy as standalone services.

## Quick Start

### Docker Compose

```bash
docker compose up -d
curl http://localhost:8080/healthz
```

### API surface

- `POST /api/v1/jobs` create a job
- `POST /api/v1/jobs/:id/trigger` manual trigger, requires `X-OrbitJob-Idempotency-Key`
- `POST /api/v1/jobs/:id/pause` pause a job
- `POST /api/v1/jobs/:id/resume` resume a job
- `DELETE /api/v1/jobs/:id` delete a job
- `GET /api/v1/instances` list instances
- `GET /api/v1/instances/:run_id` inspect one instance
- `POST /api/v1/instances/:run_id/cancel` cancel an instance
- `/openapi.json` machine-readable API contract
- `/healthz` liveness check (all components)
- `/readyz` readiness check (includes DB ping; scheduler :6060, dispatcher :6061, worker :6062)
- `/metrics` Prometheus metrics endpoint

### From source

```bash
# 1. Start PostgreSQL and the API server
DATABASE_DSN="postgres://user:<YOUR_PASSWORD>@localhost:5432/orbitjob?sslmode=disable" \
  go run ./cmd/admin-api

# 2. Create a job
curl -X POST http://localhost:8080/api/v1/jobs \
  -H "Content-Type: application/json" \
  -H "X-Actor-ID: admin" \
  -d '{"name":"hello-world","trigger_type":"manual"}'

# 3. Start background components (WORKER_ID is optional — auto-generated)
go run ./cmd/scheduler &
go run ./cmd/dispatcher &
go run ./cmd/worker &
```

All-in-one dev mode:

```bash
go run ./cmd/devserver
```

### Production

See `deploy/` for systemd units, env templates, and deployment scripts.

```bash
make build-all
DATABASE_URL="postgres://..." make migrate-up
sudo cp deploy/systemd/*.service /etc/systemd/system/
sudo systemctl enable --now orbitjob-*
```

## Development

### Requirements

Go 1.26+, PostgreSQL 17

### Running

```bash
go run ./cmd/admin-api       # API server
go run ./cmd/scheduler       # Scheduler
go run ./cmd/dispatcher      # Dispatcher
go run ./cmd/worker          # Worker (WORKER_ID optional — auto-generated)
go run ./cmd/devserver       # All-in-one dev mode
go run ./cmd/openapi-gen     # OpenAPI generation
```

### Environment Variables

| Variable | Purpose | Default |
| --- | --- | --- |
| `DATABASE_DSN` | Database connection string | — |
| `ADMIN_DSN` | Admin API DSN (overrides DATABASE_DSN) | — |
| `SCHEDULER_DSN` | Scheduler DSN | — |
| `DISPATCHER_DSN` | Dispatcher DSN | — |
| `WORKER_DSN` | Worker DSN | — |
| `DEV_DSN` | Devserver DSN | — |
| `TEST_DATABASE_DSN` | Integration test DSN | — |
| `APP_ENV` | Log mode (development / production) | — |
| `ADMIN_PORT` | API listen port for `cmd/devserver` | `8080` |
| `PORT` | HTTP listen port for `cmd/admin-api` | `8080` |
| `SCHEDULER_HEALTH_PORT` | Health HTTP port | `6060` |
| `SCHEDULER_BATCH_SIZE` | Max jobs per tick | `100` |
| `SCHEDULER_TICK_INTERVAL_SEC` | Tick interval (seconds) | `5` |
| `DISPATCHER_TENANT_ID` | Dispatcher tenant scope | `default` |
| `DISPATCHER_HEALTH_PORT` | Health HTTP port | `6061` |
| `DISPATCHER_BATCH_SIZE` | Max claims per tick | `50` |
| `DISPATCHER_TICK_INTERVAL_SEC` | Tick interval (seconds) | `2` |
| `DISPATCHER_LEASE_DURATION_SEC` | Lease duration (seconds) | `30` |
| `WORKER_ID` | Worker identifier | {hostname}-{uuid8} |
| `WORKER_TENANT_ID` | Worker tenant scope | `default` |
| `WORKER_HEALTH_PORT` | Health HTTP port | `6062` |
| `WORKER_POLL_INTERVAL_SEC` | Poll interval (seconds) | `2` |
| `WORKER_HEARTBEAT_INTERVAL_SEC` | Heartbeat interval (seconds) | `10` |
| `WORKER_LEASE_DURATION_SEC` | Lease duration (seconds) | `60` |
| `WORKER_CAPACITY` | Max concurrent executions | `1` |
| `WORKER_LABELS` | Worker labels (JSON) | `{}` |
| `RATELIMIT_READ_RPS` | Read endpoint rate limit (RPS) | `100` |
| `RATELIMIT_WRITE_RPS` | Write endpoint rate limit (RPS) | `10` |
| `RATELIMIT_TRIGGER_RPS` | Trigger endpoint rate limit (RPS) | `5` |
| `RATELIMIT_ADMIN_RPS` | Admin endpoint rate limit (RPS) | `5` |

### Testing

```bash
make test                  # Unit tests
make test-cover            # With coverage report
make test-race             # Race detector
make integration           # Integration tests (requires PostgreSQL)
make lint                  # golangci-lint
make check                 # All quality gates (lint + vet + race + openapi + tidy)
```

## License

[BSD 3-Clause](./LICENSE)
