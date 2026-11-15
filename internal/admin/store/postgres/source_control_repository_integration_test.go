//go:build integration

package postgres

import (
	"errors"
	"testing"

	app "orbitjob/internal/admin/app/sourcecontrol"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/platform/postgrestest"
	"orbitjob/internal/platform/scan"
)

func TestSourceControlRepositoryRechecksAuthorizedParentBeforeCAS(t *testing.T) {
	owner := postgrestest.Open(t)
	tenant, foreign, group, nextGroup := scan.GenerateID(), scan.GenerateID(), scan.GenerateID(), scan.GenerateID()
	for _, id := range []string{tenant, foreign} {
		if _, err := owner.Exec(`INSERT INTO tenants(id,slug,name)VALUES($1::char(26),$1::text,'Billing operations')`, id); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{group, nextGroup} {
		if _, err := owner.Exec(`INSERT INTO resource_groups(id,tenant_id,slug,name)VALUES($1::char(26),$2,$1::text,'Invoice processing')`, id, tenant); err != nil {
			t.Fatal(err)
		}
	}
	var id int64
	if err := owner.QueryRow(`INSERT INTO functions(tenant_id,resource_group_id,name,image)VALUES($1,$2,'invoice-publish','registry.example.com/finance/publish:reviewed')RETURNING id`, tenant, group).Scan(&id); err != nil {
		t.Fatal(err)
	}
	repository := NewSourceControlRepository(queryAdminConnection(t, owner))
	target, err := repository.Resolve(t.Context(), tenant, "functions", id)
	if err != nil || target.ResourceGroupID != group || target.SourceUID == "" {
		t.Fatalf("target=%+v err=%v", target, err)
	}
	if _, err := repository.Resolve(t.Context(), foreign, "functions", id); err == nil {
		t.Fatal("foreign parent resolved")
	}
	state, err := repository.Read(t.Context(), target)
	if err != nil || state.Version != 0 {
		t.Fatalf("initial state=%+v err=%v", state, err)
	}
	if _, err := owner.Exec(`UPDATE functions SET resource_group_id=$1 WHERE id=$2`, nextGroup, id); err != nil {
		t.Fatal(err)
	}
	_, err = repository.Update(t.Context(), target, app.Change{Version: 0, ExecutionPaused: true}, scan.GenerateID())
	var missing *resource.NotFoundError
	if !errors.As(err, &missing) {
		t.Fatalf("group change between authorization and CAS bypassed: %v", err)
	}
	var count int
	if err := owner.QueryRow(`SELECT count(*) FROM execution_source_controls WHERE tenant_id=$1`, tenant).Scan(&count); err != nil || count != 0 {
		t.Fatalf("unauthorized control inserted: count=%d err=%v", count, err)
	}
	target, err = repository.Resolve(t.Context(), tenant, "functions", id)
	if err != nil {
		t.Fatal(err)
	}
	state, err = repository.Update(t.Context(), target, app.Change{Version: 0, ExecutionPaused: true}, scan.GenerateID())
	if err != nil || state.Version != 1 || !state.ExecutionPaused {
		t.Fatalf("authorized CAS state=%+v err=%v", state, err)
	}
	if _, err := owner.Exec(`UPDATE functions SET deleted_at=now() WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Read(t.Context(), target); !errors.As(err, &missing) {
		t.Fatalf("deleted parent disclosed controls: %v", err)
	}
}

func TestSourceControlDefinitionProofKeepsAdminLedgerReadOnly(t *testing.T) {
	owner := postgrestest.Open(t)
	tenant := scan.GenerateID()
	if _, err := owner.Exec(`INSERT INTO tenants(id,slug,name)VALUES($1::char(26),$1::text,'Warehouse operations')`, tenant); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := owner.QueryRow(`INSERT INTO job_definition_revisions(tenant_id,source_mode,source_uid,source_namespace,source_name,generation,spec_hash,normalized_spec,is_active,actor)VALUES($1,'kubernetes',$2,'warehouse','inventory',1,'hash','{}',true,'test')RETURNING id`, tenant, scan.GenerateID()).Scan(&id); err != nil {
		t.Fatal(err)
	}
	admin := queryAdminConnection(t, owner)
	repository := NewSourceControlRepository(admin)
	target, err := repository.Resolve(t.Context(), tenant, "jobs", id)
	if err != nil {
		t.Fatal(err)
	}
	state, err := repository.Update(t.Context(), target, app.Change{Version: 0, SchedulePaused: true}, scan.GenerateID())
	if err != nil || !state.SchedulePaused {
		t.Fatalf("immutable parent proof failed: state=%+v err=%v", state, err)
	}
	var canWrite bool
	if err := admin.QueryRow(`SELECT has_table_privilege(current_user,'job_run_control_plane','INSERT') OR has_table_privilege(current_user,'job_definition_revisions','UPDATE')`).Scan(&canWrite); err != nil || canWrite {
		t.Fatalf("source controls widened Admin ledger privilege: %t err=%v", canWrite, err)
	}
}
