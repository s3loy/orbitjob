//go:build integration

package migrations

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

// These tests connect as the deployment roles and observe the schema's effect on
// real rows. The catalog tests say RLS is enabled; these say what it does.

func ctx30(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// seedTenantIDs creates tenants rows for the given ids. Since the tenant_id
// unification every tenant-owned row references tenants(id), so a fixture
// seeding 'alpha' or 'beta' rows needs the tenant to exist first or the
// foreign key rejects the seed.
func seedTenantIDs(t *testing.T, ctx context.Context, owner *sql.DB, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if _, err := owner.ExecContext(ctx, `
			INSERT INTO tenants(id, slug, name)
			VALUES ($1::char(26), 'iso-'||$1::text, 'Isolation '||$1::text)
			ON CONFLICT (id) DO NOTHING
		`, id); err != nil {
			t.Fatalf("seed tenant %s: %v", id, err)
		}
	}
}

// seedAuditRows writes three rows for tenant alpha and five for tenant beta
// through the owner connection, which bypasses RLS.
func seedAuditRows(t *testing.T, ctx context.Context, owner *sql.DB) {
	t.Helper()
	seedTenantIDs(t, ctx, owner, "alpha", "beta")
	if _, err := owner.ExecContext(ctx, `
		INSERT INTO audit_events(tenant_id,actor_type,event_type,resource_type,resource_id)
		SELECT 'alpha','system','create','tenant','a'||g FROM generate_series(1,3) g;
		INSERT INTO audit_events(tenant_id,actor_type,event_type,resource_type,resource_id)
		SELECT 'beta','system','create','tenant','b'||g FROM generate_series(1,5) g;
	`); err != nil {
		t.Fatalf("seed audit rows: %v", err)
	}
}

func countAs(t *testing.T, ctx context.Context, conn *sql.Conn, query string) int {
	t.Helper()
	var n int
	if err := conn.QueryRowContext(ctx, query).Scan(&n); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return n
}

// TestTenantIsolationThroughParentAndPartition is the headline property. The
// historical defect let a tenant read every tenant's audit rows by naming
// audit_events_default directly, even though the parent returned nothing. The
// partition carries its own RLS and its own policy, so both paths must agree.
func TestTenantIsolationThroughParentAndPartition(t *testing.T) {
	applyAllMigrations(t)
	ctx := ctx30(t)
	owner := ownerDB(t)
	seedAuditRows(t, ctx, owner)

	// Positive control: without a tenant identity all eight rows exist. If this
	// is wrong the isolation checks below prove nothing.
	var total int
	if err := owner.QueryRowContext(ctx, `SELECT count(*) FROM audit_events`).Scan(&total); err != nil {
		t.Fatalf("count as owner: %v", err)
	}
	if total != 8 {
		t.Fatalf("seeded rows = %d, want 8", total)
	}

	withRoleConn(t, "orbitjob_admin", func(ctx context.Context, conn *sql.Conn) {
		if _, err := conn.ExecContext(ctx, `SET app.tenant_id = 'alpha'`); err != nil {
			t.Fatalf("set tenant context: %v", err)
		}

		if n := countAs(t, ctx, conn, `SELECT count(*) FROM audit_events`); n != 3 {
			t.Errorf("alpha sees %d rows through the parent, want 3", n)
		}
		if n := countAs(t, ctx, conn, `SELECT count(*) FROM audit_events_default`); n != 3 {
			t.Errorf("alpha sees %d rows through the partition, want 3", n)
		}
		if n := countAs(t, ctx, conn, `SELECT count(*) FROM audit_events WHERE tenant_id = 'beta'`); n != 0 {
			t.Errorf("alpha reads %d of beta's rows through the parent, want 0", n)
		}
		if n := countAs(t, ctx, conn, `SELECT count(*) FROM audit_events_default WHERE tenant_id = 'beta'`); n != 0 {
			t.Errorf("alpha reads %d of beta's rows through the partition, want 0", n)
		}

		// With no tenant context at all the setting is NULL, every comparison is
		// NULL, and nothing is visible. Fail closed, not open.
		if _, err := conn.ExecContext(ctx, `RESET app.tenant_id`); err != nil {
			t.Fatalf("reset tenant context: %v", err)
		}
		if n := countAs(t, ctx, conn, `SELECT count(*) FROM audit_events`); n != 0 {
			t.Errorf("no tenant context sees %d rows through the parent, want 0", n)
		}
		if n := countAs(t, ctx, conn, `SELECT count(*) FROM audit_events_default`); n != 0 {
			t.Errorf("no tenant context sees %d rows through the partition, want 0", n)
		}
	})
}

