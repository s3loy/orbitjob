-- ADR0021: bounded workspace display metadata, independently of tenant RLS.
-- Only the trusted authenticated Admin API calls this helper. Its all/include/
-- exclude arguments are computed from the caller's effective policy expression,
-- including Deny and boundary; they are never supplied by the HTTP caller.
-- Visibility and literal search precede pagination, so hidden rows never affect
-- the result count, page boundaries, or has_more.
CREATE FUNCTION public.orbitjob_workspace_directory(
  p_all boolean, p_include text[], p_exclude text[], p_search text,
  p_limit integer, p_offset integer
)
RETURNS TABLE(id text, name text, slug text, status text)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
  SELECT t.id::text, t.name::text, t.slug::text, t.status::text
  FROM public.tenants AS t
  WHERE (
    (COALESCE(p_all, false)
      AND NOT (t.id::text = ANY(COALESCE(p_exclude, ARRAY[]::text[]))))
    OR t.id::text = ANY(COALESCE(p_include, ARRAY[]::text[]))
  ) AND (
    COALESCE(p_search, '') = ''
    OR strpos(lower(t.name), lower(p_search)) > 0
    OR strpos(lower(t.slug), lower(p_search)) > 0
  )
  ORDER BY lower(t.name), t.id
  LIMIT LEAST(GREATEST(COALESCE(p_limit, 21), 1), 101)
  OFFSET GREATEST(COALESCE(p_offset, 0), 0)
$$;
REVOKE ALL ON FUNCTION public.orbitjob_workspace_directory(boolean,text[],text[],text,integer,integer) FROM PUBLIC;
ALTER FUNCTION public.orbitjob_workspace_directory(boolean,text[],text[],text,integer,integer) OWNER TO orbitjob_table_owner;
GRANT EXECUTE ON FUNCTION public.orbitjob_workspace_directory(boolean,text[],text[],text,integer,integer) TO orbitjob_admin;
