# Run your first job

Use an existing job to learn the request-to-result flow. If you have authoring
access, the second part creates a small container job and runs it once.
The examples assume a configured installation with an active Operator and a
workspace namespace ready for execution. The output exercise also requires a
configured archive with available capacity; log access alone cannot reserve
space. If the archive reports a gap or capacity rejection, follow the
[log-state guidance](../tasks/read-logs.md) and verify the execution phase separately.

## Before you begin

Obtain the installation's Console address and an active workspace membership.
To run an existing job, you need `job:List`, `job:Get`, `job:Trigger`,
`instance:List` and `instance:Get` in the intended workspace, with no
resource-group restriction. Request `instance:ReadLogs` if you need output.
The Operator invitation preset includes these operations; custom policies and
boundaries can restrict them. A Viewer invitation does not permit triggering.

To create a job, also obtain `job:Apply`, an authorized namespace and an image
that the cluster can pull and run. Kubernetes namespace mappings, RBAC, storage
and network access are installation responsibilities. Creating a workspace alone
does not provide them.

Sign in and select the workspace under **Account → Open workspace**.

## Run an existing job

1. Open **Jobs**. Search for the job supplied by your administrator and open it.
   Confirm its namespace, image, command, timeout and retry settings.
2. Select **Trigger**. A suspended definition appears as **Paused** and cannot
   be triggered; ask its author to review and resume it.
3. Confirm the request in the dialog. Retain the receipt. A blank initial phase
   means the Operator has not yet reported a phase.
4. Follow the receipt to the recorded run, or use the job's **Runs** tab.
   Acceptance and execution start are separate events. Under Forbid, a manual
   request can wait without a ledger row while an earlier run holds the slot.
5. Inspect the phase and each Attempt. A platform retry produces another
   Attempt; Kubernetes backoff can create more than one Pod within one Attempt.
6. Open **Execution logs** to read retained events and output. Review any gap or
   collection warning before using the output as evidence.

The exercise is complete when the request is correlated to its recorded run
and a confirmed outcome is visible. A denied observation request, an empty
history page or an unavailable archive does not establish execution failure.
For an uncertain result, retain the receipt and use
[triggering and observation](../tasks/trigger-and-observe.md).

## Create a job as an author

Use a disposable job name in the authorized namespace. The following workload
prints one message and exits. Confirm that your installation permits the image;
a restricted registry or egress policy may require an administrator-provided
replacement.

| Setting | Value |
|---|---|
| Name | `hello-orbitjob` |
| Namespace | The execution namespace supplied by your administrator |
| Trigger mode | **Manual only** |
| Runtime | **Shell** (`busybox:1.37.0`) |
| Execution method | **Shell script** |
| Shell script | `printf 'hello from OrbitJob\n'` |
| Timeout | 60 seconds |
| Platform attempts | 1 |
| Kubernetes backoff limit | 0 |

The image tag is an example, not an immutable digest. Pin an approved digest
when reproducible image content is required. The image must support the
execution policy's non-root user and security settings.

1. Open **Jobs → Create a job** and enter the settings above. Keep the Shell
   runtime and replace its Hello example in **Shell script** with the script
   shown above. Leave additional arguments empty.
2. Review the execution preview and resource requests/limits. Leave retries
   disabled for this exercise. Enabling repeated execution requires an explicit
   retry-safe declaration under execution policy version 1.
3. Select **Review and save**, then **Check against cluster**. This validates
   the reviewed definition with Kubernetes admission without saving it.
4. Inspect the checked configuration, including any admission changes. Select
   **Save definition** and confirm the save.
5. Wait for **Saved version is ready**. A save receipt confirms the definition
   write, while readiness confirms the corresponding projected revision.
   If checks pause, select **Check readiness again**. If a newer version is
   active, open the job and review that version before running it.
6. Select **Run once**, then **Confirm run**, and follow the receipt as in the first part. With log
   access, find `hello from OrbitJob` in **Execution logs** and confirm the run's
   final phase separately.

A manual-only job has an empty schedule and remains available for future manual
requests. Saving it does not start it. If a save response is lost, use the
retained draft's reconciliation action before submitting another definition.
If the namespace, admission check, image pull or runtime fails, preserve the
reported error and ask the installation administrator to inspect the relevant
boundary. See [manage jobs](../tasks/manage-jobs.md) for normal authoring and
recovery procedures.

## Finish the exercise

Confirm the execution has reached a terminal outcome before retiring the test
workload. A manual-only definition starts no automatic work and can remain for
another exercise. If it is no longer needed, ask the namespace administrator to
remove the demonstration definition using the installation's approved native
resource procedure. Removing a definition is separate from cancelling an active
execution and from authorized log cleanup. Preserve an uncertain execution and
its receipt for investigation.
