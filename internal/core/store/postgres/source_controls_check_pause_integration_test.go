//go:build integration

package postgres

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/app/checkschedule"
	"orbitjob/internal/core/app/projection"
	"orbitjob/internal/core/domain/check"
	"orbitjob/internal/core/domain/coordination"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/workflow"
	"orbitjob/internal/domain/sourcecontrol"
	"orbitjob/internal/platform/scan"
)

type checkPausePublisher struct{ objects []v1alpha1.JobRun }

func (p *checkPausePublisher) Publish(_ context.Context, obj v1alpha1.JobRun) error {
	p.objects = append(p.objects, obj)
	return nil
}

func TestCheckSchedulePauseStopsTimerAndPreservesAcceptedRecovery(t *testing.T) {
	owner := OpenStoreDB(t)
	tenant := scan.GenerateID()
	if _, err := owner.ExecContext(t.Context(), `INSERT INTO tenants(id,slug,name) VALUES($1::char(26),$1::text,'Check schedule pause')`, tenant); err != nil {
		t.Fatal(err)
	}
	runtimeDB := openRoleDB(t, owner, "orbitjob_runtime")
	checks := NewCheckRepository(runtimeDB)
	control := NewControlPlaneRepository(runtimeDB)
	interval := 60
	planned := time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)
	c, err := NewCheckRepository(owner).Create(monitoringActorContext(t.Context()), check.CreateSpec{
		Name: "schedule-pause", TenantID: tenant, CheckType: check.CheckTypeHTTPHealth,
		CheckConfig:    map[string]any{"url": "https://example.com/health", "method": "GET", "expected_status": 200},
		AssertionRules: []check.AssertionRule{}, Labels: map[string]any{},
		ScheduleType: check.ScheduleTypeInterval, IntervalSec: &interval, Timezone: "UTC", TimeoutSec: 5,
		NextRunAt: &planned,
	})
	if err != nil {
		t.Fatal(err)
	}
	revisionID, err := (projection.Service{Revisions: control}).ApplyCheck(t.Context(), c, "checks", tenant, checkschedule.ActorCheckScheduler)
	if err != nil {
		t.Fatal(err)
	}
	publisher := &checkPausePublisher{}
	tick := checkschedule.NewTickUseCase(checks, control, control, publisher)
	tick.Now = func() time.Time { return planned }
	scope := checkschedule.Scope{TenantID: tenant, Namespace: "checks"}
	if created, err := tick.RunBatch(t.Context(), scope, 1); err != nil || created != 1 {
		t.Fatalf("initial timer created=%d err=%v", created, err)
	}
	sourceUID := check.CheckSourceUID(c.ID)
	occurrence := jobrun.JobRun{
		SourceUID: sourceUID, RevisionID: strconv.FormatInt(revisionID, 10),
		OccurrenceKey: coordination.OccurrenceKey(sourceUID, revisionID, planned),
		Trigger:       jobrun.Check, ScheduledFor: planned,
	}
	accepted, found, err := control.RunByOccurrence(t.Context(), tenant, sourceUID, occurrence.OccurrenceKey)
	if err != nil || !found {
		t.Fatalf("timer receipt=%+v found=%t err=%v", accepted, found, err)
	}
	intent := jobrun.Attempt{Number: 1, KubernetesJobName: "oj-" + strings.ToLower(scan.GenerateID()), OwnerRunUID: scan.GenerateID()}
	if err := control.createAttemptForTenant(t.Context(), tenant, accepted.ID, intent, jobrun.CreatingAttempt, true); err != nil {
		t.Fatal(err)
	}
	controls := NewSourceControlRepository(openRoleDB(t, owner, "orbitjob_admin"))
	state, err := controls.UpdateForTenant(t.Context(), tenant, sourcecontrol.Check, sourceUID, 0, true, false, "test")
	if err != nil {
		t.Fatal(err)
	}
	pausedCursor := planned.Add(time.Duration(interval) * time.Second)
	tick.Now = func() time.Time { return pausedCursor }
	for range 3 {
		publishedBefore := len(publisher.objects)
		if created, err := tick.RunBatch(t.Context(), scope, 1); err != nil || created != 0 {
			t.Fatalf("paused timer created=%d err=%v", created, err)
		}
		if len(publisher.objects) != publishedBefore+1 || publisher.objects[publishedBefore].Spec.OccurrenceKey != occurrence.OccurrenceKey {
			t.Fatal("paused timer did not repair only its accepted occurrence")
		}
	}
	var runs, audits int
	if err := owner.QueryRowContext(t.Context(), `SELECT count(*) FROM job_run_control_plane WHERE tenant_id=$1 AND source_uid=$2`, tenant, sourceUID).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRowContext(t.Context(), `SELECT count(*) FROM audit_events WHERE tenant_id=$1 AND event_type=$2`, tenant, auditEventRunCreated).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if runs != 1 || audits != 1 {
		t.Fatalf("paused timer admitted work: runs=%d creation audits=%d", runs, audits)
	}
	current, err := checks.GetByID(t.Context(), tenant, c.ID)
	if err != nil || current.NextRunAt == nil || !current.NextRunAt.Equal(pausedCursor) {
		t.Fatalf("blocked occurrence advanced cursor: %+v err=%v", current, err)
	}
	replayed, fresh, err := control.CreateOccurrenceForTenant(t.Context(), tenant, occurrence, 1, checkschedule.ActorCheckScheduler)
	if err != nil || fresh || replayed.ID != accepted.ID {
		t.Fatalf("paused receipt replay=%+v fresh=%t err=%v", replayed, fresh, err)
	}
	calls := 0
	jobUID := scan.GenerateID()
	if err := control.CreateAttemptWithEnsure(t.Context(), tenant, accepted.ID, intent, func(context.Context) (jobrun.Attempt, error) {
		calls++
		return jobrun.Attempt{Number: intent.Number, KubernetesJobName: intent.KubernetesJobName, KubernetesJobUID: jobUID, Phase: jobrun.Running}, nil
	}); err != nil {
		t.Fatal(err)
	}
	execution, found, err := control.CurrentExecution(t.Context(), tenant, accepted.ID)
	if err != nil || !found || calls != 1 || execution.Run.Phase != jobrun.Running || execution.Run.AttemptUID != jobUID || execution.DispatchStage != jobrun.DispatchBound {
		t.Fatalf("schedule pause blocked Prepared recovery: execution=%+v found=%t calls=%d err=%v", execution, found, calls, err)
	}
	if _, err := controls.UpdateForTenant(t.Context(), tenant, sourcecontrol.Check, sourceUID, state.Version, false, false, "test"); err != nil {
		t.Fatal(err)
	}
	if created, err := tick.RunBatch(t.Context(), scope, 1); err != nil || created != 1 {
		t.Fatalf("resumed timer created=%d err=%v", created, err)
	}
}

