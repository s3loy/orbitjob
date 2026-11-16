---
name: orbitjob-workloads
description: Create or edit OrbitJob ScheduledJob and WorkflowJob YAML for import into the Console, using the bundled installation schemas and API reference.
---

# OrbitJob workloads

Produce an editable workload manifest for the user's installation. This package
contains no credentials and grants no permission to contact or modify a cluster.

## Establish the workload

Use the requested kind, namespace, image, command arguments and timing. Ask for
missing operational choices rather than inventing private image references or
an installation namespace. A tenant ID is not a Kubernetes namespace; the
installation administrator must have mapped that namespace to the tenant.

Read the matching CRD in `references/` before generating YAML:

- ScheduledJob: [ScheduledJob CRD](references/scheduledjob-crd.yaml), with a
  [manual job example](assets/manual-job.yaml).
- WorkflowJob: [WorkflowJob CRD](references/workflowjob-crd.yaml), with a
  [dependent tasks example](assets/manual-workflow.yaml).

Use `workloads.orbitjob.io/v1alpha1`, one document, and the requested resource
name. Metadata contains only `name` and `namespace`; labels, annotations, status,
UID and resourceVersion cannot be imported. Kubernetes Pod, Deployment, Job and
CronJob manifests are different resources and cannot be imported here.

## Preserve execution semantics

An empty `spec.schedule` means manual only. For a scheduled workload, use a
five-field cron expression. WorkflowJob uses `spec.timeZone` with an IANA zone
such as `Asia/Shanghai`; ScheduledJob has no separate timeZone field. Preview
workflow trigger times in the Console before saving. A scheduled, unsuspended
resource can start automatically after admission; a manual job requires an
explicit trigger after the requested generation becomes ready.

ScheduledJob's typed `jobTemplate` accepts only `image`, `command`, `args`, `backoffLimit` and supported
`resources` requests/limits. Arbitrary Pod fields such as `env` and `imagePullSecrets` are
unsupported; explain this limit when the user's workload requires them. Preserve
each command argument literally, including spaces and empty strings. Prefer an
explicit image tag or digest selected by the user. Registry credentials belong
in the installation configuration, outside the manifest and AI conversation.

Workflow tasks reference existing ScheduledJobs in the same namespace using
`jobRef.name`. Create those Jobs first. Task names must be unique and dependencies
must resolve without cycles. Conditions reference declared dependencies, with
phase comparisons (`eq`, `ne`, `in`, `not_in`) or numeric attempts/duration_ms
comparisons (`lt`, `le`, `gt`, `ge`). Conditions cannot inspect exit codes or
arbitrary container outputs. Preserve signed 64-bit comparison numbers exactly.
Forbid prevents overlapping workflow runs; pausing its schedule leaves manual
runs available. Use a separate cancel action for an execution already running.

For new version 1 jobs, set `executionPolicyVersion: 1` and use the supported
CPU, memory and ephemeral-storage resource dimensions. Set `retrySafe: true`
only when the user confirms repeated execution is safe. Version 1 requires it
before enabling Pod backoff or more than one platform attempt. A declaration
does not make side effects idempotent. `timeoutSeconds` applies to each attempt's
Kubernetes Job, including its Pod retries; it is not the Run's total deadline.
Read the [resource reference](references/docs/reference/workload-resources.md)
when choosing these fields.

## Deliver and validate

Return the YAML plus a short explanation of when it runs, the referenced Jobs
or image, and any administrator setup still required. Explain unverified choices
explicitly. A schema-valid image reference does not prove registry pull access.

Guide the user through the Console: Advanced YAML → paste/import the manifest →
Import YAML into form → review configuration → Review and save → Check against
cluster → Save definition → Confirm save. Local checks and dry-run do not save a resource.
Wait for the saved resource's requested generation to become ready before
offering a trigger or invocation. Kubernetes acceptance alone does not prove execution.

For API integration, read the generated [API index](references/api-index.md),
[complete OpenAPI snapshot](references/openapi.yaml), and the relevant task linked from
the [usage index](references/usage.md). Definitions use typed spec JSON,
not a YAML request body; dry-run returns a digest required for the identical
apply, with a stable request UUID and expected UID/resourceVersion. Keep exact
int64 values. Resolve a 409 or uncertain response by reading live state, without
blindly retrying a write. Current installation `/openapi.json` is authoritative
if it differs from this package's recorded source hashes in `sources.json`.