// TestCrossTenantWriteIsRejected covers WITH CHECK. A policy that filters reads
// but not writes lets a tenant insert rows other tenants will read.
func TestCrossTenantWriteIsRejected(t *testing.T) {
	applyAllMigrations(t)
	seedTenantIDs(t, ctx30(t), ownerDB(t), "alpha", "beta")

	withRoleConn(t, "orbitjob_admin", func(ctx context.Context, conn *sql.Conn) {
		if _, err := conn.ExecContext(ctx, `SET app.tenant_id = 'alpha'`); err != nil {
			t.Fatalf("set tenant context: %v", err)
		}

		// Positive control: writing into the caller's own tenant succeeds.
		if _, err := conn.ExecContext(ctx, `
			INSERT INTO checks (name, tenant_id, check_type, schedule_type, cron_expr)
			VALUES ('own', 'alpha', 'http_health', 'cron', '* * * * *')
		`); err != nil {
			t.Fatalf("alpha could not write its own row: %v", err)
		}

		if _, err := conn.ExecContext(ctx, `
			INSERT INTO checks (name, tenant_id, check_type, schedule_type, cron_expr)
			VALUES ('forged', 'beta', 'http_health', 'cron', '* * * * *')
		`); err == nil {
			t.Error("alpha wrote a row for beta; WITH CHECK did not apply")
		}

		// A write with no tenant context must also be refused, not silently
		// attributed to whatever the session last held.
		if _, err := conn.ExecContext(ctx, `RESET app.tenant_id`); err != nil {
			t.Fatalf("reset tenant context: %v", err)
		}
		if _, err := conn.ExecContext(ctx, `
			INSERT INTO checks (name, tenant_id, check_type, schedule_type, cron_expr)
			VALUES ('unattributed', 'alpha', 'http_health', 'cron', '* * * * *')
		`); err == nil {
			t.Error("a write with no tenant context was accepted")
		}
	})
}

// TestPlatformPresetsAreReadOnlyAcrossTenants: tenant_id IS NULL marks a preset
// every tenant may bind. The platform-read policy is SELECT-only, and the tenant
// policy cannot match a NULL tenant, so no tenant may edit or delete a preset.
func TestPlatformPresetsAreReadOnlyAcrossTenants(t *testing.T) {
	applyAllMigrations(t)

	withRoleConn(t, "orbitjob_admin", func(ctx context.Context, conn *sql.Conn) {
		if _, err := conn.ExecContext(ctx, `SET app.tenant_id = 'alpha'`); err != nil {
			t.Fatalf("set tenant context: %v", err)
		}

		if n := countAs(t, ctx, conn, `SELECT count(*) FROM policies WHERE tenant_id IS NULL`); n != 3 {
			t.Fatalf("alpha reads %d platform presets, want 3", n)
		}

		res, err := conn.ExecContext(ctx, `UPDATE policies SET name = 'HACKED' WHERE tenant_id IS NULL`)
		if err != nil {
			t.Fatalf("update presets: %v", err)
		}
		if n, _ := res.RowsAffected(); n != 0 {
			t.Errorf("alpha updated %d platform presets, want 0", n)
		}

		res, err = conn.ExecContext(ctx, `DELETE FROM policies WHERE tenant_id IS NULL`)
		if err != nil {
			t.Fatalf("delete presets: %v", err)
		}
		if n, _ := res.RowsAffected(); n != 0 {
			t.Errorf("alpha deleted %d platform presets, want 0", n)
		}
	})
}

