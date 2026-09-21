//go:build integration

package migrations

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	pq "github.com/lib/pq"
)

// These tests pin the tenant_id unification decision (G3b): every tenant_id
// column is CHAR(26), every tenant-owned row references tenants(id), and the
// delete action splits the schema into the two families the baseline documents
// -- configuration cascades, history restricts. Baseline references are to
// 0001_baseline.up.sql as of 2026-09-18; line numbers drift when the file is
// edited, the quoted contract does not.

// tenantColumns lists every column in the schema that holds a tenant identity.
// tenants.id is the referenced side; the rest are the fourteen tenant_id
// columns (resource_groups, policies, api_keys, audit_events,
// job_definition_revisions, job_run_control_plane,
// job_run_attempts_control_plane, checks, check_runs, slis, slos,
// sli_snapshots, budgets, budget_alerts).
const tenantColumns = `
	SELECT c.relname, a.attname
	FROM pg_attribute a
	JOIN pg_class c ON c.oid = a.attrelid
	JOIN pg_namespace n ON n.oid = c.relnamespace
	JOIN pg_type t ON t.oid = a.atttypid
	WHERE n.nspname = 'public'
	  AND c.relkind IN ('r', 'p')
	  AND a.attnum > 0 AND NOT a.attisdropped
	  AND (
	    (a.attname = 'tenant_id')
	    OR (c.relname = 'tenants' AND a.attname = 'id')
	  )
`

// uncharTenantColumns returns the tenant identity columns that are not
// CHAR(26). CHAR(26) is bpchar with atttypmod = 26 + VARHDRSZ = 30; verified
// against a live CHAR(26) column before this test was written.
func uncharTenantColumns(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) ([]string, error) {
	rows, err := q.QueryContext(ctx, tenantColumns+`
	  AND NOT (t.typname = 'bpchar' AND a.atttypmod = 30)
	  ORDER BY c.relname, a.attname
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var rel, col string
		if err := rows.Scan(&rel, &col); err != nil {
			return nil, err
		}
		out = append(out, rel+"."+col)
	}
	return out, rows.Err()
}

// TestEveryTenantIdentityColumnIsChar26 pins the type side of G3b. Before the
// unification the column was CHAR(26) on three tables and VARCHAR(64) on the
// rest, so a join needed a cast and a foreign key could not span the two
// spellings. The set is derived from the catalog, so the fourteenth column and
// every column after it is covered the moment it exists.
func TestEveryTenantIdentityColumnIsChar26(t *testing.T) {
	applyAllMigrations(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := ownerDB(t)

	var total int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM (`+tenantColumns+`) s`).Scan(&total); err != nil {
		t.Fatalf("count tenant identity columns: %v", err)
	}
	// Seventeen base tables carry tenant_id (the baseline's fourteen, plus
	// functions and function_runs from migration 0004 and
	// workflow_run_control_plane from migration 0005); the audit_events_default
	// partition carries the eighteenth occurrence as its own catalog attribute,
	// so a partition whose column drifted from its parent's type is caught here
	// too. tenants.id is the nineteenth. If the count drops, a table lost its
	// tenant column entirely; if it grows, the new column is under test
	// automatically.
	if total != 19 {
		t.Fatalf("tenant identity columns = %d, want 19 (17 base tables + the audit_events_default partition + tenants.id)", total)
	}

	offenders, err := uncharTenantColumns(ctx, db)
	if err != nil {
		t.Fatalf("query offender columns: %v", err)
	}
	if len(offenders) != 0 {
		t.Fatalf("tenant identity columns that are not CHAR(26): %v", offenders)
	}
}

