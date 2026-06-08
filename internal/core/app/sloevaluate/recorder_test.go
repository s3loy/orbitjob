package sloevaluate

import (
	"context"
	"errors"
	"testing"
	"time"

	"orbitjob/internal/core/domain/checkrun"
	"orbitjob/internal/core/domain/sli"
)

// ---------------------------------------------------------------------------
// Stubs
// ---------------------------------------------------------------------------

type stubSLIReader struct {
	findFunc func(ctx context.Context, tenantID string, checkID int64) ([]sli.Snapshot, error)
}

func (s *stubSLIReader) FindByCheckID(ctx context.Context, tenantID string, checkID int64) ([]sli.Snapshot, error) {
	return s.findFunc(ctx, tenantID, checkID)
}

type stubSnapshotWriter struct {
	calls []incrementCall
	err   error
}

type incrementCall struct {
	tenantID    string
	sliID       int64
	windowStart time.Time
	isGood      bool
}

func (s *stubSnapshotWriter) IncrementSnapshot(ctx context.Context, tenantID string, sliID int64, windowStart time.Time, isGood bool) error {
	s.calls = append(s.calls, incrementCall{tenantID: tenantID, sliID: sliID, windowStart: windowStart, isGood: isGood})
	return s.err
}

func ptr[T any](v T) *T {
	return &v
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestRecordCheckRun_Availability_SuccessIsGood(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 30, 0, 0, time.UTC)
	windowStart := now.Truncate(5 * time.Minute)

	reader := &stubSLIReader{
		findFunc: func(_ context.Context, _ string, _ int64) ([]sli.Snapshot, error) {
			return []sli.Snapshot{
				{ID: 1, SLIType: sli.TypeAvailability},
			}, nil
		},
	}
	writer := &stubSnapshotWriter{}
	recorder := NewCheckRunRecorder(reader, writer)
	recorder.clock = func() time.Time { return now }

	run := checkrun.Snapshot{ID: 100, Status: checkrun.StatusSuccess}
	err := recorder.RecordCheckRun(context.Background(), "tenant-a", 42, run)
	if err != nil {
		t.Fatalf("RecordCheckRun() error = %v", err)
	}

	if len(writer.calls) != 1 {
		t.Fatalf("expected 1 increment call, got %d", len(writer.calls))
	}
	call := writer.calls[0]
	if call.sliID != 1 || call.tenantID != "tenant-a" || !call.windowStart.Equal(windowStart) || !call.isGood {
		t.Fatalf("unexpected increment call: %+v", call)
	}
}

func TestRecordCheckRun_Availability_FailedIsBad(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 30, 0, 0, time.UTC)
	windowStart := now.Truncate(5 * time.Minute)

	reader := &stubSLIReader{
		findFunc: func(_ context.Context, _ string, _ int64) ([]sli.Snapshot, error) {
			return []sli.Snapshot{
				{ID: 2, SLIType: sli.TypeAvailability},
			}, nil
		},
	}
	writer := &stubSnapshotWriter{}
	recorder := NewCheckRunRecorder(reader, writer)
	recorder.clock = func() time.Time { return now }

	run := checkrun.Snapshot{ID: 101, Status: checkrun.StatusFailed}
	err := recorder.RecordCheckRun(context.Background(), "tenant-a", 42, run)
	if err != nil {
		t.Fatalf("RecordCheckRun() error = %v", err)
	}

	if len(writer.calls) != 1 {
		t.Fatalf("expected 1 increment call, got %d", len(writer.calls))
	}
	call := writer.calls[0]
	if call.sliID != 2 || call.tenantID != "tenant-a" || !call.windowStart.Equal(windowStart) || call.isGood {
		t.Fatalf("expected isGood=false for failed status, got call=%+v", call)
	}
}

