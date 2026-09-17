//go:build integration

package migrations

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"testing"
)

// The slos tenancy contract. The broad read matrix in
// tenant_visibility_integration_test.go already proves for every tenant table
// -- slos included -- that reads are filtered and that no tenant context fails
// closed; this file adds what that matrix deliberately does not carry:
//
//   - the catalog shape of slos_tenant itself, so a policy that stops binding
//     app.tenant_id (the historical slos defect this guards against) is a red
//     catalog assertion and not a silent behavior drift;
//   - the slos write path under RLS: a tenant cannot INSERT or UPDATE its way
//     into another tenant's SLO, and a same-row version conflict still
//     classifies as a conflict rather than as a missing row;
//   - the load-bearing proof that the filtering above comes from the policy:
//     with slos_tenant replaced by a permissive mutant inside a rolled-back
//     transaction, the tenant's reads see the other tenant's rows.
//
// The mutation is transactional DDL, rolled back before the test ends; the
// same pattern as TestTenantScopingIsLoadBearing.

func TestSLOSPolicyBindsTenantGUC(t *testing.T) {
	applyAllMigrations(t)
	ctx := ctx30(t)
	db := ownerDB(t)

	var policyName, cmd, permissive, qual, withCheck string
	var roles []byte
	err := db.QueryRowContext(ctx, `
		SELECT policyname, cmd, permissive, qual::text, with_check::text, roles::text
		FROM pg_policies
		WHERE schemaname = 'public' AND tablename = 'slos'
	`).Scan(&policyName, &cmd, &permissive, &qual, &withCheck, &roles)
	if err == sql.ErrNoRows {
		t.Fatal("slos has no row-level security policy; the table is either wide open to " +
			"rls-exempt roles or denies every rls-bound role, depending on the grant set")
	}
	if err != nil {
		t.Fatalf("read slos policy from the catalog: %v", err)
	}

	if policyName != "slos_tenant" {
		t.Errorf("slos policy = %q, want slos_tenant", policyName)
	}
	if cmd != "ALL" {
		t.Errorf("slos policy cmd = %q, want ALL (one policy for reads and writes)", cmd)
	}
	if permissive != "PERMISSIVE" {
		t.Errorf("slos policy permissive = %q, want PERMISSIVE", permissive)
	}
	for _, role := range []string{"orbitjob_admin", "orbitjob_runtime", "orbitjob_operator"} {
		if !strings.Contains(string(roles), role) {
			t.Errorf("slos policy roles %s do not include %s", roles, role)
		}
	}
	// The exact defect this file exists to catch: a policy whose expressions
	// do not read the tenant GUC. USING filters reads, WITH CHECK bounds
	// writes; if the two ever diverge, reads and writes disagree about whom a
	// row belongs to.
	if qual != withCheck {
		t.Errorf("slos policy USING and WITH CHECK diverge:\n  USING: %s\n  WITH CHECK: %s", qual, withCheck)
	}
	if !bindsTenantGUC(qual) {
		t.Errorf("slos policy USING does not bind app.tenant_id: %s", qual)
	}
	if !bindsTenantGUC(withCheck) {
		t.Errorf("slos policy WITH CHECK does not bind app.tenant_id: %s", withCheck)
	}
}

// bindsTenantGUC reports whether a policy expression reads app.tenant_id
// through current_setting's missing-ok form, the shape every tenant policy in
// the baseline shares.
func bindsTenantGUC(expr string) bool {
	return strings.Contains(expr, "current_setting") &&
		strings.Contains(expr, "app.tenant_id") &&
		strings.Contains(expr, "true")
}

