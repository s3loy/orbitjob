// Generated from api/openapi.yaml; run npm run generate.
export interface paths {
    "/api/v1/api_keys/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        /**
         * Rename an API key
         * @description Update only an API key's optional display name. Requires apikey:Update for the exact owner and key scope. An empty name clears the label; grants, credentials and revocation are unchanged.
         */
        patch: operations["renameAPIKey"];
        trace?: never;
    };
    "/api/v1/api_keys/{id}/revoke": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Revoke one API key
         * @description Revoke an API key within its resolved owner and resource-group scope. Requires an explicit apikey:Revoke grant for that target; tenant identity alone does not grant cross-tenant authority. The key must not already be revoked.
         */
        post: operations["revokeAPIKey"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/auth/invitations": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Issue a single-use workspace invitation
         * @description Issue a single-use workspace invitation valid for 24 hours. Requires an active administrator membership in the selected active workspace and password confirmation within 15 minutes. Granted permissions must remain within the caller's effective grants and boundary. Installation administrator status alone does not grant this authority. Browser credentials are HttpOnly SameSite=Strict cookies. Credential and capability submissions require the exact configured Origin; cookie-authenticated writes additionally require X-CSRF-Token from the same-origin session snapshot. Session and account authority are evaluated against current server state. API keys cannot administer human accounts.
         */
        post: operations["inviteConsoleMember"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/auth/invitations/accept": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Consume a workspace invitation
         * @description Consume a valid, unexpired workspace invitation. Creating a new account requires a username and new password and signs in that account. An existing account must use its own active session cookie and CSRF token; username may be omitted or must match that session, and password may be omitted. Existing account credentials and display name are unchanged. A username-bound invitation must match the accepting account. No recent password confirmation is required. Browser credentials are HttpOnly SameSite=Strict cookies. Credential and capability submissions require the exact configured Origin; cookie-authenticated writes additionally require X-CSRF-Token from the same-origin session snapshot. Session and account authority are evaluated against current server state. API keys cannot administer human accounts.
         */
        post: operations["acceptConsoleInvitation"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/auth/login": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Authenticate a human account
         * @description Authenticate an active account with its username and current password, and create a session. Password confirmation starts the 15-minute recent-authentication window. Browser credentials are HttpOnly SameSite=Strict cookies. Credential and capability submissions require the exact configured Origin; cookie-authenticated writes additionally require X-CSRF-Token from the same-origin session snapshot. Session and account authority are evaluated against current server state. API keys cannot administer human accounts.
         */
        post: operations["loginConsole"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/auth/logout": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Revoke the current human session
         * @description Revoke the caller's current session and clear its cookie. Requires that session; no recent password confirmation is required. Browser credentials are HttpOnly SameSite=Strict cookies. Credential and capability submissions require the exact configured Origin; cookie-authenticated writes additionally require X-CSRF-Token from the same-origin session snapshot. Session and account authority are evaluated against current server state. API keys cannot administer human accounts.
         */
        post: operations["logoutConsole"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/auth/members": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * List workspace members
         * @description List members of the workspace selected by tenant_id. Requires an active administrator membership in that active workspace and password confirmation within 15 minutes. Installation administrator status alone does not grant workspace membership access. Browser credentials are HttpOnly SameSite=Strict cookies. Credential and capability submissions require the exact configured Origin; cookie-authenticated writes additionally require X-CSRF-Token from the same-origin session snapshot. Session and account authority are evaluated against current server state. API keys cannot administer human accounts.
         */
        get: operations["listConsoleMembers"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/auth/members/{user_id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        /**
         * Change a workspace membership
         * @description Change a member's status, role, policies or boundary in the selected workspace. Requires an active administrator membership in that active workspace and password confirmation within 15 minutes. Changed grants and reactivation must remain within the caller's effective grants and boundary. Installation administrator status alone does not grant this authority. Browser credentials are HttpOnly SameSite=Strict cookies. Credential and capability submissions require the exact configured Origin; cookie-authenticated writes additionally require X-CSRF-Token from the same-origin session snapshot. Session and account authority are evaluated against current server state. API keys cannot administer human accounts.
         */
        patch: operations["updateConsoleMember"];
        trace?: never;
    };
    "/api/v1/auth/members/{user_id}/password-reset": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Issue a single-use account password reset
         * @description Issue a single-use password reset capability valid for one hour for the selected installation account. Requires installation administrator status and password confirmation within 15 minutes. Workspace administrator membership alone is insufficient; tenant_id does not narrow this installation-scoped authority. Issuing a new reset capability invalidates earlier outstanding reset capabilities for that account. Browser credentials are HttpOnly SameSite=Strict cookies. Credential and capability submissions require the exact configured Origin; cookie-authenticated writes additionally require X-CSRF-Token from the same-origin session snapshot. Session and account authority are evaluated against current server state. API keys cannot administer human accounts.
         */
        post: operations["issueConsolePasswordReset"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/auth/password/change": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Change the current account password and revoke every session
         * @description Change the caller's password using its active session and current password. The operation verifies the supplied current password without requiring a previous recent-authentication window, revokes all of that account's sessions and outstanding reset capabilities, and clears the current cookie. Sign in again after success. Browser credentials are HttpOnly SameSite=Strict cookies. Credential and capability submissions require the exact configured Origin; cookie-authenticated writes additionally require X-CSRF-Token from the same-origin session snapshot. Session and account authority are evaluated against current server state. API keys cannot administer human accounts.
         */
        post: operations["changeConsolePassword"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/auth/password/reset": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Consume a single-use password reset capability
         * @description Consume a valid, unexpired single-use reset capability and set a new account password. No session is required. Success revokes all of that account's sessions and outstanding reset capabilities and clears the current cookie. Browser credentials are HttpOnly SameSite=Strict cookies. Credential and capability submissions require the exact configured Origin; cookie-authenticated writes additionally require X-CSRF-Token from the same-origin session snapshot. Session and account authority are evaluated against current server state. API keys cannot administer human accounts.
         */
        post: operations["resetConsolePassword"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/auth/reauth": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Confirm the account password for sensitive changes
         * @description Confirm the current account password for the caller's active session and renew its 15-minute recent-authentication window. An expired recent-authentication window does not prevent this operation. Browser credentials are HttpOnly SameSite=Strict cookies. Credential and capability submissions require the exact configured Origin; cookie-authenticated writes additionally require X-CSRF-Token from the same-origin session snapshot. Session and account authority are evaluated against current server state. API keys cannot administer human accounts.
         */
        post: operations["reauthenticateConsole"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/auth/session": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Read the current human session
         * @description Read installation initialization state and, when a valid session cookie is present, the current account, active workspace memberships and CSRF token. No login is required for an unauthenticated snapshot. Browser credentials are HttpOnly SameSite=Strict cookies. Credential and capability submissions require the exact configured Origin; cookie-authenticated writes additionally require X-CSRF-Token from the same-origin session snapshot. Session and account authority are evaluated against current server state. API keys cannot administer human accounts.
         */
        get: operations["getConsoleSession"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/auth/sessions": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * List the current account sessions
         * @description List unexpired sessions belonging to the caller's account. Requires an active session; no recent password confirmation is required. Browser credentials are HttpOnly SameSite=Strict cookies. Credential and capability submissions require the exact configured Origin; cookie-authenticated writes additionally require X-CSRF-Token from the same-origin session snapshot. Session and account authority are evaluated against current server state. API keys cannot administer human accounts.
         */
        get: operations["listConsoleSessions"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/auth/sessions/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        post?: never;
        /**
         * Revoke one of the current account sessions
         * @description Revoke a session belonging to the caller's account, including the current session. Requires an active session with password confirmation within 15 minutes. Other accounts' sessions cannot be revoked through this operation. Browser credentials are HttpOnly SameSite=Strict cookies. Credential and capability submissions require the exact configured Origin; cookie-authenticated writes additionally require X-CSRF-Token from the same-origin session snapshot. Session and account authority are evaluated against current server state. API keys cannot administer human accounts.
         */
        delete: operations["revokeConsoleSession"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/auth/setup": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Consume the installer capability and create the first administrator
         * @description Consume a valid, unexpired installer setup or recovery capability to create or recover an installation administrator and its default-workspace administrator membership. Requires the capability and account credentials; no existing session is required. A recovery capability can be bound to a username. Browser credentials are HttpOnly SameSite=Strict cookies. Credential and capability submissions require the exact configured Origin; cookie-authenticated writes additionally require X-CSRF-Token from the same-origin session snapshot. Session and account authority are evaluated against current server state. API keys cannot administer human accounts.
         */
        post: operations["setupConsole"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/auth/users": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * List installation accounts
         * @description List installation accounts. Requires installation administrator status and password confirmation within 15 minutes; workspace administrator membership alone is insufficient. Browser credentials are HttpOnly SameSite=Strict cookies. Credential and capability submissions require the exact configured Origin; cookie-authenticated writes additionally require X-CSRF-Token from the same-origin session snapshot. Session and account authority are evaluated against current server state. API keys cannot administer human accounts.
         */
        get: operations["listConsoleUsers"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/auth/users/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        /**
         * Change an installation account
         * @description Change an installation account's status, installation administrator flag or display name. Requires installation administrator status and password confirmation within 15 minutes. Disabling an account revokes its sessions. The last active installation administrator cannot be disabled or demoted. Browser credentials are HttpOnly SameSite=Strict cookies. Credential and capability submissions require the exact configured Origin; cookie-authenticated writes additionally require X-CSRF-Token from the same-origin session snapshot. Session and account authority are evaluated against current server state. API keys cannot administer human accounts.
         */
        patch: operations["updateConsoleUser"];
        trace?: never;
    };
    "/api/v1/check-runs": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * List check runs
         * @description List check execution runs for one tenant, filtered through each parent Check's resource group. Authorization must cover the whole tenant/group query domain, including explicit Deny and boundaries. If that proof fails or exceeds its budget, the entire request returns 403 without reading items or counts; filters and pagination do not bypass this check.
         */
        get: operations["listCheckRuns"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/check-runs/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Get one check run
         * @description Get one check run by id. Authorization resolves its stored parent Check's actual resource group, including for ungrouped credentials, so group-specific Deny remains effective. A credential scoped to another group reads this target as not found.
         */
        get: operations["getCheckRun"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/checks": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * List checks
         * @description List inspection checks within the authenticated tenant and resource group. Optional search is a case-insensitive literal substring of name or description, trimmed and bounded to 128 characters, applied before pagination. Percent and underscore are literal characters, not wildcards.
         */
        get: operations["listChecks"];
        put?: never;
        /**
         * Create one check
         * @description Create an inspection check definition.
         */
        post: operations["createCheck"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/checks/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Get one check
         * @description Get one check by id.
         */
        get: operations["getCheck"];
        put?: never;
        post?: never;
        /**
         * Delete one check
         * @description Soft-delete a check definition by setting deleted_at. The version body is required for optimistic locking.
         */
        delete: operations["deleteCheck"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/checks/{id}/execution-controls": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * get source execution controls
         * @description Schedule pause blocks new scheduled occurrences. Execution pause also freezes new root runs, workflow steps, and Pending or Prepared dispatch. Possibly started attempts retain recovery and cancellation. Deletion is an independent irreversible identity tombstone. Compare-and-set version zero represents previously unset controls. A lost response requires a read before any new write; equal current state is not an independent historical operation receipt.
         */
        get: operations["getcheckExecutionControls"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        /**
         * update source execution controls
         * @description Schedule pause blocks new scheduled occurrences. Execution pause also freezes new root runs, workflow steps, and Pending or Prepared dispatch. Possibly started attempts retain recovery and cancellation. Deletion is an independent irreversible identity tombstone. Compare-and-set version zero represents previously unset controls. A lost response requires a read before any new write; equal current state is not an independent historical operation receipt.
         */
        patch: operations["updatecheckExecutionControls"];
        trace?: never;
    };
    "/api/v1/checks/{id}/pause": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Pause one check
         * @description Pause an active check definition using optimistic locking by version.
         */
        post: operations["pauseCheck"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/checks/{id}/resume": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Resume one check
         * @description Resume a paused check definition using optimistic locking by version.
         */
        post: operations["resumeCheck"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/definitions/jobs/{namespace}/{name}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Read live typed job definition
         * @description Only configured tenant namespaces and ungrouped credentials. spec_json is a canonical typed JSON string preserving integer precision. PUT dry_run performs Kubernetes admission without persistence; actual writes require its digest, stable request UUID and expected UID/resourceVersion. Accepted CRs do not assert operator projection. Repeating the same accepted request and digest reconciles; changed identity or content conflicts.
         */
        get: operations["getjobDefinition"];
        /**
         * Validate or apply typed job definition
         * @description Only configured tenant namespaces and ungrouped credentials. spec_json is a canonical typed JSON string preserving integer precision. PUT dry_run performs Kubernetes admission without persistence; actual writes require its digest, stable request UUID and expected UID/resourceVersion. Accepted CRs do not assert operator projection. Repeating the same accepted request and digest reconciles; changed identity or content conflicts.
         */
        put: operations["applyjobDefinition"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/definitions/jobs/{namespace}/{name}/compat": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        /**
         * Apply a legacy definition with mandatory server checks
         * @description Explicit checked-save-v1 adapter. Checks the exact legacy payload on the server; execution changes introduced by admission require an upgraded client's review. Stable request identity and normal authorization still apply.
         */
        put: operations["applyjobDefinitionCompatible"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/definitions/jobs/{namespace}/{name}/operations/{request_id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Read an original definition save outcome
         * @description Owner-scoped durable operation history, independently of later definition changes or deletion. Unknown means no automatic write retry. Request references do not grant access.
         */
        get: operations["getjobDefinitionOperation"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/definitions/workflows/{namespace}/{name}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Read live typed workflow definition
         * @description Only configured tenant namespaces and ungrouped credentials. spec_json is a canonical typed JSON string preserving integer precision. PUT dry_run performs Kubernetes admission without persistence; actual writes require its digest, stable request UUID and expected UID/resourceVersion. Accepted CRs do not assert operator projection. Repeating the same accepted request and digest reconciles; changed identity or content conflicts.
         */
        get: operations["getworkflowDefinition"];
        /**
         * Validate or apply typed workflow definition
         * @description Only configured tenant namespaces and ungrouped credentials. spec_json is a canonical typed JSON string preserving integer precision. PUT dry_run performs Kubernetes admission without persistence; actual writes require its digest, stable request UUID and expected UID/resourceVersion. Accepted CRs do not assert operator projection. Repeating the same accepted request and digest reconciles; changed identity or content conflicts.
         */
        put: operations["applyworkflowDefinition"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/definitions/workflows/{namespace}/{name}/compat": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        /**
         * Apply a legacy definition with mandatory server checks
         * @description Explicit checked-save-v1 adapter. Checks the exact legacy payload on the server; execution changes introduced by admission require an upgraded client's review. Stable request identity and normal authorization still apply.
         */
        put: operations["applyworkflowDefinitionCompatible"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/definitions/workflows/{namespace}/{name}/operations/{request_id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Read an original definition save outcome
         * @description Owner-scoped durable operation history, independently of later definition changes or deletion. Unknown means no automatic write retry. Request references do not grant access.
         */
        get: operations["getworkflowDefinitionOperation"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/functions": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * List function definitions
         * @description List the function definitions in the tenant, newest first. Search is a case-insensitive literal substring of name or description, applied before pagination. A function is a tenant-owned definition row; a key scoped to a resource group sees only that group's functions. limit defaults to 50 when omitted.
         */
        get: operations["listFunctions"];
        put?: never;
        /**
         * Create a Function definition
         * @description Create a tenant-owned container Function. Requires independent function:Create access; reads and invokes grant no writes. Tenant is resolved from authenticated authority and tenant_id; resource group is inherited from the key. Unknown body fields, including client-owned tenant/group fields, are rejected. Defaults: active status, 60 second timeout, zero retries, and three retained successes/failures. X-OrbitJob-Idempotency-Key is required; replaying the same normalized definition under the same key and actor returns the original creation snapshot and stable resource identity, even if that definition was edited later. Reusing an identity for different input is a conflict. Creation and audit commit together. Definition version and active revision readiness are independent; ready means the active revision matches the active definition version. Operator projection is asynchronous.
         */
        post: operations["createFunction"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/functions/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Get one function definition
         * @description Get one function definition by id. A function has no schedule: it is invoke-only, and every execution-shaping field is pinned into the revision each invocation runs. version is the saved definition version; active_revision_id and active_revision_version describe the actual active projection independently. ready is true only for an active definition whose version matches that projection.
         */
        get: operations["getFunction"];
        /**
         * Replace a Function definition or pause/resume it
         * @description Replace the complete mutable container definition using required expected_version. Omitted optional settings use creation defaults. Status active or paused supports resume/pause; pausing prevents new calls and does not cancel running invocations. Requires independent function:Update on the persisted tenant and group, including explicit Deny. Tenant and resource group are immutable. A stale version returns conflict; reload and review before retrying. Successful changes increment version and commit with their audit. An active older revision may remain until asynchronous Operator projection; use expected_version on invocation to prevent running it. No deletion or arbitrary invocation payload is supported.
         */
        put: operations["updateFunction"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/functions/{id}/execution-controls": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * get source execution controls
         * @description Schedule pause blocks new scheduled occurrences. Execution pause also freezes new root runs, workflow steps, and Pending or Prepared dispatch. Possibly started attempts retain recovery and cancellation. Deletion is an independent irreversible identity tombstone. Compare-and-set version zero represents previously unset controls. A lost response requires a read before any new write; equal current state is not an independent historical operation receipt.
         */
        get: operations["getfunctionExecutionControls"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        /**
         * update source execution controls
         * @description Schedule pause blocks new scheduled occurrences. Execution pause also freezes new root runs, workflow steps, and Pending or Prepared dispatch. Possibly started attempts retain recovery and cancellation. Deletion is an independent irreversible identity tombstone. Compare-and-set version zero represents previously unset controls. A lost response requires a read before any new write; equal current state is not an independent historical operation receipt.
         */
        patch: operations["updatefunctionExecutionControls"];
        trace?: never;
    };
    "/api/v1/functions/{id}/invoke": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Invoke a function
         * @description Invoke a function by publishing a JobRun custom resource; the operator materializes the ledger run from the pinned revision and starts a new Kubernetes Job and Pod for execution. Scheduling, image availability and container startup affect execution timing; no warm execution pool is used. The request carries no body: v1 functions are parameterless, and parameters are baked into the definition. With wait_seconds omitted or zero, the call returns an asynchronous invocation receipt without waiting for completion. A positive wait_seconds (capped at 60) polls the custom resource until a terminal phase or the wait budget expires, then returns the observed phase. The wait budget does not limit the workload's execution timeout. The receipt reports the observed phase; run observations carry timestamps and the exit outcome. This invoke response contains no log text or function response body; bounded retained diagnostic logs use the separate function:ReadLogs route. Invoking a paused function is a conflict. Optional expected_version must match both the saved definition and the active executable revision; a stale review or pending projection returns 409 without publishing an older version. Omitting it preserves legacy active-revision invocation. Use the X-OrbitJob-Idempotency-Key header to prevent duplicate invocations; version-pinned calls scope that identity to the reviewed version.
         */
        post: operations["invokeFunction"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/functions/{id}/runs": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * List function invocations
         * @description List a function's terminal invocations from the function_runs read model, ordered by triggered_at DESC then id DESC. Supports offset/limit and inclusive created_from/exclusive created_before RFC3339 bounds applied to triggered_at (the invocation time), not projection created_at. The model records outcomes only -- success, failed, canceled -- because live progress lives on the run's JobRun custom resource and the ledger row. occurrence_key is the actual key from a unique same-tenant, same-Function ledger mapping, or null when the retained mapping is missing or ambiguous; clients must never derive it from run_id. attempts and last_attempt are null on list rows; read the invocation detail to load retained attempt facts.
         */
        get: operations["listFunctionRuns"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/functions/{id}/runs/{run_id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Get one function invocation
         * @description Get one terminal invocation by its deterministic run id. attempts lists actual retained execution attempts in ascending attempt_number order through a unique same-tenant, same-Function ledger mapping; last_attempt is the largest retained number. An empty array confirms no attempt exists. A missing or ambiguous mapping returns attempts=null and last_attempt=null; clients must never guess attempt 1. Log access retains its separate function:ReadLogs permission.
         */
        get: operations["getFunctionRun"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/functions/{id}/runs/{run_id}/attempts/{attempt}/logs": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Read bounded retained task logs
         * @description attempt is the positive ledger attempt_number, not the attempt row ID. Function run_id is its history UUID, mapped exactly to a ledger execution under the authorized Function parent. No client Pod or namespace locator. Verifies immutable JobRun, Job and Pod ownership; non-streaming task-container snapshot with five-second deadline. Absence is not a durability promise.
         */
        get: operations["getFunctionAttemptLogs"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/functions/{id}/runs/{run_id}/logs/archive": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * read durable execution logs
         * @description Authenticated execution evidence across attempts. Cursor reads support bounded reconnectable follow; logs have per-execution and installation byte limits. Protected archives survive Pod cleanup for 15 days after completion/seal. Gaps and capacity rejection are explicit. Delete commits an audited cleanup request and does not claim immediate deletion.
         */
        get: operations["readfunctionExecutionArchive"];
        put?: never;
        post?: never;
        /**
         * delete durable execution logs
         * @description Authenticated execution evidence across attempts. Cursor reads support bounded reconnectable follow; logs have per-execution and installation byte limits. Protected archives survive Pod cleanup for 15 days after completion/seal. Gaps and capacity rejection are explicit. Delete commits an audited cleanup request and does not claim immediate deletion.
         */
        delete: operations["deletefunctionExecutionArchive"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/functions/{id}/runs/{run_id}/logs/archive/download": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * download durable execution logs
         * @description Authenticated execution evidence across attempts. Cursor reads support bounded reconnectable follow; logs have per-execution and installation byte limits. Protected archives survive Pod cleanup for 15 days after completion/seal. Gaps and capacity rejection are explicit. Delete commits an audited cleanup request and does not claim immediate deletion.
         */
        get: operations["downloadfunctionExecutionArchive"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/identity": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Read authenticated identity and exact target capabilities
         * @description Authenticate the caller and return credential labels, configured namespace mapping and exact requested policy decisions. Requires no resource grant just to identify the caller. Platform credentials may omit tenant_id for identity only; tenant-specific metadata and ordinary capability targets require an explicit target. Pair repeated action and resource parameters in order, at most 64 pairs. Allowed capability probes do not bypass independent resource-group or runtime checks. Namespace mappings describe configuration, not cluster readiness.
         */
        get: operations["getIdentity"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/instances": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * List runs
         * @description List runs from the control-plane ledger, newest first. Each entry carries the run's phase, its attempt count against max_attempts, and the actor that triggered it. Optionally filter by phase, exact source_uid, exact occurrence_key, inclusive created_from or exclusive created_before (RFC3339, ledger created_at). Both identity filters together identify one accepted trigger; an empty list can mean the operator has not projected its run yet. source_name, source_namespace and source_kind come from the exact pinned revision joined on revision ID, tenant and source UID, including inactive revisions. They are null when that mapping is unavailable or inconsistent; current definitions never substitute for historical metadata.
         */
        get: operations["listInstances"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/instances/summary": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Summarize matching runs
         * @description Count the entire matching ledger, with the same tenant, phase, source_uid, occurrence_key and inclusive created_from/exclusive created_before filters as listInstances. Requires an ungrouped key. Returns total and counts by actual stored phase; no fixed time window or pagination applies.
         */
        get: operations["summarizeInstances"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/instances/{run_id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Get one run
         * @description Get one run from the ledger by run id, with its full attempt trail in the same response. Answers "did it run, how many times, and who triggered it": the body carries phase, attempt, max_attempts and actor.
         */
        get: operations["getInstance"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/instances/{run_id}/attempts": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * List run attempts
         * @description List the per-attempt trail for one run. Each attempt is one Kubernetes Job, so the trail answers "how many times" with the Job that carried each try. The trail is bounded by the definition's retry policy, so it takes no page parameters.
         */
        get: operations["listAttempts"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/instances/{run_id}/attempts/{attempt}/logs": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Read bounded retained task logs
         * @description attempt is the positive ledger attempt_number, not the attempt row ID. Function run_id is its history UUID, mapped exactly to a ledger execution under the authorized Function parent. No client Pod or namespace locator. Verifies immutable JobRun, Job and Pod ownership; non-streaming task-container snapshot with five-second deadline. Absence is not a durability promise.
         */
        get: operations["getInstanceAttemptLogs"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/instances/{run_id}/cancel": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Request a run cancel
         * @description Request a stop for one run by patching spec.cancelRequested on its JobRun custom resource. The request carries no body. The operator can cancel a Prepared attempt before dispatch when the locked dispatch record proves no send was authorized. If execution may have started, Canceled requires trusted termination evidence for the owned attempt and confirmation that the original owned Kubernetes Job no longer exists; retained evidence also requires completed release. Missing or uncertain evidence keeps cancellation unresolved. The response carries the phase observed at request time. Canceling a run that already finished succeeds and reports the terminal phase.
         */
        post: operations["cancelInstance"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/instances/{run_id}/logs/archive": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * read durable execution logs
         * @description Authenticated execution evidence across attempts. Cursor reads support bounded reconnectable follow; logs have per-execution and installation byte limits. Protected archives survive Pod cleanup for 15 days after completion/seal. Gaps and capacity rejection are explicit. Delete commits an audited cleanup request and does not claim immediate deletion.
         */
        get: operations["readinstanceExecutionArchive"];
        put?: never;
        post?: never;
        /**
         * delete durable execution logs
         * @description Authenticated execution evidence across attempts. Cursor reads support bounded reconnectable follow; logs have per-execution and installation byte limits. Protected archives survive Pod cleanup for 15 days after completion/seal. Gaps and capacity rejection are explicit. Delete commits an audited cleanup request and does not claim immediate deletion.
         */
        delete: operations["deleteinstanceExecutionArchive"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/instances/{run_id}/logs/archive/download": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * download durable execution logs
         * @description Authenticated execution evidence across attempts. Cursor reads support bounded reconnectable follow; logs have per-execution and installation byte limits. Protected archives survive Pod cleanup for 15 days after completion/seal. Gaps and capacity rejection are explicit. Delete commits an audited cleanup request and does not claim immediate deletion.
         */
        get: operations["downloadinstanceExecutionArchive"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/jobs": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * List job definitions
         * @description List active ScheduledJob projections in the tenant (source_kind=kubernetes). Function, Workflow and Check projections use their dedicated APIs. Search is a case-insensitive literal substring of name or namespace; namespace and source_uid are exact predicates. Kind isolation and all filters apply before pagination. ScheduledJob authoring uses the separate typed definition apply routes. limit defaults to 50 when omitted.
         */
        get: operations["listJobs"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/jobs/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Get one job definition
         * @description Get the active ScheduledJob revision by revision id. Function, Workflow, Check and inactive revisions return not found. The revision is immutable: a later edit makes a new revision active.
         */
        get: operations["getJob"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/jobs/{id}/execution-controls": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * get source execution controls
         * @description Schedule pause blocks new scheduled occurrences. Execution pause also freezes new root runs, workflow steps, and Pending or Prepared dispatch. Possibly started attempts retain recovery and cancellation. Deletion is an independent irreversible identity tombstone. Compare-and-set version zero represents previously unset controls. A lost response requires a read before any new write; equal current state is not an independent historical operation receipt.
         */
        get: operations["getjobExecutionControls"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        /**
         * update source execution controls
         * @description Schedule pause blocks new scheduled occurrences. Execution pause also freezes new root runs, workflow steps, and Pending or Prepared dispatch. Possibly started attempts retain recovery and cancellation. Deletion is an independent irreversible identity tombstone. Compare-and-set version zero represents previously unset controls. A lost response requires a read before any new write; equal current state is not an independent historical operation receipt.
         */
        patch: operations["updatejobExecutionControls"];
        trace?: never;
    };
    "/api/v1/jobs/{id}/trigger": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Trigger a manual run
         * @description Publish a JobRun custom resource for a manual execution. The operator writes the run row together with its audit trail and records the calling credential as the run's actor, so "who triggered it" is answerable after the fact. Use the X-OrbitJob-Idempotency-Key header to prevent duplicate triggers.
         */
        post: operations["triggerJob"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/policies": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * List policies
         * @description List the policies the caller's tenant may bind: its own, and the platform presets.
         */
        get: operations["listPolicies"];
        put?: never;
        /**
         * Create one policy
         * @description Create an authorization policy in the caller's tenant. Actions the platform does not implement, and the "*" wildcard, are rejected.
         */
        post: operations["createPolicy"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/policies/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Get one policy
         * @description Read one policy belonging to the caller's tenant, or a platform preset.
         */
        get: operations["getPolicy"];
        put?: never;
        post?: never;
        /**
         * Delete one policy
         * @description Delete a policy the caller's tenant owns. Platform presets are never deletable, and a policy still bound to a key is refused rather than cascaded.
         */
        delete: operations["deletePolicy"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/resource_groups": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * List resource groups
         * @description List the resource groups belonging to the caller's tenant.
         */
        get: operations["listResourceGroups"];
        put?: never;
        /**
         * Create one resource group
         * @description Create an isolation group inside the caller's tenant. Keys scoped to a group see only that group's resources.
         */
        post: operations["createResourceGroup"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/slis": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * List SLIs
         * @description List service level indicators within the authenticated tenant and resource group. Optional search is a case-insensitive literal substring of name or description, trimmed and bounded to 128 characters, applied before pagination. Percent and underscore are literal characters, not wildcards.
         */
        get: operations["listSLIs"];
        put?: never;
        /**
         * Create one SLI
         * @description Create a service level indicator definition. For a resource-group-scoped caller, source_config.source_uid must identify an active source visible in the caller's tenant and resource group. good_event_criteria accepts status (success or failed) and duration_ms (a finite nonnegative threshold or an object with op and value). Latency indicators require duration_ms; availability and quality indicators require nonempty criteria.
         */
        post: operations["createSLI"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/slis/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Get one SLI
         * @description Get one service level indicator by id.
         */
        get: operations["getSLI"];
        put?: never;
        post?: never;
        /**
         * Delete one SLI
         * @description Soft-delete a service level indicator by setting deleted_at. The version body is required for optimistic locking. The current SLI store returns 404 for either an absent row or a stale version; reload before deciding whether a new delete is appropriate.
         */
        delete: operations["deleteSLI"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/slo-alerts": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * List SLO alerts
         * @description List SLO alert entries for one tenant, filtered through each parent SLO's resource group. Authorization must cover the whole tenant/group query domain, including explicit Deny and boundaries. If that proof fails or exceeds its budget, the entire request returns 403 without reading items or counts; filters and pagination do not bypass this check.
         */
        get: operations["listSLOAlerts"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/slo-alerts/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Get one SLO alert
         * @description Get one SLO alert by id. Authorization resolves its stored parent SLO's actual resource group, including for ungrouped credentials, so group-specific Deny remains effective. A credential scoped to another group reads this target as not found.
         */
        get: operations["getSLOAlert"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/slos": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * List SLOs
         * @description List service level objectives within the authenticated tenant and resource group. Optional search is a case-insensitive literal substring of name or description, trimmed and bounded to 128 characters, applied before pagination. Percent and underscore are literal characters, not wildcards.
         */
        get: operations["listSLOs"];
        put?: never;
        /**
         * Create one SLO
         * @description Create a service level objective definition.
         */
        post: operations["createSLO"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/slos/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Get one SLO
         * @description Get one service level objective by id. The optional current_budget is included only for ungrouped callers independently authorized for slo:GetBudget on this SLO's actual resource group and ID. A group-specific Deny also removes this optional evidence.
         */
        get: operations["getSLO"];
        put?: never;
        post?: never;
        /**
         * Delete one SLO
         * @description Soft-delete a service level objective by setting deleted_at. The version body is required for optimistic locking.
         */
        delete: operations["deleteSLO"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/slos/{id}/budget": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Get SLO budget
         * @description Get the current error budget for one SLO. Authorization uses that SLO's actual resource group and ID, including for ungrouped credentials. Group-specific Deny remains effective; another credential group reads the parent as not found.
         */
        get: operations["getSLOBudget"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/slos/{id}/budgets": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * List SLO budget history
         * @description List error budget history entries for one SLO. Authorization uses that SLO's actual resource group and ID before reading the history or count. Group-specific Deny remains effective; another credential group reads the parent as not found.
         */
        get: operations["listSLOBudgets"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/slos/{id}/pause": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Pause one SLO
         * @description Pause an active service level objective using optimistic locking by version.
         */
        post: operations["pauseSLO"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/slos/{id}/resume": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Resume one SLO
         * @description Resume a paused service level objective using optimistic locking by version.
         */
        post: operations["resumeSLO"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/tenants": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * List tenants
         * @description List tenants. Limit defaults to 50 when omitted.
         */
        get: operations["listTenants"];
        put?: never;
        /**
         * Create one tenant
         * @description Create a new tenant. Status defaults to active when omitted.
         */
        post: operations["createTenant"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/tenants/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Get one tenant
         * @description Get one tenant by id.
         */
        get: operations["getTenant"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/tenants/{id}/api_keys": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * List API keys
         * @description List API keys for a tenant. Full keys and hashes are never exposed.
         */
        get: operations["listAPIKeys"];
        put?: never;
        /**
         * Create one API key
         * @description Create a new API key for a tenant. The full key is returned only once.
         */
        post: operations["createAPIKey"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/tenants/{id}/policies": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * List policies for an explicitly managed tenant
         * @description List the explicit path tenant's policies and platform presets after policy:List is authorized on that target collection. Ordinary workload queries retain their original tenant scope.
         */
        get: operations["listTenantPolicies"];
        put?: never;
        /**
         * Create a policy for an explicitly managed tenant
         * @description Requires policy:Create on the exact target collection. The requested grants, with self resolved to that target, must be a subset of the caller's effective permissions including explicit denies and boundary. Does not switch ordinary query scope.
         */
        post: operations["createTenantPolicy"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/tenants/{id}/resource_groups": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * List resource groups for an explicitly managed tenant
         * @description List resource groups for the explicit path tenant after group:List is authorized on that target collection. Selecting a management target does not switch ordinary workload query scope.
         */
        get: operations["listTenantResourceGroups"];
        put?: never;
        /**
         * Create a resource group for an explicitly managed tenant
         * @description Requires group:Create on the exact target collection before target-scoped RLS is opened. Does not grant access to any workload or switch ordinary query scope.
         */
        post: operations["createTenantResourceGroup"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/workflows": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * List workflow definitions
         * @description List the projected active revision of each WorkflowJob definition in the tenant. Search is a case-insensitive literal substring of name or namespace, applied before pagination. Create or update a WorkflowJob through the separate typed definition route, PUT /api/v1/definitions/workflows/{namespace}/{name}. An accepted definition becomes visible here after operator projection. limit defaults to 50 when omitted.
         */
        get: operations["listWorkflows"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/workflows/schedule-preview": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Preview upcoming Workflow schedule times
         * @description Validates five-field cron and an IANA timezone (default UTC) using the automatic scheduler parser. Returns strictly future UTC instants; DST gaps are skipped and repeated local times are separate instants. Requires ungrouped workflow Apply access. No workload is created.
         */
        post: operations["previewWorkflowSchedule"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/workflows/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Get one workflow definition
         * @description Get the active revision of one workflow definition by revision id, with its task DAG: each task names the ScheduledJob definition it runs, the tasks that must finish first, and an optional condition over the terminal state of its dependencies.
         */
        get: operations["getWorkflow"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/workflows/{id}/execution-controls": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * get source execution controls
         * @description Schedule pause blocks new scheduled occurrences. Execution pause also freezes new root runs, workflow steps, and Pending or Prepared dispatch. Possibly started attempts retain recovery and cancellation. Deletion is an independent irreversible identity tombstone. Compare-and-set version zero represents previously unset controls. A lost response requires a read before any new write; equal current state is not an independent historical operation receipt.
         */
        get: operations["getworkflowExecutionControls"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        /**
         * update source execution controls
         * @description Schedule pause blocks new scheduled occurrences. Execution pause also freezes new root runs, workflow steps, and Pending or Prepared dispatch. Possibly started attempts retain recovery and cancellation. Deletion is an independent irreversible identity tombstone. Compare-and-set version zero represents previously unset controls. A lost response requires a read before any new write; equal current state is not an independent historical operation receipt.
         */
        patch: operations["updateworkflowExecutionControls"];
        trace?: never;
    };
    "/api/v1/workflows/{id}/runs": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * List workflow runs
         * @description List one workflow's runs from the workflow ledger, ordered by created_at DESC then id DESC. The parent revision may be inactive; tenant and original source UID still define the authorized history. Supports inclusive created_from/exclusive created_before RFC3339 bounds before offset/limit pagination. Each entry carries the workflow-level phase -- the DAG's lifecycle, not any single step's -- its trigger, and the actor that asked for it.
         */
        get: operations["listWorkflowRuns"];
        put?: never;
        /**
         * Trigger a manual workflow run
         * @description Publish a WorkflowRun custom resource for a manual workflow execution. The operator admits the run atomically before creating its ledger row and walking the DAG. Each task is an ordinary run of the ScheduledJob definition it names. The calling credential is recorded as the actor. Pausing a schedule stops automatic starts only; manual starts remain available. Under Forbid concurrency, an accepted resource waits in Pending with a ConcurrencyBlocked condition until admission succeeds. A missing history row does not prove rejection or execution failure. Use the X-OrbitJob-Idempotency-Key header to prevent duplicate triggers.
         */
        post: operations["triggerWorkflow"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/workflows/{id}/runs/{run_id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Get one workflow run
         * @description Read one tenant-scoped workflow run and its ledger step runs. Tasks come from the exact pinned immutable revision, including inactive revisions, when available; task_decisions preserves recorded skips and reasons. Step task names are mapped by deterministic occurrence keys, never guessed from source UID. pinned_revision_available=false and tasks=null explicitly signal unavailable pinned history. A historical parent route may name an inactive Workflow revision when its original source UID matches the run; mutating routes retain active-version requirements. deadline and missing_job_reference task decisions are execution failures, while legitimate condition skips retain success semantics. Existing run and definition identifiers retain their original meanings.
         */
        get: operations["getWorkflowRun"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/workflows/{id}/runs/{run_id}/cancel": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Request a workflow run cancel
         * @description Request a stop for one workflow run by patching spec.cancelRequested on its WorkflowRun custom resource. The request carries no body. The operator fans the stop out over the workflow's non-terminal step runs; the workflow reaches Canceled only once its steps are observed terminal, and the response carries the phase observed at request time. Canceling a run that already finished succeeds and reports the terminal phase.
         */
        post: operations["cancelWorkflowRun"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/workspaces": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Discover authorized workspace display metadata
         * @description Authenticated metadata discovery, independent of business tenant selectors and the legacy tenant list. Own tenant labels are implicit identity information. Foreign items require effective tenant:Get on orbitjob:<target>:*:tenant/<target>, including Deny, boundary and caller-resolved self; group-bound credentials see only their own workspace. Platform credentials have no implicit own workspace and need no selected target. The tenant_id parameter never changes discovery scope. Search is a case-insensitive literal name/slug substring, trimmed and limited to 128 Unicode characters; percent, underscore and backslash are literal. Authorization and search precede stable lower(name),id pagination. limit defaults to 20, maximum 100. has_more describes only visible matches; no global total is disclosed. Discovery grants no workload access or management operation.
         */
        get: operations["listWorkspaces"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/healthz": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Health check
         * @description Liveness probe endpoint. Returns HTTP 200 when service is up.
         */
        get: operations["getHealthz"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/metrics": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Prometheus metrics
         * @description Prometheus scrape endpoint in text exposition format.
         */
        get: operations["getMetrics"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/openapi.json": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * OpenAPI document
         * @description Machine-readable OpenAPI 3 document generated from code-first route metadata.
         */
        get: operations["getOpenAPIDocument"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
}
export type webhooks = Record<string, never>;
export interface components {
    schemas: {
        APIError: {
            /** @enum {string} */
            code: "MALFORMED_REQUEST" | "VALIDATION_ERROR" | "UNAUTHORIZED" | "FORBIDDEN" | "NOT_FOUND" | "CONFLICT" | "RATE_LIMITED" | "INTERNAL_ERROR" | "SERVICE_UNAVAILABLE";
            field?: string;
            message: string;
        };
        APIKeyCreateResult: {
            created_at: string;
            id: string;
            key: string;
            key_prefix: string;
            name: string;
        };
        APIKeyListItem: {
            /** Format: date-time */
            created_at: string;
            id: string;
            key_prefix: string;
            name: string;
            /** Format: date-time */
            revoked_at?: string | null;
        };
        APIKeyRenameResult: {
            id: string;
            name: string;
        };
        APIKeyRevokeResult: {
            revoked: boolean;
        };
        /** @description A new account requires username and password. An existing account requires its own active session and CSRF token; username and password may be omitted. Supplied nonempty passwords are validated, but never replace an existing account's password. */
        AcceptInvitationRequest: {
            /** @description Display name encoded as valid UTF-8, with at most 128 Unicode code points. */
            display_name?: string;
            /**
             * Format: password
             * @description New password: valid UTF-8 with at least 15 Unicode code points and at most 1024 UTF-8 bytes. Common passwords and repeated patterns are rejected; no character-class requirement applies. Required for a new account. An existing account may omit this field or send an empty string; a supplied nonempty value is validated and does not change its password.
             */
            password?: string;
            /** @description Single-use capability token containing 43 ASCII characters, as issued by the installer or account API. It must be unexpired and unconsumed. */
            token: string;
            /** @description Account username, normalized by trimming surrounding whitespace and converting to lowercase. The normalized value must match [a-z0-9][a-z0-9._-]{2,63} (3 to 64 ASCII characters). Required for a new account; optional for an existing account and must match its authenticated session when supplied. */
            username?: string;
        };
        AlertListResponse: {
            items: components["schemas"]["SloalertListItem"][];
        };
        ApiKeyListResponse: {
            items: components["schemas"]["APIKeyListItem"][];
        };
        ApplyRequest: {
            /**
             * @description Run Kubernetes admission without persistence; review the canonical spec_json and manifest_digest before saving.
             * @default false
             */
            dry_run?: boolean;
            /** @description Observed resourceVersion paired with expected_uid; stale or replaced resources conflict. */
            expected_resource_version?: string;
            /** @description For an update, send the observed immutable UID together with expected_resource_version. Leave both empty for creation. */
            expected_uid?: string;
            /** @description Exact digest returned by the reviewed dry-run. Required for actual writes; missing or changed digest is rejected. */
            manifest_digest?: string;
            /**
             * Format: uuid
             * @description Stable UUID for this exact write intent; retain it through unknown outcomes and reconciliation.
             */
            request_id: string;
            /** @description JSON-encoded typed ScheduledJobSpec or WorkflowJobSpec. This is a string, not a nested object or full Kubernetes manifest. Unknown spec fields are rejected and integers retain precision. The typed apply request has an additional 256 KiB decoder limit. */
            spec_json: string;
        };
        AssertionRule: {
            metric: string;
            /** @enum {string} */
            operator: ">" | "<" | "==" | "!=" | ">=" | "<=";
            /** @enum {string} */
            severity: "warning" | "critical";
            threshold?: number;
        };
        Attempt: {
            attempt_number: number;
            /** Format: date-time */
            completed_at?: string | null;
            phase: string;
            /** Format: date-time */
            started_at?: string | null;
        };
        AttemptItem: {
            attempt_number: number;
            /** Format: date-time */
            completed_at?: string | null;
            /** Format: date-time */
            created_at: string;
            execution_unknown_reason?: string | null;
            /** Format: date-time */
            execution_unknown_since?: string | null;
            /** Format: int64 */
            id: number | string;
            kubernetes_job_name: string;
            kubernetes_job_uid?: string | null;
            observed_resource_version?: string | null;
            owner_run_uid?: string | null;
            phase: string;
            /** Format: int64 */
            run_id: number | string;
            /** Format: date-time */
            started_at?: string | null;
        };
        AttemptListResponse: {
            items: components["schemas"]["AttemptItem"][];
        };
        BudgetItem: {
            budget_consumed: number;
            budget_remaining: number;
            budget_total: number;
            burn_rate: number;
            status: string;
            window_end: string;
            window_start: string;
        };
        BudgetListResponse: {
            items: components["schemas"]["SlobudgetListItem"][];
        };
        CancelResult: {
            name: string;
            namespace: string;
            occurrence_key: string;
            phase: string;
        };
        Capability: {
            /** Format: date-time */
            expires_at: string;
            id: string;
            token: string;
        };
        ChangeCheckStatusRequest: {
            /** @description Current observed resource version. Stale Check/SLO writes return 409; the current SLI delete store returns 404 for either a stale version or an absent row. Reload and review before submitting a new intent. */
            version: number;
        };
        ChangeSLOStatusRequest: {
            /** @description Current observed resource version. Stale Check/SLO writes return 409; the current SLI delete store returns 404 for either a stale version or an absent row. Reload and review before submitting a new intent. */
            version: number;
        };
        ChangeStatusResult: {
            /** Format: int64 */
            id: number | string;
            status: string;
            version: number;
        };
        CheckAssertionRule: {
            metric: string;
            operator: string;
            severity: string;
            threshold: number;
        };
        CheckListItem: {
            check_type: string;
            /** Format: date-time */
            created_at: string;
            description?: string | null;
            execution_policy_version: number;
            /** Format: int64 */
            id: number | string;
            name: string;
            /** Format: date-time */
            next_run_at?: string | null;
            schedule_type: string;
            status: string;
            tenant_id: string;
            version: number;
        };
        CheckListResponse: {
            items: components["schemas"]["CheckListItem"][];
        };
        CheckRunListResponse: {
            items: components["schemas"]["CheckrunListItem"][];
        };
        CheckrunGetResult: {
            /** Format: int64 */
            check_id: number | string;
            /** Format: date-time */
            created_at: string;
            duration_ms?: number | null;
            evaluation_result: {
                [key: string]: unknown;
            };
            /** Format: date-time */
            finished_at?: string | null;
            /** Format: int64 */
            id: number | string;
            output: {
                [key: string]: unknown;
            };
            run_id: string;
            /** Format: date-time */
            scheduled_at: string;
            severity?: string | null;
            /** Format: date-time */
            started_at?: string | null;
            status: string;
            tenant_id: string;
            version: number;
        };
        CheckrunListItem: {
            /** Format: int64 */
            check_id: number | string;
            /** Format: date-time */
            created_at: string;
            duration_ms?: number | null;
            /** Format: int64 */
            id: number | string;
            run_id: string;
            severity?: string | null;
            status: string;
        };
        CleanupReceipt: {
            state: string;
        };
        CreateAPIKeyRequest: {
            /** @description Optional policy ID capping all bound grants. The caller may not grant beyond its own effective permissions. */
            boundary_policy_id?: string;
            name?: string;
            /** @description Policy IDs to bind at creation. Omitted or empty grants no guarded operation; authentication alone provides no management access. */
            policies?: string[];
            /** @description Optional resource group ID in the target tenant. Group scope independently limits row visibility. */
            resource_group_id?: string;
        };
        CreateCheckRequest: {
            /** @description Stored rule metadata. Current Kubernetes probes decide outcomes from their exit status; these rules do not add a response-body assertion engine. */
            assertion_rules?: components["schemas"]["AssertionRule"][];
            /** @description HTTP probe configuration. Only url, method and expected_status affect execution; extra stored keys are inert metadata. The probe has no request body, header injection or arbitrary response parser. */
            check_config: {
                /** @default 200 */
                expected_status?: number | null;
                /**
                 * @description GET, HEAD or OPTIONS; normalized by trimming and uppercasing. An empty or null value uses GET.
                 * @default GET
                 */
                method?: string | null;
                /**
                 * Format: uri
                 * @description Required absolute http or https URL with a host.
                 */
                url: string;
            } & {
                [key: string]: unknown;
            };
            /** @enum {string} */
            check_type: "http_health";
            /** @description Required for cron schedules; robfig ParseStandard syntax, at most 128 characters. */
            cron_expr?: string | null;
            description?: string | null;
            /** @description Required for interval schedules; every occurrence is a Kubernetes Job, with a minimum interval of 30 seconds. */
            interval_sec?: number | null;
            labels?: {
                [key: string]: unknown;
            };
            name: string;
            /**
             * @description Omitted or zero uses the domain default.
             * @default 5
             */
            priority?: number;
            /**
             * @description Omitted or zero uses the domain default.
             * @default 0
             */
            retry_limit?: number;
            /**
             * @description cron requires cron_expr; interval requires interval_sec. Inactive schedule fields are ignored.
             * @default cron
             * @enum {string}
             */
            schedule_type?: "cron" | "interval";
            tenant_id?: string;
            /**
             * @description Omitted or zero uses the domain default.
             * @default 30
             */
            timeout_sec?: number;
            /**
             * @description IANA time zone used when evaluating the schedule.
             * @default UTC
             */
            timezone?: string;
        };
        CreatePolicyRequest: {
            description?: string;
            /** @description Exactly one supported lowercase policy document. Duplicate members, case variants, conditions and unsupported fields are rejected rather than ignored. No credential-policy rebinding endpoint is provided. */
            document: {
                statement: {
                    /** @description Explicit known actions such as job:List or job:Get. Wildcards belong only to platform presets; tenant-authored policies cannot widen the caller's effective grants. */
                    action: string[];
                    /** @enum {string} */
                    effect: "Allow" | "Deny";
                    /** @description orbitjob:<tenant>:<group>:<type>/<id> ARN patterns. self resolves to the owning tenant. All statements are evaluated with explicit Deny and credential boundaries. */
                    resource: string[];
                }[];
                /** @description Optional document version label; this string does not enable extra policy semantics. */
                version?: string;
            };
            name: string;
        };
        CreateResourceGroupRequest: {
            name: string;
            slug: string;
        };
        CreateResult: {
            assertion_rules: components["schemas"]["CheckAssertionRule"][];
            check_config: {
                [key: string]: unknown;
            };
            check_type: string;
            /** Format: date-time */
            created_at: string;
            cron_expr?: string | null;
            description?: string | null;
            execution_policy_version: number;
            /** Format: int64 */
            id: number | string;
            interval_sec?: number | null;
            labels: {
                [key: string]: unknown;
            };
            name: string;
            /** Format: date-time */
            next_run_at?: string | null;
            priority: number;
            retry_limit: number;
            schedule_type: string;
            status: string;
            tenant_id: string;
            timeout_sec: number;
            timezone: string;
            /** Format: date-time */
            updated_at: string;
            version: number;
        };
        CreateSLIRequest: {
            /**
             * @description Aggregation mode for recorded observations.
             * @default ratio
             * @enum {string}
             */
            aggregation?: "ratio" | "count";
            description?: string | null;
            /** @description Validated at creation. Availability and quality require a nonempty object; latency requires duration_ms; custom may omit criteria. Only status and duration_ms are supported. Unsupported keys such as severity or probe output, invalid statuses and invalid numeric conditions are rejected with HTTP 400. Every supplied criterion must match the recorded outcome; a duration criterion requires actual retained timing. */
            good_event_criteria?: {
                /** @description Finite, nonnegative number for equality, or an object containing exactly op and value. The operator is one of <, <=, >, >=, ==, != and value is a finite, nonnegative number; for example {"op":"<=","value":1000}. Requires actual retained timing. */
                duration_ms?: unknown;
                /** @description Expected outcome status: exactly success or failed. */
                status?: string;
            } & {
                [key: string]: unknown;
            };
            name: string;
            /** @enum {string} */
            sli_type: "availability" | "latency" | "quality" | "custom";
            /** @description Select the exact source definition identity recorded in the run ledger; extra stored keys do not narrow the reader. */
            source_config: {
                /** @description ScheduledJob CR UID, Function source UID, or check-<check ID>. Use the actual selected source, not its display name or revision ID. */
                source_uid: string;
            } & {
                [key: string]: unknown;
            };
            /**
             * @description SLI events derive from recorded run outcomes.
             * @default job_run
             * @enum {string}
             */
            source_type?: "job_run";
        };
        CreateSLORequest: {
            /**
             * @description Omitted or zero uses the default. Must exceed the effective slow burn rate.
             * @default 14.4
             */
            alert_fast_burn_rate?: number;
            /**
             * @description Omitted or zero uses the default. Must be less than the effective fast burn rate.
             * @default 2
             */
            alert_slow_burn_rate?: number;
            description?: string | null;
            name: string;
            /** Format: int64 */
            sli_id: number | string;
            target: number;
            /** @description Go duration that resolves to whole seconds, from 1s through 8760h; for example 24h or 720h. Values below 1s or with a fractional-second remainder are rejected with HTTP 400. The d suffix is not supported. */
            window_duration: string;
            /**
             * @description Budget evaluation window type.
             * @default rolling
             * @enum {string}
             */
            window_type?: "rolling" | "calendar" | "quarterly";
        };
        CreateTenantRequest: {
            name: string;
            slug: string;
            /** @enum {string} */
            status?: "active" | "suspended";
        };
        DeletedResponse: {
            deleted: boolean;
        };
        Document: {
            statement?: components["schemas"]["Statement"][];
            version?: string;
        };
        ErrorResponse: {
            error: components["schemas"]["APIError"];
        };
        ExecutionpolicyResources: {
            limits?: {
                [key: string]: string;
            };
            requests?: {
                [key: string]: string;
            };
        };
        FunctionDefinitionRequest: {
            /** @description Container argument array; values are passed literally. */
            args?: string[];
            /** @description Container command array; preserve each literal argument. No implicit shell expansion is added. */
            command?: string[];
            description?: string;
            execution_policy_version?: number;
            /**
             * @description Retained terminal failures; zero explicitly retains none.
             * @default 3
             */
            history_failed?: number | null;
            /**
             * @description Retained terminal successes; zero explicitly retains none.
             * @default 3
             */
            history_success?: number | null;
            image: string;
            labels?: {
                [key: string]: unknown;
            };
            name: string;
            resources?: components["schemas"]["ExecutionpolicyResources"];
            /**
             * @description Retries after the initial attempt.
             * @default 0
             */
            retry_limit?: number;
            retry_safe?: boolean;
            /**
             * @description active permits invocations of the ready version; paused stops new invocations without canceling existing runs.
             * @default active
             * @enum {string}
             */
            status?: "active" | "paused";
            /**
             * @description Omitted or null uses the default.
             * @default 60
             */
            timeout_seconds?: number | null;
        };
        FunctionInvokeResult: {
            created: boolean;
            name: string;
            namespace: string;
            occurrence_key: string;
            phase: string;
            trigger: string;
        };
        FunctionItem: {
            /** Format: int64 */
            active_revision_id?: number | string | null;
            active_revision_version?: number | null;
            args: string[];
            command: string[];
            /** Format: date-time */
            created_at: string;
            description: string;
            execution_policy_version?: number;
            history_failed: number;
            history_success: number;
            /** Format: int64 */
            id: number | string;
            image: string;
            labels: {
                [key: string]: unknown;
            };
            name: string;
            ready: boolean;
            resource_group_id: string;
            resources?: components["schemas"]["Resources"];
            retry_limit: number;
            retry_safe?: boolean;
            status: string;
            tenant_id: string;
            timeout_seconds: number;
            /** Format: date-time */
            updated_at: string;
            version: number;
        };
        FunctionListResponse: {
            items: components["schemas"]["FunctionItem"][];
        };
        FunctionRunItem: {
            attempts: components["schemas"]["Attempt"][] | null;
            /** Format: date-time */
            created_at: string;
            duration_ms?: number | null;
            /** Format: date-time */
            finished_at?: string | null;
            /** Format: int64 */
            function_id: number | string;
            /** Format: int64 */
            id: number | string;
            last_attempt?: number | null;
            occurrence_key?: string | null;
            run_id: string;
            /** Format: date-time */
            started_at?: string | null;
            status: string;
            /** Format: date-time */
            triggered_at: string;
            version: number;
        };
        FunctionRunListResponse: {
            items: components["schemas"]["FunctionRunItem"][];
        };
        Gap: {
            /** Format: int64 */
            from: number | string;
            reason: string;
            /** Format: int64 */
            through: number | string;
        };
        GetItem: {
            actor: string;
            concurrency_policy: string;
            /** Format: date-time */
            created_at: string;
            /** Format: int64 */
            generation: number | string;
            /** Format: int64 */
            id: number | string;
            job_template: components["schemas"]["JobTemplate"];
            misfire_policy: string;
            name: string;
            namespace: string;
            retry_max_attempts: number;
            schedule: string;
            schedule_summary: string;
            source_mode: string;
            source_uid: string;
            spec_hash: string;
            suspend: boolean;
            tenant_id: string;
            timeout_seconds: number;
        };
        GetResult: {
            assertion_rules: components["schemas"]["CheckAssertionRule"][];
            check_config: {
                [key: string]: unknown;
            };
            check_type: string;
            /** Format: date-time */
            created_at: string;
            cron_expr?: string | null;
            description?: string | null;
            execution_policy_version: number;
            /** Format: int64 */
            id: number | string;
            interval_sec?: number | null;
            labels: {
                [key: string]: unknown;
            };
            name: string;
            /** Format: date-time */
            next_run_at?: string | null;
            priority: number;
            retry_limit: number;
            schedule_type: string;
            status: string;
            tenant_id: string;
            timeout_sec: number;
            timezone: string;
            /** Format: date-time */
            updated_at: string;
            version: number;
        };
        Grant: {
            /** @description Optional boundary policy ID. An empty value inherits the inviting administrator's membership boundary; delegated grants must preserve that boundary. */
            boundary_policy_id?: string;
            /** @description Optional explicit policy IDs. Omitted or empty uses the selected role's preset; the resulting grant must be contained within the caller's effective permissions. */
            policy_ids?: string[];
            /** @enum {string} */
            role: "admin" | "operator" | "viewer";
            tenant_id: string;
            /** @description Account username, normalized by trimming surrounding whitespace and converting to lowercase. The normalized value must match [a-z0-9][a-z0-9._-]{2,63} (3 to 64 ASCII characters). Omit to allow any eligible account to accept the invitation. */
            username?: string;
        };
        HealthzResponse: {
            status: string;
        };
        IdentityCapability: {
            action: string;
            allowed: boolean;
            resource: string;
        };
        IdentityResponse: {
            boundary_policy_id: string;
            capabilities: components["schemas"]["IdentityCapability"][];
            display_name?: string;
            group_id: string;
            installation_admin: boolean;
            key_id: string;
            key_name?: string;
            kind: string;
            membership_role?: string;
            subject_type: string;
            target_tenant_id?: string;
            tenant_id: string;
            tenant_name?: string;
            tenant_slug?: string;
            user_id?: string;
            username?: string;
            workload_namespaces: string[];
        };
        JobListResponse: {
            items: components["schemas"]["ListItem"][];
        };
        JobTemplate: {
            args?: string[];
            backoff_limit: number;
            command?: string[];
            image: string;
        };
        ListItem: {
            actor: string;
            concurrency_policy: string;
            /** Format: date-time */
            created_at: string;
            /** Format: int64 */
            generation: number | string;
            /** Format: int64 */
            id: number | string;
            misfire_policy: string;
            name: string;
            namespace: string;
            schedule: string;
            schedule_summary: string;
            source_kind?: string;
            source_uid: string;
            suspend: boolean;
            tenant_id: string;
        };
        LoginRequest: {
            /**
             * Format: password
             * @description Current account password. At most 1024 UTF-8 bytes; login and password confirmation do not apply the new-password length or quality rules.
             */
            password: string;
            /**
             * @description Use a seven-day session instead of the default eight-hour session.
             * @default false
             */
            remember?: boolean;
            /** @description Account username, normalized by trimming surrounding whitespace and converting to lowercase. The normalized value must match [a-z0-9][a-z0-9._-]{2,63} (3 to 64 ASCII characters). */
            username: string;
        };
        LogsResult: {
            attempt: number;
            container: string;
            /** Format: int64 */
            limit_bytes: number | string;
            reason: string;
            state: string;
            /** Format: int64 */
            tail_lines: number | string;
            text: string;
            truncated: boolean;
        };
        MemberChangeRequest: {
            boundary_policy_id?: string | null;
            /** @description Explicit IDs replace the current policy bindings. An empty array resets bindings to the selected role's preset. Omit or use null to retain current bindings unless role changes, which selects the new role's preset. The resulting grant must fit the administrator's effective permissions and boundary. */
            policy_ids?: string[];
            /** @description Set role to admin, operator or viewer. Omit or use null to keep the current role. */
            role?: string | null;
            /** @description Set status to active or disabled. Omit or use null to keep the current status. */
            status?: string | null;
            tenant_id: string;
        };
        MemberListResponse: {
            items: components["schemas"]["Membership"][];
        };
        Membership: {
            boundary_policy_id?: string;
            policy_ids: string[];
            role: string;
            status: string;
            tenant_id: string;
            user: components["schemas"]["User"];
        };
        Operation: {
            actor_id: string;
            admitted_uid?: string;
            /** Format: date-time */
            checked_until: string;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            expires_at: string;
            kind: string;
            name: string;
            namespace: string;
            request_id: string;
            result?: components["schemas"]["Result"];
            state: string;
            /** Format: date-time */
            updated_at: string;
        };
        Page: {
            gaps: components["schemas"]["Gap"][];
            has_more: boolean;
            /** Format: int64 */
            limit_bytes: number | string;
            /** Format: int64 */
            next: number | string;
            outcome: string;
            /** Format: int64 */
            output_bytes: number | string;
            /** Format: date-time */
            protected_until?: string | null;
            reason: string;
            records: components["schemas"]["Record"][];
            /** Format: int64 */
            retained_bytes: number | string;
            state: string;
        };
        PasswordChangeRequest: {
            /** @description Current account password. At most 1024 UTF-8 bytes; login and password confirmation do not apply the new-password length or quality rules. */
            current_password: string;
            /**
             * Format: password
             * @description New password: valid UTF-8 with at least 15 Unicode code points and at most 1024 UTF-8 bytes. Common passwords and repeated patterns are rejected; no character-class requirement applies.
             */
            password: string;
        };
        PasswordRequest: {
            /**
             * Format: password
             * @description Current account password. At most 1024 UTF-8 bytes; login and password confirmation do not apply the new-password length or quality rules.
             */
            password: string;
        };
        PasswordResetRequest: {
            /** @description Optional compatibility field. Password reset issuance requires installation administrator authority regardless of this value. */
            tenant_id?: string;
        };
        PolicyCreateResult: {
            description: string;
            id: string;
            name: string;
        };
        PolicyDocument: {
            statement: components["schemas"]["PolicyStatement"][];
            version: string;
        };
        PolicyGetResult: {
            description: string;
            document: components["schemas"]["PolicyDocument"];
            id: string;
            name: string;
            platform: boolean;
        };
        PolicyListItem: {
            description: string;
            document: components["schemas"]["PolicyDocument"];
            id: string;
            name: string;
            platform: boolean;
        };
        PolicyListResponse: {
            items: components["schemas"]["PolicyListItem"][];
        };
        PolicyStatement: {
            action: string[];
            effect: string;
            resource: string[];
        };
        Record: {
            attempt: number;
            container: string;
            exit_code?: number | null;
            id: string;
            kind: string;
            /** Format: date-time */
            observed_at: string;
            pod_uid: string;
            restart: number;
            /** Format: int64 */
            sequence: number | string;
            signal?: number | null;
            stage: string;
            text: string;
            /** Format: date-time */
            time: string;
            time_source: string;
        };
        RenameAPIKeyRequest: {
            name: string;
        };
        ResetPasswordRequest: {
            /**
             * Format: password
             * @description New password: valid UTF-8 with at least 15 Unicode code points and at most 1024 UTF-8 bytes. Common passwords and repeated patterns are rejected; no character-class requirement applies.
             */
            password: string;
            /** @description Single-use capability token containing 43 ASCII characters, as issued by the installer or account API. It must be unexpired and unconsumed. */
            token: string;
        };
        ResourceGroupListResponse: {
            items: components["schemas"]["ResourcegroupListItem"][];
        };
        ResourcegroupCreateResult: {
            id: string;
            name: string;
            slug: string;
        };
        ResourcegroupListItem: {
            id: string;
            name: string;
            slug: string;
        };
        Resources: {
            limits?: {
                [key: string]: string;
            };
            requests?: {
                [key: string]: string;
            };
        };
        Result: {
            /** Format: int64 */
            generation: number | string;
            kind: string;
            last_manifest_digest: string;
            last_request_id: string;
            manifest_digest: string;
            name: string;
            namespace: string;
            request_id?: string;
            resource_version: string;
            spec_json: string;
            state?: string;
            uid: string;
        };
        RunGetItem: {
            actor: string;
            attempt: number;
            attempts: components["schemas"]["AttemptItem"][];
            /** Format: date-time */
            created_at: string;
            /** Format: int64 */
            id: number | string;
            max_attempts: number;
            occurrence_key: string;
            phase: string;
            /** Format: int64 */
            revision_id: number | string;
            source_uid: string;
            tenant_id: string;
            trigger: string;
            /** Format: date-time */
            updated_at: string;
        };
        RunListItem: {
            actor: string;
            attempt: number;
            /** Format: date-time */
            created_at: string;
            /** Format: int64 */
            id: number | string;
            max_attempts: number;
            occurrence_key: string;
            phase: string;
            /** Format: int64 */
            revision_id: number | string;
            source_kind?: string | null;
            source_name?: string | null;
            source_namespace?: string | null;
            source_uid: string;
            tenant_id: string;
            trigger: string;
            /** Format: date-time */
            updated_at: string;
        };
        RunListResponse: {
            items: components["schemas"]["RunListItem"][];
        };
        SessionListItem: {
            /** Format: date-time */
            created_at: string;
            current: boolean;
            /** Format: date-time */
            expires_at: string;
            id: string;
            remember: boolean;
        };
        SessionListResponse: {
            items: components["schemas"]["SessionListItem"][];
        };
        SessionSnapshot: {
            authenticated: boolean;
            csrf_token?: string;
            /** Format: date-time */
            expires_at?: string | null;
            initialized: boolean;
            /** Format: date-time */
            recent_auth_until?: string | null;
            user?: components["schemas"]["User"];
            workspaces: components["schemas"]["Workspace"][];
        };
        SetupAccountRequest: {
            /** @description Display name encoded as valid UTF-8, with at most 128 Unicode code points. */
            display_name?: string;
            /**
             * Format: password
             * @description New password: valid UTF-8 with at least 15 Unicode code points and at most 1024 UTF-8 bytes. Common passwords and repeated patterns are rejected; no character-class requirement applies.
             */
            password: string;
            /** @description Single-use capability token containing 43 ASCII characters, as issued by the installer or account API. It must be unexpired and unconsumed. */
            token: string;
            /** @description Account username, normalized by trimming surrounding whitespace and converting to lowercase. The normalized value must match [a-z0-9][a-z0-9._-]{2,63} (3 to 64 ASCII characters). */
            username: string;
        };
        SliCreateResult: {
            aggregation: string;
            created_at: string;
            description?: string | null;
            good_event_criteria: {
                [key: string]: unknown;
            };
            /** Format: int64 */
            id: number | string;
            name: string;
            sli_type: string;
            source_config: {
                [key: string]: unknown;
            };
            source_type: string;
            version: number;
        };
        SliGetItem: {
            aggregation: string;
            created_at: string;
            description?: string | null;
            good_event_criteria: {
                [key: string]: unknown;
            };
            /** Format: int64 */
            id: number | string;
            name: string;
            sli_type: string;
            source_config: {
                [key: string]: unknown;
            };
            source_type: string;
            updated_at: string;
            version: number;
        };
        SliListItem: {
            aggregation: string;
            created_at: string;
            /** Format: int64 */
            id: number | string;
            name: string;
            sli_type: string;
            source_type: string;
            version: number;
        };
        SliListResponse: {
            items: components["schemas"]["SliListItem"][];
        };
        SloChangeStatusResult: {
            /** Format: int64 */
            id: number | string;
            status: string;
        };
        SloCreateResult: {
            alert_fast_burn_rate: number;
            alert_slow_burn_rate: number;
            created_at: string;
            description?: string | null;
            /** Format: int64 */
            id: number | string;
            name: string;
            /** Format: int64 */
            sli_id: number | string;
            status: string;
            target: number;
            version: number;
            window_duration: string;
            window_type: string;
        };
        SloGetItem: {
            alert_fast_burn_rate: number;
            alert_slow_burn_rate: number;
            created_at: string;
            current_budget?: components["schemas"]["BudgetItem"];
            description?: string | null;
            /** Format: int64 */
            id: number | string;
            name: string;
            /** Format: int64 */
            sli_id: number | string;
            status: string;
            target: number;
            updated_at: string;
            version: number;
            window_duration: string;
            window_type: string;
        };
        SloListItem: {
            alert_fast_burn_rate: number;
            alert_slow_burn_rate: number;
            created_at: string;
            /** Format: int64 */
            id: number | string;
            name: string;
            /** Format: int64 */
            sli_id: number | string;
            status: string;
            target: number;
            version: number;
            window_duration: string;
            window_type: string;
        };
        SloListResponse: {
            items: components["schemas"]["SloListItem"][];
        };
        SloalertGetItem: {
            alert_type: string;
            /** Format: int64 */
            budget_id: number | string;
            burn_rate: number;
            /** Format: int64 */
            id: number | string;
            resolved_at?: string | null;
            /** Format: int64 */
            slo_id: number | string;
            status: string;
            triggered_at: string;
        };
        SloalertListItem: {
            alert_type: string;
            burn_rate: number;
            /** Format: int64 */
            id: number | string;
            /** Format: int64 */
            slo_id: number | string;
            status: string;
            triggered_at: string;
        };
        SlobudgetGetItem: {
            budget_consumed: number;
            budget_remaining: number;
            budget_total: number;
            burn_rate: number;
            status: string;
            version: number;
            window_end: string;
            window_start: string;
        };
        SlobudgetListItem: {
            budget_consumed: number;
            budget_remaining: number;
            budget_total: number;
            burn_rate: number;
            status: string;
            window_end: string;
            window_start: string;
        };
        SourceControlResponse: {
            actor?: string;
            can_manage: boolean;
            execution_paused: boolean;
            schedule_paused: boolean;
            source_mode: string;
            source_uid: string;
            tombstoned: boolean;
            /** Format: date-time */
            updated_at?: string | null;
            /** Format: int64 */
            version: number | string;
        };
        SourceControlUpdateRequest: {
            execution_paused: boolean;
            schedule_paused: boolean;
            /** Format: int64 */
            version: number | string;
        };
        Statement: {
            action?: string[];
            effect?: string;
            resource?: string[];
        };
        Summary: {
            phases: {
                [key: string]: number | string;
            };
            /** Format: int64 */
            total: number | string;
        };
        TaskDecision: {
            reason?: string;
            skipped: boolean;
        };
        TaskSnapshot: {
            image: string;
            name: string;
            namespace: string;
            /** Format: int64 */
            revision_id: number | string;
            source_uid: string;
        };
        TenantCreateResult: {
            id: string;
            name: string;
            slug: string;
            status: string;
        };
        TenantGetResult: {
            id: string;
            name: string;
            slug: string;
            status: string;
        };
        TenantListItem: {
            id: string;
            name: string;
            slug: string;
            status: string;
        };
        TenantListResponse: {
            items: components["schemas"]["TenantListItem"][];
        };
        TriggerResult: {
            created: boolean;
            name: string;
            namespace: string;
            occurrence_key: string;
            phase: string;
            trigger: string;
        };
        UpdateFunctionRequest: {
            /** @description Container argument array; values are passed literally. */
            args?: string[];
            /** @description Container command array; preserve each literal argument. No implicit shell expansion is added. */
            command?: string[];
            description?: string;
            execution_policy_version?: number;
            expected_version: number;
            /**
             * @description Retained terminal failures; zero explicitly retains none.
             * @default 3
             */
            history_failed?: number | null;
            /**
             * @description Retained terminal successes; zero explicitly retains none.
             * @default 3
             */
            history_success?: number | null;
            image: string;
            labels?: {
                [key: string]: unknown;
            };
            name: string;
            resources?: components["schemas"]["ExecutionpolicyResources"];
            /**
             * @description Retries after the initial attempt.
             * @default 0
             */
            retry_limit?: number;
            retry_safe?: boolean;
            /**
             * @description active permits invocations of the ready version; paused stops new invocations without canceling existing runs.
             * @default active
             * @enum {string}
             */
            status?: "active" | "paused";
            /**
             * @description Omitted or null uses the default.
             * @default 60
             */
            timeout_seconds?: number | null;
        };
        User: {
            /** Format: date-time */
            created_at: string;
            display_name: string;
            id: string;
            installation_admin: boolean;
            status: string;
            username: string;
        };
        UserChange: {
            /** @description Display name encoded as valid UTF-8, with at most 128 Unicode code points. */
            display_name?: string | null;
            installation_admin?: boolean | null;
            /** @description Set status to active or disabled. Omit or use null to keep the current status. */
            status?: string | null;
        };
        UserListResponse: {
            items: components["schemas"]["User"][];
        };
        VersionRequest: {
            /** @description Current observed resource version. Stale Check/SLO writes return 409; the current SLI delete store returns 404 for either a stale version or an absent row. Reload and review before submitting a new intent. */
            version: number;
        };
        WorkflowCancelResult: {
            name: string;
            namespace: string;
            occurrence_key: string;
            phase: string;
        };
        WorkflowConditionItem: {
            rules: components["schemas"]["WorkflowRuleItem"][];
            type: string;
        };
        WorkflowGetItem: {
            concurrency_policy: string;
            /** Format: date-time */
            created_at: string;
            fail_policy: string;
            /** Format: int64 */
            generation: number | string;
            /** Format: int64 */
            id: number | string;
            name: string;
            namespace: string;
            schedule: string;
            source_uid: string;
            suspend: boolean;
            task_count: number;
            tasks: components["schemas"]["WorkflowTaskItem"][];
            time_zone: string;
            timeout_seconds: number;
        };
        WorkflowListItem: {
            concurrency_policy: string;
            /** Format: date-time */
            created_at: string;
            fail_policy: string;
            /** Format: int64 */
            generation: number | string;
            /** Format: int64 */
            id: number | string;
            name: string;
            namespace: string;
            schedule: string;
            source_uid: string;
            suspend: boolean;
            task_count: number;
            time_zone: string;
            timeout_seconds: number;
        };
        WorkflowListResponse: {
            items: components["schemas"]["WorkflowListItem"][];
        };
        WorkflowRuleItem: {
            field: string;
            /** Format: int64 */
            number?: number | string | null;
            operator: string;
            task: string;
            values?: string[];
        };
        WorkflowRunGetItem: {
            actor: string;
            /** Format: date-time */
            created_at: string;
            /** Format: int64 */
            id: number | string;
            occurrence_key: string;
            phase: string;
            pinned_revision_available: boolean;
            reason?: string;
            /** Format: int64 */
            revision_id: number | string;
            /** Format: date-time */
            scheduled_for?: string | null;
            snapshot_version: number;
            steps: components["schemas"]["WorkflowStepItem"][];
            task_decisions: {
                [key: string]: components["schemas"]["TaskDecision"];
            };
            task_snapshots: {
                [key: string]: components["schemas"]["TaskSnapshot"];
            };
            tasks?: components["schemas"]["WorkflowTaskItem"][] | null;
            trigger: string;
            /** Format: date-time */
            updated_at: string;
            workflow_source_uid: string;
        };
        WorkflowRunItem: {
            actor: string;
            /** Format: date-time */
            created_at: string;
            /** Format: int64 */
            id: number | string;
            occurrence_key: string;
            phase: string;
            reason?: string;
            /** Format: int64 */
            revision_id: number | string;
            /** Format: date-time */
            scheduled_for?: string | null;
            trigger: string;
            /** Format: date-time */
            updated_at: string;
            workflow_source_uid: string;
        };
        WorkflowRunListResponse: {
            items: components["schemas"]["WorkflowRunItem"][];
        };
        WorkflowSchedulePreviewRequest: {
            /**
             * @description Omitted or zero uses the default.
             * @default 5
             */
            count?: number;
            schedule: string;
            /**
             * @description IANA time zone; response instants are UTC.
             * @default UTC
             */
            time_zone?: string;
        };
        WorkflowSchedulePreviewResult: {
            time_zone: string;
            upcoming: string[];
        };
        WorkflowStepItem: {
            attempt: number;
            /** Format: date-time */
            created_at: string;
            /** Format: int64 */
            id: number | string;
            occurrence_key: string;
            phase: string;
            source_uid: string;
            task_name?: string;
            /** Format: date-time */
            updated_at: string;
        };
        WorkflowTaskItem: {
            condition?: components["schemas"]["WorkflowConditionItem"];
            depends_on: string[];
            job_ref: string;
            name: string;
        };
        WorkflowTriggerResult: {
            created: boolean;
            name: string;
            namespace: string;
            occurrence_key: string;
            phase: string;
            reason?: string;
            trigger: string;
        };
        Workspace: {
            id: string;
            name: string;
            role: string;
            slug: string;
            status?: string;
        };
        WorkspaceListItem: {
            id: string;
            name: string;
            slug: string;
            status: string;
        };
        WorkspaceListResult: {
            has_more: boolean;
            items: components["schemas"]["WorkspaceListItem"][];
        };
    };
    responses: never;
    parameters: never;
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export interface operations {
    renameAPIKey: {
        parameters: {
            query?: never;
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                /**
                 * @example {
                 *       "name": "Production operator"
                 *     }
                 */
                "application/json": components["schemas"]["RenameAPIKeyRequest"];
            };
        };
        responses: {
            /** @description Renamed API key */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIKeyRenameResult"];
                };
            };
            /** @description Invalid name */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Key unavailable or permission denied */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description API key not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    revokeAPIKey: {
        parameters: {
            query?: never;
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Revoked API key */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIKeyRevokeResult"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description API key not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    inviteConsoleMember: {
        parameters: {
            query?: never;
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["Grant"];
            };
        };
        responses: {
            /** @description Account operation completed */
            201: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Capability"];
                };
            };
            /** @description Malformed request or invalid input; inspect error.code, error.message and optional error.field */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Operation forbidden, CSRF rejected, or recent_auth_required where required */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Conflicting state, including last administrator protection */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    acceptConsoleInvitation: {
        parameters: {
            query?: never;
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["AcceptInvitationRequest"];
            };
        };
        responses: {
            /** @description Account operation completed */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SessionSnapshot"];
                };
            };
            /** @description Malformed request or invalid input; inspect error.code, error.message and optional error.field */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Operation forbidden, CSRF rejected, or recent_auth_required where required */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Conflicting state, including last administrator protection */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    loginConsole: {
        parameters: {
            query?: never;
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["LoginRequest"];
            };
        };
        responses: {
            /** @description Account operation completed */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SessionSnapshot"];
                };
            };
            /** @description Malformed request or invalid input; inspect error.code, error.message and optional error.field */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Operation forbidden, CSRF rejected, or recent_auth_required where required */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Conflicting state, including last administrator protection */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    logoutConsole: {
        parameters: {
            query?: never;
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Account operation completed */
            204: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Malformed request or invalid input; inspect error.code, error.message and optional error.field */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Operation forbidden, CSRF rejected, or recent_auth_required where required */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Conflicting state, including last administrator protection */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    listConsoleMembers: {
        parameters: {
            query: {
                tenant_id: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Account operation completed */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["MemberListResponse"];
                };
            };
            /** @description Malformed request or invalid input; inspect error.code, error.message and optional error.field */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Operation forbidden, CSRF rejected, or recent_auth_required where required */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Conflicting state, including last administrator protection */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    updateConsoleMember: {
        parameters: {
            query?: never;
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                user_id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["MemberChangeRequest"];
            };
        };
        responses: {
            /** @description Account operation completed */
            204: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Malformed request or invalid input; inspect error.code, error.message and optional error.field */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Operation forbidden, CSRF rejected, or recent_auth_required where required */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Conflicting state, including last administrator protection */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    issueConsolePasswordReset: {
        parameters: {
            query?: never;
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                user_id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["PasswordResetRequest"];
            };
        };
        responses: {
            /** @description Account operation completed */
            201: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Capability"];
                };
            };
            /** @description Malformed request or invalid input; inspect error.code, error.message and optional error.field */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Operation forbidden, CSRF rejected, or recent_auth_required where required */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Conflicting state, including last administrator protection */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    changeConsolePassword: {
        parameters: {
            query?: never;
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["PasswordChangeRequest"];
            };
        };
        responses: {
            /** @description Account operation completed */
            204: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Malformed request or invalid input; inspect error.code, error.message and optional error.field */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Operation forbidden, CSRF rejected, or recent_auth_required where required */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Conflicting state, including last administrator protection */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    resetConsolePassword: {
        parameters: {
            query?: never;
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ResetPasswordRequest"];
            };
        };
        responses: {
            /** @description Account operation completed */
            204: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Malformed request or invalid input; inspect error.code, error.message and optional error.field */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Operation forbidden, CSRF rejected, or recent_auth_required where required */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Conflicting state, including last administrator protection */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    reauthenticateConsole: {
        parameters: {
            query?: never;
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["PasswordRequest"];
            };
        };
        responses: {
            /** @description Account operation completed */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SessionSnapshot"];
                };
            };
            /** @description Malformed request or invalid input; inspect error.code, error.message and optional error.field */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Operation forbidden, CSRF rejected, or recent_auth_required where required */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Conflicting state, including last administrator protection */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    getConsoleSession: {
        parameters: {
            query?: never;
            header?: {
                "X-Trace-ID"?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Account operation completed */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SessionSnapshot"];
                };
            };
            /** @description Malformed request or invalid input; inspect error.code, error.message and optional error.field */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Operation forbidden, CSRF rejected, or recent_auth_required where required */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Conflicting state, including last administrator protection */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    listConsoleSessions: {
        parameters: {
            query?: never;
            header?: {
                "X-Trace-ID"?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Account operation completed */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SessionListResponse"];
                };
            };
            /** @description Malformed request or invalid input; inspect error.code, error.message and optional error.field */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Operation forbidden, CSRF rejected, or recent_auth_required where required */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Conflicting state, including last administrator protection */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    revokeConsoleSession: {
        parameters: {
            query?: never;
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Account operation completed */
            204: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Malformed request or invalid input; inspect error.code, error.message and optional error.field */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Operation forbidden, CSRF rejected, or recent_auth_required where required */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Conflicting state, including last administrator protection */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    setupConsole: {
        parameters: {
            query?: never;
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["SetupAccountRequest"];
            };
        };
        responses: {
            /** @description Account operation completed */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SessionSnapshot"];
                };
            };
            /** @description Malformed request or invalid input; inspect error.code, error.message and optional error.field */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Operation forbidden, CSRF rejected, or recent_auth_required where required */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Conflicting state, including last administrator protection */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    listConsoleUsers: {
        parameters: {
            query?: never;
            header?: {
                "X-Trace-ID"?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Account operation completed */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["UserListResponse"];
                };
            };
            /** @description Malformed request or invalid input; inspect error.code, error.message and optional error.field */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Operation forbidden, CSRF rejected, or recent_auth_required where required */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Conflicting state, including last administrator protection */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    updateConsoleUser: {
        parameters: {
            query?: never;
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["UserChange"];
            };
        };
        responses: {
            /** @description Account operation completed */
            204: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Malformed request or invalid input; inspect error.code, error.message and optional error.field */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Operation forbidden, CSRF rejected, or recent_auth_required where required */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Conflicting state, including last administrator protection */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    listCheckRuns: {
        parameters: {
            query?: {
                tenant_id?: string;
                check_id?: number | string;
                status?: "pending" | "running" | "success" | "failed";
                limit?: number;
                offset?: number;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Check run list */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CheckRunListResponse"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    getCheckRun: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                id: number | string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Check run detail */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CheckrunGetResult"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Check run not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    listChecks: {
        parameters: {
            query?: {
                search?: string;
                tenant_id?: string;
                status?: "active" | "paused";
                limit?: number;
                offset?: number;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Check list */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CheckListResponse"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    createCheck: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                /**
                 * @example {
                 *       "check_config": {
                 *         "expected_status": 200,
                 *         "method": "GET",
                 *         "url": "https://payments.example.com/health"
                 *       },
                 *       "check_type": "http_health",
                 *       "interval_sec": 60,
                 *       "name": "Payments API health",
                 *       "schedule_type": "interval"
                 *     }
                 */
                "application/json": components["schemas"]["CreateCheckRequest"];
            };
        };
        responses: {
            /** @description Created check */
            201: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CreateResult"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    getCheck: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                id: number | string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Check detail */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["GetResult"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Check not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    deleteCheck: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                id: number | string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                /**
                 * @example {
                 *       "version": 1
                 *     }
                 */
                "application/json": components["schemas"]["VersionRequest"];
            };
        };
        responses: {
            /** @description Deleted check */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["DeletedResponse"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Check not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Version conflict; reload the resource before submitting a new delete intent */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    getcheckExecutionControls: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                id: number | string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Current source controls and exact write capability */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SourceControlResponse"];
                };
            };
            /** @description Invalid controls */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Independent resource permission denied */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Parent resource unavailable in this scope */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Control version changed; read current controls */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    updatecheckExecutionControls: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                id: number | string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["SourceControlUpdateRequest"];
            };
        };
        responses: {
            /** @description Current source controls and exact write capability */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SourceControlResponse"];
                };
            };
            /** @description Invalid controls */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Independent resource permission denied */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Parent resource unavailable in this scope */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Control version changed; read current controls */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    pauseCheck: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                id: number | string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                /**
                 * @example {
                 *       "version": 1
                 *     }
                 */
                "application/json": components["schemas"]["ChangeCheckStatusRequest"];
            };
        };
        responses: {
            /** @description Paused check */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ChangeStatusResult"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Check not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Version conflict */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    resumeCheck: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                id: number | string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                /**
                 * @example {
                 *       "version": 1
                 *     }
                 */
                "application/json": components["schemas"]["ChangeCheckStatusRequest"];
            };
        };
        responses: {
            /** @description Resumed check */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ChangeStatusResult"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Check not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Version conflict */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    getjobDefinition: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                namespace: string;
                name: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Live definition, validation, acceptance or reconciliation */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Result"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Denied */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Not retained or unavailable parent */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Identity or version conflict */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    applyjobDefinition: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                namespace: string;
                name: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                /**
                 * @example {
                 *       "dry_run": true,
                 *       "request_id": "11111111-1111-4111-8111-111111111111",
                 *       "spec_json": "{\"executionPolicyVersion\":1,\"schedule\":\"0 2 * * *\",\"jobTemplate\":{\"image\":\"busybox:1.37.0\",\"command\":[\"df\"],\"args\":[\"-P\",\"/\"]}}"
                 *     }
                 */
                "application/json": components["schemas"]["ApplyRequest"];
            };
        };
        responses: {
            /** @description Live definition, validation, acceptance or reconciliation */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Result"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Denied */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Not retained or unavailable parent */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Identity or version conflict */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    applyjobDefinitionCompatible: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                namespace: string;
                name: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ApplyRequest"];
            };
        };
        responses: {
            /** @description Checked legacy operation */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Result"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Denied */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Not retained or unavailable parent */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Identity or version conflict */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    getjobDefinitionOperation: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                namespace: string;
                name: string;
                request_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Retained operation outcome */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Operation"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Denied */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Not retained or unavailable parent */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Identity or version conflict */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    getworkflowDefinition: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                namespace: string;
                name: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Live definition, validation, acceptance or reconciliation */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Result"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Denied */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Not retained or unavailable parent */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Identity or version conflict */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    applyworkflowDefinition: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                namespace: string;
                name: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                /**
                 * @example {
                 *       "dry_run": true,
                 *       "request_id": "11111111-1111-4111-8111-111111111111",
                 *       "spec_json": "{\"executionPolicyVersion\":1,\"tasks\":[{\"name\":\"extract\",\"jobRef\":{\"name\":\"billing-extract\"}},{\"name\":\"publish\",\"jobRef\":{\"name\":\"billing-publish\"},\"dependsOn\":[\"extract\"]}]}"
                 *     }
                 */
                "application/json": components["schemas"]["ApplyRequest"];
            };
        };
        responses: {
            /** @description Live definition, validation, acceptance or reconciliation */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Result"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Denied */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Not retained or unavailable parent */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Identity or version conflict */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    applyworkflowDefinitionCompatible: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                namespace: string;
                name: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ApplyRequest"];
            };
        };
        responses: {
            /** @description Checked legacy operation */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Result"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Denied */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Not retained or unavailable parent */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Identity or version conflict */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    getworkflowDefinitionOperation: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                namespace: string;
                name: string;
                request_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Retained operation outcome */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Operation"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Denied */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Not retained or unavailable parent */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Identity or version conflict */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    listFunctions: {
        parameters: {
            query?: {
                search?: string;
                tenant_id?: string;
                limit?: number;
                offset?: number;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Function list */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["FunctionListResponse"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    createFunction: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header: {
                "X-Trace-ID"?: string;
                "X-OrbitJob-Idempotency-Key": string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                /**
                 * @example {
                 *       "args": [
                 *         "-P",
                 *         "/"
                 *       ],
                 *       "command": [
                 *         "df"
                 *       ],
                 *       "image": "busybox:1.37.0",
                 *       "name": "disk-usage-snapshot"
                 *     }
                 */
                "application/json": components["schemas"]["FunctionDefinitionRequest"];
            };
        };
        responses: {
            /** @description Original Function returned for idempotent replay */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["FunctionItem"];
                };
            };
            /** @description Function created */
            201: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["FunctionItem"];
                };
            };
            /** @description Invalid definition or missing request identity */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Function creation denied */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request identity conflicts with earlier input */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error; replay with the same request identity after an uncertain response */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    getFunction: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                id: number | string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Function detail */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["FunctionItem"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Function not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    updateFunction: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                id: number | string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                /**
                 * @example {
                 *       "args": [
                 *         "-P",
                 *         "/"
                 *       ],
                 *       "command": [
                 *         "df"
                 *       ],
                 *       "expected_version": 1,
                 *       "image": "busybox:1.37.0",
                 *       "name": "disk-usage-snapshot",
                 *       "status": "paused"
                 *     }
                 */
                "application/json": components["schemas"]["UpdateFunctionRequest"];
            };
        };
        responses: {
            /** @description Definition saved; inspect readiness before invoking */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["FunctionItem"];
                };
            };
            /** @description Invalid definition or expected version */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Function unavailable in the authorized scope or update denied */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Function no longer exists */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description The definition version changed */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    getfunctionExecutionControls: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                id: number | string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Current source controls and exact write capability */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SourceControlResponse"];
                };
            };
            /** @description Invalid controls */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Independent resource permission denied */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Parent resource unavailable in this scope */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Control version changed; read current controls */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    updatefunctionExecutionControls: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                id: number | string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["SourceControlUpdateRequest"];
            };
        };
        responses: {
            /** @description Current source controls and exact write capability */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SourceControlResponse"];
                };
            };
            /** @description Invalid controls */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Independent resource permission denied */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Parent resource unavailable in this scope */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Control version changed; read current controls */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    invokeFunction: {
        parameters: {
            query?: {
                wait_seconds?: number;
                expected_version?: number;
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
                "X-OrbitJob-Idempotency-Key"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                id: number | string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Existing invocation returned for idempotent replay, or the wait expired and the async reference is repeated */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["FunctionInvokeResult"];
                };
            };
            /** @description Invocation published */
            201: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["FunctionInvokeResult"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Function not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description The function is paused, the reviewed definition changed, or the required revision is not ready */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    listFunctionRuns: {
        parameters: {
            query?: {
                offset?: number;
                created_from?: string;
                created_before?: string;
                tenant_id?: string;
                limit?: number;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                id: number | string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Function run list */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["FunctionRunListResponse"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Function not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    getFunctionRun: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                id: number | string;
                run_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Function run detail */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["FunctionRunItem"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Function or run not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    getFunctionAttemptLogs: {
        parameters: {
            query?: {
                tenant_id?: string;
                tail_lines?: number | string;
                limit_bytes?: number | string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                id: number | string;
                run_id: string;
                attempt: number;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Bounded snapshot or explicit retention/start/availability state */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["LogsResult"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Denied */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Not retained or unavailable parent */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Identity or version conflict */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    readfunctionExecutionArchive: {
        parameters: {
            query?: {
                tenant_id?: string;
                after?: number | string;
                limit?: number;
                attempt?: number;
                search?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                id: number | string;
                run_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Archive page, retained output export or cleanup receipt */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Page"];
                };
            };
            /** @description Invalid cursor or request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Independent log permission denied */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Archive not registered */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    deletefunctionExecutionArchive: {
        parameters: {
            query?: {
                tenant_id?: string;
                after?: number | string;
                limit?: number;
                attempt?: number;
                search?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                id: number | string;
                run_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Archive page, retained output export or cleanup receipt */
            202: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CleanupReceipt"];
                };
            };
            /** @description Invalid cursor or request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Independent log permission denied */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Archive not registered */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    downloadfunctionExecutionArchive: {
        parameters: {
            query?: {
                tenant_id?: string;
                after?: number | string;
                limit?: number;
                attempt?: number;
                search?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                id: number | string;
                run_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Archive page, retained output export or cleanup receipt */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "text/plain": string;
                };
            };
            /** @description Invalid cursor or request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Independent log permission denied */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Archive not registered */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    getIdentity: {
        parameters: {
            query?: {
                tenant_id?: string;
                action?: string[];
                resource?: string[];
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Authenticated identity */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["IdentityResponse"];
                };
            };
            /** @description Malformed request or invalid input; inspect error.code, error.message and optional error.field */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    listInstances: {
        parameters: {
            query?: {
                created_from?: string;
                created_before?: string;
                source_uid?: string;
                occurrence_key?: string;
                phase?: "Pending" | "CreatingAttempt" | "Running" | "RetryWaiting" | "Succeeded" | "Failed" | "CancelRequested" | "Canceled" | "CancelUnknown" | "ExecutionUnknown";
                limit?: number;
                offset?: number;
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Run list */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RunListResponse"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    summarizeInstances: {
        parameters: {
            query?: {
                source_uid?: string;
                occurrence_key?: string;
                phase?: "Pending" | "CreatingAttempt" | "Running" | "RetryWaiting" | "Succeeded" | "Failed" | "CancelRequested" | "Canceled" | "CancelUnknown" | "ExecutionUnknown";
                created_from?: string;
                created_before?: string;
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Matching run counts */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Summary"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    getInstance: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                run_id: number | string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Run detail */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RunGetItem"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Run not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    listAttempts: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                run_id: number | string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Attempt list */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["AttemptListResponse"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Run not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    getInstanceAttemptLogs: {
        parameters: {
            query?: {
                tenant_id?: string;
                tail_lines?: number | string;
                limit_bytes?: number | string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                run_id: number | string;
                attempt: number;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Bounded snapshot or explicit retention/start/availability state */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["LogsResult"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Denied */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Not retained or unavailable parent */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Identity or version conflict */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    cancelInstance: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                run_id: number | string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Cancel request accepted, or the run was already terminal */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CancelResult"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Run not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description The run's JobRun custom resource is missing */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    readinstanceExecutionArchive: {
        parameters: {
            query?: {
                tenant_id?: string;
                after?: number | string;
                limit?: number;
                attempt?: number;
                search?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                run_id: number | string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Archive page, retained output export or cleanup receipt */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Page"];
                };
            };
            /** @description Invalid cursor or request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Independent log permission denied */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Archive not registered */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    deleteinstanceExecutionArchive: {
        parameters: {
            query?: {
                tenant_id?: string;
                after?: number | string;
                limit?: number;
                attempt?: number;
                search?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                run_id: number | string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Archive page, retained output export or cleanup receipt */
            202: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CleanupReceipt"];
                };
            };
            /** @description Invalid cursor or request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Independent log permission denied */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Archive not registered */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    downloadinstanceExecutionArchive: {
        parameters: {
            query?: {
                tenant_id?: string;
                after?: number | string;
                limit?: number;
                attempt?: number;
                search?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                run_id: number | string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Archive page, retained output export or cleanup receipt */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "text/plain": string;
                };
            };
            /** @description Invalid cursor or request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Independent log permission denied */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Archive not registered */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    listJobs: {
        parameters: {
            query?: {
                search?: string;
                namespace?: string;
                source_uid?: string;
                tenant_id?: string;
                limit?: number;
                offset?: number;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Job list */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["JobListResponse"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    getJob: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                id: number | string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Job detail */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["GetItem"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Job not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    getjobExecutionControls: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                id: number | string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Current source controls and exact write capability */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SourceControlResponse"];
                };
            };
            /** @description Invalid controls */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Independent resource permission denied */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Parent resource unavailable in this scope */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Control version changed; read current controls */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    updatejobExecutionControls: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                id: number | string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["SourceControlUpdateRequest"];
            };
        };
        responses: {
            /** @description Current source controls and exact write capability */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SourceControlResponse"];
                };
            };
            /** @description Invalid controls */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Independent resource permission denied */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Parent resource unavailable in this scope */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Control version changed; read current controls */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    triggerJob: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
                "X-OrbitJob-Idempotency-Key"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                id: number | string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Existing run returned for idempotent trigger */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["TriggerResult"];
                };
            };
            /** @description Created run */
            201: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["TriggerResult"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Job not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Definition is suspended */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    listPolicies: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Policy list */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["PolicyListResponse"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    createPolicy: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                /**
                 * @example {
                 *       "document": {
                 *         "statement": [
                 *           {
                 *             "action": [
                 *               "job:List",
                 *               "job:Get"
                 *             ],
                 *             "effect": "Allow",
                 *             "resource": [
                 *               "orbitjob:self:*:job/*"
                 *             ]
                 *           }
                 *         ],
                 *         "version": "2026-01-01"
                 *       },
                 *       "name": "Read job definitions"
                 *     }
                 */
                "application/json": components["schemas"]["CreatePolicyRequest"];
            };
        };
        responses: {
            /** @description Created policy */
            201: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["PolicyCreateResult"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Not permitted */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    getPolicy: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Policy detail */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["PolicyGetResult"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Policy not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    deletePolicy: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Deleted policy */
            204: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Not permitted */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Policy not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Policy still bound */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    listResourceGroups: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Resource group list */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ResourceGroupListResponse"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    createResourceGroup: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                /**
                 * @example {
                 *       "name": "Reporting",
                 *       "slug": "reporting"
                 *     }
                 */
                "application/json": components["schemas"]["CreateResourceGroupRequest"];
            };
        };
        responses: {
            /** @description Created resource group */
            201: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ResourcegroupCreateResult"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Slug already used */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    listSLIs: {
        parameters: {
            query?: {
                search?: string;
                tenant_id?: string;
                limit?: number;
                offset?: number;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description SLI list */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SliListResponse"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    createSLI: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                /**
                 * @example {
                 *       "aggregation": "ratio",
                 *       "good_event_criteria": {
                 *         "status": "success"
                 *       },
                 *       "name": "Payments API readiness",
                 *       "sli_type": "availability",
                 *       "source_config": {
                 *         "source_uid": "check-1"
                 *       },
                 *       "source_type": "job_run"
                 *     }
                 */
                "application/json": components["schemas"]["CreateSLIRequest"];
            };
        };
        responses: {
            /** @description Created SLI */
            201: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SliCreateResult"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    getSLI: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                id: number | string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description SLI detail */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SliGetItem"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description SLI not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    deleteSLI: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                id: number | string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                /**
                 * @example {
                 *       "version": 1
                 *     }
                 */
                "application/json": components["schemas"]["VersionRequest"];
            };
        };
        responses: {
            /** @description Deleted SLI */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["DeletedResponse"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description SLI not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    listSLOAlerts: {
        parameters: {
            query?: {
                tenant_id?: string;
                slo_id?: number | string;
                status?: string;
                limit?: number;
                offset?: number;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description SLO alert list */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["AlertListResponse"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    getSLOAlert: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                id: number | string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description SLO alert detail */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SloalertGetItem"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description SLO alert not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    listSLOs: {
        parameters: {
            query?: {
                search?: string;
                tenant_id?: string;
                limit?: number;
                offset?: number;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description SLO list */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SloListResponse"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    createSLO: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                /**
                 * @example {
                 *       "name": "Payments API availability",
                 *       "sli_id": 1,
                 *       "target": 0.99,
                 *       "window_duration": "24h",
                 *       "window_type": "rolling"
                 *     }
                 */
                "application/json": components["schemas"]["CreateSLORequest"];
            };
        };
        responses: {
            /** @description Created SLO */
            201: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SloCreateResult"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    getSLO: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                id: number | string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description SLO detail */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SloGetItem"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description SLO not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    deleteSLO: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                id: number | string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                /**
                 * @example {
                 *       "version": 1
                 *     }
                 */
                "application/json": components["schemas"]["VersionRequest"];
            };
        };
        responses: {
            /** @description Deleted SLO */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["DeletedResponse"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description SLO not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Version conflict; reload the resource before submitting a new delete intent */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    getSLOBudget: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                id: number | string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description SLO budget detail */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SlobudgetGetItem"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description SLO not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    listSLOBudgets: {
        parameters: {
            query?: {
                tenant_id?: string;
                limit?: number;
                offset?: number;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                id: number | string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description SLO budget history list */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["BudgetListResponse"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description SLO not found in the credential scope */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    pauseSLO: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                id: number | string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                /**
                 * @example {
                 *       "version": 1
                 *     }
                 */
                "application/json": components["schemas"]["ChangeSLOStatusRequest"];
            };
        };
        responses: {
            /** @description Paused SLO */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SloChangeStatusResult"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description SLO not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Version conflict */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    resumeSLO: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                id: number | string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                /**
                 * @example {
                 *       "version": 1
                 *     }
                 */
                "application/json": components["schemas"]["ChangeSLOStatusRequest"];
            };
        };
        responses: {
            /** @description Resumed SLO */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SloChangeStatusResult"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description SLO not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Version conflict */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    listTenants: {
        parameters: {
            query?: {
                limit?: number;
                offset?: number;
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Tenant list */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["TenantListResponse"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    createTenant: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                /**
                 * @example {
                 *       "name": "Commerce Operations",
                 *       "slug": "commerce-ops"
                 *     }
                 */
                "application/json": components["schemas"]["CreateTenantRequest"];
            };
        };
        responses: {
            /** @description Created tenant */
            201: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["TenantCreateResult"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    getTenant: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Tenant detail */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["TenantGetResult"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Tenant not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    listAPIKeys: {
        parameters: {
            query?: never;
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description API key list */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ApiKeyListResponse"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    createAPIKey: {
        parameters: {
            query?: never;
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody?: {
            content: {
                /**
                 * @example {
                 *       "name": "Job reader",
                 *       "policies": [
                 *         "REPLACE_WITH_POLICY_ID"
                 *       ]
                 *     }
                 */
                "application/json": components["schemas"]["CreateAPIKeyRequest"];
            };
        };
        responses: {
            /** @description Created API key */
            201: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIKeyCreateResult"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    listTenantPolicies: {
        parameters: {
            query?: never;
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Target tenant policies and platform presets */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["PolicyListResponse"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Exact target action denied or requested grant exceeds caller permissions */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Resource conflicts with current state */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    createTenantPolicy: {
        parameters: {
            query?: never;
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                /**
                 * @example {
                 *       "document": {
                 *         "statement": [
                 *           {
                 *             "action": [
                 *               "job:List",
                 *               "job:Get"
                 *             ],
                 *             "effect": "Allow",
                 *             "resource": [
                 *               "orbitjob:self:*:job/*"
                 *             ]
                 *           }
                 *         ],
                 *         "version": "2026-01-01"
                 *       },
                 *       "name": "Read job definitions"
                 *     }
                 */
                "application/json": components["schemas"]["CreatePolicyRequest"];
            };
        };
        responses: {
            /** @description Created target policy */
            201: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["PolicyCreateResult"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Exact target action denied or requested grant exceeds caller permissions */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Resource conflicts with current state */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    listTenantResourceGroups: {
        parameters: {
            query?: never;
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Target tenant resource groups */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ResourceGroupListResponse"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Exact target action denied or requested grant exceeds caller permissions */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Resource conflicts with current state */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    createTenantResourceGroup: {
        parameters: {
            query?: never;
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                /**
                 * @example {
                 *       "name": "Reporting",
                 *       "slug": "reporting"
                 *     }
                 */
                "application/json": components["schemas"]["CreateResourceGroupRequest"];
            };
        };
        responses: {
            /** @description Created target resource group */
            201: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ResourcegroupCreateResult"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Exact target action denied or requested grant exceeds caller permissions */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Resource conflicts with current state */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    listWorkflows: {
        parameters: {
            query?: {
                search?: string;
                tenant_id?: string;
                limit?: number;
                offset?: number;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Workflow list */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkflowListResponse"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description A group-scoped key cannot reach workflow definitions */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    previewWorkflowSchedule: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                /**
                 * @example {
                 *       "count": 5,
                 *       "schedule": "0 9 * * 1-5",
                 *       "time_zone": "Asia/Shanghai"
                 *     }
                 */
                "application/json": components["schemas"]["WorkflowSchedulePreviewRequest"];
            };
        };
        responses: {
            /** @description Upcoming UTC instants */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkflowSchedulePreviewResult"];
                };
            };
            /** @description Invalid cron, timezone or count */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Denied */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    getWorkflow: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                id: number | string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Workflow detail */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkflowGetItem"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description A group-scoped key cannot reach workflow definitions */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Workflow not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    getworkflowExecutionControls: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                id: number | string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Current source controls and exact write capability */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SourceControlResponse"];
                };
            };
            /** @description Invalid controls */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Independent resource permission denied */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Parent resource unavailable in this scope */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Control version changed; read current controls */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    updateworkflowExecutionControls: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                id: number | string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["SourceControlUpdateRequest"];
            };
        };
        responses: {
            /** @description Current source controls and exact write capability */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SourceControlResponse"];
                };
            };
            /** @description Invalid controls */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Independent resource permission denied */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Parent resource unavailable in this scope */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Control version changed; read current controls */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error, including unavailable authorization context or permission loading */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    listWorkflowRuns: {
        parameters: {
            query?: {
                created_from?: string;
                created_before?: string;
                tenant_id?: string;
                limit?: number;
                offset?: number;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                id: number | string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Workflow run list */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkflowRunListResponse"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description A group-scoped key cannot reach workflow runs */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Workflow not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    triggerWorkflow: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
                "X-OrbitJob-Idempotency-Key"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                id: number | string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Existing run returned for idempotent trigger */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkflowTriggerResult"];
                };
            };
            /** @description Created workflow run */
            201: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkflowTriggerResult"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description A group-scoped key cannot trigger workflows */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Workflow not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description The Workflow definition cannot currently be triggered */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    getWorkflowRun: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path: {
                id: number | string;
                run_id: number | string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Workflow run detail */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkflowRunGetItem"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description A group-scoped key cannot reach workflow runs */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Workflow or run not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    cancelWorkflowRun: {
        parameters: {
            query?: {
                tenant_id?: string;
            };
            header?: {
                "X-Trace-ID"?: string;
                "X-CSRF-Token"?: string;
                Origin?: string;
            };
            path: {
                id: number | string;
                run_id: number | string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Cancel request accepted, or the run was already terminal */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkflowCancelResult"];
                };
            };
            /** @description Invalid request */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description A group-scoped key cannot cancel workflow runs */
            403: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Workflow or run not found */
            404: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description The run's WorkflowRun custom resource is missing */
            409: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Internal error */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    listWorkspaces: {
        parameters: {
            query?: {
                search?: string;
                limit?: number;
                offset?: number;
            };
            header?: {
                "X-Trace-ID"?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Authorized workspace directory page */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspaceListResult"];
                };
            };
            /** @description Invalid search or pagination */
            400: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Request body exceeds the shared 1 MiB limit */
            413: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Rate limit exceeded */
            429: {
                headers: {
                    /** @description Minimum seconds until the request may be retried (default 1) */
                    "Retry-After"?: string;
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Directory unavailable */
            500: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
            /** @description Authentication, authorization or operation dependency unavailable; retain the original write identity */
            503: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ErrorResponse"];
                };
            };
        };
    };
    getHealthz: {
        parameters: {
            query?: never;
            header?: {
                "X-Trace-ID"?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Service is healthy */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["HealthzResponse"];
                };
            };
        };
    };
    getMetrics: {
        parameters: {
            query?: never;
            header?: {
                "X-Trace-ID"?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Metrics payload */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "text/plain": string;
                };
            };
        };
    };
    getOpenAPIDocument: {
        parameters: {
            query?: never;
            header?: {
                "X-Trace-ID"?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OpenAPI 3 document */
            200: {
                headers: {
                    /** @description Trace identifier echoed back to clients. Generated when request does not provide one. */
                    "X-Trace-ID"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": {
                        [key: string]: unknown;
                    };
                };
            };
        };
    };
}
