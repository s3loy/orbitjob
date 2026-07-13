# OrbitJob

[![Go](https://img.shields.io/badge/Go-1.26.5-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/github/license/s3loy/orbitjob)](./LICENSE)
[![golangci-lint](https://github.com/s3loy/orbitjob/actions/workflows/lint.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/lint.yml)
[![Build Status](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml)
[![govulncheck](https://github.com/s3loy/orbitjob/actions/workflows/govulncheck.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/govulncheck.yml)
[![Coverage Status](https://codecov.io/gh/s3loy/orbitjob/graph/badge.svg)](https://codecov.io/gh/s3loy/orbitjob)

[中文](./README.md)

![Stone Badge](https://stone.professorlee.work/api/stone/s3loy/orbitjob)

OrbitJob is a Kubernetes-first distributed job scheduler. PostgreSQL stores transactional execution state. Four processes—`admin-api`, `scheduler`, `dispatcher`, and `worker`—handle the control plane, triggering, dispatch, and execution.

Good fit: multi-tenant cron jobs, API-triggered jobs, Kubernetes Job execution, HTTP checks, and SLO calculation.

Poor fit: millisecond-latency scheduling, exactly-once execution, or replacing a durable message queue with PostgreSQL `NOTIFY`. OrbitJob uses at-least-once Claim/Lease semantics, so handlers must be idempotent.

## Available today

- Cron and manual triggers; fixed/exponential retry; misfire and concurrency policies
- `exec`, `http`, `webhook`, `pg_notify`, and `container` handlers
- Kubernetes Job execution with digest checks, RBAC, and a restricted Pod security context
- API-key authentication, tenant isolation, PostgreSQL roles, RLS, and scoped `SECURITY DEFINER` entry points
- Checksum-aware migration runner, advisory locking, a formal v0.2.0 baseline, and pre-release database reset enforcement
- Checks, CheckRuns, SLIs, SLOs, error budgets, and burn-rate alerts
- Docker Compose, a Helm Chart, and kind install/upgrade verification
- Prometheus metrics, Grafana dashboards, structured logs, and trace IDs
- In-process and etcd election/discovery

Not delivered yet: Operator, CRDs, Kubernetes Lease, PostgreSQL epoch fencing, Workflow, and Serverless. Helm currently installs regular Kubernetes workloads; CRD specs are not yet the declaration authority.

## Design choices

- PostgreSQL is the persistent state source. Transactions, `FOR UPDATE SKIP LOCKED`, and optimistic `version` checks protect state transitions.
- Multiple schedulers and workers can claim rows concurrently, but throughput remains bounded by PostgreSQL connections and hot rows.
- Runtime processes use separate admin/runtime database roles. Pre-authentication lookup and cross-tenant scheduling go through narrowly granted database functions.
- The container handler creates `batch/v1` Jobs. Pods run as UID 65534, disable privilege escalation, drop all capabilities, and use a read-only root filesystem.
- Kubernetes Lease is not implemented. Use etcd when a deployment needs cross-process election or discovery.

## Docker Compose quick start

You need Docker Compose v2 and `make`:

> **v0.2.0 database reset requirement**
>
> v0.2.0 squashes the pre-release `0001`–`0011` migration history into one formal baseline. Existing development databases and named volumes cannot be upgraded in place. Before starting this version for the first time, run `make docker-reset`, then run `make docker-up`.
>
> Old development histories are rejected with:
>
> ```text
> unsupported pre-release schema history; recreate the database for v0.2.0
> ```
>
> Do not edit `schema_migrations` or insert a fabricated baseline row.

```bash
make docker-up
```

The command generates random `.env` credentials, initializes database roles, runs migrations and bootstrap, then waits for the runtime, Prometheus, and Grafana. Do not copy the empty placeholders from `.env.example` into `.env`.

```bash
export ORBITJOB_API_KEY="$(make --no-print-directory bootstrap-key)"
curl -fsS http://localhost:8080/healthz
curl -fsS \
  -H "Authorization: Bearer $ORBITJOB_API_KEY" \
  http://localhost:8080/api/v1/tenants
```

Local endpoints:

| Service | URL |
|---|---|
| Admin API | <http://localhost:8080> |
| OpenAPI | <http://localhost:8080/openapi.json> |
| Prometheus | <http://localhost:9090> |
| Grafana | <http://localhost:3000> |

```bash
make docker-status
make grafana-password
make docker-down          # preserve data
make docker-reset         # delete volumes after confirmation
```

Enable optional services:

```bash
docker compose --profile coordination-etcd up -d
docker compose --profile logs up -d
```

Starting the etcd container does not switch the runtime automatically. Set `ETCD_ENABLED=true` and `ETCD_ENDPOINTS` as well.

## Helm and Kubernetes

The Chart is version `0.2.0` and lives under `charts/orbitjob`. It installs owner-init, migration, bootstrap, all four runtime processes, RBAC, and a task namespace for container Jobs. It does not install PostgreSQL.

```bash
make helm-check

helm upgrade --install orbitjob charts/orbitjob \
  --namespace orbitjob-system \
  --create-namespace \
  -f values.production.yaml
```

Recommended production setting:

```yaml
worker:
  containerExecution:
    requireDigest: true
```

This rejects workload images that use a mutable tag without an `@sha256:` digest.

Run the local kind verification:

```bash
make kind-up
make kind-v020-verify
make kind-down
```

The script covers first install, repeated upgrades, and migration-failure blocking.

## API example

```bash
export ORBITJOB_API=http://localhost:8080
export AUTH="Authorization: Bearer $ORBITJOB_API_KEY"

curl -sS -X POST "$ORBITJOB_API/api/v1/jobs" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{
    "name":"daily-report",
    "trigger_type":"cron",
    "cron_expr":"0 9 * * *",
    "timezone":"UTC",
    "handler_type":"http",
    "handler_payload":{
      "url":"https://example.com/report",
      "method":"POST",
      "body":"{\"source\":\"orbitjob\"}"
    },
    "timeout_sec":30,
    "retry_limit":3,
    "concurrency_policy":"forbid"
  }'
```

The Admin API uses one error envelope:

```json
{"error":{"code":"VALIDATION_ERROR","message":"is required","field":"name"}}
```

See [USAGE.md](./USAGE.md) for curl examples, handler payloads, the Helm Secret contract, and troubleshooting. See [api/openapi.yaml](./api/openapi.yaml) for request and response schemas.

## Development and verification

See `go.mod` for the Go version and dependencies.

```bash
make test
make test-cover
make check
make integration # requires TEST_DATABASE_DSN
make helm-check
```

`make check` runs golangci-lint, vet, race tests, the OpenAPI synchronization check, and the `go mod tidy` check.

Create development branches from `dev` and open pull requests back to `dev`. See [CONTRIBUTING.md](./CONTRIBUTING.md) for commit and coverage rules. See [SECURITY.md](./SECURITY.md) for reporting and the current database security boundary.

## License

[BSD 3-Clause](./LICENSE)
