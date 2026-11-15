# OrbitJob

[![Go](https://img.shields.io/badge/Go-00ADD8?logo=go)](./go.mod)
[![License](https://img.shields.io/github/license/s3loy/orbitjob)](./LICENSE)
[![golangci-lint](https://github.com/s3loy/orbitjob/actions/workflows/lint.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/lint.yml)
[![Build Status](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml)
[![govulncheck](https://github.com/s3loy/orbitjob/actions/workflows/govulncheck.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/govulncheck.yml)
[![Coverage Status](https://codecov.io/gh/s3loy/orbitjob/graph/badge.svg)](https://codecov.io/gh/s3loy/orbitjob)

[Chinese](./README.zh.md)


OrbitJob is an open source platform for scheduling and managing container tasks.
It supports scheduled jobs, manual execution and workflows with task dependencies.
Developers use the HTTP API, Kubernetes resources and CLI to define tasks,
configure retries and concurrency, and inspect execution results and logs.

OrbitJob runs tasks as Kubernetes Jobs and stores execution history and audit
records in PostgreSQL. Tenant workspaces and explicit policy grants control access
to workloads and their results. See [architecture](docs/architecture.md) for the
component model and [security](SECURITY.md) for access boundaries.

These instructions describe the current source. Use a published version's
[version-specific documentation](https://github.com/s3loy/orbitjob/tags) when
installing that version. Build the chart and component images from one reviewed
revision for a source installation.

## Features

- Scheduled and manual container tasks with retries, deadlines, concurrency and history-retention policies
- DAG workflows with dependencies, conditions and timezone-aware schedules
- Versioned Functions with definition management and parameterless HTTP invocation
- Execution history, cancellation and retained logs with explicit collection gaps
- Personal accounts, revocable HTTP sessions and explicit tenant workspace memberships
- API keys, policy grants and resource-group restrictions
- HTTP Checks, SLI snapshots, SLO error budgets and burn alerts
- HTTP API, Kubernetes CRDs and CLI; Helm deployment and Prometheus/Grafana integration

## Quick Start

Obtain the installation's Admin API origin and a dedicated tenant API key from
its administrator. Jobs and Runs require an ungrouped key with the corresponding
read/trigger permissions. Log reads require a separate grant. The administrator
must configure the tenant's execution namespace and Kubernetes RBAC first.

Load the key privately into `ORBITJOB_API_KEY`, set the supplied API origin and
check authorized definitions:

```bash
export ORBITJOB_API='https://orbitjob.example.com'
curl -sS --fail-with-body -H "Authorization: Bearer $ORBITJOB_API_KEY" \
  "$ORBITJOB_API/api/v1/jobs?limit=50"
```

To create and run a task, follow the complete
[definition procedure](USAGE.md#101-job-definitions-the-scheduledjob-cr),
[manual trigger](USAGE.md#102-manual-trigger) and
[execution observation](USAGE.md#103-runs-and-phases) examples. A successful
trigger response acknowledges publication; inspect the matching execution's
phase and logs to determine its outcome.

<a id="web-console"></a>

The backend Admin image provides HTTP and account APIs without compiled browser
assets. `/console/` returns 503 when those assets are absent; API routes remain
available. Personal-account setup, sessions and recovery are documented in
[accounts](docs/accounts.md).

<a id="helm"></a>

## Deployment

For a published version, use the chart, component images and instructions from
that same release. Combining older images with the current chart is not a
supported source installation.

The current source can be built and installed locally through
[Development](#development). To package it for an existing cluster, build all
components from the same reviewed revision and follow
[Helm installation](USAGE.md#7-helm-and-kubernetes). Prepare PostgreSQL, the
installation Secret, persistent storage, tenant namespace mappings and RBAC
before installation. The [database guide](docs/database-setup.md) describes
credential preparation and migration requirements.

Choose the API address during setup and preserve `/api/v1/` and
`/openapi.json` when configuring a reverse proxy. Installation below an additional
path prefix is not supported. See
[installation address and external access](USAGE.md#installation-address-and-external-access)
for values and prerequisites. Use HTTPS and gateway access controls for production.
The [security policy](SECURITY.md) describes credential handling;
[verification](docs/verification.md) records checked configurations and their limits.

## Development

Source development uses a separate kind cluster and locally built `:dev` images.
The following versions apply to the current source:

| Tool | Requirement or reference version | Use |
|---|---|---|
| Go | **1.27.3+** | Go development and local installation helpers |
| Docker Engine / kind / Helm | **29.5.2 / 0.33.0 / 4.3.1** | Reference versions for the local environment, not declared compatibility minima |
| kubectl | Match the cluster's Kubernetes minor version | Cluster management; observe the upstream version-skew policy |

The complete [development prerequisites](docs/local-deployment.md#prerequisites)
include shell utilities and storage requirements. The combined local environment
requires at least **4 vCPU, 8 GiB RAM and 40 GiB free backing storage**, including
a 5-GiB database volume and a separate 10-GiB log volume.

From a new shell, obtain the source before running repository scripts:

```bash
git clone --branch dev https://github.com/s3loy/orbitjob.git
cd orbitjob
bash scripts/quickstart.sh
```

The script builds the current source, prepares PostgreSQL and credentials,
loads development images and installs the chart in `orbitjob-dev`. It writes
`.kind/env.sh`; it does not start the API port-forward. In a separate terminal:

```bash
kubectl --context kind-orbitjob-dev -n orbitjob-system \
  port-forward svc/orbitjob-admin-api 18080:8080
```

Keep that terminal running. For API examples, `source .kind/env.sh` loads the
bootstrap integration key and API address into the current shell. Issue a narrower
dedicated key for ordinary use. The bootstrap key does not administer personal
accounts. Follow [account setup](docs/accounts.md#create-the-first-administrator)
when account sessions are required.

For changes, tests and redeployment, use the
[development guide](docs/local-deployment.md) and
[contribution guide](CONTRIBUTING.md).
The local script is not an upgrade procedure; review
[upgrade requirements](docs/local-deployment.md#upgrading-an-existing-installation)
before modifying an existing environment.

## Documentation

- [Usage](USAGE.md): API examples, workload operations and installation procedures
- [Accounts](docs/accounts.md): first administrator, memberships, sessions and recovery
- [Deployment development guide](docs/local-deployment.md): local setup, changes, diagnostics and tests
- [Database configuration](docs/database-setup.md): PostgreSQL credentials, Secrets and migrations
- [Security](SECURITY.md): authentication, authorization and credential handling
- [CLI reference](docs/cli.md): commands and configuration
- [Architecture](docs/architecture.md): components and execution behavior
- [Project status](docs/status.md), [verification](docs/verification.md) and [known issues](docs/known-issues.md)
- [OpenAPI](api/openapi.yaml): HTTP request and response schemas
- [Contributing](CONTRIBUTING.md): development workflow and contribution requirements

## License

[BSD 3-Clause](LICENSE)
