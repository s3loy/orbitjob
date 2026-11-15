# OrbitJob Usage Guide

Use this guide to call the Admin HTTP API, manage Kubernetes-backed workloads
and operate an installation. These instructions describe the current source.
Use a published version's own chart, images and instructions when installing it.

## Before you begin

- HTTP clients and scripts need a dedicated tenant API key with the required
  grants. Jobs and Runs require an ungrouped key.
- Personal-account clients use the server-managed session, explicit workspace
  membership and CSRF contract in [accounts](docs/accounts.md). API keys cannot
  administer accounts.
- Installation administrators prepare PostgreSQL, credentials, storage and
  namespace RBAC through [installation](#7-helm-and-kubernetes). Creating a
  workspace alone does not create its execution environment.
- Source contributors start with the
  [development environment](docs/local-deployment.md). Local `:dev` images are
  separate from release artifacts.

<a id="web-console"></a>

The backend Admin image contains the API and identity tool without a compiled
Console bundle. `/console/` returns 503 when browser assets are absent; API routes
remain available. [Project status](docs/status.md) describes backend behavior;
[verification](docs/verification.md) records the scope of completed checks.

## Calling the API

<a id="8-authentication-and-request-conventions"></a>

### Authentication and request conventions

<a id="81-choose-an-installation-and-credential"></a>

#### Choose an installation and credential

Send API calls to the Admin API of the intended installation. Its
`GET /openapi.json` describes that
installation; the repository [OpenAPI document](api/openapi.yaml) describes
this source tree. A deployment on an older revision can have a different
contract.

The examples below use Bash, curl and, where JSON is assembled from files,
Python 3. Start with a dedicated tenant API key issued by the installation
administrator and loaded into `ORBITJOB_API_KEY` through your local secret
workflow. For the local Kind installation, `source <(make kind-env)` loads the
administrator credential for setup; issue a narrower tenant key for normal use.
Set `ORBITJOB_API` to the API origin supplied by that installation's administrator,
including the scheme and port when needed, without a `/console/` suffix. For the
local port-forward described here, the origin is `http://localhost:18080`.
For a domain or IP installation, use its exposed origin, such as
`https://orbitjob.example.com` or `http://192.0.2.10:30080`.

```bash
ORBITJOB_API='https://<installation-host>'
AUTH="Authorization: Bearer ${ORBITJOB_API_KEY:?load an issued API key first}"
curl -sS --fail-with-body -H "$AUTH" "$ORBITJOB_API/api/v1/identity"
```

Keep credentials out of shared commands, screenshots, source files and request
logs. `AUTH` is a shell convenience, not a server environment variable. A `200`
from `/healthz` confirms public health only; `/api/v1/identity` verifies the key.

Public endpoints require no credential:

```text
GET /healthz
GET /metrics
GET /openapi.json
```

Expose `/openapi.json` through the administrator-managed proxy. Keep `/healthz`
and `/metrics` on the internal Service or a separately controlled administration
path; the chart does not configure an external proxy.

The automation examples below authenticate with an issued API key:

```http
Authorization: Bearer otj_...
```

The OpenAPI security scheme is named `APIKey`; its wire format is the Bearer
header above. The server stores only a bcrypt hash. Unknown, expired and revoked
keys return `401`.

Personal-account clients use sessions through HttpOnly,
SameSite=Strict cookies. Browser credential and capability submissions require
the configured Origin; cookie-authenticated writes also require the current
session's `X-CSRF-Token`. Explicit `Authorization` takes precedence and never
falls back to a session cookie. Human account administration uses the `/auth/*`
routes and cannot be performed with API keys. See
[Accounts and workspace access](docs/accounts.md) for setup, invitation,
session expiry and administrator recovery.

Experiment credentials, foreground experiment APIs and OAuth are not available.
Identity capability results below describe policy decisions, not credentials.

<a id="82-tenant-group-and-permission-scope"></a>

#### Tenant, group and permission scope

Tenant keys cannot switch their authenticated tenant with a query or body
`tenant_id`. Platform keys must explicitly select `tenant_id` for tenant data;
guard evaluation and data access use the same target. Tenant-object management
routes continue to authorize their explicit path tenant.

For a platform key, add `tenant_id` to each tenant-data request's query string,
including detail, trigger, cancel and definition requests. Set it to the exact
target tenant ID returned by an authorized directory or create operation. Do not
put it in a body that has no `tenant_id` field, and do not use an invented
`X-OrbitJob-Tenant-Id` header. Tenant-key examples below omit this query because
their credential already fixes the tenant.

Each operation needs its own action on
`orbitjob:<tenant>:<group-or-*>:<resource-type>/<id-or-*>`. List, read, write and
log access are independent. An authenticated key with no policies grants no
protected operation. Explicit Deny and permission boundaries still apply.

| Journey | Required actions |
|---|---|
| Read Job definitions and execution history | `job:List`, `job:Get`, `instance:List`, `instance:Get` |
| Publish and cancel a Job run | `job:Trigger`, `instance:Cancel` |
| Read / validate and apply live workload specs | `job:Get` / `job:Apply`, or `workflow:Get` / `workflow:Apply` |
| Manage / invoke Functions | `function:Create`, `function:Update` / `function:Invoke`; reads require `function:List`, `function:Get` |
| Read / trigger and cancel Workflows | `workflow:List`, `workflow:Get` / `workflow:Trigger` |
| Read retained task logs | `instance:ReadLogs` or `function:ReadLogs` |

Jobs, Instances, Workflows and typed definition routes require an ungrouped
credential. Functions support the credential's own resource group. A collection
grant does not widen a group restriction. Monitoring and access management
retain their own action and scope checks; consult each reference operation.

`GET /api/v1/identity` returns non-secret key kind, key ID, tenant, group and
boundary identifiers. Platform keys without a target receive metadata and no
capability results. Supply at most 64 paired `action` and `resource` query
values to check exact policy decisions within the selected tenant. This does
not enumerate resources or reveal policy documents:

```bash
curl -sS -G "$ORBITJOB_API/api/v1/identity" -H "$AUTH" \
  --data-urlencode "tenant_id=$TENANT_ID" \
  --data-urlencode 'action=job:Get' \
  --data-urlencode "resource=orbitjob:$TENANT_ID:*:job/42"
```

The identity response can include workspace labels and configured workload
namespaces. Labels and namespace mappings are metadata, not proof of cluster
readiness or Apply permission. `/api/v1/workspaces` searches authorized workspace
labels; it does not switch a tenant key or grant workload access.

<a id="83-ids-filters-and-pagination"></a>

#### IDs, filters and pagination

Preserve int64 IDs as exact decimal text in clients. JavaScript `Number` loses
integer precision above `9007199254740991`; parse responses with a lossless JSON
parser before constructing paths. Converting an already rounded number to a
string cannot restore its ID. Python's JSON integers and quoted shell variables
preserve these values. API request fields declared as integers still need JSON
integer tokens, not quoted strings.

| Identifier | Where to obtain it / where to use it |
|---|---|
| Tenant, key, policy and group IDs | Opaque strings from management responses; not display names or slugs |
| Job / Workflow definition `id` | Numeric immutable revision ID from their lists; edits can produce a new active ID |
| `source_uid` | Stable workload source identity; use it to find revisions or executions of the same source |
| Instance `id` | Numeric ledger run ID; use for Instance detail, attempts, logs and cancel |
| Function `id` | Numeric Function identity; independent of its active revision ID |
| Function history `run_id` | UUID from Function history; use for Function history detail and logs |
| Workflow history `id` | Numeric workflow ledger run ID; independent of its definition ID and step run IDs |
| Trigger `occurrence_key` | Receipt correlation value; not a ledger ID or a Function history UUID |

Jobs and Workflows accept literal `search` over name/namespace; Functions over
name/description. Instance and Workflow history apply `created_from` (inclusive)
and `created_before` (exclusive) to ledger creation time. Function history uses
invocation trigger time and also accepts `offset`. All bounds require RFC3339
with a timezone and at most nine fractional digits; empty/reversed windows are rejected. Histories use descending
timestamp and descending ID as the tie breaker.

```bash
curl -sS -G "$ORBITJOB_API/api/v1/instances/summary" -H "$AUTH" \
  --data-urlencode 'phase=Succeeded' \
  --data-urlencode 'created_from=2026-10-01T00:00:00Z' \
  --data-urlencode 'created_before=2026-11-01T00:00:00Z'
```

The summary returns `total` and `phases` for the same authorized filter as the
Instances list, without page bounds. It requires `instance:List` and retains
the list's ungrouped-key restriction.

Optional trace header:

```http
X-Trace-ID: request-20260713-001
```

The service echoes the value back; one is generated when absent. Retain this
response header with the HTTP status and error code for operator diagnosis.

List APIs use `limit`/`offset` where declared in the reference. Send explicit
positive limits and nonnegative offsets; ordinary paged lists cap `limit` at
100. The workspace directory defaults to 20 and returns `has_more`. Most other
collections return only `{"items":[...]}`; do not invent `total` or `has_more`.
For these collections, advance by the received page length and stop on a short
page; a full page may require one additional empty request. Concurrent writes
can shift offset pages, so deduplicate stable IDs and use time bounds for
repeatable history investigations. Instance `/summary` is the explicit count
operation, not a list pagination field.

<a id="9-tenants-and-api-keys"></a>

### Tenants and API keys

Tenants have a display `name`, a `slug` for a readable workspace reference and
an immutable ID for API addressing. A name or slug does not change the tenant's scope.
Creating a tenant does not provision its execution environment; configure the
namespace mapping and permissions as described in [namespace topology](#71-namespace-topology).

Create a tenant:

```bash
curl -sS -X POST "$ORBITJOB_API/api/v1/tenants" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"slug":"payments","name":"Payments","status":"active"}'
```

Query:

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/tenants?limit=50&offset=0"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/tenants/<tenant_id>"
```

Find workspaces by name or slug without retaining their IDs:

```bash
curl -sS --get -H "$AUTH" "$ORBITJOB_API/api/v1/workspaces" \
  --data-urlencode 'search=Payments' --data 'limit=20' --data 'offset=0'
```

The directory returns `items` with `id`, `name`, `slug` and `status`, plus
`has_more` for the caller's visible results. Search is a trimmed literal
substring of name or slug, with a maximum of 128 characters. Results use stable
name/ID ordering; authorization and search apply before pagination. The default
limit is 20 and the maximum is 100. No global tenant count is exposed.
Own-workspace labels do not require tenant-management privileges. Foreign rows
require effective `tenant:Get` on `orbitjob:<target>:*:tenant/<target>`; explicit
Deny, permission boundaries and group scope remain authoritative. A platform
credential has no implicit own workspace. The directory accepts no scope override
and does not grant access to any listed workspace's workloads or management.

A personal-account workload request selects an active workspace membership
with the `tenant_id` query parameter. Installation-administrator status alone grants no
workload access. Automation using a platform key explicitly selects its target
tenant; a tenant key remains limited to its own tenant.

Create a named API key with a policy selected for its intended work. Replace
`<policy_id>` with an ID returned by the authorized policy list. The management
target is explicit for each operation and does not switch ordinary tenant-key
workload queries.

Installation administrators onboarding another workspace use these explicit
management routes. Each requires its existing `policy:List`, `policy:Create`,
`group:List` or `group:Create` grant on the exact target collection. The server
checks the caller's effective grants before target-scoped access; custom policy
grants cannot exceed those grants, including explicit denies and boundaries.
Ordinary `/policies` and `/resource_groups` queries keep their original scope.

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/tenants/<tenant_id>/policies"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/tenants/<tenant_id>/resource_groups"
curl -sS -X POST "$ORBITJOB_API/api/v1/tenants/<tenant_id>/policies" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"name":"read-job-history","document":{"version":"2026-01-01","statement":[{"effect":"Allow","action":["job:List","job:Get","instance:List","instance:Get"],"resource":["orbitjob:self:*:job/*","orbitjob:self:*:instance/*"]}]}}'
curl -sS -X POST "$ORBITJOB_API/api/v1/tenants/<tenant_id>/resource_groups" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"slug":"payments-services","name":"Payments services"}'
```

```bash
curl -sS -X POST "$ORBITJOB_API/api/v1/tenants/<tenant_id>/api_keys" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"name":"payments-export-worker","policies":["<policy_id>"]}'
```

The optional name is display metadata. Existing requests without `name` remain
compatible; an empty `{}` request still creates a credential without policy
grants, so it can authenticate but cannot use protected capabilities. See the
[OpenAPI schema](api/openapi.yaml) for the optional boundary and group fields.
The `key` in the create response is shown once in plaintext; save it immediately.
List APIs return metadata, including the name and key prefix, without plaintext.

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/tenants/<tenant_id>/api_keys"
curl -sS -X POST -H "$AUTH" "$ORBITJOB_API/api/v1/api_keys/<key_id>/revoke"
```

Rename an active key without rotating its secret or changing grants:

```bash
curl -sS -X PATCH "$ORBITJOB_API/api/v1/api_keys/<key_id>" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"name":"payments-export-production"}'
```

The response is `{"id":"<key_id>","name":"payments-export-production"}`.
This operation requires `apikey:Update` for the key's actual owner and scope;
clearing the name with an empty string leaves an unnamed key. It does not issue
a replacement plaintext, update policy bindings or reactivate a revoked key.

If a plaintext key is lost, request a replacement with the required policies,
switch the person or service to the replacement, then revoke the old credential.
Use its name, prefix, creation time and ID to identify the old record; a label
does not recover or prove possession of its secret. If the old key may be exposed,
revoke it promptly and coordinate the replacement with the installation owner.

Management endpoints enforce explicit action/resource policy grants. A key
without grants can authenticate but cannot access protected capabilities.
Revocation authorizes `apikey:Revoke` on the target key's actual owner tenant
and key ID. Tenant credentials cannot revoke foreign keys; foreign and absent
keys produce the same denial. Platform credentials still require an applicable
grant on that exact target.
Never expose the bootstrap key to ordinary business callers; use a dedicated
tenant key with the required grants, and put a gateway ACL in front of
untrusted networks.

<a id="10-jobs-runs-functions-and-workflows"></a>

### Jobs, runs, Functions and Workflows

<a id="101-job-definitions-the-scheduledjob-cr"></a>

#### Job definitions (the ScheduledJob CR)

Job definitions are `ScheduledJob` custom resources, materialized into
`job_definition_revisions` by the Operator. The following endpoints read those
immutable revisions; creation and updates use the separate typed
`/api/v1/definitions/jobs/{namespace}/{name}` validation/apply contract described
under [typed definition authoring](#104-create-or-update-a-typed-kubernetes-definition). These writes update the Kubernetes resource,
not the Operator-owned execution ledger.

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/jobs?limit=50"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/jobs/<job_id>"
```

Alternatively, declare a definition through GitOps in a namespace already
mapped to its tenant. Save this YAML as `reconcile-billing.yaml` before applying it:

```yaml
apiVersion: workloads.orbitjob.io/v1alpha1
kind: ScheduledJob
metadata:
  name: reconcile-billing
  namespace: your-tenant-namespace
spec:
  schedule: "17 3 * * *"
  concurrencyPolicy: Forbid
  misfirePolicy: Skip
  timeoutSeconds: 300
  retryPolicy:
    maxAttempts: 3
  history:
    successfulRuns: 10
    failedRuns: 10
  jobTemplate:
    image: registry.example.com/jobs/reconcile
    command: ["/app/reconcile"]
    args: ["--tenant", "payments"]
```

```bash
kubectl -n '<tenant-namespace>' apply -f reconcile-billing.yaml
```

| Field | Values / meaning |
|---|---|
| `schedule` | standard five-field cron; every due occurrence produces one run |
| `concurrencyPolicy` | `Allow`, `Forbid`, `Replace` |
| `misfirePolicy` | `Skip`, `FireOnce`, `CatchUpBounded` |
| `timeoutSeconds` | per-run deadline, written to the K8s Job's `activeDeadlineSeconds` |
| `retryPolicy.maxAttempts` | total attempt budget, minimum 1 (unset means 1, not unlimited retries) |
| `history` | terminal runs retained by outcome (`successfulRuns`/`failedRuns`); 0 is the default, not unlimited |
| `suspend` | `true` pauses triggering |
| `jobTemplate` | `image`, `command`, `args`, `backoffLimit` |

Source admission applies the active source's Forbid policy
atomically to ScheduledJob cron, manual and Workflow-step requests. A prior
occurrence returns its existing identity before admission is evaluated; its
execution payload stays pinned. Accepted manual requests blocked by Forbid wait
as Pending/ConcurrencyBlocked without a new ledger row. Cron skip/misfire rules
remain in force; Replace keeps its existing cron cancellation behavior. Check
and Function synthetic sources retain Allow. See
[architecture](docs/architecture.md#execution-recovery-and-concurrent-admission)
and [verification](docs/verification.md).

<a id="102-manual-trigger"></a>

#### Manual trigger

Set `JOB_ID` to a reviewed active revision returned by the Jobs API. No request
body is required. Use one new idempotency key per intended execution, and keep
that value when recovering the same request:

```bash
JOB_ID='<active revision id>'
curl -sS --fail-with-body -H "$AUTH" \
  "$ORBITJOB_API/api/v1/jobs/$JOB_ID" -o job-reviewed.json
SOURCE_UID="$(python3 -c 'import json; print(json.load(open("job-reviewed.json"))["source_uid"])')"
IDEMPOTENCY_KEY="$(python3 -c 'import uuid; print(uuid.uuid4())')"
curl -sS --fail-with-body -X POST "$ORBITJOB_API/api/v1/jobs/$JOB_ID/trigger" \
  -H "$AUTH" -H "X-OrbitJob-Idempotency-Key: $IDEMPOTENCY_KEY" \
  -o trigger-receipt.json
```

A first publication returns `201`; replay returns `200` with the original
resource reference. The receipt contains `namespace`, `name`, `occurrence_key`,
`trigger`, `phase` and `created`. It has no ledger run ID, and its phase can be
empty while the Operator has not observed it. HTTP success confirms publication,
not execution success. The actor is the authenticated key, not a supplied header.

Read the exact occurrence from the ledger after publication:

```bash
OCCURRENCE_KEY="$(python3 -c 'import json; print(json.load(open("trigger-receipt.json"))["occurrence_key"])')"
curl -sS --fail-with-body -G "$ORBITJOB_API/api/v1/instances" -H "$AUTH" \
  --data-urlencode "source_uid=$SOURCE_UID" \
  --data-urlencode "occurrence_key=$OCCURRENCE_KEY" --data 'limit=100'
```

An empty page can mean projection is still pending. Use a bounded observation
window and backoff; retain the receipt if it expires. Once present, use the
Instance `id` for detail, attempts and cancellation. Trigger permission does not
grant these reads. A `403` on observation does not undo publication or justify a
new trigger. Idempotency headers are trimmed and limited to 128 characters;
omitting one gives each call a new execution identity. They apply to the pinned
definition revision, so a new active revision is a different request target.

<a id="103-runs-and-phases"></a>

#### Runs and phases

Every run (scheduled or manual) is a `JobRun` CR; the operator renders it into a
Kubernetes Job and writes the ledger. Phase transitions:

```text
Pending -> CreatingAttempt -> Running -> Succeeded
                              |-> RetryWaiting -> CreatingAttempt (next attempt)
                              |-> ExecutionUnknown -> Running / RetryWaiting / Succeeded
                              `-> Failed
Pending/CreatingAttempt/Running/RetryWaiting/ExecutionUnknown -> CancelRequested -> Canceled
                                                        `-> CancelUnknown
```

`ExecutionUnknown` is the accepted nonterminal recovery state for a bound Job
that is missing or whose name now resolves to another UID. The Operator must
first recover any trusted terminal attempt fact, retain original identity and
avoid creating another attempt while observation is uncertain. This run keeps
its Forbid slot and produces no terminal result or SLI sample. Attempts expose
`owner_run_uid`, `execution_unknown_since` and `execution_unknown_reason` when
known. Permission, throttling and transport failures are observation errors,
not proof that a Job is absent. See
[execution recovery](docs/architecture.md#execution-recovery-and-concurrent-admission)
for the recovery rules and [verification](docs/verification.md) for exercised scenarios.

`CancelUnknown` means the cancel outcome could not be confirmed. Workload Pods
use `restartPolicy: Never`; a terminal failed Job ends one platform attempt.
Configured Job backoff can create another Pod within that attempt, and platform
retry can create another attempt. Neither is exactly-once external execution.

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/instances?phase=Failed&limit=50"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/instances/<run_id>"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/instances/<run_id>/attempts"
```

Cancel takes no request body:

```bash
curl -sS -X POST -H "$AUTH" "$ORBITJOB_API/api/v1/instances/<run_id>/cancel"
```

Cancelling patches `spec.cancelRequested` on the owned JobRun CR. A failed
execution attempt remains Failed if its logical run is canceled while waiting
to retry. New workload Pods carry the `orbitjob.io/termination-evidence`
finalizer from creation. For an active bound attempt, the Operator shortens the
original Job's execution deadline with UID and resourceVersion checks, then
waits for its controller to acknowledge stopping. An existing controller
completion condition can also supply this acknowledgement. The Operator then
validates the complete owned-Pod inventory, commits trustworthy container exit
evidence to PostgreSQL and removes only its own finalizer with Pod UID and
resourceVersion checks. It requests foreground Job deletion with the original
UID only after evidence release, and confirms `Canceled` after the Job is gone
and the committed proof remains valid. The stopped Kubernetes Job may report
`DeadlineExceeded` while the logical run becomes `Canceled`.

Normal termination stays `CancelRequested` within the five-minute confirmation
window. Reconciliation retries and Operator restarts do not restart that window.
Job deletion does not acknowledge completion of in-flight Pod creation.
Removal, an empty Pod inventory or a terminal phase alone does not establish
that workload code stopped. Expiration without confirmation becomes
`CancelUnknown`. A new execution can complete later if its full inventory is
still protected by the evidence finalizers or an existing committed proof.
Identity conflicts, node loss and definite evidence gaps remain isolated;
a later empty or narrower inventory cannot clear them. Existing workloads
without the finalizer receive no reconstructed guarantee. Unavailable
observations are retried without inventing a stop fact. See
[verification](docs/verification.md) for cancellation and recovery checks.
A committed Workflow stop prevents late step publication and new attempts.

When a ledger row exists but its CR is missing, the API returns `409 Conflict`;
a run that never existed returns `404`. An accepted cancellation response is
intent, not proof of termination. Terminal runs remain final.

<a id="104-create-or-update-a-typed-kubernetes-definition"></a>

#### Create or update a typed Kubernetes definition

The live definition path addresses a Kubernetes name; Jobs/Workflows lists
address projected numeric revision IDs. Use a namespace mapped to this tenant
and a valid Kubernetes resource name. For an update, first `GET` the same path
and retain its `uid`, `resource_version` and full `spec_json` baseline.

This create example makes a manual-only ScheduledJob. Replace the image with
one your cluster can pull. The body contains only a typed `spec`, not a full
Kubernetes manifest. Python preserves integer values while encoding that object
as the required `spec_json` string:

```bash
NAMESPACE='<mapped workload namespace>'
DEFINITION_NAME='nightly-export'
DEFINITION_URL="$ORBITJOB_API/api/v1/definitions/jobs/$NAMESPACE/$DEFINITION_NAME"
cat > workload-spec.json <<'JSON'
{"schedule":"","jobTemplate":{"image":"registry.example.com/team/task:reviewed"},"timeoutSeconds":300,"retryPolicy":{"maxAttempts":1},"history":{"successfulRuns":3,"failedRuns":3}}
JSON
python3 - <<'PY'
import json, uuid
spec = json.load(open('workload-spec.json'))
request = dict(request_id=str(uuid.uuid4()), expected_uid='',
               expected_resource_version='', spec_json=json.dumps(spec), dry_run=True)
with open('definition-dry-run.json', 'w') as f:
    json.dump(request, f)
PY
curl -sS --fail-with-body -X PUT "$DEFINITION_URL" -H "$AUTH" \
  -H 'Content-Type: application/json' --data-binary @definition-dry-run.json \
  -o definition-validated.json
```

The dry-run performs cluster admission without saving. Review the returned
canonical `spec_json` and `manifest_digest` before preparing the actual write:

```bash
python3 - <<'PY'
import json
request = json.load(open('definition-dry-run.json'))
validated = json.load(open('definition-validated.json'))
assert validated['state'] == 'validated'
request.update(spec_json=validated['spec_json'],
               manifest_digest=validated['manifest_digest'], dry_run=False)
with open('definition-apply.json', 'w') as f:
    json.dump(request, f)
PY
curl -sS --fail-with-body -X PUT "$DEFINITION_URL" -H "$AUTH" \
  -H 'Content-Type: application/json' --data-binary @definition-apply.json
```

For an update, supply both expectations from the initial GET, edit the complete
spec and repeat validation. A stale version or replaced UID returns `409`.
Read and compare the live spec before adopting another resourceVersion; never
replace the expected UID to overwrite a different resource. `state:accepted`
means Kubernetes accepted the write. Observe the exact returned UID/generation
in the projected definition before triggering it. Definition metadata is not
part of the writable schema, and there is no definition-delete API.

If the response is lost, retain the original request ID and digest and GET the
definition. Matching `last_request_id` and `last_manifest_digest` establish that
write's identity; otherwise keep the outcome uncertain until reconciled. Do not
automatically send a newly identified write. Applying an unsuspended nonempty
schedule can enable automatic execution, independently of manual Trigger grants.

<a id="105-manage-and-invoke-a-function"></a>

#### Manage and invoke a Function

Functions execute a container with the command and arguments saved in its
definition. Invocation accepts no request body. `POST /functions` requires
`function:Create` and a required idempotency header. Create identity is scoped
to tenant, authenticated actor and idempotency key; an identical normalized request returns the original
snapshot, even after later edits. A different definition with that identity is
a conflict. Replace the example image with an accessible workload image:

```bash
FUNCTION_CREATE_KEY="$(python3 -c 'import uuid; print(uuid.uuid4())')"
curl -sS --fail-with-body -X POST "$ORBITJOB_API/api/v1/functions" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -H "X-OrbitJob-Idempotency-Key: $FUNCTION_CREATE_KEY" \
  -d '{"name":"report-export","image":"registry.example.com/team/task:reviewed","status":"active","timeout_seconds":60,"retry_limit":0,"history_success":3,"history_failed":3}' \
  -o function-created.json
FUNCTION_ID="$(python3 -c 'import json; print(json.load(open("function-created.json"))["id"])')"
curl -sS --fail-with-body -H "$AUTH" \
  "$ORBITJOB_API/api/v1/functions/$FUNCTION_ID" -o function-reviewed.json
```

The returned `version` is the saved definition version. `ready:true` means it
is active and `active_revision_version` matches it; projection is asynchronous.
Read until that exact saved version is ready within a bounded deadline. A
later version needs a new review. Invoke the version you actually reviewed:

```bash
FUNCTION_VERSION="$(python3 -c 'import json; d=json.load(open("function-reviewed.json")); assert d["ready"]; print(d["version"])')"
FUNCTION_INVOKE_KEY="$(python3 -c 'import uuid; print(uuid.uuid4())')"
curl -sS --fail-with-body -X POST \
  "$ORBITJOB_API/api/v1/functions/$FUNCTION_ID/invoke?expected_version=$FUNCTION_VERSION&wait_seconds=30" \
  -H "$AUTH" -H "X-OrbitJob-Idempotency-Key: $FUNCTION_INVOKE_KEY"
curl -sS --fail-with-body -H "$AUTH" \
  "$ORBITJOB_API/api/v1/functions/$FUNCTION_ID/runs?limit=50&offset=0"
```

Invoke takes no body. `wait_seconds` is 0–60; timeout returns the last observed
phase and receipt, not an invented terminal result. A paused Function, changed
reviewed version or pending matching revision returns `409`. `201` versus `200`
reflects new versus replayed publication, not success versus failure. Function
executions run in Kubernetes Pods; replaying the same invocation identity does
not create another execution. The receipt contains neither a Function HTTP
response body nor log text.

History records terminal outcomes (`success`, `failed`, `canceled`) only. Match
its non-null `occurrence_key` to the invocation receipt and use its UUID
`run_id` in `GET /functions/{id}/runs/{run_id}`. A null occurrence mapping is
unknown; never derive one from the UUID. List rows have `attempts:null`; detail
returns retained attempt facts and `last_attempt`. Null means unavailable,
while `[]` confirms no retained attempt. Do not guess attempt 1 for logs.

Update uses `PUT /functions/{id}` with the **complete** Function definition plus
the latest `expected_version`; omitted options take replacement defaults.
Copy only writable fields from GET into an update file, excluding IDs,
readiness and timestamps. To pause or resume, replace `status` with `paused` or
`active` in that full body and review the new saved version/readiness. There is
no separate Function pause, delete or group-scoped cancel endpoint. Ungrouped
callers with independent Instance permissions can observe and cancel the
underlying exact occurrence through the Instance API.

```bash
python3 - <<'PY'
import json
reviewed = json.load(open('function-reviewed.json'))
fields = ('name', 'description', 'status', 'image', 'command', 'args',
          'timeout_seconds', 'retry_limit', 'history_success', 'history_failed', 'labels')
request = {field: reviewed[field] for field in fields}
request.update(expected_version=reviewed['version'], status='paused')
with open('function-update.json', 'w') as f:
    json.dump(request, f)
PY
curl -sS --fail-with-body -X PUT "$ORBITJOB_API/api/v1/functions/$FUNCTION_ID" \
  -H "$AUTH" -H 'Content-Type: application/json' --data-binary @function-update.json
```

<a id="106-trigger-observe-and-cancel-a-workflow"></a>

#### Trigger, observe and cancel a Workflow

Create/update a WorkflowJob through `/definitions/workflows/{namespace}/{name}`
using the same dry-run, canonical-digest and UID/version flow as Jobs. Its spec
contains `tasks` whose `jobRef.name` references ScheduledJobs in the same
namespace; it does not embed arbitrary images or Pod specs. Read the active
Workflow revision and its DAG before triggering.

```bash
curl -sS --fail-with-body -H "$AUTH" "$ORBITJOB_API/api/v1/workflows?limit=50"
WORKFLOW_ID='<reviewed active workflow revision id>'
curl -sS --fail-with-body -H "$AUTH" "$ORBITJOB_API/api/v1/workflows/$WORKFLOW_ID"
WORKFLOW_TRIGGER_KEY="$(python3 -c 'import uuid; print(uuid.uuid4())')"
curl -sS --fail-with-body -X POST "$ORBITJOB_API/api/v1/workflows/$WORKFLOW_ID/runs" \
  -H "$AUTH" -H "X-OrbitJob-Idempotency-Key: $WORKFLOW_TRIGGER_KEY" \
  -o workflow-receipt.json
curl -sS --fail-with-body -H "$AUTH" \
  "$ORBITJOB_API/api/v1/workflows/$WORKFLOW_ID/runs?limit=50&offset=0"
```

Trigger takes no body and returns a WorkflowRun receipt, not its numeric ledger
ID. Match `occurrence_key` against the paged workflow history; the list has no
occurrence filter. Once found, set `WORKFLOW_RUN_ID` to that row's numeric `id`:

```bash
WORKFLOW_RUN_ID='<matching workflow history id>'
curl -sS --fail-with-body -H "$AUTH" \
  "$ORBITJOB_API/api/v1/workflows/$WORKFLOW_ID/runs/$WORKFLOW_RUN_ID"
curl -sS --fail-with-body -X POST -H "$AUTH" \
  "$ORBITJOB_API/api/v1/workflows/$WORKFLOW_ID/runs/$WORKFLOW_RUN_ID/cancel"
```

Detail exposes the pinned DAG and step ledger IDs. Use those step IDs for
Instance attempts and independently authorized logs. If
`pinned_revision_available:false` and `tasks:null`, historical graph metadata is
unavailable; use recorded step/task decisions without substituting today's DAG.
Retained historical parent revision IDs remain readable; triggers use active
revisions. Cancel requires `workflow:Trigger`; the Operator fans the intent out
to unfinished steps. Its `200` response does not prove all Pods have stopped.

For automatic scheduling, validate five-field cron and an IANA time zone:

```bash
curl -sS --fail-with-body -X POST "$ORBITJOB_API/api/v1/workflows/schedule-preview" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"schedule":"0 9 * * 1-5","time_zone":"Asia/Shanghai","count":5}'
```

Preview requires ungrouped `workflow:Apply`; `count` is 1–20 and `upcoming`
contains future UTC instants. The saved spec uses camelCase `timeZone`, while
this preview body uses `time_zone`. Workflow concurrency supports `Allow` or
`Forbid`, missed starts are skipped, and `suspend:true` pauses automatic starts
while manual trigger remains available. A scheduled Forbid collision retains a
terminal Rejected skip receipt with reason `concurrency_forbid`. Under the
manual-admission contract, a manually accepted WorkflowRun instead stays
Pending/ConcurrencyBlocked without a ledger row or steps until it can enter.
Unknown child executions prevent new steps and retain the parent's concurrency
slot while already running siblings remain observable. The
[verification record](docs/verification.md) describes the exercised scenarios.

<a id="107-read-retained-attempt-logs"></a>

#### Read retained attempt logs

For durable history and reconnectable live observation, use
`GET /api/v1/instances/{run_id}/logs/archive?after=0&limit=500` or the equivalent
Function history `/logs/archive` route. Pass the returned `next` cursor as
`after`; `attempt` and `search` filter retained evidence. The `/download` suffix
exports retained output and gap markers. Independent ReadLogs grants apply.
An audited `DELETE` on the archive base route requires DeleteLogs and returns
an asynchronous request receipt. See [the archive guide](docs/execution-logs.md).
The snapshot routes below remain compatible for older clients and legacy runs.

Use the positive attempt number actually returned by Instance attempts or
Function invocation detail. It is not the attempt-row ID:

```bash
RUN_ID='<instance ledger id>'
ATTEMPT='<retained attempt number>'
curl -sS --fail-with-body -G \
  "$ORBITJOB_API/api/v1/instances/$RUN_ID/attempts/$ATTEMPT/logs" -H "$AUTH" \
  --data 'tail_lines=100' --data 'limit_bytes=16384'
FUNCTION_RUN_UUID='<function history run_id>'
curl -sS --fail-with-body -G \
  "$ORBITJOB_API/api/v1/functions/$FUNCTION_ID/runs/$FUNCTION_RUN_UUID/attempts/$ATTEMPT/logs" \
  -H "$AUTH" --data 'tail_lines=100' --data 'limit_bytes=16384'
```

`tail_lines` is 1–500 and `limit_bytes` is 1–65536. Inspect `state`, `reason`
and `truncated` before interpreting `text`; HTTP `200` can report starting,
absent or unavailable evidence. The server verifies immutable ownership and
resolves namespace/Pod/container locators itself. Caller-supplied locators are
rejected. Logs are bounded snapshots with a five-second request deadline, not
streams or durable archives. Deleting the source JobRun/Pod can remove them.
If the Pod is gone but its verified Job remains, `reason` includes the Job's
recorded failure reason and message, such as `DeadlineExceeded`. This is platform
diagnostic evidence; `state` remains `absent` and `text` stays empty because
container output was not recovered.

<a id="11-scheduledjob-reference-and-execution-semantics"></a>

### ScheduledJob reference and execution semantics

- Runs execute as `batch/v1` Jobs rendered from the definition's `jobTemplate`
  in the namespace mapped to their tenant. Their Pod spec disables service-account
  token mounting, including in namespaces created after chart installation.
- The platform owns retry: attempt counts live in the ledger (`attempt`/`max_attempts`),
  and `backoffLimit` is handed to Kubernetes for in-Job pod retries.
- History: the in-process `exec`/`http`/`webhook`/`pg_notify`/`container` handlers were removed
  with the worker execution path. User code executes only in the user's container image.

<a id="12-checks-slis-and-slos"></a>

### Checks, SLIs and SLOs

<a id="121-checks"></a>

#### Checks

Only `http_health` is currently supported. A check executes as a Kubernetes
Job — a digest-pinned `curlimages/curl` probe, methods GET/HEAD/OPTIONS only —
and interval schedules must be at least 30 seconds:

```bash
curl -sS -X POST "$ORBITJOB_API/api/v1/checks" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{
    "name":"public-api-health",
    "check_type":"http_health",
    "check_config":{"url":"https://example.com/health","method":"GET","expected_status":200},
    "assertion_rules":[{"metric":"response_time_ms","operator":">","threshold":500,"severity":"warning"}],
    "schedule_type":"interval",
    "interval_sec":60,
    "timeout_sec":10
  }'
```

The example stores an assertion rule for a warning when response time exceeds
500ms. Assertion rules accept `>`, `<`, `==`, `!=`, `>=`, `<=`, but the current
container probe does not evaluate `assertion_rules` or produce warning severity.
It checks the configured URL, method and expected HTTP status within the timeout;
Check results are derived from the execution outcome.

Keep the returned numeric Check `id`. A Check's execution source is
`check-<id>`; Check responses do not contain a `source_uid` field. A Check run's
UUID reference is distinct from the numeric Instance ledger ID. Read the
recorded Check run by its own numeric `id`:

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/checks?status=active"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/check-runs?check_id=<id>"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/check-runs/<check_run_id>"
```

Check pause, resume and delete all require the current `version`; delete also
requires a JSON body. Fetch a fresh version before each separate operation.
For example, pause a Check using a response-derived version:

```bash
CHECK_ID='<created check id>'
curl -sS --fail-with-body -H "$AUTH" \
  "$ORBITJOB_API/api/v1/checks/$CHECK_ID" -o check-current.json
python3 - <<'PY'
import json
with open('check-version.json', 'w') as f:
    json.dump({'version': json.load(open('check-current.json'))['version']}, f)
PY
curl -sS --fail-with-body -X POST "$ORBITJOB_API/api/v1/checks/$CHECK_ID/pause" \
  -H "$AUTH" -H 'Content-Type: application/json' --data-binary @check-version.json
```

Resume uses `/checks/{id}/resume` with a newly read version. Delete uses the
same version-body shape and a new read; it does not use a query parameter:

```bash
curl -sS -X DELETE "$ORBITJOB_API/api/v1/checks/<id>" \
  -H "$AUTH" -H 'Content-Type: application/json' -d '{"version":2}'
```

<a id="122-slis"></a>

#### SLIs

```bash
curl -sS -X POST "$ORBITJOB_API/api/v1/slis" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{
    "name":"public-api-availability",
    "sli_type":"availability",
    "source_type":"job_run",
    "source_config":{"source_uid":"<source_uid>"},
    "aggregation":"ratio",
    "good_event_criteria":{"status":"success"}
  }'
```

SLIs generate five-minute UTC buckets from run-ledger events. `source_type` is
`job_run` and `source_config.source_uid` names the source definition. Use the
actual source identity from authorized reads: `check-` followed by the returned
Check ID, a Job's returned `source_uid` (the ScheduledJob CR UID), or `function-`
followed by the actual Function ID. The
source must be active and visible in the caller's tenant/group.
`good_event_criteria` accepts only `status` (`success` or `failed`) and
`duration_ms` (a finite nonnegative number, or an object such as
`{"op":"<=","value":500}`). Availability/quality require nonempty criteria;
latency requires `duration_ms`. Custom criteria may be empty, which counts every
produced event as good. Unknown fields/operators are rejected. Read definitions
through `GET /slis` and `GET /slis/{id}`; readable configuration does not prove
recorded samples.

Terminal projection replay is idempotent: the same tenant/run/SLI fact increments
a bucket once. Failed derivatives remain pending for retry and protect their run
history from pruning. This does not imply exactly-once workload execution.

Deleting an SLI requires the current `version`:

```bash
curl -sS -X DELETE "$ORBITJOB_API/api/v1/slis/<id>" \
  -H "$AUTH" -H 'Content-Type: application/json' -d '{"version":2}'
```

<a id="123-slos-budgets-and-alerts"></a>

#### SLOs, budgets and alerts

```bash
curl -sS -X POST "$ORBITJOB_API/api/v1/slos" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{
    "name":"public-api-99.9",
    "sli_id":9,
    "target":0.999,
    "window_type":"rolling",
    "window_duration":"720h",
    "alert_fast_burn_rate":14.4,
    "alert_slow_burn_rate":6
  }'
```

Replace the example `sli_id` with the ID returned by SLI creation. An SLO can
reference only a visible SLI in the same tenant and applicable resource group.
`window_duration` uses Go duration format, e.g. `168h` or `720h`; see OpenAPI for
its bounds. Burn thresholds must be finite, and slow burn must be below fast
burn. A target of `1` is supported with explicit zero-budget behavior.

Replace example `sli_id:9` with the exact numeric ID returned when creating or
reading the SLI. It must be accessible in the same tenant and permitted group.
Use the SLO's returned numeric ID for budgets and alerts:

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/slos/<id>"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/slos/<id>/budget"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/slos/<id>/budgets"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/slo-alerts?slo_id=<id>&status=firing"
```

SLO pause, resume and delete all require the current `version` in a JSON body,
using the same fresh-GET pattern as Checks. The routes are
`POST /slos/{id}/pause`, `POST /slos/{id}/resume` and `DELETE /slos/{id}`.
`GET /slo-alerts/{id}` reads one alert; alert acknowledgment is not exposed.
With no Check/SLI samples the budget may be empty; that is not an API error.
Zero samples and a zero allowance do not establish observed health or a usable
burn-rate comparison. Monitoring permissions remain independent from execution
permissions. Group keys can manage their own Check/SLI/SLO definitions and read
independently authorized Check results, budgets and alerts within the parent
Check/SLO's group. Detail and single-SLO budget/history authorization uses the
actual parent group and ID even for ungrouped credentials, so a group-specific
Deny remains effective; a foreign-group parent returns 404, including history.
CheckRun/Alert collection access must cover the entire tenant or credential-group
query domain. Relevant per-ID/group Deny, boundary restrictions or an exhausted
proof budget return 403 before reading items/counts; filters and pagination do
not bypass this check or produce a silently filtered partial response. The
optional embedded `current_budget` remains omitted for group-scoped callers.
For ungrouped callers it requires `slo:GetBudget` on the actual SLO group and ID,
including explicit Deny.

<a id="13-errors-rate-limits-and-optimistic-locking"></a>

### Errors, rate limits and optimistic locking

Error structure:

```json
{"error":{"code":"VALIDATION_ERROR","message":"is required","field":"name"}}
```

| HTTP | code | Scenario |
|---:|---|---|
| 400 | `MALFORMED_REQUEST` | JSON or Content-Type cannot be parsed |
| 400 | `VALIDATION_ERROR` | field or domain rule failed |
| 401 | `UNAUTHORIZED` | invalid Bearer key |
| 403 | `FORBIDDEN` | insufficient permission |
| 404 | `NOT_FOUND` | resource does not exist |
| 409 | `CONFLICT` | stale version, state conflict, unique-key conflict |
| 413 | `MALFORMED_REQUEST` | Admin request body exceeds the shared size limit |
| 429 | `RATE_LIMITED` | tenant token bucket exhausted |
| 500 | `INTERNAL_ERROR` | unmapped error |
| 503 | `SERVICE_UNAVAILABLE` | dependency unavailable |

`field` is optional. Use the HTTP status and machine-readable `code` for client
behavior; messages identify the current failure rather than a stable enum.
Authorization can deliberately hide whether a foreign resource exists.

Rate-limit configuration is owned by the process
[environment examples](deploy/env/admin-api.env.example) and Helm
[`adminapi.rateLimits`](charts/orbitjob/values.yaml). Those installation settings
can differ from standalone code defaults. The `trigger` bucket covers generic
Job trigger and Instance cancel. Function invoke and Workflow lifecycle routes
use the write bucket in the current implementation. `429` includes
`Retry-After` in seconds; wait at least that duration, use backoff and retain the
same execution intent. Do not assume each API key has an independent budget.

| Result | Safe next step |
|---|---|
| `400` | Correct the named input; repeating the same invalid payload cannot help |
| `401` | Load a valid unexpired credential; public health cannot validate a key |
| `403` | Check exact action, target tenant/group and boundaries with the administrator; do not broaden queries to bypass scope |
| `404` | Recheck the resource's ID kind, parent and tenant; pending projection may not yet have a readable ledger row |
| `409` on a versioned write | GET current state, compare with the reviewed baseline, then explicitly decide whether to retry using the actual current version |
| `429` | Honor `Retry-After`, then back off within a bounded retry budget |
| Timeout / dropped write response / `5xx` | Treat outcome as unknown; reconcile through a read or the documented same-identity replay before starting another write |

Do not increment `version` without reading and reviewing the current state. Function update uses `expected_version`;
typed definition writes use UID/resourceVersion plus request ID and digest.
These are different concurrency contracts. A successful write followed by a
denied detail read still created the resource; retain its receipt and do not
repeat its POST. Tenant, policy, group, API-key, Check, SLI and SLO creation have
no general idempotency header contract. Reconcile an uncertain create through
an authorized directory and installation audit before another attempt. The
documented Job/Workflow trigger, Function create/invoke and definition-write
identities do not make unrelated POST endpoints automatically retryable.

<a id="17-api-index"></a>

### API index

Full schema, request fields and response models:

- Repository file: `api/openapi.yaml`
- At runtime: `GET /openapi.json`

The generated reference lists every operation, parameter, security requirement,
request schema and response model. This index groups the operational journeys;
it does not redefine their schemas. Paths below have the `/api/v1` prefix.

| Area | Supported operations / guide |
|---|---|
| Identity and discovery | `GET /identity`, `GET /workspaces`; [authentication and scope](#8-authentication-and-request-conventions) |
| Personal accounts | `/auth/*` setup, login, invitations, session management, password confirmation and reset; [accounts and workspace access](docs/accounts.md). These routes use human sessions or explicit one-time capabilities, not API keys. |
| Workspaces and credentials | Tenant create/list/get; tenant API-key create/list; key rename/revoke; [tenant and key workflow](#9-tenants-and-api-keys) |
| Policies and groups | Policy create/list/get/delete; group create/list; explicit tenant policy/group create/list; [management scope](#9-tenants-and-api-keys) |
| Live definitions | Job/Workflow typed GET and PUT; [validation, apply and reconciliation](#104-create-or-update-a-typed-kubernetes-definition) |
| Jobs | Active revision list/get; manual trigger; [publication and exact occurrence lookup](#102-manual-trigger) |
| Instances | List, summary, get, attempts and cancel; [execution phases](#103-runs-and-phases) |
| Source execution controls | GET/PATCH `/<kind>/<id>/execution-controls` for Jobs, Workflows, Functions and Checks; [maintenance and safe resumption](docs/local-maintenance.md) |
| Functions | Create/list/get/update, invoke, terminal history list/get; [versioned invocation](#105-manage-and-invoke-a-function) |
| Workflows | Definition list/get, schedule preview, run trigger/list/get/cancel; [workflow journey](#106-trigger-observe-and-cancel-a-workflow) |
| Logs | Instance and Function archives, downloads and audited deletion; [execution logs](docs/execution-logs.md). Legacy [attempt snapshots](#107-read-retained-attempt-logs) remain available while Kubernetes resources are retained. |
| Checks and results | Check create/list/get/pause/resume/delete; result list/get; [Check journey](#121-checks) |
| SLIs | Create/list/get/delete; [source identity and criteria](#122-slis) |
| SLOs, budgets and alerts | SLO create/list/get/pause/resume/delete; current and historical budget reads; alert list/get; [reliability journey](#123-slos-budgets-and-alerts) |

Unauthenticated `/healthz`, `/metrics` and `/openapi.json` are outside that prefix.
Keep health and metrics endpoints outside the administrator-managed public route.
The API does not support arbitrary Kubernetes
resource access, definition deletion, Function payloads or result bodies,
rebinding an existing API key's policies, alert acknowledgment or experiments.

## Installation and operations

These procedures require installation-administrator access to the cluster and
database. Application users call the API with their issued
credentials; they do not perform cluster setup.

<a id="1-choosing-a-deployment-method"></a>

### Choosing a deployment method

| Method | Use case | Database | Process topology | Status |
|---|---|---|---|---|
| **kind + Helm** | Development and disposable tests | Helper-provisioned PostgreSQL | Admin API, scheduler, operator, log archive and collector | Development guide below |
| **Helm on an existing Kubernetes cluster** | Managed installation | Configured PostgreSQL | Admin API, scheduler, operator, log archive and collector | Configure admission, credentials, storage and backups |
| Individual binaries | Development and custom process packaging | Migrated PostgreSQL | Component-specific | admin-api/operator still require in-cluster Kubernetes credentials |

> Historical note: the Docker Compose local stack was removed together with the worker execution path; use kind + Helm for local development.

**Source development**: use kind + Helm for the complete environment; see [`docs/local-deployment.md`](docs/local-deployment.md).

<a id="7-helm-and-kubernetes"></a>

### Helm and Kubernetes

The chart lives in [charts/orbitjob/](charts/orbitjob/); [Chart.yaml](charts/orbitjob/Chart.yaml)
defines its version. This procedure is for an installation administrator and
assumes Go 1.27.3+ for the configuration helper, Python 3 and OpenSSL for
Secret preparation, an existing PostgreSQL instance, cluster-administrator access for chart
resources, persistent storage and a complete set of component images built from
the same source revision. For published software, use that release's chart and
instructions. Older images do not supply the current account and log-archive
contracts; do not rely on the chart's default appVersion as an image choice.

<a id="71-namespace-topology"></a>

#### Namespace topology

Namespace layout after install:

| Namespace | Contents |
|---|---|
| `orbitjob-system` | Admin API, scheduler and operator Deployments; log-archive StatefulSet/PVC, log collector and install hook Jobs |
| Namespaces configured in `operator.namespaceTenants` | ScheduledJob/JobRun and WorkflowJob/WorkflowRun CRs, plus rendered container Jobs |
| `monitoring` | kube-prometheus-stack: Prometheus, Grafana, alerting (optional, installed via `make monitoring-up`) |

One installation uses one `orbitjob-database` Secret, shared by all replicas.

<a id="72-first-install"></a>

#### First install

**Step 1: prepare the database Secret.** Generated from a bootstrap DSN:

```bash
install -m 0600 /dev/null /tmp/orbitjob-bootstrap-dsn
printf '%s\n' 'postgres://<owner>:<password>@<host>:5432/orbitjob?sslmode=require' \
  > /tmp/orbitjob-bootstrap-dsn

go run ./cmd/configure setup \
  --mode external \
  --database-dsn-file /tmp/orbitjob-bootstrap-dsn \
  --state .runtime/database.json \
  --runtime-env .runtime/database.env
```

This command creates local installation state; it does not itself install the
Kubernetes Secret. Apply that state using the Secret workflow in
[database setup](docs/database-setup.md) before installing Helm. The default keys
are defined in `charts/orbitjob/values.yaml`:

```text
bootstrap-owner-dsn  migrator-dsn  admin-dsn  runtime-dsn
migrator-password    admin-password  runtime-password  bootstrap-api-key
```

Full constraints: [`docs/database-setup.md`](docs/database-setup.md).

**Step 2: select reviewed images and install.** Build/publish the images for the
chosen revision in an authorized environment, then select their registry and tag.
The chart's defaults and tag fallback are in its values and Chart.yaml. Select
an available tag containing all seven OrbitJob components: admin-api, scheduler,
operator, bootstrap, migrate, log-archive and admission. `make docker-build`
builds these targets from the current source. The Admin image includes the
identity tool and API without compiled Console assets. The log collector uses the separately pinned Fluent Bit
image in the chart.

For example, build the component images with the registry and tag that will be
available to the cluster:

```bash
make docker-build REGISTRY=registry.example.com TAG='<reviewed-tag>'
```

Publish those images through the registry's image-delivery procedure and configure
any required image-pull access before installing. Save the following values:

```yaml
global:
  imageRegistry: registry.example.com
  imageTag: "<reviewed-tag>"

images:
  admin: {repository: orbitjob-admin-api}
  scheduler: {repository: orbitjob-scheduler}
  bootstrap: {repository: orbitjob-bootstrap}
  migrate: {repository: orbitjob-migrate}

operator:
  image: {repository: orbitjob-operator}
  # First installation: map this namespace to the reserved Default tenant.
  namespaceTenants: "orbitjob-tasks=00000000000000000000000001"

taskNamespace:
  create: true
  name: orbitjob-tasks

logs:
  image: {repository: orbitjob-log-archive}

admission:
  image: {repository: orbitjob-admission}
```

Save the configuration as `values.installation.yaml` and replace the registry and
image tag with the reviewed images prepared for this installation. The example
creates `orbitjob-tasks` and maps it to the reserved Default tenant, so the first
installation does not require tenant IDs from an API that has not started yet.
After installation, use IDs returned by actual tenant creation when adding
workspaces and namespace mappings. Comma-separated mappings belong in a values
file rather than plain `--set`.

Choose the installation address in the following section before running Helm.

##### Installation address and external access

Expose `/api/v1/` and `/openapi.json` through the installation's public origin,
preserving request paths and query strings. Installation below an additional
prefix such as `/orbitjob/` is not supported. The optional `/console/` route
requires separately installed browser assets; the backend image returns 503
there while keeping API routes available.

For account clients, set `adminapi.console.publicOrigin` to the exact HTTPS
origin, or leave it empty and preserve the public Host through the proxy. The
server otherwise expects `https://` plus that Host; forwarded headers are not
trusted for this decision. Cookie-authenticated writes enforce exact Origin and
CSRF checks. See [account origin configuration](docs/accounts.md#https-and-local-development).

The chart creates an internal ClusterIP Service named `orbitjob-admin-api` on
port `8080`. This source does not provide chart values for Service type, port or
Ingress configuration. Use the internal Service for port-forwarding or configure
an administrator-managed proxy that can reach
`orbitjob-admin-api.orbitjob-system.svc:8080`.

For external access, the installation administrator must provide the route to
that Service, public DNS or address, HTTPS termination and the certificate. Keep
the direct Service private. Forward `/api/v1/` and `/openapi.json` to Admin while
preserving the public Host, paths and query strings. Exclude `/healthz` and
`/metrics` from the public route and apply the installation's external access
controls. HTTP exposes automation Bearer keys in transit; use HTTPS before
transmitting integration keys or account credentials. See
[credential handling](SECURITY.md).

The optional `/console/` handler can be routed separately when compatible browser
assets have been installed. The backend image does not contain those assets;
exposing that path does not add a Console bundle. Account clients must use the
same allowed origin as the API and satisfy its Origin and CSRF requirements.

Install with the prepared component values:

```bash
helm upgrade --install orbitjob charts/orbitjob \
  --namespace orbitjob-system \
  --create-namespace \
  -f values.installation.yaml \
  --wait --wait-for-jobs
```

For local HTTP account requests, explicitly set
`adminapi.console.allowInsecureLocalhost: true` in the installation values. Leave
it disabled for production; it does not permit HTTP sessions through a node IP.

**Step 3: inspect the installation and establish access.** Check Pod and Helm status:

```bash
kubectl get pods -n orbitjob-system
helm status orbitjob -n orbitjob-system
```

Inspect the internal Service and the administrator-managed external route to
confirm their actual addresses:

```bash
kubectl -n orbitjob-system get svc orbitjob-admin-api
```

For initial administration or local access, forward the Service's fixed port:

```bash
kubectl -n orbitjob-system port-forward svc/orbitjob-admin-api 18080:8080
```

Keep the forwarding terminal running. For local HTTP account requests at
`http://localhost:18080`, explicitly enable loopback sessions as described above;
otherwise use the configured HTTPS origin. The archive has a separate internal
Service protected by mTLS; do not expose it through the public API route.

Follow [first administrator setup](docs/accounts.md#create-the-first-administrator)
when account access is required. For automated clients, issue a dedicated API key
as described under [initial access](#6-bootstrap-and-the-api-key), then exercise
the [workload API](#10-jobs-runs-functions-and-workflows). Ready Pods and
successful authentication do not by themselves establish that a task ran.

<a id="73-day-to-day-operations"></a>

#### Day-to-day operations

All commands assume the `orbitjob-system` namespace. Setting the default namespace reduces typing:

```bash
kubectl config set-context --current --namespace=orbitjob-system
```

**Observing:**

| Command | Purpose |
|---|---|
| `kubectl get pods -A` | Cluster-wide overview to locate abnormal Pods |
| `kubectl get pods,svc,deploy -o wide` | Control-plane resources + IPs + nodes |
| `kubectl get jobs -n '<tenant-namespace>'` | Container Job executions in a namespace configured by `operator.namespaceTenants` |
| `kubectl get endpoints` | Confirm a Service has Pod endpoints; empty endpoints are a common failure source |
| `helm list -A` | Installed releases and revisions |

**Diagnosing:**

| Command | Purpose |
|---|---|
| `kubectl describe pod '<pod>'` | Events, probe configuration, image, environment variables |
| `kubectl logs '<pod>' --tail=50` | Recent logs |
| `kubectl logs -l app.kubernetes.io/name=orbitjob-operator --tail=50` | Logs by label, without hardcoding a pod name |
| `kubectl logs '<pod>' --previous` | Logs from before the last crash (for restart loops) |
| `kubectl get events --sort-by=.lastTimestamp` | Cluster events sorted by time |

OrbitJob runtime images are built from `scratch` and contain no shell. `kubectl exec -it '<pod>' -- sh` fails with `executable file not found`. To inspect one, use an ephemeral debug container:

```bash
kubectl debug -it '<pod>' --image=busybox --target='<container>'
```

In most cases, use `kubectl logs` and `kubectl get endpoints` for diagnosis; exec is rarely required.

**Port-forward the API** (each forward occupies a terminal):

```bash
kubectl port-forward svc/orbitjob-admin-api 18080:8080
```

Monitoring Service names depend on the monitoring release; use the commands in
[local deployment](docs/local-deployment.md) or inspect `kubectl -n monitoring get svc`.

**Restart and scale:**

```bash
kubectl rollout restart deploy/orbitjob-scheduler  # rolling restart
kubectl rollout status deploy/orbitjob-scheduler   # watch the rollout
kubectl delete pod '<pod>'                          # delete a pod to force recreation
```

Operator replicas share Kubernetes Lease leadership. Scheduler replication needs
its separately configured optional election path; increasing replicas alone does
not establish singleton SLO evaluation.

<a id="74-upgrade-and-uninstall"></a>

#### Upgrade and uninstall

After changing the chart or values, review the migration and compatibility
requirements before upgrading. For migration 0012, stop old operators before
applying migrations and start the new version only afterwards. A rolling overlap
can count projections without matching idempotency facts. Historical terminal
rows keep their aggregates and are acknowledged at cutover; automatic recovery
covers later terminal transitions. See
[migration compatibility](docs/database-setup.md#migration-lineage-and-compatibility). The additive
execution-observation migration 0013 also requires compatible writers and CRD
schemas. Follow [the upgrade guide](docs/local-deployment.md#upgrading-an-existing-installation);
do not rewrite earlier Console migration checksums or treat Helm rollback as a
database downgrade.

For an upgrade whose migration procedure permits the chosen rollout:

```bash
# If images changed, load them into the kind nodes first
kind load docker-image --name orbitjob-dev '<image>:<tag>'

# Upgrade the release
helm upgrade orbitjob charts/orbitjob -n orbitjob-system -f values.installation.yaml --wait
```

After editing `db/migrations/*.up.sql`, sync the chart copy:

```bash
make helm-migrations-sync
make helm-migrations-check
```

Before uninstalling, stop new scheduled and manual submissions through each
source's controls. Allow existing runs to finish or request their cancellation.
Keep the compatible Operator, PostgreSQL and namespace RBAC available until
termination evidence has been committed and all OrbitJob evidence finalizers
have been released. Inspect every namespace in `operator.namespaceTenants`:

```bash
kubectl -n '<tenant-namespace>' get pods \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.finalizers}{"\n"}{end}'
```

Any remaining `orbitjob.io/termination-evidence` finalizer requires recovery
before uninstall or downgrade. Preserve the database and retained Pods while
repairing the compatible Operator; removing finalizers manually discards this
protection. An older Operator cannot discharge these obligations.

The `hold_*` outcomes of `orbitjob_execution_recovery_total` count recovery
events and can increase on repeated observations. Use the Pod inventory above
to determine which evidence holds remain; these counters are not a current
held-Pod count.

The Operator also reads current `ExecutionUnknown` and `CancelUnknown` obligations
from the ledger, including runs whose CR is missing. Inspect
`orbitjob_execution_isolated_runs`,
`orbitjob_execution_isolation_oldest_timestamp_seconds` and
`orbitjob_execution_isolation_observed_timestamp_seconds`. Each replica observes
the same configured tenant scopes; use `max` within an installation rather than
summing replicas. A failed observation preserves the previous snapshot and sets
`orbitjob_execution_isolation_collection_success` to zero. A stale or incomplete
inventory is not evidence that all work has stopped. Read the authorized attempt
trail with `orbitjob runs get ID` for the original Job UID, first unknown time and
reason. Restore that evidence before resuming the affected source.

For a local storage or networking change, follow the coordinated
[maintenance procedure](docs/local-maintenance.md) before stopping components.

After the drain completes:

```bash
helm uninstall orbitjob -n orbitjob-system
```

The chart does not delete external PostgreSQL data and does not clean up leftover Jobs in tenant namespaces configured through `operator.namespaceTenants`.

<a id="75-common-troubleshooting"></a>

#### Common troubleshooting

**Pod Running but not Ready.** Three steps:

1. `kubectl describe pod` — why the probe fails (`Readiness probe failed: ...`)
2. `kubectl logs` — application errors
3. `kubectl get endpoints` — confirm the dependent Service has backends

Probe failures usually mean a dependency is not ready (database, downstream Service), not a crash. For crashes, use `--previous` logs.

**PostgreSQL connection failures.** Pod `0/1` Running, not Ready, `kubectl logs` shows `connection refused`. Check:

```bash
kubectl get endpoints orbitjob-postgres -n orbitjob
```

`<none>` means no pod behind the Service — the deployment has 0 replicas or PostgreSQL is not deployed to that namespace.

**Health boundaries.** Scheduler/operator `/readyz` checks database connectivity;
the Admin API's `/healthz` is liveness only. Readiness does not prove scheduling,
Job completion or business recovery. Inspect component logs and actual run/attempt
progress before declaring the execution path healthy.

**Container Job not running:**

```bash
kubectl -n '<tenant-namespace>' get jobs,pods
kubectl -n '<tenant-namespace>' describe job '<name>'
kubectl auth can-i create jobs \
  --as system:serviceaccount:orbitjob-system:orbitjob-operator \
  -n '<tenant-namespace>'
```

Use the namespace key mapped to the JobRun's tenant in `operator.namespaceTenants`. Check the image and command, operator RBAC, quotas and Pod Security restrictions; then compare with the status conditions of the corresponding JobRun CR. Rendered workload Pods have `automountServiceAccountToken: false`, so they receive no Kubernetes API token.

**ImagePullBackOff.** This can indicate a missing image, wrong tag, registry
credentials or a network failure. For a local kind build, ensure the exact tag
was loaded with `kind load docker-image --name '<cluster>' '<image>:<tag>'`.
Inspect Pod events before recreating a Pod.

<a id="76-kind-verification"></a>

#### kind verification

Requires Docker, kind, kubectl, Helm and OpenSSL:

```bash
make kind-verify
```

The script creates or reuses the `orbitjob-verify` cluster, builds and loads images, installs PostgreSQL and the chart, and verifies repeat upgrades, migration-failure rollback and data retention. On success it deletes the temporary cluster; on failure it keeps it for investigation.

Daily development uses the `orbitjob-dev` cluster:

```bash
make kind-up         # create the cluster
make kind-status     # nodes and pods
make kind-down       # delete the cluster after typing delete-kind to confirm
```

<a id="6-bootstrap-and-the-api-key"></a>

### Initial administrator access

After Helm installation, create the first personal-account administrator through the
[one-time setup procedure](docs/accounts.md#create-the-first-administrator).
Ordinary users receive workspace invitations and sign in with personal accounts.
They need no Kubernetes Secret access. Lost installation administration uses the
[operator recovery procedure](docs/accounts.md#recover-installation-administration).

#### Bootstrap integration key

The bootstrap key remains available for legacy integrations and initial scripted
API setup. It cannot sign in to the Console and does not establish human account
ownership. Confirm the Kubernetes context and use the namespace in which the
Helm release was installed. With permission to read its Secrets, run:

```bash
INSTALLATION_NAMESPACE='orbitjob-system'
kubectl -n "$INSTALLATION_NAMESPACE" get secret bootstrap-api-key \
  -o jsonpath='{.data.api-key}' | base64 --decode
```

Keep the output private. It is the integration key delivered by bootstrap.
It belongs to Default and carries the initial administrator preset; it is not a
platform credential and does not switch its business-data tenant. Local
quickstart writes it and the API address to `.kind/env.sh` for shell use.

For ordinary scripts, issue a named dedicated key through
`POST /api/v1/tenants/{tenant_id}/api_keys` with the required actions and resource scope. Save the one-time plaintext in an
approved secret store. Use an ungrouped key for Jobs/Runs and include separate
log grants when needed. Human session access uses [memberships](docs/accounts.md)
independently of these keys.

The `bootstrap-api-key` output Secret has an `api-key` data key. The installation
input Secret, normally `orbitjob-database`, supplies bootstrap's configured
plaintext through its `bootstrap-api-key` data key. These are separate Secret
locations. Do not modify either as an improvised credential-rotation procedure.

Bootstrap is idempotent. If delivery fails after the key commits, rerun it with
the same configured plaintext. It verifies the stored bcrypt hash before
retrying delivery and refuses a conflicting file or Secret. It cannot recover
unknown plaintext from PostgreSQL or replace an existing credential silently.
Use key replacement and revocation for a lost ordinary credential.

> **Deprecated**: `bootstrap.secretName` is retained as an ignored chart value.
> Bootstrap writes the fixed `bootstrap-api-key` output Secret in the installation
> namespace; remove the obsolete override from installation values.

<a id="14-monitoring-and-troubleshooting"></a>

### Monitoring and troubleshooting

For an installation with the monitoring release configured, run repository
commands from the repository root with the intended Kubernetes context selected.
Set the [installation API origin](#81-choose-an-installation-and-credential) as
shown in the API setup; retain that same address for metrics:

```bash
make observability-status
curl -fsS "$ORBITJOB_API/metrics"
```

Read the effective deployment's `adminapi.rateLimits` in the
[Helm values](charts/orbitjob/values.yaml) rather than assuming standalone
process settings apply to the installed API.

Grafana ships with the Prometheus datasource and the OrbitJob dashboard pre-provisioned (after `make monitoring-up`).

#### Migration failures

```bash
kubectl -n orbitjob-system get pods | grep -E 'migrate|owner-init'
kubectl -n orbitjob-system logs '<migrate-or-owner-init-pod>'
```

- `unsupported pre-release schema history; recreate the database`: the runner
  refuses an incompatible development history. Confirm the target and preserve
  needed data before using a new disposable database or a separately reviewed
  migration. Do not delete an installed database or edit `schema_migrations`
  merely to bypass the guard.
- `migration 0001 checksum mismatch` with a ledger entry named `baseline`: the released baseline file was modified. Restore the released file; do not rebuild the database to bypass a modified migration history.
- Checksum mismatches on later migrations: likewise restore the original file, or fix the schema with a new, consecutively numbered migration.

#### Bootstrap key retrieval fails

```bash
kubectl -n orbitjob-system get pods        # bootstrap Job status
kubectl -n orbitjob-system logs '<bootstrap-pod>'
```

If the bootstrap Job fails, read its logs first; on success the key is in the `bootstrap-api-key` Secret. Never write the key into logs or commit it to Git.

#### Run stuck Pending

```bash
kubectl -n orbitjob-system logs deployment/orbitjob-operator --tail=100
kubectl -n orbitjob-system get scheduledjob,jobrun -A
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/instances/<run_id>"
```

Check whether the operator's schedule loop created the JobRun CR, reconcile errors in the operator logs,
whether `operator.namespaceTenants` covers the namespace that owns the tenant, and whether the K8s Job
hits quotas or admission restrictions.

#### Container Job not running

```bash
kubectl -n '<tenant-namespace>' get jobs,pods
kubectl -n '<tenant-namespace>' describe job '<name>'
kubectl auth can-i create jobs \
  --as system:serviceaccount:orbitjob-system:orbitjob-operator \
  -n '<tenant-namespace>'
```

Use the namespace key mapped to the tenant in `operator.namespaceTenants`. Check the image and command, operator RBAC, quotas and Pod Security restrictions. Rendered workload Pods do not mount ServiceAccount tokens. Workflow history retention also needs `delete` on
`workflowruns` in that same namespace; the chart grants this to the operator.
Cancellation reports `Canceled` only after controller stop acknowledgement,
verified owned-Pod termination evidence and removal of the original Job.
The Operator needs Pod
`patch` permission in each mapped namespace to release its evidence finalizer.
Database or Operator outages can retain exited Pods and their Jobs until
recovery completes. An ambiguous creation request remains `CancelUnknown` until
reconciliation can confirm the actual workload state.

Use the namespace and name returned by the trigger API when addressing a run.
New run resources keep the complete occurrence digest in their names. Matching
historical short names remain reachable after complete identity validation;
existing resources are not renamed. See
[migration compatibility](docs/database-setup.md#migration-lineage-and-compatibility).

## Source development

<a id="local-quick-start"></a>
<a id="prerequisites"></a>
<a id="one-shot-start"></a>
<a id="2-kind--helm-deployment-recommended"></a>
<a id="2-kind-helm-deployment-recommended"></a>

Build the current source in a disposable kind environment using the
[development guide](docs/local-deployment.md#installation-shortcut). It contains
the complete prerequisites, setup order and separate-terminal instructions.
The resulting `:dev` installation is a source build, not a published version.

<a id="3-docker-compose-removed"></a>

The old Docker Compose stack and worker execution path are removed. Use kind for
the local system, `make monitoring-up` for monitoring, and the
[database guide](docs/database-setup.md) for database setup.

<a id="4-local-go-development"></a>

### Local Go development

The scheduler and database tools can run locally against an appropriate migrated
database. The admin-api and operator currently call `rest.InClusterConfig`; setting
`KUBECONFIG` alone does not enable out-of-cluster startup. Run those components in
their chart Deployments for the normal development path.

For a local scheduler, use the generated installation configuration and a database
port-forward. Stop another scheduler using the same database before starting a
second evaluator unintentionally:

```bash
# Port-forward the in-cluster PostgreSQL (installed by make kind-db)
kubectl -n orbitjob port-forward deployment/orbitjob-postgres 15432:5432 &

# Use the generated installation state (make kind-db writes it to
# .runtime/kind/database.env; the admin DSN is ADMIN_DSN)
set -a
. ./.runtime/kind/database.env
set +a

# Update the DSN host/port for this local port-forward before running.
go run ./cmd/scheduler
```

`make migrate-up DATABASE_URL="$MIGRATOR_DSN"` runs the migration runner against
the complete `db/migrations` directory. New installations normally use Helm's
owner-init and migrate hooks; applying only the baseline omits later features
and integrity changes.

- The Admin API listens on `8080` by default; change it with `PORT`
- Running a binary directly does not create the database, run migrations or bootstrap; those are done by the Helm hooks or `cmd/bootstrap`
- Scripted API calls require a Bearer key; obtain initial integration access through
  the [administrator procedure](#6-bootstrap-and-the-api-key). Console accounts use
  [server sessions](docs/accounts.md) after all migrations and bootstrap complete.

<a id="5-standalone-processes"></a>

### Standalone processes

Compile all Go packages for the current platform:

```bash
make build
```

Cross-compile Linux/amd64 binaries:

```bash
make build-all
```

`make build` checks compilation without producing those executables in `bin/`.
`make build-all` writes the configured Linux cross-build binaries there. For a
native executable, use an explicit output path, for example:

```bash
go build -o bin/scheduler ./cmd/scheduler
```

Standalone processes should reference the same installation configuration. admin-api uses `ADMIN_DSN`; scheduler and operator share `RUNTIME_DSN` (the operator reads `OPERATOR_DSN` first):

```bash
set -a
. /opt/orbitjob/etc/database.env
set +a
./bin/scheduler
```

Use native binaries on the host architecture. admin-api and operator additionally
need their in-cluster service-account configuration; a bare-metal process with
only a DSN is insufficient. The legacy `DATABASE_DSN` and `SCHEDULER_DSN` remain
compatibility fallbacks, not the configuration choice for new deployments.

Recommended start order:

```text
owner-init -> migrate -> bootstrap -> admin-api/scheduler/operator/log-archive
```

Never let runtime processes share the owner or superuser DSN in production. Helm separates the admin/runtime/migrator roles; custom deployments should reuse the same boundary.

Process environment variable examples:

```text
deploy/env/admin-api.env.example
deploy/env/scheduler.env.example
```

Common variables:

| Process | Variable | Meaning / authority |
|---|---|---|
| admin-api | `PORT` | HTTP listener; see the environment example |
| admin-api | `OPERATOR_NAMESPACE_TENANTS` | same installation mapping as Operator; unset disables typed definition/log routes |
| admin-api | `RATELIMIT_READ_RPS` / `RATELIMIT_WRITE_RPS` / `RATELIMIT_TRIGGER_RPS` | Token-bucket settings; see environment examples and chart values |
| scheduler | `RUNTIME_DSN` | installation runtime connection |
| scheduler | `SCHEDULER_HEALTH_PORT` | Component health listener |
| operator | `OPERATOR_DSN` (fallback `RUNTIME_DSN`) | installation runtime connection |
| operator | `OPERATOR_NAMESPACE_TENANTS` | namespace=tenant mapping (injected by Helm via `operator.namespaceTenants`) |

Scheduler/operator component listeners expose `/healthz` and `/readyz`; readiness
pings the database. The Admin API exposes `/healthz`, `/metrics` and
`/openapi.json`, with no `/readyz` route. Its liveness response does not establish
database connectivity or completed work.
The chart wires scheduler/operator readiness to `/readyz`; their liveness probes
use `/healthz`, as does scheduler startup. The Admin API readiness remains `/healthz` and
must not be used as database evidence.

```text
GET /healthz   process alive
GET /readyz    readiness probe
```

Stopping processes: with a process in the foreground, press `Ctrl+C` and wait for graceful shutdown. In the background, use `kill -TERM '<pid>'` after replacing the placeholder. For maintenance, first stop workload publication and execution writers, then follow the database or archive backup procedure.

<a id="15-smoke-test-and-sample-data"></a>

### Smoke test and sample data

The full-surface smoke test covers tenant, API key, trigger and run-query flows and fills in
`smoke-test-results.md`:

```bash
ADMIN_BOOTSTRAP_API_KEY="$ORBITJOB_API_KEY" \
  ./scripts/smoke-test.sh http://localhost:8080
```

The script creates data and triggers runs; it modifies the database. Never run it against production.
Its cancellation scenario is required: a missing target, early terminal run or
failure to observe Running fails the check. HTTP transport errors fail even if
an incomplete transfer already reported status 200. Optional skipped probes are
reported as unverified rather than included in an "all checks passed" claim.

> Historical note: `scripts/import-jobs.sh` and `scripts/job-dataset.json` targeted the removed
> `POST /api/v1/jobs` endpoint (handler payload dataset) and do not work on this branch.

<a id="16-development-verification"></a>

### Development verification

```bash
make test
make test-cover
make test-race
TEST_DATABASE_DSN='<dedicated disposable database DSN>' make integration
make check
make helm-check
```

`make check` runs golangci-lint, `go vet`, race tests, the OpenAPI sync check and the `go mod tidy` check.

`make integration` does not create PostgreSQL. A dedicated `TEST_DATABASE_DSN`
is required; tests can rebuild schema, provision roles and change data. Never
use an installed environment's database. Exact quality gates are maintained in
[CONTRIBUTING.md](CONTRIBUTING.md); executed checks and cluster/load boundaries
are recorded in [verification](docs/verification.md).
