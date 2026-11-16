> Repository source: docs/tasks/checks-and-slos.md. References marked repository require a source checkout.

# Configure Checks and SLOs

Checks schedule HTTP probes as container executions. Their recorded outcomes
can supply service-level indicators (SLIs). Service-level objectives (SLOs)
evaluate those indicators into error budgets and alerts.

## Before you begin

Use an active workspace and grants for the operations you need. Creating a
Check, SLI or SLO requires `check:Create`, `sli:Create` or `slo:Create`,
respectively. Definition reads, CheckRun evidence, budgets, budget history and
alerts have independent permissions. Group-scoped access follows the actual
parent Check or SLO and its group; a parent read grant does not imply every
derived read.

The installation must have an execution namespace for Checks and the ability to pull the
digest-pinned probe image. The HTTP target must be reachable from workload Pods under the
installation's network policy. A successful request from your laptop does not
prove that the probe can reach it.

## Create and observe a Check

Open **Checks** in the Console and choose **New check**. Set a name,
`http_health` configuration, schedule, timeout and retry limit.
Schedules support cron or intervals; intervals must be at least 30 seconds.
The configured timezone must be valid. Review the HTTP URL, supported method and
expected status before creating the Check. Do not put credentials in an
unprotected target URL or copied diagnostic output.

Creation stores the configuration. The Operator schedules the probe, records
its container execution and derives a CheckRun from the confirmed terminal
outcome. Inspect the CheckRun result and underlying execution evidence before
changing the probe configuration. Pause and resume require `check:Pause` and `check:Resume`.
Pausing new checks does not cancel a possibly started execution.

Only a plan eligible for the active execution revision can start. If a due time
precedes that revision's activation, the Operator skips the unaccepted plan and
advances to the next interval or cron time. It does not replay that skipped
plan. A new or resumed Check can therefore wait until its next eligible schedule
before producing a result.

A missed or unavailable result does not establish a healthy target. Probe
collection, workload execution and derived projection can fail independently.
Inspect the recorded error and installation health before interpreting an
empty result set.

## Create an SLI

Choose the intended check or executable source and configure the criterion
against facts actually produced by its outcomes. Run-backed sources use
`source_uid`, not a display name or revision ID. Supported criterion keys are
`status` (`success` or `failed`) and `duration_ms`; numeric duration criteria
support `==`, `!=`, `<`, `<=`, `>` and `>=` with finite nonnegative thresholds.
Latency SLIs require a duration criterion. Creating availability or quality
SLIs also requires a nonempty good-event criterion.

Results aggregate into five-minute buckets. Schedule adherence has no dedicated
SLI producer. A criterion unsupported by the produced facts cannot establish
an objective merely because its name describes one.

## Create and inspect an SLO

Select an accessible SLI in the same tenant and, for a group-scoped caller, the
same group. Set the target and supported evaluation window: rolling, calendar
month or quarter. The scheduler evaluates snapshots into budgets and burn
alerts; it does not execute probe Jobs.

Read the current budget, budget history and alerts using their separate grants
(`slo:GetBudget`, `slo:ListBudgets`, `sloalert:Get`, `sloalert:List`). Collection
authorization must cover the complete query scope. A restrictive Deny or boundary
can reject the read instead of returning silently filtered counts.

A window with no observed events is unevaluated. Inactive sources or stale
measurements also produce unevaluated budgets rather than a healthy result.
A target of 100% has no allowed errors; observed errors exhaust its budget.
Inspect totals and evaluation reasons alongside the budget. Pausing an SLO
stops normal evaluation while preserving its stored definition and prior evidence.

## Change or remove monitoring resources

The API uses optimistic versions for Check/SLI/SLO mutations that require them.
Read the current version before a versioned request. DELETE bodies carry the
required version declared in [OpenAPI](../../openapi.yaml); a request with
only an ID is incomplete. On a lost creation response, inspect retained Console
operation state and the collection before creating a replacement. Do not assume
a monitoring create is safe to replay with a new identity.

See architecture (repository: `../architecture.md#terminal-recovery-and-slos`) for outcome
projection and deduplication, and security (repository: `../../SECURITY.md#monitoring-evidence`)
for the parent and collection authorization boundaries.
