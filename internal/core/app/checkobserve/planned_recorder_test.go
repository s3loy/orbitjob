package checkobserve

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"orbitjob/internal/core/app/controlplane"
	"orbitjob/internal/core/domain/check"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/sli"
	"orbitjob/internal/platform/metrics"
	"orbitjob/internal/platform/scan"
)

type plannedSLISource struct {
	t        *testing.T
	ctx      context.Context
	tenant   string
	runID    int64
	plan     sli.EventConfiguration
	err      error
	requests int
}

func (s *plannedSLISource) EventConfigurationForOutcome(ctx context.Context, tenant string, runID int64) (sli.EventConfiguration, error) {
	s.t.Helper()
	if ctx != s.ctx || tenant != s.tenant || runID != s.runID {
		s.t.Fatalf("plan lookup lost context or run identity: tenant=%q run=%d", tenant, runID)
	}
	s.requests++
	return s.plan, s.err
}

func (s *plannedSLISource) FindBySourceUID(context.Context, string, string) ([]sli.Snapshot, error) {
	s.t.Fatal("frozen event plan must not reload current SLI definitions")
	return nil, nil
}

type plannedEvaluations struct {
	t         *testing.T
	ctx       context.Context
	tenant    string
	attempts  []sli.Evaluation
	committed map[[2]int64]sli.Evaluation
	failSLI   int64
	err       error
}

func (w *plannedEvaluations) RecordEvaluation(ctx context.Context, tenant string, e sli.Evaluation) (bool, error) {
	w.t.Helper()
	if ctx != w.ctx || tenant != w.tenant {
		w.t.Fatalf("evaluation lost trusted context or tenant: %q", tenant)
	}
	w.attempts = append(w.attempts, e)
	if e.SLIID == w.failSLI {
		return false, w.err
	}
	key := [2]int64{e.RunID, e.SLIID}
	if _, exists := w.committed[key]; exists {
		return false, nil
	}
	w.committed[key] = e
	return true, nil
}

func (w *plannedEvaluations) IncrementSnapshotForRun(context.Context, string, int64, int64, time.Time, bool) (bool, error) {
	w.t.Fatal("frozen plan must use the plan-aware evaluation writer")
	return false, nil
}

func newPlannedRecorder(t *testing.T, outcome controlplane.TerminalOutcome, plan sli.EventConfiguration) (*Recorder, *plannedSLISource, *plannedEvaluations) {
	t.Helper()
	ctx := context.Background()
	tenant := scan.GenerateID()
	source := &plannedSLISource{t: t, ctx: ctx, tenant: tenant, runID: outcome.RunID, plan: plan}
	writer := &plannedEvaluations{t: t, ctx: ctx, tenant: tenant, committed: map[[2]int64]sli.Evaluation{}}
	r := NewRecorder(&stubOutcomes{outcome: outcome}, &stubReadModel{}, &stubCheckTypes{snapshot: check.Snapshot{CheckType: check.CheckTypeHTTPHealth}}, source, writer)
	t.Cleanup(func() {
		labels := prometheus.Labels{"tenant_id": tenant}
		metrics.CheckRunsCompletedTotal.DeletePartialMatch(labels)
		metrics.CheckRunDurationSeconds.DeletePartialMatch(labels)
		metrics.SLIEvaluationsTotal.DeletePartialMatch(labels)
		metrics.SLOSnapshotIncrementsTotal.DeletePartialMatch(labels)
	})
	r.Now = func() time.Time {
		t.Fatal("frozen event plan must not use recovery wall-clock time")
		return time.Time{}
	}
	return r, source, writer
}

