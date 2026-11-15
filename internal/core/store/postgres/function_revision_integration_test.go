//go:build integration

package postgres

import (
	"encoding/json"
	"testing"
	"time"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/app/projection"
	"orbitjob/internal/core/domain/function"
)

func TestFunctionDefinitionOrderingAndPinnedRevision(t *testing.T) {
	owner := OpenStoreDB(t)
	ctx := ctxStore(t)
	const tenant = "01J00000000000000000000001"
	if _, err := owner.ExecContext(ctx, `INSERT INTO tenants(id,slug,name) VALUES($1,'function-ordering','Function ordering')`, tenant); err != nil {
		t.Fatal(err)
	}
	db := openRoleDB(t, owner, "orbitjob_admin")
	functions := NewFunctionRepository(db)
	var defs []function.Definition
	for _, name := range []string{"oldest", "same-time-first", "same-time-second", "middle"} {
		def, err := functions.CreateForTenant(ctx, tenant, function.Definition{
			Name: name, Status: function.StatusActive, Image: "busybox:1.37",
			Command: []string{"sh", "-c", "sleep 3"}, TimeoutSeconds: 60,
			HistorySuccess: 10, HistoryFailed: 10,
		})
		if err != nil {
			t.Fatal(err)
		}
		defs = append(defs, def)
	}
	for index, age := range []int{30, 10, 10, 20} {
		if _, err := owner.ExecContext(ctx, `UPDATE functions SET created_at=$1 WHERE id=$2`, time.Date(2026, 10, 19, 0, 0, age, 0, time.UTC), defs[index].ID); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := functions.ListForTenant(ctx, tenant)
	if err != nil {
		t.Fatal(err)
	}
	// Newest creation time wins; equal timestamps use descending immutable ID.
	for index, expected := range []int{0, 3, 2, 1} {
		if rows[index].ID != defs[expected].ID {
			t.Fatalf("row %d ID=%d, want %d", index, rows[index].ID, defs[expected].ID)
		}
	}
	control := NewControlPlaneRepository(owner)
	service := projection.Service{Revisions: control}
	id, err := service.ApplyFunction(ctx, defs[0], "orbitjob", tenant, "test")
	if err != nil {
		t.Fatal(err)
	}
	updated := defs[0]
	updated.TimeoutSeconds = 1
	if _, err := functions.UpdateForTenant(ctx, tenant, updated); err != nil {
		t.Fatal(err)
	}
	active, err := NewControlPlaneRepository(db).ActiveFunctionRevision(ctx, tenant, function.SourceUID(updated.ID))
	if err != nil {
		t.Fatal(err)
	}
	var pinned v1alpha1.ScheduledJobSpec
	if err := json.Unmarshal([]byte(active.NormalizedSpec), &pinned); err != nil {
		t.Fatal(err)
	}
	if active.ID != id || active.Generation != 1 || pinned.TimeoutSeconds != 60 {
		t.Fatalf("active revision changed before projection: %+v timeout=%d", active, pinned.TimeoutSeconds)
	}
}
