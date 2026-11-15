package operator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/app/controlplane"
	"orbitjob/internal/core/domain/coordination"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
	"orbitjob/internal/core/domain/workflow"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/workflowschedule"
)

const ActorWorkflowScheduler = "orbitjob-workflow-scheduler"

var workflowScheduleDecisions = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "orbitjob_workflow_schedule_decisions_total",
	Help: "Workflow scheduling and publication decisions by bounded outcome.",
}, []string{"outcome"})

type WorkflowScheduleRevisions interface {
	ActiveWorkflowRevisions(context.Context, string) ([]revision.Revision, error)
	RevisionByID(context.Context, string, int64) (revision.Revision, error)
}

type WorkflowSchedulePublisher interface {
	PublishWorkflow(context.Context, v1alpha1.WorkflowRun) (string, error)
}

// WorkflowScheduler shares the Operator Lease. The database serializes every
// insertion origin; this loop only selects the current minute, never history.
type WorkflowScheduler struct {
	Revisions WorkflowScheduleRevisions
	Workflows WorkflowRunStore
	Publisher WorkflowSchedulePublisher
	Tenants   []string
	Now       func() time.Time
}

func (s WorkflowScheduler) Tick(ctx context.Context) (int, error) {
	if s.Revisions == nil || s.Workflows == nil || s.Publisher == nil {
		return 0, fmt.Errorf("workflow schedule dependencies are required")
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	created := 0
	var failures []error
	for _, tenant := range s.Tenants {
		if err := ctx.Err(); err != nil {
			return created, err
		}
		// Repair all open scheduled rows, including paused or inactive revisions.
		if err := s.repair(ctx, tenant); err != nil {
			failures = append(failures, err)
		}
		definitions, err := s.Revisions.ActiveWorkflowRevisions(ctx, tenant)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		for _, rev := range definitions {
			count, err := s.fire(ctx, tenant, rev, now)
			created += count
			if err != nil {
				slog.ErrorContext(ctx, "workflow scheduling failed", "tenant", tenant, "revision_id", rev.ID, "error", err)
				failures = append(failures, err)
			}
		}
	}
	return created, errors.Join(failures...)
}

func (s WorkflowScheduler) fire(ctx context.Context, tenant string, rev revision.Revision, now time.Time) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	var spec v1alpha1.WorkflowJobSpec
	if err := json.Unmarshal([]byte(rev.NormalizedSpec), &spec); err != nil {
		return 0, err
	}
	if err := spec.Validate(); err != nil {
		workflowScheduleDecisions.WithLabelValues("invalid").Inc()
		return 0, err
	}
	if err := workflow.ValidateDAG(workflow.DAGFromSpec(spec)); err != nil {
		workflowScheduleDecisions.WithLabelValues("invalid").Inc()
		return 0, err
	}
	if spec.Suspend {
		workflowScheduleDecisions.WithLabelValues("skipped_paused").Inc()
		return 0, nil
	}
	if spec.Schedule == "" {
		return 0, nil
	}
	parsed, err := workflowschedule.Parse(spec.Schedule, spec.TimeZone)
	if err != nil {
		workflowScheduleDecisions.WithLabelValues("invalid").Inc()
		return 0, err
	}
	planned := now.Truncate(time.Minute)
	if !parsed.Next(planned.Add(-time.Second)).Equal(planned) {
		return 0, nil
	}
	if planned.Before(rev.CreatedAt) {
		workflowScheduleDecisions.WithLabelValues("skipped_before_activation").Inc()
		return 0, nil
	}
	key := coordination.OccurrenceKey(rev.Identity.SourceUID, rev.ID, planned)
	row, created, err := s.Workflows.CreateRunForTenant(ctx, tenant, workflow.Run{
		SourceUID: rev.Identity.SourceUID, RevisionID: rev.ID, OccurrenceKey: key,
		Trigger: jobrun.Schedule, Actor: ActorWorkflowScheduler, Phase: workflow.PhasePending,
		ScheduledFor: &planned, ConcurrencyPolicy: string(spec.ConcurrencyPolicy),
	})
	if err != nil {
		var conflict *resource.ConflictError
		if errors.As(err, &conflict) {
			workflowScheduleDecisions.WithLabelValues("conflict").Inc()
			return 0, nil
		}
		return 0, err
	}
	count := 0
	if row.Phase == workflow.PhaseRejected {
		if created {
			workflowScheduleDecisions.WithLabelValues("skipped_forbid").Inc()
		}
		return 0, nil
	}
	if created {
		count = 1
		workflowScheduleDecisions.WithLabelValues("created").Inc()
	}
	if !workflow.Terminal(row.Phase) {
		if err := s.publish(ctx, tenant, rev, row); err != nil {
			return count, err
		}
	}
	return count, nil
}

func (s WorkflowScheduler) repair(ctx context.Context, tenant string) error {
	rows, err := s.Workflows.OpenRuns(ctx, tenant)
	if err != nil {
		return err
	}
	var failures []error
	for _, row := range rows {
		if row.Trigger != jobrun.Schedule {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rev, err := s.Revisions.RevisionByID(ctx, tenant, row.RevisionID)
		if err == nil && (rev.Identity.SourceUID != row.SourceUID || rev.Identity.SourceMode != workflow.SourceModeWorkflow) {
			err = fmt.Errorf("scheduled workflow row %d revision identity mismatch", row.ID)
		}
		if err == nil {
			err = s.publish(ctx, tenant, rev, row)
		}
		if err != nil {
			failures = append(failures, err)
			continue
		}
		workflowScheduleDecisions.WithLabelValues("repaired").Inc()
	}
	return errors.Join(failures...)
}

func (s WorkflowScheduler) publish(ctx context.Context, tenant string, rev revision.Revision, row workflow.Run) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	annotations := map[string]string{}
	if row.ScheduledFor != nil {
		annotations[controlplane.AnnotationScheduledAt] = row.ScheduledFor.UTC().Format(time.RFC3339)
	}
	obj := v1alpha1.WorkflowRun{
		TypeMeta: metav1.TypeMeta{APIVersion: v1alpha1.GroupVersion.String(), Kind: "WorkflowRun"},
		ObjectMeta: metav1.ObjectMeta{
			Name: v1alpha1.WorkflowRunObjectName(rev.Identity.Name, row.OccurrenceKey), Namespace: rev.Identity.Namespace, UID: types.UID(row.ResourceUID),
			Annotations:     annotations,
			OwnerReferences: []metav1.OwnerReference{{APIVersion: v1alpha1.GroupVersion.String(), Kind: "WorkflowJob", Name: rev.Identity.Name, UID: types.UID(rev.Identity.SourceUID), Controller: ptr(true), BlockOwnerDeletion: ptr(false)}},
		},
		Spec: v1alpha1.WorkflowRunSpec{WorkflowRef: v1alpha1.ObjectReference{Name: rev.Identity.Name, UID: row.SourceUID}, DefinitionRevision: row.RevisionID, Trigger: v1alpha1.Schedule, Actor: row.Actor, OccurrenceKey: row.OccurrenceKey,
			CancelRequested: row.Phase == workflow.PhaseCancelRequested || row.Phase == workflow.PhaseCancelUnknown},
	}
	uid, err := s.Publisher.PublishWorkflow(ctx, obj)
	if err != nil {
		workflowScheduleDecisions.WithLabelValues("publication_error").Inc()
		return err
	}
	if err := s.Workflows.BindResource(ctx, tenant, row.ID, uid); err != nil {
		workflowScheduleDecisions.WithLabelValues("binding_error").Inc()
		return err
	}
	return nil
}
