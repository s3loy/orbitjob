// Package checkschedule fires due checks through the Kubernetes control plane.
// A check is not a queue entry: on each occurrence the loop materializes the
// check's definition revision, records one ledger row with trigger Check, and
// publishes a JobRun custom resource, so the ordinary operator pipeline
// renders, executes and observes it like any other run.
package checkschedule

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/robfig/cron/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/app/controlplane"
	"orbitjob/internal/core/app/projection"
	"orbitjob/internal/core/domain/check"
	"orbitjob/internal/core/domain/coordination"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
	"orbitjob/internal/platform/metrics"
)

// ActorCheckScheduler is the identity recorded on every run this loop creates
// and every revision it projects. The ledger's actor column exists so "who
// triggered this" stays answerable, and a check occurrence has exactly one
// honest answer; sharing the job scheduler's actor would erase the distinction
// the trigger column draws.
const ActorCheckScheduler = "orbitjob-check-scheduler"

// DefaultBatchSize bounds how many due checks one tick fires per tenant.
const DefaultBatchSize = 50

// Scope is one tenant and the namespace its check runs are published into. The
// JobRun custom resource and the Kubernetes Job both live in that namespace,
// which the tenancy mapping must resolve back to the same tenant.
type Scope struct {
	TenantID  string
	Namespace string
}

// CheckSource reads due checks and advances their scheduling cursor.
type CheckSource interface {
	ListDue(ctx context.Context, tenantID string, now time.Time, limit int) ([]check.Snapshot, error)
	UpdateNextRunAt(ctx context.Context, tenantID string, id int64, nextRunAt time.Time) error
}

// RevisionWriter materializes the check's definition revision.
type RevisionWriter interface {
	ApplyRevisionForTenant(ctx context.Context, tenantID string, rev revision.Revision) (int64, error)
}

// RunRecorder stores occurrences and reports the runs that still need a live
// Kubernetes representation.
type RunRecorder interface {
	CreateOccurrenceForTenant(ctx context.Context, tenantID string, run jobrun.JobRun, maxAttempts int, actor string) (jobrun.StoredRun, bool, error)
	OpenRuns(ctx context.Context, tenantID, sourceUID string) ([]jobrun.OpenRun, error)
}

// RunPublisher exposes a stored run as a JobRun custom resource. It must be
// idempotent: the run row already exists, so a failure here is repaired by
// re-publishing.
type RunPublisher interface {
	Publish(ctx context.Context, spec v1alpha1.JobRun) error
}

// TickUseCase fires every due check of one tenant scope per tick.
type TickUseCase struct {
	checks    CheckSource
	revisions RevisionWriter
	runs      RunRecorder
	publisher RunPublisher
	// Now defaults to time.Now; tests pin it so occurrence keys are stable.
	Now func() time.Time
}

// NewTickUseCase assembles the check firing loop.
func NewTickUseCase(checks CheckSource, revisions RevisionWriter, runs RunRecorder, publisher RunPublisher) *TickUseCase {
	return &TickUseCase{checks: checks, revisions: revisions, runs: runs, publisher: publisher}
}

func (uc *TickUseCase) now() time.Time {
	if uc.Now != nil {
		return uc.Now().UTC()
	}
	return time.Now().UTC()
}

// RunBatch scans one tenant's due checks and fires each occurrence. A check
// that fails is logged and skipped so one bad definition cannot stall the rest
// of the tenant; only a failure to list due checks aborts the tick.
func (uc *TickUseCase) RunBatch(ctx context.Context, scope Scope, limit int) (int, error) {
	if uc.checks == nil || uc.revisions == nil || uc.runs == nil || uc.publisher == nil {
		return 0, fmt.Errorf("check scheduler dependencies are required")
	}
	if limit < 1 {
		limit = DefaultBatchSize
	}
	now := uc.now()

	checks, err := uc.checks.ListDue(ctx, scope.TenantID, now, limit)
	if err != nil {
		return 0, fmt.Errorf("list due checks: %w", err)
	}

	created := 0
	for _, c := range checks {
		fired, err := uc.fireCheck(ctx, scope, c, now)
		if err != nil {
			slog.Error("firing check failed; skipping it",
				"tenant", scope.TenantID,
				"check_id", c.ID,
				"source_uid", check.CheckSourceUID(c.ID),
				"error", err,
			)
			continue
		}
		created += fired
	}
	return created, nil
}

