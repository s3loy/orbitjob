# HTTP client conventions

Send requests to the installation's Admin API origin. Its `/openapi.json`
describes the deployed contract. The [calling task](../tasks/call-api.md)
provides a request-to-result example; each operation's reference declares its
parameters, independent grants, request body and responses.

## Identifiers and JSON integers

Preserve numeric IDs as exact decimal values. JavaScript `Number` loses integer
precision above `9007199254740991`. Decode responses with a lossless parser
before using an ID in a path; converting an already rounded number to a string
cannot recover it. The Console transport preserves unsafe integers as decimal
text. Python JSON integers also preserve this precision.

Request fields declared as integers require JSON integer tokens, not quoted
strings. A client can retain an exact decimal representation internally and
serialize it as the declared JSON type without converting through a floating
point number. Keep comparison values, resource versions and revision IDs exact.

| Identifier | Obtain and use it |
| --- | --- |
| Tenant, policy, API key and group IDs | Opaque strings from management responses; a display name or slug is not its ID. |
| Job or Workflow definition `id` | Numeric immutable revision ID from the authorized list. An edit can produce a new active ID. |
| `source_uid` | Stable workload source identity across revisions. |
| Instance `id` | Numeric execution-ledger ID for instance details, attempts, cancellation and instance logs. |
| Function `id` | Numeric Function identity, distinct from its active revision. |
| Function history `run_id` | History UUID for Function invocation details and Function logs. |
| Workflow history `id` | Numeric workflow-ledger ID, distinct from its definition ID and child Run IDs. |
| `occurrence_key` | Receipt correlation identity; it is not a ledger ID or Function history UUID. |

A trigger can be accepted before the corresponding ledger row is visible. Keep
its source and occurrence identity and continue bounded observation; see
[trigger and observe](../tasks/trigger-and-observe.md).

## Filters and time windows

Use only parameters declared by the operation. Jobs and Workflows support
literal name/namespace search; Functions support name/description search.
SQL wildcard characters are not a client pattern language.

Instance and Workflow histories apply `created_from` inclusively and
`created_before` exclusively to ledger creation time. Function history uses
invocation trigger time. Bounds use RFC3339 with a timezone and at most nine
fractional digits. A window with equal bounds or reversed bounds is rejected. Histories order by
descending timestamp with descending ID as a tie breaker.

Use URL encoding for query values. For example:

```bash
curl --silent --show-error --fail-with-body --get \
  -H "Authorization: Bearer $ORBITJOB_API_KEY" \
  --data-urlencode 'phase=Succeeded' \
  --data-urlencode 'created_from=2026-11-01T00:00:00Z' \
  --data-urlencode 'created_before=2026-11-16T00:00:00Z' \
  "$ORBITJOB_API_URL/api/v1/instances/summary"
```

Instance summary uses the authorized list filter without page bounds and
requires `instance:List`. It returns `total` and phase counts. It retains the
instance routes' ungrouped-credential restriction.

## Pagination

Ordinary offset-paged collections use declared `limit` and `offset` parameters.
Send an explicit positive limit and nonnegative offset; ordinary page limits
are capped at 100. Most list responses contain `items` without `total` or
`has_more`. Advance the offset by the number received and stop on a short page;
a full page can require a final empty request.

The workspace directory defaults to 20 items and includes `has_more` for visible
matches. It discloses no global total. Log archives use their own cursor and
`has_more` protocol; follow the returned cursor rather than offset rules.
Concurrent writes can shift offset pages. Deduplicate stable IDs and use bounded
time windows for repeatable history investigations.

## Authentication, errors and diagnostics

Automation uses `Authorization: Bearer <issued-key>`. The Console uses a human
session Cookie and exact-Origin/CSRF checks. An explicit Authorization header
takes precedence over a Cookie. Account administration cannot be performed with
API keys; follow [accounts and sessions](../accounts.md).

The standard Admin error envelope has `error.code`, `error.message` and an
optional `error.field`. Use the HTTP status and machine-readable code, without
parsing prose as a stable contract. Authentication, authorization, validation,
conflict, rate limiting and service unavailability require different handling;
see [calling the API](../tasks/call-api.md#handle-failures).

The service forwards `X-Trace-ID` or generates one when absent, and echoes it in
the response. Retain that header, status and error code when requesting operator
help. Keep credentials, capabilities and sensitive response bodies out of
shared request logs. A retry must preserve the operation's original identity,
principal and scope where reconciliation is supported.
