//go:build integration

package postgres

import (
	"errors"
	"testing"
	"time"

	"orbitjob/internal/core/domain/jobrun"
)

func TestLateObservationsPreserveAttemptAndCancellationProgress(t *testing.T) {
	db := OpenStoreDB(t)
	ctx := ctxStore(t)
	const tenant = "01J00000000000000000000001"
	if _, err := db.ExecContext(ctx, `INSERT INTO tenants(id,slug,name) VALUES($1::char(26),'late-observation','Late observation')`, tenant); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := db.QueryRowContext(ctx, `WITH revision AS (
 INSERT INTO job_definition_revisions(tenant_id,source_mode,source_uid,source_namespace,source_name,generation,spec_hash,normalized_spec,actor)
 VALUES($1::char(26),'kubernetes','late-source','tasks','nightly',1,repeat('a',64),'{}','operator') RETURNING id)
 INSERT INTO job_run_control_plane(tenant_id,source_uid,revision_id,occurrence_key,trigger,actor,phase,attempt,max_attempts)
 SELECT $1::char(26),'late-source',id,repeat('b',64),'Manual','fixture','Running',2,3 FROM revision RETURNING id`, tenant).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO job_run_attempts_control_plane(tenant_id,run_id,attempt_number,phase,kubernetes_job_name,kubernetes_job_uid,completed_at)
 VALUES($1::char(26),$2,1,'Failed','old-attempt','uid-1',now()),($1::char(26),$2,2,'Running','current-attempt','uid-2',NULL)`, tenant, id); err != nil {
		t.Fatal(err)
	}
	repo := NewControlPlaneRepository(db)
	if changed, err := repo.UpdateRunPhase(ctx, tenant, id, jobrun.RetryWaiting, 1); err != nil || changed {
		t.Fatalf("stale observation changed=%v err=%v", changed, err)
	}
	assertRun := func(want string) {
		t.Helper()
		var phase string
		var attempt int
		if err := db.QueryRowContext(ctx, `SELECT phase,attempt FROM job_run_control_plane WHERE id=$1`, id).Scan(&phase, &attempt); err != nil {
			t.Fatal(err)
		}
		if phase != want || attempt != 2 {
			t.Fatalf("run phase=%s attempt=%d, want %s/2", phase, attempt, want)
		}
	}
	assertRun("Running")
	if _, _, err := repo.UpdateAttemptPhase(ctx, tenant, "current-attempt", "CreatingAttempt", "uid-2", "late-pending"); !errors.Is(err, ErrStaleObservation) {
		t.Fatalf("late creation observation returned %v", err)
	}
	var active string
	if err := db.QueryRowContext(ctx, `SELECT phase FROM job_run_attempts_control_plane WHERE kubernetes_job_name='current-attempt'`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != "Running" {
		t.Fatalf("a late/deleting Job observation regressed an active attempt: %s", active)
	}

	for _, pending := range []jobrun.Phase{jobrun.CancelRequested, jobrun.CancelUnknown} {
		if _, err := repo.UpdateRunPhase(ctx, tenant, id, pending, 2); err != nil {
			t.Fatal(err)
		}
		if changed, err := repo.UpdateRunPhase(ctx, tenant, id, jobrun.Running, 2); err != nil || changed {
			t.Fatalf("late workload observation changed=%v err=%v", changed, err)
		}
		assertRun(string(pending))
	}
	if _, _, err := repo.UpdateAttemptPhase(ctx, tenant, "current-attempt", "Canceled", "", ""); err != nil {
		t.Fatal(err)
	}
	var completed time.Time
	if err := db.QueryRowContext(ctx, `SELECT completed_at FROM job_run_attempts_control_plane WHERE kubernetes_job_name='current-attempt'`).Scan(&completed); err != nil {
		t.Fatal(err)
	}
	for _, observation := range []string{"Running", "Succeeded", "Failed"} {
		if _, _, err := repo.UpdateAttemptPhase(ctx, tenant, "current-attempt", observation, "uid-2", "later-rv"); !errors.Is(err, ErrStaleObservation) {
			t.Fatalf("late terminal overwrite returned %v", err)
		}
		var phase string
		var after time.Time
		if err := db.QueryRowContext(ctx, `SELECT phase,completed_at FROM job_run_attempts_control_plane WHERE kubernetes_job_name='current-attempt'`).Scan(&phase, &after); err != nil {
			t.Fatal(err)
		}
		if phase != "Canceled" || !after.Equal(completed) {
			t.Fatalf("late %s rewrote a completed attempt: %s %s", observation, phase, after)
		}
	}
	if _, _, err := repo.UpdateAttemptPhase(ctx, tenant, "old-attempt", "Canceled", "", ""); !errors.Is(err, ErrStaleObservation) {
		t.Fatalf("cancellation of finished attempt returned %v", err)
	}
	var prior string
	if err := db.QueryRowContext(ctx, `SELECT phase FROM job_run_attempts_control_plane WHERE kubernetes_job_name='old-attempt'`).Scan(&prior); err != nil {
		t.Fatal(err)
	}
	if prior != "Failed" {
		t.Fatalf("cancellation erased a failed execution: %s", prior)
	}
	assertRun("CancelUnknown")
}