func TestPlannedTerminalReplayKeepsFrozenFactsAndCountsOnlyCommittedEvaluations(t *testing.T) {
	outcome := checkOutcome(jobrun.Succeeded)
	started := time.Date(2026, 11, 14, 23, 7, 59, 750000000, time.FixedZone("UTC+8", 8*60*60))
	finished := started.Add(250 * time.Millisecond)
	plan := sli.EventConfiguration{
		RunID: outcome.RunID, Semantics: sli.EventTime, EventTime: finished, TimeQuality: sli.TimeContainerFinished,
		ContainerStartedAt: &started, ContainerFinishedAt: &finished,
		Configurations: []sli.Snapshot{
			{ID: 11, SLIType: sli.TypeAvailability, ConfigurationVersion: 3},
			{ID: 12, SLIType: sli.TypeLatency, ConfigurationVersion: 8, GoodEventCriteria: map[string]any{"duration_ms": map[string]any{"op": "<=", "value": 300}}},
		},
	}
	r, source, writer := newPlannedRecorder(t, outcome, plan)
	model := &onceReadModel{seen: map[string]bool{}}
	r.ReadModel = model
	writer.failSLI, writer.err = 12, errors.New("second evaluation interrupted")
	if err := r.RecordTerminalPhaseOnce(source.ctx, source.tenant, outcome.RunID, jobrun.Failed); !errors.Is(err, writer.err) {
		t.Fatalf("first recording error = %v, want pending evaluation failure", err)
	}
	if len(writer.committed) != 1 {
		t.Fatalf("committed evaluations = %d, want first derivative only", len(writer.committed))
	}
	if got := testutil.ToFloat64(metrics.SLIEvaluationsTotal.WithLabelValues(source.tenant, sli.TypeLatency, "good")); got != 0 {
		t.Fatalf("failed write emitted %v latency samples", got)
	}
	writer.failSLI = 0
	recovered := outcome
	recovered.StartedAt = outcome.StartedAt.Add(24 * time.Hour)
	recovered.CompletedAt = recovered.StartedAt.Add(time.Hour)
	r.Outcomes = &stubOutcomes{outcome: recovered}
	if err := r.RecordTerminalPhaseOnce(source.ctx, source.tenant, outcome.RunID, jobrun.Succeeded); err != nil {
		t.Fatal(err)
	}
	if source.requests != 2 || len(writer.committed) != 2 || len(writer.attempts) != 4 {
		t.Fatalf("replay results: plan requests=%d committed=%d attempts=%d", source.requests, len(writer.committed), len(writer.attempts))
	}
	for i, configuration := range plan.Configurations {
		first, replay := writer.attempts[i], writer.attempts[i+2]
		if !reflect.DeepEqual(first, replay) {
			t.Fatalf("replay changed frozen evaluation: first=%+v replay=%+v", first, replay)
		}
		if first.RunID != outcome.RunID || first.SLIID != configuration.ID || first.ConfigurationVersion != configuration.ConfigurationVersion || first.Semantics != sli.EventTime || first.TimeQuality != sli.TimeContainerFinished || !first.EventTime.Equal(finished) {
			t.Fatalf("evaluation lost frozen identity/configuration/time: %+v", first)
		}
		wantWindow := time.Date(2026, 11, 14, 15, 5, 0, 0, time.UTC)
		if first.WindowStart != wantWindow || first.Good == nil || !*first.Good || first.MissingReason != "" {
			t.Fatalf("evaluation = %+v, want good sample in UTC event window %v", first, wantWindow)
		}
		if got := testutil.ToFloat64(metrics.SLIEvaluationsTotal.WithLabelValues(source.tenant, configuration.SLIType, "good")); got != 1 {
			t.Fatalf("%s good samples = %v, want one after replay", configuration.SLIType, got)
		}
		if got := testutil.ToFloat64(metrics.SLOSnapshotIncrementsTotal.WithLabelValues(source.tenant, formatInt(configuration.ID))); got != 1 {
			t.Fatalf("SLI %d snapshot metric = %v, want one after replay", configuration.ID, got)
		}
	}
	if len(model.records) != 1 {
		t.Fatalf("check read-model records = %d, want one after replay", len(model.records))
	}
	record := model.records[0]
	if record.RunID != "5b1cf2c4-a0d9-e8f7-a6b5-c4d3e2f10a9b" || !record.StartedAt.Equal(started) || !record.FinishedAt.Equal(finished) || record.DurationMs != 250 {
		t.Fatalf("check read model did not use stable occurrence identity and frozen task span: %+v", record)
	}
}

