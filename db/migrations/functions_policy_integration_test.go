//go:build integration

package migrations

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	pq "github.com/lib/pq"
)

// The functions tenancy and family contracts, migration 0004. functions is
// configuration (tenant_id CASCADE, admin DML); function_runs is ledger-adjacent
// history (tenant_id RESTRICT, the runtime/operator roles write it and the admin
// reads). The catalog tests already know both tables carry RLS and CHAR(26)
// tenant columns; these tests observe what the schema does to real rows through
// the deployment roles.
//
// Writes run as the roles the grants actually allow: cross-tenant INSERT and
// UPDATE on functions as orbitjob_admin, on function_runs as orbitjob_runtime,
// the one writer the D1 rule gives the derived read model. A grant-denied role
// would prove the grant, not the policy.

func seedFunctions(t *testing.T, ctx context.Context, owner *sql.DB, tenantID string, names ...string) int64 {
	t.Helper()
	var firstID int64
	for i, name := range names {
		var id int64
		if err := owner.QueryRowContext(ctx, `
			INSERT INTO functions (name, tenant_id, image)
			VALUES ($1, $2::char(26), $3)
			RETURNING id
		`, name, tenantID, "registry.example/"+name+":1").Scan(&id); err != nil {
			t.Fatalf("seed function %s for %s: %v", name, tenantID, err)
		}
		if i == 0 {
			firstID = id
		}
	}
	return firstID
}

// seedFunctionRuns writes n terminal runs for one function, as the owner. A
// success row needs started_at and finished_at with finished_at >= started_at;
// identical timestamps satisfy the comparison.
func seedFunctionRuns(t *testing.T, ctx context.Context, owner *sql.DB, tenantID string, functionID int64, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := owner.ExecContext(ctx, `
			INSERT INTO function_runs (tenant_id, function_id, status, triggered_at, started_at, finished_at)
			VALUES ($1::char(26), $2, 'success', now(), now(), now())
		`, tenantID, functionID); err != nil {
			t.Fatalf("seed function run for %s: %v", tenantID, err)
		}
	}
}

// TestFunctionsAreTenantIsolated is the functions half of the isolation
// contract: reads filter by the tenant GUC, writes are bounded by WITH CHECK,
// and no tenant context fails closed. functions is the admin-writable side of
// 0004, so the whole matrix runs as orbitjob_admin.
func TestFunctionsAreTenantIsolated(t *testing.T) {
	applyAllMigrations(t)
	ctx := ctx30(t)
	owner := ownerDB(t)
	seedTenantIDs(t, ctx, owner, "alpha", "beta")
	seedFunctions(t, ctx, owner, "alpha", "fn-a1", "fn-a2")
	betaFn := seedFunctions(t, ctx, owner, "beta", "fn-b1", "fn-b2", "fn-b3")

	// Positive control: without a tenant identity all five rows exist. If this
	// is wrong the isolation checks below prove nothing.
	var total int
	if err := owner.QueryRowContext(ctx, `SELECT count(*) FROM functions`).Scan(&total); err != nil {
		t.Fatalf("count as owner: %v", err)
	}
	if total != 5 {
		t.Fatalf("seeded functions = %d, want 5", total)
	}

	withRoleConn(t, "orbitjob_admin", func(ctx context.Context, conn *sql.Conn) {
		if _, err := conn.ExecContext(ctx, `SET app.tenant_id = 'alpha'`); err != nil {
			t.Fatalf("set tenant context: %v", err)
		}

		if n := countAs(t, ctx, conn, `SELECT count(*) FROM functions`); n != 2 {
			t.Fatalf("alpha sees %d function row(s), want 2; the positive control failed "+
				"so the isolation assertions below prove nothing", n)
		}
		if n := countAs(t, ctx, conn, `SELECT count(*) FROM functions WHERE tenant_id = 'beta'`); n != 0 {
			t.Errorf("alpha reads %d of beta's function rows, want 0", n)
		}

		// Positive control: updating its own row succeeds.
		res, err := conn.ExecContext(ctx, `
			UPDATE functions SET name = 'fn-a1-renamed' WHERE tenant_id = 'alpha' AND name = 'fn-a1'
		`)
		if err != nil {
			t.Fatalf("alpha could not update its own function: %v", err)
		}
		if n, _ := res.RowsAffected(); n != 1 {
			t.Errorf("own-tenant update touched %d row(s), want 1", n)
		}

		// A cross-tenant INSERT violates WITH CHECK.
		if _, err := conn.ExecContext(ctx, `
			INSERT INTO functions (name, tenant_id, image)
			VALUES ('fn-hijack', 'beta', 'registry.example/hijack:1')
		`); err == nil {
			t.Error("alpha inserted a function for beta; WITH CHECK did not apply")
		}

		// Cross-tenant UPDATE by tenant predicate and by primary key: both must
		// affect zero rows, invisibly rather than by denial.
		res, err = conn.ExecContext(ctx, `UPDATE functions SET name = 'hijacked' WHERE tenant_id = 'beta'`)
		if err != nil {
			t.Errorf("cross-tenant update by tenant_id errored instead of filtering: %v", err)
		} else if n, _ := res.RowsAffected(); n != 0 {
			t.Errorf("cross-tenant update by tenant_id touched %d row(s), want 0", n)
		}
		res, err = conn.ExecContext(ctx, `UPDATE functions SET name = 'hijacked' WHERE id = $1`, betaFn)
		if err != nil {
			t.Errorf("cross-tenant update by id errored instead of filtering: %v", err)
		} else if n, _ := res.RowsAffected(); n != 0 {
			t.Errorf("cross-tenant update by id touched %d row(s), want 0", n)
		}

		// The row the session could not touch is intact; only the owner can see it.
		var name string
		if err := owner.QueryRowContext(ctx,
			`SELECT name FROM functions WHERE id = $1`, betaFn).Scan(&name); err != nil {
			t.Fatalf("read beta's function as owner: %v", err)
		}
		if name != "fn-b1" {
			t.Errorf("beta's function was renamed through alpha's session to %q", name)
		}

		// Fail closed: no tenant context, no rows.
		if _, err := conn.ExecContext(ctx, `RESET app.tenant_id`); err != nil {
			t.Fatalf("reset tenant context: %v", err)
		}
		if n := countAs(t, ctx, conn, `SELECT count(*) FROM functions`); n != 0 {
			t.Errorf("no tenant context sees %d function row(s), want 0; RLS did not fail closed", n)
		}
	})
}

