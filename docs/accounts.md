# Personal accounts and workspace access

The Admin API supports personal usernames and passwords through server-managed
sessions. HTTP clients and scripts also use separate named API keys with
`Authorization: Bearer <key>`. API keys, including the bootstrap key, cannot
administer personal accounts. Accounts have stable user IDs independent of
API-key IDs. OIDC, passkeys, email delivery and multifactor authentication are
not implemented in this source.

These instructions apply to a source build containing migration
`0027_personal_accounts.up.sql` and its matching Admin image and chart. The
backend image provides account endpoints and `/orbitjob-identity` without a
compiled Console bundle; `/console/` returns 503 when that bundle is absent.
The account HTTP routes remain available. Upgrading source files does not
upgrade a running installation. See
[database compatibility](database-setup.md#personal-account-migration).

## Create the first administrator

The installer needs Kubernetes access to execute a command in the installed
Admin container, whose `ADMIN_DSN` uses the `orbitjob_admin` database role.
Complete database bootstrap and all migrations first. The Default workspace
must exist and be active. Configure the allowed origin as described under
[HTTPS and local development](#https-and-local-development).

Confirm the Kubernetes context, replace the example HTTPS origin with the
installation's actual origin, and run:

```bash
INSTALLATION_NAMESPACE='orbitjob-system'
kubectl -n "$INSTALLATION_NAMESPACE" exec deploy/orbitjob-admin-api -c admin-api -- \
  /orbitjob-identity -action setup -origin https://orbitjob.example.com
```

For an explicitly configured local development installation, use
`-origin http://localhost:18080` instead. An origin includes only the scheme,
host and optional port, without a path or trailing slash. The command can also
run outside Kubernetes with the same build and a privately supplied `ADMIN_DSN`.
It accepts `-origin` or defaults to `CONSOLE_PUBLIC_ORIGIN`.

The private JSON result contains `id`, `token`, `expires_at` and, when an origin
is supplied, `url`. The URL has the form
`https://orbitjob.example.com/console/connection#setup=<token>`. This URL requires
separately installed compatible browser assets. For the backend image, consume
`token` through `POST /api/v1/auth/setup` using a same-origin account client.
Send JSON fields `token`, `username`, `password` and optional `display_name`,
and the exact allowed `Origin` header. Store the returned session cookie
privately; the response contains the session snapshot and CSRF token.

The capability expires after 15 minutes by default; `-expires-in` may select a
positive duration up to one hour. Issuing another setup or recovery capability
invalidates earlier unconsumed installation capabilities. The database stores
only the token hash. Treat the command output as a credential and deliver it
only through an approved private channel.

Usernames contain 3-64 ASCII letters, digits, dots, hyphens or underscores,
begin with a letter or digit, and are normalized to lowercase. Passwords require
at least 15 characters and at most 1,024 UTF-8 bytes. Common passwords and short
repeated patterns are rejected. Use a unique password from a password manager.

The server consumes the capability, creates the installation administrator and
grants an explicit administrator membership with the owner policy in Default in
one transaction. A successful setup returns a session. The installation marker
remains claimed even if account rows are later removed; setup never reopens
automatically. Registration requires an installation capability or invitation.

## Sign in and choose a workspace

`POST /api/v1/auth/login` accepts `username`, `password` and `remember`.
`remember: true` creates a persistent cookie with a seven-day absolute server
lifetime. `remember: false` creates a browser-session cookie with an eight-hour
absolute server lifetime. Browser session restoration can retain session
cookies; sign out explicitly on shared devices. Ordinary requests do not extend
either lifetime. At expiry, sign in again.

`GET /api/v1/auth/session` returns initialization state and, when authenticated,
the account, session expiry, CSRF token and active workspace memberships. A
workload request selects the workspace with `tenant_id` in its query; it requires
an active membership and its effective policy grants. Installation-administrator
status permits global account and workspace-metadata management; it grants no
automatic access to workspace workloads. Creating a workspace grants its creator
an explicit owner membership. A new workspace still needs an execution namespace
mapping and Kubernetes RBAC before tasks can run; see
[namespace topology](../USAGE.md#71-namespace-topology).

A 403 denies the operation without establishing that the session has expired.
Stable user actor IDs identify account actions independently of API-key IDs.
The account API returns credentials and session snapshots with
`Cache-Control: no-store`; clients must protect cookies, CSRF tokens and capability
results.

## Invite and manage members

An authorized workspace administrator issues an invitation through
`POST /api/v1/auth/invitations`, providing `tenant_id`, `role` and the intended
policy scope. A username may bind the invitation to one account. Review the
assigned role and policy scope; delegation cannot exceed the administrator's
effective grants or policy boundary. Request schemas are in
[OpenAPI](../api/openapi.yaml).

Role defaults use explicit policy presets: Viewer uses `ReadOnlyAccess`,
Operator uses `JobOperator`, and Administrator uses `TenantAdminAccess`.
The existing administrator preset does not include Job/Workflow `Apply` or
execution-log read grants. Those actions require an explicit policy grant.
The first administrator and each workspace creator instead receive the
workspace-scoped `ConsoleOwnerAccess` policy, which includes those actions.

The returned one-time invitation capability must be delivered privately.
Invitations expire after 24 hours and grant membership only in their recorded
workspace. `POST /api/v1/auth/invitations/accept` accepts the capability token.
A recipient creating a new account supplies a username and password. An existing
account must first authenticate, then accept with its live session and CSRF
token. A supplied username must match that session. Invitation acceptance never
replaces an existing account's password. Expired, consumed or mismatched
capabilities are rejected.

`GET /api/v1/auth/members?tenant_id=<workspace_id>` lists workspace members;
`PATCH /api/v1/auth/members/{user_id}` changes the selected membership's role,
status or grants. Disabled membership blocks subsequent requests to that
workspace. Installation administrators use `GET /api/v1/auth/users` and
`PATCH /api/v1/auth/users/{id}` for global account status and
installation-administrator roles. Disabling an account revokes its sessions.
The final active installation administrator cannot be disabled or demoted.

Access changes require password confirmation within the preceding 15 minutes.
Submit the current password to `POST /api/v1/auth/reauth`, then retry the intended
action with the refreshed session snapshot. The same requirement protects
API-key, policy and resource-group writes made with a session. Each request
checks the current account, membership and policies; role changes and revocation
affect subsequent requests.

## Change or reset a password

`POST /api/v1/auth/password/change` accepts `current_password` and the new
`password`. A successful change revokes every session for that account,
including the current session. Sign in again with the new password.

After recent password confirmation, an installation administrator issues a
reset through `POST /api/v1/auth/members/{user_id}/password-reset`. This controls
a global identity, so workspace-administrator status alone cannot issue a reset.
The returned capability expires after one hour, is single use and is shown once.
Issuing another reset invalidates the earlier unconsumed reset for that account.
The recipient submits `token` and the new `password` to
`POST /api/v1/auth/password/reset`. Success revokes all of that account's
sessions; the recipient must sign in again. There is no email reset service.

`GET /api/v1/auth/sessions` lists the current account's sessions and expiration
times. `DELETE /api/v1/auth/sessions/{id}` revokes one of those sessions.
`POST /api/v1/auth/logout` revokes the current session and clears its cookie.
Sign out when leaving a shared device, even with `remember: false`.

## Recover installation administration

If no installation administrator can sign in, an operator with the installation's
Admin database access can issue a recovery capability:

```bash
INSTALLATION_NAMESPACE='orbitjob-system'
kubectl -n "$INSTALLATION_NAMESPACE" exec deploy/orbitjob-admin-api -c admin-api -- \
  /orbitjob-identity -action recovery -username administrator \
  -origin https://orbitjob.example.com
```

Replace `administrator` with the account to recover. `-username` is optional;
binding it limits which username may consume the capability. Recovery uses
`POST /api/v1/auth/setup` and the same lifetime controls as initial setup. It can
reactivate an existing account, reset its password and installation-administrator
role, or create a replacement account. It grants explicit owner membership in
Default and revokes existing sessions for a recovered account. It requires an
already claimed installation and an active Default workspace. This is an
operator-level recovery procedure; legacy API keys do not prove ownership of
an account or reopen setup.

## HTTPS and local development

Production sessions use `__Host-orbitjob_session` with `HttpOnly`, `Secure`,
`SameSite=Strict`, `Path=/` and no `Domain`. The session secret is stored only as
a hash in PostgreSQL. The session snapshot returns a CSRF token separate from
the session credential. Cookie-authenticated writes require both an exact
allowed `Origin` and `X-CSRF-Token`. Login and capability submission also enforce
the allowed origin. An explicit invalid Bearer header fails without falling
back to a session cookie. Account endpoints reject automation Authorization
headers.

Set `CONSOLE_PUBLIC_ORIGIN` on Admin, or Helm
`adminapi.console.publicOrigin`, to the exact public HTTPS origin. When omitted,
the server derives `https://` plus the request Host. Preserve the public Host
through a reverse proxy. Forwarded headers do not select a trusted origin or
disable cookie security. Review this setting when the public address changes.

`CONSOLE_ALLOW_INSECURE_LOCALHOST` defaults to `false`. For HTTP development,
set it to `true`, or set Helm `adminapi.console.allowInsecureLocalhost: true`.
Only loopback hosts such as `localhost`, `127.0.0.1` and `::1` qualify. That mode
uses `orbitjob_session_local` without `Secure`; it does not permit HTTP login
through a node IP or public address. The kind development values explicitly
enable this local mode. Keep the client Host and Origin consistent.

An `HttpOnly` cookie still authorizes its holder. Serve account clients over
HTTPS, protect administrator devices and limit session lifetime on shared
browsers. A script running in the account client's origin can act through that
session despite `HttpOnly`. Passwords use Argon2id with 64 MiB memory, three
iterations and parallelism one. API keys retain their separate bcrypt
verification and automation policies.
