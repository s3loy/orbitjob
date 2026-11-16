> Repository source: docs/tasks/functions-and-workflows.md. References marked repository require a source checkout.

# Use Functions and Workflows

Functions expose parameterless container executions through an invocation API.
Workflows coordinate a graph of existing jobs. Both execute through the same
Run, Attempt and Kubernetes Job model as ordinary jobs.

## Manage and invoke a Function

Function creation requires `function:Create`; editing requires
`function:Update`. Read, invoke and log actions are independent. A group-scoped
credential creates within its own group and accesses the persisted Function
parent. Explicit Deny remains effective.

Open **Functions → Create a function**. Review the container image, argv,
resources, timeout, retry settings and active/paused status before saving.
The creation API requires `X-OrbitJob-Idempotency-Key`. The same normalized
payload under the same retained actor/request identity returns the original
creation snapshot, even if the Function was later edited. Changed input under
the same identity conflicts.

Updates replace the complete mutable definition and require `expected_version`.
Read and review a concurrent version before another update; omitted optional
settings resolve to creation defaults. A saved definition and an executable
active revision have separate readiness. Wait until the active revision matches
the intended definition version before invoking it.

Select **Invoke** to request execution. The Console pins the reviewed version.
API invocation uses `POST /api/v1/functions/{id}/invoke`, an optional stable
`X-OrbitJob-Idempotency-Key`, and `expected_version` when the caller requires a
specific definition version. A Function invocation takes no arbitrary input
body. `wait_seconds` supports a bounded synchronous wait from 0 through 60;
expiration of that wait does not cancel the execution or establish its outcome.
Keep the JobRun receipt and continue observing.

Function history contains terminal invocation projections. A still-running
invocation need not appear there, and projection can follow the execution's
terminal commit. Function history uses UUID run identities; these differ from
numeric instance IDs. Read retained output through the exact Function history
URL with `function:ReadLogs`. Pausing a Function prevents new invocations and
leaves existing execution running. The management API provides no Function
delete operation.

## Create and run a Workflow

Workflow reads require `workflow:Get` and `workflow:List`. Authoring additionally
requires `workflow:Apply`, an authorized namespace and credentials without a
resource-group restriction. Triggering and cancellation use `workflow:Trigger`.

Open **Workflows → Create a workflow**. Reference ScheduledJobs in the same
namespace. Give each task a unique name, define dependencies and review the
graph. A cycle or an unknown task name in `dependsOn` prevents projection.
A nonempty `jobRef` can name an unavailable ScheduledJob and still project;
an active Workflow revision does not establish that its referenced jobs are ready
for execution. Task conditions can read declared upstream terminal phases,
Attempt counts or terminal-attempt
duration. A condition that does not pass skips the task without a child Run.

Choose Allow or Forbid concurrency. An optional schedule uses five-field cron
and an IANA timezone, defaulting to UTC. Workflow schedule preview checks the
same parser without creating work. Daylight-saving gaps are skipped and repeated
local times represent separate instants. Workflow suspension pauses automatic starts; manual requests remain
available. Use the separate execution-pause control to freeze new admission.

Execution policy version 1 resolves referenced ScheduledJobs and pins all task
revisions at parent-run admission; a missing referenced job prevents that
admission. Legacy version 0 resolves jobs and selects revisions as tasks start;
a missing job can skip a task with `missing_job_reference`. Each task uses its
referenced job's own resources, timeout and retry budget. The Workflow
wall-clock timeout is Operator-enforced, so an Operator outage can delay that
enforcement. It does not replace each Attempt's Kubernetes deadline.

Review, check against the cluster and save the typed definition. Wait for the
intended revision to be ready, then select **Trigger**. Keep its WorkflowRun
receipt and inspect the parent plus individual step outcomes. Under FailFast,
a terminal step failure stops new work while already running steps remain
observable. Continue allows dependency and condition rules to decide eligible
work. A step with unknown execution evidence blocks new Workflow work; it cannot
be assumed failed, canceled or safe to replace.

Cancellation records stop intent and fans it out to nonterminal steps. The
parent waits for sufficient child termination evidence. See
[cancel a run](cancel-a-run.md) and [execution model](../concepts/execution-model.md).

## Recover uncertain mutations

Keep request keys, target versions and accepted resource references. Reconcile
a lost Function create with the exact original creation request. For a lost
Function update response, read the stored version and review it before another
write. Workflow typed saves retain durable operation identity and require
UID/resourceVersion checks. Reusing an accepted request reconciles that
operation; changing its content conflicts.

A successful trigger/invocation response confirms publication rather than
execution. A denied history read does not reject the original request. On an
unknown trigger result, preserve the original request identity and observe or
reconcile it before starting replacement work.
