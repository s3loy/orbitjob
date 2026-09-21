//go:build integration

package postgrestest

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
)

// These tests prove the D1 grant matrix (schema-strategy.md) at the point of
// use for the tables the 0002-0005 migrations introduced, plus one
// pre-existing ledger table as a control:
//
//	0004: functions, function_runs
//	0005: workflow_run_control_plane
//	0001: job_run_control_plane (control; no new table in 0002 or 0003)
//
// The catalog tests in db/migrations say the grants exist; these say what they
// do to a real statement. orbitjob_admin must take SQLSTATE 42501
// "permission denied for table" -- the grant-level denial, not the RLS
// denial, and never a silent zero-row no-op -- on every ledger table, and the
// runtime identity (the operator's login role) must be able to write the
// tables internal/core/store/postgres actually writes.

// writeDenialTenantID is the scratch tenant every probe runs under. 26
// characters, matching tenants.id CHAR(26), in the fixture style of
// helperTenantID.
const writeDenialTenantID = "30000000000000000000000001"

// adminLedgerWriteDenials probes the tables where the migrations give
// orbitjob_admin SELECT and nothing else. job_run_control_plane is the
// pre-existing control: if the baseline's denial ever regressed, the new
// tables' proofs would mean nothing.
func TestAdminLedgerWriteDenials(t *testing.T) {
	tables := []struct {
		name    string
		migrate string
	}{
		{"function_runs", "0004"},
		{"workflow_run_control_plane", "0005"},
		{"job_run_control_plane", "0001 (control)"},
	}

	for _, table := range tables {
		t.Run(table.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			db := Open(t)
			seedWriteDenialFixtures(t, ctx, db)

			conn := roleProbeConn(t, "orbitjob_admin")
			setProbeTenant(t, ctx, conn)

			// Positive control: the role reads the seeded rows, so the denials
			// below are about the write grant and not about a missing table or
			// an RLS filter that hides the rows.
			var rows int
			if err := conn.QueryRowContext(ctx,
				`SELECT count(*) FROM `+table.name+` WHERE tenant_id = $1`, writeDenialTenantID,
			).Scan(&rows); err != nil {
				t.Fatalf("select %s: %v", table.name, err)
			}
			if rows < 1 {
				t.Fatalf("orbitjob_admin sees %d rows in %s, want at least 1; the denials would prove nothing", rows, table.name)
			}

			// The statements mirror the store's write shapes so the denial is
			// proven against the grammar the write path really issues.
			switch table.name {
			case "function_runs": // recordCompletedFunctionRunSQL
				requireGrantDenied(t, ctx, conn, "orbitjob_admin", table.name, "INSERT", `
					INSERT INTO function_runs
					  (run_id, tenant_id, function_id, status, triggered_at, started_at, finished_at, duration_ms)
					VALUES (gen_random_uuid(), $1,
					        (SELECT id FROM functions WHERE tenant_id = $1 LIMIT 1),
					        'success', now(), now(), now(), 5)`, writeDenialTenantID)
				requireGrantDenied(t, ctx, conn, "orbitjob_admin", table.name, "UPDATE", `
					UPDATE function_runs SET duration_ms = duration_ms + 1 WHERE tenant_id = $1`, writeDenialTenantID)
				requireGrantDenied(t, ctx, conn, "orbitjob_admin", table.name, "DELETE", `
					DELETE FROM function_runs WHERE tenant_id = $1`, writeDenialTenantID)
			case "workflow_run_control_plane": // insertWorkflowRunSQL
				requireGrantDenied(t, ctx, conn, "orbitjob_admin", table.name, "INSERT", `
					INSERT INTO workflow_run_control_plane
					  (tenant_id, source_uid, revision_id, occurrence_key, trigger, actor, phase)
					VALUES ($1, 'admin-denial-probe',
					        (SELECT id FROM job_definition_revisions WHERE tenant_id = $1 LIMIT 1),
					        repeat('a', 64), 'Manual', 'admin-probe', 'Pending')`, writeDenialTenantID)
				requireGrantDenied(t, ctx, conn, "orbitjob_admin", table.name, "UPDATE", `
					UPDATE workflow_run_control_plane SET phase = 'Running' WHERE tenant_id = $1`, writeDenialTenantID)
				requireGrantDenied(t, ctx, conn, "orbitjob_admin", table.name, "DELETE", `
					DELETE FROM workflow_run_control_plane WHERE tenant_id = $1`, writeDenialTenantID)
			case "job_run_control_plane": // control_plane_repository.go CreateOccurrence
				requireGrantDenied(t, ctx, conn, "orbitjob_admin", table.name, "INSERT", `
					INSERT INTO job_run_control_plane
					  (tenant_id, source_uid, revision_id, occurrence_key, trigger, actor, phase)
					VALUES ($1, 'admin-denial-probe',
					        (SELECT id FROM job_definition_revisions WHERE tenant_id = $1 LIMIT 1),
					        repeat('j', 64), 'manual', 'admin-probe', 'Pending')`, writeDenialTenantID)
				requireGrantDenied(t, ctx, conn, "orbitjob_admin", table.name, "UPDATE", `
					UPDATE job_run_control_plane SET phase = 'Running' WHERE tenant_id = $1`, writeDenialTenantID)
				requireGrantDenied(t, ctx, conn, "orbitjob_admin", table.name, "DELETE", `
					DELETE FROM job_run_control_plane WHERE tenant_id = $1`, writeDenialTenantID)
			}
		})
	}
}

