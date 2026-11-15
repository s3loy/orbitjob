//go:build integration

package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"orbitjob/internal/platform/postgrestest"
	"orbitjob/internal/platform/scan"
)

func TestResetPreservesUnrelatedTenantsAndTheirHistory(t *testing.T) {
	db := postgrestest.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ownedMetadata, err := json.Marshal(map[string]string{loadManagedByLabelKey: loadManagedByLabelValue})
	if err != nil {
		t.Fatal(err)
	}
	type fixture struct {
		id, slug, metadata string
		owned              bool
	}
	var fixtures []fixture
	for _, row := range []fixture{
		{slug: "default", metadata: "{}"},
		{slug: "ordinary-customer", metadata: "{}"},
		{slug: "load-unmarked", metadata: "{}"},
		{slug: "marked-customer", metadata: string(ownedMetadata)},
		{slug: "load-owned", metadata: string(ownedMetadata), owned: true},
	} {
		row.id = scan.GenerateID()
		if _, err := db.ExecContext(ctx, `INSERT INTO tenants(id,slug,name,metadata) VALUES($1,$2,$2,$3)`, row.id, row.slug, row.metadata); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO audit_events(tenant_id,actor_type,event_type,resource_type,resource_id) VALUES($1,'system','seed','tenant',$2)`, row.id, row.id); err != nil {
			t.Fatal(err)
		}
		fixtures = append(fixtures, row)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, step := range resetSteps() {
		if _, err := tx.ExecContext(ctx, step.cmd[len(step.cmd)-1]); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
	}
	for _, row := range fixtures {
		for _, table := range []string{"tenants", "audit_events"} {
			column := "tenant_id"
			if table == "tenants" {
				column = "id"
			}
			var count int
			if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE "+column+"=$1", row.id).Scan(&count); err != nil {
				t.Fatal(err)
			}
			want := 1
			if row.owned {
				want = 0
			}
			if count != want {
				t.Errorf("%s tenant %s: rows=%d want=%d", table, row.slug, count, want)
			}
		}
	}
}
