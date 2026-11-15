//go:build integration

package postgres

import (
	"context"
	"orbitjob/internal/platform/scan"
	"testing"

	checkquery "orbitjob/internal/admin/app/check/query"
	"orbitjob/internal/platform/postgrestest"
)

func TestMonitoringSearchFiltersAuthorizedSetBeforeCountAndPagination(t *testing.T) {
	db := postgrestest.Open(t)
	ctx := context.Background()
	tenant, foreign, group, otherGroup := scan.GenerateID(), scan.GenerateID(), scan.GenerateID(), scan.GenerateID()
	for _, id := range []string{tenant, foreign} {
		if _, err := db.Exec(`INSERT INTO tenants(id,slug,name) VALUES($1,$2,'Monitoring search')`, id, "monitor-"+id); err != nil {
			t.Fatal(err)
		}
	}
	expected := map[string][]int64{}
	for index, row := range []struct {
		tenant, group, name string
		description         any
		deleted             bool
	}{
		{tenant, group, "API_% first", nil, false},
		{tenant, group, "description match", "API_% second", false},
		{tenant, group, "APIxx wildcard lookalike", nil, false},
		{tenant, otherGroup, "API_% private group", nil, false},
		{foreign, group, "API_% foreign tenant", nil, false},
		{tenant, group, "API_% deleted", nil, true},
	} {
		var checkID, sliID, sloID int64
		if err := db.QueryRow(`INSERT INTO checks(tenant_id,resource_group_id,name,description,check_type,schedule_type,interval_sec,deleted_at)
		 VALUES($1,$2,$3,$4,'http_health','interval',60,CASE WHEN $5 THEN now() END) RETURNING id`, row.tenant, row.group, row.name, row.description, row.deleted).Scan(&checkID); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`INSERT INTO slis(tenant_id,resource_group_id,name,description,sli_type,source_type,deleted_at)
		 VALUES($1,$2,$3,$4,'availability','job_run',CASE WHEN $5 THEN now() END) RETURNING id`, row.tenant, row.group, row.name, row.description, row.deleted).Scan(&sliID); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`INSERT INTO slos(tenant_id,resource_group_id,name,description,sli_id,target,deleted_at)
		 VALUES($1,$2,$3,$4,$5,0.99,CASE WHEN $6 THEN now() END) RETURNING id`, row.tenant, row.group, row.name, row.description, sliID, row.deleted).Scan(&sloID); err != nil {
			t.Fatal(err)
		}
		if index < 2 {
			expected["checks"] = append(expected["checks"], checkID)
			expected["slis"] = append(expected["slis"], sliID)
			expected["slos"] = append(expected["slos"], sloID)
		}
	}
	admin := queryAdminConnection(t, db)
	checks, slis, slos := NewCheckRepository(admin), NewSLIReadRepository(admin), NewSLOReadRepository(admin)
	for family, read := range map[string]func(int) ([]int64, int64, error){
		"checks": func(offset int) ([]int64, int64, error) {
			rows, total, err := checks.List(ctx, checkquery.ListChecksInput{TenantID: tenant, ResourceGroupID: group, Search: "api_%", Limit: 1, Offset: offset})
			var ids []int64
			for _, row := range rows {
				ids = append(ids, row.ID)
			}
			return ids, int64(total), err
		},
		"slis": func(offset int) ([]int64, int64, error) {
			rows, total, err := slis.ListSearch(ctx, tenant, group, "api_%", 1, offset)
			var ids []int64
			for _, row := range rows {
				ids = append(ids, row.ID)
			}
			return ids, total, err
		},
		"slos": func(offset int) ([]int64, int64, error) {
			rows, total, err := slos.ListSearch(ctx, tenant, group, "api_%", 1, offset)
			var ids []int64
			for _, row := range rows {
				ids = append(ids, row.ID)
			}
			return ids, total, err
		},
	} {
		for offset := range 3 {
			ids, total, err := read(offset)
			if err != nil || total != 2 {
				t.Fatalf("%s offset=%d ids=%v total=%d error=%v", family, offset, ids, total, err)
			}
			if offset < 2 {
				if len(ids) != 1 || ids[0] != expected[family][1-offset] {
					t.Fatalf("%s filtered after pagination: offset=%d ids=%v", family, offset, ids)
				}
			} else if len(ids) != 0 {
				t.Fatalf("%s last page=%v", family, ids)
			}
		}
	}
}
