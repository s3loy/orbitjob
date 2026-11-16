# OrbitJob

[Simplified Chinese](README.zh.md)

OrbitJob runs scheduled and on-demand container workloads on Kubernetes. It
supports cron schedules, dependency-based workflows and versioned container
Functions. Workloads are managed through a same-origin Console, HTTP API or CLI.

The Operator handles scheduling and execution. It projects definitions into
immutable revisions and binds admitted `JobRun` resources to their definition
revisions. Each platform Attempt corresponds to one Kubernetes Job, which can
create replacement Pods under its own backoff policy. PostgreSQL stores
definition revisions and the execution ledger, recording triggers, actors,
attempts and observed outcomes. It also stores access policies and audit records.

A dedicated, bounded log archive retains container output independently of
workload cleanup and reports collection gaps. HTTP Checks and SLI snapshots
provide observations for SLO error budgets and burn alerts; the Scheduler
evaluates those budgets from recorded state.

## Get started

### Requirements

To use an existing installation, obtain its trusted Console address, a workspace
invitation and permission to run the intended job and read its execution history.
Reading output requires separate log access. Personal accounts sign people into
the Console; API keys authenticate scripts and HTTP clients. See
[account setup and access](docs/accounts.md) for invitations and administrator setup.

To install OrbitJob, prepare Kubernetes, PostgreSQL, durable database and log
storage, and the required component images. The
[installation prerequisites](docs/setup/prerequisites.md) specify versions,
capacity, namespace mapping and permissions. For a source development
environment, use the [local development guide](docs/local-deployment.md).

### Run your first job

On a configured installation with a job supplied by your administrator:

1. Sign in to `/console/`. Under **Account**, select the workspace and choose
   **Open workspace**.
2. Open **Jobs**, select the job and review its namespace and execution settings.
   Select **Trigger** and confirm the request.
3. Keep the receipt and follow it to the recorded run. Inspect the final phase
   and its Attempts; open **Execution logs** when authorized.

A receipt acknowledges the request. Run history appears asynchronously, and
missing output does not establish the execution outcome. Missing termination
evidence can leave a Run unknown. The
[first-job tutorial](docs/tutorials/first-job.md) includes the permissions,
observation steps and cleanup. It also walks authors through creating a small
manual job that prints a message. Saving a manual definition does not run it.

### Install or build from source

Use the [installation guide](docs/setup/installation.md) for an existing cluster.
Build the current components from one source commit and use the matching chart
and database migrations. The guide covers credentials, storage, namespace
mappings, access and installation checks.

For local source development, the [kind workflow](docs/local-deployment.md)
builds and loads development images into an isolated cluster. Its prerequisites
and lifecycle differ from upgrading an existing installation. See the
[upgrade procedure](docs/operations/upgrading.md) before changing a working system.

## Next steps

- Follow the [getting-started guide](docs/getting-started.md) for workspace access
  and the request-to-result flow.
- Browse [tutorials and tasks](USAGE.md) for workload authoring, execution,
  cancellation, logs and access management.
- Read the [execution model](docs/concepts/execution-model.md) and
  [technical architecture](docs/architecture.md) for revisions, retries,
  uncertainty and component responsibilities. External workload effects can
  repeat; make them idempotent where repetition is unsafe.
- [Call the API](docs/tasks/call-api.md) with a separately issued API key. The
  installation exposes its contract at `/openapi.json`; use the contract that
  matches the installation. The repository copy is [OpenAPI](api/openapi.yaml).
- Look up [workload resources](docs/reference/workload-resources.md),
  [configuration](docs/reference/configuration.md) and [CLI commands](docs/cli.md).
- Review [database roles and migrations](docs/database-setup.md),
  [execution-log retention](docs/execution-logs.md), [security](SECURITY.md) and
  [known limitations](docs/known-issues.md) before operating an installation.

## Contributing

See [CONTRIBUTING](CONTRIBUTING.md) for development prerequisites, tests, branch
conventions and migration rules. Documentation contributions follow the
[documentation workflow](docs/project-site.md).

## Project status

The current source is under development toward v0.3.0. A chart version or a
successful local build does not establish image availability in a registry.
Check the [Changelog](CHANGELOG.md) for release history and
[verification](docs/verification.md) for the environments and behaviors exercised.
Those checks do not establish production throughput or HA qualification.

## License

[BSD 3-Clause](LICENSE)
