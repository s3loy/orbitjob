//go:build integration

package postgres

import (
	"testing"

	"orbitjob/internal/core/domain/function"
	"orbitjob/internal/domain/executionpolicy"
	"orbitjob/internal/platform/scan"
)

func TestFunctionResourcesPersistAcrossRepositoryAndRuntimeReads(t *testing.T) {
	owner := OpenStoreDB(t)
	ctx := ctxStore(t)
	tenant := scan.GenerateID()
	if _, err := owner.ExecContext(ctx, `INSERT INTO tenants(id,slug,name) VALUES($1::char(26),$1::text,'Finance operations')`, tenant); err != nil {
		t.Fatal(err)
	}
	resources, err := executionpolicy.Normalize(1, false, 0, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	definition := function.Definition{TenantID: tenant, Name: "billing-extract", Image: "registry.example.com/finance/extract:reviewed", TimeoutSeconds: 60, Status: function.StatusActive, ExecutionPolicyVersion: 1, Resources: resources}
	admin := NewFunctionRepository(openRoleDB(t, owner, "orbitjob_admin"))
	created, err := admin.CreateForTenant(ctx, tenant, definition)
	if err != nil || created.ExecutionPolicyVersion != 1 || created.Resources == nil || created.Resources.Limits["memory"] != "256Mi" {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	runtime := NewFunctionRepository(openRoleDB(t, owner, "orbitjob_runtime"))
	active, err := runtime.ActiveForTenant(ctx, tenant)
	if err != nil || len(active) != 1 || active[0].Resources == nil || active[0].Resources.Requests["cpu"] != "100m" {
		t.Fatalf("runtime lost fixed policy: %+v err=%v", active, err)
	}
	created.Resources.Requests["memory"] = "256Mi"
	created.Resources.Limits["memory"] = "512Mi"
	updated, err := admin.UpdateForTenant(ctx, tenant, created)
	if err != nil || updated.Version != created.Version+1 || updated.Resources.Limits["memory"] != "512Mi" {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	if active[0].Resources.Limits["memory"] != "256Mi" {
		t.Fatal("saving new resources mutated the prior definition snapshot")
	}
}
