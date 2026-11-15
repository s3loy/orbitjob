//go:build integration

package postgres

import (
	"context"
	"errors"
	"orbitjob/internal/platform/scan"
	"testing"

	query "orbitjob/internal/admin/app/job/query"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/platform/postgrestest"
)

func TestGenericJobsExcludeAllNonScheduledWorkloadKinds(t *testing.T) {
	db := postgrestest.Open(t)
	tenant := scan.GenerateID()
	if _, err := db.Exec(`INSERT INTO tenants(id,slug,name)VALUES($1,'job-kinds','Kinds')`, tenant); err != nil {
		t.Fatal(err)
	}
	ids := map[string]int64{}
	for _, kind := range []string{"kubernetes", "function", "workflow", "check"} {
		var id int64
		if err := db.QueryRow(`INSERT INTO job_definition_revisions(tenant_id,source_mode,source_uid,source_namespace,source_name,generation,spec_hash,normalized_spec,is_active,actor)VALUES($1,$2,$2,'own',$2,1,'hash','{}',true,'test')RETURNING id`, tenant, kind).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids[kind] = id
	}
	repo := NewJobRepository(queryAdminConnection(t, db))
	ctx := context.Background()
	rows, err := repo.List(ctx, query.ListInput{TenantID: tenant, Limit: 50})
	if err != nil || len(rows) != 1 || rows[0].SourceKind != "kubernetes" {
		t.Fatalf("generic list mixed kinds: %+v %v", rows, err)
	}
	for kind, id := range ids {
		item, err := repo.Get(ctx, query.GetInput{TenantID: tenant, ID: id})
		if kind == "kubernetes" {
			if err != nil || item.SourceMode != kind {
				t.Fatalf("ScheduledJob missing: %+v %v", item, err)
			}
			continue
		}
		var missing *resource.NotFoundError
		if !errors.As(err, &missing) {
			t.Fatalf("%s exposed through generic Get: %+v %v", kind, item, err)
		}
	}
}
