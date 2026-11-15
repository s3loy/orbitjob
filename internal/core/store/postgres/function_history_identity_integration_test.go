//go:build integration

package postgres

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"orbitjob/internal/core/domain/function"
)

func TestFunctionHistoryOccurrenceUsesUniqueRealLedgerParent(t *testing.T) {
	owner := OpenStoreDB(t)
	ctx := ctxStore(t)
	const tenant = "01J00000000000000000000001"
	const foreign = "01J00000000000000000000002"
	if _, err := owner.ExecContext(ctx, `INSERT INTO tenants(id,slug,name) VALUES($1,'function-history','History'),($2,'function-other','Other')`, tenant, foreign); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := owner.QueryRowContext(ctx, `INSERT INTO functions(tenant_id,name,image) VALUES($1,'history','app:v1') RETURNING id`, tenant).Scan(&id); err != nil {
		t.Fatal(err)
	}
	var revision int64
	source := function.SourceUID(id)
	if err := owner.QueryRowContext(ctx, `INSERT INTO job_definition_revisions(tenant_id,source_mode,source_uid,source_namespace,source_name,generation,spec_hash,normalized_spec,is_active,actor)
	 VALUES($1,'function',$2,'own',$2,1,'hash','{}',true,'test') RETURNING id`, tenant, source).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	reader := NewFunctionRunRepository(openRoleDB(t, owner, "orbitjob_admin"))
	for index, key := range []string{strings.Repeat("a", 64), "legacy-occurrence"} {
		digest := key
		if len(key) != 64 {
			sum := sha256.Sum256([]byte(key))
			digest = hex.EncodeToString(sum[:])
		}
		uuid := fmt.Sprintf("%s-%s-%s-%s-%s", digest[:8], digest[8:12], digest[12:16], digest[16:20], digest[20:32])
		if _, err := owner.ExecContext(ctx, `INSERT INTO job_run_control_plane(tenant_id,source_uid,revision_id,occurrence_key,trigger,actor,phase)
		 VALUES($1,$2,$3,$4,'Function','test','Succeeded')`, tenant, source, revision, key); err != nil {
			t.Fatal(err)
		}
		if _, err := owner.ExecContext(ctx, `INSERT INTO function_runs(tenant_id,function_id,run_id,status,triggered_at,started_at,finished_at)
		 VALUES($1,$2,$3,'success',now(),now(),now())`, tenant, id, uuid); err != nil {
			t.Fatal(err)
		}
		row, found, err := reader.RunByRunID(ctx, tenant, id, uuid)
		if err != nil || !found || row.OccurrenceKey == nil || *row.OccurrenceKey != key {
			t.Fatalf("mapping = %+v, found=%t, error=%v", row, found, err)
		}
		if _, found, err := reader.RunByRunID(ctx, foreign, id, uuid); err != nil || found {
			t.Fatalf("foreign tenant found=%t, error=%v", found, err)
		}
		if _, found, err := reader.RunByRunID(ctx, tenant, id+1, uuid); err != nil || found {
			t.Fatalf("foreign Function found=%t, error=%v", found, err)
		}
		if index == 0 {
			// The same UUID prefix can map to two different full occurrence
			// digests. Neither may be chosen as if that mapping were exact.
			collision := key[:32] + strings.Repeat("b", 32)
			if _, err := owner.ExecContext(ctx, `INSERT INTO job_run_control_plane(tenant_id,source_uid,revision_id,occurrence_key,trigger,actor,phase)
			 VALUES($1,$2,$3,$4,'Function','test','Succeeded')`, tenant, source, revision, collision); err != nil {
				t.Fatal(err)
			}
			row, found, err = reader.RunByRunID(ctx, tenant, id, uuid)
			if err != nil || !found || row.OccurrenceKey != nil {
				t.Fatalf("ambiguous mapping = %+v, found=%t, error=%v", row, found, err)
			}
		}
	}
	rows, err := reader.RunsByFunction(ctx, tenant, id, 10)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%+v, error=%v", rows, err)
	}
	mapped := 0
	for _, row := range rows {
		if row.OccurrenceKey != nil {
			mapped++
		}
	}
	if mapped != 1 {
		t.Fatalf("list must retain only unique real occurrence mappings: %+v", rows)
	}
}