func TestSLOSWritesAreTenantIsolated(t *testing.T) {
	applyAllMigrations(t)
	ctx := ctx30(t)
	owner := ownerDB(t)
	seedVisibilityFixtures(t, ctx, owner, "a", visAlphaID)
	seedVisibilityFixtures(t, ctx, owner, "b", visBetaID)

	// Beta's ids, read as the owner: under RLS alpha cannot see them, so the
	// write attempts below would pass vacuously if they guessed.
	var betaSloID, betaSliID int64
	if err := owner.QueryRowContext(ctx,
		`SELECT id FROM slos WHERE tenant_id = '`+visBetaID+`'`).Scan(&betaSloID); err != nil {
		t.Fatalf("read beta slo as owner: %v", err)
	}
	if err := owner.QueryRowContext(ctx,
		`SELECT id FROM slis WHERE tenant_id = '`+visBetaID+`'`).Scan(&betaSliID); err != nil {
		t.Fatalf("read beta sli as owner: %v", err)
	}

	withRoleConn(t, "orbitjob_admin", func(ctx context.Context, conn *sql.Conn) {
		if _, err := conn.ExecContext(ctx, `SET app.tenant_id = '`+visAlphaID+`'`); err != nil {
			t.Fatalf("set tenant context: %v", err)
		}

		if got := countAs(t, ctx, conn, `SELECT count(*) FROM slos WHERE tenant_id = '`+visAlphaID+`'`); got != 1 {
			t.Fatalf("alpha sees %d own slo row(s), want 1; the positive control failed "+
				"so the isolation assertions below prove nothing", got)
		}
		if got := countAs(t, ctx, conn, `SELECT count(*) FROM slos`); got != 1 {
			t.Fatalf("alpha sees %d slo row(s) in total, want 1; the difference is another tenant's data", got)
		}

		// A cross-tenant INSERT violates WITH CHECK even though the referenced
		// sli is beta's and would satisfy the foreign key.
		if _, err := conn.ExecContext(ctx, `
			INSERT INTO slos (tenant_id, name, sli_id, target)
			VALUES ('`+visBetaID+`', 'slo-hijack', `+strconv.FormatInt(betaSliID, 10)+`, 0.999)`); err == nil {
			t.Error("alpha inserted an slo row for beta; WITH CHECK did not apply")
		}

		// Cross-tenant UPDATE by tenant predicate and by primary key: both must
		// affect zero rows, invisibly rather than by denial.
		if _, err := conn.ExecContext(ctx,
			`UPDATE slos SET name = 'hijacked' WHERE tenant_id = '`+visBetaID+`'`); err != nil {
			t.Errorf("cross-tenant update by tenant_id errored instead of filtering: %v", err)
		}
		if _, err := conn.ExecContext(ctx,
			`UPDATE slos SET name = 'hijacked' WHERE id = `+strconv.FormatInt(betaSloID, 10)); err != nil {
			t.Errorf("cross-tenant update by id errored instead of filtering: %v", err)
		}
		if n, err := countOwnerSloName(ctx, owner, betaSloID); err != nil || n != 1 {
			t.Errorf("beta's slo row is missing or was modified through alpha's session "+
				"(rows still carrying the seeded name: %d): %v", n, err)
		}

		// Fail closed: no tenant context, no rows.
		if _, err := conn.ExecContext(ctx, `RESET app.tenant_id`); err != nil {
			t.Fatalf("reset tenant context: %v", err)
		}
		if got := countAs(t, ctx, conn, `SELECT count(*) FROM slos`); got != 0 {
			t.Errorf("no tenant context sees %d slo row(s), want 0; RLS did not fail closed", got)
		}
	})

	// Load-bearing: inside one rolled-back transaction, replace slos_tenant
	// with a permissive mutant and observe alpha reading beta's row. If the
	// assertions above were satisfied by anything other than the policy, this
	// substitution changes nothing and the file proves nothing.
	tx, err := owner.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DROP POLICY slos_tenant ON public.slos`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE POLICY slos_open ON public.slos
		  FOR ALL TO orbitjob_admin, orbitjob_runtime, orbitjob_operator
		  USING (true) WITH CHECK (true)`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx,
		`SET ROLE orbitjob_admin; SET app.tenant_id = '`+visAlphaID+`'`); err != nil {
		t.Fatalf("assume admin under mutation: %v", err)
	}
	var betaRows int
	if err := tx.QueryRowContext(ctx,
		`SELECT count(*) FROM slos WHERE tenant_id = '`+visBetaID+`'`).Scan(&betaRows); err != nil {
		t.Fatal(err)
	}
	if betaRows != 1 {
		t.Fatalf("with a permissive slos policy alpha sees %d of beta's slo row(s), want 1; "+
			"the tenant predicate is not what filters slos reads", betaRows)
	}
}

// countOwnerSloName checks, as the owner, whether beta's slo still carries its
// seeded name; the alpha session cannot see the row, so only the owner can.
func countOwnerSloName(ctx context.Context, owner *sql.DB, sloID int64) (int, error) {
	var n int
	err := owner.QueryRowContext(ctx,
		`SELECT count(*) FROM slos WHERE id = $1 AND name = 'slo-b'`, sloID).Scan(&n)
	return n, err
}
