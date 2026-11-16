# Call the Admin API

The Admin API provides authenticated management operations and execution
history. Automation uses named API keys. The installed Console uses a separate
same-origin personal-account session; an API key cannot sign in to it.

## Before you begin

Obtain the installation's trusted API base URL and a dedicated key with the
required action/resource grants. Use HTTPS outside explicitly configured
loopback development. The examples use `curl` with `--fail-with-body` support
and `jq` for JSON inspection. Supply the key through your normal secret-delivery
process, without putting its literal value in shared shell history:

```sh
set -o pipefail
export ORBITJOB_API_URL='https://orbitjob.example.com'
read -r -s ORBITJOB_API_KEY
export ORBITJOB_API_KEY
```

Run these commands in Bash or Zsh and enter the supplied key for `read`.
`pipefail` preserves a failed curl exit status when a response is piped to
`jq` or `tee`; check each command succeeds before continuing.
The key stays in the authorization header. Do not add it to a URL. Open
`$ORBITJOB_API_URL/openapi.json` for the installation's machine-readable contract;
the source contract is [OpenAPI](../../api/openapi.yaml).

Preserve numeric IDs with lossless JSON parsing and follow each collection's
pagination contract. See [HTTP client conventions](../reference/http-conventions.md)
for exact int64 values, identifier types, time windows and trace diagnostics.

## Read available jobs

```sh
curl --silent --show-error --fail-with-body \
  -H "Authorization: Bearer $ORBITJOB_API_KEY" \
  "$ORBITJOB_API_URL/api/v1/jobs?limit=20" | jq .
```

The response's `items` are active projected job revisions in the authorized
workspace. Record the intended item's numeric `id`, `source_uid`, namespace
and configuration. This read requires `job:List`; detail requires `job:Get`.
Job and instance routes require credentials without a resource-group restriction.

Tenant credentials remain bound to their own tenant. Platform credentials must
select an authorized target with `tenant_id`; use that same target on every
related request. Human-session requests also select the active workspace and
require active membership. A tenant selector never grants access by itself.

## Trigger one run

Use a new key for each intended execution and preserve it across retries of
that execution. Replay with the same authenticated principal and workspace;
switching from a human session to an API key changes the recorded actor and can
produce a conflict. Replace `42` with the selected active job revision ID:

```sh
JOB_ID=42
REQUEST_KEY='invoice-check-2026-11-16-001'
curl --silent --show-error --fail-with-body \
  -X POST \
  -H "Authorization: Bearer $ORBITJOB_API_KEY" \
  -H "X-OrbitJob-Idempotency-Key: $REQUEST_KEY" \
  "$ORBITJOB_API_URL/api/v1/jobs/$JOB_ID/trigger" | tee trigger-receipt.json
```

The trigger takes no body and requires `job:Trigger`. A new publication returns
201; a reconciled existing resource returns 200. The JSON fields are
`namespace`, `name`, `occurrence_key`, `trigger`, `phase` and `created`. The
initial phase can be empty. This receipt identifies the requested JobRun and
does not contain a ledger run ID or confirm Pod start.

The header is optional for this route, but omitting it gives each trigger new
entropy and prevents safe automatic replay. It is trimmed and limited to 128
bytes after trimming. Its occurrence identity includes the tenant, source UID
and selected revision ID. Changing the revision or key can request another run. Retention
limits identity availability; it is not a permanent deduplication guarantee.
Protect receipts when their identifiers or errors are sensitive.

## Observe the request

Read the exact occurrence from the receipt. This requires independent
`instance:List` access:

```sh
OCCURRENCE_KEY=$(jq -er '.occurrence_key | select(type == "string" and length > 0)' trigger-receipt.json)
curl --silent --show-error --fail-with-body --get \
  -H "Authorization: Bearer $ORBITJOB_API_KEY" \
  --data-urlencode "occurrence_key=$OCCURRENCE_KEY" \
  --data-urlencode 'limit=20' \
  "$ORBITJOB_API_URL/api/v1/instances" | jq .
```

A newly accepted request can be absent before Operator admission, including
while Forbid holds it behind an earlier execution. Observe with a bounded
interval and retain the receipt if the wait expires. A timeout does not reject
the request. Once an item appears, read `/api/v1/instances/{id}` with
`instance:Get`. Inspect its Run phase and Attempt trail. Only `Succeeded`,
`Failed` and `Canceled` establish terminal Run outcomes.

Use `POST /api/v1/instances/{id}/cancel` with `instance:Cancel` to request a
stop, then continue observing. Read or download archives with the independent
`instance:ReadLogs` action. See [cancellation](cancel-a-run.md) and
[logs](read-logs.md).

## Handle failures

| Result | Client action |
|---|---|
| 400 or 413 | Correct the request or body size before retrying. The Admin body limit is 1 MiB. |
| 401 | Authentication was rejected. Obtain a valid credential; do not interpret it as a workload result. |
| 403 | Check the exact action, target, membership, Deny and boundary. Repeated submission does not resolve authorization. |
| 404 | Verify the target and scope; resource absence in this view does not prove that workload code never ran. |
| 409 | Reload current state and review the contract's version or identity conflict. |
| 429 | Back off with jitter and a bounded retry budget. Preserve the original mutation identity. |
| 5xx, transport error or lost response | Preserve the original request. Observe or reconcile its result before requesting new work. |

Authentication and permission storage outages can return 503 while the current
credential remains valid; retain it. Honor `Retry-After` when supplied, but do
not assume every rate-limit response includes that header. Job trigger and
instance cancel have a default per-process tenant budget of 5 requests/second;
generic writes use 10 and reads 100, each with the same burst size. Installation settings can
change these values. They are not workload execution-throughput guarantees.

Definition saves, Function creation and versioned updates have their own
identity and concurrency requirements. Reuse the exact retained request only
where that route defines reconciliation. A generic HTTP retry policy cannot
safely replay every mutation. See [manage jobs](manage-jobs.md) and
[Functions and Workflows](functions-and-workflows.md).
