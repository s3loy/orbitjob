# Create and update jobs

A job definition supplies the container configuration for manual and scheduled
runs. Authoring saves a definition; the Operator publishes its executable
revision asynchronously. Existing runs keep their pinned revisions.

## Before you begin

Use an active workspace membership with `job:Apply`, `job:Get` and `job:List`.
Online authoring requires credentials without a resource-group restriction and
an execution namespace mapped to the workspace. The default Administrator
invitation does not supply `job:Apply`; ask the workspace administrator for an
explicit grant. Installation administrators configure the namespace mapping
and Kubernetes RBAC.

Use an approved runtime preset or custom container image. Review its code,
arguments, resource needs, allowed network destinations and repeat-execution
behavior. Definition
creation does not grant registry access, Secret access or arbitrary Kubernetes
resource access.

## Create a definition

1. Open **Jobs → Create a job**. Use the default Shell runtime, choose another
   runtime, choose a task template or reuse a Job. Set a unique name and authorized namespace.
2. Choose **Manual only** or **Scheduled and manual**. Job cron schedules use
   five fields and UTC. An empty schedule creates no automatic occurrences.
3. Review the timeout and run-policy defaults. Open resource and retry settings
   when the task needs overrides. Read the execution preview before saving.
4. Select **Review and save**, then **Check against cluster**. Review the exact
   admitted configuration, including changes made by Kubernetes admission.
5. Select **Save definition** and confirm. Retain the receipt and wait for
   **Saved version is ready** before requesting a run.

Cluster checking uses a dry-run request. Persistence requires that checked
configuration's digest and stable request identity. A successful save does not
assert Operator readiness or workload execution. Use
[the first-job tutorial](../tutorials/first-job.md#create-a-job-as-an-author)
for a complete example.

## Start from a task template

Select **Choose a task template** to prepare a message, an HTTP status check or
a JSON task. Each template supplies editable defaults and explains its inputs.
The execution preview updates as those inputs change. It shows the image,
program, script and separate argument values that will be copied into the Job.
Invalid inputs block application and show an error beside the affected field.

**Use this template** prepares a normal Job draft. It does not contact the HTTP
target, execute code, save a definition or start a run. Replacing an existing
draft requires confirmation and preserves its target namespace. Templates start
in manual mode with one attempt and no Pod retries. Review the generated name,
execution content and policies before saving; generated code remains editable.

The message template prints its text to logs. The HTTP template makes one GET
request without following redirects, compares the response status with the
expected status, and fails on a mismatch or network error. It requires the
execution container to reach the target. The JSON template formats inline JSON
or selects one top-level field and writes its JSON representation to logs.
These templates do not create persistent files. Input values become part of the
Job definition, so do not enter credentials or confidential data in them.

## Choose a runtime and execution content

New drafts start with a manual Shell example and `busybox:1.37.0`.
**Runtime** also offers Python with `python:3.14-slim` and Node.js with
`node:24-bookworm-slim`, each with editable Hello code. Switching untouched
examples needs no confirmation; replacing modified code or a custom image does.
**Custom image** preserves the current configuration. Additional dependencies
must be available in that image; selecting a runtime does not install packages.
Images still pass through the existing cluster checks and image policy.

**Command** accepts a program and its arguments in one field. Quotes group an
argument, including an empty value; a backslash escapes the next character
outside single quotes. This field does not execute shell syntax or expand
variables. Script modes pass multiline code to the corresponding interpreter.
Unfinished quotes and empty scripts block review and save. Imported definitions
with extra command entries retain their arrays in **Exact command and arguments**.
Kubernetes variable expansion and the image's startup configuration still apply.

## Generate a schedule

After selecting **Scheduled and manual**, choose **Generate schedule** for common
minute intervals, hourly, daily or weekly settings. Only **Use this schedule**
writes the generated five-field cron expression. Cancel keeps the existing
expression, including custom patterns that the generator cannot represent.
Job schedules use UTC: `0 2 * * *` means daily at 02:00 UTC. The generator does
not calculate future occurrences or enable automatic execution on its own.
Workflow authoring uses the same generator with its configured time zone.

## Update a definition

Open the job's **Definition** view and edit the live definition. Review and
check the changes again before saving. Writes compare the original UID and
resourceVersion; a concurrent change produces a conflict. Reload the changed
definition, review its differences and reapply the intended changes. Do not
bypass a conflict by submitting a create request for the same name.

Changing a definition does not alter an existing Run's pinned image, command,
resources or retry budget. If the projected generation is newer than your save
receipt, review that newer definition before triggering it.

## Configure execution behavior

`maxAttempts` includes the initial platform Attempt. Kubernetes `backoffLimit`
controls Pod replacement inside each Attempt. Version 1 requires `retrySafe`
when either setting enables repeated execution. `timeoutSeconds` applies to
each Attempt, so it is not a total wall-clock budget across retries. Confirm
that the container supports the non-root policy. See
[execution model](../concepts/execution-model.md#retries-and-deadlines).

Suspension prevents scheduled and manual starts. Separate source execution
controls require `job:ManageExecution`: schedule pause blocks new automatic
occurrences, while execution pause also freezes new runs and not-yet-started
dispatch. Already possibly started executions retain observation and cancellation.
Pausing a source does not stop those executions. Identity deletion is a separate,
irreversible control; use it only when retiring that source identity.

## Recover a save with no response

Keep the draft, target, request UUID, digest and original UID/resourceVersion.
Use the Console's reconciliation action to read the retained operation outcome.
Do not infer acceptance from an unrelated later definition that happens to match.
An unknown outcome requires investigation before another write. Durable operation
history is scoped to its actor and target; a request reference grants no access.

Source-control writes use a compare-and-set version. After a lost control-write
response, read current controls before deciding on another write. Equal current
state is not an independent historical operation receipt.

A 403 requires an access decision; rechecking or changing credentials cannot
safely resolve the intended permission. A 409 requires current state and review.
An unavailable Kubernetes API can prevent checking, saving or readiness even
while the Console remains reachable.

An installation upgraded from an older schema must apply migration 0028 before
checking definitions with a `namespace/name` longer than 64 characters. The
current schema retains the full identifier. See the
[database migration procedure](../database-setup.md#migration-execution-and-compatibility).
