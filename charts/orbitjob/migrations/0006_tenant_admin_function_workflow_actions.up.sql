-- ============================================================
-- Migration 0006: TenantAdminAccess gains the Functions/Workflows actions
--
-- Migrations 0004 (functions) and 0005 (workflows) deliberately touched no
-- preset-policy rows: an action earns a preset place only when the HTTP API
-- enforces it, so the preset update lands with the routes it arms. Those
-- routes are landed now -- /functions (List/Get/Invoke and the runs reads
-- behind function:List / function:Get) and /workflows (List/Get/Trigger and
-- the runs reads behind workflow:List / workflow:Get) -- so this migration
-- adds the six actions to the TenantAdminAccess preset document:
--
--   function:List, function:Get, function:Invoke
--   workflow:List, workflow:Get, workflow:Trigger
--
-- Tenant-admin keys minted after this migration can invoke functions and
-- trigger workflows out of the box, exactly as they could already trigger
-- jobs. The document is replaced in full (the baseline's convention: action
-- lists are written out, never wildcarded) and only when the new actions are
-- absent, so re-applying is a no-op.
--
-- The ReadOnlyAccess preset gains nothing: its read actions cover definitions
-- and runs the routes expose, and extending it is a separate product ruling.
-- ============================================================

UPDATE policies
SET document = '{"version":"1","statement":[{"effect":"Allow","action":["tenant:Get","tenant:List","apikey:Create","apikey:List","apikey:Revoke","policy:Create","policy:Get","policy:List","policy:Delete","group:Create","group:List","job:Get","job:List","job:Trigger","instance:Get","instance:List","instance:Cancel","check:Create","check:Get","check:List","check:Delete","check:Pause","check:Resume","checkrun:Get","checkrun:List","sli:Create","sli:Get","sli:List","sli:Delete","slo:Create","slo:Get","slo:List","slo:Delete","slo:Pause","slo:Resume","slo:GetBudget","slo:ListBudgets","sloalert:Get","sloalert:List","function:List","function:Get","function:Invoke","workflow:List","workflow:Get","workflow:Trigger"],"resource":["orbitjob:self:*:*/*"]}]}'
WHERE id = '00000000000000000000000011'
  AND name = 'TenantAdminAccess'
  AND tenant_id IS NULL
  AND NOT document @> '{"statement":[{"action":["function:Invoke"]}]}';
