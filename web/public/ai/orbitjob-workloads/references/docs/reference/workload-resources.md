> Repository source: docs/reference/workload-resources.md. References marked repository require a source checkout.

# Workload resource reference

OrbitJob uses `workloads.orbitjob.io/v1alpha1` resources. Application users
normally author ScheduledJob and WorkflowJob definitions through the Console or
typed definition API. Direct Kubernetes writers need explicit namespace RBAC and
admission authorization. A schema-valid resource still needs successful
reconciliation before it has an executable revision.

## Definition creation and legacy resources

The default typed definition API requires `executionPolicyVersion: 1` when
creating a new ScheduledJob or WorkflowJob. Omitted version or `0` below
explains the underlying CRD and existing legacy configuration; it is not a
valid new-definition request through the default typed API. An explicitly
enabled legacy adapter is a separate installation choice.

## ScheduledJob

| Field in `spec` | Meaning |
| --- | --- |
| `schedule` | Empty or omitted means manual only. A configured schedule uses the supported five-field cron grammar. |
| `executionPolicyVersion` | `1` accepts an explicit retry/resource policy; omitted or `0` preserves the legacy policy. |
| `retrySafe` | Required for version 1 when `jobTemplate.backoffLimit > 0` or `retryPolicy.maxAttempts > 1`. Declaring it does not make external effects idempotent. |
| `suspend` | Stops automatic scheduling and rejects manual triggers; it does not cancel an execution already running. |
| `concurrencyPolicy` | `Allow`, `Forbid` or `Replace`. Unknown executions can continue to hold a Forbid slot. |
| `misfirePolicy` | `Skip`, `FireOnce` or `CatchUpBounded`; these control missed scheduling occurrences. |
| `timeoutSeconds` | Copied to the run and applied as `activeDeadlineSeconds` to each attempt's Kubernetes Job. It is not a total multi-attempt Run budget. |
| `jobTemplate.image` | Required container image; registry access is an installation prerequisite. |
| `jobTemplate.command`, `jobTemplate.args` | Literal argument arrays. A shell is used only when explicitly selected as the command. |
| `jobTemplate.backoffLimit` | Kubernetes Job retry budget within one platform attempt. |
| `jobTemplate.resources` | CPU, memory and ephemeral-storage requests/limits supported by the execution policy. |
| `retryPolicy.maxAttempts` | Total platform attempts, including the first. Effective omission is one attempt. |
| `history.successfulRuns`, `history.failedRuns` | Retained terminal runs by outcome. Zero selects the platform default; it does not request unlimited history. |

Version 1 normalizes all three resource dimensions. The current standard profile
uses these values:

| Resource | Request | Limit | Acceptance maximum |
| --- | --- | --- | --- |
| CPU | `100m` | `1` | `16` |
| Memory | `128Mi` | `256Mi` | `64Gi` |
| Ephemeral storage | `100Mi` | `1Gi` | `100Gi` |

Quantities must be positive, CPU uses at most millicore precision and byte
quantities contain whole bytes. Requests cannot exceed limits. These are
acceptance bounds, not measured node capacity. Accepted revisions retain their
normalized values. Consult the execution policy implementation (repository: `../../internal/domain/executionpolicy/policy.go`)
and [distributed CRD](../../scheduledjob-crd.yaml)
when configuring native tooling.

The typed template does not accept an arbitrary Kubernetes Pod spec. Configure
only supported fields; arbitrary environment variables, volume mounts and Pod
security settings are not import fields in this API. See
[manage jobs](../tasks/manage-jobs.md) for the complete authoring workflow.

## WorkflowJob

| Field in `spec` | Meaning |
| --- | --- |
| `executionPolicyVersion` | Version `1` pins referenced task revisions before accepting execution; omission retains legacy task-by-task selection. |
| `schedule`, `timeZone` | Optional cron schedule and IANA time zone. Empty schedule means manual only; a supplied time zone must still be valid. |
| `suspend` | Stops automatic scheduling while preserving manual starts. |
| `concurrencyPolicy` | `Allow` or `Forbid`; WorkflowJob does not support `Replace`. |
| `timeoutSeconds` | Workflow wall-clock deadline enforced by the Operator walker. An Operator outage delays enforcement. |
| `failPolicy` | `FailFast` (the default) stops creating steps after a terminal step failure; `Continue` keeps evaluating remaining tasks by their dependencies and conditions. See the behavior below. |
| `tasks` | Nonempty named task list with same-namespace ScheduledJob references. Dependencies must resolve without cycles. |
| `history` | Terminal workflow history limits by outcome. Zero selects defaults. |

`FailFast` leaves already created steps running. Tasks without a step are marked
skipped with reason `fail_fast` after their dependencies resolve; tasks still
waiting on running dependencies remain undecided. If a task is ready to start
but its referenced ScheduledJob is unavailable, it is skipped and `FailFast`
stops new steps. `Continue` allows a task to run after its dependencies finish,
including failed dependencies, if its declared condition passes. A skipped
dependency causes its dependent tasks to be skipped; without a condition, a
failed dependency alone does not skip the next task.

Without a Workflow cancellation request, either policy yields a failed Workflow
once all tasks have resolved if any step failed or a task was skipped because its
ScheduledJob reference was unavailable. A condition-based skip alone does not make
it fail. Unknown execution or cancellation evidence keeps the Workflow nonterminal.
Workflow cancellation and deadlines stop new work and request cancellation of
in-flight steps independently of `failPolicy`.

Conditions read declared dependencies using phase comparisons (`eq`, `ne`, `in`,
`not_in`) or numeric `attempts`/`duration_ms` comparisons (`lt`, `le`, `gt`, `ge`).
They do not read arbitrary task output or create a general dataflow system.
Preserve integer values exactly. See the
[WorkflowJob CRD](../../workflowjob-crd.yaml)
and [Function and Workflow tasks](../tasks/functions-and-workflows.md).

## JobRun and WorkflowRun

Run resources represent execution intent and observation. Ordinary users trigger
or cancel through the authorized API instead of constructing arbitrary run
resources. A run identifies its source and occurrence, pins execution content
and records the actor supplied by its trusted writer.

`Succeeded`, `Failed` and `Canceled` are terminal JobRun outcomes. WorkflowRun
also has terminal `Rejected` for an execution rejected during admission.
`ExecutionUnknown` and `CancelUnknown` preserve unresolved evidence. A missing
Job, an empty Pod list or an accepted cancel request alone cannot establish
termination. See [execution model](../concepts/execution-model.md).

## Definition acceptance and readiness

The typed API uses a dry-run digest, stable request UUID and expected immutable
Kubernetes identity/resource version for writes. A successful admission response
means the definition was accepted. Check observed generation, active revision
and readiness before triggering the requested version. After an ambiguous
write result, reconcile with the same request identity and live state rather
than submitting a different write. See [manage jobs](../tasks/manage-jobs.md).
