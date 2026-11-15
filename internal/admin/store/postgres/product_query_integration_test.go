//go:build integration

package postgres

import (
	"context"
	"fmt"
	"orbitjob/internal/platform/scan"
	"testing"

	jobquery "orbitjob/internal/admin/app/job/query"
	runquery "orbitjob/internal/admin/app/run/query"
	"orbitjob/internal/platform/postgrestest"
)

func TestProductExactJobFiltersAndPinnedRunMetadata(t *testing.T) {
	db := postgrestest.Open(t)
	ctx := context.Background()
	tenant, foreign := scan.GenerateID(), scan.GenerateID()
	for _, id := range []string{tenant, foreign} {
		if _, err := db.Exec(`INSERT INTO tenants(id,slug,name) VALUES($1,$2,'Product query')`, id, "product-"+id); err != nil {
			t.Fatal(err)
		}
	}
	ids := map[string]int64{}
	for index, row := range []struct {
		tenant, uid, namespace, name, mode string
		generation                         int
		active                             bool
	}{
		{tenant, "old-source", "prod", "old-name", "kubernetes", 1, false},
		{tenant, "old-source", "changed", "new-name", "kubernetes", 2, true},
		{tenant, "match-one", "prod", "API_% one", "kubernetes", 1, true},
		{tenant, "match-two", "prod", "API_% two", "function", 1, true},
		{tenant, "outside", "other", "API_% outside", "kubernetes", 1, true},
		{foreign, "foreign-source", "prod", "API_% foreign", "kubernetes", 1, true},
	} {
		var id int64
		if err := db.QueryRow(`INSERT INTO job_definition_revisions(tenant_id,source_mode,source_uid,source_namespace,source_name,generation,spec_hash,normalized_spec,is_active,actor)
		 VALUES($1,$2,$3,$4,$5,$6,'hash','{}',$7,'test') RETURNING id`, row.tenant, row.mode, row.uid, row.namespace, row.name, row.generation, row.active).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids[fmt.Sprint(index)] = id
	}
	admin := queryAdminConnection(t, db)
	jobs := NewJobRepository(admin)
	for offset := range 1 {
		rows, err := jobs.List(ctx, jobquery.ListInput{TenantID: tenant, Namespace: "prod", Search: "api_%", Limit: 1, Offset: offset})
		if err != nil || len(rows) != 1 || rows[0].Namespace != "prod" || rows[0].TenantID != tenant || rows[0].SourceKind == "" {
			t.Fatalf("filtered page=%+v error=%v", rows, err)
		}
	}
	rows, err := jobs.List(ctx, jobquery.ListInput{TenantID: tenant, Namespace: "prod", Search: "api_%", Limit: 1, Offset: 1})
	if err != nil || len(rows) != 0 {
		t.Fatalf("pagination before filtering: rows=%+v error=%v", rows, err)
	}
	rows, err = jobs.List(ctx, jobquery.ListInput{TenantID: tenant, Namespace: "prod", SourceUID: "match-two", Limit: 10})
	if err != nil || len(rows) != 0 {
		t.Fatalf("exact identity=%+v error=%v", rows, err)
	}
	for index, row := range []struct {
		uid      string
		revision int64
	}{{"old-source", ids["0"]}, {"mismatched-source", ids["0"]}, {"foreign-source", ids["5"]}} {
		if _, err := db.Exec(`INSERT INTO job_run_control_plane(tenant_id,source_uid,revision_id,occurrence_key,trigger,actor,phase) VALUES($1,$2,$3,$4,'Manual','test','Pending')`, tenant, row.uid, row.revision, fmt.Sprint("product-run-", index)); err != nil {
			t.Fatal(err)
		}
	}
	runs, err := NewRunRepository(admin).List(ctx, runquery.ListInput{TenantID: tenant, Limit: 10})
	if err != nil || len(runs) != 3 {
		t.Fatalf("runs=%+v error=%v", runs, err)
	}
	for _, run := range runs {
		if run.SourceUID == "old-source" {
			if run.SourceName == nil || *run.SourceName != "old-name" || run.SourceNamespace == nil || *run.SourceNamespace != "prod" || run.SourceKind == nil || *run.SourceKind != "kubernetes" {
				t.Fatalf("used active rather than exact pinned revision: %+v", run)
			}
		} else if run.SourceName != nil || run.SourceNamespace != nil || run.SourceKind != nil {
			t.Fatalf("foreign/mismatched revision leaked metadata: %+v", run)
		}
	}
}
