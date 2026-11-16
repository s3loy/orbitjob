> Repository source: docs/getting-started.md. References marked repository require a source checkout.

# Getting started

OrbitJob runs container workloads as Kubernetes Jobs. Use the installed Console
to request work and inspect its outcome, or use the Admin API and CLI for
automation. An installation provides the execution environment; a workspace
provides the tenant boundary for definitions, runs and access policies.

## Choose your starting point

| Your task | Start here |
|---|---|
| Run a job supplied by your team | Run your first job (repository: `tutorials/first-job.md#run-an-existing-job`) |
| Create and run your own container job | Create your first job (repository: `tutorials/first-job.md#create-a-job-as-an-author`) |
| Connect a script or service | [Call the API](tasks/call-api.md) |
| Install OrbitJob for a team | Install OrbitJob (repository: `setup/installation.md`) |
| Understand runs, retries and uncertain outcomes | [Execution model](concepts/execution-model.md) |

## Obtain access

Ask the installation or workspace administrator for:

- The trusted Console address, such as `https://orbitjob.example.com/console/`.
- A workspace invitation or an existing account with active membership.
- The name of a job you can run, or an execution namespace and permission to
  create definitions if you are an author.
- Permission to read the run and, when needed, its execution logs.

Sign in with your personal username and password. Under **Account**, select an
active workspace and choose **Open workspace**. An API key cannot sign in to the
Console. API clients use a separate named key supplied by an administrator; keep
it in an authorization header. See accounts (repository: `accounts.md`) for invitations,
passwords, sessions and administrator setup.

A role name alone does not establish every capability. The default Operator
preset can trigger existing jobs and read logs. Viewer access cannot trigger;
the default Administrator preset does not include Job/Workflow `Apply` or
execution-log read actions. Authors need an explicit `job:Apply` grant and
readiness/read permissions. Installation-administrator status grants no implicit
workspace workload access. [Manage access](tasks/manage-access.md) lists the
capabilities required by each task.

## Follow one execution

Open **Jobs**, choose a job and select **Trigger**. Review the job and namespace
before submitting. Keep the receipt and follow it to the run. The receipt
confirms a run request; the Operator records and starts it asynchronously.
Forbid concurrency can delay admission while an earlier execution remains
active or uncertain.

Inspect the run's phase and attempt trail. `Succeeded`, `Failed` and `Canceled`
are confirmed terminal outcomes. `ExecutionUnknown` and `CancelUnknown` require
further observation; they do not establish that code stopped. Open **Execution
logs** when you have the separate log permission. Missing output cannot establish
a successful or failed business outcome.

Continue with [triggering and observation](tasks/trigger-and-observe.md),
[cancellation](tasks/cancel-a-run.md), or [reading logs](tasks/read-logs.md).
