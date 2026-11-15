//go:build integration

package migrations

import (
	"os"
	"testing"

	platformmigrate "orbitjob/internal/platform/migrate"
	"orbitjob/internal/platform/scan"
)

func TestOutcomeRecoveryCutoverPreservesHistoricalStatistics(t *testing.T) {
	prepareMigrationRoles(t)
	resetPublicSchema(t)
	ctx := ctx30(t)
	migrations, err := platformmigrate.Load(os.DirFS("."), ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) < 12 || migrations[11].Name != "storage_integrity" {
		t.Fatalf("expected storage integrity cutover after the Console lineage, got %d", len(migrations))
	}
	migrator := openMigrationDB(t, testRoleDSN(t, "orbitjob_migrator"))
	if _, err := platformmigrate.Execute(ctx, migrator, migrations[:11], platformmigrate.Options{}); err != nil {
		t.Fatal(err)
	}
	owner := ownerDB(t)
	tenant := scan.GenerateID()
	if _, err := owner.ExecContext(ctx, `INSERT INTO tenants(id,slug,name) VALUES($1::char(26),$1::text,$1::text)`, tenant); err != nil {
		t.Fatal(err)
	}
	var runID, sliID int64
	if err := owner.QueryRowContext(ctx, `WITH rev AS (
 INSERT INTO job_definition_revisions(tenant_id,source_mode,source_uid,source_namespace,source_name,generation,spec_hash,normalized_spec,actor)
 VALUES($1,'kubernetes','old-source','ns','job',1,'hash','{}','operator') RETURNING id)
 INSERT INTO job_run_control_plane(tenant_id,source_uid,revision_id,occurrence_key,trigger,actor,phase,attempt,max_attempts)
 SELECT $1,'old-source',id,'old-occurrence','Manual','operator','Succeeded',1,1 FROM rev RETURNING id`, tenant).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRowContext(ctx, `INSERT INTO slis(tenant_id,name,sli_type) VALUES($1,'existing-sli','availability') RETURNING id`, tenant).Scan(&sliID); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.ExecContext(ctx, `INSERT INTO sli_snapshots(tenant_id,sli_id,window_start,window_end,good_events_count,total_events_count,sli_value) VALUES($1,$2,now(),now()+interval '5 minutes',3,4,0.75)`, tenant, sliID); err != nil {
		t.Fatal(err)
	}
	if _, err := platformmigrate.Execute(ctx, migrator, migrations, platformmigrate.Options{}); err != nil {
		t.Fatal(err)
	}
	var pending, good, total, dedup int
	if err := owner.QueryRowContext(ctx, `SELECT count(*) FROM job_run_control_plane WHERE phase IN ('Succeeded','Failed','Canceled') AND outcome_recorded_at IS NULL`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Fatalf("historical terminal became pending: %d", pending)
	}
	if err := owner.QueryRowContext(ctx, `SELECT good_events_count,total_events_count FROM sli_snapshots WHERE sli_id=$1`, sliID).Scan(&good, &total); err != nil {
		t.Fatal(err)
	}
	if good != 3 || total != 4 {
		t.Fatalf("migration changed old aggregate: %d/%d", good, total)
	}
	if err := owner.QueryRowContext(ctx, `SELECT count(*) FROM sli_snapshot_events`).Scan(&dedup); err != nil {
		t.Fatal(err)
	}
	if dedup != 0 {
		t.Fatalf("invented %d historical dedup facts", dedup)
	}
	var newRun int64
	if err := owner.QueryRowContext(ctx, `INSERT INTO job_run_control_plane(tenant_id,source_uid,revision_id,occurrence_key,trigger,actor,phase,attempt,max_attempts) SELECT tenant_id,source_uid,revision_id,'new-occurrence','Manual','operator','Running',1,1 FROM job_run_control_plane WHERE id=$1 RETURNING id`, runID).Scan(&newRun); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.ExecContext(ctx, `UPDATE job_run_control_plane SET phase='Succeeded' WHERE id=$1`, newRun); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRowContext(ctx, `SELECT count(*) FROM job_run_control_plane WHERE phase IN ('Succeeded','Failed','Canceled') AND outcome_recorded_at IS NULL`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 1 {
		t.Fatalf("new terminal pending=%d, want 1", pending)
	}
}
