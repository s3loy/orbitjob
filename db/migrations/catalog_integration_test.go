//go:build integration

package migrations

import (
	"context"
	"strings"
	"testing"
	"time"
)

// tenantRelationPredicate derives "this relation carries tenant data" from the
// catalog instead of a list of names. A relation qualifies if it has a
// tenant_id column, if it is the tenants table itself (keyed by id, not
// tenant_id), or if it already carries a policy. The last clause is what closes
// the gap in the baseline's own rule 2: key_policies has no tenant_id column and
// is not named tenants, so rule 2 cannot see it lose its RLS.
const tenantRelationPredicate = `
	c.relname = 'tenants'
	OR EXISTS (SELECT 1 FROM pg_attribute a
	           WHERE a.attrelid = c.oid AND a.attname = 'tenant_id'
	             AND a.attnum > 0 AND NOT a.attisdropped)
	OR EXISTS (SELECT 1 FROM pg_policies p
	           WHERE p.schemaname = 'public' AND p.tablename = c.relname)`

// TestEveryTenantRelationHasRLS is property 1. It is checked from the catalog so
// a new tenant table or a new partition is covered the moment it exists. It is
// deliberately wider than the assertion inside the baseline: the baseline's rule
// 2 misses a relation with no tenant_id column, and key_policies is one.
func TestEveryTenantRelationHasRLS(t *testing.T) {
	applyAllMigrations(t)
	db := ownerDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	offenders := names(t, ctx, db, `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public'
		  AND c.relkind IN ('r', 'p')
		  AND NOT c.relrowsecurity
		  AND (`+tenantRelationPredicate+`)
		ORDER BY c.relname
	`)
	if len(offenders) != 0 {
		t.Fatalf("tenant relations with row level security disabled: %v", offenders)
	}
}

// TestNoRelationUsesForceRLS is property 3. ADR 0001 makes RLS ENABLE-only: the
// table owner is NOLOGIN, so no process queries as the owner and the
// owner-bypass path FORCE would close is unreachable. A FORCE anywhere is a
// defence that guards nobody and hides that the real protection is the grants.
func TestNoRelationUsesForceRLS(t *testing.T) {
	applyAllMigrations(t)
	db := ownerDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	offenders := names(t, ctx, db, `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relforcerowsecurity
		ORDER BY c.relname
	`)
	if len(offenders) != 0 {
		t.Fatalf("relations with FORCE ROW LEVEL SECURITY: %v", offenders)
	}
}

// TestTenantRelationsHaveAFullTenantPolicy is the other half of isolation:
// RLS enabled with no policy denies everything, and a policy that covers SELECT
// but not UPDATE would let a tenant see its rows and not change them, or the
// reverse. Every tenant relation must carry at least one FOR ALL policy whose
// USING and WITH CHECK both bind to app.tenant_id.
func TestTenantRelationsHaveAFullTenantPolicy(t *testing.T) {
	applyAllMigrations(t)
	db := ownerDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	offenders := names(t, ctx, db, `
		WITH tenant_rel AS (
			SELECT c.oid, c.relname
			FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p')
			  AND (`+tenantRelationPredicate+`)
		)
		SELECT t.relname FROM tenant_rel t
		WHERE NOT EXISTS (
			SELECT 1 FROM pg_policies p
			WHERE p.schemaname = 'public' AND p.tablename = t.relname
			  AND p.cmd = 'ALL'
			  AND p.qual LIKE '%app.tenant_id%'
			  AND p.with_check LIKE '%app.tenant_id%'
		)
		ORDER BY t.relname
	`)
	if len(offenders) != 0 {
		t.Fatalf("tenant relations without a full (FOR ALL, USING + WITH CHECK on app.tenant_id) policy: %v", offenders)
	}
}

// TestStructuralAssertionAcceptsCleanSchema is the positive control for the
// probes below. If the assertion raised on every schema, a probe going red would
// prove nothing.
func TestStructuralAssertionAcceptsCleanSchema(t *testing.T) {
	applyAllMigrations(t)
	db := ownerDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, baselineStructuralAssertion(t)); err != nil {
		t.Fatalf("the baseline's own assertion rejected a clean schema: %v", err)
	}
}

