-- ADR0011: only Admin may load the authenticated platform key's preset grants.
-- The caller is trusted to supply the ID resolved by authentication; this
-- helper is not credential authentication and accepts no target tenant.
CREATE FUNCTION public.orbitjob_platform_key_documents(p_key_id text)
RETURNS TABLE(policy_id text, document jsonb, is_boundary boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
DECLARE
  boundary_id text;
BEGIN
  SELECT ak.boundary_policy_id::text INTO boundary_id
  FROM public.api_keys ak
  WHERE ak.id = p_key_id AND ak.kind = 'platform' AND ak.tenant_id IS NULL
    AND ak.revoked_at IS NULL
    AND (ak.expires_at IS NULL OR ak.expires_at > CURRENT_TIMESTAMP);
  IF NOT FOUND THEN
    RETURN;
  END IF;

  -- A configured but ineligible boundary must never become "no boundary".
  IF boundary_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM public.policies p WHERE p.id = boundary_id AND p.tenant_id IS NULL
  ) THEN
    RAISE EXCEPTION 'platform permission boundary unavailable' USING ERRCODE = '42501';
  END IF;

  RETURN QUERY
    SELECT p.id::text, p.document, false
    FROM public.key_policies kp
    JOIN public.policies p ON p.id = kp.policy_id
    WHERE kp.key_id = p_key_id AND p.tenant_id IS NULL
    ORDER BY p.id;
  IF boundary_id IS NOT NULL THEN
    RETURN QUERY SELECT p.id::text, p.document, true
      FROM public.policies p WHERE p.id = boundary_id AND p.tenant_id IS NULL;
  END IF;
END
$$;
REVOKE ALL ON FUNCTION public.orbitjob_platform_key_documents(text) FROM PUBLIC;
ALTER FUNCTION public.orbitjob_platform_key_documents(text) OWNER TO orbitjob_table_owner;
GRANT EXECUTE ON FUNCTION public.orbitjob_platform_key_documents(text) TO orbitjob_admin;
