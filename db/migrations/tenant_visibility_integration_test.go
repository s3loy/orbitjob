//go:build integration

package migrations

import (
	"context"
	"database/sql"
	"testing"
)

// Tenant scoping, observed on real rows in every tenant-owned relation. The
// isolation tests that predate this file proved the property deeply on one
// table (audit_events, parent and partition); this file proves it broadly: a
// row seeded for tenant beta through the owner connection is invisible to a
// connection speaking for tenant alpha in every table the baseline gives a
// tenant_id, including through the partition name.
//
// The owner connection is the superuser DSN, which RLS does not bind; that is
// what makes it able to seed both tenants. Every observation runs as
// orbitjob_admin on its own connection with app.tenant_id set.

const (
	visAlphaID = "aaaaaaaaaaaaaaaaaaaaaaaaaa"
	visBetaID  = "bbbbbbbbbbbbbbbbbbbbbbbbbb"

	// The preset key_policies binds to; the baseline seeds three platform
	// policies with fixed CHAR(26) ids.
	visAdminPolicyID = "00000000000000000000000010"
)

// seedVisibilityFixtures creates tenant alpha and tenant beta with one row in
// every tenant-owned relation, through the owner connection, committed. Ids
// are built from each tenant's letter so the two tenants never collide and
// every table's row is attributable at a glance.
func seedVisibilityFixtures(t *testing.T, ctx context.Context, db *sql.DB, ch, tenantID string) {
	t.Helper()
	seed := `
		INSERT INTO tenants(id, slug, name) VALUES ('` + tenantID + `', 'vis-` + ch + `', 'Tenant ` + ch + `');

		INSERT INTO resource_groups(id, tenant_id, slug, name)
		  VALUES (repeat('` + ch + `', 23)||'grp', '` + tenantID + `', 'grp-` + ch + `', 'Group ` + ch + `');

		INSERT INTO policies(id, tenant_id, name, document)
		  VALUES (repeat('` + ch + `', 23)||'pol', '` + tenantID + `', 'policy-` + ch + `', '{}');

		WITH k AS (
			INSERT INTO api_keys(id, tenant_id, kind, key_hash, key_prefix)
			  VALUES (repeat('` + ch + `', 23)||'key', '` + tenantID + `', 'tenant',
			          'hash-` + ch + `', '` + ch + `pref')
			  RETURNING id
		)
		INSERT INTO key_policies(key_id, policy_id, bound_by)
		  SELECT k.id, '` + visAdminPolicyID + `', 'test' FROM k;

		INSERT INTO audit_events(tenant_id, actor_type, event_type, resource_type, resource_id)
		  SELECT '` + tenantID + `', 'system', 'create', 'tenant', 'vis-'||g
		  FROM generate_series(1, 2) g;

		INSERT INTO job_definition_revisions
		  (tenant_id, source_mode, source_uid, source_namespace, source_name,
		   generation, spec_hash, normalized_spec, actor)
		  VALUES ('` + tenantID + `', 'k8s', 'vis-uid-` + ch + `', 'ns', 'name', 1,
		          repeat('h', 64), '{}', 'operator');

		INSERT INTO job_run_control_plane
		  (tenant_id, source_uid, revision_id, occurrence_key, trigger, actor, phase)
		  SELECT '` + tenantID + `', r.source_uid, r.id, repeat('k', 64), 'manual', 'operator', 'Pending'
		  FROM job_definition_revisions r
		  WHERE r.tenant_id = '` + tenantID + `';

		INSERT INTO job_run_attempts_control_plane
		  (tenant_id, run_id, attempt_number, phase, kubernetes_job_name)
		  SELECT '` + tenantID + `', r.id, 1, 'Pending', 'vis-attempt-` + ch + `'
		  FROM job_run_control_plane r
		  WHERE r.tenant_id = '` + tenantID + `';

		INSERT INTO checks(name, tenant_id, check_type, schedule_type, cron_expr)
		  VALUES ('vis-check-` + ch + `', '` + tenantID + `', 'http_health', 'cron', '* * * * *');

		INSERT INTO check_runs(tenant_id, check_id, scheduled_at)
		  SELECT '` + tenantID + `', c.id, now() FROM checks c
		  WHERE c.tenant_id = '` + tenantID + `';

		INSERT INTO slis(tenant_id, name, sli_type)
		  VALUES ('` + tenantID + `', 'sli-` + ch + `', 'ratio');

		INSERT INTO slos(tenant_id, name, sli_id, target)
		  SELECT '` + tenantID + `', 'slo-` + ch + `', id, 0.999 FROM slis
		  WHERE tenant_id = '` + tenantID + `';

		INSERT INTO sli_snapshots(tenant_id, sli_id, window_start, window_end)
		  SELECT '` + tenantID + `', id, now(), now() + interval '1 hour' FROM slis
		  WHERE tenant_id = '` + tenantID + `';

		INSERT INTO budgets(tenant_id, slo_id, window_start, window_end, budget_total, budget_remaining)
		  SELECT '` + tenantID + `', id, now(), now() + interval '30 days', 1, 1 FROM slos
		  WHERE tenant_id = '` + tenantID + `';

		INSERT INTO budget_alerts(tenant_id, slo_id, budget_id, alert_type, burn_rate)
		  SELECT s.tenant_id, s.id, b.id, 'burn_rate', 1
		  FROM slos s JOIN budgets b ON b.tenant_id = s.tenant_id
		  WHERE s.tenant_id = '` + tenantID + `';
	`
	if _, err := db.ExecContext(ctx, seed); err != nil {
		t.Fatalf("seed visibility fixtures for tenant %s: %v", ch, err)
	}
}