func TestCheckSchedulePausePreservesManualAndExistingWorkflow(t *testing.T) {
	f := newWorkflowSnapshotFixture(t)
	planned := time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)
	parentInput := workflow.Run{
		SourceUID: f.source, RevisionID: f.revision,
		OccurrenceKey: coordination.OccurrenceKey(f.source, f.revision, planned),
		Trigger:       jobrun.Schedule, ScheduledFor: &planned, Actor: "test",
	}
	parent, created, err := f.store.CreateRunForTenant(t.Context(), f.tenant, parentInput)
	if err != nil || !created {
		t.Fatalf("accepted workflow=%+v created=%t err=%v", parent, created, err)
	}
	controls := NewSourceControlRepository(openRoleDB(t, f.owner, "orbitjob_admin"))
	for _, source := range []struct{ mode, uid string }{
		{sourcecontrol.Workflow, f.source}, {sourcecontrol.Kubernetes, f.extract}, {sourcecontrol.Kubernetes, f.load},
	} {
		if _, err := controls.UpdateForTenant(t.Context(), f.tenant, source.mode, source.uid, 0, true, false, "test"); err != nil {
			t.Fatal(err)
		}
	}
	later := planned.Add(time.Minute)
	blocked := parentInput
	blocked.ScheduledFor = &later
	blocked.OccurrenceKey = coordination.OccurrenceKey(f.source, f.revision, later)
	_, _, err = f.store.CreateRunForTenant(t.Context(), f.tenant, blocked)
	requireAdmissionReason(t, err, "schedule_paused")
	manualInput := parentInput
	manualInput.Trigger, manualInput.ScheduledFor = jobrun.Manual, nil
	manualInput.OccurrenceKey = scan.GenerateID()
	if _, fresh, err := f.store.CreateRunForTenant(t.Context(), f.tenant, manualInput); err != nil || !fresh {
		t.Fatalf("schedule pause blocked manual workflow: fresh=%t err=%v", fresh, err)
	}
	if _, fresh, err := f.control.CreateOccurrenceForTenant(t.Context(), f.tenant, jobrun.JobRun{
		SourceUID: f.extract, RevisionID: strconv.FormatInt(f.extractRevision, 10),
		OccurrenceKey: scan.GenerateID(), Trigger: jobrun.Manual,
	}, 1, "test"); err != nil || !fresh {
		t.Fatalf("schedule pause blocked manual job: fresh=%t err=%v", fresh, err)
	}
	for _, task := range []struct {
		name, uid string
		revision  int64
	}{{"extract", f.extract, f.extractRevision}, {"load", f.load, f.loadRevision}} {
		step, fresh, err := f.store.CreateStepForTenant(t.Context(), f.tenant, parent.ID, workflow.StepRun{
			SourceUID: task.uid, RevisionID: task.revision,
			OccurrenceKey: workflow.StepOccurrenceKey(f.source, parent.OccurrenceKey, task.name), Trigger: jobrun.Workflow,
		}, 1, "test")
		if err != nil || !fresh {
			t.Fatalf("paused sources blocked accepted %s step: fresh=%t err=%v", task.name, fresh, err)
		}
		intent := jobrun.Attempt{Number: 1, KubernetesJobName: "oj-" + strings.ToLower(scan.GenerateID()), OwnerRunUID: scan.GenerateID()}
		jobUID := scan.GenerateID()
		if err := f.control.CreateAttemptWithEnsure(t.Context(), f.tenant, step.ID, intent, func(context.Context) (jobrun.Attempt, error) {
			return jobrun.Attempt{Number: intent.Number, KubernetesJobName: intent.KubernetesJobName, KubernetesJobUID: jobUID, Phase: jobrun.Running}, nil
		}); err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.control.UpdateAttemptPhase(t.Context(), f.tenant, intent.KubernetesJobName, string(jobrun.Succeeded), jobUID, "1"); err != nil {
			t.Fatal(err)
		}
		if changed, err := f.control.UpdateRunPhase(t.Context(), f.tenant, step.ID, jobrun.Succeeded, 1); err != nil || !changed {
			t.Fatalf("accepted %s completion changed=%t err=%v", task.name, changed, err)
		}
	}
}
