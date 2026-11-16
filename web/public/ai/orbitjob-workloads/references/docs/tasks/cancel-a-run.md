> Repository source: docs/tasks/cancel-a-run.md. References marked repository require a source checkout.

# Cancel a run

Cancellation records a stop request. The Operator confirms the final outcome
only after it has sufficient ownership and termination evidence. An accepted
HTTP response does not establish that workload code stopped.

## Before you begin

For a job run, obtain its numeric instance ID and `instance:Cancel` permission.
Use `instance:Get` to observe it after requesting the stop. Instance routes
require credentials without a resource-group restriction. A workflow uses its
own cancellation route and `workflow:Trigger` permission; inspect the parent
workflow and step outcomes afterward.

Confirm the intended workspace, run and Attempts. Cancellation cannot undo
external side effects already performed by a container.

## Request cancellation

Open the run in the Console and select its cancellation action. Review the
current phase and confirm the request. Alternatively:

```sh
RUN_ID=73
orbitjob runs get "$RUN_ID"
orbitjob runs cancel "$RUN_ID" --json
orbitjob runs get "$RUN_ID" --json
```

Replace `73` with the actual run ID. The response reports the resource reference
and phase at request time. Continue reading the same run until the Operator
reports a confirmed terminal outcome.

## Interpret the result

| Observation | Required interpretation |
|---|---|
| `CancelRequested` | Stop intent is recorded; termination is still being checked. |
| `Canceled` | Requested cancellation is confirmed. |
| `Succeeded` or `Failed` | A confirmed execution outcome remains final; cancellation does not replace it. |
| `CancelUnknown` | The platform cannot yet prove the requested stop. Preserve evidence and continue recovery. |
| `ExecutionUnknown` | Execution outcome remains unverified; requesting cancellation still requires stop proof. |

The Operator shortens the original Job's deadline with identity/version checks,
waits for controller stop acknowledgement and persists complete owned-Pod
termination evidence. It releases its evidence finalizers before foreground
deletion and confirms the stop against retained proof. A Job deletion timestamp,
empty Pod list or missing UID cannot establish termination. The five-minute
confirmation window uses persisted cancellation time and survives restarts;
reaching the window does not turn uncertainty into `Canceled`.

Workflow cancellation prevents new steps and requests cancellation of
nonterminal steps. Already running siblings remain observable. The workflow
must wait for their confirmed outcomes; a stopped parent does not prove every
container has stopped.

## Recover a lost cancellation response

Read the same run first. Keep its original namespace/name, occurrence key and
run ID. If the current state confirms stop intent or a final outcome, follow
that observation. If no result is available, use the original run's cancellation
action for a deliberate retry. Do not create a replacement run while the
original might still execute.

For `CancelUnknown`, an administrator must inspect the original Job, retained
Pods, ledger and Operator errors. Force-deleting Pods, removing evidence
finalizers or deleting ledger rows can destroy the evidence needed for recovery.
There is no public force-resolution endpoint. See
execution recovery (repository: `../architecture.md#execution-recovery-and-concurrent-admission`).
