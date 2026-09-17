// Package checkobserve records what a terminal run means for the surfaces
// downstream of the ledger: the check_runs read model the admin API serves,
// the SLI snapshots budgets aggregate, and the check metrics. One recorder
// owns all three so they describe the same fact the same way, from the one
// hook the operator fires when a run's phase actually changes.
package checkobserve

import (
	"context"
	"log/slog"
	"time"

	"orbitjob/internal/core/app/controlplane"
	"orbitjob/internal/core/domain/check"
	"orbitjob/internal/core/domain/checkrun"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/sli"
	"orbitjob/internal/platform/metrics"
)

// snapshotWindow is the width of one pre-aggregated SLI snapshot row. It must
// match the width the snapshot store uses when it closes a window.
const snapshotWindow = 5 * time.Minute

// unknownCheckType labels outcomes whose check definition has since been
// deleted: the run happened, but its type is no longer answerable.
const unknownCheckType = "unknown"

// OutcomeReader loads the terminal facts of a run.
type OutcomeReader interface {
	TerminalOutcome(ctx context.Context, tenantID string, runID int64) (controlplane.TerminalOutcome, error)
}

// ReadModelWriter maintains the check_runs read model.
type ReadModelWriter interface {
	RecordCompleted(ctx context.Context, tenantID string, record checkrun.CompletedRecord) error
}

// CheckTypes resolves a check definition's type for metric labels.
type CheckTypes interface {
	GetByID(ctx context.Context, tenantID string, id int64) (check.Snapshot, error)
}

// SLISource finds the SLIs whose source_config names a source_uid.
type SLISource interface {
	FindBySourceUID(ctx context.Context, tenantID, sourceUID string) ([]sli.Snapshot, error)
}

// SnapshotIncrements moves one good/bad event into the current window's
// snapshot row.
type SnapshotIncrements interface {
	IncrementSnapshot(ctx context.Context, tenantID string, sliID int64, windowStart time.Time, isGood bool) error
}

// Recorder is the operator's terminal-phase bookkeeper for check outcomes.
type Recorder struct {
	Outcomes  OutcomeReader
	ReadModel ReadModelWriter
	Checks    CheckTypes
	SLIs      SLISource
	Snapshots SnapshotIncrements
	// Now defaults to time.Now; tests pin it for stable windows.
	Now func() time.Time
}

// NewRecorder assembles the recorder.
func NewRecorder(outcomes OutcomeReader, readModel ReadModelWriter, checks CheckTypes, slis SLISource, snapshots SnapshotIncrements) *Recorder {
	return &Recorder{Outcomes: outcomes, ReadModel: readModel, Checks: checks, SLIs: slis, Snapshots: snapshots}
}

// RecordTerminalPhase records the consequences of a run reaching Succeeded or
// Failed. It is called only on a real phase transition, so a resync that
// re-observes a finished run cannot double-count snapshots or metrics.
//
// Bookkeeping failures are logged, not returned: the phase write is the fact,
// and failing the reconcile for a missed observation would requeue into a run
// that can never transition again, turning one missed row into a hot loop.
func (r *Recorder) RecordTerminalPhase(ctx context.Context, tenantID string, runID int64, phase jobrun.Phase) {
	if phase != jobrun.Succeeded && phase != jobrun.Failed {
		return
	}
	if r.Outcomes == nil {
		return
	}

	outcome, err := r.Outcomes.TerminalOutcome(ctx, tenantID, runID)
	if err != nil {
		slog.Error("read terminal outcome failed", "tenant_id", tenantID, "run_id", runID, "error", err)
		return
	}

	if checkID, isCheck := check.CheckIDFromSourceUID(outcome.SourceUID); isCheck {
		r.recordCheckRun(ctx, tenantID, checkID, outcome)
	}
	r.recordSLIEvents(ctx, tenantID, outcome)
}