func TestRecordCheckRun_Quality_SeverityOKIsGood(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 30, 0, 0, time.UTC)
	windowStart := now.Truncate(5 * time.Minute)

	reader := &stubSLIReader{
		findFunc: func(_ context.Context, _ string, _ int64) ([]sli.Snapshot, error) {
			return []sli.Snapshot{
				{ID: 3, SLIType: sli.TypeQuality},
			}, nil
		},
	}
	writer := &stubSnapshotWriter{}
	recorder := NewCheckRunRecorder(reader, writer)
	recorder.clock = func() time.Time { return now }

	run := checkrun.Snapshot{ID: 102, Status: checkrun.StatusSuccess, Severity: ptr(checkrun.SeverityOK)}
	err := recorder.RecordCheckRun(context.Background(), "tenant-a", 42, run)
	if err != nil {
		t.Fatalf("RecordCheckRun() error = %v", err)
	}

	if len(writer.calls) != 1 {
		t.Fatalf("expected 1 increment call, got %d", len(writer.calls))
	}
	call := writer.calls[0]
	if call.sliID != 3 || call.tenantID != "tenant-a" || !call.windowStart.Equal(windowStart) || !call.isGood {
		t.Fatalf("expected isGood=true for severity=ok, got call=%+v", call)
	}
}

func TestRecordCheckRun_NoAssociatedSLIs(t *testing.T) {
	reader := &stubSLIReader{
		findFunc: func(_ context.Context, _ string, _ int64) ([]sli.Snapshot, error) {
			return nil, nil
		},
	}
	writer := &stubSnapshotWriter{}
	recorder := NewCheckRunRecorder(reader, writer)

	run := checkrun.Snapshot{ID: 103, Status: checkrun.StatusSuccess}
	err := recorder.RecordCheckRun(context.Background(), "tenant-a", 42, run)
	if err != nil {
		t.Fatalf("RecordCheckRun() error = %v", err)
	}

	if len(writer.calls) != 0 {
		t.Fatalf("expected 0 increment calls when no SLIs, got %d", len(writer.calls))
	}
}

func TestRecordCheckRun_Latency_DurationMsCriteria(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 30, 0, 0, time.UTC)
	windowStart := now.Truncate(5 * time.Minute)

	reader := &stubSLIReader{
		findFunc: func(_ context.Context, _ string, _ int64) ([]sli.Snapshot, error) {
			return []sli.Snapshot{
				{
					ID:      4,
					SLIType: sli.TypeLatency,
					GoodEventCriteria: map[string]any{
						"duration_ms": map[string]any{"op": "<=", "value": 1000},
					},
				},
			}, nil
		},
	}
	writer := &stubSnapshotWriter{}
	recorder := NewCheckRunRecorder(reader, writer)
	recorder.clock = func() time.Time { return now }

	// Case: duration_ms = 500 <= 1000 -> good
	run := checkrun.Snapshot{ID: 104, Status: checkrun.StatusSuccess, DurationMs: ptr(500)}
	err := recorder.RecordCheckRun(context.Background(), "tenant-a", 42, run)
	if err != nil {
		t.Fatalf("RecordCheckRun() error = %v", err)
	}

	if len(writer.calls) != 1 {
		t.Fatalf("expected 1 increment call, got %d", len(writer.calls))
	}
	if !writer.calls[0].isGood {
		t.Fatalf("expected isGood=true for duration_ms=500 <= 1000")
	}

	// Case: duration_ms = 1500 > 1000 -> bad
	writer.calls = nil
	run2 := checkrun.Snapshot{ID: 105, Status: checkrun.StatusSuccess, DurationMs: ptr(1500)}
	err = recorder.RecordCheckRun(context.Background(), "tenant-a", 42, run2)
	if err != nil {
		t.Fatalf("RecordCheckRun() error = %v", err)
	}

	if len(writer.calls) != 1 {
		t.Fatalf("expected 1 increment call, got %d", len(writer.calls))
	}
	if writer.calls[0].isGood {
		t.Fatalf("expected isGood=false for duration_ms=1500 > 1000")
	}
	if !writer.calls[0].windowStart.Equal(windowStart) {
		t.Fatalf("expected windowStart=%v, got %v", windowStart, writer.calls[0].windowStart)
	}
}

