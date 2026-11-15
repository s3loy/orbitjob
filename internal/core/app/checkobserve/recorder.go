// Package checkobserve records what a terminal run means for the surfaces
// downstream of the ledger: the check_runs read model the admin API serves,
// the SLI snapshots budgets aggregate, and the check metrics. One recorder
// owns all three so they describe the same fact the same way, from the one
// hook the operator fires when a run's phase actually changes.
package checkobserve

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
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
	RecordCompletedOnce(ctx context.Context, tenantID string, record checkrun.CompletedRecord) (bool, error)
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
	IncrementSnapshotForRun(ctx context.Context, tenantID string, runID, sliID int64, windowStart time.Time, isGood bool) (bool, error)
}

type eventConfigurationSource interface {
	EventConfigurationForOutcome(context.Context, string, int64) (sli.EventConfiguration, error)
}

type plannedSnapshotWriter interface {
	RecordEvaluation(context.Context, string, sli.Evaluation) (bool, error)
}

var unavailableSamples = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "orbitjob_sli_samples_unavailable_total", Help: "Committed missing-fact evaluations excluded from sample counters."}, []string{"reason"})

func init() { prometheus.MustRegister(unavailableSamples) }

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
// Failed. This compatibility entry point logs a failure. The runtime uses
// RecordTerminalPhaseOnce and leaves the ledger acknowledgement pending on error.
func (r *Recorder) RecordTerminalPhase(ctx context.Context, tenantID string, runID int64, phase jobrun.Phase) {
	if err := r.RecordTerminalPhaseOnce(ctx, tenantID, runID, phase); err != nil {
		slog.Error("terminal bookkeeping remains pending", "tenant_id", tenantID, "run_id", runID, "error", err)
	}
}

// RecordTerminalPhaseOnce propagates failures so the ledger's acknowledgement
// remains pending. Production writers deduplicate by run, including SLI facts.
func (r *Recorder) RecordTerminalPhaseOnce(ctx context.Context, tenantID string, runID int64, phase jobrun.Phase) error {
	if phase != jobrun.Succeeded && phase != jobrun.Failed {
		return nil
	}
	if r.Outcomes == nil {
		return fmt.Errorf("outcome reader is required")
	}
	outcome, err := r.Outcomes.TerminalOutcome(ctx, tenantID, runID)
	if err != nil {
		return err
	}
	var plan *sli.EventConfiguration
	if source, ok := r.SLIs.(eventConfigurationSource); ok {
		configuration, err := source.EventConfigurationForOutcome(ctx, tenantID, runID)
		if err != nil {
			return err
		}
		if configuration.RunID != runID || configuration.EventTime.IsZero() {
			return fmt.Errorf("invalid SLI event plan identity")
		}
		plan = &configuration
		if plan.Semantics == sli.EventTime {
			outcome.StartedAt = time.Time{}
			outcome.CompletedAt = time.Time{}
			if plan.ContainerStartedAt != nil && plan.ContainerFinishedAt != nil {
				outcome.StartedAt = *plan.ContainerStartedAt
				outcome.CompletedAt = *plan.ContainerFinishedAt
			}
		}
	}
	var checkErr error
	if checkID, ok := check.CheckIDFromSourceUID(outcome.SourceUID); ok {
		checkErr = r.recordCheckRun(ctx, tenantID, checkID, outcome)
	}
	if plan != nil {
		return errors.Join(checkErr, r.recordPlannedSLIEvents(ctx, tenantID, outcome, *plan))
	}
	return errors.Join(checkErr, r.recordSLIEvents(ctx, tenantID, outcome))
}

