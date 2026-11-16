# Execution model

OrbitJob executes a pinned container definition through Kubernetes Jobs. A
logical Run records one requested occurrence. Its Attempts record separate
platform execution tries, each bound to one Kubernetes Job. A Kubernetes Job
can create replacement Pods according to its own backoff policy.

## Definitions, revisions and requests

A ScheduledJob defines a container job with an optional cron schedule.
WorkflowJob defines a dependency graph whose tasks reference ScheduledJobs in
the same namespace. Functions and Checks store their definitions through the
Admin API. The Operator projects executable configurations into immutable
revisions. Editing a definition changes future execution selection; it does not
rewrite the pinned configuration of an admitted run.

The API publishes a JobRun for a manual job request or Function invocation,
and a WorkflowRun for a workflow request. The Operator admits the resource,
records the ledger and creates workload Jobs. The initial HTTP receipt contains
a resource namespace/name and occurrence key. It is not a ledger run ID or proof
that a Pod has started. Scheduled occurrences and workflow steps persist their
occurrence/step identity before publication so interrupted publication can be
repaired against the original identity.

A stable idempotency key lets a client reconcile a repeated trigger against the
same occurrence identity. Keep the original target revision, key and receipt.
Changing the target revision or generating a new key can request different work.
Idempotency depends on retained identity and does not make external workload
effects exactly once.

## Run and Attempt lifecycle

| Phase | Meaning |
|---|---|
| `Pending` | The admitted run is waiting to dispatch. |
| `CreatingAttempt` | Durable creation intent exists; the Kubernetes creation result may still be unresolved. |
| `Running` | The current attempt is being observed. |
| `RetryWaiting` | A failed attempt is awaiting platform retry or exhaustion handling. |
| `CancelRequested` | Stop intent is recorded; termination has not yet been confirmed. |
| `ExecutionUnknown` | Execution evidence is insufficient to determine the running or completed outcome. |
| `CancelUnknown` | Cancellation has not established a complete stop proof. |
| `Succeeded` | Successful completion is confirmed. |
| `Failed` | Failure is confirmed after applicable retry processing. |
| `Canceled` | Requested cancellation is confirmed. |

Only `Succeeded`, `Failed` and `Canceled` are terminal JobRun phases. Workflow
parents also support terminal `Rejected` when admission refuses the request
before its steps start; a skipped workflow task has no child Run. A missing
Job, an expired observation window or an API timeout cannot establish a terminal
outcome. Unknown runs retain their identity and concurrency slot. OrbitJob does
not automatically replace an execution merely because its resources disappear.
There is no public force-resolution API.

Before requesting a Kubernetes Job, the Operator persists `CreatingAttempt`.
An uncertain creation response is reconciled against that attempt's original
address and immutable identity. The Operator verifies JobRun, Job and Pod
ownership; matching a resource name alone is insufficient. Terminal outcomes
remain final when delayed observations arrive.

## Retries and deadlines

`retryPolicy.maxAttempts` bounds platform Attempts, including the first one.
An omitted or zero value resolves to one Attempt; negative values are invalid.
`jobTemplate.backoffLimit` sets Kubernetes Job backoff within each Attempt.
Pods use `restartPolicy: Never`,
so a failing container is not silently restarted in the same Pod. More Pods or
another Attempt can still repeat the workload's external effects. Make repeated
effects safe before enabling either retry mechanism.

Execution policy version 1 requires `retrySafe: true` when `backoffLimit > 0` or
`maxAttempts > 1`. It resolves CPU, memory and ephemeral-storage requests/limits
into the revision. Workload Pods use a non-root UID, RuntimeDefault seccomp,
no privilege escalation and dropped Linux capabilities. Kubernetes service-account
token mounting is disabled. Legacy version 0 revisions retain their narrower
historical configuration contract; editing a definition does not silently
upgrade a running revision.

ScheduledJob `timeoutSeconds` becomes `activeDeadlineSeconds` on each Attempt's
Kubernetes Job. It bounds an individual Attempt and is enforced by Kubernetes
even while the Operator is unavailable. Multiple Attempts can take longer than
this value overall. Zero supplies no Job deadline. A Workflow's own timeout is
checked by the Operator and does not provide the same outage-independent
enforcement as an Attempt's Kubernetes deadline.

## Concurrency, pause and cancellation

A ScheduledJob supports Allow, Forbid and Replace for scheduled admission.
Manual requests under Forbid wait for earlier active or uncertain execution.
Scheduled skip/misfire handling is a separate contract. Replace requests a stop
before admitting replacement scheduled work; it cannot treat uncertainty as a
confirmed stop. Workflows support Allow and Forbid.

A suspended ScheduledJob prevents scheduled and manual starts. WorkflowJob
suspension pauses automatic starts while manual requests remain available.
Execution controls distinguish schedule pause, execution pause and identity deletion.
Execution pause also blocks new root runs, workflow steps and not-yet-started
dispatch; possibly started work remains observable and cancellable. Pausing does
not cancel running work. Identity deletion is an irreversible tombstone; it is
independent of log cleanup.

Cancellation requests stop intent and then waits for owned workloads to stop.
New workload Pods retain termination evidence with an Operator-owned finalizer.
The Operator requires controller stop acknowledgement and complete owned-Pod
termination evidence before releasing its evidence hold and deleting the
original Job. A deletion timestamp, empty Pod inventory, node loss or force
removal cannot establish that workload code stopped. A confirmation timeout
preserves uncertainty and can later recover when sufficient evidence returns.

## History, output and derived results

The run ledger records identities, attempts and outcomes. The execution archive
stores bounded lifecycle events and container stdout/stderr independently of
Pod retention. Its gaps and collection state qualify what output is available;
log loss does not change a workload's business outcome.

Terminal execution facts precede derived Check, Function and SLI records.
Pending projections are retried idempotently and protected from normal pruning.
A terminal run can therefore be visible before a derived history or metric
appears. Execution history, Function history and log protection have separate
retention lifecycles. See [execution logs](../execution-logs.md) and
[architecture](../architecture.md) for operational boundaries.
