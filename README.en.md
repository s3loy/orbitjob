# OrbitJob

[![Go](https://img.shields.io/badge/Go-1.26.5-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/github/license/s3loy/orbitjob)](./LICENSE)
[![golangci-lint](https://github.com/s3loy/orbitjob/actions/workflows/lint.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/lint.yml)
[![Build Status](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml)
[![govulncheck](https://github.com/s3loy/orbitjob/actions/workflows/govulncheck.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/govulncheck.yml)
[![Coverage Status](https://codecov.io/gh/s3loy/orbitjob/graph/badge.svg)](https://codecov.io/gh/s3loy/orbitjob)

[中文](./README.md)

OrbitJob is a Kubernetes-first distributed job scheduling platform. PostgreSQL serves as the persistent state store for execution transactions. Four independent processes — `admin-api`, `scheduler`, `dispatcher`, and `worker` — handle the control plane, cron triggering, dispatch, and execution respectively.

OrbitJob targets multi-tenant cron and API-triggered jobs, Kubernetes Job execution, HTTP health checks (Check/CheckRun), and SLI/SLO calculation with error budget alerts. Scheduling uses Claim/Lease at-least-once delivery semantics; handlers must be idempotent.

The four processes coordinate through PostgreSQL: the `scheduler` scans due Jobs and creates Instances, the `dispatcher` assigns pending Instances to capable `worker` processes for execution, and the `admin-api` exposes the HTTP control plane. Multiple schedulers and workers compete via `FOR UPDATE SKIP LOCKED`. Cross-process election and discovery support both memory-based and etcd-based backends.

## Getting Started

Docker Compose v2 and `make` are required. Ensure ports `8080`, `9090`, `3000`, and `5432` are available. See [USAGE.md](./USAGE.md) for all deployment options, API examples, handler payloads, and troubleshooting. The quickest path is below.

### Docker Compose

```bash
make setup
make docker-up
```

```bash
export ORBITJOB_API_KEY="$(make --no-print-directory bootstrap-key)"
curl -fsS http://localhost:8080/healthz
curl -fsS -H "Authorization: Bearer $ORBITJOB_API_KEY" http://localhost:8080/api/v1/tenants
```

`make setup` creates an installation-level database configuration. The bundled PostgreSQL requires no DSN. The Admin API listens on `8080` and serves Prometheus metrics at `/metrics` and the OpenAPI schema at `/openapi.json`.

### Helm

```bash
helm upgrade --install orbitjob charts/orbitjob \
  --namespace orbitjob-system --create-namespace \
  -f values.production.yaml
```

The Chart is version `0.2.0`. It installs four runtime Deployments, RBAC, and a container task namespace. PostgreSQL is not included. One installation uses one `orbitjob-database` Secret shared by all replicas. See [`docs/database-setup.md`](docs/database-setup.md) for the database configuration workflow.

## Developing

See [CONTRIBUTING.md](./CONTRIBUTING.md) for branch strategy, code conventions, testing layers, and the PR process.

```bash
git clone https://github.com/s3loy/orbitjob.git
cd orbitjob

make test          # unit tests
make test-cover    # coverage
make check         # lint + vet + race + openapi-check + tidy-check
make integration   # integration tests (requires TEST_DATABASE_DSN)
```

## Features

- Cron and manual triggers; fixed and exponential retry; misfire and concurrency policies
- Five handler types: `exec`, `http`, `webhook`, `pg_notify`, and `container`
- Kubernetes Job container execution with image digest verification, RBAC, and a restricted Pod Security Context
- API key authentication, tenant isolation, PostgreSQL roles, RLS, and `SECURITY DEFINER` entry-point functions
- Checks, CheckRuns, SLIs, SLOs, error budgets, and burn-rate alerts
- Prometheus metrics, Grafana dashboard, structured logging, and trace ID propagation
- Memory-based and etcd-based election and discovery

Not yet delivered: Operator, CRD, Kubernetes Lease, PG epoch fencing, Workflow, and Serverless.

## Documentation

| Document | Contents |
|---|---|
| [USAGE.md](./USAGE.md) | Deployment options, Docker Compose / Helm / standalone, full API examples, handler payloads, Check/SLI/SLO operations, monitoring and troubleshooting |
| [CONTRIBUTING.md](./CONTRIBUTING.md) | Branch naming, dependency direction, test layers, migration conventions, commit format, PR template |
| [SECURITY.md](./SECURITY.md) | Security model, vulnerability reporting, PostgreSQL roles and RLS boundaries, container security constraints |
| [docs/database-setup.md](docs/database-setup.md) | Bundled / external PostgreSQL configuration, TLS, Kubernetes Secret contract |
| [api/openapi.yaml](./api/openapi.yaml) | API schema |

## Community

- [GitHub Issues](https://github.com/s3loy/orbitjob/issues) — report bugs or request features
- [SECURITY.md](./SECURITY.md) — do not disclose vulnerabilities publicly; follow the reporting process in this file

## License

[BSD 3-Clause](./LICENSE)
