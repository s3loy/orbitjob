> Repository source: docs/tasks/manage-access.md. References marked repository require a source checkout.

# Manage workspace access

Use personal accounts for the Console and named API keys for scripts or
services. Both require explicit permissions in the workspace. Installation
administration and workload authorization have separate scopes.

## Select the required capabilities

| Task | Required actions |
|---|---|
| Find and inspect jobs | `job:List`, `job:Get` |
| Create or update typed job definitions | `job:Apply` |
| Trigger a job | `job:Trigger` |
| List and inspect runs | `instance:List`, `instance:Get` |
| Cancel a run | `instance:Cancel` |
| Read or download job-run logs | `instance:ReadLogs` |
| Delete job-run logs | `instance:DeleteLogs` |
| Create/update a Function | `function:Create`, `function:Update`, respectively |
| Invoke a Function | `function:Invoke` |
| Read/delete Function logs | `function:ReadLogs`, `function:DeleteLogs`, respectively |
| Create/update a Workflow definition | `workflow:Apply` |
| Trigger or cancel a Workflow run | `workflow:Trigger` |
| Pause execution admission | The source kind's `ManageExecution` action |

Collection and detail reads have independent actions for Checks, SLIs, SLOs,
budgets and alerts. A grant for a parent definition does not imply every derived
read. Explicit Deny and policy boundaries remain effective even when another
policy allows an action.

Job, Workflow and instance surfaces require credentials without a resource-group
restriction when they cannot enforce group scope. Checks, Functions and monitoring
surfaces resolve their persisted parent group. A request reference or guessed
resource ID grants no permission.

## Invite a Console user

A workspace administrator opens the intended workspace, then
**Administration → Accounts**. Select Viewer, Operator or Administrator and
choose **Create invitation**. Optionally bind the invitation to a username.
Deliver the one-time link privately; it expires after 24 hours.

The default Viewer uses `ReadOnlyAccess`. Operator uses `JobOperator`, including
job trigger, run cancellation and execution-log reads. Administrator uses
`TenantAdminAccess`, including access administration, but that preset does not
include Job/Workflow `Apply` or execution-log read actions. Additional policy
grants are required for those tasks. The initial administrator and workspace
creator receive the explicit `ConsoleOwnerAccess` policy in their workspace.

Review actual policies and boundaries before choosing a role. Delegation cannot
exceed the administrator's effective grants or boundary. Disabling membership
blocks subsequent workspace requests. Account disablement and password reset
are global account operations reserved for installation administrators. See
account administration (repository: `../accounts.md#invite-and-manage-members`).

## Issue an automation key

Use **Administration** to create a named key with the minimum required policy
and, where supported, resource-group scope. Browser access writes require
password confirmation within the preceding 15 minutes. Copy the plaintext
result to its intended secret store; it is returned once. A key with no grants
can authenticate while every protected operation is denied.

Use a dedicated key for each caller so its identity and revocation are
independent. Key metadata does not recover the secret. Revoke a compromised or
unused key and issue a replacement through the authorized administrator.
Keep installation bootstrap keys out of ordinary workloads.

On a lost create response, reconcile the retained creation state and key
metadata before another creation. Do not automatically mint another credential.
The Console retains an available one-time result in page memory until it is
discarded or the connection is cleared; it cannot recover that plaintext after
it is lost.

## Diagnose access failures

A 401 means authentication was rejected. A 403 means the operation is denied;
it does not establish that a session expired. Check the active workspace,
membership status, exact action, resource scope, explicit Deny and policy
boundary. Installation-administrator status alone does not grant workspace
workload access. Escalating a key's scope is not a substitute for identifying
the missing intended grant.

See security model (repository: `../../SECURITY.md`) for the database, Kubernetes, session
and log boundaries that apply alongside HTTP authorization.

See [access policy reference](../reference/access-policies.md) for document fields,
resource patterns, Deny precedence and permission boundaries.