// TestFunctionRunsAreTenantIsolated is the read-model half of the 0004
// isolation contract. The admin only ever reads function_runs, so its matrix is
// reads plus fail-closed; the write matrix runs as orbitjob_runtime, the role
// the grants make the writer.
func TestFunctionRunsAreTenantIsolated(t *testing.T) {
	applyAllMigrations(t)
	ctx := ctx30(t)
	owner := ownerDB(t)
	seedTenantIDs(t, ctx, owner, "alpha", "beta")
	alphaFn := seedFunctions(t, ctx, owner, "alpha", "fn-runs-a")
	betaFn := seedFunctions(t, ctx, owner, "beta", "fn-runs-b")
	seedFunctionRuns(t, ctx, owner, "alpha", alphaFn, 2)
	seedFunctionRuns(t, ctx, owner, "beta", betaFn, 3)

	withRoleConn(t, "orbitjob_admin", func(ctx context.Context, conn *sql.Conn) {
		if _, err := conn.ExecContext(ctx, `SET app.tenant_id = 'alpha'`); err != nil {
			t.Fatalf("set tenant context: %v", err)
		}
		if n := countAs(t, ctx, conn, `SELECT count(*) FROM function_runs`); n != 2 {
			t.Fatalf("alpha sees %d function_run row(s), want 2", n)
		}
		if n := countAs(t, ctx, conn, `SELECT count(*) FROM function_runs WHERE tenant_id = 'beta'`); n != 0 {
			t.Errorf("alpha reads %d of beta's function runs, want 0", n)
		}
		if _, err := conn.ExecContext(ctx, `RESET app.tenant_id`); err != nil {
			t.Fatalf("reset tenant context: %v", err)
		}
		if n := countAs(t, ctx, conn, `SELECT count(*) FROM function_runs`); n != 0 {
			t.Errorf("no tenant context sees %d function_run row(s), want 0; RLS did not fail closed", n)
		}
	})

	withRoleConn(t, "orbitjob_runtime", func(ctx context.Context, conn *sql.Conn) {
		if _, err := conn.ExecContext(ctx, `SET app.tenant_id = 'alpha'`); err != nil {
			t.Fatalf("set tenant context: %v", err)
		}

		// Positive control: the writer records a terminal run for its own tenant.
		if _, err := conn.ExecContext(ctx, `
			INSERT INTO function_runs (tenant_id, function_id, status, triggered_at, started_at, finished_at)
			VALUES ('alpha', $1, 'success', now(), now(), now())
		`, alphaFn); err != nil {
			t.Fatalf("runtime could not record alpha's own function run: %v", err)
		}

		// The function referenced is beta's, so the composite foreign key is
		// satisfied and WITH CHECK is what rejects the row.
		if _, err := conn.ExecContext(ctx, `
			INSERT INTO function_runs (tenant_id, function_id, status, triggered_at, started_at, finished_at)
			VALUES ('beta', $1, 'success', now(), now(), now())
		`, betaFn); err == nil {
			t.Error("runtime recorded a function run for beta under alpha's context; WITH CHECK did not apply")
		}

		res, err := conn.ExecContext(ctx,
			`UPDATE function_runs SET duration_ms = 0 WHERE tenant_id = 'beta'`)
		if err != nil {
			t.Errorf("cross-tenant update errored instead of filtering: %v", err)
		} else if n, _ := res.RowsAffected(); n != 0 {
			t.Errorf("cross-tenant update touched %d row(s), want 0", n)
		}

		if _, err := conn.ExecContext(ctx, `RESET app.tenant_id`); err != nil {
			t.Fatalf("reset tenant context: %v", err)
		}
		if _, err := conn.ExecContext(ctx, `
			INSERT INTO function_runs (tenant_id, function_id, status, triggered_at, started_at, finished_at)
			VALUES ('alpha', $1, 'success', now(), now(), now())
		`, alphaFn); err == nil {
			t.Error("a function run was recorded with no tenant context")
		}
	})
}

