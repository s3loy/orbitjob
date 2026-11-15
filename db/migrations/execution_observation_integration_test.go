//go:build integration

package migrations

import (
	"database/sql"
	"os"
	"testing"

	platformmigrate "orbitjob/internal/platform/migrate"
	"orbitjob/internal/platform/scan"
)

func TestExecutionObservationUpgradePreservesUnknownHistoricalIdentity(t *testing.T) {
	prepareMigrationRoles(t)
	resetPublicSchema(t)
	ctx := ctx30(t)
	all, err := platformmigrate.Load(os.DirFS("."), ".")
	if err != nil {
		t.Fatal(err)
	}
	cutover := -1
	for i, migration := range all {
		if migration.Name == "execution_observation" {
			cutover = i
			break
		}
	}
	if cutover < 1 {
		t.Fatal("execution observation migration not found")
	}
	migrator := openMigrationDB(t, testRoleDSN(t, "orbitjob_migrator"))
	if _, err := platformmigrate.Execute(ctx, migrator, all[:cutover], platformmigrate.Options{}); err != nil {
		t.Fatal(err)
	}
	owner := ownerDB(t)
	tenant := scan.GenerateID()
	if _, err := owner.ExecContext(ctx, `INSERT INTO tenants(id,slug,name) VALUES($1::char(26),$1::text,$1::text)`, tenant); err != nil {
		t.Fatal(err)
	}
	var attemptID int64
	if err := owner.QueryRowContext(ctx, `WITH revision AS (
 INSERT INTO job_definition_revisions(tenant_id,source_mode,source_uid,source_namespace,source_name,generation,spec_hash,normalized_spec,actor)
 VALUES($1,'kubernetes','historical-source','jobs','task',1,'hash','{}','operator') RETURNING id), run AS (
 INSERT INTO job_run_control_plane(tenant_id,source_uid,revision_id,occurrence_key,trigger,actor,phase,attempt,max_attempts)
 SELECT $1,'historical-source',id,'historical-occurrence','Manual','operator','Running',1,2 FROM revision RETURNING id)
 INSERT INTO job_run_attempts_control_plane(tenant_id,run_id,attempt_number,phase,kubernetes_job_name,kubernetes_job_uid)
 SELECT $1,id,1,'Running','historical-job','original-job-uid' FROM run RETURNING id`, tenant).Scan(&attemptID); err != nil {
		t.Fatal(err)
	}
	if _, err := platformmigrate.Execute(ctx, migrator, all, platformmigrate.Options{}); err != nil {
		t.Fatal(err)
	}
	var ownerUID, reason sql.NullString
	var since sql.NullTime
	var phase, jobUID string
	if err := owner.QueryRowContext(ctx, `SELECT owner_run_uid,execution_unknown_since,execution_unknown_reason,phase,kubernetes_job_uid FROM job_run_attempts_control_plane WHERE id=$1`, attemptID).Scan(&ownerUID, &since, &reason, &phase, &jobUID); err != nil {
		t.Fatal(err)
	}
	if ownerUID.Valid || since.Valid || reason.Valid || phase != "Running" || jobUID != "original-job-uid" {
		t.Fatalf("migration invented or rewrote historical evidence: %v %v %v %s %s", ownerUID, since, reason, phase, jobUID)
	}
	var canRead, canWrite bool
	if err := owner.QueryRowContext(ctx, `SELECT has_table_privilege('orbitjob_admin','job_run_attempts_control_plane','SELECT'),has_table_privilege('orbitjob_admin','job_run_attempts_control_plane','UPDATE')`).Scan(&canRead, &canWrite); err != nil {
		t.Fatal(err)
	}
	if !canRead || canWrite {
		t.Fatal("migration changed Admin execution privileges")
	}
	if _, err := owner.ExecContext(ctx, `UPDATE job_run_attempts_control_plane SET execution_unknown_reason='Invented' WHERE id=$1`, attemptID); err == nil {
		t.Fatal("unknown reason is not bounded")
	}
}
