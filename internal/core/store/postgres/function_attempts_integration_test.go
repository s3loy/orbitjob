//go:build integration

package postgres

import (
	"fmt"
	"strings"
	"testing"

	"orbitjob/internal/core/domain/function"
)

func TestFunctionAttemptMetadataUsesUniqueTenantFunctionLedgerMapping(t *testing.T) {
	db := OpenStoreDB(t)
	ctx := ctxStore(t)
	const tenant = "01J00000000000000000000001"
	const foreign = "01J00000000000000000000002"
	if _, err := db.ExecContext(ctx, `INSERT INTO tenants(id,slug,name)VALUES($1,'attempt-home','Home'),($2,'attempt-other','Other')`, tenant, foreign); err != nil {
		t.Fatal(err)
	}
	var fn, rev, run int64
	if err := db.QueryRowContext(ctx, `INSERT INTO functions(tenant_id,name,image)VALUES($1,'attempts','busybox')RETURNING id`, tenant).Scan(&fn); err != nil {
		t.Fatal(err)
	}
	source := function.SourceUID(fn)
	if err := db.QueryRowContext(ctx, `INSERT INTO job_definition_revisions(tenant_id,source_mode,source_uid,source_namespace,source_name,generation,spec_hash,normalized_spec,is_active,actor)VALUES($1,'function',$2,'own',$2,1,'hash','{}',true,'test')RETURNING id`, tenant, source).Scan(&rev); err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("a", 64)
	uuid := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	if err := db.QueryRowContext(ctx, `INSERT INTO job_run_control_plane(tenant_id,source_uid,revision_id,occurrence_key,trigger,actor,phase)VALUES($1,$2,$3,$4,'Function','test','Succeeded')RETURNING id`, tenant, source, rev, key).Scan(&run); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO function_runs(tenant_id,function_id,run_id,status,triggered_at,started_at,finished_at)VALUES($1,$2,$3,'success',now(),now(),now())`, tenant, fn, uuid); err != nil {
		t.Fatal(err)
	}
	reader := NewFunctionRunRepository(openRoleDB(t, db, "orbitjob_admin"))
	attempts, known, err := reader.AttemptsByRunID(ctx, tenant, fn, uuid)
	if err != nil || !known || attempts == nil || len(attempts) != 0 {
		t.Fatalf("no-attempt state %+v %v %v", attempts, known, err)
	}
	for _, number := range []int{2, 4} {
		phase := "Failed"
		if number == 4 {
			phase = "Succeeded"
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO job_run_attempts_control_plane(tenant_id,run_id,attempt_number,phase,kubernetes_job_name,started_at,completed_at)VALUES($1,$2,$3,$4,$5,now(),now())`, tenant, run, number, phase, fmt.Sprint("attempt-", number)); err != nil {
			t.Fatal(err)
		}
	}
	attempts, known, err = reader.AttemptsByRunID(ctx, tenant, fn, uuid)
	if err != nil || !known || len(attempts) != 2 || attempts[0].AttemptNumber != 2 || attempts[1].AttemptNumber != 4 || attempts[1].Phase != "Succeeded" || attempts[1].CompletedAt == nil {
		t.Fatalf("real attempts %+v %v %v", attempts, known, err)
	}
	for _, target := range []struct {
		tenant string
		fn     int64
	}{{foreign, fn}, {tenant, fn + 1}} {
		attempts, known, err = reader.AttemptsByRunID(ctx, target.tenant, target.fn, uuid)
		if err != nil || known || attempts != nil {
			t.Fatalf("foreign attempt leak %+v %v %v", attempts, known, err)
		}
	}
	// Ambiguous UUID prefixes never choose one retained execution's attempts.
	collision := key[:32] + strings.Repeat("b", 32)
	if _, err := db.ExecContext(ctx, `INSERT INTO job_run_control_plane(tenant_id,source_uid,revision_id,occurrence_key,trigger,actor,phase)VALUES($1,$2,$3,$4,'Function','test','Succeeded')`, tenant, source, rev, collision); err != nil {
		t.Fatal(err)
	}
	attempts, known, err = reader.AttemptsByRunID(ctx, tenant, fn, uuid)
	if err != nil || known || attempts != nil {
		t.Fatalf("ambiguous attempts exposed %+v %v %v", attempts, known, err)
	}
}
