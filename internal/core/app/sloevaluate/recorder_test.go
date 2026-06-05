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
	tenantID   string
	sliID      int64
	windowStart time.Time
	isGood     bool
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
