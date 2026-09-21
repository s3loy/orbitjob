//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"testing"
	"time"

	"orbitjob/internal/core/domain/slo"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/platform/migrate"
	"orbitjob/internal/platform/postgrestest"
)

// SLORepository used to be the only core repository that ran without the
// tenant GUC: every sibling (checks, check_runs, budgets, budget_alerts,
// slis, sli_snapshots) opens its transaction and sets app.tenant_id first.
// Against the deployment roles -- orbitjob_admin and orbitjob_runtime are
// LOGIN ... NOBYPASSRLS -- that meant Create was rejected by the slos WITH
// CHECK, ListActive silently returned nothing, and ChangeStatus/Delete
// reported every stale-version conflict as not-found because
// classifyWriteMiss could not see the row either.
//
// This file runs the repository as orbitjob_admin against a real schema, so
// the GUC binding is not a mock nicety but the difference between the SLO
// write path working and not working.

// rlsTenantAlpha/Beta are CHAR(26) tenant ids seeded as owner rows; the admin
// connection must never see beta's.
const (
	rlsTenantAlpha = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	rlsTenantBeta  = "01ARZ3NDEKTSV4RRFFQ69G5FW"
)

// rlsRolePasswords matches the password map the db/migrations integration
// suite installs on the deployment roles; TCP into the test container
// requires scram auth, so the role must be reachable with a known password.
var rlsRolePasswords = map[string]string{
	"orbitjob_migrator": "itest-migrator",
	"orbitjob_admin":    "itest-admin",
	"orbitjob_runtime":  "itest-runtime",
	"orbitjob_operator": "itest-operator",
}

