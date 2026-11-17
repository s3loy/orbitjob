//go:build integration

package postgres

import (
	"errors"
	"orbitjob/internal/core/domain/slo"
	"orbitjob/internal/domain/resource"
	"testing"
	"time"
)

func TestSLOCreationRequiresLiveScopedSLI(t *testing.T) {
	owner := OpenStoreDB(t)
	ctx := monitoringActorContext(ctxStore(t))
	const tenant = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	const other = "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	const group = "01ARZ3NDEKTSV4RRFFQ69G5FAX"
	_, err := owner.ExecContext(ctx, `INSERT INTO tenants(id,slug,name) VALUES($1,'m3-a','M3 A'),($2,'m3-b','M3 B');`, tenant, other)
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.ExecContext(ctx, `INSERT INTO resource_groups(id,tenant_id,slug,name) VALUES($1,$2,'m3-group','M3 group')`, group, tenant)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]int64{}
	for _, c := range []struct {
		name, tenant string
		group        any
		deleted      bool
	}{
		{"same", tenant, nil, false}, {"group", tenant, group, false}, {"foreign", other, nil, false}, {"deleted", tenant, nil, true},
	} {
		var id int64
		err = owner.QueryRowContext(ctx, `INSERT INTO slis(tenant_id,name,sli_type,resource_group_id,deleted_at) VALUES($1,$2,'availability',$3,CASE WHEN $4 THEN now() ELSE NULL END) RETURNING id`, c.tenant, c.name, c.group, c.deleted).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		ids[c.name] = id
	}
	repo := NewSLORepository(openRoleDB(t, owner, "orbitjob_admin"))
	for _, c := range []struct {
		name, scope string
		allowed     bool
	}{
		{"same", "", true}, {"group", group, true}, {"foreign", "", false}, {"deleted", "", false}, {"same", group, false},
	} {
		_, err = repo.Create(ctx, tenant, slo.CreateSpec{Name: "objective-" + c.name + c.scope, SLIID: ids[c.name], ResourceGroupID: c.scope, Target: .99, WindowType: "rolling", WindowDuration: time.Hour, AlertFastBurnRate: 14.4, AlertSlowBurnRate: 6})
		if c.allowed {
			if err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
		} else {
			var missing *resource.NotFoundError
			if !errors.As(err, &missing) {
				t.Fatalf("%s expected hidden reference, got %v", c.name, err)
			}
		}
	}
	var count int
	if err = owner.QueryRowContext(ctx, `SELECT count(*) FROM slos`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("created %d objectives: %v", count, err)
	}
}
