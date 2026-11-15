# The orbitjob CLI

`orbitjob` is the developer CLI for an OrbitJob installation. It is a client
only: it sends HTTP requests to the Admin API and accesses the cluster through kubectl.
It never opens a database connection. The operator owns execution-ledger
writes; the admin API reads execution history and publishes run requests as
custom resources. CLI mutations use that API.

## Install

```sh
mkdir -p "$(go env GOPATH)/bin"
go build -o "$(go env GOPATH)/bin/orbitjob" ./cmd/orbitjob
```

Add that directory to `PATH`, or invoke the binary by its full path. Use the
Go version specified in [`go.mod`](../go.mod).

## Configuration

Every command accepts `--api-url` and `--api-key`. When the flags are absent,
the CLI reads the corresponding environment variables:

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
as do the repository scripts. There are no config files.

## Commands

```text
orbitjob status          install snapshot (kubectl only)
orbitjob doctor          diagnostic checks (API + cluster)
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
the ledger's numeric IDs as printed by the list commands. `jobs trigger` takes
an optional `--idempotency-key`, which makes a retry return the same run
instead of creating a second one.

API commands print fixed-width tables or detail fields. Their `--json` mode
prints the API response body with a final newline; it does not promise
byte-for-byte transport preservation. `status` and `doctor` instead print
diagnostic lines, or JSON arrays of `{status, detail}` records with `--json`.
Both diagnostic formats keep the same failure exit code.

### status

Reads installation details through kubectl: the Helm release (its state secret, with
revision), the three deployments and their restart counts, the migration
version shipped by the release (the keys of the `<release>-migrations`
ConfigMap), the Custom Resource definitions, and the monitoring release.
This reads the highest visible Helm revision, not the release's deployed
status, and the shipped migration filenames, not the database's applied
`schema_migrations` rows. Confirm hook results and applied versions separately
when diagnosing an upgrade. The two workflow CRDs currently produce `WARN`
when absent; that tolerance does not prove the current operator can run
without them. The chart ships all four CRDs.

Restart counts require a readable, nonempty Pod list and container statuses with
valid nonnegative integer counters. RBAC/API errors, malformed responses and
missing counters produce `[FAIL]` and a nonzero exit in both text and JSON modes.
An `OK ... 0 restarts` line therefore means observed zero counters, not a failed
inspection converted to zero.

Illustrative output:

```text
$ orbitjob status
[OK] helm release orbitjob revision 13 in namespace orbitjob-system
[OK] deployment orbitjob-admin-api: 1/1 ready, 0 restarts
...
WARN crd workflowruns.workloads.orbitjob.io: not installed (workflow support)
[OK] monitoring release kube-prometheus-stack present in namespace monitoring
```

### doctor

Performs all status checks, plus API authentication, leader Lease and RBAC checks:

- the API key, resolved from `--api-key`, then `ORBITJOB_API_KEY`, then the
  `bootstrap-api-key` secret in the release namespace — the output names the
  source, never the key;
- `GET /api/v1/tenants` must answer 200;
- the leader lease `orbitjob-operator-singleton` must have a holder; this does
  not check its renewal age or prove the holder is making progress;
- per-namespace `kubectl auth can-i` for the JobRun custom resources, checked
  as the current kubeconfig identity, rather than as a workload ServiceAccount,
  in the namespaces passed via `--namespaces`; when omitted, `doctor` discovers
  the keys of `OPERATOR_NAMESPACE_TENANTS` from the deployed operator. Workflow
  resources are checked only once their CRDs are installed.

`orbitjob doctor` exits non-zero on any `[FAIL]` line; `WARN` never fails it.
Use it to inspect API access and visible cluster configuration. It does not
execute a workload, prove business recovery, or certify an installation.

## Exit codes

| code | meaning |
| --- | --- |
| 0 | success; for status/doctor, no `[FAIL]` lines |
| 1 | usage error, connection refused (with a `[FAIL]` hint about the port-forward), 5xx, or any other untyped failure |
| 3 | the requested object does not exist (API 404) |
| 4 | the credential is unauthorized or lacks permission (API 403, or 401) |

Errors print the server's own code and message (for example
`admin API returned 404 NOT_FOUND: resource not found`). The client does not
print its authorization header; key resolution reports the source only.

## Scope

The current CLI does not support shell completion, watching or tailing, daemon
operation, direct database access, or mutations other than cancel and trigger.
Flags are accepted per command (`orbitjob runs list --api-key ...`); there are
no global flags before the subcommand.
Use `orbitjob status -h`, `orbitjob runs list -h`, or the corresponding leaf
command's `-h` for its flags; top-level `orbitjob -h` is not a supported help
command.