func TestPlannedTerminalMissingTaskTimingDoesNotFabricateSamples(t *testing.T) {
	started := time.Date(2026, 11, 14, 12, 2, 0, 0, time.UTC)
	finished, reversed, zero := started.Add(time.Second), started.Add(-time.Second), time.Time{}
	for _, tt := range []struct {
		name       string
		start, end *time.Time
	}{
		{name: "no container timing"},
		{name: "start only", start: &started},
		{name: "finish only", end: &finished},
		{name: "reversed span", start: &started, end: &reversed},
		{name: "zero start", start: &zero, end: &finished},
		{name: "zero finish", start: &started, end: &zero},
	} {
		t.Run(tt.name, func(t *testing.T) {
			outcome := checkOutcome(jobrun.Succeeded)
			outcome.SourceUID = "function-17"
			plan := sli.EventConfiguration{
				RunID: outcome.RunID, Semantics: sli.EventTime, EventTime: finished, TimeQuality: sli.TimeObserved,
				ContainerStartedAt: tt.start, ContainerFinishedAt: tt.end,
				Configurations: []sli.Snapshot{
					{ID: 1, SLIType: sli.TypeAvailability, ConfigurationVersion: 1},
					{ID: 2, SLIType: sli.TypeLatency, ConfigurationVersion: 4, GoodEventCriteria: map[string]any{"duration_ms": map[string]any{"op": "<=", "value": 3000}}},
				},
			}
			r, source, writer := newPlannedRecorder(t, outcome, plan)
			unavailableBefore := testutil.ToFloat64(unavailableSamples.WithLabelValues("task_timing_unavailable"))
			for range 2 {
				if err := r.RecordTerminalPhaseOnce(source.ctx, source.tenant, outcome.RunID, outcome.Phase); err != nil {
					t.Fatal(err)
				}
			}
			if len(writer.committed) != 2 {
				t.Fatalf("committed facts = %d, want availability and unavailable latency fact", len(writer.committed))
			}
			availability := writer.committed[[2]int64{outcome.RunID, 1}]
			if availability.Good == nil || !*availability.Good {
				t.Fatalf("availability lost confirmed successful outcome: %+v", availability)
			}
			latency := writer.committed[[2]int64{outcome.RunID, 2}]
			if latency.Good != nil || latency.MissingReason != "task_timing_unavailable" || !latency.EventTime.Equal(finished) || latency.TimeQuality != sli.TimeObserved {
				t.Fatalf("missing timing did not remain an unavailable fact: %+v", latency)
			}
			for _, result := range []string{"good", "bad"} {
				if got := testutil.ToFloat64(metrics.SLIEvaluationsTotal.WithLabelValues(source.tenant, sli.TypeLatency, result)); got != 0 {
					t.Fatalf("missing timing emitted %v %s latency samples", got, result)
				}
			}
			if got := testutil.ToFloat64(metrics.SLOSnapshotIncrementsTotal.WithLabelValues(source.tenant, "2")); got != 0 {
				t.Fatalf("missing timing incremented sample snapshots %v times", got)
			}
			if got := testutil.ToFloat64(unavailableSamples.WithLabelValues("task_timing_unavailable")) - unavailableBefore; got != 1 {
				t.Fatalf("unavailable metric delta = %v, want one committed fact after replay", got)
			}
		})
	}
}

func TestPlannedTerminalPreservesLegacyAttemptTiming(t *testing.T) {
	outcome := checkOutcome(jobrun.Succeeded)
	outcome.SourceUID = "function-17"
	plan := sli.EventConfiguration{
		RunID: outcome.RunID, Semantics: sli.LegacyProcessing, EventTime: outcome.CompletedAt, TimeQuality: "legacy_processing",
		Configurations: []sli.Snapshot{{ID: 6, ConfigurationVersion: 1, SLIType: sli.TypeLatency, GoodEventCriteria: map[string]any{"duration_ms": map[string]any{"op": "<=", "value": 100}}}},
	}
	r, source, writer := newPlannedRecorder(t, outcome, plan)
	if err := r.RecordTerminalPhaseOnce(source.ctx, source.tenant, outcome.RunID, outcome.Phase); err != nil {
		t.Fatal(err)
	}
	got := writer.committed[[2]int64{outcome.RunID, 6}]
	if got.Good == nil || *got.Good || got.Semantics != sli.LegacyProcessing || got.MissingReason != "" {
		t.Fatalf("legacy attempt latency must remain a bad sample, got %+v", got)
	}
	if count := testutil.ToFloat64(metrics.SLIEvaluationsTotal.WithLabelValues(source.tenant, sli.TypeLatency, "bad")); count != 1 {
		t.Fatalf("legacy bad samples = %v, want 1", count)
	}
}

