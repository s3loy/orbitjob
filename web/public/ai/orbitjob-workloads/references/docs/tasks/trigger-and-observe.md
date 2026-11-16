> Repository source: docs/tasks/trigger-and-observe.md. References marked repository require a source checkout.

# Trigger and observe a run

A trigger requests one execution of an active job revision. Keep its receipt
until the request is matched to a run and its confirmed outcome.

## Before you begin

You need `job:Get`, `job:List` and `job:Trigger` for the job. Observation needs
independent `instance:List` and `instance:Get` grants. Job and instance routes
require credentials without a resource-group restriction. Logs additionally
require `instance:ReadLogs`. Obtain access before triggering work whose result
you must inspect.

The job must have a projected active revision and must not be suspended.
Confirm its namespace and executable configuration, particularly retry safety
and timeout. See [execution model](../concepts/execution-model.md).

## Use the Console

Open **Jobs**, select the job and choose **Trigger**. Review the target and
submit. The Console associates the operation with a stable request key and
retains accepted receipts across navigation within the current connection.
Follow the receipt to the run or open the job's **Runs** tab.

Forbid can leave an accepted manual JobRun waiting before a ledger row exists.
An empty history result during that interval does not reject the request.
Observation also depends on read grants, the selected workspace and any list
filters or time window. Adjust those before treating a run as absent.

The Console bounds automatic observation. If it pauses, resume the check or
refresh the run. A paused browser check does not pause server execution.
Receipts held in page memory clear on sign-out, a 401, or an identity/workspace
change. Record the namespace/name and occurrence key when handing an uncertain
request to an administrator.

## Use the CLI

Set the installation API address and a dedicated key as described in
CLI configuration (repository: `../cli.md#configuration`). Keep one request identity for
one intended run:

```sh
JOB_ID=42
REQUEST_KEY='invoice-check-2026-11-16-001'
orbitjob jobs get "$JOB_ID"
orbitjob jobs trigger "$JOB_ID" --idempotency-key "$REQUEST_KEY" --json
```

`JOB_ID` is the numeric active revision ID printed by `jobs list`; replace the
example with the intended job's current ID. The trigger result has
`namespace`, `name`, `occurrence_key`, `trigger`, `phase` and `created`.
It has no ledger run ID. An empty phase is valid before Operator observation.
For exact correlation, call the instance list API with the receipt's
`occurrence_key` as described in [call the API](call-api.md#observe-the-request).
The CLI's `runs list` supports a phase filter, but no occurrence-key filter.

Once you obtain the numeric instance ID:

```sh
RUN_ID=73
orbitjob runs get "$RUN_ID" --json
```

Replace `73` with the actual recorded ID. Inspect all Attempts and the Run's
phase. `ExecutionUnknown` and `CancelUnknown` remain nonterminal. Missing logs
or Kubernetes objects are not proof of success, failure or cancellation.

## Recover an uncertain trigger

If the response is lost, retain the original job revision ID and request key.
Reissue the same trigger with that key to reconcile the same occurrence while
its identity is retained. A new key or different revision can create different
work. Do not let an HTTP library automatically repeat keyless trigger requests.
If the original target is unavailable or its identity has been pruned, ask an
administrator to reconcile retained execution evidence before requesting
replacement work.

A 401 rejects authentication; sign in again or obtain a valid key. A 403 means
access is denied and does not establish session expiry. A 429 requires delayed,
bounded retry; reuse the original request identity. A transport failure or 5xx
can leave the operation outcome uncertain. Preserve the receipt or request
identity and observe/reconcile before issuing a new request.

Reconcile an existing trigger as the same authenticated principal and workspace.
A human-session request and an API-key request record different actors; reusing
a key across those identities does not transfer ownership of the original request.