// TestStructuralAssertionRejectsPartitionWithoutRLS proves the in-file assertion
// is sensitive to the failure it names. A partition does not inherit its
// parent's policy, and a named list never contained audit_events_default. If the
// assertion is weakened to a list, this probe stops raising and goes red.
func TestStructuralAssertionRejectsPartitionWithoutRLS(t *testing.T) {
	applyAllMigrations(t)
	db := ownerDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const probe = "audit_events_rls_probe"
	if _, err := db.ExecContext(ctx, "CREATE TABLE public."+probe+
		" PARTITION OF public.audit_events FOR VALUES FROM ('2099-01-01') TO ('2100-01-01')"); err != nil {
		t.Fatalf("create probe partition: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec("DROP TABLE IF EXISTS public." + probe) })

	_, err := db.ExecContext(ctx, baselineStructuralAssertion(t))
	if err == nil {
		t.Fatalf("assertion accepted partition %s of a protected table with no RLS of its own; "+
			"an unnamed partition is exactly what a name-list assertion cannot see", probe)
	}
	if !strings.Contains(err.Error(), probe) {
		t.Fatalf("assertion failed for the wrong reason: %v", err)
	}
}

// TestStructuralAssertionRejectsTenantRelationWithoutRLS proves rule 2 is
// sensitive: a relation carrying tenant_id with no RLS must be named.
func TestStructuralAssertionRejectsTenantRelationWithoutRLS(t *testing.T) {
	applyAllMigrations(t)
	db := ownerDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const probe = "rls_probe_tenant_table"
	if _, err := db.ExecContext(ctx, "CREATE TABLE public."+probe+" (id bigint, tenant_id text NOT NULL)"); err != nil {
		t.Fatalf("create probe table: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec("DROP TABLE IF EXISTS public." + probe) })

	_, err := db.ExecContext(ctx, baselineStructuralAssertion(t))
	if err == nil {
		t.Fatalf("assertion accepted tenant relation %s with row level security disabled", probe)
	}
	if !strings.Contains(err.Error(), probe) {
		t.Fatalf("assertion failed for the wrong reason: %v", err)
	}
}

func TestRoleAttributesAndOwnership(t *testing.T) {
	applyAllMigrations(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := ownerDB(t)

	// The attributes are what let the baseline run without a superuser: none of
	// these roles may be superuser, bypass RLS, create roles, or inherit, and
	// only the deployment logins may log in.
	for _, role := range []struct {
		name  string
		login bool
	}{
		{name: "orbitjob_table_owner", login: false},
		{name: "orbitjob_migrator", login: true},
		{name: "orbitjob_admin", login: true},
		{name: "orbitjob_runtime", login: true},
		{name: "orbitjob_operator", login: false},
		{name: "orbitjob_owner", login: false},
		{name: "orbitjob_reader", login: false},
	} {
		var login, superuser, bypassRLS, createRole, inherit bool
		if err := db.QueryRowContext(ctx, `SELECT rolcanlogin, rolsuper, rolbypassrls, rolcreaterole, rolinherit FROM pg_roles WHERE rolname=$1`, role.name).
			Scan(&login, &superuser, &bypassRLS, &createRole, &inherit); err != nil {
			t.Fatalf("query role %s: %v", role.name, err)
		}
		if login != role.login || superuser || bypassRLS || createRole || inherit {
			t.Errorf("role %s login=%v super=%v bypass=%v create=%v inherit=%v", role.name, login, superuser, bypassRLS, createRole, inherit)
		}
	}

	// The owner is a NOLOGIN role, and only the migrator may assume it. That
	// membership is the one thing standing between the migrator and a blanket
	// RLS bypass, so it is asserted rather than assumed.
	var migratorMember bool
	if err := db.QueryRowContext(ctx, `SELECT pg_has_role('orbitjob_migrator','orbitjob_table_owner','MEMBER')`).Scan(&migratorMember); err != nil {
		t.Fatalf("query membership: %v", err)
	}
	if !migratorMember {
		t.Error("orbitjob_migrator is not a member of orbitjob_table_owner")
	}
	for _, role := range []string{"orbitjob_admin", "orbitjob_runtime", "orbitjob_operator"} {
		var member bool
		if err := db.QueryRowContext(ctx, `SELECT pg_has_role($1,'orbitjob_table_owner','MEMBER')`, role).Scan(&member); err != nil {
			t.Fatalf("query membership for %s: %v", role, err)
		}
		if member {
			t.Errorf("%s can assume orbitjob_table_owner and bypass row level security", role)
		}
	}

	var unexpected int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM pg_class c
		JOIN pg_namespace n ON n.oid=c.relnamespace
		JOIN pg_roles r ON r.oid=c.relowner
		WHERE n.nspname='public' AND c.relkind IN ('r','p','S')
		  AND r.rolname <> 'orbitjob_table_owner'
	`).Scan(&unexpected); err != nil {
		t.Fatalf("query ownership: %v", err)
	}
	if unexpected != 0 {
		t.Fatalf("public objects not owned by orbitjob_table_owner = %d", unexpected)
	}
}

func TestSecurityDefinerFunctionMatrix(t *testing.T) {
	applyAllMigrations(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := ownerDB(t)

	// Each SECURITY DEFINER function runs as the table owner and can see rows
	// the caller's RLS policy would hide, so the grant is the whole control.
	for _, tt := range []struct {
		signature string
		role      string
		allowed   bool
	}{
		{"public.orbitjob_auth_api_key(text)", "orbitjob_admin", true},
		{"public.orbitjob_auth_api_key(text)", "orbitjob_runtime", false},
		{"public.orbitjob_list_active_tenant_ids()", "orbitjob_runtime", true},
		{"public.orbitjob_list_active_tenant_ids()", "orbitjob_admin", false},
		{"public.orbitjob_bootstrap_default(text,text,text,text,text,text,text)", "orbitjob_admin", true},
		{"public.orbitjob_bootstrap_default(text,text,text,text,text,text,text)", "orbitjob_runtime", false},
		{"public.orbitjob_find_key_tenant(text)", "orbitjob_admin", true},
		{"public.orbitjob_find_key_tenant(text)", "orbitjob_runtime", false},
	} {
		var allowed bool
		if err := db.QueryRowContext(ctx, `SELECT has_function_privilege($1,$2,'EXECUTE')`, tt.role, tt.signature).Scan(&allowed); err != nil {
			t.Fatalf("query %s %s: %v", tt.role, tt.signature, err)
		}
		if allowed != tt.allowed {
			t.Errorf("%s execute %s = %v, want %v", tt.role, tt.signature, allowed, tt.allowed)
		}
	}

	rows, err := db.QueryContext(ctx, `
		SELECT p.proname, r.rolname, p.prosecdef, coalesce(array_to_string(p.proconfig, ','), '')
		FROM pg_proc p
		JOIN pg_namespace n ON n.oid=p.pronamespace
		JOIN pg_roles r ON r.oid=p.proowner
		WHERE n.nspname='public'
		  AND p.proname IN ('orbitjob_auth_api_key','orbitjob_list_active_tenant_ids','orbitjob_bootstrap_default','orbitjob_find_key_tenant')
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	count := 0
	for rows.Next() {
		var name, owner, config string
		var securityDefiner bool
		if err := rows.Scan(&name, &owner, &securityDefiner, &config); err != nil {
			t.Fatal(err)
		}
		count++
		if owner != "orbitjob_table_owner" || !securityDefiner || !strings.Contains(config, "search_path=pg_catalog, public") {
			t.Errorf("%s owner=%s security=%v config=%s", name, owner, securityDefiner, config)
		}
	}
	if count != 4 {
		t.Fatalf("restricted function count=%d", count)
	}

	// None of the four may be executable by PUBLIC. A SECURITY DEFINER function
	// reachable by PUBLIC is a cross-tenant read for anyone who can connect.
	var publicExecute int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM pg_proc p
		JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE n.nspname = 'public'
		  AND p.proname IN ('orbitjob_auth_api_key','orbitjob_list_active_tenant_ids','orbitjob_bootstrap_default','orbitjob_find_key_tenant')
		  AND EXISTS (
		    SELECT 1 FROM unnest(coalesce(p.proacl, ARRAY[]::aclitem[])) AS acl
		    WHERE acl::text LIKE '=X/%'
		  )
	`).Scan(&publicExecute); err != nil {
		t.Fatalf("query public execute: %v", err)
	}
	if publicExecute != 0 {
		t.Fatalf("PUBLIC execute grants found = %d", publicExecute)
	}
}
