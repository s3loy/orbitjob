//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/app/projection"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/workflow"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/platform/scan"
)

func TestWorkflowForbidSerializesManualAndScheduledStarts(t *testing.T) {
	owner := OpenStoreDB(t)
	ctx := ctxStore(t)
	tenant := scan.GenerateID()
	if _, err := owner.ExecContext(ctx, `INSERT INTO tenants(id,slug,name) VALUES($1,'workflow-forbid','Workflow Forbid')`, tenant); err != nil {
		t.Fatal(err)
	}
	control := NewControlPlaneRepository(owner)
	id, err := (projection.Service{Revisions: control}).ApplyWorkflowJob(ctx, v1alpha1.WorkflowJob{
		ObjectMeta: metav1.ObjectMeta{Name: "pipeline", Namespace: "finance", UID: "wf-forbid", Generation: 1},
		Spec:       v1alpha1.WorkflowJobSpec{Schedule: "* * * * *", ConcurrencyPolicy: v1alpha1.Forbid, Tasks: []v1alpha1.WorkflowTaskSpec{{Name: "one", JobRef: v1alpha1.WorkflowTaskJobRef{Name: "job"}}}},
	}, tenant, "test")
	if err != nil {
		t.Fatal(err)
	}
	// The Operator uses the least-privilege runtime login; operator is a
	// NOLOGIN capability role. Do not test as the schema owner.
	repo := NewWorkflowRunRepository(openRoleDB(t, owner, "orbitjob_runtime"))
	const writers = 12
	start := make(chan struct{})
	type result struct {
		row workflow.Run
		err error
	}
	results := make(chan result, writers)
	var wg sync.WaitGroup
	planned := time.Now().UTC().Add(time.Second)
	for index := range writers {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			row := workflow.Run{SourceUID: "wf-forbid", RevisionID: id, OccurrenceKey: fmt.Sprintf("%064x", index+1), Trigger: jobrun.Manual, Actor: "test", ConcurrencyPolicy: "Forbid"}
			if index%2 == 0 {
				row.Trigger = jobrun.Schedule
				row.ScheduledFor = &planned
			}
			stored, _, err := repo.CreateRunForTenant(ctx, tenant, row)
			results <- result{stored, err}
		}(index)
	}
	close(start)
	wg.Wait()
	close(results)
	accepted := 0
	var rejected []workflow.Run
	scheduledRejected, manualBlocked := 0, 0
	for got := range results {
		if got.err != nil {
			if errors.Is(got.err, jobrun.ErrAdmissionBlocked) {
				manualBlocked++
				continue
			}
			var conflict *resource.ConflictError
			if !errors.As(got.err, &conflict) {
				t.Fatal(got.err)
			}
			continue
		}
		if got.row.Phase == workflow.PhaseRejected {
			rejected = append(rejected, got.row)
			if got.row.Trigger == jobrun.Schedule {
				scheduledRejected++
			}
			continue
		}
		accepted++
	}
	if scheduledRejected < 5 || manualBlocked < 5 {
		t.Fatalf("missing scheduled receipts/manual waiting: scheduled=%d manual=%d", scheduledRejected, manualBlocked)
	}
	if accepted != 1 {
		t.Fatalf("accepted %d concurrent executions, want 1", accepted)
	}
	open, err := repo.OpenRuns(ctx, tenant)
	if err != nil || len(open) != 1 {
		t.Fatalf("open=%+v err=%v", open, err)
	}
	if err := repo.BindResource(ctx, tenant, open[0].ID, "original-resource-uid"); err != nil {
		t.Fatal(err)
	}
	if err := repo.BindResource(ctx, tenant, open[0].ID, "original-resource-uid"); err != nil {
		t.Fatal(err)
	}
	if err := repo.BindResource(ctx, tenant, open[0].ID, "replacement-resource-uid"); err == nil {
		t.Fatal("replacement UID took over original occurrence")
	}
	if _, err := repo.UpdatePhase(ctx, tenant, open[0].ID, workflow.PhaseSucceeded, nil); err != nil {
		t.Fatal(err)
	}
	// Retrying a rejected Manual receipt after the winning run ends must never
	// turn the old request into queued work.
	for _, row := range rejected {
		stored, created, err := repo.CreateRunForTenant(context.Background(), tenant, workflow.Run{SourceUID: row.SourceUID, RevisionID: id, OccurrenceKey: row.OccurrenceKey, Trigger: row.Trigger, Actor: "test", ConcurrencyPolicy: "Forbid"})
		if err != nil || created || stored.Phase != workflow.PhaseRejected {
			t.Fatalf("rejected retry=%+v created=%v err=%v", stored, created, err)
		}
	}
}
