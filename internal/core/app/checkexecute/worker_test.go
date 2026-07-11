package checkexecute

import (
	"context"
	"errors"
	"testing"
	"time"

	"orbitjob/internal/core/app/evaluate"
	"orbitjob/internal/core/app/execute"
	"orbitjob/internal/core/app/execute/handler"
	"orbitjob/internal/core/domain/check"
	"orbitjob/internal/core/domain/checkrun"
)

type stubCheckReader struct {
	chk check.Snapshot
	err error
}

func (s *stubCheckReader) GetByID(_ context.Context, _ string, _ int64) (check.Snapshot, error) {
	return s.chk, s.err
}

type completedRun struct {
	id       int64
	status   string
	severity string
}

type stubCheckRunRepo struct {
	runs        []checkrun.Snapshot
	claimErr    error
	completed   []completedRun
	completeErr error
}

func (s *stubCheckRunRepo) ClaimNext(_ context.Context, _ string, _ int, _ time.Time) ([]checkrun.Snapshot, error) {
	return s.runs, s.claimErr
}

func (s *stubCheckRunRepo) Complete(_ context.Context, _ string, id int64, status, severity string, _, _ map[string]any, _ int, _ time.Time) error {
	s.completed = append(s.completed, completedRun{id: id, status: status, severity: severity})
	return s.completeErr
}

type stubEvaluator struct {
	result evaluate.Result
}

func (s *stubEvaluator) Evaluate(_ map[string]any, _ []check.AssertionRule) evaluate.Result {
	return s.result
}

type stubSLIRecorder struct {
	recorded []int64
	err      error
}

func (s *stubSLIRecorder) RecordCheckRun(_ context.Context, _ string, checkID int64, _ checkrun.Snapshot) error {
	s.recorded = append(s.recorded, checkID)
	return s.err
}

type mockHandler struct {
	result execute.Result
}

func (m *mockHandler) Execute(_ context.Context, _ execute.AssignedTask) execute.Result {
	return m.result
}

func newTestUC(cr *stubCheckReader, crr *stubCheckRunRepo, eval evaluator, rec sliEventRecorder, now time.Time) *TickUseCase {
	uc := NewTickUseCase(cr, crr, eval, rec)
	uc.clock = func() time.Time { return now }
	return uc
}

func TestRunBatch_ClaimError(t *testing.T) {
	crr := &stubCheckRunRepo{claimErr: errors.New("claim failed")}
	uc := newTestUC(&stubCheckReader{}, crr, &stubEvaluator{}, nil, time.Now())
	n, err := uc.RunBatch(context.Background(), "t1", 10)
	if err == nil {
		t.Fatal("expected error from ClaimNext")
	}
	if n != 0 {
		t.Fatalf("expected 0 executed on claim error, got %d", n)
	}
}

func TestRunBatch_NoRuns(t *testing.T) {
	crr := &stubCheckRunRepo{}
	uc := newTestUC(&stubCheckReader{}, crr, &stubEvaluator{}, nil, time.Now())
	n, err := uc.RunBatch(context.Background(), "t1", 10)
	if err != nil || n != 0 {
		t.Fatalf("expected 0 nil, got %d %v", n, err)
	}
}

func TestRunBatch_Success(t *testing.T) {
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	handler.Register("check_http_health", &mockHandler{result: execute.Result{
		Success:    true,
		ResultCode: "0",
		Output:     map[string]any{"status": 200},
	}})
	cr := &stubCheckReader{chk: check.Snapshot{ID: 1, CheckType: check.CheckTypeHTTPHealth, TimeoutSec: 5}}
	crr := &stubCheckRunRepo{runs: []checkrun.Snapshot{{ID: 10, RunID: "r1", CheckID: 1}}}
	eval := &stubEvaluator{result: evaluate.Result{
		OverallSeverity: evaluate.SeverityOK,
		Passed:          1,
		Failed:          0,
		Results:         []evaluate.RuleResult{{Metric: "status", Passed: true, Severity: evaluate.SeverityOK}},
	}}
	rec := &stubSLIRecorder{}
	uc := newTestUC(cr, crr, eval, rec, now)

	n, err := uc.RunBatch(context.Background(), "t1", 10)
	if err != nil || n != 1 {
		t.Fatalf("expected 1 nil, got %d %v", n, err)
	}
	if len(crr.completed) != 1 || crr.completed[0].status != checkrun.StatusSuccess {
		t.Fatalf("expected one success completion, got %v", crr.completed)
	}
	if crr.completed[0].severity != evaluate.SeverityOK {
		t.Fatalf("expected severity ok, got %q", crr.completed[0].severity)
	}
	if len(rec.recorded) != 1 || rec.recorded[0] != 1 {
		t.Fatalf("expected sli event recorded for check 1, got %v", rec.recorded)
	}
}

