# The orbitjob CLI

`orbitjob` calls the Admin API for job, run and Check operations, and uses
kubectl for installation diagnostics. It never opens a database connection.
Use the Console or API for authoring and administration beyond its command set.

## Install

From the source checkout, use Go 1.27.3 or later, matching the minimum in
[`go.mod`](../go.mod):

```sh
mkdir -p "$(go env GOPATH)/bin"
go build -o "$(go env GOPATH)/bin/orbitjob" ./cmd/orbitjob
```

Add that directory to `PATH`, or invoke the binary by its absolute path.
API commands require the installation's API address and a named key with the
relevant grants. `status` and the cluster portion of `doctor` also need kubectl
and a kubeconfig identity allowed to inspect that installation.

## Configuration

Connection flags belong to each leaf command. Place them after the subcommand,
for example `orbitjob jobs list --api-url https://orbitjob.example.com`.
There is no configuration file or global-flag prefix.

| Setting | Precedence |
|---|---|
| API base URL | Nonempty `--api-url`, then trimmed `ORBITJOB_API_URL`, then trimmed `ORBITJOB_API`, then `http://localhost:8080` |
| API key | Nonempty `--api-key`, then `ORBITJOB_API_KEY`; absent credentials fail API commands |
| doctor key fallback | After its flag/environment lookup, read `bootstrap-api-key` in the release namespace |

The URL variables contain a base URL only; `ORBITJOB_API_KEY` contains the key.
Use environment variables or your normal secret mechanism to keep key literals
out of shared shell history.

Cluster diagnostics use the current kubeconfig context. Their defaults are
namespace `orbitjob-system`, release `orbitjob` and monitoring namespace
`monitoring`. Select another installation with `--namespace`, `--release` and
`--monitoring-namespace`. `status` does not use an API key even though it accepts
the shared connection flags. `doctor` can read the installation bootstrap credential;
run it only with installation authority, or provide the intended dedicated key
explicitly.

For the repository's configured kind installation, `make kind-env` prints
exports and a port-forward supplies local access:

```sh
source <(make kind-env)
kubectl -n orbitjob-system port-forward svc/orbitjob-admin-api 18080:8080
```

Keep the port-forward running. In another shell, load the same exports before
calling API commands. [Local deployment](local-deployment.md) describes that
source-development installation and its prerequisites.

## Commands

| Command | Operation |
|---|---|
| `status` | Inspect installation objects through kubectl |
| `doctor` | Run installation checks plus API authentication, Lease and RBAC checks |
| `jobs list` | `GET /api/v1/jobs` |
| `jobs get ID` | `GET /api/v1/jobs/{id}` |
| `jobs trigger ID` | `POST /api/v1/jobs/{id}/trigger` |
| `runs list` | `GET /api/v1/instances` |
| `runs get ID` | `GET /api/v1/instances/{id}`, including Attempts |
| `runs cancel ID` | `POST /api/v1/instances/{id}/cancel` |
| `checks list` | `GET /api/v1/checks` |
| `checks get ID` | `GET /api/v1/checks/{id}` |

Job IDs are active revision IDs. Run IDs are numeric ledger instance IDs.
Check IDs identify stored Check definitions. Use IDs from the corresponding
list response. A trigger returns a JobRun reference and occurrence key, without
a ledger run ID; Operator admission creates that row asynchronously.

List commands accept `--limit` and `--offset`; the API defaults to 50 items and
permits 1–100. `runs list` also accepts `--phase`; `checks list` accepts
`--status active|paused`. The CLI exposes fewer filters than the HTTP API.
For example:

```sh
orbitjob jobs list --limit 20
orbitjob runs list --phase Running --limit 20 --offset 0
orbitjob checks list --status active
```

## Trigger and cancel

Preserve a stable key for one intended trigger:

```sh
JOB_ID=42
REQUEST_KEY='invoice-check-2026-11-16-001'
orbitjob jobs trigger "$JOB_ID" --idempotency-key "$REQUEST_KEY" --json
```

Replace the ID with the intended active revision. Repeating the same retained
target/request identity reconciles that occurrence. Omitting the key makes
separate requests create separate identities; changing the revision can create
different work even with the same key. A lost response requires reconciliation
with the original identity before requesting replacement work. See
[trigger and observe](tasks/trigger-and-observe.md).

Cancellation reports the phase at request time, then requires observation:

```sh
RUN_ID=73
orbitjob runs cancel "$RUN_ID" --json
orbitjob runs get "$RUN_ID" --json
```

Only a confirmed `Canceled` phase establishes the requested stop.
`CancelUnknown` and `ExecutionUnknown` remain nonterminal. The command does not
wait for termination. See [cancel a run](tasks/cancel-a-run.md).

## Output and help

API commands print tables or detail fields. `--json` prints the API response
body plus a final newline, without a byte-for-byte transport guarantee.
`status` and `doctor` print diagnostic lines or JSON arrays of `{status, detail}`
records. JSON does not change their failure exit code.

Use help on a supported leaf command:

```sh
orbitjob status -h
orbitjob doctor -h
orbitjob runs list -h
orbitjob jobs trigger -h
```

Leaf `-h` prints flags and exits successfully without contacting the API.
Top-level `orbitjob -h` and group-only `orbitjob jobs -h` are not supported help
commands. ID commands accept the ID before or after their flags. List commands
follow Go flag parsing, so place their flags before any unintended positional
arguments.

## Interpret installation diagnostics

`status` reads the highest visible Helm release Secret revision, the three
Admin/Operator/scheduler deployments, observed Pod restart counts, shipped
migration filenames from `<release>-migrations`, four CRDs and the
`kube-prometheus-stack` release. It does not inspect the Helm release's deployed
status or the database's applied `schema_migrations`. Verify hook outcomes and
applied versions separately during upgrades.

Restart inspection requires a readable nonempty Pod list with valid container
status counters. Missing or malformed evidence fails; it is not reported as
zero restarts. Missing Workflow CRDs and monitoring are warnings in this CLI.
The chart still ships all four CRDs; tolerated warnings do not establish a
valid current installation.

`doctor` includes those checks and:

- Reports the credential source, without printing the credential.
- Requires `GET /api/v1/tenants` to return 200. A dedicated workload key can
  fail this check if it lacks tenant-list permission while other tasks work.
- Checks that `orbitjob-operator-singleton` has a holder. It does not assess
  renewal age or execution progress.
- Runs `kubectl auth can-i` as the current kubeconfig identity for JobRun and,
  when installed, Workflow resources. `--namespaces` selects workload namespaces;
  otherwise it reads the deployed Operator's namespace mapping. This does not
  test the workload ServiceAccount's authority.

A passing diagnostic bundle describes visible configuration and access. It does
not execute a workload, verify failure recovery or qualify an installation.

## Exit codes and scope

| Code | Meaning |
|---|---|
| 0 | Successful command; diagnostics have no `[FAIL]` lines |
| 1 | Usage, transport, server, decoding or other untyped error; diagnostic failure |
| 3 | API 404 |
| 4 | API 401 or 403 |

API errors retain the server's error code/message. The client omits its
Authorization header from error output. A connection-refused error includes a
port-forward hint. Warnings alone do not fail `status` or `doctor`.

The CLI provides no watching, log tailing, shell completion, daemon, direct
SQL or mutations beyond trigger and cancel. Use the API or Console for
[logs](tasks/read-logs.md), [authoring](tasks/manage-jobs.md),
[Functions and Workflows](tasks/functions-and-workflows.md) and access management.
