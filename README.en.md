# OrbitJob

[![Go](https://img.shields.io/badge/Go-1.26.4-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/github/license/s3loy/orbitjob)](./LICENSE)
[![golangci-lint](https://github.com/s3loy/orbitjob/actions/workflows/lint.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/lint.yml)
[![Build Status](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml)
[![govulncheck](https://github.com/s3loy/orbitjob/actions/workflows/govulncheck.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/govulncheck.yml)
[![Coverage Status](https://codecov.io/gh/s3loy/orbitjob/graph/badge.svg)](https://codecov.io/gh/s3loy/orbitjob)

[中文](./README.md)

![Stone Badge](https://stone.professorlee.work/api/stone/s3loy/orbitjob)

OrbitJob is a cloud-native distributed job scheduling platform. PostgreSQL is the source of truth; four processes (admin-api / scheduler / dispatcher / worker) separate concerns; coordination uses etcd or K8s Lease.

Use OrbitJob to:

- Trigger jobs on cron schedules or manually
- Run jobs with multi-tenant isolation, per-tenant API keys
- Auto-recover failed jobs (lease reclaim, retry, orphan recovery)
- Monitor execution with Prometheus metrics, structured logs, health checks
- Define inspections and SLOs, track error budgets

## Core mechanics

- PostgreSQL stores job state; transactions guarantee consistency
- Four processes: admin-api (HTTP), scheduler (trigger), dispatcher (dispatch), worker (execute)
- Claim/Lease protocol ensures at-least-once execution; instance-level lease prevents duplicates
- Coordination: PG-only (dev/CI), etcd, K8s Lease (production)

## Deployment

| Mode | Use case | Status |
|---|---|---|
| Docker Compose | Local dev | Available |
| Helm + K8s | Production (cloud-native) | Planned |

## Roadmap

- [x] S0 deployment and auth
- [x] S1 job scheduling closed loop
- [ ] Helm K8s deployment (in progress)
- [ ] S2 workflow orchestration
- [ ] S3 serverless functions
- [ ] S4 high availability

## Quick Start

```bash
docker compose up -d --build
```

Creates a bootstrap tenant and an initial API key. To view the key:

```bash
go run ./cmd/bootstrap "$DATABASE_DSN"
```

On macOS Docker Desktop, add the override to disable Promtail:

```bash
docker compose -f docker-compose.yml -f docker-compose.macos.yml up -d --build
```

## License

[BSD 3-Clause](./LICENSE)