// recordCheckRun upserts the check_runs read model and emits the check
// metrics. Status maps straight from the run phase: v1 evaluation is
// exit-code-only, so there is no severity and no evaluation result to carry.
func (r *Recorder) recordCheckRun(ctx context.Context, tenantID string, checkID int64, outcome controlplane.TerminalOutcome) {
	status := checkrun.StatusFailed
	if outcome.Phase == jobrun.Succeeded {
		status = checkrun.StatusSuccess
	}
	record := checkrun.CompletedRecord{
		RunID:       occurrenceRunID(outcome.OccurrenceKey),
		CheckID:     checkID,
		Status:      status,
		ScheduledAt: outcome.ScheduledFor,
		StartedAt:   outcome.StartedAt,
		FinishedAt:  outcome.CompletedAt,
	}
	if !outcome.StartedAt.IsZero() && !outcome.CompletedAt.IsZero() {
		record.DurationMs = int(outcome.CompletedAt.Sub(outcome.StartedAt).Milliseconds())
	}
	if err := r.ReadModel.RecordCompleted(ctx, tenantID, record); err != nil {
		slog.Error("record check run read model failed",
			"tenant_id", tenantID, "run_id", outcome.RunID, "check_id", checkID, "error", err)
		return
	}

	outcomeLabel := "failed"
	if status == checkrun.StatusSuccess {
		outcomeLabel = "success"
	}
	checkType := r.checkType(ctx, tenantID, checkID)
	metrics.CheckRunsCompletedTotal.WithLabelValues(tenantID, checkType, outcomeLabel).Inc()
	if !outcome.StartedAt.IsZero() && !outcome.CompletedAt.IsZero() && outcome.CompletedAt.After(outcome.StartedAt) {
		metrics.CheckRunDurationSeconds.WithLabelValues(tenantID, checkType).
			Observe(outcome.CompletedAt.Sub(outcome.StartedAt).Seconds())
	}
}

// checkType resolves the metric label; a deleted check keeps its outcome
// countable under an honest "unknown" rather than dropped.
func (r *Recorder) checkType(ctx context.Context, tenantID string, checkID int64) string {
	snap, err := r.Checks.GetByID(ctx, tenantID, checkID)
	if err != nil {
		return unknownCheckType
	}
	return snap.CheckType
}

// recordSLIEvents derives good/total for every SLI whose source_config names
// this run's source_uid -- check or ScheduledJob alike -- and increments the
// current window's snapshot. Canceled runs never reach this method, so a
// human stop stays out of the denominator.
func (r *Recorder) recordSLIEvents(ctx context.Context, tenantID string, outcome controlplane.TerminalOutcome) {
	if r.SLIs == nil || r.Snapshots == nil {
		return
	}
	slis, err := r.SLIs.FindBySourceUID(ctx, tenantID, outcome.SourceUID)
	if err != nil {
		slog.Error("find slis for source failed",
			"tenant_id", tenantID, "source_uid", outcome.SourceUID, "error", err)
		return
	}
	if len(slis) == 0 {
		return
	}

	event := outcomeEvent{
		Status:    statusForEvent(outcome.Phase),
		Duration:  outcome.CompletedAt.Sub(outcome.StartedAt),
		hasTiming: !outcome.StartedAt.IsZero() && !outcome.CompletedAt.IsZero(),
	}
	windowStart := r.now().Truncate(snapshotWindow)
	for _, s := range slis {
		isGood, err := isGoodEvent(event, s)
		if err != nil {
			slog.Error("invalid sli configuration, skipping snapshot",
				"tenant_id", tenantID, "sli_id", s.ID, "source_uid", outcome.SourceUID, "error", err)
			continue
		}
		if err := r.Snapshots.IncrementSnapshot(ctx, tenantID, s.ID, windowStart, isGood); err != nil {
			slog.Error("increment sli snapshot failed",
				"tenant_id", tenantID, "sli_id", s.ID, "source_uid", outcome.SourceUID, "error", err)
			continue
		}
		result := "bad"
		if isGood {
			result = "good"
		}
		metrics.SLIEvaluationsTotal.WithLabelValues(tenantID, s.SLIType, result).Inc()
		metrics.SLOSnapshotIncrementsTotal.WithLabelValues(tenantID, formatInt(s.ID)).Inc()
	}
}

func (r *Recorder) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

// statusForEvent maps the run phase onto the status vocabulary criteria and the
// read model share.
func statusForEvent(phase jobrun.Phase) string {
	if phase == jobrun.Succeeded {
		return checkrun.StatusSuccess
	}
	return checkrun.StatusFailed
}
