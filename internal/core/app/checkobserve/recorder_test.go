package checkobserve

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"orbitjob/internal/core/app/controlplane"
	"orbitjob/internal/core/domain/check"
	"orbitjob/internal/core/domain/checkrun"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/sli"
	"orbitjob/internal/platform/metrics"
)

var testTenant = "00000000000000000000000001"

type stubOutcomes struct{ outcome controlplane.TerminalOutcome }

func (s *stubOutcomes) TerminalOutcome(context.Context, string, int64) (controlplane.TerminalOutcome, error) {
	return s.outcome, nil
}

type stubReadModel struct {
	records []checkrun.CompletedRecord
	err     error
}

func (s *stubReadModel) RecordCompleted(_ context.Context, _ string, record checkrun.CompletedRecord) error {
	if s.err != nil {
		return s.err
	}
	s.records = append(s.records, record)
	return nil
}

type stubCheckTypes struct {
	snapshot check.Snapshot
	err      error
}

func (s *stubCheckTypes) GetByID(context.Context, string, int64) (check.Snapshot, error) {
	return s.snapshot, s.err
}

type stubSLIs struct {
	slis    []sli.Snapshot
	err     error
	queried []string
}

func (s *stubSLIs) FindBySourceUID(_ context.Context, _ string, sourceUID string) ([]sli.Snapshot, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.queried = append(s.queried, sourceUID)
	return s.slis, nil
}

type stubSnapshots struct {
	good    map[int64]int
	bad     map[int64]int
	windows map[int64]time.Time
}

func newStubSnapshots() *stubSnapshots {
	return &stubSnapshots{good: map[int64]int{}, bad: map[int64]int{}, windows: map[int64]time.Time{}}
}

func (s *stubSnapshots) IncrementSnapshot(_ context.Context, _ string, sliID int64, windowStart time.Time, isGood bool) error {
	if isGood {
		s.good[sliID]++
	} else {
		s.bad[sliID]++
	}
	s.windows[sliID] = windowStart
	return nil
}

type fixture struct {
	recorder  *Recorder
	outcomes  *stubOutcomes
	readModel *stubReadModel
	checks    *stubCheckTypes
	slis      *stubSLIs
	snapshots *stubSnapshots
	now       time.Time
}

func newFixture() *fixture {
	f := &fixture{
		outcomes:  &stubOutcomes{},
		readModel: &stubReadModel{},
		checks:    &stubCheckTypes{snapshot: check.Snapshot{ID: 42, CheckType: check.CheckTypeHTTPHealth}},
		slis:      &stubSLIs{},
		snapshots: newStubSnapshots(),
		now:       time.Date(2026, 9, 18, 12, 3, 47, 0, time.UTC),
	}
	f.recorder = NewRecorder(f.outcomes, f.readModel, f.checks, f.slis, f.snapshots)
	f.recorder.Now = func() time.Time { return f.now }
	return f
}

func checkOutcome(phase jobrun.Phase) controlplane.TerminalOutcome {
	started := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	return controlplane.TerminalOutcome{
		RunID:         9,
		SourceUID:     "check-42",
		OccurrenceKey: "5b1cf2c4a0d9e8f7a6b5c4d3e2f10a9b8c7d6e5f4a3b2c1d0e9f8a7b6c5d4e3f",
		Phase:         phase,
		ScheduledFor:  started.Add(-time.Second),
		StartedAt:     started,
		CompletedAt:   started.Add(2500 * time.Millisecond),
	}
}

func TestRecordTerminalPhase_IgnoresNonTerminalPhases(t *testing.T) {
	f := newFixture()
	for _, phase := range []jobrun.Phase{jobrun.Pending, jobrun.Running, jobrun.RetryWaiting, jobrun.Canceled, jobrun.CancelUnknown} {
		f.recorder.RecordTerminalPhase(context.Background(), testTenant, 9, phase)
	}
	if len(f.readModel.records) != 0 || len(f.slis.queried) != 0 {
		t.Fatalf("non-terminal phases produced bookkeeping: records=%d sli queries=%d",
			len(f.readModel.records), len(f.slis.queried))
	}
}