func TestRecordCheckRun_MultipleSLIs(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 30, 0, 0, time.UTC)

	reader := &stubSLIReader{
		findFunc: func(_ context.Context, _ string, _ int64) ([]sli.Snapshot, error) {
			return []sli.Snapshot{
				{ID: 10, SLIType: sli.TypeAvailability},
				{ID: 11, SLIType: sli.TypeQuality},
			}, nil
		},
	}
	writer := &stubSnapshotWriter{}
	recorder := NewCheckRunRecorder(reader, writer)
	recorder.clock = func() time.Time { return now }

	run := checkrun.Snapshot{ID: 200, Status: checkrun.StatusSuccess, Severity: ptr(checkrun.SeverityOK)}
	err := recorder.RecordCheckRun(context.Background(), "tenant-b", 99, run)
	if err != nil {
		t.Fatalf("RecordCheckRun() error = %v", err)
	}

	if len(writer.calls) != 2 {
		t.Fatalf("expected 2 increment calls, got %d", len(writer.calls))
	}
	if writer.calls[0].sliID != 10 || !writer.calls[0].isGood {
		t.Fatalf("first call unexpected: %+v", writer.calls[0])
	}
	if writer.calls[1].sliID != 11 || !writer.calls[1].isGood {
		t.Fatalf("second call unexpected: %+v", writer.calls[1])
	}
}

func TestRecordCheckRun_ReaderError(t *testing.T) {
	wantErr := errors.New("db down")
	reader := &stubSLIReader{
		findFunc: func(_ context.Context, _ string, _ int64) ([]sli.Snapshot, error) {
			return nil, wantErr
		},
	}
	writer := &stubSnapshotWriter{}
	recorder := NewCheckRunRecorder(reader, writer)

	run := checkrun.Snapshot{ID: 300, Status: checkrun.StatusSuccess}
	err := recorder.RecordCheckRun(context.Background(), "tenant-a", 42, run)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected error wrapping %v, got %v", wantErr, err)
	}
}

func TestRecordCheckRun_WriterError_Continues(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 30, 0, 0, time.UTC)

	reader := &stubSLIReader{
		findFunc: func(_ context.Context, _ string, _ int64) ([]sli.Snapshot, error) {
			return []sli.Snapshot{
				{ID: 20, SLIType: sli.TypeAvailability},
				{ID: 21, SLIType: sli.TypeAvailability},
			}, nil
		},
	}
	writer := &stubSnapshotWriter{err: errors.New("write failed")}
	recorder := NewCheckRunRecorder(reader, writer)
	recorder.clock = func() time.Time { return now }

	run := checkrun.Snapshot{ID: 400, Status: checkrun.StatusSuccess}
	// Writer errors are logged and continue; RecordCheckRun returns nil.
	err := recorder.RecordCheckRun(context.Background(), "tenant-a", 42, run)
	if err != nil {
		t.Fatalf("RecordCheckRun() error = %v, expected nil (errors continue)", err)
	}

	if len(writer.calls) != 2 {
		t.Fatalf("expected 2 increment calls even with errors, got %d", len(writer.calls))
	}
}