func TestPlannedTerminalRejectsInvalidPlansBeforeDerivativeWrites(t *testing.T) {
	outcome := checkOutcome(jobrun.Succeeded)
	planFailure := errors.New("plan storage unavailable")
	for _, tt := range []struct {
		name, want string
		plan       sli.EventConfiguration
		err        error
	}{
		{name: "wrong run", plan: sli.EventConfiguration{RunID: outcome.RunID + 1, EventTime: outcome.CompletedAt}, want: "invalid SLI event plan identity"},
		{name: "missing event time", plan: sli.EventConfiguration{RunID: outcome.RunID}, want: "invalid SLI event plan identity"},
		{name: "plan read failure", err: planFailure, want: planFailure.Error()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, source, writer := newPlannedRecorder(t, outcome, tt.plan)
			source.err = tt.err
			model := &stubReadModel{}
			r.ReadModel = model
			err := r.RecordTerminalPhaseOnce(source.ctx, source.tenant, outcome.RunID, outcome.Phase)
			if err == nil || !strings.Contains(err.Error(), tt.want) || (tt.err != nil && !errors.Is(err, tt.err)) {
				t.Fatalf("recording error = %v, want %q", err, tt.want)
			}
			if len(model.records) != 0 || len(writer.attempts) != 0 {
				t.Fatalf("invalid plan wrote derivatives: records=%d evaluations=%d", len(model.records), len(writer.attempts))
			}
		})
	}
}

func TestPlannedTerminalRejectsInvalidCriteriaAndLegacyWriter(t *testing.T) {
	outcome := checkOutcome(jobrun.Succeeded)
	outcome.SourceUID = "function-17"
	plan := sli.EventConfiguration{RunID: outcome.RunID, Semantics: sli.EventTime, EventTime: outcome.CompletedAt,
		Configurations: []sli.Snapshot{{ID: 44, ConfigurationVersion: 2, SLIType: sli.TypeAvailability, GoodEventCriteria: map[string]any{"severity": "ok"}}}}
	r, source, writer := newPlannedRecorder(t, outcome, plan)
	err := r.RecordTerminalPhaseOnce(source.ctx, source.tenant, outcome.RunID, outcome.Phase)
	if err == nil || !strings.Contains(err.Error(), "invalid SLI configuration 44") || len(writer.attempts) != 0 {
		t.Fatalf("invalid criteria: error=%v evaluation attempts=%d", err, len(writer.attempts))
	}
	r.Snapshots = newStubSnapshots()
	err = r.RecordTerminalPhaseOnce(source.ctx, source.tenant, outcome.RunID, outcome.Phase)
	if err == nil || !strings.Contains(err.Error(), "plan-aware SLI snapshot writer is required") {
		t.Fatalf("legacy writer error = %v, want plan-aware writer requirement", err)
	}
}

func TestPlannedTerminalReturnsBothReadModelAndEvaluationFailures(t *testing.T) {
	outcome := checkOutcome(jobrun.Failed)
	plan := sli.EventConfiguration{RunID: outcome.RunID, Semantics: sli.EventTime, EventTime: outcome.CompletedAt, TimeQuality: sli.TimeObserved,
		Configurations: []sli.Snapshot{{ID: 8, SLIType: sli.TypeAvailability, ConfigurationVersion: 2}}}
	r, source, writer := newPlannedRecorder(t, outcome, plan)
	readErr, evaluationErr := errors.New("check read model unavailable"), errors.New("evaluation unavailable")
	r.ReadModel = &stubReadModel{err: readErr}
	writer.failSLI, writer.err = 8, evaluationErr
	err := r.RecordTerminalPhaseOnce(source.ctx, source.tenant, outcome.RunID, outcome.Phase)
	if !errors.Is(err, readErr) || !errors.Is(err, evaluationErr) {
		t.Fatalf("recording error = %v, want both failed derivatives", err)
	}
	if len(writer.attempts) != 1 || writer.attempts[0].Good == nil || *writer.attempts[0].Good || len(writer.committed) != 0 {
		t.Fatalf("failed check did not attempt a bad availability evaluation: %+v", writer.attempts)
	}
	if got := testutil.ToFloat64(metrics.SLIEvaluationsTotal.WithLabelValues(source.tenant, sli.TypeAvailability, "bad")); got != 0 {
		t.Fatalf("uncommitted evaluation emitted %v bad samples", got)
	}
}