// TestFunctionsCascadeButRunsRestrictTenantDelete pins the family placement of
// both 0004 tables with live deletes: functions is configuration and dies with
// the tenant; function_runs is history and blocks the delete until it is
// explicitly purged. The catalog prefix pins the referential actions the live
// half relies on, including workflow_run_control_plane so the whole 0004/0005
// tenant foreign key set is stated in one place.
func TestFunctionsCascadeButRunsRestrictTenantDelete(t *testing.T) {
	applyAllMigrations(t)
	ctx := ctx30(t)
	db := ownerDB(t)

	want := map[string]string{
		"functions":                  "c",
		"function_runs":              "r",
		"workflow_run_control_plane": "r",
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

	// Direction one: a tenant whose only 0004 rows are functions deletes clean,
	// and the definition rows go with it.
	seedTenantIDs(t, ctx, db, "famcfg")
	cfgFn := seedFunctions(t, ctx, db, "famcfg", "fn-cascade")
	if _, err := db.ExecContext(ctx, `DELETE FROM tenants WHERE id = 'famcfg'`); err != nil {
		t.Fatalf("deleting a tenant with only configuration was blocked: %v", err)
	}
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM functions WHERE id = $1`, cfgFn).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("functions kept %d row(s) after their tenant was deleted; CASCADE did not reach it", n)
	}

	// Direction two: a tenant with invocation history cannot be deleted. The
	// failure is the function_runs foreign key, and the tenant survives.
	seedTenantIDs(t, ctx, db, "famhist")
	histFn := seedFunctions(t, ctx, db, "famhist", "fn-restrict")
	seedFunctionRuns(t, ctx, db, "famhist", histFn, 1)

	_, err := db.ExecContext(ctx, `DELETE FROM tenants WHERE id = 'famhist'`)
	if err == nil {
		t.Fatal("a tenant carrying function runs was deleted; the read model no longer RESTRICTs")
	}
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) || string(pqErr.Code) != "23503" {
		t.Fatalf("tenant delete failed for the wrong reason (want foreign_key_violation 23503): %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM tenants WHERE id = 'famhist'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("tenant rows after the failed delete = %d, want 1", n)
	}

	// Direction three: once the history is explicitly purged the delete
	// succeeds, configuration still cascading behind it.
	if _, err := db.ExecContext(ctx, `DELETE FROM function_runs WHERE tenant_id = 'famhist'`); err != nil {
		t.Fatalf("purge function runs: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM tenants WHERE id = 'famhist'`); err != nil {
		t.Fatalf("tenant delete after its history was purged: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM functions WHERE tenant_id = 'famhist'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("functions kept %d row(s) after the purged tenant was deleted", n)
	}
}