// TestChar26ProbeIsFlagged proves the type query can fail. A relation whose
// tenant_id is VARCHAR(64) -- the pre-unification spelling -- must be named by
// the same query. The probe table lives inside one transaction and is rolled
// back; no DDL survives the test.
func TestChar26ProbeIsFlagged(t *testing.T) {
	applyAllMigrations(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := ownerDB(t)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE public.char26_probe (
			id bigint, tenant_id varchar(64) NOT NULL
		)`); err != nil {
		t.Fatalf("create probe: %v", err)
	}

	offenders, err := uncharTenantColumns(ctx, tx)
	if err != nil {
		t.Fatalf("query offenders under mutation: %v", err)
	}
	found := false
	for _, o := range offenders {
		if strings.HasPrefix(o, "char26_probe.") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the type query accepted a VARCHAR(64) tenant_id column; "+
			"it cannot detect the pre-unification spelling (offenders = %v)", offenders)
	}
}

// TestTenantForeignKeyDeleteActionMatrix pins the referential action of every
// tenant foreign key, from pg_constraint.confdeltype: 'c' = CASCADE, 'r' =
// RESTRICT, 'a' = NO ACTION. Configuration dies with the tenant; history
// outlives it until explicitly purged. api_keys is pinned as NOT CASCADE:
// credentials must not vanish because the tenant row went away. The baseline
// documents the two families at the audit_events and checks table comments.
func TestTenantForeignKeyDeleteActionMatrix(t *testing.T) {
	applyAllMigrations(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := ownerDB(t)

	want := map[string]string{
		"resource_groups":                "c",
		"policies":                       "c",
		"checks":                         "c",
		"slis":                           "c",
		"slos":                           "c",
		"budgets":                        "c",
		"audit_events":                   "r",
		"job_definition_revisions":       "r",
		"job_run_control_plane":          "r",
		"job_run_attempts_control_plane": "r",
		"check_runs":                     "r",
		"sli_snapshots":                  "r",
		"budget_alerts":                  "r",
	}
	for table, action := range want {
		var got string
		err := db.QueryRowContext(ctx, `
			SELECT confdeltype::text
			FROM pg_constraint
			WHERE contype = 'f'
			  AND conrelid = ('public.'||$1)::regclass
			  AND confrelid = 'public.tenants'::regclass`, table).Scan(&got)
		if err != nil {
			t.Fatalf("query %s delete action: %v", table, err)
		}
		if got != action {
			t.Errorf("%s.tenant_id ON DELETE %s, want %s", table, describeAction(got), describeAction(action))
		}
	}

	// api_keys is in neither documented family. It must not cascade (a
	// credential may not disappear silently), which covers both NO ACTION and
	// RESTRICT; the exact spelling is an owner choice this test does not pin.
	var keysAction string
	if err := db.QueryRowContext(ctx, `
		SELECT confdeltype::text
		FROM pg_constraint
		WHERE contype = 'f'
		  AND conrelid = 'public.api_keys'::regclass
		  AND confrelid = 'public.tenants'::regclass`).Scan(&keysAction); err != nil {
		t.Fatalf("query api_keys delete action: %v", err)
	}
	if keysAction == "c" {
		t.Error("api_keys.tenant_id ON DELETE CASCADE: deleting a tenant destroys its credentials")
	}
}

func describeAction(code string) string {
	switch code {
	case "c":
		return "CASCADE"
	case "r":
		return "RESTRICT"
	case "a":
		return "NO ACTION"
	default:
		return code
	}
}

// TestTenantDeleteCascadesConfigurationFamily is the live half of the
// configuration contract: deleting a tenant removes its resource group,
// tenant policy, check, SLI, SLO and budget in the same statement.
// budget_alerts is deliberately absent: its tenant foreign key RESTRICTs (it
// is history, per the baseline's family comment) and the first draft of this
// test that assumed otherwise failed red on 2026-09-18 with
// budget_alerts_tenant_id_fkey -- the restriction is real. Everything runs as
// the owner inside one transaction that is rolled back, so no row and no
// tenant survives the test.
func TestTenantDeleteCascadesConfigurationFamily(t *testing.T) {
	applyAllMigrations(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := ownerDB(t)

	const alpha = "aaaaaaaaaaaaaaaaaaaaaaaaaa"
	seed := `
		INSERT INTO tenants(id, slug, name) VALUES ('` + alpha + `', 'config-family', 'Config Family');
		INSERT INTO resource_groups(id, tenant_id, slug, name)
		  VALUES (repeat('g', 26), '` + alpha + `', 'grp', 'Group');
		INSERT INTO policies(id, tenant_id, name, document)
		  VALUES (repeat('p', 26), '` + alpha + `', 'tenant-policy', '{}');
		INSERT INTO checks(name, tenant_id, check_type, schedule_type, cron_expr)
		  VALUES ('config-family-check', '` + alpha + `', 'http_health', 'cron', '* * * * *');
		WITH sli AS (
			INSERT INTO slis(tenant_id, name, sli_type)
			  VALUES ('` + alpha + `', 'sli', 'ratio') RETURNING id
		), slo AS (
			INSERT INTO slos(tenant_id, name, sli_id, target)
			  SELECT '` + alpha + `', 'slo', id, 0.999 FROM sli RETURNING id
		)
		INSERT INTO budgets(tenant_id, slo_id, window_start, window_end, budget_total, budget_remaining)
		  SELECT '` + alpha + `', id, now(), now() + interval '30 days', 1, 1 FROM slo;
	`
	if _, err := db.ExecContext(ctx, seed); err != nil {
		t.Fatalf("seed configuration family: %v", err)
	}

	if _, err := db.ExecContext(ctx,
		`DELETE FROM tenants WHERE id = '`+alpha+`'`); err != nil {
		t.Fatalf("deleting a tenant was blocked by the configuration family: %v", err)
	}

	for _, table := range []string{
		"resource_groups", "policies", "checks", "slis", "slos", "budgets",
	} {
		var n int
		if err := db.QueryRowContext(ctx,
			`SELECT count(*) FROM public.`+table+` WHERE tenant_id = '`+alpha+`'`).Scan(&n); err != nil {
			t.Fatalf("count %s after tenant delete: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s kept %d row(s) after its tenant was deleted; CASCADE did not reach it", table, n)
		}
	}
}

// TestTenantDeleteIsBlockedByHistoryFamily is the live half of the history
// contract: a tenant carrying audit rows, ledger rows, check runs, SLI
// snapshots or budget alerts cannot be deleted until the history is explicitly
// purged. That explicitness is the point of an audit product.
func TestTenantDeleteIsBlockedByHistoryFamily(t *testing.T) {
	applyAllMigrations(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := ownerDB(t)

	const alpha = "aaaaaaaaaaaaaaaaaaaaaaaaaa"
	seed := `
		INSERT INTO tenants(id, slug, name) VALUES ('` + alpha + `', 'history-family', 'History Family');
		INSERT INTO audit_events(tenant_id, actor_type, event_type, resource_type, resource_id)
		  VALUES ('` + alpha + `', 'system', 'create', 'tenant', 'seed');
		INSERT INTO job_definition_revisions
		  (tenant_id, source_mode, source_uid, source_namespace, source_name,
		   generation, spec_hash, normalized_spec, actor)
		  VALUES ('` + alpha + `', 'k8s', 'uid-history', 'ns', 'name', 1, repeat('h', 64), '{}', 'operator');
		INSERT INTO checks(name, tenant_id, check_type, schedule_type, cron_expr)
		  VALUES ('history-family-check', '` + alpha + `', 'http_health', 'cron', '* * * * *');
		INSERT INTO check_runs(tenant_id, check_id, scheduled_at)
		  SELECT '` + alpha + `', id, now() FROM checks WHERE tenant_id = '` + alpha + `';
		WITH sli AS (
			INSERT INTO slis(tenant_id, name, sli_type)
			  VALUES ('` + alpha + `', 'sli', 'ratio') RETURNING id
		), slo AS (
			INSERT INTO slos(tenant_id, name, sli_id, target)
			  SELECT '` + alpha + `', 'slo', id, 0.999 FROM sli RETURNING id
		), snap AS (
			INSERT INTO sli_snapshots(tenant_id, sli_id, window_start, window_end)
			  SELECT '` + alpha + `', id, now(), now() + interval '1 hour' FROM sli RETURNING id, sli_id
		), bud AS (
			INSERT INTO budgets(tenant_id, slo_id, window_start, window_end, budget_total, budget_remaining)
			  SELECT '` + alpha + `', id, now(), now() + interval '30 days', 1, 1 FROM slo RETURNING id
		)
		INSERT INTO budget_alerts(tenant_id, slo_id, budget_id, alert_type, burn_rate)
		  SELECT '` + alpha + `', sn.sli_id, b.id, 'burn_rate', 1 FROM snap sn, bud b;
	`
	if _, err := db.ExecContext(ctx, seed); err != nil {
		t.Fatalf("seed history family: %v", err)
	}

	// The ledger rows need the revision to exist first, and the attempt needs
	// the run; they are separate statements so the ids can be carried over.
	var revisionID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO job_run_control_plane
		  (tenant_id, source_uid, revision_id, occurrence_key, trigger, actor, phase)
		SELECT r.tenant_id, r.source_uid, r.id, repeat('k', 64), 'manual', 'operator', 'Pending'
		FROM job_definition_revisions r WHERE r.tenant_id = '`+alpha+`'
		RETURNING id`).Scan(&revisionID); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO job_run_attempts_control_plane
		  (tenant_id, run_id, attempt_number, phase, kubernetes_job_name)
		VALUES ('`+alpha+`', $1, 1, 'Pending', 'history-family-attempt')`, revisionID); err != nil {
		t.Fatalf("seed attempt: %v", err)
	}

	_, err := db.ExecContext(ctx, `DELETE FROM tenants WHERE id = '`+alpha+`'`)
	if err == nil {
		t.Fatal("a tenant carrying history was deleted; the history family no longer RESTRICTs")
	}
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) || string(pqErr.Code) != "23503" {
		t.Fatalf("tenant delete failed for the wrong reason (want foreign_key_violation 23503): %v", err)
	}

	var stillThere int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM tenants WHERE id = '`+alpha+`'`).Scan(&stillThere); err != nil {
		t.Fatal(err)
	}
	if stillThere != 1 {
		t.Fatalf("tenant rows after the failed delete = %d, want 1", stillThere)
	}
}