// TestEveryTenantTableFiltersCrossTenantReads seeds both tenants, then, as
// alpha: every table shows exactly alpha's rows, zero of beta's, and -- after
// the tenant context is reset -- nothing at all, with one exception the
// baseline intends: the three platform policy presets stay readable without a
// tenant. audit_events is checked through the partition name as well, because
// a partition does not inherit its parent's policy.
func TestEveryTenantTableFiltersCrossTenantReads(t *testing.T) {
	applyAllMigrations(t)
	ctx := ctx30(t)
	owner := ownerDB(t)
	seedVisibilityFixtures(t, ctx, owner, "a", visAlphaID)
	seedVisibilityFixtures(t, ctx, owner, "b", visBetaID)

	// table, predicate naming one of alpha's rows, the count that predicate
	// must return, and the TOTAL row count alpha may see in that table. The
	// total is the honest leak check: a WHERE tenant_id = beta predicate is
	// vacuous when the row is invisible, but count(*) cannot shrink for any
	// reason other than filtering. tenants is keyed by id; key_policies has no
	// tenant_id and is scoped through its key, so only its total is asserted;
	// audit_events is also read through the partition name.
	rows := []struct {
		table     string
		own       string
		ownWant   int
		totalWant int
	}{
		{"tenants", "id = '" + visAlphaID + "'", 1, 1},
		{"resource_groups", "tenant_id = '" + visAlphaID + "'", 1, 1},
		{"policies", "tenant_id = '" + visAlphaID + "'", 1, 4},
		{"api_keys", "tenant_id = '" + visAlphaID + "'", 1, 1},
		{"key_policies", "", 0, 1},
		{"audit_events", "tenant_id = '" + visAlphaID + "'", 2, 2},
		{"audit_events_default", "tenant_id = '" + visAlphaID + "'", 2, 2},
		{"job_definition_revisions", "tenant_id = '" + visAlphaID + "'", 1, 1},
		{"job_run_control_plane", "tenant_id = '" + visAlphaID + "'", 1, 1},
		{"job_run_attempts_control_plane", "tenant_id = '" + visAlphaID + "'", 1, 1},
		{"checks", "tenant_id = '" + visAlphaID + "'", 1, 1},
		{"check_runs", "tenant_id = '" + visAlphaID + "'", 1, 1},
		{"slis", "tenant_id = '" + visAlphaID + "'", 1, 1},
		{"slos", "tenant_id = '" + visAlphaID + "'", 1, 1},
		{"sli_snapshots", "tenant_id = '" + visAlphaID + "'", 1, 1},
		{"budgets", "tenant_id = '" + visAlphaID + "'", 1, 1},
		{"budget_alerts", "tenant_id = '" + visAlphaID + "'", 1, 1},
	}

	withRoleConn(t, "orbitjob_admin", func(ctx context.Context, conn *sql.Conn) {
		if _, err := conn.ExecContext(ctx, `SET app.tenant_id = '`+visAlphaID+`'`); err != nil {
			t.Fatalf("set tenant context: %v", err)
		}
		for _, r := range rows {
			if r.own != "" {
				if n := countAs(t, ctx, conn, `SELECT count(*) FROM `+r.table+` WHERE `+r.own); n != r.ownWant {
					t.Errorf("alpha sees %d own row(s) in %s (predicate %s), want %d; "+
						"the positive control failed so the total check below proves nothing", n, r.table, r.own, r.ownWant)
					continue
				}
			}
			if n := countAs(t, ctx, conn, `SELECT count(*) FROM `+r.table); n != r.totalWant {
				t.Errorf("alpha sees %d row(s) in %s in total, want %d; "+
					"the difference is another tenant's data coming through", n, r.table, r.totalWant)
			}
		}

		// Fail closed: with no tenant context the GUC is NULL, every policy
		// comparison is NULL, and nothing is visible -- except the platform
		// presets, whose SELECT-only policy deliberately matches tenant_id IS
		// NULL. policies is asserted against that exception explicitly.
		if _, err := conn.ExecContext(ctx, `RESET app.tenant_id`); err != nil {
			t.Fatalf("reset tenant context: %v", err)
		}
		for _, r := range rows {
			want := 0
			if r.table == "policies" {
				want = 3
			}
			if n := countAs(t, ctx, conn, `SELECT count(*) FROM `+r.table); n != want {
				t.Errorf("no tenant context sees %d row(s) in %s, want %d; RLS did not fail closed", n, r.table, want)
			}
		}
	})
}

