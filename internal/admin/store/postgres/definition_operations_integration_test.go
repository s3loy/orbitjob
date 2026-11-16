//go:build integration

package postgres

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"orbitjob/internal/admin/definition"
	"orbitjob/internal/platform/postgrestest"
	"orbitjob/internal/platform/scan"
)

func TestDefinitionOperationAuditsFullNamespacedIdentity(t *testing.T) {
	db := postgrestest.Open(t)
	ctx := context.Background()
	subject := definition.Subject{TenantID: scan.GenerateID(), ActorID: scan.GenerateID()}
	if _, err := db.Exec(`INSERT INTO tenants(id,slug,name,status)VALUES($1,$2,'Long definition identity','active')`, subject.TenantID, "identity-"+subject.TenantID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	op := definition.Operation{
		RequestID: uuid.NewString(), ActorID: subject.ActorID, Kind: "jobs",
		Namespace: strings.Repeat("n", 63), Name: strings.Repeat("j", 63),
		PayloadHash: strings.Repeat("a", 64), State: "checked",
		Request:      definition.ApplyRequest{SpecJSON: `{}`},
		CheckedUntil: now.Add(time.Minute), ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now,
	}
	repo := DefinitionOperations{DB: queryAdminConnection(t, db)}
	if err := repo.Register(ctx, subject, op); err != nil {
		t.Fatalf("check with a valid namespaced identity: %v", err)
	}
	if _, claimed, err := repo.Claim(ctx, subject, op.RequestID, op.PayloadHash, now); err != nil || !claimed {
		t.Fatalf("claim checked identity: claimed=%t err=%v", claimed, err)
	}
	result := definition.Result{RequestID: op.RequestID, Kind: op.Kind, Namespace: op.Namespace, Name: op.Name, State: "accepted", UID: uuid.NewString()}
	if err := repo.Finish(ctx, subject, op.RequestID, "accepted", &result, now); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM audit_events WHERE tenant_id=$1 AND resource_id=$2 AND resource_type='jobs'`, subject.TenantID, op.Namespace+"/"+op.Name).Scan(&count); err != nil || count != 3 {
		t.Fatalf("full resource identity must survive all three audit events: count=%d err=%v", count, err)
	}
}

func TestDefinitionOperationClaimsAndHistoryUseRealTenantTransactions(t *testing.T) {
	db := postgrestest.Open(t)
	ctx := context.Background()
	subject := definition.Subject{TenantID: scan.GenerateID(), ActorID: scan.GenerateID()}
	foreign := scan.GenerateID()
	for _, tenant := range []string{subject.TenantID, foreign} {
		if _, err := db.Exec(`INSERT INTO tenants(id,slug,name,status)VALUES($1,$2,'Billing operations','active')`, tenant, "operations-"+tenant); err != nil {
			t.Fatal(err)
		}
	}
	repo := DefinitionOperations{DB: queryAdminConnection(t, db)}
	canonical, digest, err := definition.CanonicalSpec("jobs", `{"jobTemplate":{"image":"busybox:1.37.0"}}`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	op := definition.Operation{RequestID: uuid.NewString(), ActorID: subject.ActorID, Kind: "jobs", Namespace: "finance-prod", Name: "billing-reconciliation", PayloadHash: digest, State: "checked", Request: definition.ApplyRequest{SpecJSON: canonical, ManifestDigest: digest}, CheckedUntil: now.Add(time.Minute), ExpiresAt: now.Add(30 * 24 * time.Hour), CreatedAt: now, UpdatedAt: now}
	if err = repo.Register(ctx, subject, op); err != nil {
		t.Fatal(err)
	}
	var claimed atomic.Int32
	var group sync.WaitGroup
	errors := make(chan error, 12)
	for range 12 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, ok, err := repo.Claim(ctx, subject, op.RequestID, digest, now)
			if err != nil {
				errors <- err
			}
			if ok {
				claimed.Add(1)
			}
		}()
	}
	group.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	if claimed.Load() != 1 {
		t.Fatalf("concurrent callers obtained %d dispatch permissions", claimed.Load())
	}
	if _, err = repo.Lookup(ctx, definition.Subject{TenantID: foreign, ActorID: subject.ActorID}, op.RequestID); err == nil {
		t.Fatal("foreign tenant read the operation")
	}
	if _, err = repo.Lookup(ctx, definition.Subject{TenantID: subject.TenantID, ActorID: scan.GenerateID()}, op.RequestID); err == nil {
		t.Fatal("another key inherited a private operation")
	}
	result := definition.Result{RequestID: op.RequestID, UID: uuid.NewString(), Kind: "jobs", Namespace: op.Namespace, Name: op.Name, ManifestDigest: digest, SpecJSON: canonical, State: "accepted"}
	if err = repo.Finish(ctx, subject, op.RequestID, "accepted", &result, now); err != nil {
		t.Fatal(err)
	}
	if err = repo.Finish(ctx, subject, op.RequestID, "unknown", nil, now); err != nil {
		t.Fatal(err)
	}
	retained, err := repo.Lookup(ctx, subject, op.RequestID)
	if err != nil || retained.State != "accepted" || retained.Result == nil || retained.Result.UID != result.UID {
		t.Fatalf("accepted history lost: %+v %v", retained, err)
	}
	var audits int
	if err = db.QueryRow(`SELECT count(*) FROM audit_events WHERE tenant_id=$1 AND resource_type='jobs'`, subject.TenantID).Scan(&audits); err != nil || audits != 3 {
		t.Fatalf("unexpected operation audit: %d %v", audits, err)
	}
}

func TestDefinitionCheckRollsBackWhenAuditCannotCommit(t *testing.T) {
	db := postgrestest.Open(t)
	ctx := context.Background()
	s := definition.Subject{TenantID: scan.GenerateID(), ActorID: scan.GenerateID()}
	if _, err := db.Exec(`INSERT INTO tenants(id,slug,name,status)VALUES($1,$2,'Audited operations','active')`, s.TenantID, "operation-"+s.TenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE audit_events ADD CONSTRAINT reject_definition_test CHECK(event_type NOT LIKE 'definition%') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	op := definition.Operation{RequestID: uuid.NewString(), ActorID: s.ActorID, Kind: "jobs", Namespace: "finance-prod", Name: "billing-reconciliation", PayloadHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", State: "checked", Request: definition.ApplyRequest{SpecJSON: `{}`}, CheckedUntil: now.Add(time.Minute), ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now}
	if err := (DefinitionOperations{DB: queryAdminConnection(t, db)}).Register(ctx, s, op); err == nil {
		t.Fatal("check was stored without its audit")
	}
	var records int
	if err := db.QueryRow(`SELECT count(*) FROM definition_operations WHERE tenant_id=$1`, s.TenantID).Scan(&records); err != nil || records != 0 {
		t.Fatalf("unaudited check survived: %d %v", records, err)
	}
}
