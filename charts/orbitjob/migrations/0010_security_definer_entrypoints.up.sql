-- Narrow cross-tenant entry points required before a tenant context is known.
CREATE OR REPLACE FUNCTION orbitjob_auth_api_key(p_prefix text)
RETURNS TABLE(id text, tenant_id text, key_hash text, revoked text, expired text)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
  SELECT ak.id::text, ak.tenant_id::text, ak.key_hash::text,
         CASE WHEN ak.revoked_at IS NOT NULL THEN 'revoked' END,
         CASE WHEN ak.expires_at IS NOT NULL AND ak.expires_at < now() THEN 'expired' END
  FROM public.api_keys ak
  JOIN public.tenants t ON t.id = ak.tenant_id
  WHERE ak.key_prefix = p_prefix
    AND ak.revoked_at IS NULL
    AND t.status = 'active'
  ORDER BY ak.created_at DESC
$$;

CREATE OR REPLACE FUNCTION orbitjob_list_active_tenant_ids()
RETURNS TABLE(id text)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
  SELECT t.id::text FROM public.tenants t WHERE t.status = 'active' ORDER BY t.id
$$;

REVOKE ALL ON FUNCTION public.orbitjob_auth_api_key(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.orbitjob_list_active_tenant_ids() FROM PUBLIC;
ALTER FUNCTION public.orbitjob_auth_api_key(text) OWNER TO orbitjob_table_owner;
ALTER FUNCTION public.orbitjob_list_active_tenant_ids() OWNER TO orbitjob_table_owner;
GRANT EXECUTE ON FUNCTION public.orbitjob_auth_api_key(text) TO orbitjob_admin;
GRANT EXECUTE ON FUNCTION public.orbitjob_list_active_tenant_ids() TO orbitjob_runtime;