func TestRecordCheckRun_Quality_NilSeverity_SkipsWithError(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 30, 0, 0, time.UTC)

	reader := &stubSLIReader{
		findFunc: func(_ context.Context, _ string, _ int64) ([]sli.Snapshot, error) {
			return []sli.Snapshot{
				{ID: 5, SLIType: sli.TypeQuality},
			}, nil
		},
	}
	writer := &stubSnapshotWriter{}
	recorder := NewCheckRunRecorder(reader, writer)
	recorder.clock = func() time.Time { return now }

	run := checkrun.Snapshot{ID: 500, Status: checkrun.StatusSuccess, Severity: nil}
	// Missing good_event_criteria and severity -> configuration error -> skipped
	err := recorder.RecordCheckRun(context.Background(), "tenant-a", 42, run)
	if err != nil {
		t.Fatalf("RecordCheckRun() error = %v", err)
	}

	if len(writer.calls) != 0 {
		t.Fatalf("expected 0 increment calls when quality sli is misconfigured, got %d", len(writer.calls))
	}
}

func TestRecordCheckRun_Latency_NoCriteria_SkipsWithError(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 30, 0, 0, time.UTC)

	reader := &stubSLIReader{
		findFunc: func(_ context.Context, _ string, _ int64) ([]sli.Snapshot, error) {
			return []sli.Snapshot{
				{ID: 7, SLIType: sli.TypeLatency},
			}, nil
		},
	}
	writer := &stubSnapshotWriter{}
	recorder := NewCheckRunRecorder(reader, writer)
	recorder.clock = func() time.Time { return now }

	run := checkrun.Snapshot{ID: 700, Status: checkrun.StatusSuccess, DurationMs: ptr(500)}
	err := recorder.RecordCheckRun(context.Background(), "tenant-a", 42, run)
	if err != nil {
		t.Fatalf("RecordCheckRun() error = %v", err)
	}

	if len(writer.calls) != 0 {
		t.Fatalf("expected 0 increment calls when latency sli lacks criteria, got %d", len(writer.calls))
	}
}

func TestRecordCheckRun_Availability_CustomCriteria(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 30, 0, 0, time.UTC)

	reader := &stubSLIReader{
		findFunc: func(_ context.Context, _ string, _ int64) ([]sli.Snapshot, error) {
			return []sli.Snapshot{
				{
					ID:      6,
					SLIType: sli.TypeAvailability,
					GoodEventCriteria: map[string]any{
						"status": checkrun.StatusSuccess,
					},
				},
			}, nil
		},
	}
	writer := &stubSnapshotWriter{}
	recorder := NewCheckRunRecorder(reader, writer)
	recorder.clock = func() time.Time { return now }

	// Matches criteria
	run := checkrun.Snapshot{ID: 600, Status: checkrun.StatusSuccess}
	err := recorder.RecordCheckRun(context.Background(), "tenant-a", 42, run)
	if err != nil {
		t.Fatalf("RecordCheckRun() error = %v", err)
	}
	if !writer.calls[0].isGood {
		t.Fatalf("expected isGood=true when status matches custom criteria")
	}

	// Does not match criteria
	writer.calls = nil
	run2 := checkrun.Snapshot{ID: 601, Status: checkrun.StatusFailed}
	err = recorder.RecordCheckRun(context.Background(), "tenant-a", 42, run2)
	if err != nil {
		t.Fatalf("RecordCheckRun() error = %v", err)
	}
	if writer.calls[0].isGood {
		t.Fatalf("expected isGood=false when status does not match custom criteria")
	}
}

// ---------------------------------------------------------------------------
// evaluateCriteria tests
// ---------------------------------------------------------------------------

func TestEvaluateCriteria_StatusTypeError(t *testing.T) {
	recorder := NewCheckRunRecorder(nil, nil)
	_, err := recorder.evaluateCriteria(checkrun.Snapshot{Status: checkrun.StatusSuccess}, map[string]any{"status": 123})
	if err == nil {
		t.Fatal("expected error for non-string status criteria")
	}
}

func TestEvaluateCriteria_SeverityTypeError(t *testing.T) {
	recorder := NewCheckRunRecorder(nil, nil)
	_, err := recorder.evaluateCriteria(checkrun.Snapshot{Severity: ptr(checkrun.SeverityOK)}, map[string]any{"severity": 123})
	if err == nil {
		t.Fatal("expected error for non-string severity criteria")
	}
}

