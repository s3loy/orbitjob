//go:build integration

package postgres

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"orbitjob/internal/core/domain/checkrun"
	"orbitjob/internal/core/domain/function"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
	"orbitjob/internal/core/domain/sli"
	"orbitjob/internal/core/domain/slo"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/platform/scan"
)

func seedIntegrityRun(t *testing.T, phase jobrun.Phase, attempt int) (*sql.DB, string, int64) {
	t.Helper()
	db := OpenStoreDB(t)
	ctx := ctxStore(t)
	tenant := scan.GenerateID()
	if _, err := db.ExecContext(ctx, `INSERT INTO tenants(id,slug,name) VALUES($1::char(26),$1::text,$1::text)`, tenant); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := db.QueryRowContext(ctx, `WITH rev AS (
 INSERT INTO job_definition_revisions(tenant_id,source_mode,source_uid,source_namespace,source_name,generation,spec_hash,normalized_spec,actor)
 VALUES($1::char(26),'kubernetes',$1::text,'ns','job',1,repeat('a',64),'{}','operator') RETURNING id)
 INSERT INTO job_run_control_plane(tenant_id,source_uid,revision_id,occurrence_key,trigger,actor,phase,attempt,max_attempts)
 SELECT $1::char(26),$1::text,id,repeat('b',64),'Manual','operator',$2,$3,3 FROM rev RETURNING id`, tenant, phase, attempt).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return db, tenant, id
}

func TestRunObservationsCannotUndoCancellationOrNewAttempt(t *testing.T) {
	for _, tc := range []struct {
		phase           jobrun.Phase
		attempt         int
		observed        jobrun.Phase
		observedAttempt int
	}{
		{jobrun.CancelRequested, 1, jobrun.Running, 1},
		{jobrun.Running, 2, jobrun.RetryWaiting, 1},
		{jobrun.RetryWaiting, 1, jobrun.Running, 1},
	} {
		t.Run(fmt.Sprintf("%s-%d", tc.phase, tc.attempt), func(t *testing.T) {
			db, tenant, id := seedIntegrityRun(t, tc.phase, tc.attempt)
			repo := NewControlPlaneRepository(db)
			changed, err := repo.UpdateRunPhase(ctxStore(t), tenant, id, tc.observed, tc.observedAttempt)
			if err != nil || changed {
				t.Fatalf("stale update changed=%v error=%v", changed, err)
			}
			var phase string
			var attempt int
			if err := repo.db.QueryRowContext(ctxStore(t), `SELECT phase,attempt FROM job_run_control_plane WHERE id=$1`, id).Scan(&phase, &attempt); err != nil {
				t.Fatal(err)
			}
			if phase != string(tc.phase) || attempt != tc.attempt {
				t.Fatalf("persisted %s/%d", phase, attempt)
			}
		})
	}
}

func TestAttemptCreationRefusesCancellationAndNonConsecutiveAttempt(t *testing.T) {
	for _, tc := range []struct {
		phase         jobrun.Phase
		current, next int
	}{{jobrun.CancelRequested, 0, 1}, {jobrun.Pending, 0, 2}, {jobrun.Running, 2, 1}} {
		t.Run(fmt.Sprintf("%s-%d", tc.phase, tc.next), func(t *testing.T) {
			db, tenant, id := seedIntegrityRun(t, tc.phase, tc.current)
			repo := NewControlPlaneRepository(db)
			err := repo.CreateAttemptForTenant(ctxStore(t), tenant, id, jobrun.Attempt{Number: tc.next, Phase: jobrun.CreatingAttempt, KubernetesJobName: tenant, KubernetesJobUID: tenant}, jobrun.CreatingAttempt)
			if !errors.Is(err, ErrRunChanged) {
				t.Fatalf("create returned %v", err)
			}
			var n int
			if err := repo.db.QueryRowContext(ctxStore(t), `SELECT count(*) FROM job_run_attempts_control_plane WHERE run_id=$1`, id).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 0 {
				t.Fatalf("refused attempt left %d rows", n)
			}
		})
	}
}

