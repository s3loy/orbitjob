# OrbitJob usage

Use the guides below with the current source and matching component images.
The Console runs at your installation's `/console/`; it is separate from the
static documentation site. Obtain an installation address and the required
workspace membership or API key before performing operations.

## Getting started

- [Choose your first-use path](docs/getting-started.md)
- [Run your first job](docs/tutorials/first-job.md)
- [Prepare installation prerequisites](docs/setup/prerequisites.md)
- [Install on Kubernetes](docs/setup/installation.md)

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
- [Architecture and component ownership](docs/architecture.md)
- [Security boundaries](SECURITY.md)
- [Workload resource fields](docs/reference/workload-resources.md)
- [Installation and process configuration](docs/reference/configuration.md)
- [CLI commands](docs/cli.md)
- [HTTP client conventions](docs/reference/http-conventions.md)
- [Access policies](docs/reference/access-policies.md)
- [HTTP contract](api/openapi.yaml)
- [Accounts and sessions](docs/accounts.md)
- [Execution-log retention, capacity and integrity](docs/execution-logs.md)

## Installation and operations

- [Database configuration and migrations](docs/database-setup.md)
- [Upgrade an installation](docs/operations/upgrading.md)
- [Back up and recover](docs/operations/backup-and-recovery.md)
- [Configure monitoring](docs/operations/monitoring.md)
- [Troubleshoot an installation](docs/operations/troubleshooting.md)
- [Maintain a local installation](docs/local-maintenance.md)
- [Read known limitations](docs/known-issues.md)

## Development and project records

- [Build and test locally](docs/local-deployment.md)
- [Contribute changes](CONTRIBUTING.md)
- [Run load tests](docs/loadtest-guide.md)
- [Understand CI](docs/ci.md)
- [Maintain documentation](docs/project-site.md)
- [Implementation status](docs/status.md)
- [Verification scope and history](docs/verification.md)
- [Product requirements](docs/prd.md)

A definition, an accepted request and a completed execution are different
states. Use the corresponding task guide for permission checks, request
reconciliation, failure handling and result verification.