func TestRunBatch_GetByIDError(t *testing.T) {
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	cr := &stubCheckReader{err: errors.New("not found")}
	crr := &stubCheckRunRepo{runs: []checkrun.Snapshot{{ID: 10, RunID: "r1", CheckID: 1}}}
	uc := newTestUC(cr, crr, &stubEvaluator{}, nil, now)

	n, err := uc.RunBatch(context.Background(), "t1", 10)
	if err != nil {
		t.Fatalf("expected nil (per-run error is logged, not returned), got %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 executed when GetByID fails, got %d", n)
	}
	if len(crr.completed) != 0 {
		t.Fatalf("expected no completions when GetByID fails, got %d", len(crr.completed))
	}
}

func TestRunBatch_UnknownCheckTypeFails(t *testing.T) {
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	// "unknown" has no registered "check_unknown" handler and is not the built-in
	// http_health, so executeRun falls back to an unknown_handler result (failed).
	cr := &stubCheckReader{chk: check.Snapshot{ID: 2, CheckType: "unknown", TimeoutSec: 5}}
	crr := &stubCheckRunRepo{runs: []checkrun.Snapshot{{ID: 11, RunID: "r2", CheckID: 2}}}
	rec := &stubSLIRecorder{}
	uc := newTestUC(cr, crr, &stubEvaluator{}, rec, now)

	n, err := uc.RunBatch(context.Background(), "t1", 10)
	if err != nil || n != 1 {
		t.Fatalf("expected 1 nil, got %d %v", n, err)
	}
	if len(crr.completed) != 1 || crr.completed[0].status != checkrun.StatusFailed {
		t.Fatalf("expected failed status for unknown check type, got %v", crr.completed)
	}
	if crr.completed[0].severity != evaluate.SeverityCritical {
		t.Fatalf("expected critical severity for failed run, got %q", crr.completed[0].severity)
	}
}

func TestRunBatch_CompleteError(t *testing.T) {
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	handler.Register("check_http_health", &mockHandler{result: execute.Result{
		Success:    true,
		ResultCode: "0",
		Output:     map[string]any{"status": 200},
	}})
	cr := &stubCheckReader{chk: check.Snapshot{ID: 1, CheckType: check.CheckTypeHTTPHealth, TimeoutSec: 5}}
	crr := &stubCheckRunRepo{
		runs:        []checkrun.Snapshot{{ID: 10, RunID: "r1", CheckID: 1}},
		completeErr: errors.New("complete failed"),
	}
	uc := newTestUC(cr, crr, &stubEvaluator{result: evaluate.Result{OverallSeverity: evaluate.SeverityOK}}, nil, now)

	n, err := uc.RunBatch(context.Background(), "t1", 10)
	if err != nil {
		t.Fatalf("expected nil (complete error is logged per-run), got %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 executed when Complete fails, got %d", n)
	}
}

