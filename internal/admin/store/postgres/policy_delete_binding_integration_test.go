//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"orbitjob/internal/platform/scan"
	"testing"
	"time"

	"orbitjob/internal/core/domain/audit"
	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/platform/postgrestest"
)

const deleteGrantDoc = `{"version":"1","statement":[{"effect":"Allow","action":["job:Get"],"resource":["orbitjob:self:*:job/*"]}]}`

func seedDeletePolicy(t *testing.T) (*sql.DB, *sql.DB, string, string, string) {
	t.Helper()
	db := postgrestest.Open(t)
	tenant, key, id := scan.GenerateID(), scan.GenerateID(), scan.GenerateID()
	if _, err := db.Exec(`INSERT INTO tenants(id,slug,name,status)VALUES($1,'delete-policy','Delete policy','active')`, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO policies(id,tenant_id,name,document)VALUES($1,$2,'Owned',$3::jsonb)`, id, tenant, deleteGrantDoc); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO api_keys(id,tenant_id,kind,key_hash,key_prefix)VALUES($1,$2,'tenant','delete-hash','delete')`, key, tenant); err != nil {
		t.Fatal(err)
	}
	return db, queryAdminConnection(t, db), tenant, key, id
}
func policyDeleteEvent(tenant, actor, id string) audit.Event {
	return audit.Event{TenantID: tenant, ActorID: actor, EventType: audit.EventDelete, ResourceType: audit.ResourcePolicy, ResourceID: id}
}
func assertPolicyBindingIntact(t *testing.T, db *sql.DB, key, id string) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT count(*)FROM key_policies WHERE key_id=$1 AND policy_id=$2`, key, id).Scan(&count); err != nil || count != 1 {
		t.Fatalf("binding removed: count=%d err=%v", count, err)
	}
}

func TestPolicyDeleteRefusesActiveAndRevokedBindings(t *testing.T) {
	db, admin, tenant, key, id := seedDeletePolicy(t)
	ctx := context.Background()
	repo := NewPolicyRepository(admin)
	if _, err := db.Exec(`INSERT INTO key_policies(key_id,policy_id)VALUES($1,$2)`, key, id); err != nil {
		t.Fatal(err)
	}
	request := policy.Request{Action: "job:Get", Resource: policy.ARN{Tenant: tenant, Group: "ci", Type: "job", ID: "1"}}
	for _, revoked := range []bool{false, true} {
		if revoked {
			if _, err := db.Exec(`UPDATE api_keys SET revoked_at=now()WHERE id=$1`, key); err != nil {
				t.Fatal(err)
			}
		}
		err := repo.Delete(ctx, tenant, id, policyDeleteEvent(tenant, key, id))
		assertConflict(t, err, "policy", "id")
		assertPolicyBindingIntact(t, db, key, id)
		docs, err := repo.EffectiveDocuments(ctx, tenant, key)
		if err != nil || policy.Evaluate(docs, request) != policy.DecisionAllow {
			t.Fatalf("policy grant changed (revoked=%v): documents=%+v err=%v", revoked, docs, err)
		}
	}
	var audits int
	if err := db.QueryRow(`SELECT count(*)FROM audit_events WHERE resource_id=$1 AND event_type='delete'`, id).Scan(&audits); err != nil || audits != 0 {
		t.Fatalf("refused deletion audited: %d %v", audits, err)
	}
}
func TestPolicyDeleteUnboundBoundaryPresetAndForeign(t *testing.T) {
	db, admin, tenant, key, id := seedDeletePolicy(t)
	ctx := context.Background()
	repo := NewPolicyRepository(admin)
	// A non-cascading boundary FK retains the same conflict response.
	if _, err := db.Exec(`UPDATE api_keys SET boundary_policy_id=$1 WHERE id=$2`, id, key); err != nil {
		t.Fatal(err)
	}
	assertConflict(t, repo.Delete(ctx, tenant, id, policyDeleteEvent(tenant, key, id)), "policy", "id")
	if _, err := db.Exec(`UPDATE api_keys SET boundary_policy_id=NULL WHERE id=$1`, key); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, tenant, id, policyDeleteEvent(tenant, key, id)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*)FROM policies WHERE id=$1`, id).Scan(&count); err != nil || count != 0 {
		t.Fatalf("unbound policy still exists: %d %v", count, err)
	}
	if err := db.QueryRow(`SELECT count(*)FROM audit_events WHERE resource_id=$1 AND event_type='delete'`, id).Scan(&count); err != nil || count != 1 {
		t.Fatalf("successful deletion audit count: %d %v", count, err)
	}
	preset := scan.GenerateID()
	if _, err := db.Exec(`INSERT INTO policies(id,name,document)VALUES($1,'Preset',$2::jsonb)`, preset, deleteGrantDoc); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, tenant, preset, policyDeleteEvent(tenant, key, preset)); !errors.Is(err, policy.ErrPlatformPolicyImmutable) {
		t.Fatalf("preset refusal changed: %v", err)
	}
	other, foreign := scan.GenerateID(), scan.GenerateID()
	if _, err := db.Exec(`INSERT INTO tenants(id,slug,name,status)VALUES($1,'foreign','Foreign','active')`, other); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO policies(id,tenant_id,name,document)VALUES($1,$2,'Foreign',$3::jsonb)`, foreign, other, deleteGrantDoc); err != nil {
		t.Fatal(err)
	}
	assertNotFound(t, repo.Delete(ctx, tenant, foreign, policyDeleteEvent(tenant, key, foreign)), "policy")
}

func TestPolicyDeleteWaitsForConcurrentBindingThenRefuses(t *testing.T) {
	db, admin, tenant, key, id := seedDeletePolicy(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	binding, err := WithTenant(ctx, admin, tenant)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = binding.Rollback() }()
	var bindingPID int
	if err := binding.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&bindingPID); err != nil {
		t.Fatal(err)
	}
	if _, err := binding.ExecContext(ctx, `INSERT INTO key_policies(key_id,policy_id)VALUES($1,$2)`, key, id); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		result <- NewPolicyRepository(admin).Delete(ctx, tenant, id, policyDeleteEvent(tenant, key, id))
	}()
	// Observe the actual PostgreSQL lock wait, rather than assuming scheduling
	// from a sleep. The insert's FK KEY SHARE precedes the delete's FOR UPDATE.
	deadline := time.Now().Add(5 * time.Second)
	blocked := false
	for time.Now().Before(deadline) {
		if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid))AND query LIKE '%FOR UPDATE%')`, bindingPID).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case err := <-result:
			t.Fatalf("delete did not await binding commit: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
	if !blocked {
		t.Fatal("delete never acquired the expected policy-row lock wait")
	}
	if err := binding.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		assertConflict(t, err, "policy", "id")
	case <-ctx.Done():
		t.Fatal("delete remained blocked after binding commit")
	}
	assertPolicyBindingIntact(t, db, key, id)
}