func TestAttemptObservationsAreTerminalAndResourceVersionMonotone(t *testing.T) {
	db, tenant, id := seedIntegrityRun(t, jobrun.Running, 1)
	ctx := ctxStore(t)
	repo := NewControlPlaneRepository(db)
	if _, err := db.ExecContext(ctx, `INSERT INTO job_run_attempts_control_plane(tenant_id,run_id,attempt_number,phase,kubernetes_job_name,kubernetes_job_uid,observed_resource_version) VALUES($1::char(26),$2,1,'Running',$1::text,$1::text,'20')`, tenant, id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.UpdateAttemptPhase(ctx, tenant, tenant, "Succeeded", tenant, "19"); !errors.Is(err, ErrStaleObservation) {
		t.Fatalf("old RV: %v", err)
	}
	if _, _, err := repo.UpdateAttemptPhase(ctx, tenant, tenant, "Succeeded", tenant, "21"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.UpdateAttemptPhase(ctx, tenant, tenant, "Running", tenant, "22"); !errors.Is(err, ErrStaleObservation) {
		t.Fatalf("terminal overwrite: %v", err)
	}
	var phase, rv string
	if err := db.QueryRowContext(ctx, `SELECT phase,observed_resource_version FROM job_run_attempts_control_plane WHERE run_id=$1`, id).Scan(&phase, &rv); err != nil {
		t.Fatal(err)
	}
	if phase != "Succeeded" || rv != "21" {
		t.Fatalf("persisted phase=%s rv=%s", phase, rv)
	}
}

func TestRevisionReplayCannotActivateAnOlderGeneration(t *testing.T) {
	db, tenant, _ := seedIntegrityRun(t, jobrun.Pending, 0)
	repo := NewControlPlaneRepository(db)
	ctx := ctxStore(t)
	rev := revision.Revision{Identity: revision.Identity{SourceMode: "kubernetes", SourceUID: "new-source-" + tenant, Namespace: "ns", Name: "job"}, Generation: 2, SpecHash: "hash2", NormalizedSpec: "{}", Actor: "operator"}
	id, err := repo.ApplyRevisionForTenant(ctx, tenant, rev)
	if err != nil {
		t.Fatal(err)
	}
	rev.Generation = 1
	rev.SpecHash = "hash1"
	if _, err := repo.ApplyRevisionForTenant(ctx, tenant, rev); err != nil {
		t.Fatal(err)
	}
	var active int64
	if err := repo.db.QueryRowContext(ctx, `SELECT id FROM job_definition_revisions WHERE tenant_id=$1 AND source_uid=$2 AND is_active`, tenant, rev.Identity.SourceUID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != id {
		t.Fatalf("old generation replaced active id %d with %d", id, active)
	}
}

func TestGroupedDerivedResourcesRequireVisibleParents(t *testing.T) {
	db := OpenStoreDB(t)
	ctx := ctxStore(t)
	tenant, g1, g2 := scan.GenerateID(), scan.GenerateID(), scan.GenerateID()
	if _, err := db.ExecContext(ctx, `INSERT INTO tenants(id,slug,name) VALUES($1::char(26),$1::text,$1::text);`, tenant); err != nil {
		t.Fatal(err)
	}
	for _, g := range []string{g1, g2} {
		if _, err := db.ExecContext(ctx, `INSERT INTO resource_groups(id,tenant_id,slug,name) VALUES($1::char(26),$2::char(26),$1::text,$1::text)`, g, tenant); err != nil {
			t.Fatal(err)
		}
	}
	var checkID, sliID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO checks(tenant_id,name,resource_group_id,check_type,cron_expr,check_config) VALUES($1,'foreign-check',$2,'http_health','* * * * *','{}') RETURNING id`, tenant, g2).Scan(&checkID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO slis(tenant_id,name,resource_group_id,sli_type) VALUES($1,'foreign-sli',$2,'availability') RETURNING id`, tenant, g2).Scan(&sliID); err != nil {
		t.Fatal(err)
	}
	admin := openRoleDB(t, db, "orbitjob_admin")
	_, err := NewSLIRepository(admin).Create(ctx, tenant, sli.CreateSpec{Name: "indirect-check", SLIType: "availability", SourceType: "job_run", SourceConfig: map[string]any{"source_uid": fmt.Sprintf("check-%d", checkID)}, Aggregation: "ratio", ResourceGroupID: g1})
	var missing *resource.NotFoundError
	if !errors.As(err, &missing) {
		t.Fatalf("cross-group SLI returned %v", err)
	}
	_, err = NewSLORepository(admin).Create(ctx, tenant, slo.CreateSpec{Name: "indirect-sli", SLIID: sliID, Target: .99, WindowType: "rolling", WindowDuration: time.Hour, ResourceGroupID: g1})
	if !errors.As(err, &missing) {
		t.Fatalf("cross-group SLO returned %v", err)
	}
	if _, err := NewSLIRepository(admin).Create(ctx, tenant, sli.CreateSpec{Name: "same-group-check", SLIType: "availability", SourceType: "job_run", SourceConfig: map[string]any{"source_uid": fmt.Sprintf("check-%d", checkID)}, Aggregation: "ratio", ResourceGroupID: g2}); err != nil {
		t.Fatalf("same-group SLI: %v", err)
	}
	if _, err := NewSLORepository(admin).Create(ctx, tenant, slo.CreateSpec{Name: "same-group-sli", SLIID: sliID, Target: .99, WindowType: "rolling", WindowDuration: time.Hour, ResourceGroupID: g2}); err != nil {
		t.Fatalf("same-group SLO: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO job_definition_revisions(tenant_id,source_mode,source_uid,source_namespace,source_name,generation,spec_hash,normalized_spec,actor,is_active) VALUES($1,'kubernetes','plain-job','ns','plain',1,'hash','{}','operator',true)`, tenant); err != nil {
		t.Fatal(err)
	}
	_, err = NewSLIRepository(admin).Create(ctx, tenant, sli.CreateSpec{Name: "groupless-job", SLIType: "availability", SourceType: "job_run", SourceConfig: map[string]any{"source_uid": "plain-job"}, Aggregation: "ratio", ResourceGroupID: g1})
	if !errors.As(err, &missing) {
		t.Fatalf("group-scoped link to groupless Job: %v", err)
	}

}

func TestOutcomeReplayIsIdempotentAndRetentionWaitsForAcknowledgement(t *testing.T) {
	db, tenant, id := seedIntegrityRun(t, jobrun.Succeeded, 1)
	ctx := ctxStore(t)
	repo := NewControlPlaneRepository(db)
	pending, err := repo.TerminalOutcomesPending(ctx, tenant, 10)
	if err != nil || len(pending) != 1 || pending[0].RunID != id {
		t.Fatalf("pending=%v error=%v", pending, err)
	}
	if deleted, err := repo.DeleteRun(ctx, tenant, id); err != nil || deleted {
		t.Fatalf("unprojected prune deleted=%v error=%v", deleted, err)
	}
	var sliID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO slis(tenant_id,name,sli_type) VALUES($1,'run-sli','availability') RETURNING id`, tenant).Scan(&sliID); err != nil {
		t.Fatal(err)
	}
	snapshots := NewSLISnapshotRepository(openRoleDB(t, db, "orbitjob_runtime"))
	window := time.Now().UTC().Truncate(5 * time.Minute)
	for n := 0; n < 2; n++ {
		created, err := snapshots.IncrementSnapshotForRun(ctx, tenant, id, sliID, window, true)
		if err != nil || created != (n == 0) {
			t.Fatalf("increment %d created=%v error=%v", n, created, err)
		}
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT total_events_count FROM sli_snapshots WHERE tenant_id=$1 AND sli_id=$2`, tenant, sliID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("duplicate replay incremented %d", count)
	}
	if err := repo.MarkOutcomeRecorded(ctx, tenant, id); err != nil {
		t.Fatal(err)
	}
	if pending, err := repo.TerminalOutcomesPending(ctx, tenant, 10); err != nil || len(pending) != 0 {
		t.Fatalf("ack pending=%v error=%v", pending, err)
	}
	if deleted, err := repo.DeleteRun(ctx, tenant, id); err != nil || deleted {
		t.Fatalf("new terminal run without archive lost collection grace: %v %v", deleted, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE job_run_control_plane SET updated_at=now()-interval '16 days' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if deleted, err := repo.DeleteRun(ctx, tenant, id); err != nil || !deleted {
		t.Fatalf("ack prune deleted=%v error=%v", deleted, err)
	}
}

func TestSourceDeactivationPreservesPinnedRevisionAndRetention(t *testing.T) {
	db, tenant, _ := seedIntegrityRun(t, jobrun.Pending, 0)
	ctx := ctxStore(t)
	repo := NewControlPlaneRepository(db)
	rev := revision.Revision{Identity: revision.Identity{SourceMode: "kubernetes", SourceUID: "deleted-uid", Namespace: "ns", Name: "same-name"}, Generation: 1, SpecHash: "hash", NormalizedSpec: "{}", Actor: "operator"}
	oldID, err := repo.ApplyRevisionForTenant(ctx, tenant, rev)
	if err != nil {
		t.Fatal(err)
	}
	rev.Identity.SourceUID = "replacement-uid"
	newID, err := repo.ApplyRevisionForTenant(ctx, tenant, rev)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.DeactivateSource(ctx, tenant, "kubernetes", "deleted-uid"); err != nil {
		t.Fatal(err)
	}
	var oldActive, newActive bool
	if err := db.QueryRowContext(ctx, `SELECT is_active FROM job_definition_revisions WHERE id=$1`, oldID).Scan(&oldActive); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT is_active FROM job_definition_revisions WHERE id=$1`, newID).Scan(&newActive); err != nil {
		t.Fatal(err)
	}
	if oldActive || !newActive {
		t.Fatalf("old active=%v replacement active=%v", oldActive, newActive)
	}
	retained, err := repo.RetentionRevisions(ctx, tenant)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, rev := range retained {
		found = found || rev.ID == oldID
	}
	if !found {
		t.Fatal("inactive revision omitted from retention")
	}
	if err := repo.DeactivateSourceByName(ctx, tenant, "kubernetes", "ns", "same-name"); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeactivateSourcesMissing(ctx, tenant, "kubernetes", nil); err != nil {
		t.Fatal(err)
	}
	active, err := repo.ActiveRevisions(ctx, tenant)
	if err != nil || len(active) != 0 {
		t.Fatalf("active=%v error=%v", active, err)
	}
	var audits int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE tenant_id=$1 AND event_type='revision.deactivated'`, tenant).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 2 {
		t.Fatalf("deactivation audits=%d, want 2", audits)
	}
}

func TestCompletedReadModelsReportDuplicateAsSuccess(t *testing.T) {
	db, tenant, _ := seedIntegrityRun(t, jobrun.Pending, 0)
	ctx := ctxStore(t)
	var checkID, functionID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO checks(tenant_id,name,check_type,cron_expr) VALUES($1,'completion-check','http_health','* * * * *') RETURNING id`, tenant).Scan(&checkID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO functions(tenant_id,name,image) VALUES($1,'completion-function','busybox') RETURNING id`, tenant).Scan(&functionID); err != nil {
		t.Fatal(err)
	}
	runtime := openRoleDB(t, db, "orbitjob_runtime")
	now := time.Now().UTC()
	c := checkrun.CompletedRecord{RunID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", CheckID: checkID, Status: checkrun.StatusSuccess, ScheduledAt: now, StartedAt: now, FinishedAt: now}
	f := function.CompletedRecord{RunID: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", FunctionID: functionID, Status: function.StatusSuccess, TriggeredAt: now, StartedAt: now, FinishedAt: now}
	for n := 0; n < 2; n++ {
		if created, err := NewCheckRunRepository(runtime).RecordCompletedOnce(ctx, tenant, c); err != nil || created != (n == 0) {
			t.Fatalf("check n=%d created=%v error=%v", n, created, err)
		}
		if created, err := NewFunctionRunRepository(runtime).RecordCompletedOnce(ctx, tenant, f); err != nil || created != (n == 0) {
			t.Fatalf("function n=%d created=%v error=%v", n, created, err)
		}
	}
}

func TestCancelUnknownRetentionPreservesIsolation(t *testing.T) {
	db, tenant, id := seedIntegrityRun(t, jobrun.CancelUnknown, 1)
	ctx := ctxStore(t)
	repo := NewControlPlaneRepository(db)
	// Even a historical projection acknowledgement cannot turn an uncertain
	// stop into a terminal cleanup candidate or release the source's open slot.
	if _, err := db.ExecContext(ctx, `UPDATE job_run_control_plane SET outcome_recorded_at=now() WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	runs, err := repo.PrunableRuns(ctx, tenant, tenant, 0, 0)
	if err != nil || len(runs) != 0 {
		t.Fatalf("retention candidates=%v error=%v", runs, err)
	}
	if deleted, err := repo.DeleteRun(ctx, tenant, id); err != nil || deleted {
		t.Fatalf("CancelUnknown deleted=%v error=%v", deleted, err)
	}
	if count, err := repo.CountOpenRuns(ctx, tenant, tenant); err != nil || count != 1 {
		t.Fatalf("uncertain stop released open slot: count=%d err=%v", count, err)
	}
}