// openRoleDB derives from the owner's test-scoped DSN a connection pool
// speaking as a non-superuser role. The search_path is taken from the owner
// handle itself -- the harness-scoped DSN in the environment points at the
// package schema, not at this test's schema -- and the role password is
// installed first because the container's hba only trusts local sockets.
func openRoleDB(t *testing.T, owner *sql.DB, role string) *sql.DB {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := migrate.EnsureRolePasswords(ctx, owner, rlsRolePasswords); err != nil {
		t.Fatalf("ensure role passwords: %v", err)
	}

	var searchPath string
	if err := owner.QueryRowContext(ctx, `SHOW search_path`).Scan(&searchPath); err != nil {
		t.Fatalf("read test schema search_path: %v", err)
	}

	parsed, err := url.Parse(postgrestest.DSN(t))
	if err != nil {
		t.Fatalf("parse harness dsn: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", searchPath)
	parsed.RawQuery = query.Encode()
	parsed.User = url.UserPassword(role, rlsRolePasswords[role])

	db, err := sql.Open("postgres", parsed.String())
	if err != nil {
		t.Fatalf("open %s pool: %v", role, err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping %s pool: %v", role, err)
	}
	return db
}

func TestSLORepositoryUnderRLS(t *testing.T) {
	owner := OpenStoreDB(t)
	ctx := ctxStore(t)

	// Seed two tenants, one SLI and one active SLO each, as the owner: RLS
	// does not bind it, which is what lets one connection speak for both.
	seed := `
		INSERT INTO tenants (id, slug, name) VALUES
		  ('` + rlsTenantAlpha + `', 'rls-alpha', 'RLS Alpha'),
		  ('` + rlsTenantBeta + `', 'rls-beta', 'RLS Beta');

		INSERT INTO slis (tenant_id, name, sli_type) VALUES
		  ('` + rlsTenantAlpha + `', 'sli-alpha', 'ratio'),
		  ('` + rlsTenantBeta + `', 'sli-beta', 'ratio');

		INSERT INTO slos (tenant_id, name, sli_id, target)
		SELECT tenant_id, name || '-slo', id, 0.999 FROM slis;
	`
	if _, err := owner.ExecContext(ctx, seed); err != nil {
		t.Fatalf("seed rls fixtures: %v", err)
	}

	var alphaSliID, alphaSloID, betaSloID int64
	if err := owner.QueryRowContext(ctx,
		`SELECT id FROM slis WHERE tenant_id = '`+rlsTenantAlpha+`'`).Scan(&alphaSliID); err != nil {
		t.Fatalf("read alpha sli: %v", err)
	}
	if err := owner.QueryRowContext(ctx,
		`SELECT id FROM slos WHERE tenant_id = '`+rlsTenantAlpha+`'`).Scan(&alphaSloID); err != nil {
		t.Fatalf("read alpha slo: %v", err)
	}
	if err := owner.QueryRowContext(ctx,
		`SELECT id FROM slos WHERE tenant_id = '`+rlsTenantBeta+`'`).Scan(&betaSloID); err != nil {
		t.Fatalf("read beta slo: %v", err)
	}

	admin := openRoleDB(t, owner, "orbitjob_admin")
	repo := NewSLORepository(admin)

	// 1. Create: the slos WITH CHECK rejects a GUC-less insert.
	snap, err := repo.Create(ctx, rlsTenantAlpha, slo.CreateSpec{
		Name:           "created-under-rls",
		SLIID:          alphaSliID,
		Target:         0.99,
		WindowType:     "rolling",
		WindowDuration: 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("create as orbitjob_admin rejected: %v\n"+
			"the repository never bound app.tenant_id, so the slos WITH CHECK denied the insert", err)
	}
	if snap.TenantID != rlsTenantAlpha || snap.Name != "created-under-rls" {
		t.Errorf("created snapshot = (%s, %s), want (%s, created-under-rls)",
			snap.TenantID, snap.Name, rlsTenantAlpha)
	}

	// 2. ListActive: without the GUC this silently returns zero rows.
	active, err := repo.ListActive(ctx, rlsTenantAlpha)
	if err != nil {
		t.Fatalf("list active as orbitjob_admin: %v", err)
	}
	if len(active) != 2 {
		t.Errorf("alpha sees %d active slo(s), want 2; an empty result here is the "+
			"silent scheduler blindness the missing tenant GUC causes", len(active))
	}

	// 3. ChangeStatus on a stale version is a conflict, not a missing row:
	// classifyWriteMiss must be able to see the row it classifies.
	_, err = repo.ChangeStatus(ctx, rlsTenantAlpha, "", alphaSloID, 99, "paused")
	var conflict *resource.ConflictError
	var notFound *resource.NotFoundError
	switch {
	case errors.As(err, &conflict):
		// correct
	case errors.As(err, &notFound):
		t.Errorf("stale-version change on alpha's own slo reported not-found; " +
			"classifyWriteMiss ran without the tenant GUC and could not see the row")
	default:
		t.Errorf("stale-version change returned %T: %v, want *resource.ConflictError", err, err)
	}

	// 4. Beta's slo stays unreachable, for the right reason.
	if _, err := repo.ChangeStatus(ctx, rlsTenantAlpha, "", betaSloID, 1, "paused"); !errors.As(err, &notFound) {
		t.Errorf("change on beta's slo returned %T: %v, want *resource.NotFoundError", err, err)
	}

	// 5. Delete of alpha's own slo succeeds and really soft-deletes.
	if err := repo.Delete(ctx, rlsTenantAlpha, "", alphaSloID, 1); err != nil {
		t.Errorf("delete alpha's own slo: %v", err)
	}
	var deleted int
	if err := owner.QueryRowContext(ctx,
		`SELECT count(*) FROM slos WHERE id = $1 AND deleted_at IS NOT NULL`, alphaSloID).Scan(&deleted); err != nil {
		t.Fatalf("verify soft delete: %v", err)
	}
	if deleted != 1 {
		t.Errorf("alpha's slo was not soft-deleted (deleted_at set on %d row(s))", deleted)
	}
}
