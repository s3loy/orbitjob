> Repository source: docs/tasks/read-logs.md. References marked repository require a source checkout.

# Read execution logs

The **Execution logs** panel combines retained platform events with container
stdout/stderr across Attempts. Read the collection state and gap markers along
with the output; collection and execution outcome are separate observations.

## Before you begin

Job-run archives require `instance:ReadLogs`; Function archives require
`function:ReadLogs` on the actual Function parent. Ordinary run or Function read
access does not grant log access. Instance archive routes require credentials
without a resource-group restriction. A group-scoped Function credential uses
the Function history URL and the Function's persisted group authorization.

Open the recorded run in **Jobs** or **Runs**, or a terminal Function invocation
in its history. The archive must be registered and reachable. Retained files
alone do not keep a Function log URL accessible if its parent definition is
unavailable.

## Inspect output in the Console

Open **Execution logs**. Select the Attempt or all Attempts, filter stages and
search retained text. Follow reconnects from the last cursor; pausing follow
stops browser polling and leaves collection running. Copy or download the
retained output when needed.

Command evidence records the reviewed image and argv, emitted output and any
observed exit code or signal. Shell commands inside the workload appear only
when the workload prints or traces them. Treat output as sensitive text.

A sealed archive is not proof of complete source coverage. Node loss, queue
eviction, source rotation and capacity limits can omit output. A gap with an
unknown extent must remain unknown. An empty page can mean no matching retained
records; it cannot establish that the workload produced no output.

## Read an archive through the API

Use the authenticated API setup in [call the API](call-api.md). Replace `73`
with the actual instance ID:

```sh
RUN_ID=73
curl --silent --show-error --fail-with-body \
  -H "Authorization: Bearer $ORBITJOB_API_KEY" \
  "$ORBITJOB_API_URL/api/v1/instances/$RUN_ID/logs/archive?after=0&limit=500"
```

Use the response's `next` cursor for a subsequent read, retaining that cursor
when reconnecting. Query parameters are `after` (nonnegative), `limit` (1–1,000),
`attempt` (positive Attempt number) and `search` (at most 256 Unicode code points).
Search covers retained evidence only. The Function route is
`/api/v1/functions/{function_id}/runs/{history_uuid}/logs/archive`.

Append `/download` before the query string for a text export. Downloads include
omission markers and an explicit end marker. A successful HTTP status followed
by an interrupted transfer does not establish a complete export.

The separate `/attempts/{attempt}/logs` endpoint reads a bounded snapshot from
retained Kubernetes resources. It has no durable retention guarantee and may be
unavailable after Kubernetes garbage collection. The `attempt` path is the
positive ledger Attempt number, not its row ID.

## Retention and deletion

By default, an archive is protected for 15 days from the later of completion
and collection seal. Late accepted output extends protection; output caps still
apply. The default logical execution budget is 32 MiB, shared across Attempts,
Pods and streams. Over-limit output retains the first 8 MiB and a recent 24-MiB
window with visible omissions. See archive limits (repository: `../execution-logs.md#installation-and-capacity`)
for tenant, workflow and installation budgets.

**Delete retained logs** requires the separate `instance:DeleteLogs` or
`function:DeleteLogs` action. It commits an audited cleanup request and can
remove a terminal archive during protection. Cleanup is asynchronous and leaves
the execution summary. Log deletion does not cancel execution or retire a
source definition.

For archive unavailability or integrity errors, preserve the reported identity
and contact the installation administrator. Do not replace corrupt evidence
with an empty archive. See archive recovery (repository: `../execution-logs.md#integrity-and-recovery`).