func TestEvaluateCriteria_DurationMsNil(t *testing.T) {
	recorder := NewCheckRunRecorder(nil, nil)
	good, err := recorder.evaluateCriteria(checkrun.Snapshot{DurationMs: nil}, map[string]any{"duration_ms": map[string]any{"op": "<=", "value": 1000}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if good {
		t.Fatal("expected false when DurationMs is nil")
	}
}

func TestEvaluateCriteria_OutputKeyMatch(t *testing.T) {
	recorder := NewCheckRunRecorder(nil, nil)
	good, err := recorder.evaluateCriteria(checkrun.Snapshot{Output: map[string]any{"cpu": 50.0}}, map[string]any{"cpu": map[string]any{"op": "<=", "value": 100}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !good {
		t.Fatal("expected true when output key matches criteria")
	}
}

func TestEvaluateCriteria_OutputKeyMismatch(t *testing.T) {
	recorder := NewCheckRunRecorder(nil, nil)
	good, err := recorder.evaluateCriteria(checkrun.Snapshot{Output: map[string]any{"cpu": 150.0}}, map[string]any{"cpu": map[string]any{"op": "<=", "value": 100}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if good {
		t.Fatal("expected false when output key exceeds criteria")
	}
}

func TestEvaluateCriteria_OutputKeyTypeError(t *testing.T) {
	recorder := NewCheckRunRecorder(nil, nil)
	_, err := recorder.evaluateCriteria(checkrun.Snapshot{Output: map[string]any{"cpu": "not-a-number"}}, map[string]any{"cpu": map[string]any{"op": "<=", "value": 100}})
	if err == nil {
		t.Fatal("expected error when output key value cannot convert to float64")
	}
}

func TestEvaluateCriteria_MissingOutputKey(t *testing.T) {
	recorder := NewCheckRunRecorder(nil, nil)
	good, err := recorder.evaluateCriteria(checkrun.Snapshot{Output: map[string]any{}}, map[string]any{"cpu": map[string]any{"op": "<=", "value": 100}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if good {
		t.Fatal("expected false when output key is missing")
	}
}

// ---------------------------------------------------------------------------
// evaluateNumericCondition tests
// ---------------------------------------------------------------------------

func TestEvaluateNumericCondition_SimpleEquality(t *testing.T) {
	recorder := NewCheckRunRecorder(nil, nil)
	good, err := recorder.evaluateNumericCondition(42.0, 42.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !good {
		t.Fatal("expected true for simple equality")
	}
}

func TestEvaluateNumericCondition_SimpleInequality(t *testing.T) {
	recorder := NewCheckRunRecorder(nil, nil)
	good, err := recorder.evaluateNumericCondition(42.0, 99.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if good {
		t.Fatal("expected false for simple inequality")
	}
}

func TestEvaluateNumericCondition_MissingOp(t *testing.T) {
	recorder := NewCheckRunRecorder(nil, nil)
	_, err := recorder.evaluateNumericCondition(42.0, map[string]any{"value": 100})
	if err == nil {
		t.Fatal("expected error when op is missing")
	}
}

func TestEvaluateNumericCondition_MissingValue(t *testing.T) {
	recorder := NewCheckRunRecorder(nil, nil)
	_, err := recorder.evaluateNumericCondition(42.0, map[string]any{"op": "<="})
	if err == nil {
		t.Fatal("expected error when value is missing")
	}
}

func TestEvaluateNumericCondition_AllOperators(t *testing.T) {
	recorder := NewCheckRunRecorder(nil, nil)
	tests := []struct {
		op     string
		actual float64
		thresh float64
		want   bool
	}{
		{"<", 5.0, 10.0, true},
		{"<", 15.0, 10.0, false},
		{"<=", 10.0, 10.0, true},
		{"<=", 15.0, 10.0, false},
		{">", 15.0, 10.0, true},
		{">", 5.0, 10.0, false},
		{">=", 10.0, 10.0, true},
		{">=", 5.0, 10.0, false},
		{"==", 10.0, 10.0, true},
		{"==", 5.0, 10.0, false},
		{"!=", 5.0, 10.0, true},
		{"!=", 10.0, 10.0, false},
	}
	for _, tt := range tests {
		t.Run(tt.op, func(t *testing.T) {
			good, err := recorder.evaluateNumericCondition(tt.actual, map[string]any{"op": tt.op, "value": tt.thresh})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if good != tt.want {
				t.Fatalf("op=%s actual=%v thresh=%v: want %v, got %v", tt.op, tt.actual, tt.thresh, tt.want, good)
			}
		})
	}
}

func TestEvaluateNumericCondition_UnsupportedOp(t *testing.T) {
	recorder := NewCheckRunRecorder(nil, nil)
	_, err := recorder.evaluateNumericCondition(42.0, map[string]any{"op": "~~", "value": 100})
	if err == nil {
		t.Fatal("expected error for unsupported operator")
	}
}

// ---------------------------------------------------------------------------
// toFloat64 tests
// ---------------------------------------------------------------------------

func TestToFloat64_AllTypes(t *testing.T) {
	tests := []struct {
		input any
		want  float64
	}{
		{float64(3.14), 3.14},
		{float32(2.5), 2.5},
		{int(42), 42.0},
		{int64(99), 99.0},
		{int32(7), 7.0},
		{uint(10), 10.0},
		{uint64(100), 100.0},
	}
	for _, tt := range tests {
		got, err := toFloat64(tt.input)
		if err != nil {
			t.Fatalf("toFloat64(%v) error = %v", tt.input, err)
		}
		if got != tt.want {
			t.Fatalf("toFloat64(%v) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestToFloat64_Unsupported(t *testing.T) {
	_, err := toFloat64("not-a-number")
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
}

// ---------------------------------------------------------------------------
// isGoodEvent for Custom SLI
// ---------------------------------------------------------------------------

func TestIsGoodEvent_CustomSLI_MatchesCriteria(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 30, 0, 0, time.UTC)

	reader := &stubSLIReader{
		findFunc: func(_ context.Context, _ string, _ int64) ([]sli.Snapshot, error) {
			return []sli.Snapshot{
				{
					ID:      50,
					SLIType: sli.TypeCustom,
					GoodEventCriteria: map[string]any{
						"status": checkrun.StatusSuccess,
					},
				},
			}, nil
		},
	}
	writer := &stubSnapshotWriter{}
	recorder := NewCheckRunRecorder(reader, writer)
	recorder.clock = func() time.Time { return now }

	run := checkrun.Snapshot{ID: 500, Status: checkrun.StatusSuccess}
	err := recorder.RecordCheckRun(context.Background(), "tenant-a", 42, run)
	if err != nil {
		t.Fatalf("RecordCheckRun() error = %v", err)
	}
	if len(writer.calls) != 1 || !writer.calls[0].isGood {
		t.Fatal("expected custom SLI with matching criteria to be good")
	}
}

func TestIsGoodEvent_CustomSLI_MismatchCriteria(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 30, 0, 0, time.UTC)

	reader := &stubSLIReader{
		findFunc: func(_ context.Context, _ string, _ int64) ([]sli.Snapshot, error) {
			return []sli.Snapshot{
				{
					ID:      51,
					SLIType: sli.TypeCustom,
					GoodEventCriteria: map[string]any{
						"status": checkrun.StatusSuccess,
					},
				},
			}, nil
		},
	}
	writer := &stubSnapshotWriter{}
	recorder := NewCheckRunRecorder(reader, writer)
	recorder.clock = func() time.Time { return now }

	run := checkrun.Snapshot{ID: 501, Status: checkrun.StatusFailed}
	err := recorder.RecordCheckRun(context.Background(), "tenant-a", 42, run)
	if err != nil {
		t.Fatalf("RecordCheckRun() error = %v", err)
	}
	if len(writer.calls) != 1 || writer.calls[0].isGood {
		t.Fatal("expected custom SLI with mismatching criteria to be bad")
	}
}

// ---------------------------------------------------------------------------
// isGoodQuality with criteria
// ---------------------------------------------------------------------------

func TestIsGoodQuality_WithCriteria(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 30, 0, 0, time.UTC)

	reader := &stubSLIReader{
		findFunc: func(_ context.Context, _ string, _ int64) ([]sli.Snapshot, error) {
			return []sli.Snapshot{
				{
					ID:      60,
					SLIType: sli.TypeQuality,
					GoodEventCriteria: map[string]any{
						"status": checkrun.StatusSuccess,
					},
				},
			}, nil
		},
	}
	writer := &stubSnapshotWriter{}
	recorder := NewCheckRunRecorder(reader, writer)
	recorder.clock = func() time.Time { return now }

	// Matches criteria
	run := checkrun.Snapshot{ID: 600, Status: checkrun.StatusSuccess}
	err := recorder.RecordCheckRun(context.Background(), "tenant-a", 42, run)
	if err != nil {
		t.Fatalf("RecordCheckRun() error = %v", err)
	}
	if len(writer.calls) != 1 || !writer.calls[0].isGood {
		t.Fatal("expected quality SLI with matching criteria to be good")
	}

	// Does not match criteria
	writer.calls = nil
	run2 := checkrun.Snapshot{ID: 601, Status: checkrun.StatusFailed}
	err = recorder.RecordCheckRun(context.Background(), "tenant-a", 42, run2)
	if err != nil {
		t.Fatalf("RecordCheckRun() error = %v", err)
	}
	if len(writer.calls) != 1 || writer.calls[0].isGood {
		t.Fatal("expected quality SLI with mismatching criteria to be bad")
	}
}

func TestEvaluateCriteria_SeverityMatch(t *testing.T) {
	recorder := NewCheckRunRecorder(nil, nil)
	good, err := recorder.evaluateCriteria(
		checkrun.Snapshot{Severity: ptr(checkrun.SeverityOK)},
		map[string]any{"severity": checkrun.SeverityOK},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !good {
		t.Fatal("expected true when severity matches")
	}
}

func TestEvaluateCriteria_DurationMsConditionError(t *testing.T) {
	recorder := NewCheckRunRecorder(nil, nil)
	_, err := recorder.evaluateCriteria(
		checkrun.Snapshot{DurationMs: ptr(100)},
		map[string]any{"duration_ms": "not-a-map"},
	)
	if err == nil {
		t.Fatal("expected error for malformed duration_ms condition")
	}
}

func TestEvaluateCriteria_OutputNil(t *testing.T) {
	recorder := NewCheckRunRecorder(nil, nil)
	good, err := recorder.evaluateCriteria(
		checkrun.Snapshot{Output: nil},
		map[string]any{"cpu": map[string]any{"op": "<=", "value": 100}},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if good {
		t.Fatal("expected false when output is nil")
	}
}

func TestEvaluateCriteria_OutputKeyConditionError(t *testing.T) {
	recorder := NewCheckRunRecorder(nil, nil)
	_, err := recorder.evaluateCriteria(
		checkrun.Snapshot{Output: map[string]any{"cpu": 50.0}},
		map[string]any{"cpu": "not-a-map"},
	)
	if err == nil {
		t.Fatal("expected error for malformed output key condition")
	}
}

func TestEvaluateNumericCondition_SimpleEqualityError(t *testing.T) {
	recorder := NewCheckRunRecorder(nil, nil)
	_, err := recorder.evaluateNumericCondition(42.0, "not-a-number")
	if err == nil {
		t.Fatal("expected error for simple equality with non-numeric value")
	}
}
