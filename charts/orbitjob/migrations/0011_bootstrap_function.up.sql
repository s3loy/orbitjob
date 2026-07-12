-- Bootstrap is the only cross-tenant write path. The caller supplies a bcrypt
-- hash; plaintext API keys never enter PostgreSQL functions or logs.
CREATE OR REPLACE FUNCTION orbitjob_bootstrap_default(
  p_tenant_id text,
  p_tenant_slug text,
  p_tenant_name text,
  p_tenant_status text,
  p_key_id text,
  p_key_hash text,
  p_key_prefix text,
  p_permissions jsonb
)
RETURNS TABLE(tenant_created boolean, key_created boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
DECLARE
  tenant_rows bigint;
  key_rows bigint;
BEGIN
  IF session_user <> 'orbitjob_admin' THEN
    RAISE EXCEPTION 'bootstrap function requires orbitjob_admin';
  END IF;
  IF p_tenant_id <> '00000000000000000000000001'
     OR p_tenant_slug <> 'default'
     OR p_tenant_name <> 'Default'
     OR p_tenant_status <> 'active'
     OR p_key_id <> '00000000000000000000000002'
     OR p_permissions <> '{}'::jsonb THEN
    RAISE EXCEPTION 'bootstrap function only accepts the default bootstrap identity';
  END IF;

  INSERT INTO public.tenants (id, slug, name, status, created_at, updated_at)
  VALUES (p_tenant_id, p_tenant_slug, p_tenant_name, p_tenant_status, now(), now())
  ON CONFLICT (id) DO NOTHING;
  GET DIAGNOSTICS tenant_rows = ROW_COUNT;

  IF tenant_rows = 0 AND NOT EXISTS (
    SELECT 1 FROM public.tenants
    WHERE id = p_tenant_id
      AND slug = p_tenant_slug
      AND name = p_tenant_name
      AND status = p_tenant_status
  ) THEN
    RAISE EXCEPTION 'bootstrap tenant conflicts with existing row';
  END IF;

  INSERT INTO public.api_keys (id, tenant_id, key_hash, key_prefix, permissions, created_at)
  VALUES (p_key_id, p_tenant_id, p_key_hash, p_key_prefix, p_permissions, now())
  ON CONFLICT (id) DO NOTHING;
  GET DIAGNOSTICS key_rows = ROW_COUNT;

  IF key_rows = 0 AND NOT EXISTS (
    SELECT 1 FROM public.api_keys
    WHERE id = p_key_id
      AND tenant_id = p_tenant_id
      AND key_prefix = p_key_prefix
      AND permissions = p_permissions
  ) THEN
    RAISE EXCEPTION 'bootstrap API key conflicts with existing row';
  END IF;

  RETURN QUERY SELECT tenant_rows = 1, key_rows = 1;
END
$$;

REVOKE ALL ON FUNCTION public.orbitjob_bootstrap_default(text,text,text,text,text,text,text,jsonb) FROM PUBLIC;
ALTER FUNCTION public.orbitjob_bootstrap_default(text,text,text,text,text,text,text,jsonb) OWNER TO orbitjob_table_owner;
GRANT EXECUTE ON FUNCTION public.orbitjob_bootstrap_default(text,text,text,text,text,text,text,jsonb) TO orbitjob_admin;
