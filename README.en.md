# OrbitJob

[![Go](https://img.shields.io/badge/Go-1.26.3-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/github/license/s3loy/orbitjob)](./LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/s3loy/orbitjob)](https://goreportcard.com/report/github.com/s3loy/orbitjob)
[![Build Status](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml)
[![govulncheck](https://github.com/s3loy/orbitjob/actions/workflows/govulncheck.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/govulncheck.yml)
[![Coverage Status](https://codecov.io/gh/s3loy/orbitjob/graph/badge.svg)](https://codecov.io/gh/s3loy/orbitjob)
[![Stars](https://img.shields.io/github/stars/s3loy/orbitjob)](https://github.com/s3loy/orbitjob/stargazers)

[中文](./README.md)

![Stone Badge](https://stone.professorlee.work/api/stone/s3loy/orbitjob)

Like `database/sql` defines how Go talks to databases, OrbitJob defines how Go schedules tasks.

Pure Go. PostgreSQL is the only required dependency. Built-in observability, cron and manual triggers. Embed as a library or deploy standalone.

- **Single dependency** — PostgreSQL is the only required external dependency; no Redis or Kafka needed
- **Two modes** — Embed as a library in your app, or deploy as standalone multi-process services
- **Built-in observability** — Prometheus metrics, health checks, and distributed tracing out of the box
- **Flexible triggers** — Cron scheduling and manual trigger support
- **Extensible handlers** — Built-in exec, HTTP, webhook, PGNotify; add your own custom handlers

## Quick Start

```bash
docker compose up -d
```

See [docs](./docs) for usage.

## License

[BSD 3-Clause](./LICENSE)
