//go:build integration

package postgres

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"orbitjob/internal/core/domain/function"
	"orbitjob/internal/domain/resource"
)

func TestFunctionManagementConcurrentCreateAndLostResponseReplay(t *testing.T) {
	owner := OpenStoreDB(t)
	ctx := ctxStore(t)
	const tenant = "01J00000000000000000000001"
	if _, err := owner.ExecContext(ctx, `INSERT INTO tenants(id,slug,name) VALUES($1,'function-management','Function management')`, tenant); err != nil {
		t.Fatal(err)
	}
	db := openRoleDB(t, owner, "orbitjob_admin")
	repo := NewFunctionRepository(db)
	in := function.ManagedWrite{ActorID: "test-key", IdempotencyKey: "lost-response", PayloadHash: strings.Repeat("a", 64), Definition: function.Definition{
		TenantID: tenant, Name: "charge", Image: "busybox:1.37", TimeoutSeconds: 60, Status: function.StatusActive,
	}}
	type receipt struct {
		definition function.Definition
		created    bool
		err        error
	}
	const requests = 6
	results := make(chan receipt, requests)
	var wg sync.WaitGroup
	for range requests {
		wg.Go(func() {
			definition, created, err := repo.CreateManaged(ctx, in)
			results <- receipt{definition, created, err}
		})
	}
	wg.Wait()
	close(results)
	var id int64
	created := 0
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if id == 0 {
			id = result.definition.ID
		}
		if result.definition.ID != id {
			t.Fatal("concurrent replay created multiple definitions")
		}
		if result.created {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("created receipts = %d", created)
	}
	var definitions, audits int
	if err := owner.QueryRowContext(ctx, `SELECT count(*) FROM functions WHERE tenant_id=$1`, tenant).Scan(&definitions); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE tenant_id=$1 AND resource_type='function'`, tenant).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if definitions != 1 || audits != 1 {
		t.Fatalf("definitions = %d, audits = %d", definitions, audits)
	}
	// Retrying after an uncertain response returns the original identity.
	replay, replayCreated, err := repo.CreateManaged(ctx, in)
	if err != nil || replayCreated || replay.ID != id {
		t.Fatalf("replay = %+v, error = %v", replay, err)
	}
	in.Definition.Image = "busybox:other"
	in.PayloadHash = strings.Repeat("b", 64)
	_, _, err = repo.CreateManaged(ctx, in)
	var conflict *resource.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("changed payload error = %v", err)
	}
}

func TestFunctionManagementVersionGroupAndAuditRollback(t *testing.T) {
	owner := OpenStoreDB(t)
	ctx := ctxStore(t)
	const tenant = "01J00000000000000000000001"
	const group = "01J00000000000000000000011"
	if _, err := owner.ExecContext(ctx, `INSERT INTO tenants(id,slug,name) VALUES($1,'function-audit','Function audit')`, tenant); err != nil {
		t.Fatal(err)
	}
	repo := NewFunctionRepository(openRoleDB(t, owner, "orbitjob_admin"))
	in := function.ManagedWrite{ActorID: "test-key", IdempotencyKey: "atomic-create", PayloadHash: strings.Repeat("a", 64), CallerGroupID: group, Definition: function.Definition{
		TenantID: tenant, ResourceGroupID: group, Name: "charge", Image: "busybox:1.37", TimeoutSeconds: 60, Status: function.StatusActive,
	}}
	// A rejected audit cannot leave behind a successful workload write.
	if _, err := owner.ExecContext(ctx, `ALTER TABLE audit_events ADD CONSTRAINT reject_function_audit CHECK(resource_type <> 'function') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.CreateManaged(ctx, in); err == nil {
		t.Fatal("expected rejected audit")
	}
	var count int
	if err := owner.QueryRowContext(ctx, `SELECT count(*) FROM functions`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("create persisted after audit rejection: count=%d, error=%v", count, err)
	}
	if _, err := owner.ExecContext(ctx, `ALTER TABLE audit_events DROP CONSTRAINT reject_function_audit`); err != nil {
		t.Fatal(err)
	}
	created, _, err := repo.CreateManaged(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	in.Definition = created
	in.Definition.Status = function.StatusPaused
	paused, err := repo.UpdateManaged(ctx, in)
	if err != nil || paused.Version != 2 || paused.ResourceGroupID != group {
		t.Fatalf("pause=%+v, error=%v", paused, err)
	}
	if _, err := repo.UpdateManaged(ctx, in); err == nil {
		t.Fatal("stale expected version must conflict")
	}
	in.Definition = paused
	in.CallerGroupID = "01J00000000000000000000012"
	_, err = repo.UpdateManaged(ctx, in)
	var missing *resource.NotFoundError
	if !errors.As(err, &missing) {
		t.Fatalf("foreign group error = %v", err)
	}
	in.CallerGroupID = group
	in.Definition.Status = function.StatusActive
	if _, err := owner.ExecContext(ctx, `ALTER TABLE audit_events ADD CONSTRAINT reject_function_audit CHECK(resource_type <> 'function') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpdateManaged(ctx, in); err == nil {
		t.Fatal("expected rejected update audit")
	}
	stored, found, err := repo.GetForTenant(context.Background(), tenant, paused.ID)
	if err != nil || !found || stored.Status != function.StatusPaused || stored.Version != 2 {
		t.Fatalf("update persisted after audit rejection: %+v, error=%v", stored, err)
	}
}