func TestRunBatch_DurationRecorded(t *testing.T) {
	// Use a clock that advances on each call so durationMs > 0, exercising the
	// duration-metric observe branch.
	start := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	var calls int
	advancingClock := func() time.Time {
		calls++
		return start.Add(time.Duration(calls) * time.Millisecond)
	}
	handler.Register("check_http_health", &mockHandler{result: execute.Result{
		Success:    true,
		ResultCode: "0",
		Output:     map[string]any{"status": 200},
	}})
	cr := &stubCheckReader{chk: check.Snapshot{ID: 1, CheckType: check.CheckTypeHTTPHealth, TimeoutSec: 5}}
	crr := &stubCheckRunRepo{runs: []checkrun.Snapshot{{ID: 10, RunID: "r1", CheckID: 1}}}
	uc := NewTickUseCase(cr, crr, &stubEvaluator{result: evaluate.Result{OverallSeverity: evaluate.SeverityOK}}, nil)
	uc.clock = advancingClock

	n, err := uc.RunBatch(context.Background(), "t1", 10)
	if err != nil || n != 1 {
		t.Fatalf("expected 1 nil, got %d %v", n, err)
	}
	if len(crr.completed) != 1 {
		t.Fatalf("expected one completion, got %d", len(crr.completed))
	}
}

func TestNewTickUseCase_DefaultClock(t *testing.T) {
	// Exercise the default clock set by NewTickUseCase (not overridden).
	handler.Register("check_http_health", &mockHandler{result: execute.Result{
		Success:    true,
		ResultCode: "0",
		Output:     map[string]any{"status": 200},
	}})
	cr := &stubCheckReader{chk: check.Snapshot{ID: 1, CheckType: check.CheckTypeHTTPHealth, TimeoutSec: 5}}
	crr := &stubCheckRunRepo{runs: []checkrun.Snapshot{{ID: 10, RunID: "r1", CheckID: 1}}}
	uc := NewTickUseCase(cr, crr, &stubEvaluator{result: evaluate.Result{OverallSeverity: evaluate.SeverityOK}}, nil)

	n, err := uc.RunBatch(context.Background(), "t1", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 created with default clock, got %d", n)
	}
}

func TestRunBatch_SLIRecordErrorLogged(t *testing.T) {
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	handler.Register("check_http_health", &mockHandler{result: execute.Result{
		Success:    true,
		ResultCode: "0",
		Output:     map[string]any{"status": 200},
	}})
	cr := &stubCheckReader{chk: check.Snapshot{ID: 1, CheckType: check.CheckTypeHTTPHealth, TimeoutSec: 5}}
	crr := &stubCheckRunRepo{runs: []checkrun.Snapshot{{ID: 10, RunID: "r1", CheckID: 1}}}
	rec := &stubSLIRecorder{err: errors.New("sli failed")}
	uc := newTestUC(cr, crr, &stubEvaluator{result: evaluate.Result{OverallSeverity: evaluate.SeverityOK}}, rec, now)

	n, err := uc.RunBatch(context.Background(), "t1", 10)
	if err != nil {
		t.Fatalf("expected nil (sli error is logged, not returned), got %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 executed even when sli record fails, got %d", n)
	}
}

func TestRunBatch_SuccessWithoutOutput(t *testing.T) {
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	// Success but no Output: skips evaluation, severity stays OK.
	handler.Register("check_http_health", &mockHandler{result: execute.Result{Success: true, ResultCode: "0"}})
	cr := &stubCheckReader{chk: check.Snapshot{ID: 1, CheckType: check.CheckTypeHTTPHealth, TimeoutSec: 5}}
	crr := &stubCheckRunRepo{runs: []checkrun.Snapshot{{ID: 10, RunID: "r1", CheckID: 1}}}
	uc := newTestUC(cr, crr, &stubEvaluator{}, nil, now)

	n, err := uc.RunBatch(context.Background(), "t1", 10)
	if err != nil || n != 1 {
		t.Fatalf("expected 1 nil, got %d %v", n, err)
	}
	if len(crr.completed) != 1 || crr.completed[0].status != checkrun.StatusSuccess {
		t.Fatalf("expected success, got %v", crr.completed)
	}
	if crr.completed[0].severity != evaluate.SeverityOK {
		t.Fatalf("expected ok severity when success without output, got %q", crr.completed[0].severity)
	}
}
