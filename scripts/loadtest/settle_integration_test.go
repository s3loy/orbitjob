//go:build integration

package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
	"orbitjob/internal/platform/postgrestest"
	"orbitjob/internal/platform/scan"
)

func TestSettleInFlightHonorsRLSAndRunScope(t *testing.T) {
	owner := postgrestest.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin := settleAdminConnection(t, ctx, owner)
	newTenant := func() string {
		t.Helper()
		id := scan.GenerateID()
		if _, err := owner.ExecContext(ctx, `INSERT INTO tenants(id,slug,name) VALUES($1,$2,'settle regression')`, id, "settle-"+id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	newRevision := func(tenant string) CreatedDefinition {
		t.Helper()
		source := scan.GenerateID()
		definition := CreatedDefinition{CaseID: source, Tenant: tenant, ExpectedTerminalState: "success"}
		err := owner.QueryRowContext(ctx, `
			INSERT INTO job_definition_revisions
			(tenant_id,source_mode,source_uid,source_namespace,source_name,generation,spec_hash,normalized_spec,actor)
			VALUES($1,'kubernetes',$2,'settle-regression',$2,1,$3,'{}','settle regression') RETURNING id`,
			tenant, source, fmt.Sprintf("%x", sha256.Sum256([]byte(source)))).Scan(&definition.RevisionID)
		if err != nil {
			t.Fatal(err)
		}
		return definition
	}
	newRun := func(definition CreatedDefinition, phase string) {
		t.Helper()
		occurrence := fmt.Sprintf("%x", sha256.Sum256([]byte(scan.GenerateID())))
		if _, err := owner.ExecContext(ctx, `
			INSERT INTO job_run_control_plane(tenant_id,source_uid,revision_id,occurrence_key,trigger,actor,phase)
			VALUES($1,$2,$3,$4,'Manual','settle regression',$5)`,
			definition.Tenant, definition.CaseID, definition.RevisionID, occurrence, phase); err != nil {
			t.Fatal(err)
		}
	}

	first := newRevision(newTenant())
	second := newRevision(newTenant())
	outsideRevision := newRevision(first.Tenant)
	outsideTenant := newRevision(newTenant())
	missingEvidence := newRevision(newTenant())
	newRun(first, phaseSucceeded)
	newRun(first, phaseCanceled)
	newRun(second, "Running")
	newRun(outsideRevision, "Running")
	newRun(outsideTenant, "Running")
	created := []CreatedDefinition{first, second}

	// This is the former shell query. Under the actual application role it
	// returns zero without a tenant guard even though three runs are active.
	var ownerActive, oldActive int64
	const oldQuery = `SELECT count(*) FROM job_run_control_plane WHERE phase NOT IN ('Succeeded','Failed','Canceled')`
	if err := owner.QueryRowContext(ctx, oldQuery).Scan(&ownerActive); err != nil || ownerActive != 3 {
		t.Fatalf("fixture active runs=%d want=3 err=%v", ownerActive, err)
	}
	if err := admin.QueryRowContext(ctx, oldQuery).Scan(&oldActive); err != nil || oldActive != 0 {
		t.Fatalf("unscoped application query=%d want=0 err=%v", oldActive, err)
	}

	t.Run("later tenant remains active and unrelated runs are excluded", func(t *testing.T) {
		active, err := settleInFlight(ctx, admin, created)
		if err != nil || active != 1 {
			t.Fatalf("active=%d want=1 err=%v", active, err)
		}
		assertSettleTenantGuardCleared(t, ctx, admin)
	})

	t.Run("invalid or absent evidence never means settled", func(t *testing.T) {
		mismatched := first
		mismatched.Tenant = second.Tenant
		invalidTenant := first
		invalidTenant.Tenant = "not-a-tenant"
		invalidRevision := first
		invalidRevision.RevisionID = 0
		for _, tc := range []struct {
			name    string
			created []CreatedDefinition
		}{
			{"empty artifact", nil},
			{"invalid tenant", []CreatedDefinition{invalidTenant}},
			{"invalid revision", []CreatedDefinition{invalidRevision}},
			{"revision belongs to another tenant", []CreatedDefinition{mismatched}},
			{"later tenant has no ledger rows", []CreatedDefinition{first, missingEvidence}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if active, err := settleInFlight(ctx, admin, tc.created); err == nil {
					t.Fatalf("settle accepted incomplete evidence: active=%d", active)
				}
				assertSettleTenantGuardCleared(t, ctx, admin)
			})
		}
	})

	t.Run("every nonterminal phase remains in flight", func(t *testing.T) {
		for _, phase := range []string{"Pending", "CreatingAttempt", "Starting", "Running", "Canceling", "CancelUnknown", "UnknownFuturePhase"} {
			t.Run(phase, func(t *testing.T) {
				if _, err := owner.ExecContext(ctx, `UPDATE job_run_control_plane SET phase=$1 WHERE revision_id=$2`, phase, second.RevisionID); err != nil {
					t.Fatal(err)
				}
				active, err := settleInFlight(ctx, admin, created)
				if err != nil || active != 1 {
					t.Fatalf("phase=%s active=%d want=1 err=%v", phase, active, err)
				}
				assertSettleTenantGuardCleared(t, ctx, admin)
			})
		}
	})

	t.Run("all selected runs terminal while unrelated runs remain active", func(t *testing.T) {
		if _, err := owner.ExecContext(ctx, `UPDATE job_run_control_plane SET phase='Failed' WHERE revision_id=ANY($1)`, pq.Array([]int64{second.RevisionID})); err != nil {
			t.Fatal(err)
		}
		active, err := settleInFlight(ctx, admin, created)
		if err != nil || active != 0 {
			t.Fatalf("terminal selected runs: active=%d want=0 err=%v", active, err)
		}
		if err := owner.QueryRowContext(ctx, oldQuery).Scan(&ownerActive); err != nil || ownerActive != 2 {
			t.Fatalf("unrelated active runs=%d want=2 err=%v", ownerActive, err)
		}
		assertSettleTenantGuardCleared(t, ctx, admin)
	})
}

func settleAdminConnection(t *testing.T, ctx context.Context, owner *sql.DB) *sql.DB {
	t.Helper()
	var searchPath string
	if err := owner.QueryRowContext(ctx, "SHOW search_path").Scan(&searchPath); err != nil {
		t.Fatal(err)
	}
	dsn, err := url.Parse(postgrestest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	query := dsn.Query()
	query.Set("search_path", searchPath)
	// Keep harness credentials; do not alter cluster-wide role passwords.
	query.Set("options", strings.TrimSpace(query.Get("options")+" -c role=orbitjob_admin"))
	dsn.RawQuery = query.Encode()
	admin, err := sql.Open("postgres", dsn.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	// Reusing one physical connection makes a leaked tenant GUC observable.
	admin.SetMaxOpenConns(1)
	admin.SetMaxIdleConns(1)
	var role string
	var superuser, bypassRLS bool
	err = admin.QueryRowContext(ctx, `SELECT current_user,rolsuper,rolbypassrls FROM pg_roles WHERE rolname=current_user`).Scan(&role, &superuser, &bypassRLS)
	if err != nil || role != "orbitjob_admin" || superuser || bypassRLS {
		t.Fatalf("settle role=%s superuser=%t bypassRLS=%t err=%v", role, superuser, bypassRLS, err)
	}
	assertSettleTenantGuardCleared(t, ctx, admin)
	return admin
}

func assertSettleTenantGuardCleared(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	var tenant string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(current_setting('app.tenant_id',true),'')`).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	if tenant != "" {
		t.Fatalf("tenant guard leaked to pooled connection: %q", tenant)
	}
}
