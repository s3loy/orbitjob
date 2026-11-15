//go:build integration

package migrations

import (
	"context"
	"strings"
	"testing"
)

func TestWorkspaceDirectoryDefinerHasOnlyBoundedAdminMetadataPrivilege(t *testing.T) {
	applyAllMigrations(t)
	db := ownerDB(t)
	ctx := context.Background()
	const signature = "public.orbitjob_workspace_directory(boolean,text[],text[],text,integer,integer)"
	for _, role := range []string{"orbitjob_admin", "orbitjob_runtime", "orbitjob_operator", "orbitjob_migrator"} {
		var allowed bool
		if err := db.QueryRowContext(ctx, `SELECT has_function_privilege($1,$2,'EXECUTE')`, role, signature).Scan(&allowed); err != nil {
			t.Fatal(err)
		}
		if allowed != (role == "orbitjob_admin") {
			t.Fatalf("%s execute=%v", role, allowed)
		}
	}
	var owner, config, result string
	var definer, publicExecute bool
	if err := db.QueryRowContext(ctx, `SELECT r.rolname,p.prosecdef,array_to_string(p.proconfig,','),pg_get_function_result(p.oid),EXISTS(SELECT 1 FROM unnest(coalesce(p.proacl,ARRAY[]::aclitem[])) acl WHERE acl::text LIKE '=X/%') FROM pg_proc p JOIN pg_roles r ON r.oid=p.proowner WHERE p.oid=$1::regprocedure`, signature).Scan(&owner, &definer, &config, &result, &publicExecute); err != nil {
		t.Fatal(err)
	}
	if owner != "orbitjob_table_owner" || !definer || !strings.Contains(config, "search_path=pg_catalog, public") || publicExecute || result != "TABLE(id text, name text, slug text, status text)" {
		t.Fatalf("unsafe definer owner=%s definer=%v config=%s result=%s public=%v", owner, definer, config, result, publicExecute)
	}
	// Table policies and helper role assignments have not been expanded.
	var rls bool
	if err := db.QueryRowContext(ctx, `SELECT relrowsecurity FROM pg_class WHERE oid='public.tenants'::regclass`).Scan(&rls); err != nil || !rls {
		t.Fatalf("tenant RLS disabled: %v %v", rls, err)
	}
}