// TestLedgerWritePrivilegeMatrix is property 4 at the grant level, with positive
// controls so a database with no grants at all could not pass it. The run ledger
// is written only by the operator; the identity that serves HTTP reads it.
func TestLedgerWritePrivilegeMatrix(t *testing.T) {
	applyAllMigrations(t)
	ctx := ctx30(t)
	db := ownerDB(t)

	ledger := []string{
		"job_definition_revisions", "job_run_control_plane", "job_run_attempts_control_plane",
	}
	for _, table := range ledger {
		for _, priv := range []string{"INSERT", "UPDATE", "DELETE", "TRUNCATE"} {
			var allowed bool
			if err := db.QueryRowContext(ctx, `SELECT has_table_privilege('orbitjob_admin', 'public.'||$1, $2)`, table, priv).
				Scan(&allowed); err != nil {
				t.Fatalf("query %s %s: %v", table, priv, err)
			}
			if allowed {
				t.Errorf("orbitjob_admin holds %s on ledger table %s", priv, table)
			}
		}
		var canSelect bool
		if err := db.QueryRowContext(ctx, `SELECT has_table_privilege('orbitjob_admin', 'public.'||$1, 'SELECT')`, table).
			Scan(&canSelect); err != nil {
			t.Fatalf("query %s SELECT: %v", table, err)
		}
		if !canSelect {
			t.Errorf("orbitjob_admin cannot SELECT ledger table %s", table)
		}
	}

	// Positive controls: the checks that would otherwise be satisfied by a
	// schema with no privileges anywhere.
	var adminWritesChecks, runtimeWritesLedger, operatorWritesLedger bool
	if err := db.QueryRowContext(ctx, `SELECT has_table_privilege('orbitjob_admin','public.checks','INSERT')`).Scan(&adminWritesChecks); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT has_table_privilege('orbitjob_runtime','public.job_run_control_plane','INSERT')`).Scan(&runtimeWritesLedger); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT has_table_privilege('orbitjob_operator','public.job_run_control_plane','INSERT')`).Scan(&operatorWritesLedger); err != nil {
		t.Fatal(err)
	}
	if !adminWritesChecks {
		t.Error("orbitjob_admin cannot INSERT its own table checks; the ledger denials are meaningless")
	}
	if !runtimeWritesLedger {
		t.Error("orbitjob_runtime cannot INSERT the ledger; the ledger is unwritable by its writer")
	}
	if !operatorWritesLedger {
		t.Error("orbitjob_operator cannot INSERT the ledger")
	}
}

// TestAdminCannotWriteRunLedger proves the privilege gap is real at the point of
// use, not only in the catalog.
func TestAdminCannotWriteRunLedger(t *testing.T) {
	applyAllMigrations(t)

	withRoleConn(t, "orbitjob_admin", func(ctx context.Context, conn *sql.Conn) {
		if _, err := conn.ExecContext(ctx, `SET app.tenant_id = 'alpha'`); err != nil {
			t.Fatalf("set tenant context: %v", err)
		}
		// The read the API is allowed to do must work, so the insert failure is
		// about the write grant and not about a missing schema object.
		if n := countAs(t, ctx, conn, `SELECT count(*) FROM job_run_control_plane`); n != 0 {
			t.Fatalf("ledger rows = %d, want 0", n)
		}
		_, err := conn.ExecContext(ctx, `
			INSERT INTO job_run_control_plane
			  (tenant_id, source_uid, revision_id, occurrence_key, trigger, actor, phase)
			VALUES ('alpha', 'u1', 1, repeat('x',64), 'manual', 'someone', 'Pending')
		`)
		if err == nil {
			t.Fatal("orbitjob_admin inserted a run row; the ledger is writable by the HTTP identity")
		}
		if !strings.Contains(strings.ToLower(err.Error()), "permission denied") {
			t.Fatalf("insert failed for the wrong reason: %v", err)
		}
	})
}

// TestRunActorIsRequiredAndNonEmpty is property 5 on the run table: the column
// the ledger uses to answer "who triggered this" cannot be omitted or blanked.
func TestRunActorIsRequiredAndNonEmpty(t *testing.T) {
	applyAllMigrations(t)
	ctx := ctx30(t)
	db := ownerDB(t)
	seedTenantIDs(t, ctx, db, "alpha")

	var revisionID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO job_definition_revisions
		  (tenant_id, source_mode, source_uid, source_namespace, source_name,
		   generation, spec_hash, normalized_spec, actor)
		VALUES ('alpha','k8s','uid-1','ns','name',1,repeat('a',64),'{}','operator')
		RETURNING id
	`).Scan(&revisionID); err != nil {
		t.Fatalf("seed revision: %v", err)
	}

	// Positive control: a run with a real actor is accepted.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO job_run_control_plane
		  (tenant_id, source_uid, revision_id, occurrence_key, trigger, actor, phase)
		VALUES ('alpha','uid-1',$1,repeat('o',64),'manual','scheduler@cluster','Pending')
	`, revisionID); err != nil {
		t.Fatalf("a run with a valid actor was rejected: %v", err)
	}

	for _, actor := range []string{"''", "'   '"} {
		_, err := db.ExecContext(ctx, `
			INSERT INTO job_run_control_plane
			  (tenant_id, source_uid, revision_id, occurrence_key, trigger, actor, phase)
			VALUES ('alpha','uid-1',$1,repeat('z',64),'manual',`+actor+`,'Pending')
		`, revisionID)
		if err == nil {
			t.Errorf("job_run_control_plane accepted actor=%s", actor)
		}
	}
}