// The mutation proofs below share one pattern: replace the tenant policy with
// a permissive one (or widen the grant) inside an open transaction, observe
// the guarded behavior disappear, roll back. PostgreSQL DDL and grants are
// transactional, so nothing survives the ROLLBACK; doing the read on the same
// connection avoids waiting on the ACCESS EXCLUSIVE lock the mutation holds.
// SET ROLE from the superuser connection makes RLS apply while the
// transaction is open -- the owner itself would bypass it.

// TestTenantScopingIsLoadBearing breaks the checks policy. The isolation the
// matrix above asserts must come from the predicate bound to app.tenant_id:
// with a USING (true) policy in place, alpha sees beta's rows. If the mutation
// changes nothing, the filtering was coming from somewhere else and the matrix
// is not testing the policy.
func TestTenantScopingIsLoadBearing(t *testing.T) {
	applyAllMigrations(t)
	ctx := ctx30(t)
	owner := ownerDB(t)
	seedVisibilityFixtures(t, ctx, owner, "a", visAlphaID)
	seedVisibilityFixtures(t, ctx, owner, "b", visBetaID)

	tx, err := owner.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DROP POLICY checks_tenant ON public.checks`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE POLICY checks_open ON public.checks
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
		`SELECT count(*) FROM checks WHERE tenant_id = '`+visBetaID+`'`).Scan(&betaRows); err != nil {
		t.Fatal(err)
	}
	if betaRows != 1 {
		t.Fatalf("with a permissive policy alpha sees %d of beta's check(s), want 1; "+
			"the tenant predicate is not what filters reads", betaRows)
	}
}

// TestPartitionScopingIsLoadBearing re-creates the historical defect on
// purpose: the partition audit_events_default queried by name, its policy
// replaced with a permissive one. Seeing beta's audit rows through the
// partition name is exactly the leak that produced 49,167 cross-tenant rows
// once (ADR 0002), and this test proves today's isolation test would catch
// it. Rolled back.
func TestPartitionScopingIsLoadBearing(t *testing.T) {
	applyAllMigrations(t)
	ctx := ctx30(t)
	owner := ownerDB(t)
	seedVisibilityFixtures(t, ctx, owner, "a", visAlphaID)
	seedVisibilityFixtures(t, ctx, owner, "b", visBetaID)

	tx, err := owner.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`DROP POLICY audit_events_default_tenant ON public.audit_events_default`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE POLICY audit_events_default_open ON public.audit_events_default
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
		`SELECT count(*) FROM audit_events_default WHERE tenant_id = '`+visBetaID+`'`).Scan(&betaRows); err != nil {
		t.Fatal(err)
	}
	if betaRows != 2 {
		t.Fatalf("with a permissive partition policy alpha sees %d of beta's audit row(s) "+
			"through the partition name, want 2", betaRows)
	}
}

// TestAdminLedgerDenialComesFromTheGrant proves the ledger write denial is the
// privilege system, not RLS or an absent object. With INSERT granted inside
// the transaction, the admin insert succeeds into its own tenant; the same
// widened grant still cannot write beta's tenant, because WITH CHECK keeps
// applying. Both observations inside one rolled-back transaction.
func TestAdminLedgerDenialComesFromTheGrant(t *testing.T) {
	applyAllMigrations(t)
	ctx := ctx30(t)
	owner := ownerDB(t)
	seedVisibilityFixtures(t, ctx, owner, "a", visAlphaID)
	seedVisibilityFixtures(t, ctx, owner, "b", visBetaID)

	// Beta's revision id is read as the owner first: under RLS the admin
	// cannot see it, so an INSERT ... SELECT from a tenant-scoped source would
	// insert zero rows and pass vacuously. The literal id makes the rejected
	// insert real.
	var betaRevisionID int64
	if err := owner.QueryRowContext(ctx,
		`SELECT id FROM job_definition_revisions WHERE tenant_id = '`+visBetaID+`'`).Scan(&betaRevisionID); err != nil {
		t.Fatalf("read beta revision as owner: %v", err)
	}

	tx, err := owner.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`GRANT INSERT ON public.job_run_control_plane TO orbitjob_admin`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx,
		`SET ROLE orbitjob_admin; SET app.tenant_id = '`+visAlphaID+`'`); err != nil {
		t.Fatalf("assume admin under mutation: %v", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO job_run_control_plane
		  (tenant_id, source_uid, revision_id, occurrence_key, trigger, actor, phase)
		SELECT '`+visAlphaID+`', r.source_uid, r.id, repeat('o', 63)||'1', 'manual', 'operator', 'Pending'
		FROM job_definition_revisions r WHERE r.tenant_id = '`+visAlphaID+`'`); err != nil {
		t.Fatalf("with INSERT granted the admin write into its own tenant must succeed: %v", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO job_run_control_plane
		  (tenant_id, source_uid, revision_id, occurrence_key, trigger, actor, phase)
		VALUES ('`+visBetaID+`', 'vis-uid-b', $1, repeat('o', 63)||'2', 'manual', 'operator', 'Pending')`,
		betaRevisionID); err == nil {
		t.Fatal("with INSERT granted the admin wrote a run row for beta; WITH CHECK did not apply")
	}
}
