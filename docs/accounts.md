# Console accounts and workspace access

Console users sign in with personal usernames and passwords. Scripts and
services use separate named API keys in `Authorization: Bearer <key>`. API keys,
including the installation bootstrap key, cannot sign in to the Console.
Personal accounts have stable user IDs independent of API-key IDs.

For an ordinary user, begin with [sign-in](#sign-in-and-choose-a-workspace).
Installation operators create the first administrator and perform out-of-band
recovery. Workspace administrators invite members and assign workload access.
This source implements no OIDC, passkeys, email delivery or multifactor
authentication.

These procedures require migration `0027_personal_accounts.up.sql` and the
matching Admin image/chart. Updating a checkout does not update a running
installation. Follow the [account migration procedure](database-setup.md#schema-privileges-and-account-changes)
before enabling accounts on an existing installation.

## Create the first administrator

The installation operator needs Kubernetes permission to execute the identity
command in the installed Admin container. That container's `ADMIN_DSN` must
use the `orbitjob_admin` database role. Complete database bootstrap and all
migrations first; the Default workspace must exist and be active. Configure
the exact Console origin as described in
[HTTPS and local development](#https-and-local-development).

Confirm the intended cluster/context, set the installation namespace and use
its actual public origin:

```sh
INSTALLATION_NAMESPACE='orbitjob-system'
kubectl -n "$INSTALLATION_NAMESPACE" exec deploy/orbitjob-admin-api -c admin-api -- \
  /orbitjob-identity -action setup -origin https://orbitjob.example.com
```

For explicitly configured local development, the origin can be
`http://localhost:18080`. An origin contains only scheme, host and optional
port, without a path or trailing slash. The command can also run outside
Kubernetes with the same build and a privately supplied `ADMIN_DSN`. If
`-origin` is omitted, it reads `CONSOLE_PUBLIC_ORIGIN`.

The private JSON result contains `id`, `token`, `expires_at` and, when an
origin is supplied, `url`. The link has the form
`https://orbitjob.example.com/console/connection#setup=<token>`. Its default
lifetime is 15 minutes; `-expires-in` accepts a positive duration up to one
hour. Issuing a setup or recovery capability invalidates earlier unconsumed
installation capabilities. The database stores the token hash.

Deliver the result through an approved private channel. In a trusted browser,
open the link, enter a username, optional display name and password, and select
**Create administrator account**.

Usernames contain 3–64 ASCII letters, digits, dots, hyphens or underscores,
begin with a letter or digit and normalize to lowercase. Passwords require
at least 15 characters and at most 1,024 UTF-8 bytes. The local common-password
blocklist and short repeated patterns are rejected. Use a unique password.

Setup consumes the capability, creates the installation administrator and
grants explicit owner membership in Default in one transaction. The Console
then signs in that account. The installation remains claimed even if account
rows are later removed; setup does not reopen automatically. There is no open
registration without an installation capability or invitation.

The Console removes the link fragment from the address bar and holds its token
in page memory. Reloading or choosing **Discard link** clears that copy. An
unused capability remains valid until expiration or replacement.

## Sign in and choose a workspace

Open the installation's trusted `/console/` address. Enter your username and
password and select **Sign in**. Obtain a workspace invitation from its
administrator if you do not yet have an account or active membership.

**Keep me signed in for 7 days** is selected by default. It creates a persistent
cookie and a seven-day absolute server session. Clearing it creates a
browser-session cookie with an eight-hour absolute server lifetime. Requests
and polling do not extend either lifetime. At expiry, sign in again. Browser
session restoration can retain session cookies; sign out explicitly on shared
devices.

Under **Account**, select an active workspace and choose **Open workspace**.
Each workload request requires current active membership and effective policy
grants. Installation-administrator status permits global account and workspace
metadata management; it grants no automatic workload access. Workspace creation
gives its creator an explicit owner membership, but execution still requires
an installation namespace mapping and Kubernetes RBAC. Ask the installation
operator to configure them before running work.

The browser stores the non-secret selected workspace ID and unconfirmed
source-control intent in `sessionStorage`. Retained source-control intent is
scoped to the authenticated subject, workspace and source so the same account
can review it after reconnecting. Protected in-memory query and operation state
clears on sign-out, a 401, or an identity/workspace change. A 403 denies the
operation without establishing session expiry.

Job, Workflow and Function authoring drafts remain only in page memory for the
current connection. Reloading, ending or changing that connection clears them;
server-side private draft persistence and recovery are unavailable. Save
unfinished content outside the Console before doing so. Accepted definition-save
and Function-create operations retain actor/request identity for reconciliation
after reconnecting; their receipts do not recover unsaved authoring drafts.

## Invite and manage members

A workspace administrator opens the intended workspace and navigates to
**Administration → Accounts**. Choose a role, optionally bind the invitation
to a username, and select **Create invitation**. The default presets differ:

| Invitation role | Default policy | Workload implications |
|---|---|---|
| Viewer | `ReadOnlyAccess` | Read operations from the preset; no trigger or log read grant. |
| Operator | `JobOperator` | Existing-workload reads, job/workflow trigger, run cancellation, Function invocation and log reads; no Job/Workflow Apply. |
| Administrator | `TenantAdminAccess` | Workspace access administration and configured management actions; no Job/Workflow Apply or execution-log read grant. |

The initial administrator and each workspace creator receive the explicit
`ConsoleOwnerAccess` policy in their workspace, which includes authoring and
log actions. Custom policies, explicit Deny and boundaries can further change
effective access. Review the actual grants before promising a member that a
role can perform a task. Delegation cannot exceed the administrator's effective
grants or policy boundary. [Manage access](tasks/manage-access.md) maps common
tasks to actions.

Copy the one-time **Invitation link** and deliver it privately. It expires after
24 hours and applies only to its recorded workspace. A new user creates an
account through the link. An existing user selects **I already have an account**,
signs in and accepts using that account's live session. For a username-bound
invitation, the username must match. An invitation does not replace an existing
password. Expired, consumed and mismatched links are rejected.

The member table can change roles or disable membership. Membership disablement
blocks subsequent requests to that workspace. Under **Installation accounts**,
an installation administrator can change global account status and installation
administrator roles. Disabling an account revokes its sessions. The final active
installation administrator cannot be disabled or demoted.

Access changes require password confirmation within the preceding 15 minutes.
Complete **Confirm your password** and retry the intended action when prompted.
The same requirement protects API-key, policy and resource-group writes made
through a browser session. Each request rechecks current account, membership
and policy state, so revocation and role changes affect subsequent requests.

## Change or reset a password

Under **Account → Change password**, supply the current password and new
password and select **Change password and sign out**. Success revokes every
session for that account, including this browser. Sign in again.

An installation administrator can select **Create password reset link** for an
account after recent password confirmation. This changes a global identity;
workspace-administrator status alone cannot issue it. The link is shown once,
is single use and expires after one hour. Issuing a new reset invalidates the
previous unconsumed reset for that account. Successful reset revokes all of
that account's sessions. There is no email reset service.

Expand **Signed-in sessions** under **Account** to inspect expiration times and
revoke a session. Revoking the current one signs this browser out. Sign out
before leaving a shared device, including when the remember option is cleared.

## Recover installation administration

An operator with the installation's Admin database access can issue an
out-of-band recovery capability when no installation administrator can sign in:

```sh
INSTALLATION_NAMESPACE='orbitjob-system'
kubectl -n "$INSTALLATION_NAMESPACE" exec deploy/orbitjob-admin-api -c admin-api -- \
  /orbitjob-identity -action recovery -username administrator \
  -origin https://orbitjob.example.com
```

Replace `administrator` and the origin with the intended account and trusted
installation origin. `-username` is optional; supplying it restricts which
username can consume the capability. The setup form and lifetime controls are
the same as initial setup.

Recovery requires an already claimed installation and active Default workspace.
It can reactivate an existing account, reset its password and restore its
installation-administrator status, or create a replacement account. It grants
explicit owner membership in Default and revokes the recovered account's
existing sessions. Treat this command's output as an installation credential.
Legacy API keys do not establish personal-account ownership or reopen setup.

## HTTPS and local development

Production uses `__Host-orbitjob_session` with `HttpOnly`, `Secure`,
`SameSite=Strict`, `Path=/` and no `Domain`. PostgreSQL stores the hash of the
session secret. A separate CSRF token remains in page memory. Cookie-authenticated
writes require an exact allowed `Origin` and `X-CSRF-Token`; login and capability
submission also check Origin. An explicit invalid Bearer header fails without
falling back to the session cookie.

Set `CONSOLE_PUBLIC_ORIGIN`, or Helm `adminapi.console.publicOrigin`, to the
exact public HTTPS origin. If omitted, Admin expects `https://` plus request
Host. Preserve the public Host through a reverse proxy. Forwarded headers do
not select a trusted origin or relax cookie security. Review this setting when
the address changes; rebuilding the frontend is unnecessary.

`CONSOLE_ALLOW_INSECURE_LOCALHOST` defaults to `false`. Explicit HTTP development
uses `true`, or Helm `adminapi.console.allowInsecureLocalhost: true`, and permits
only loopback hosts such as `localhost`, `127.0.0.1` and `::1`. It uses
`orbitjob_session_local` without `Secure`. A node IP or public HTTP address cannot
sign in through that exception. For the frontend development proxy, keep browser
Host and Origin consistent and leave `publicOrigin` empty or set it to the
frontend origin. See [frontend development](local-deployment.md#change-check-and-redeploy).

An HttpOnly cookie still authorizes its holder, and same-origin script execution
can act through it. Serve the Console over HTTPS, preserve its CSP and protect
administrator devices. Password hashes use Argon2id with 64 MiB memory, three
iterations and parallelism one. API keys retain separate bcrypt verification
and automation policies.