// fireCheck takes one due check from definition to published run, in the same
// order the job scheduler uses: revision, ledger row, custom resource, cursor.
// The row precedes the CR, so a Check-trigger CR always finds the run the
// check scheduler committed, and the cursor advances last, so a failure after
// the occurrence re-derives the same occurrence key and deduplicates instead
// of executing twice.
func (uc *TickUseCase) fireCheck(ctx context.Context, scope Scope, c check.Snapshot, now time.Time) (int, error) {
	sourceUID := check.CheckSourceUID(c.ID)

	// Repair before firing. A run whose CR was never published sits non-terminal
	// forever with nothing observing it; republishing open runs here breaks that
	// cycle the way the job scheduler repairs its own before applying policy.
	if err := uc.repairOpenRuns(ctx, scope, sourceUID); err != nil {
		return 0, err
	}

	revisionID, err := (projection.Service{Revisions: uc.revisions, Now: uc.Now}).
		ApplyCheck(ctx, c, scope.Namespace, scope.TenantID, ActorCheckScheduler)
	if err != nil {
		return 0, fmt.Errorf("materialize revision: %w", err)
	}
	if c.NextRunAt == nil {
		return 0, fmt.Errorf("check %s is due but has no next_run_at", sourceUID)
	}
	scheduledAt := c.NextRunAt.UTC()
	occurrenceKey := coordination.OccurrenceKey(sourceUID, revisionID, scheduledAt)

	state, isNew, err := uc.runs.CreateOccurrenceForTenant(ctx, scope.TenantID, jobrun.JobRun{
		SourceUID:     sourceUID,
		RevisionID:    strconv.FormatInt(revisionID, 10),
		OccurrenceKey: occurrenceKey,
		Trigger:       jobrun.Check,
		Phase:         jobrun.Pending,
		ScheduledFor:  scheduledAt,
		// The check semantics count retries; the platform counts attempts.
	}, c.RetryLimit+1, ActorCheckScheduler)
	if err != nil {
		return 0, fmt.Errorf("create occurrence: %w", err)
	}

	// Publish even for an occurrence that already exists, unless it finished:
	// publish is idempotent, and a run whose CR was lost would otherwise be
	// stranded by deduplication with nothing to execute it. Terminal history is
	// skipped so a resync costs no API calls.
	if !isNew && jobrun.Terminal(state.Phase) {
		return 0, nil
	}
	if err := uc.publisher.Publish(ctx, uc.jobRunObject(scope, sourceUID, state, occurrenceKey, scheduledAt)); err != nil {
		return 0, fmt.Errorf("publish run: %w", err)
	}

	next := nextRunAt(c, now)
	if err := uc.checks.UpdateNextRunAt(ctx, scope.TenantID, c.ID, next); err != nil {
		return 0, fmt.Errorf("advance next_run_at: %w", err)
	}

	if isNew {
		metrics.CheckRunsCreatedTotal.WithLabelValues(scope.TenantID).Inc()
		return 1, nil
	}
	return 0, nil
}

// repairOpenRuns republishes the custom resource of every unfinished run of one
// check. Names and annotations derive from stored state, so a repaired run is
// indistinguishable from one published at occurrence time.
func (uc *TickUseCase) repairOpenRuns(ctx context.Context, scope Scope, sourceUID string) error {
	open, err := uc.runs.OpenRuns(ctx, scope.TenantID, sourceUID)
	if err != nil {
		return fmt.Errorf("list open runs for %s: %w", sourceUID, err)
	}
	for _, run := range open {
		obj := uc.jobRunObject(scope, sourceUID, jobrun.StoredRun{
			ID: run.ID, RevisionID: run.RevisionID, Phase: run.Phase,
		}, run.OccurrenceKey, run.ScheduledFor)
		if err := uc.publisher.Publish(ctx, obj); err != nil {
			return fmt.Errorf("repair run %d for %s: %w", run.ID, sourceUID, err)
		}
	}
	return nil
}

// jobRunObject renders the JobRun CR for one check occurrence. There are no
// owner references: a check is not a ScheduledJob CR, and an owner reference
// naming an object that does not exist makes the garbage collector delete the
// run it points at. Retention removes check runs explicitly, like every other
// run.
func (uc *TickUseCase) jobRunObject(scope Scope, sourceUID string, state jobrun.StoredRun, occurrenceKey string, scheduledAt time.Time) v1alpha1.JobRun {
	return v1alpha1.JobRun{
		TypeMeta: metav1.TypeMeta{APIVersion: v1alpha1.GroupVersion.String(), Kind: "JobRun"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      v1alpha1.RunObjectName(sourceUID, occurrenceKey),
			Namespace: scope.Namespace,
			Labels: map[string]string{
				controlplane.LabelTenant:       scope.Namespace,
				controlplane.LabelScheduledJob: sourceUID,
			},
			Annotations: scheduleAnnotation(scheduledAt),
		},
		Spec: v1alpha1.JobRunSpec{
			ScheduledJobRef:    v1alpha1.ObjectReference{Name: sourceUID, UID: sourceUID},
			DefinitionRevision: state.RevisionID,
			Trigger:            v1alpha1.Check,
			Actor:              ActorCheckScheduler,
			OccurrenceKey:      occurrenceKey,
		},
	}
}

// scheduleAnnotation records the occurrence time, reusing the control plane's
// annotation key so one reader decodes both run families. The stored
// scheduled_for is authoritative; a zero time means unknown, and an absent
// annotation is better than a wrong one.
func scheduleAnnotation(at time.Time) map[string]string {
	if at.IsZero() {
		return nil
	}
	return map[string]string{controlplane.AnnotationScheduledAt: at.UTC().Format(time.RFC3339)}
}

// nextRunAt computes the check's next due instant from its own schedule. A cron
// expression the parser rejects falls back to one minute ahead: the check stays
// visible and firing retries, where dropping the advance would hot-loop it
// every tick.
func nextRunAt(c check.Snapshot, now time.Time) time.Time {
	if c.ScheduleType == check.ScheduleTypeCron && c.CronExpr != nil {
		schedule, err := cron.ParseStandard(*c.CronExpr)
		if err == nil {
			return schedule.Next(now)
		}
		slog.Error("check cron expression no longer parses; falling back to one minute",
			"check_id", c.ID, "cron", *c.CronExpr, "error", err)
		return now.Add(time.Minute)
	}
	if c.ScheduleType == check.ScheduleTypeInterval && c.IntervalSec != nil {
		return now.Add(time.Duration(*c.IntervalSec) * time.Second)
	}
	return now.Add(time.Minute)
}
