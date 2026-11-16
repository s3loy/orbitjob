> Repository source: USAGE.md. References marked repository require a source checkout.

# OrbitJob usage

Use the guides below with the current source and matching component images.
The Console runs at your installation's `/console/`; it is separate from the
static documentation site. Obtain an installation address and the required
workspace membership or API key before performing operations.

## Getting started

- [Choose your first-use path](docs/getting-started.md)
- Run your first job (repository: `docs/tutorials/first-job.md`)
- Prepare installation prerequisites (repository: `docs/setup/prerequisites.md`)
- Install on Kubernetes (repository: `docs/setup/installation.md`)

## Tasks

- [Create and manage job definitions](docs/tasks/manage-jobs.md)
- [Trigger work and observe its outcome](docs/tasks/trigger-and-observe.md)
- [Cancel an execution](docs/tasks/cancel-a-run.md)
- [Read and download execution logs](docs/tasks/read-logs.md)
- [Manage workspace access](docs/tasks/manage-access.md)
- [Call the HTTP API safely](docs/tasks/call-api.md)
- [Use Functions and Workflows](docs/tasks/functions-and-workflows.md)
- [Configure Checks, SLIs and SLOs](docs/tasks/checks-and-slos.md)

## Concepts and reference

- [Execution model](docs/concepts/execution-model.md)
- Architecture and component ownership (repository: `docs/architecture.md`)
- Security boundaries (repository: `SECURITY.md`)
- [Workload resource fields](docs/reference/workload-resources.md)
- Installation and process configuration (repository: `docs/reference/configuration.md`)
- CLI commands (repository: `docs/cli.md`)
- [HTTP client conventions](docs/reference/http-conventions.md)
- [Access policies](docs/reference/access-policies.md)
- [HTTP contract](openapi.yaml)
- Accounts and sessions (repository: `docs/accounts.md`)
- Execution-log retention, capacity and integrity (repository: `docs/execution-logs.md`)

## Installation and operations

- Database configuration and migrations (repository: `docs/database-setup.md`)
- Upgrade an installation (repository: `docs/operations/upgrading.md`)
- Back up and recover (repository: `docs/operations/backup-and-recovery.md`)
- Configure monitoring (repository: `docs/operations/monitoring.md`)
- Troubleshoot an installation (repository: `docs/operations/troubleshooting.md`)
- Maintain a local installation (repository: `docs/local-maintenance.md`)
- Read known limitations (repository: `docs/known-issues.md`)

## Development and project records

- Build and test locally (repository: `docs/local-deployment.md`)
- Contribute changes (repository: `CONTRIBUTING.md`)
- Run load tests (repository: `docs/loadtest-guide.md`)
- Understand CI (repository: `docs/ci.md`)
- Maintain documentation (repository: `docs/project-site.md`)
- Implementation status (repository: `docs/status.md`)
- Verification scope and history (repository: `docs/verification.md`)
- Product requirements (repository: `docs/prd.md`)

A definition, an accepted request and a completed execution are different
states. Use the corresponding task guide for permission checks, request
reconciliation, failure handling and result verification.
