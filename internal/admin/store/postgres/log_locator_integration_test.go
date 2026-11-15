//go:build integration

package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"orbitjob/internal/platform/scan"
	"strings"
	"testing"

	"orbitjob/internal/core/domain/function"
	"orbitjob/internal/platform/postgrestest"
)

func TestLogLocatorPinsFunctionHistoryAndTenant(t *testing.T) {
	db := postgrestest.Open(t)
	tenant, foreign, group := scan.GenerateID(), scan.GenerateID(), scan.GenerateID()
	for _, id := range []string{tenant, foreign} {
		if _, err := db.Exec(`INSERT INTO tenants(id,slug,name,status)VALUES($1,$2,'Logs','active')`, id, "logs-"+id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO resource_groups(id,tenant_id,slug,name)VALUES($1,$2,'logs-group','Logs group')`, group, tenant); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := db.QueryRow(`INSERT INTO functions(tenant_id,resource_group_id,name,image)VALUES($1,$2,'Task','task:v1')RETURNING id`, tenant, group).Scan(&id); err != nil {
		t.Fatal(err)
	}
	reader := LogLocatorRepository{DB: queryAdminConnection(t, db)}
	for index, key := range []string{strings.Repeat("a", 64), "legacy-occurrence"} {
		source := function.SourceUID(id)
		var rev, run int64
		if err := db.QueryRow(`INSERT INTO job_definition_revisions(tenant_id,source_mode,source_uid,source_namespace,source_name,generation,spec_hash,normalized_spec,is_active,actor)VALUES($1,'function',$2,'own',$2,$3,'hash','{}',false,'test')RETURNING id`, tenant, source, index+1).Scan(&rev); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`INSERT INTO job_run_control_plane(tenant_id,source_uid,revision_id,occurrence_key,trigger,actor,phase)VALUES($1,$2,$3,$4,'Manual','test','Succeeded')RETURNING id`, tenant, source, rev, key).Scan(&run); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO job_run_attempts_control_plane(tenant_id,run_id,attempt_number,phase,kubernetes_job_name,kubernetes_job_uid)VALUES($1,$2,1,'Succeeded',$3,'immutable-job')`, tenant, run, fmt.Sprintf("job-%d", index)); err != nil {
			t.Fatal(err)
		}
		digest := key
		if len(key) != 64 {
			sum := sha256.Sum256([]byte(key))
			digest = hex.EncodeToString(sum[:])
		}
		uuid := fmt.Sprintf("%s-%s-%s-%s-%s", digest[:8], digest[8:12], digest[12:16], digest[16:20], digest[20:32])
		// Ledger presence alone does not authorize a guessed Function history UUID.
		if _, err := reader.ResolveFunction(context.Background(), tenant, id, uuid, 1); err == nil {
			t.Fatal("accepted absent history projection")
		}
		if _, err := db.Exec(`INSERT INTO function_runs(tenant_id,function_id,run_id,status,triggered_at,started_at,finished_at)VALUES($1,$2,$3,'success',now(),now(),now())`, tenant, id, uuid); err != nil {
			t.Fatal(err)
		}
		out, err := reader.ResolveFunction(context.Background(), tenant, id, uuid, 1)
		if err != nil {
			t.Fatal(err)
		}
		if out.RevisionID != rev || out.JobUID != "immutable-job" || out.GroupID != group || out.Namespace != "own" || out.OccurrenceKey != key {
			t.Fatalf("lost immutable locator: %+v", out)
		}
		if _, err := reader.ResolveFunction(context.Background(), foreign, id, uuid, 1); err == nil {
			t.Fatal("cross-tenant mapping leaked")
		}
		if _, err := reader.ResolveFunction(context.Background(), tenant, id+1, uuid, 1); err == nil {
			t.Fatal("foreign Function mapping leaked")
		}
		if _, err := reader.Resolve(context.Background(), tenant, run, 999); err == nil {
			t.Fatal("nonexistent attempt returned a locator")
		}
		generic, err := reader.Resolve(context.Background(), tenant, run, 1)
		if err != nil || generic.JobUID != out.JobUID {
			t.Fatalf("ledger locator: %+v %v", generic, err)
		}
	}
}
