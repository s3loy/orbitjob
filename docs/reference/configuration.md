# Configuration reference

Configure an installation with the chart's values and installation Secrets.
Process environment variables are useful for source development and components
run outside the chart. Changing an environment file or Secret does not update an
already running process or rotate a PostgreSQL role password.

## Chart configuration

Use [values.yaml](../../charts/orbitjob/values.yaml) together with
[values.schema.json](../../charts/orbitjob/values.schema.json). The schema checks
shape and conditional requirements; templates add deployment constraints.

| Area | Configuration and prerequisite |
| --- | --- |
| Images | Set one `global.imageTag` for images built from the same revision. Set the pull policy for registry or locally loaded images. |
| Database | Supply the installation Secret prepared by `cmd/configure`; PostgreSQL runs separately from this chart. See [database configuration](../database-setup.md). |
| Workspaces | `operator.namespaceTenants` maps Kubernetes namespaces to tenant IDs. Provision matching namespace RBAC; a database tenant row alone grants no namespace access. |
| Console access | `adminapi.service` selects exposure. `adminapi.ingress` uses an existing controller and optionally an existing TLS Secret. Configure the exact external Console origin. |
| Runtime logs | Archive storage, collector spool limits, internal TLS and network policy require installation-specific preparation. See [execution logs](../execution-logs.md). |
| Resources | Component resource requests and limits are separate from the accepted resource profile of workload containers. |

Render before installation:

```bash
helm lint ./charts/orbitjob -f /path/to/installation-values.yaml
helm template orbitjob ./charts/orbitjob \
  --namespace orbitjob-system -f /path/to/installation-values.yaml
```

Rendering does not prove Secret availability, storage allocation, network-policy
enforcement or database connectivity. Follow the [installation procedure](../setup/installation.md)
for those checks.

## Database environment resolution

The first populated variable in each row is used. DSNs must identify the role
for that component; a shared fallback does not make an owner connection suitable
for application use.

| Process | Resolution order |
| --- | --- |
| Admin API | `ADMIN_DSN`, then `DATABASE_DSN` |
| Operator | `OPERATOR_DSN`, then `RUNTIME_DSN`, then `DATABASE_DSN` |
| Scheduler | `RUNTIME_DSN`, then `SCHEDULER_DSN`, then `DATABASE_DSN` |
| Log archive | `RUNTIME_DSN` is required |
| Migrator | `MIGRATION_MODE=owner-init` uses owner initialization inputs; `migrate` requires `MIGRATOR_DSN`. See the database guide for role and checksum-channel constraints. |

The owning implementations are the respective [command entrypoints](../../cmd/)
and [database configuration resolver](../../internal/platform/config/).
Environment examples are in [deploy/env](../../deploy/env/).

## API and browser settings

| Variable | Behavior |
| --- | --- |
| `PORT` | Admin HTTP port; defaults to `8080` when empty. |
| `CONSOLE_PUBLIC_ORIGIN` | Exact browser origin, including a non-default port; use HTTPS for production sessions. Proxy headers do not redefine it. |
| `CONSOLE_ALLOW_INSECURE_LOCALHOST` | Explicit local-development exception for HTTP loopback sessions; it does not permit public HTTP origins. |
| `OPERATOR_NAMESPACE_TENANTS` | Explicit namespace/tenant mapping used by the Operator and additional Admin routes. |
| `LOG_ARCHIVE_URL` | Internal archive endpoint. The Admin reader uses the mounted reader certificate, key and CA. |
| `RATELIMIT_READ_RPS` | Per-tenant read token bucket. Code default `100`; chart default `100`. |
| `RATELIMIT_WRITE_RPS` | Per-tenant generic write bucket, including Function invocation and Workflow mutations. Code default `10`; chart default `100`. |
| `RATELIMIT_TRIGGER_RPS` | Job trigger and instance cancel bucket. Function/Workflow writes use the write bucket. Code default `5`; chart default `10`. |

Rate values set both refill rate and burst size. A `429` response requires a
bounded retry according to the response; repeated execution requests must retain
their original idempotency identity. See [calling the API](../tasks/call-api.md).

## Operator and archive settings

The Operator defaults to a five-second schedule interval
(`OPERATOR_SCHEDULE_INTERVAL_SEC`), a five-minute retention interval
(`OPERATOR_RETENTION_INTERVAL_MIN`) and health port `8082`
(`OPERATOR_HEALTH_PORT`). The Scheduler health port defaults to `6060`
(`SCHEDULER_HEALTH_PORT`). These settings do not define a scheduling-latency or
availability guarantee.

The archive stores data under `LOG_DIRECTORY` (default `/data`). Its byte limits
use `LOG_TOTAL_BYTES`, `LOG_RUN_BYTES`, `LOG_HEAD_BYTES`, `LOG_WORKFLOW_BYTES`,
`LOG_TENANT_BYTES`, `LOG_DECLARED_BYTES` and `LOG_RESERVE_BYTES`. Use the
[archive capacity reference](../execution-logs.md) before changing them; lowering
a configured budget cannot create filesystem space or safely discard protected
evidence.

## CLI connection settings

`--api-url` overrides `ORBITJOB_API_URL`, which overrides `ORBITJOB_API`; the
fallback is `http://localhost:8080`. Both environment variables are URLs.
`--api-key` overrides `ORBITJOB_API_KEY`. Use the [CLI reference](../cli.md) for
commands, output and exit behavior. Keep credentials out of source files,
terminal recordings and documentation-site inputs.