// TestRevisionActorIsRequiredAndNonEmpty is property 5 on the revision table.
//
// This test used to be the red contract for schema-strategy.md G1; the baseline
// has since landed the fix -- `actor VARCHAR(255) NOT NULL` with
// `CHECK (btrim(actor) <> ”)` -- so the test is green and now guards the rule
// instead of demanding it.
func TestRevisionActorIsRequiredAndNonEmpty(t *testing.T) {
	applyAllMigrations(t)
	ctx := ctx30(t)
	db := ownerDB(t)
	seedTenantIDs(t, ctx, db, "alpha")

	// Positive control: a revision with a real actor is accepted.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO job_definition_revisions
		  (tenant_id, source_mode, source_uid, source_namespace, source_name,
		   generation, spec_hash, normalized_spec, actor)
		VALUES ('alpha','k8s','uid-ok','ns','name',1,repeat('a',64),'{}','operator')
	`); err != nil {
		t.Fatalf("a revision with a valid actor was rejected: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO job_definition_revisions
		  (tenant_id, source_mode, source_uid, source_namespace, source_name,
		   generation, spec_hash, normalized_spec, actor)
		VALUES ('alpha','k8s','uid-empty','ns','name',1,repeat('e',64),'{}','')
	`); err == nil {
		t.Error("job_definition_revisions accepted an empty actor; `actor` is NOT NULL with a non-empty CHECK on both ledger tables (schema-strategy.md G1)")
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO job_definition_revisions
		  (tenant_id, source_mode, source_uid, source_namespace, source_name,
		   generation, spec_hash, normalized_spec)
		VALUES ('alpha','k8s','uid-omitted','ns','name',1,repeat('o',64),'{}')
	`); err == nil {
		t.Error("job_definition_revisions accepted a row with actor omitted; DEFAULT '' silently stores an empty actor (schema-strategy.md G1)")
	}
}

// TestResourceGroupScopingColumnIsUniform is the DB-layer half of the group
// scoping contract. A key can be scoped to a resource group, and every resource
// the key can list or trigger must carry the column that scope filters on.
//
// This test used to be the red contract for schema-strategy.md G2; migration
// 0003 has since landed the fix -- job_definition_revisions and
// job_run_control_plane now carry a nullable resource_group_id, stamped from
// the check row through the revision -- so the test is green and now guards
// the rule instead of demanding it. The admin API's refusal of group-scoped
// keys on job/run surfaces (resource.RequireUnscoped) is unchanged.
func TestResourceGroupScopingColumnIsUniform(t *testing.T) {
	applyAllMigrations(t)
	ctx := ctx30(t)
	db := ownerDB(t)

	// Relations a group-scoped key can reach. job_run_attempts_control_plane is
	// scoped through its run and is intentionally absent.
	missing := names(t, ctx, db, `
		SELECT required.name
		FROM unnest(ARRAY[
			'api_keys','checks','slis','slos',
			'job_definition_revisions','job_run_control_plane'
		]) AS required(name)
		WHERE NOT EXISTS (
			SELECT 1 FROM pg_attribute a
			JOIN pg_class c ON c.oid = a.attrelid
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = 'public' AND c.relname = required.name
			  AND a.attname = 'resource_group_id'
			  AND a.attnum > 0 AND NOT a.attisdropped
		)
		ORDER BY required.name
	`)
	if len(missing) != 0 {
		t.Fatalf("relations a group-scoped API key can reach with no resource_group_id column: %v\n"+
			"a key scoped to one group lists and triggers resources in every group; "+
			"add the column and filter on it, or refuse group-scoped keys on these resources", missing)
	}
}
