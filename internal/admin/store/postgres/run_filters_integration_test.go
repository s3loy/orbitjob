//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"net/url"
	"orbitjob/internal/platform/scan"
	"testing"

	runquery "orbitjob/internal/admin/app/run/query"
	"orbitjob/internal/platform/postgrestest"
)

func TestRunFiltersExactAndTenantBound(t *testing.T) {
	db := postgrestest.Open(t)
	ctx := context.Background()
	tenantA, tenantB := scan.GenerateID(), scan.GenerateID()
	for _, tenant := range []string{tenantA, tenantB} {
		if _, err := db.ExecContext(ctx, "INSERT INTO tenants (id,slug,name,status) VALUES ($1,$2,$3,'active')", tenant, "filters-"+tenant, "Filters "+tenant); err != nil {
			t.Fatal(err)
		}
	}
	for i, source := range []string{"source-a", "source-b", "source-foreign"} {
		tenant := tenantA
		if i == 2 {
			tenant = tenantB
		}
		var revision int64
		err := db.QueryRowContext(ctx, `INSERT INTO job_definition_revisions
			(tenant_id,source_mode,source_uid,source_namespace,source_name,generation,spec_hash,normalized_spec,is_active,actor)
			VALUES ($1,'kubernetes',$2,'default',$2,1,'hash','{}',true,'test') RETURNING id`, tenant, source).Scan(&revision)
		if err != nil {
			t.Fatal(err)
		}
		for _, occurrence := range []string{"same-key", "other-key"} {
			if _, err = db.ExecContext(ctx, `INSERT INTO job_run_control_plane
				(tenant_id,source_uid,revision_id,occurrence_key,trigger,actor,phase)
				VALUES ($1,$2,$3,$4,'Manual','test','Pending')`, tenant, source, revision, occurrence); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Startup options set the deployment role on every pooled connection.
	// Preserve this test's schema instead of opening the package-level DSN.
	var schema string
	if err := db.QueryRowContext(ctx, "SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(postgrestest.DSN(t))
	if err != nil {
		t.Fatal("invalid test DSN")
	}
	params := parsed.Query()
	params.Set("search_path", schema+",public")
	params.Set("options", "-c role=orbitjob_admin")
	parsed.RawQuery = params.Encode()
	admin, err := sql.Open("postgres", parsed.String())
	if err != nil {
		t.Fatal("open admin test connection")
	}
	t.Cleanup(func() { _ = admin.Close() })
	var role string
	if err := admin.QueryRowContext(ctx, "SELECT current_user").Scan(&role); err != nil {
		t.Fatal(err)
	}
	if role != "orbitjob_admin" {
		t.Fatalf("repository role: %s", role)
	}
	repo := NewRunRepository(admin)
	for _, tc := range []struct {
		source, occurrence string
		count              int
	}{
		{"source-a", "same-key", 1},
		{"source-b", "same-key", 1},
		{"source-a", "", 2},
		{"", "same-key", 2},
		{"source-foreign", "same-key", 0},
		{"source-a", "not-yet-projected", 0},
	} {
		rows, err := repo.List(ctx, runquery.ListInput{
			TenantID: tenantA, SourceUID: tc.source, OccurrenceKey: tc.occurrence, Limit: 20,
		})
		if err != nil || len(rows) != tc.count {
			t.Fatalf("%+v: %d rows, %v", tc, len(rows), err)
		}
		for _, row := range rows {
			if row.TenantID != tenantA || (tc.source != "" && row.SourceUID != tc.source) ||
				(tc.occurrence != "" && row.OccurrenceKey != tc.occurrence) {
				t.Fatalf("inexact or foreign row: %+v", row)
			}
		}
	}
}