func (r *Recorder) recordPlannedSLIEvents(ctx context.Context, tenant string, outcome controlplane.TerminalOutcome, plan sli.EventConfiguration) error {
	writer, ok := r.Snapshots.(plannedSnapshotWriter)
	if !ok {
		return fmt.Errorf("plan-aware SLI snapshot writer is required")
	}
	event := outcomeEvent{Status: statusForEvent(outcome.Phase), Duration: outcome.CompletedAt.Sub(outcome.StartedAt), hasTiming: !outcome.StartedAt.IsZero() && !outcome.CompletedAt.IsZero() && !outcome.CompletedAt.Before(outcome.StartedAt)}
	for _, configuration := range plan.Configurations {
		if err := sli.ValidateCriteria(configuration.SLIType, configuration.GoodEventCriteria); err != nil {
			return fmt.Errorf("invalid SLI configuration %d: %w", configuration.ID, err)
		}
		evaluation := sli.Evaluation{RunID: outcome.RunID, SLIID: configuration.ID, ConfigurationVersion: configuration.ConfigurationVersion, Semantics: plan.Semantics, EventTime: plan.EventTime, TimeQuality: plan.TimeQuality, WindowStart: plan.EventTime.UTC().Truncate(snapshotWindow)}
		if _, needsTiming := configuration.GoodEventCriteria["duration_ms"]; needsTiming && !event.hasTiming {
			evaluation.MissingReason = "task_timing_unavailable"
		} else {
			good, err := isGoodEvent(event, configuration)
			if err != nil {
				return err
			}
			evaluation.Good = &good
		}
		inserted, err := writer.RecordEvaluation(ctx, tenant, evaluation)
		if err != nil {
			return err
		}
		if !inserted {
			continue
		}
		if evaluation.Good == nil {
			unavailableSamples.WithLabelValues(evaluation.MissingReason).Inc()
			continue
		}
		result := "bad"
		if *evaluation.Good {
			result = "good"
		}
		metrics.SLIEvaluationsTotal.WithLabelValues(tenant, configuration.SLIType, result).Inc()
		metrics.SLOSnapshotIncrementsTotal.WithLabelValues(tenant, formatInt(configuration.ID)).Inc()
	}
	return nil
}

// recordCheckRun upserts the check_runs read model and emits the check
// metrics. Status maps straight from the run phase: v1 evaluation is
// exit-code-only, so there is no severity and no evaluation result to carry.
func (r *Recorder) recordCheckRun(ctx context.Context, tenantID string, checkID int64, outcome controlplane.TerminalOutcome) error {
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
	if !outcome.StartedAt.IsZero() && !outcome.CompletedAt.IsZero() && !outcome.CompletedAt.Before(outcome.StartedAt) {
		record.DurationMs = int(outcome.CompletedAt.Sub(outcome.StartedAt).Milliseconds())
	}
	if r.ReadModel == nil {
		return fmt.Errorf("check read model is required")
	}
	inserted, err := r.ReadModel.RecordCompletedOnce(ctx, tenantID, record)
	if err != nil {
		return err
	}
	if !inserted {
		return nil
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
	return nil
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
// completion window's snapshot. Canceled runs never reach this method, so a
// human stop stays out of the denominator.
func (r *Recorder) recordSLIEvents(ctx context.Context, tenantID string, outcome controlplane.TerminalOutcome) error {
	if r.SLIs == nil || r.Snapshots == nil {
		return nil
	}
	slis, err := r.SLIs.FindBySourceUID(ctx, tenantID, outcome.SourceUID)
	if err != nil {
		return err
	}
	event := outcomeEvent{Status: statusForEvent(outcome.Phase), Duration: outcome.CompletedAt.Sub(outcome.StartedAt), hasTiming: !outcome.StartedAt.IsZero() && !outcome.CompletedAt.IsZero() && !outcome.CompletedAt.Before(outcome.StartedAt)}
	at := outcome.CompletedAt
	if at.IsZero() {
		at = outcome.ScheduledFor
	}
	if at.IsZero() {
		at = r.now()
	}
	windowStart := at.UTC().Truncate(snapshotWindow)
	for _, s := range slis {
		isGood, err := isGoodEvent(event, s)
		if err != nil {
			return fmt.Errorf("invalid sli %d: %w", s.ID, err)
		}
		inserted, err := r.Snapshots.IncrementSnapshotForRun(ctx, tenantID, outcome.RunID, s.ID, windowStart, isGood)
		if err != nil {
			return err
		}
		if !inserted {
			continue
		}
		result := "bad"
		if isGood {
			result = "good"
		}
		metrics.SLIEvaluationsTotal.WithLabelValues(tenantID, s.SLIType, result).Inc()
		metrics.SLOSnapshotIncrementsTotal.WithLabelValues(tenantID, formatInt(s.ID)).Inc()
	}
	return nil
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
