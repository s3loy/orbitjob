# The orbitjob CLI

`orbitjob` is the developer CLI for an OrbitJob installation. It is a client
only: it talks to the admin API over HTTP and to the cluster through kubectl.
It never opens a database connection — the ledger stays write-only for the
operator and SELECT-only for the admin API, and the CLI inherits that posture
by being just another API client.

## Install

```sh
go build -o "$(go env GOPATH)/bin/orbitjob" ./cmd/orbitjob
```

## Configuration

Every command accepts `--api-url` and `--api-key`. When the flags are absent
the environment supplies them:

| source | base URL | key |
| --- | --- | --- |
| flag (highest) | `--api-url` | `--api-key` |
| environment | `ORBITJOB_API_URL`, then `ORBITJOB_API` | `ORBITJOB_API_KEY` |
| default | `http://localhost:8080` | — (error; `doctor` also tries the secret) |

`make kind-env` prints the exports a kind install needs:

```sh
source <(make kind-env)          # exports ORBITJOB_API_KEY and ORBITJOB_API
kubectl -n orbitjob-system port-forward svc/orbitjob-admin-api 18080:8080 &
orbitjob status
```

Cluster inspection (`status`, `doctor`) uses the current kubeconfig context,
like the repo's scripts. There are no config files.

## Commands

```text
orbitjob status          install snapshot (kubectl only)
orbitjob doctor          full diagnostic bundle (API + cluster)
orbitjob runs list       list runs                GET  /api/v1/instances
orbitjob runs get ID     one run + attempt trail  GET  /api/v1/instances/ID
orbitjob runs cancel ID  request a stop           POST /api/v1/instances/ID/cancel
orbitjob jobs list       list definitions         GET  /api/v1/jobs
orbitjob jobs get ID     one active revision      GET  /api/v1/jobs/ID
orbitjob jobs trigger ID publish a manual run    POST /api/v1/jobs/ID/trigger
orbitjob checks list     list checks              GET  /api/v1/checks
orbitjob checks get ID   one check                GET  /api/v1/checks/ID
```

List filters mirror the API contract: `runs list --phase Running --limit 20
--offset 0`, `jobs list --limit 10`, `checks list --status active`. IDs are
the ledger's numeric ids as printed by the list commands. `jobs trigger` takes
an optional `--idempotency-key`, which makes a retry return the same run
instead of creating a second one.

Every command prints a fixed-width table (a detail view for single objects)
designed to be read by eyes and grep alike. `--json` switches to the raw API
response body, byte for byte, so scripting against the CLI is scripting
against the documented API contract.

### status

Reads the install through kubectl: the helm release (its state secret, with
revision), the three deployments and their restart counts, the migration
version shipped by the release (the keys of the `<release>-migrations`
ConfigMap — the same files the pre-install Job applies), the Custom Resource
definitions, and the monitoring release. The two workflow CRDs are reported as
a WARN when absent, not a failure: an install that predates them is still
valid.

```text
$ orbitjob status
[OK] helm release orbitjob revision 13 in namespace orbitjob-system
[OK] deployment orbitjob-admin-api: 1/1 ready, 0 restarts
...
WARN crd workflowruns.workloads.orbitjob.io: not installed (workflow support)
[OK] monitoring release kube-prometheus-stack present in namespace monitoring
```

### doctor

Everything status checks, plus the API auth path, the leader lease, and RBAC:

- the API key, resolved from `--api-key`, then `ORBITJOB_API_KEY`, then the
  `bootstrap-api-key` secret in the release namespace — the output names the
  source, never the key;
- `GET /api/v1/tenants` must answer 200;
- the leader lease `orbitjob-operator-singleton` must have a holder;
- per-namespace `kubectl auth can-i` for the JobRun custom resources, checked
  in the namespaces passed via `--namespaces`; when omitted, `doctor` discovers
  the keys of `OPERATOR_NAMESPACE_TENANTS` from the deployed operator. Workflow
  resources are checked only once their CRDs are installed.

`orbitjob doctor` exits non-zero on any `[FAIL]` line; `WARN` never fails it.
Run it first — it is the one command that answers "is this install healthy,
and may I talk to it".

## Exit codes

| code | meaning |
| --- | --- |
| 0 | success; for doctor, no `[FAIL]` lines |
| 1 | usage error, connection refused (with a `[FAIL]` hint about the port-forward), 5xx, or any other untyped failure |
| 3 | the requested object does not exist (API 404) |
| 4 | the key may not do that (API 403, or 401) |

Errors print the server's own code and message (for example
`admin API returned 404 NOT_FOUND: resource not found`); there are no stack
traces, and the API key never appears in output or errors.

## Scope

v1 deliberately does not do shell completion, watching or tailing, daemon
work, direct database access, or any mutation besides cancel and trigger.
Flags are accepted per command (`orbitjob runs list --api-key ...`); there are
no global flags before the subcommand.
