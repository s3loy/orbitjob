package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
)

// ErrObjectDeletionPending preserves the ledger while foreground GC progresses.
var ErrObjectDeletionPending = errors.New("kubernetes object deletion is pending")

// Default retention when a definition does not state one. Zero cannot mean
// "keep nothing", so an unset policy resolves to these values here.
const (
	DefaultSuccessfulHistory = 3
	DefaultFailedHistory     = 3
)

// RunPruner lists and removes runs beyond a definition's retained history.
type RunPruner interface {
	PrunableRuns(ctx context.Context, tenantID, sourceUID string, keepSuccessful, keepFailed int) ([]jobrun.PrunableRun, error)
	DeleteRun(ctx context.Context, tenantID string, runID int64) (bool, error)
}

// RunRemover deletes the Kubernetes object representing a pruned run.
type RunRemover interface {
	Remove(ctx context.Context, namespace, scheduledJobName, occurrenceKey string) error
}

// Retainer trims terminal history using the latest revision of each source,
// including inactive sources when the production retention reader is present.
type Retainer struct {
	Definitions RevisionSource
	History     RunPruner
	Remover     RunRemover
	Tenants     []string
}

// Sweep prunes history for every retained definition and reports how many runs it
// removed.
//
// The Kubernetes object is removed before the row. Deleting the row first would
// leave a JobRun with no platform record, whereas a failed object removal just
// makes the next sweep retry while the row still anchors the audit trail.
func (r Retainer) Sweep(ctx context.Context) (int, error) {
	if r.Definitions == nil || r.History == nil || r.Remover == nil {
		return 0, fmt.Errorf("retainer dependencies are required")
	}
	removed := 0
	for _, tenant := range r.Tenants {
		revisions, err := r.Definitions.ActiveRevisions(ctx, tenant)
		if reader, ok := r.Definitions.(interface {
			RetentionRevisions(context.Context, string) ([]revision.Revision, error)
		}); ok {
			revisions, err = reader.RetentionRevisions(ctx, tenant)
		}
		if err != nil {
			return removed, fmt.Errorf("list active revisions for tenant %s: %w", tenant, err)
		}
		for _, rev := range revisions {
			count, err := r.pruneDefinition(ctx, tenant, rev)
			if err != nil {
				return removed, err
			}
			removed += count
		}
	}
	return removed, nil
}

func (r Retainer) pruneDefinition(ctx context.Context, tenant string, rev revision.Revision) (int, error) {
	var spec v1alpha1.ScheduledJobSpec
	if err := json.Unmarshal([]byte(rev.NormalizedSpec), &spec); err != nil {
		return 0, fmt.Errorf("decode revision %d for %s: %w", rev.ID, rev.Identity.Name, err)
	}
	keepSuccessful, keepFailed := historyLimits(spec.History)

	prunable, err := r.History.PrunableRuns(ctx, tenant, rev.Identity.SourceUID, keepSuccessful, keepFailed)
	if err != nil {
		return 0, fmt.Errorf("scan prunable runs for %s: %w", rev.Identity.Name, err)
	}

	removed := 0
	for _, run := range prunable {
		var removeErr error
		if safe, ok := r.Remover.(interface {
			RemoveIdentity(context.Context, string, string, v1alpha1.RunIdentity) error
		}); ok {
			removeErr = safe.RemoveIdentity(ctx, rev.Identity.Namespace, rev.Identity.Name, v1alpha1.RunIdentity{SourceUID: rev.Identity.SourceUID, OccurrenceKey: run.OccurrenceKey, RevisionID: run.RevisionID, Trigger: v1alpha1.Trigger(run.Trigger)})
		} else {
			removeErr = r.Remover.Remove(ctx, rev.Identity.Namespace, rev.Identity.Name, run.OccurrenceKey)
		}
		if errors.Is(removeErr, ErrObjectDeletionPending) {
			continue
		}
		if err := removeErr; err != nil {
			return removed, fmt.Errorf("remove job run for %s: %w", rev.Identity.Name, err)
		}
		deleted, err := r.History.DeleteRun(ctx, tenant, run.ID)
		if err != nil {
			return removed, fmt.Errorf("delete run %d: %w", run.ID, err)
		}
		if deleted {
			removed++
		}
	}
	return removed, nil
}

// historyLimits resolves a definition's retention into concrete counts. A
// negative value is treated as unset rather than as "keep nothing", so a
// malformed spec cannot silently delete a user's entire history.
func historyLimits(policy v1alpha1.HistoryPolicy) (successful, failed int) {
	successful = int(policy.SuccessfulRuns)
	if successful <= 0 {
		successful = DefaultSuccessfulHistory
	}
	failed = int(policy.FailedRuns)
	if failed <= 0 {
		failed = DefaultFailedHistory
	}
	return successful, failed
}
