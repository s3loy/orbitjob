//go:build integration

package migrations

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestV020TenantTablesHaveForcedRLS(t *testing.T) {
	applyV020Baseline(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := openMigrationDB(t, testOwnerDSN(t))

	expectedTables := []string{
		"tenants", "api_keys", "jobs", "job_instances", "job_instance_attempts", "workers",
		"audit_events", "job_change_audits", "checks", "check_runs", "slis", "slos",
		"sli_snapshots", "budgets", "budget_alerts",
	}
	for _, table := range expectedTables {
		var enabled bool
		var policies int
		err := db.QueryRowContext(ctx, `
			SELECT c.relrowsecurity, count(p.policyname)
			FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			LEFT JOIN pg_policies p ON p.schemaname = n.nspname AND p.tablename = c.relname
			WHERE n.nspname = 'public' AND c.relname = $1
			GROUP BY c.relrowsecurity
		`, table).Scan(&enabled, &policies)
		if err != nil {
			t.Fatalf("query %s: %v", table, err)
		}
		if !enabled || policies == 0 {
			t.Errorf("%s enabled=%v policies=%d", table, enabled, policies)
		}
	}
}

func TestV020RoleAttributesAndOwnership(t *testing.T) {
	applyV020Baseline(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := openMigrationDB(t, testOwnerDSN(t))

	for _, role := range []struct {
		name  string
		login bool
	}{
		{name: "orbitjob_table_owner", login: false},
		{name: "orbitjob_migrator", login: true},
		{name: "orbitjob_admin", login: true},
		{name: "orbitjob_runtime", login: true},
		{name: "orbitjob_operator", login: false},
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

	var unexpected int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM pg_class c
		JOIN pg_namespace n ON n.oid=c.relnamespace
		JOIN pg_roles r ON r.oid=c.relowner
		WHERE n.nspname='public' AND c.relkind IN ('r','p','S') AND r.rolname <> 'orbitjob_table_owner'
	`).Scan(&unexpected); err != nil {
		t.Fatalf("query ownership: %v", err)
	}
	if unexpected != 0 {
		t.Fatalf("unexpected public object owners = %d", unexpected)
	}
}

func TestV020SecurityDefinerFunctionMatrix(t *testing.T) {
	applyV020Baseline(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := openMigrationDB(t, testOwnerDSN(t))

	for _, tt := range []struct {
		signature string
		role      string
		allowed   bool
	}{
		{"public.orbitjob_auth_api_key(text)", "orbitjob_admin", true},
		{"public.orbitjob_auth_api_key(text)", "orbitjob_runtime", false},
		{"public.orbitjob_list_active_tenant_ids()", "orbitjob_runtime", true},
		{"public.orbitjob_bootstrap_default(text,text,text,text,text,text,text,jsonb)", "orbitjob_admin", true},
		{"public.orbitjob_bootstrap_default(text,text,text,text,text,text,text,jsonb)", "orbitjob_runtime", false},
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
		WHERE n.nspname='public' AND p.proname IN ('orbitjob_auth_api_key','orbitjob_list_active_tenant_ids','orbitjob_bootstrap_default')
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
	if count != 3 {
		t.Fatalf("restricted function count=%d", count)
	}

	var publicExecute int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM pg_proc p
		JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE n.nspname = 'public'
		  AND p.proname IN ('orbitjob_auth_api_key','orbitjob_list_active_tenant_ids','orbitjob_bootstrap_default')
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
