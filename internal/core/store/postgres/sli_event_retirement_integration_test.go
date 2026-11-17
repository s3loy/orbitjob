//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"orbitjob/internal/core/app/slimaintenance"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/sli"
	"orbitjob/internal/platform/scan"
)

type sliRetirementFixture struct {
	owner, operator *sql.DB
	tenant          string
	runID           int64
	sliID           int64
	evaluation      sli.Evaluation
}

func seedSLIRetirement(t *testing.T, missing bool) sliRetirementFixture {
	t.Helper()
	owner, tenant, runID := seedIntegrityRun(t, jobrun.Pending, 0)
	admin := openRoleDB(t, owner, "orbitjob_admin")
	snapshot, err := NewSLIRepository(admin).Create(monitoringActorContext(t.Context()), tenant, sli.CreateSpec{Name: "retirement-measurement", SLIType: sli.TypeAvailability, SourceType: "job_run", SourceConfig: map[string]any{"source_uid": tenant}, Aggregation: "ratio", GoodEventCriteria: map[string]any{"status": "success"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.ExecContext(t.Context(), `UPDATE job_run_control_plane SET phase='Succeeded' WHERE id=$1`, runID); err != nil {
		t.Fatal(err)
	}
	operator := openRoleDB(t, owner, "orbitjob_runtime")
	plan, err := NewSLIRepository(operator).EventConfigurationForOutcome(t.Context(), tenant, runID)
	if err != nil || len(plan.Configurations) != 1 {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	good := true
	e := sli.Evaluation{RunID: runID, SLIID: snapshot.ID, ConfigurationVersion: 1, Semantics: plan.Semantics, EventTime: plan.EventTime, TimeQuality: plan.TimeQuality, WindowStart: plan.EventTime.UTC().Truncate(defaultSnapshotWindow), Good: &good}
	if missing {
		e.Good = nil
		e.MissingReason = "task_timing_unavailable"
	}
	if created, err := NewSLISnapshotRepository(operator).RecordEvaluation(t.Context(), tenant, e); err != nil || !created {
		t.Fatalf("created=%t err=%v", created, err)
	}
	f := sliRetirementFixture{owner: owner, operator: operator, tenant: tenant, runID: runID, sliID: snapshot.ID, evaluation: e}
	f.age(t, 31)
	return f
}

func (f sliRetirementFixture) age(t *testing.T, days int) {
	t.Helper()
	for _, table := range []string{"sli_event_plans", "sli_snapshot_events"} {
		if _, err := f.owner.ExecContext(t.Context(), `UPDATE `+table+` SET created_at=clock_timestamp()-($2::int*interval '1 day') WHERE tenant_id=$1`, f.tenant, days); err != nil {
			t.Fatal(err)
		}
	}
}
func (f sliRetirementFixture) removeRun(t *testing.T) {
	t.Helper()
	if _, err := f.owner.ExecContext(t.Context(), `DELETE FROM job_run_control_plane WHERE id=$1`, f.runID); err != nil {
		t.Fatal(err)
	}
}
func (f sliRetirementFixture) count(t *testing.T, table string) int {
	t.Helper()
	var count int
	if err := f.owner.QueryRowContext(t.Context(), `SELECT count(*) FROM `+table+` WHERE tenant_id=$1`, f.tenant).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
func (f sliRetirementFixture) prune(t *testing.T) slimaintenance.Page {
	t.Helper()
	page, err := NewSLIRepository(f.operator).PruneRetiredEventsPage(t.Context(), f.tenant, time.Now().Add(24*time.Hour), slimaintenance.Cursor{}, 128)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func (f sliRetirementFixture) emptyRetiredPlan(t *testing.T, tenant string) int64 {
	t.Helper()
	source := scan.GenerateID()
	var runID int64
	if err := f.owner.QueryRowContext(t.Context(), `WITH revision AS (
 INSERT INTO job_definition_revisions(tenant_id,source_mode,source_uid,source_namespace,source_name,generation,spec_hash,normalized_spec,actor)
 VALUES($1,'kubernetes',$2,'sli-retirement','empty-plan',1,repeat('c',64),'{}','test') RETURNING id)
 INSERT INTO job_run_control_plane(tenant_id,source_uid,revision_id,occurrence_key,trigger,actor,phase,max_attempts)
 SELECT $1,$2,id,repeat('d',64),'Manual','test','Succeeded',1 FROM revision RETURNING id`, tenant, source).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	plan, err := NewSLIRepository(f.operator).EventConfigurationForOutcome(t.Context(), tenant, runID)
	if err != nil || len(plan.Configurations) != 0 {
		t.Fatalf("empty plan=%+v err=%v", plan, err)
	}
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE sli_event_plans SET created_at=now()-interval '31 days' WHERE tenant_id=$1 AND run_id=$2`, tenant, runID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.owner.ExecContext(t.Context(), `DELETE FROM job_run_control_plane WHERE tenant_id=$1 AND id=$2`, tenant, runID); err != nil {
		t.Fatal(err)
	}
	return runID
}

func TestSLIRetirementPreservesPendingIncompleteAndLateFacts(t *testing.T) {
	for _, kind := range []string{"pending_run", "incomplete_plan", "young_plan", "late_fact", "recent_missing", "missing_without_event_time"} {
		t.Run(kind, func(t *testing.T) {
			missing := kind == "recent_missing" || kind == "missing_without_event_time"
			f := seedSLIRetirement(t, missing)
			if kind != "pending_run" {
				f.removeRun(t)
			}
			switch kind {
			case "incomplete_plan":
				if _, err := f.owner.ExecContext(t.Context(), `DELETE FROM sli_snapshot_events WHERE tenant_id=$1`, f.tenant); err != nil {
					t.Fatal(err)
				}
			case "young_plan":
				if _, err := f.owner.ExecContext(t.Context(), `UPDATE sli_event_plans SET created_at=now()-interval '29 days' WHERE tenant_id=$1`, f.tenant); err != nil {
					t.Fatal(err)
				}
			case "late_fact":
				if _, err := f.owner.ExecContext(t.Context(), `UPDATE sli_snapshot_events SET created_at=now() WHERE tenant_id=$1`, f.tenant); err != nil {
					t.Fatal(err)
				}
			case "missing_without_event_time":
				if _, err := f.owner.ExecContext(t.Context(), `UPDATE sli_snapshot_events SET event_time=NULL WHERE tenant_id=$1`, f.tenant); err != nil {
					t.Fatal(err)
				}
			}
			page := f.prune(t)
			if page.Counts.RemovedPlans != 0 || page.Counts.RemovedEvents != 0 || f.count(t, "sli_event_plans") != 1 {
				t.Fatalf("unsafe retirement: %+v", page)
			}
			if kind == "recent_missing" {
				agg, err := NewSLISnapshotRepository(f.operator).AggregateWindow(t.Context(), f.tenant, f.sliID, time.Now().Add(-8760*time.Hour), time.Now().Add(time.Hour))
				if err != nil || agg.MissingReason != "task_timing_unavailable" {
					t.Fatalf("missing evidence lost: %+v %v", agg, err)
				}
			}
		})
	}
}

func TestSLIRetirementKeepsCountersAndRejectsDelayedReplay(t *testing.T) {
	f := seedSLIRetirement(t, false)
	f.removeRun(t)
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE tenants SET status='suspended' WHERE id=$1`, f.tenant); err != nil {
		t.Fatal(err)
	}
	foreign := scan.GenerateID()
	if _, err := f.owner.ExecContext(t.Context(), `INSERT INTO tenants(id,slug,name)VALUES($1::char(26),$1::text,$1::text)`, foreign); err != nil {
		t.Fatal(err)
	}
	f.emptyRetiredPlan(t, foreign)
	page := f.prune(t)
	if page.Counts.RemovedPlans != 1 || page.Counts.RemovedEvents != 1 || f.count(t, "sli_event_plans") != 0 || f.count(t, "sli_snapshot_events") != 0 || f.count(t, "sli_event_plan_members") != 0 {
		t.Fatalf("retirement=%+v", page)
	}
	if created, err := NewSLISnapshotRepository(f.operator).RecordEvaluation(t.Context(), f.tenant, f.evaluation); err == nil || created {
		t.Fatalf("retired plan recreated: created=%t err=%v", created, err)
	}
	if created, err := NewSLISnapshotRepository(f.operator).IncrementSnapshotForRun(t.Context(), f.tenant, f.runID, f.sliID, f.evaluation.WindowStart, true); err != nil || created {
		t.Fatalf("compatibility replay incremented: created=%t err=%v", created, err)
	}
	var total, foreignPlans int
	if err := f.owner.QueryRowContext(t.Context(), `SELECT total_events_count FROM sli_snapshots WHERE tenant_id=$1`, f.tenant).Scan(&total); err != nil || total != 1 {
		t.Fatalf("total=%d err=%v", total, err)
	}
	if err := f.owner.QueryRowContext(t.Context(), `SELECT count(*) FROM sli_event_plans WHERE tenant_id=$1`, foreign).Scan(&foreignPlans); err != nil || foreignPlans != 1 {
		t.Fatalf("foreign plans=%d err=%v", foreignPlans, err)
	}
}

func TestSLIRetirementMissingEvidenceExpiresOnlyOutsideMaximumWindow(t *testing.T) {
	f := seedSLIRetirement(t, true)
	f.removeRun(t)
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE sli_snapshot_events SET event_time=clock_timestamp()-interval '366 days' WHERE tenant_id=$1`, f.tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE sli_event_plans SET event_time=(SELECT event_time FROM sli_snapshot_events WHERE tenant_id=$1 LIMIT 1) WHERE tenant_id=$1`, f.tenant); err != nil {
		t.Fatal(err)
	}
	olderReader, err := NewSLIRepository(f.operator).PruneRetiredEventsPage(t.Context(), f.tenant, time.Now().Add(-48*time.Hour), slimaintenance.Cursor{}, 128)
	if err != nil || olderReader.Counts.RemovedPlans != 0 {
		t.Fatalf("reader horizon ignored: page=%+v err=%v", olderReader, err)
	}
	page := f.prune(t)
	if page.Counts.RemovedPlans != 1 || page.Counts.RemovedEvents != 1 {
		t.Fatalf("retirement=%+v", page)
	}
}

func TestSLIRetirementCursorRotatesPastProtectedPlan(t *testing.T) {
	f := seedSLIRetirement(t, false)
	f.emptyRetiredPlan(t, f.tenant)
	repo := NewSLIRepository(f.operator)
	first, err := repo.PruneRetiredEventsPage(t.Context(), f.tenant, time.Now(), slimaintenance.Cursor{}, 1)
	if err != nil || first.Counts.RemovedPlans != 0 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := repo.PruneRetiredEventsPage(t.Context(), f.tenant, time.Now(), first.Next, 1)
	if err != nil || second.Counts.RemovedPlans != 1 {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	third, err := repo.PruneRetiredEventsPage(t.Context(), f.tenant, time.Now(), second.Next, 1)
	if err != nil || third.Next.PlanRunID != f.runID || third.Counts.ScannedPlans != 1 {
		t.Fatalf("wrap=%+v err=%v", third, err)
	}
}

func maintenanceRoleSession(t *testing.T, f sliRetirementFixture) (*sql.DB, int) {
	t.Helper()
	db := openRoleDB(t, f.owner, "orbitjob_runtime")
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.ExecContext(t.Context(), `SET default_transaction_isolation='repeatable read'`); err != nil {
		t.Fatal(err)
	}
	var pid int
	if err := db.QueryRowContext(t.Context(), `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	return db, pid
}

func TestSLIRetirementSerializesPlannedReplayAcrossConnections(t *testing.T) {
	f := seedSLIRetirement(t, false)
	f.removeRun(t)
	pruner, prunerPID := maintenanceRoleSession(t, f)
	writer, writerPID := maintenanceRoleSession(t, f)
	barrier, err := f.owner.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer barrier.Rollback()
	if _, err := barrier.ExecContext(t.Context(), `SELECT orbitjob_lock_sli_source($1,$2)`, f.tenant, f.tenant); err != nil {
		t.Fatal(err)
	}
	pruned, replayed := make(chan error, 1), make(chan error, 1)
	go func() {
		page, err := NewSLIRepository(pruner).PruneRetiredEventsPage(t.Context(), f.tenant, time.Now(), slimaintenance.Cursor{}, 128)
		if err == nil && page.Counts.RemovedPlans != 1 {
			err = fmt.Errorf("retirement=%+v", page)
		}
		pruned <- err
	}()
	waitControlSessionLock(t, f.owner, prunerPID, pruned)
	go func() {
		created, err := NewSLISnapshotRepository(writer).RecordEvaluation(t.Context(), f.tenant, f.evaluation)
		if err == nil || created {
			replayed <- fmt.Errorf("stale evaluation accepted: created=%t err=%v", created, err)
			return
		}
		replayed <- nil
	}()
	waitControlSessionLock(t, f.owner, writerPID, replayed)
	if err := barrier.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, done := range []<-chan error{pruned, replayed} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("retirement race did not drain")
		}
	}
	var total int
	if err := f.owner.QueryRowContext(t.Context(), `SELECT total_events_count FROM sli_snapshots WHERE tenant_id=$1`, f.tenant).Scan(&total); err != nil || total != 1 {
		t.Fatalf("total=%d err=%v", total, err)
	}
}

func TestSLIRetirementLegacyReplayWaitsForFreshRunExistenceCheck(t *testing.T) {
	f := seedSLIRetirement(t, false)
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE sli_snapshot_events SET semantics='legacy_processing_v1',plan_run_id=NULL WHERE tenant_id=$1`, f.tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := f.owner.ExecContext(t.Context(), `DELETE FROM sli_event_plans WHERE tenant_id=$1`, f.tenant); err != nil {
		t.Fatal(err)
	}
	writer, pid := maintenanceRoleSession(t, f)
	barrier, err := f.owner.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer barrier.Rollback()
	if err := lockSLIEventRun(t.Context(), barrier, f.tenant, f.runID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		created, err := NewSLISnapshotRepository(writer).IncrementSnapshotForRun(t.Context(), f.tenant, f.runID, f.sliID, f.evaluation.WindowStart, true)
		if err == nil && created {
			err = fmt.Errorf("legacy stale replay incremented")
		}
		done <- err
	}()
	waitControlSessionLock(t, f.owner, pid, done)
	f.removeRun(t)
	if _, err := barrier.ExecContext(t.Context(), `DELETE FROM sli_snapshot_events WHERE tenant_id=$1`, f.tenant); err != nil {
		t.Fatal(err)
	}
	if err := barrier.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("legacy replay did not drain")
	}
	if f.count(t, "sli_snapshot_events") != 0 {
		t.Fatal("legacy event recreated")
	}
}

func TestSLIRetirementLockTimeoutReportsFailureAndAdvancesCursor(t *testing.T) {
	f := seedSLIRetirement(t, false)
	f.removeRun(t)
	barrier, err := f.owner.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer barrier.Rollback()
	if _, err := barrier.ExecContext(t.Context(), `SELECT orbitjob_lock_sli_source($1,$2)`, f.tenant, f.tenant); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	page, err := NewSLIRepository(f.operator).PruneRetiredEventsPage(ctx, f.tenant, time.Now(), slimaintenance.Cursor{}, 128)
	if err == nil || page.Next.PlanRunID != f.runID || page.Counts.RemovedPlans != 0 || f.count(t, "sli_event_plans") != 1 {
		t.Fatalf("timeout hidden: page=%+v err=%v", page, err)
	}
}

func TestSLIRetirementLegacyFactsRetainMissingEvidenceAndUnknownVersions(t *testing.T) {
	for _, kind := range []string{"evaluated", "recent_missing", "old_missing", "unknown_missing_time", "unknown_version"} {
		t.Run(kind, func(t *testing.T) {
			f := seedSLIRetirement(t, kind != "evaluated" && kind != "unknown_version")
			if _, err := f.owner.ExecContext(t.Context(), `UPDATE sli_snapshot_events SET semantics='legacy_processing_v1',plan_run_id=NULL WHERE tenant_id=$1`, f.tenant); err != nil {
				t.Fatal(err)
			}
			if _, err := f.owner.ExecContext(t.Context(), `DELETE FROM sli_event_plans WHERE tenant_id=$1`, f.tenant); err != nil {
				t.Fatal(err)
			}
			f.removeRun(t)
			switch kind {
			case "old_missing":
				if _, err := f.owner.ExecContext(t.Context(), `UPDATE sli_snapshot_events SET event_time=now()-interval '366 days' WHERE tenant_id=$1`, f.tenant); err != nil {
					t.Fatal(err)
				}
			case "unknown_missing_time":
				if _, err := f.owner.ExecContext(t.Context(), `UPDATE sli_snapshot_events SET event_time=NULL WHERE tenant_id=$1`, f.tenant); err != nil {
					t.Fatal(err)
				}
			case "unknown_version":
				if _, err := f.owner.ExecContext(t.Context(), `INSERT INTO sli_configuration_versions SELECT tenant_id,sli_id,2,definition_json,effective_from,effective_until FROM sli_configuration_versions WHERE tenant_id=$1`, f.tenant); err != nil {
					t.Fatal(err)
				}
				if _, err := f.owner.ExecContext(t.Context(), `UPDATE sli_snapshot_events SET configuration_version=2 WHERE tenant_id=$1`, f.tenant); err != nil {
					t.Fatal(err)
				}
			}
			page := f.prune(t)
			want := 0
			if kind == "evaluated" || kind == "old_missing" {
				want = 1
			}
			if page.Counts.RemovedPlans != 0 || page.Counts.RemovedEvents != want || f.count(t, "sli_snapshot_events") != 1-want {
				t.Fatalf("retirement=%+v want=%d", page, want)
			}
		})
	}
}

func TestSLIRetirementRejectsMismatchedConfigurationAndRollsBackDeletionFailure(t *testing.T) {
	for _, kind := range []string{"mismatched_configuration", "delete_failure"} {
		t.Run(kind, func(t *testing.T) {
			f := seedSLIRetirement(t, false)
			f.removeRun(t)
			if kind == "mismatched_configuration" {
				if _, err := f.owner.ExecContext(t.Context(), `INSERT INTO sli_configuration_versions SELECT tenant_id,sli_id,2,definition_json,effective_from,effective_until FROM sli_configuration_versions WHERE tenant_id=$1`, f.tenant); err != nil {
					t.Fatal(err)
				}
				if _, err := f.owner.ExecContext(t.Context(), `UPDATE sli_snapshot_events SET configuration_version=2 WHERE tenant_id=$1`, f.tenant); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := f.owner.ExecContext(t.Context(), `CREATE FUNCTION reject_sli_plan_delete() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'retirement rollback injection'; END $$;
 CREATE TRIGGER reject_sli_plan_delete BEFORE DELETE ON sli_event_plans FOR EACH ROW EXECUTE FUNCTION reject_sli_plan_delete()`); err != nil {
					t.Fatal(err)
				}
			}
			page, err := NewSLIRepository(f.operator).PruneRetiredEventsPage(t.Context(), f.tenant, time.Now(), slimaintenance.Cursor{}, 128)
			if (kind == "delete_failure") != (err != nil) || page.Counts.RemovedPlans != 0 || page.Counts.RemovedEvents != 0 || f.count(t, "sli_event_plans") != 1 || f.count(t, "sli_snapshot_events") != 1 {
				t.Fatalf("non-atomic retirement=%+v err=%v", page, err)
			}
		})
	}
}

func TestSLIRetirementSerializesFirstLegacyPlanWithFactRetirement(t *testing.T) {
	f := seedSLIRetirement(t, false)
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE sli_snapshot_events SET semantics='legacy_processing_v1',plan_run_id=NULL WHERE tenant_id=$1`, f.tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := f.owner.ExecContext(t.Context(), `DELETE FROM sli_event_plans WHERE tenant_id=$1`, f.tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_run_control_plane SET sli_semantics_version=1 WHERE tenant_id=$1`, f.tenant); err != nil {
		t.Fatal(err)
	}
	planner, plannerPID := maintenanceRoleSession(t, f)
	pruner, prunerPID := maintenanceRoleSession(t, f)
	barrier, err := f.owner.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer barrier.Rollback()
	if err := lockSLIEventRun(t.Context(), barrier, f.tenant, f.runID); err != nil {
		t.Fatal(err)
	}
	planned, pruned := make(chan error, 1), make(chan error, 1)
	go func() {
		_, err := NewSLIRepository(planner).EventConfigurationForOutcome(t.Context(), f.tenant, f.runID)
		if err == nil {
			planned <- fmt.Errorf("plan recreated after its original Run retired")
			return
		}
		planned <- nil
	}()
	waitControlSessionLock(t, f.owner, plannerPID, planned)
	f.removeRun(t)
	go func() {
		page, err := NewSLIRepository(pruner).PruneRetiredEventsPage(t.Context(), f.tenant, time.Now(), slimaintenance.Cursor{}, 128)
		if err == nil && page.Counts.RemovedEvents != 1 {
			err = fmt.Errorf("retirement=%+v", page)
		}
		pruned <- err
	}()
	waitControlSessionLock(t, f.owner, prunerPID, pruned)
	if err := barrier.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, done := range []<-chan error{planned, pruned} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("legacy first-plan race did not drain")
		}
	}
	if f.count(t, "sli_event_plans") != 0 || f.count(t, "sli_snapshot_events") != 0 {
		t.Fatal("retired legacy replay details recreated")
	}
}
