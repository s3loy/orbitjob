//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"orbitjob/internal/platform/scan"
	"testing"
	"time"

	jobquery "orbitjob/internal/admin/app/job/query"
	runquery "orbitjob/internal/admin/app/run/query"
	corepostgres "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/platform/postgrestest"
)

func queryAdminConnection(t *testing.T, db *sql.DB) *sql.DB {
	t.Helper()
	var schema string
	if err := db.QueryRow("SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(postgrestest.DSN(t))
	if err != nil {
		t.Fatal("invalid DSN")
	}
	q := u.Query()
	q.Set("search_path", schema+",public")
	q.Set("options", "-c role=orbitjob_admin")
	u.RawQuery = q.Encode()
	admin, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal("open scoped admin")
	}
	t.Cleanup(func() { _ = admin.Close() })
	return admin
}

func TestQueryControlsSQLSearchHistorySummary(t *testing.T) {
	db := postgrestest.Open(t)
	ctx := context.Background()
	a, b := scan.GenerateID(), scan.GenerateID()
	base := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	for _, tenant := range []string{a, b} {
		if _, err := db.Exec(`INSERT INTO tenants(id,slug,name,status)VALUES($1,$2,'Query tests','active')`, tenant, "query-"+tenant); err != nil {
			t.Fatal(err)
		}
	}
	revisions := map[string]int64{}
	for _, seed := range []struct{ tenant, source, name, namespace, mode string }{{a, "job-a", "API_%", "prod", "kubernetes"}, {a, "job-b", "other", "API_%", "kubernetes"}, {a, "job-c", "nonmatching", "elsewhere", "kubernetes"}, {a, "wf-a", "API_% workflow", "prod", "workflow"}, {b, "job-foreign", "API_%", "prod", "kubernetes"}} {
		var id int64
		if err := db.QueryRow(`INSERT INTO job_definition_revisions(tenant_id,source_mode,source_uid,source_namespace,source_name,generation,spec_hash,normalized_spec,is_active,actor,created_at)VALUES($1,$2,$3,$4,$5,1,'hash','{}',true,'test',$6)RETURNING id`, seed.tenant, seed.mode, seed.source, seed.namespace, seed.name, base).Scan(&id); err != nil {
			t.Fatal(err)
		}
		revisions[seed.source] = id
	}
	// Both interval edges, same-instant ties and a foreign tenant are present.
	var jobIDs []int64
	for i, instant := range []time.Time{base.Add(-time.Second), base, base.Add(30 * time.Minute), base.Add(30 * time.Minute), base.Add(time.Hour)} {
		var id int64
		phase := "Running"
		if i == 1 {
			phase = "Pending"
		}
		if err := db.QueryRow(`INSERT INTO job_run_control_plane(tenant_id,source_uid,revision_id,occurrence_key,trigger,actor,phase,created_at)VALUES($1,'job-a',$2,$3,'Manual','test',$4,$5)RETURNING id`, a, revisions["job-a"], fmt.Sprintf("job-%d", i), phase, instant).Scan(&id); err != nil {
			t.Fatal(err)
		}
		jobIDs = append(jobIDs, id)
		if _, err := db.Exec(`INSERT INTO workflow_run_control_plane(tenant_id,source_uid,revision_id,occurrence_key,trigger,actor,phase,created_at)VALUES($1,'wf-a',$2,$3,'Manual','test','Pending',$4)`, a, revisions["wf-a"], fmt.Sprintf("wf-%d", i), instant); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO job_run_control_plane(tenant_id,source_uid,revision_id,occurrence_key,trigger,actor,phase,created_at)VALUES($1,'job-foreign',$2,'foreign','Manual','test','Running',$3)`, b, revisions["job-foreign"], base); err != nil {
		t.Fatal(err)
	}
	var functionID int64
	if err := db.QueryRow(`INSERT INTO functions(tenant_id,name,image)VALUES($1,'API function','example:v1')RETURNING id`, a).Scan(&functionID); err != nil {
		t.Fatal(err)
	}
	for i, instant := range []time.Time{base.Add(-time.Second), base, base.Add(30 * time.Minute), base.Add(30 * time.Minute), base.Add(time.Hour)} {
		if _, err := db.Exec(`INSERT INTO function_runs(tenant_id,function_id,status,triggered_at,finished_at,created_at)VALUES($1,$2,'canceled',$3,$3,$4)`, a, functionID, instant, base.Add(time.Duration(i)*time.Hour+72*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	var foreignFunction int64
	if err := db.QueryRow(`INSERT INTO functions(tenant_id,name,image)VALUES($1,'Foreign function','example:v1')RETURNING id`, b).Scan(&foreignFunction); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO function_runs(tenant_id,function_id,status,triggered_at,finished_at)VALUES($1,$2,'canceled',$3,$3)`, b, foreignFunction, base); err != nil {
		t.Fatal(err)
	}
	admin := queryAdminConnection(t, db)
	jobs := NewJobRepository(admin)
	runs := NewRunRepository(admin)
	workflowDefs := corepostgres.NewWorkflowDefinitionRepository(admin)
	workflowRuns := corepostgres.NewWorkflowRunRepository(admin)
	functionRuns := corepostgres.NewFunctionRunRepository(admin)
	// Literal percent/underscore is not LIKE wildcard syntax, and search precedes paging.
	for offset := 0; offset < 2; offset++ {
		rows, err := jobs.List(ctx, jobquery.ListInput{TenantID: a, Search: "api_%", Limit: 1, Offset: offset})
		if err != nil || len(rows) != 1 || rows[0].TenantID != a {
			t.Fatalf("job search page %d: %+v %v", offset, rows, err)
		}
	}
	rows, err := jobs.List(ctx, jobquery.ListInput{TenantID: a, Search: "api_%", Limit: 1, Offset: 2})
	if err != nil || len(rows) != 0 {
		t.Fatalf("last job page %+v %v", rows, err)
	}
	defs, err := workflowDefs.SearchActiveWorkflowDefinitions(ctx, a, 1, 0, "api_%")
	if err != nil || len(defs) != 1 || defs[0].SourceUID != "wf-a" {
		t.Fatalf("workflow search %+v %v", defs, err)
	}
	input, err := runquery.NormalizeListInput(runquery.ListInput{TenantID: a, SourceUID: "job-a", CreatedFrom: "2026-10-19T08:00:00+08:00", CreatedBefore: "2026-10-19T02:00:00+01:00", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	for offset, want := range []int64{jobIDs[3], jobIDs[2], jobIDs[1]} {
		input.Offset = offset
		page, err := runs.List(ctx, input)
		if err != nil || len(page) != 1 || page[0].ID != want {
			t.Fatalf("run page %d: %+v %v", offset, page, err)
		}
	}
	input.Offset = 3
	page, err := runs.List(ctx, input)
	if err != nil || len(page) != 0 {
		t.Fatalf("last run page %+v %v", page, err)
	}
	summary, err := runs.Summary(ctx, input)
	if err != nil || summary.Total != 3 || summary.Phases["Running"] != 2 || summary.Phases["Pending"] != 1 {
		t.Fatalf("summary %+v %v", summary, err)
	}
	input.Phase = "Pending"
	summary, err = runs.Summary(ctx, input)
	if err != nil || summary.Total != 1 {
		t.Fatalf("filtered summary %+v %v", summary, err)
	}
	input.SourceUID = "job-foreign"
	summary, err = runs.Summary(ctx, input)
	if err != nil || summary.Total != 0 {
		t.Fatalf("foreign summary %+v %v", summary, err)
	}
	foreign, err := functionRuns.RunsByFunctionPage(ctx, a, foreignFunction, 10, 0, "", "")
	if err != nil || len(foreign) != 0 {
		t.Fatalf("foreign function history %+v %v", foreign, err)
	}
	foreignWF, err := workflowRuns.RunsForDefinitionWindow(ctx, b, "wf-a", 10, 0, "", "")
	if err != nil || len(foreignWF) != 0 {
		t.Fatalf("foreign workflow history %+v %v", foreignWF, err)
	}
	var lastWF, lastFN int64
	for offset := 0; offset < 4; offset++ {
		wf, err := workflowRuns.RunsForDefinitionWindow(ctx, a, "wf-a", 1, offset, "2026-10-19T00:00:00Z", "2026-10-19T01:00:00Z")
		if err != nil {
			t.Fatal(err)
		}
		fn, err := functionRuns.RunsByFunctionPage(ctx, a, functionID, 1, offset, "2026-10-19T00:00:00Z", "2026-10-19T01:00:00Z")
		if err != nil {
			t.Fatal(err)
		}
		want := 1
		if offset == 3 {
			want = 0
		}
		if len(wf) != want || len(fn) != want {
			t.Fatalf("history page %d wf=%d fn=%d", offset, len(wf), len(fn))
		}
		if offset < 3 {
			if offset > 0 && (wf[0].ID >= lastWF || fn[0].ID >= lastFN) {
				t.Fatalf("unstable same-instant history order wf=%d/%d fn=%d/%d", wf[0].ID, lastWF, fn[0].ID, lastFN)
			}
			lastWF, lastFN = wf[0].ID, fn[0].ID
		}
		for _, row := range fn {
			if row.TriggeredAt.Before(base) || !row.TriggeredAt.Before(base.Add(time.Hour)) {
				t.Fatalf("outside triggered_at window %+v", row)
			}
		}
	}
	// Nanosecond bounds must not be rounded down by PostgreSQL's microsecond
	// timestamptz parser. The same predicate semantics apply to all histories.
	for _, edge := range []struct {
		from, before string
		count        int
	}{
		{"2026-10-19T00:00:00.000000001Z", "2026-10-19T00:00:01Z", 0},
		{"2026-10-18T23:59:59.000000001Z", "2026-10-19T00:00:00.000000001Z", 1},
	} {
		in := runquery.ListInput{TenantID: a, SourceUID: "job-a", CreatedFrom: edge.from, CreatedBefore: edge.before, Limit: 10}
		list, err := runs.List(ctx, in)
		if err != nil || len(list) != edge.count {
			t.Fatalf("nanosecond run bound: %d want %d err %v", len(list), edge.count, err)
		}
		count, err := runs.Summary(ctx, in)
		if err != nil || count.Total != int64(edge.count) {
			t.Fatalf("nanosecond summary bound: %+v want %d err %v", count, edge.count, err)
		}
		wf, err := workflowRuns.RunsForDefinitionWindow(ctx, a, "wf-a", 10, 0, edge.from, edge.before)
		if err != nil || len(wf) != edge.count {
			t.Fatalf("nanosecond workflow bound: %d want %d err %v", len(wf), edge.count, err)
		}
		fn, err := functionRuns.RunsByFunctionPage(ctx, a, functionID, 10, 0, edge.from, edge.before)
		if err != nil || len(fn) != edge.count {
			t.Fatalf("nanosecond function bound: %d want %d err %v", len(fn), edge.count, err)
		}
	}

}