// TestAdminKeepsConfigurationWritesOnFunctions is the positive half of 0004's
// matrix: functions is configuration the admin API owns end to end, so the
// ledger denials above must not read as "the admin role cannot write at all".
func TestAdminKeepsConfigurationWritesOnFunctions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := Open(t)
	seedWriteDenialFixtures(t, ctx, db)

	conn := roleProbeConn(t, "orbitjob_admin")
	setProbeTenant(t, ctx, conn)

	if _, err := conn.ExecContext(ctx, `
		INSERT INTO functions (tenant_id, name, image)
		VALUES ($1, 'admin-write-probe', 'write-denial:latest')`, writeDenialTenantID,
	); err != nil {
		t.Fatalf("orbitjob_admin could not INSERT its own functions row: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `
		UPDATE functions SET name = 'admin-write-probe-2'
		WHERE tenant_id = $1 AND name = 'admin-write-probe'`, writeDenialTenantID,
	); err != nil {
		t.Fatalf("orbitjob_admin could not UPDATE its own functions row: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `
		DELETE FROM functions
		WHERE tenant_id = $1 AND name = 'admin-write-probe-2'`, writeDenialTenantID,
	); err != nil {
		t.Fatalf("orbitjob_admin could not DELETE its own functions row: %v", err)
	}
}

// TestRuntimeWritesTheRunLedgerTables proves the write half of the matrix with
// the store's statement shapes: the operator's login identity (orbitjob_runtime)
// records runs, workflow rows and terminal function invocations, reads the
// function definitions its revision-sync loop needs, and nothing more.
func TestRuntimeWritesTheRunLedgerTables(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := Open(t)
	fx := seedWriteDenialFixtures(t, ctx, db)

	conn := roleProbeConn(t, "orbitjob_runtime")
	setProbeTenant(t, ctx, conn)

	// control_plane_repository.go CreateOccurrence. The occurrence keys differ
	// from the seeded rows' so the UNIQUE (source_uid, occurrence_key) anchor
	// is the probe's own, not a dedup race with the fixture.
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO job_run_control_plane
		  (tenant_id, source_uid, revision_id, occurrence_key, trigger, actor, phase)
		VALUES ($1, $2, $3, repeat('u', 64), 'manual', 'runtime-probe', 'Pending')`,
		writeDenialTenantID, fx.runSourceUID, fx.revisionID,
	); err != nil {
		t.Fatalf("orbitjob_runtime could not INSERT a job run: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `
		UPDATE job_run_control_plane SET phase = 'Running' WHERE tenant_id = $1 AND source_uid = $2`,
		writeDenialTenantID, fx.runSourceUID,
	); err != nil {
		t.Fatalf("orbitjob_runtime could not UPDATE the job run phase: %v", err)
	}

	// workflow_repository.go insertWorkflowRunSQL and the walker's phase merge
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO workflow_run_control_plane
		  (tenant_id, source_uid, revision_id, occurrence_key, trigger, actor, phase)
		VALUES ($1, $2, $3, repeat('v', 64), 'Schedule', 'walker', 'Pending')`,
		writeDenialTenantID, fx.workflowSourceUID, fx.revisionID,
	); err != nil {
		t.Fatalf("orbitjob_runtime could not INSERT a workflow run: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `
		UPDATE workflow_run_control_plane
		SET phase = 'Running', updated_at = now()
		WHERE tenant_id = $1 AND source_uid = $2`, writeDenialTenantID, fx.workflowSourceUID,
	); err != nil {
		t.Fatalf("orbitjob_runtime could not UPDATE the workflow phase: %v", err)
	}

	// function_repository.go recordCompletedFunctionRunSQL
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO function_runs
		  (run_id, tenant_id, function_id, status, triggered_at, started_at, finished_at, duration_ms)
		VALUES (gen_random_uuid(), $1, $2, 'success', now(), now(), now(), 5)`,
		writeDenialTenantID, fx.functionID,
	); err != nil {
		t.Fatalf("orbitjob_runtime could not INSERT a function run: %v", err)
	}

	// Positive control on the read side, then the denials: 0004 gives the
	// runtime role SELECT on functions and nothing else.
	var definitions int
	if err := conn.QueryRowContext(ctx,
		`SELECT count(*) FROM functions WHERE tenant_id = $1`, writeDenialTenantID,
	).Scan(&definitions); err != nil {
		t.Fatalf("orbitjob_runtime could not SELECT functions: %v", err)
	}
	requireGrantDenied(t, ctx, conn, "orbitjob_runtime", "functions", "INSERT", `
		INSERT INTO functions (tenant_id, name, image)
		VALUES ($1, 'runtime-write-probe', 'write-denial:latest')`, writeDenialTenantID)
	requireGrantDenied(t, ctx, conn, "orbitjob_runtime", "functions", "UPDATE", `
		UPDATE functions SET name = 'runtime-write-probe' WHERE tenant_id = $1`, writeDenialTenantID)
	requireGrantDenied(t, ctx, conn, "orbitjob_runtime", "functions", "DELETE", `
		DELETE FROM functions WHERE tenant_id = $1`, writeDenialTenantID)
}

// TestNoTableBeyondTheConfigurationSetGrantsAdminWrites is the guard that
// outlives this file's hand-written probes: it sweeps every table in the
// schema from the catalog and fails if orbitjob_admin holds any write
// privilege anywhere the migrations did not name one. A future migration that
// grants INSERT on a new ledger table to orbitjob_admin fails here without
// this test being edited.
func TestNoTableBeyondTheConfigurationSetGrantsAdminWrites(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := Open(t)

	rows, err := db.QueryContext(ctx, `
		SELECT DISTINCT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		CROSS JOIN LATERAL aclexplode(COALESCE(c.relacl, acldefault('r', c.relowner))) a
		JOIN pg_roles g ON g.oid = a.grantee
		WHERE n.nspname = current_schema()
		  AND c.relkind IN ('r', 'p')
		  AND c.relname <> 'schema_migrations'
		  AND g.rolname = 'orbitjob_admin'
		  AND a.privilege_type IN ('INSERT', 'UPDATE', 'DELETE', 'TRUNCATE')
		ORDER BY c.relname
	`)
	if err != nil {
		t.Fatalf("sweep admin table grants: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var observed []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan granted table: %v", err)
		}
		observed = append(observed, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("sweep admin table grants: %v", err)
	}

	// Exactly the tables the migrations grant orbitjob_admin a write on: the
	// baseline's configuration family, audit trail, and read models, plus
	// functions from 0004. Anything else -- and any grant that disappears from
	// these -- is a migration the review must see.
	expected := []string{
		"audit_events", "audit_events_default",
		"api_keys", "budget_alerts", "budgets", "check_runs", "checks",
		"functions", "key_policies", "policies", "resource_groups",
		"sli_snapshots", "slis", "slos", "tenants",
	}

	unexpected, missing := diffStringSets(expected, observed)
	if len(unexpected) > 0 || len(missing) > 0 {
		t.Fatalf("orbitjob_admin table-grant drift (schema %s):\n  writes on tables beyond the grant matrix: %v\n  matrix tables that lost every write grant: %v\nthe ledger's SELECT-only posture (D1) depends on the first list staying empty",
			currentSchemaNameForTest(t), unexpected, missing)
	}
}

// ---------------------------------------------------------------------------
// fixtures and probes
// ---------------------------------------------------------------------------

// writeDenialFixtures carries the seeded row identities the probes reference.
type writeDenialFixtures struct {
	revisionID        int64
	functionID        int64
	runSourceUID      string
	workflowSourceUID string
}

// seedWriteDenialFixtures seeds, on the owner connection, the tenant, one
// revision, one function definition, and one row in each probed table, so the
// denials and the runtime positives run against real, visible rows. The owner
// bypasses RLS; every probe below re-scopes with app.tenant_id.
func seedWriteDenialFixtures(t *testing.T, ctx context.Context, owner *sql.DB) writeDenialFixtures {
	t.Helper()

	if len(writeDenialTenantID) != 26 {
		t.Fatalf("writeDenialTenantID is %d characters, want 26", len(writeDenialTenantID))
	}

	if _, err := owner.ExecContext(ctx, `
		INSERT INTO tenants(id, slug, name)
		VALUES ($1, 'write-denial', 'Write denial scratch')
		ON CONFLICT (id) DO NOTHING
	`, writeDenialTenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	fx := writeDenialFixtures{
		runSourceUID:      "write-denial-run",
		workflowSourceUID: "write-denial-workflow",
	}
	if err := owner.QueryRowContext(ctx, `
		INSERT INTO job_definition_revisions
		  (tenant_id, source_mode, source_uid, source_namespace, source_name,
		   generation, spec_hash, normalized_spec, actor)
		VALUES ($1, 'k8s', $2, 'write-denial', 'write-denial', 1, repeat('a', 64), '{}', 'scratch')
		RETURNING id
	`, writeDenialTenantID, fx.runSourceUID).Scan(&fx.revisionID); err != nil {
		t.Fatalf("seed revision: %v", err)
	}
	if err := owner.QueryRowContext(ctx, `
		INSERT INTO functions (tenant_id, name, image)
		VALUES ($1, 'write-denial-fn', 'write-denial:latest')
		RETURNING id
	`, writeDenialTenantID).Scan(&fx.functionID); err != nil {
		t.Fatalf("seed function: %v", err)
	}

	if _, err := owner.ExecContext(ctx, `
		INSERT INTO job_run_control_plane
		  (tenant_id, source_uid, revision_id, occurrence_key, trigger, actor, phase)
		VALUES ($1, $2, $3, repeat('r', 64), 'manual', 'scratch', 'Pending')
	`, writeDenialTenantID, fx.runSourceUID, fx.revisionID); err != nil {
		t.Fatalf("seed job run: %v", err)
	}
	if _, err := owner.ExecContext(ctx, `
		INSERT INTO workflow_run_control_plane
		  (tenant_id, source_uid, revision_id, occurrence_key, trigger, actor, phase)
		VALUES ($1, $2, $3, repeat('w', 64), 'Schedule', 'walker', 'Pending')
	`, writeDenialTenantID, fx.workflowSourceUID, fx.revisionID); err != nil {
		t.Fatalf("seed workflow run: %v", err)
	}
	if _, err := owner.ExecContext(ctx, `
		INSERT INTO function_runs
		  (run_id, tenant_id, function_id, status, triggered_at, started_at, finished_at, duration_ms)
		VALUES (gen_random_uuid(), $1, $2, 'success', now(), now(), now(), 5)
	`, writeDenialTenantID, fx.functionID); err != nil {
		t.Fatalf("seed function run: %v", err)
	}

	return fx
}

// roleProbeConn opens a dedicated connection to this test's schema and puts it
// in the given deployment role. A pooled handle would leak the SET ROLE (and
// the tenant setting) across connections, so the probe owns one connection on
// a private handle for the length of the test and both are closed before the
// harness drops the schema.
func roleProbeConn(t *testing.T, role string) *sql.Conn {
	t.Helper()

	dsn, _, err := testDSN(DSN(t), t.Name())
	if err != nil {
		t.Fatalf("scope probe dsn: %v", redactDSN(err.Error()))
	}
	probeDB, err := open(dsn)
	if err != nil {
		t.Fatalf("open probe db as %s: %v", role, redactDSN(err.Error()))
	}
	t.Cleanup(func() { _ = probeDB.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := probeDB.Conn(ctx)
	if err != nil {
		t.Fatalf("open probe connection as %s: %v", role, err)
	}
	t.Cleanup(func() {
		// Hand the connection back without its role, so the pool it dies with
		// holds no identity a later statement could inherit.
		resetCtx, resetCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer resetCancel()
		_, _ = conn.ExecContext(resetCtx, "RESET ROLE")
		_ = conn.Close()
	})

	// role is a compile-time literal at every call site, never user input.
	if _, err := conn.ExecContext(ctx, "SET ROLE "+role); err != nil {
		t.Fatalf("SET ROLE %s: %v", role, err)
	}
	return conn
}

// setProbeTenant scopes the probe connection's RLS context to the scratch
// tenant, so a denial is the grant's and not the policy's.
func setProbeTenant(t *testing.T, ctx context.Context, conn *sql.Conn) {
	t.Helper()

	// writeDenialTenantID is a fixed literal; SET takes no parameters.
	if _, err := conn.ExecContext(ctx,
		`SET app.tenant_id = '`+writeDenialTenantID+`'`,
	); err != nil {
		t.Fatalf("set app.tenant_id: %v", err)
	}
}

// requireGrantDenied asserts the exact failure the grant matrix predicts: the
// statement fails with SQLSTATE 42501 and the table-level permission message.
// An RLS violation carries the same SQLSTATE but a different message ("new row
// violates row-level security policy"), and a missing grant on a sequence or
// another object names that object instead -- both are different defects and
// must not pass.
func requireGrantDenied(t *testing.T, ctx context.Context, conn *sql.Conn, role, table, action, query string, args ...any) {
	t.Helper()

	_, err := conn.ExecContext(ctx, query, args...)
	if err == nil {
		t.Fatalf("%s ran %s on %s; the write denial the admin-api's posture depends on is gone", role, action, table)
	}
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) {
		t.Fatalf("%s %s on %s failed without a PostgreSQL error: %v", action, table, role, err)
	}
	if pqErr.Code != "42501" {
		t.Fatalf("%s %s on %s failed with SQLSTATE %s (%s), want 42501", action, table, role, pqErr.Code, pqErr.Message)
	}
	if !strings.Contains(pqErr.Message, "permission denied for table "+table) {
		t.Fatalf("%s %s on %s was not the grant-level denial: %q", action, table, role, pqErr.Message)
	}
}

func diffStringSets(expected, observed []string) (unexpected, missing []string) {
	want := make(map[string]bool, len(expected))
	for _, name := range expected {
		want[name] = true
	}
	got := make(map[string]bool, len(observed))
	for _, name := range observed {
		got[name] = true
	}
	for name := range got {
		if !want[name] {
			unexpected = append(unexpected, name)
		}
	}
	for name := range want {
		if !got[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(unexpected)
	sort.Strings(missing)
	return unexpected, missing
}

func currentSchemaNameForTest(t *testing.T) string {
	t.Helper()

	_, schemaName, err := testDSN(DSN(t), t.Name())
	if err != nil {
		t.Fatalf("scope schema name: %v", redactDSN(err.Error()))
	}
	return schemaName
}