// tenantFKConstraintName returns the name of the tenant foreign key on table.
// Every tenant-owned table has exactly one foreign key to tenants, so the
// lookup cannot be ambiguous today; if a second one appears the error says so
// rather than guessing.
func tenantFKConstraintName(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, table string) (string, error) {
	var name string
	err := q.QueryRowContext(ctx, `
		SELECT conname FROM pg_constraint
		WHERE contype = 'f'
		  AND conrelid = ('public.'||$1)::regclass
		  AND confrelid = 'public.tenants'::regclass`, table).Scan(&name)
	return name, err
}

// TestRestrictIsLoadBearing proves the history contract comes from the
// constraint and not from something incidental. Inside one rolled-back
// transaction the audit_events foreign key is replaced with ON DELETE CASCADE;
// the delete that TestTenantDeleteIsBlockedByHistoryFamily relies on must now
// succeed and take the audit rows with it. If that mutation changes nothing,
// the RESTRICT test was passing for the wrong reason. No DDL survives: the
// transaction is rolled back.
func TestRestrictIsLoadBearing(t *testing.T) {
	applyAllMigrations(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := ownerDB(t)

	const alpha = "aaaaaaaaaaaaaaaaaaaaaaaaaa"
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	constraint, err := tenantFKConstraintName(ctx, tx, "audit_events")
	if err != nil {
		t.Fatalf("find audit_events tenant constraint: %v", err)
	}
	if _, err := tx.ExecContext(ctx,
		`ALTER TABLE public.audit_events DROP CONSTRAINT `+constraint); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx,
		`ALTER TABLE public.audit_events ADD CONSTRAINT `+constraint+
			` FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE`); err != nil {
		t.Fatal(err)
	}

	seed := `
		INSERT INTO tenants(id, slug, name) VALUES ('` + alpha + `', 'restrict-proof', 'Restrict Proof');
		INSERT INTO audit_events(tenant_id, actor_type, event_type, resource_type, resource_id)
		  VALUES ('` + alpha + `', 'system', 'create', 'tenant', 'proof');
	`
	if _, err := tx.ExecContext(ctx, seed); err != nil {
		t.Fatalf("seed under mutation: %v", err)
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM tenants WHERE id = '`+alpha+`'`); err != nil {
		t.Fatalf("with the constraint replaced by CASCADE the delete must succeed: %v", err)
	}
	var remaining int
	if err := tx.QueryRowContext(ctx,
		`SELECT count(*) FROM audit_events WHERE tenant_id = '`+alpha+`'`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("audit rows survived a cascaded tenant delete = %d, want 0", remaining)
	}
}

// TestCascadeIsLoadBearing is the mirror proof for the configuration family:
// with resource_groups' foreign key replaced by RESTRICT, the delete that
// TestTenantDeleteCascadesConfigurationFamily relies on must now be blocked.
// Rolled back like its sibling.
func TestCascadeIsLoadBearing(t *testing.T) {
	applyAllMigrations(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := ownerDB(t)

	const alpha = "aaaaaaaaaaaaaaaaaaaaaaaaaa"
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	constraint, err := tenantFKConstraintName(ctx, tx, "resource_groups")
	if err != nil {
		t.Fatalf("find resource_groups tenant constraint: %v", err)
	}
	if _, err := tx.ExecContext(ctx,
		`ALTER TABLE public.resource_groups DROP CONSTRAINT `+constraint); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx,
		`ALTER TABLE public.resource_groups ADD CONSTRAINT `+constraint+
			` FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE RESTRICT`); err != nil {
		t.Fatal(err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO tenants(id, slug, name) VALUES ('`+alpha+`', 'cascade-proof', 'Cascade Proof');
		INSERT INTO resource_groups(id, tenant_id, slug, name)
		  VALUES (repeat('g', 26), '`+alpha+`', 'grp', 'Group');
	`); err != nil {
		t.Fatalf("seed under mutation: %v", err)
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM tenants WHERE id = '`+alpha+`'`); err == nil {
		t.Fatal("with the constraint replaced by RESTRICT the delete must be blocked")
	}
}