// TestRecordTerminalPhase_Success is the whole hook in one case: a succeeded
// check run writes one read-model row whose run id derives from the occurrence
// key, counts the completion metric, and observes the attempt duration.
func TestRecordTerminalPhase_Success(t *testing.T) {
	f := newFixture()
	f.outcomes.outcome = checkOutcome(jobrun.Succeeded)

	f.recorder.RecordTerminalPhase(context.Background(), testTenant, 9, jobrun.Succeeded)

	if len(f.readModel.records) != 1 {
		t.Fatalf("read model rows = %d, want 1", len(f.readModel.records))
	}
	rec := f.readModel.records[0]
	if rec.Status != checkrun.StatusSuccess {
		t.Fatalf("status = %q, want success", rec.Status)
	}
	if rec.CheckID != 42 {
		t.Fatalf("check id = %d, want 42 (parsed from the source uid)", rec.CheckID)
	}
	wantRunID := "5b1cf2c4-a0d9-e8f7-a6b5-c4d3e2f10a9b"
	if rec.RunID != wantRunID {
		t.Fatalf("run id = %q, want derived %q", rec.RunID, wantRunID)
	}
	if rec.DurationMs != 2500 {
		t.Fatalf("duration = %d, want 2500 from the attempt span", rec.DurationMs)
	}
	if got := testutil.ToFloat64(metrics.CheckRunsCompletedTotal.WithLabelValues(testTenant, check.CheckTypeHTTPHealth, "success")); got != 1 {
		t.Fatalf("completed metric = %v, want 1", got)
	}
	if got := testutil.CollectAndCount(metrics.CheckRunDurationSeconds, "orbitjob_check_run_duration_seconds"); got != 1 {
		t.Fatalf("duration series = %d, want 1 observation", got)
	}
}

func TestRecordTerminalPhase_Failure(t *testing.T) {
	f := newFixture()
	f.outcomes.outcome = checkOutcome(jobrun.Failed)

	f.recorder.RecordTerminalPhase(context.Background(), testTenant, 9, jobrun.Failed)

	if len(f.readModel.records) != 1 || f.readModel.records[0].Status != checkrun.StatusFailed {
		t.Fatalf("records = %+v, want one failed row", f.readModel.records)
	}
	if got := testutil.ToFloat64(metrics.CheckRunsCompletedTotal.WithLabelValues(testTenant, check.CheckTypeHTTPHealth, "failed")); got != 1 {
		t.Fatalf("completed metric = %v, want 1", got)
	}
}

func TestRecordTerminalPhase_ReadModelFailureDoesNotStopSLIBookkeeping(t *testing.T) {
	f := newFixture()
	f.outcomes.outcome = checkOutcome(jobrun.Succeeded)
	f.readModel.err = errors.New("db down")
	f.slis.slis = []sli.Snapshot{{ID: 5, SLIType: sli.TypeAvailability}}

	f.recorder.RecordTerminalPhase(context.Background(), testTenant, 9, jobrun.Succeeded)

	// The read model failed, but the SLI increment still happened.
	if len(f.snapshots.good) != 1 || f.snapshots.good[5] != 1 {
		t.Fatalf("snapshot increments = %+v, want one good event for sli 5", f.snapshots.good)
	}
}

// TestRecordTerminalPhase_JobRunSourcesSLIsOnly pins the split: a run of a
// declared job (source_uid not check-shaped) gets SLI bookkeeping but no
// check_runs read-model row.
func TestRecordTerminalPhase_JobRunSourcesSLIsOnly(t *testing.T) {
	f := newFixture()
	outcome := checkOutcome(jobrun.Succeeded)
	outcome.SourceUID = "018f3a2b-7c1d-7c3e-9f4a-b2d1e0c8a910"
	f.outcomes.outcome = outcome
	f.slis.slis = []sli.Snapshot{{ID: 6, SLIType: sli.TypeAvailability}}

	f.recorder.RecordTerminalPhase(context.Background(), testTenant, 9, jobrun.Succeeded)

	if len(f.readModel.records) != 0 {
		t.Fatalf("job-run outcome wrote %d read-model rows, want 0", len(f.readModel.records))
	}
	if len(f.slis.queried) != 1 || f.slis.queried[0] != outcome.SourceUID {
		t.Fatalf("sli lookups = %v, want the job's source uid", f.slis.queried)
	}
	if f.snapshots.good[6] != 1 {
		t.Fatalf("snapshot increments = %+v, want one good event", f.snapshots.good)
	}
}

