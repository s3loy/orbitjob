-- 0003: cross-tenant API key lookup for admin revocation
--
-- FindKeyTenant (internal/admin/store/postgres/apikey_repository.go)
-- must locate the owning tenant of an arbitrary API key before setting
-- the tenant context for revocation. The api_keys table has RLS enabled,
-- so a direct SELECT returns zero rows without app.tenant_id set.
-- This SECURITY DEFINER function runs as table owner and bypasses RLS,
-- matching the existing orbitjob_auth_api_key and orbitjob_list_active_tenant_ids
-- patterns.

CREATE OR REPLACE FUNCTION orbitjob_find_key_tenant(p_key_id text)
RETURNS TABLE(tenant_id text)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
  SELECT ak.tenant_id::text FROM public.api_keys ak
  WHERE ak.id = p_key_id AND ak.revoked_at IS NULL
$$;

REVOKE ALL ON FUNCTION public.orbitjob_find_key_tenant(text) FROM PUBLIC;
ALTER FUNCTION public.orbitjob_find_key_tenant(text) OWNER TO orbitjob_table_owner;
GRANT EXECUTE ON FUNCTION public.orbitjob_find_key_tenant(text) TO orbitjob_admin;
