//go:build integration

package postgres

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"testing"

	"orbitjob/internal/core/domain/function"
	"orbitjob/internal/platform/scan"
)

func TestFunctionResourcesPersistAcrossManagementAndRuntimeReads(t *testing.T) {
	owner := OpenStoreDB(t)
	ctx := ctxStore(t)
	tenant := scan.GenerateID()
	if _, err := owner.ExecContext(ctx, `INSERT INTO tenants(id,slug,name) VALUES($1::char(26),$1::text,'Finance operations')`, tenant); err != nil {
		t.Fatal(err)
	}
	definition, err := function.NormalizeManagedDefinition(function.Definition{TenantID: tenant, Name: "billing-extract", Image: "registry.example.com/finance/extract:reviewed", TimeoutSeconds: 60, Status: function.StatusActive, ExecutionPolicyVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	admin := NewFunctionRepository(openRoleDB(t, owner, "orbitjob_admin"))
	write := function.ManagedWrite{Definition: definition, ActorID: scan.GenerateID(), IdempotencyKey: scan.GenerateID(), PayloadHash: hex.EncodeToString(digest[:])}
	created, fresh, err := admin.CreateManaged(ctx, write)
	if err != nil || !fresh || created.ExecutionPolicyVersion != 1 || created.Resources == nil || created.Resources.Limits["memory"] != "256Mi" {
		t.Fatalf("created=%+v fresh=%t err=%v", created, fresh, err)
	}
	runtime := NewFunctionRepository(openRoleDB(t, owner, "orbitjob_runtime"))
	active, err := runtime.ActiveForTenant(ctx, tenant)
	if err != nil || len(active) != 1 || active[0].Resources == nil || active[0].Resources.Requests["cpu"] != "100m" {
		t.Fatalf("runtime lost fixed policy: %+v err=%v", active, err)
	}
	created.Resources.Requests["memory"] = "256Mi"
	created.Resources.Limits["memory"] = "512Mi"
	write.Definition = created
	updated, err := admin.UpdateManaged(ctx, write)
	if err != nil || updated.Version != created.Version+1 || updated.Resources.Limits["memory"] != "512Mi" {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	if active[0].Resources.Limits["memory"] != "256Mi" {
		t.Fatal("saving new resources mutated the prior definition snapshot")
	}
	var audited string
	if err := owner.QueryRowContext(ctx, `SELECT diff->'resources'->'limits'->>'memory' FROM audit_events WHERE tenant_id=$1 AND resource_type='function' AND resource_id=$2 AND event_type='update'`, tenant, strconv.FormatInt(created.ID, 10)).Scan(&audited); err != nil || audited != "512Mi" {
		t.Fatalf("resource change missing audit: value=%q err=%v", audited, err)
	}
}