func TestRecordTerminalPhase_SLICriteria(t *testing.T) {
	window := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	success := checkOutcome(jobrun.Succeeded)
	failed := checkOutcome(jobrun.Failed)

	tests := []struct {
		name    string
		sli     sli.Snapshot
		outcome controlplane.TerminalOutcome
		want    int
		wantBad bool
		wantErr bool
	}{
		{
			name:    "availability counts success good by default",
			sli:     sli.Snapshot{ID: 1, SLIType: sli.TypeAvailability},
			outcome: success,
			want:    1,
		},
		{
			name:    "availability counts failure bad by default",
			sli:     sli.Snapshot{ID: 1, SLIType: sli.TypeAvailability},
			outcome: failed,
			wantBad: true,
		},
		{
			name:    "duration within threshold is good",
			sli:     sli.Snapshot{ID: 2, SLIType: sli.TypeLatency, GoodEventCriteria: map[string]any{"duration_ms": map[string]any{"op": "<=", "value": 3000}}},
			outcome: success,
			want:    1,
		},
		{
			name:    "duration over threshold is bad",
			sli:     sli.Snapshot{ID: 2, SLIType: sli.TypeLatency, GoodEventCriteria: map[string]any{"duration_ms": map[string]any{"op": "<=", "value": 100}}},
			outcome: success,
			wantBad: true,
		},
		{
			name:    "status criterion matching",
			sli:     sli.Snapshot{ID: 3, SLIType: sli.TypeCustom, GoodEventCriteria: map[string]any{"status": "failed"}},
			outcome: failed,
			want:    1,
		},
		{
			name:    "severity criterion has no producer and skips the snapshot",
			sli:     sli.Snapshot{ID: 4, SLIType: sli.TypeQuality, GoodEventCriteria: map[string]any{"severity": "ok"}},
			outcome: success,
			wantErr: true,
		},
		{
			name:    "latency without criteria is a configuration error",
			sli:     sli.Snapshot{ID: 5, SLIType: sli.TypeLatency},
			outcome: success,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()
			f.outcomes.outcome = tt.outcome
			f.slis.slis = []sli.Snapshot{tt.sli}

			f.recorder.RecordTerminalPhase(context.Background(), testTenant, 9, tt.outcome.Phase)

			good, bad := f.snapshots.good[tt.sli.ID], f.snapshots.bad[tt.sli.ID]
			if tt.wantErr {
				if good != 0 || bad != 0 {
					t.Fatalf("a configuration error must skip the snapshot, got good=%d bad=%d", good, bad)
				}
				return
			}
			if tt.want != 0 && good != tt.want {
				t.Fatalf("good events = %d, want %d", good, tt.want)
			}
			if tt.wantBad && bad != 1 {
				t.Fatalf("bad events = %d, want 1", bad)
			}
			if !f.snapshots.windows[tt.sli.ID].Equal(window) {
				t.Fatalf("window start = %v, want the 5-minute window %v", f.snapshots.windows[tt.sli.ID], window)
			}
		})
	}
}

func TestRecordTerminalPhase_DeletedCheckCountsAsUnknown(t *testing.T) {
	f := newFixture()
	f.outcomes.outcome = checkOutcome(jobrun.Succeeded)
	f.checks.err = errors.New("not found: the check was deleted")

	f.recorder.RecordTerminalPhase(context.Background(), testTenant, 9, jobrun.Succeeded)

	if got := testutil.ToFloat64(metrics.CheckRunsCompletedTotal.WithLabelValues(testTenant, unknownCheckType, "success")); got != 1 {
		t.Fatalf("completed metric with unknown type = %v, want 1", got)
	}
}

// TestOccurrenceRunIDIsDeterministic pins the derivation twice: the same key
// always yields the same UUID, and a key of an unexpected shape still yields a
// stable UUID instead of an error mid-bookkeeping.
func TestOccurrenceRunIDIsDeterministic(t *testing.T) {
	key := "5b1cf2c4a0d9e8f7a6b5c4d3e2f10a9b8c7d6e5f4a3b2c1d0e9f8a7b6c5d4e3f"
	first, second := occurrenceRunID(key), occurrenceRunID(key)
	if first != second {
		t.Fatalf("derivations diverge: %q vs %q", first, second)
	}
	if len(first) != 36 || first[8] != '-' || first[13] != '-' || first[18] != '-' || first[23] != '-' {
		t.Fatalf("derived id %q is not UUID-shaped", first)
	}
	if occurrenceRunID("short") == occurrenceRunID("short2") {
		t.Fatal("distinct malformed keys must not collide")
	}
}
