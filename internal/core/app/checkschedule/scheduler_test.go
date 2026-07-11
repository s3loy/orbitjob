package checkschedule

import (
	"context"
	"errors"
	"testing"
	"time"

	"orbitjob/internal/core/domain/check"
	"orbitjob/internal/core/domain/checkrun"
)

type stubCheckRepo struct {
	due       []check.Snapshot
	listErr   error
	updated   []nextRunUpdate
	updateErr error
}

type nextRunUpdate struct {
	id        int64
	nextRunAt time.Time
}

func (s *stubCheckRepo) ListDue(_ context.Context, _ string, _ time.Time, _ int) ([]check.Snapshot, error) {
	return s.due, s.listErr
}

func (s *stubCheckRepo) UpdateNextRunAt(_ context.Context, _ string, id int64, nextRunAt time.Time) error {
	s.updated = append(s.updated, nextRunUpdate{id: id, nextRunAt: nextRunAt})
	return s.updateErr
}

type stubCheckRunRepo struct {
	created   []int64
	createErr error
}

func (s *stubCheckRunRepo) Create(_ context.Context, _ string, checkID int64, _ time.Time) (checkrun.Snapshot, error) {
	s.created = append(s.created, checkID)
	return checkrun.Snapshot{}, s.createErr
}

func newTestUC(cr *stubCheckRepo, crr *stubCheckRunRepo, now time.Time) *TickUseCase {
	uc := NewTickUseCase(cr, crr)
	uc.clock = func() time.Time { return now }
	return uc
}

func TestRunBatch_ListDueError(t *testing.T) {
	cr := &stubCheckRepo{listErr: errors.New("db down")}
	uc := newTestUC(cr, &stubCheckRunRepo{}, time.Now())
	n, err := uc.RunBatch(context.Background(), "t1", 10)
	if err == nil {
		t.Fatal("expected error from ListDue")
	}
	if n != 0 {
		t.Fatalf("expected 0 created on error, got %d", n)
	}
}

func TestRunBatch_NoDue(t *testing.T) {
	cr := &stubCheckRepo{}
	crr := &stubCheckRunRepo{}
	uc := newTestUC(cr, crr, time.Now())
	n, err := uc.RunBatch(context.Background(), "t1", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 created when no due checks, got %d", n)
	}
	if len(crr.created) != 0 || len(cr.updated) != 0 {
		t.Fatal("expected no creates or updates when no due checks")
	}
}

func TestRunBatch_CronSchedule(t *testing.T) {
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	cronExpr := "*/5 * * * *"
	cr := &stubCheckRepo{due: []check.Snapshot{{ID: 1, ScheduleType: check.ScheduleTypeCron, CronExpr: &cronExpr}}}
	crr := &stubCheckRunRepo{}
	uc := newTestUC(cr, crr, now)

	n, err := uc.RunBatch(context.Background(), "t1", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 created, got %d", n)
	}
	if len(crr.created) != 1 || crr.created[0] != 1 {
		t.Fatalf("expected check run created for check 1, got %v", crr.created)
	}
	// "*/5 * * * *" from 12:00:00 -> 12:05:00
	expected := now.Add(5 * time.Minute)
	if !cr.updated[0].nextRunAt.Equal(expected) {
		t.Fatalf("cron next run: expected %v, got %v", expected, cr.updated[0].nextRunAt)
	}
}

func TestRunBatch_IntervalSchedule(t *testing.T) {
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	interval := 30
	cr := &stubCheckRepo{due: []check.Snapshot{{ID: 2, ScheduleType: check.ScheduleTypeInterval, IntervalSec: &interval}}}
	crr := &stubCheckRunRepo{}
	uc := newTestUC(cr, crr, now)

	n, err := uc.RunBatch(context.Background(), "t1", 10)
	if err != nil || n != 1 {
		t.Fatalf("expected 1 nil, got %d %v", n, err)
	}
	expected := now.Add(30 * time.Second)
	if !cr.updated[0].nextRunAt.Equal(expected) {
		t.Fatalf("interval next run: expected %v, got %v", expected, cr.updated[0].nextRunAt)
	}
}

func TestRunBatch_DefaultScheduleFallback(t *testing.T) {
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	// Unknown schedule type with no cron/interval -> fallback to now + 1 min.
	cr := &stubCheckRepo{due: []check.Snapshot{{ID: 3, ScheduleType: "unknown"}}}
	crr := &stubCheckRunRepo{}
	uc := newTestUC(cr, crr, now)

	n, err := uc.RunBatch(context.Background(), "t1", 10)
	if err != nil || n != 1 {
		t.Fatalf("expected 1 nil, got %d %v", n, err)
	}
	expected := now.Add(time.Minute)
	if !cr.updated[0].nextRunAt.Equal(expected) {
		t.Fatalf("default next run: expected %v, got %v", expected, cr.updated[0].nextRunAt)
	}
}

func TestRunBatch_InvalidCronFallsBack(t *testing.T) {
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	cronExpr := "not a cron"
	cr := &stubCheckRepo{due: []check.Snapshot{{ID: 1, ScheduleType: check.ScheduleTypeCron, CronExpr: &cronExpr}}}
	crr := &stubCheckRunRepo{}
	uc := newTestUC(cr, crr, now)

	n, err := uc.RunBatch(context.Background(), "t1", 10)
	if err != nil || n != 1 {
		t.Fatalf("expected 1 nil despite invalid cron, got %d %v", n, err)
	}
	// Invalid cron must not abort; it falls back to now + 1 min.
	expected := now.Add(time.Minute)
	if !cr.updated[0].nextRunAt.Equal(expected) {
		t.Fatalf("invalid cron fallback: expected %v, got %v", expected, cr.updated[0].nextRunAt)
	}
}

func TestRunBatch_CreateErrorSkips(t *testing.T) {
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	cronExpr := "*/5 * * * *"
	cr := &stubCheckRepo{due: []check.Snapshot{{ID: 1, ScheduleType: check.ScheduleTypeCron, CronExpr: &cronExpr}}}
	crr := &stubCheckRunRepo{createErr: errors.New("write failed")}
	uc := newTestUC(cr, crr, now)

	n, err := uc.RunBatch(context.Background(), "t1", 10)
	if err != nil {
		t.Fatalf("expected nil error (create error is logged, not returned), got %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 created when Create fails, got %d", n)
	}
	if len(cr.updated) != 0 {
		t.Fatalf("expected no next-run update when Create fails, got %d", len(cr.updated))
	}
}

func TestRunBatch_UpdateErrorContinues(t *testing.T) {
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	interval := 30
	// UpdateNextRunAt fails; the run is still counted as created and the loop continues.
	cr := &stubCheckRepo{
		due:       []check.Snapshot{{ID: 5, ScheduleType: check.ScheduleTypeInterval, IntervalSec: &interval}},
		updateErr: errors.New("update failed"),
	}
	crr := &stubCheckRunRepo{}
	uc := newTestUC(cr, crr, now)

	n, err := uc.RunBatch(context.Background(), "t1", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 created even when UpdateNextRunAt fails, got %d", n)
	}
	if len(cr.updated) != 1 {
		t.Fatalf("expected UpdateNextRunAt to be attempted, got %d", len(cr.updated))
	}
}

func TestNewTickUseCase_DefaultClock(t *testing.T) {
	// Exercise the default clock set by NewTickUseCase (not overridden) so the
	// time.Now branch is covered. We only assert the run was created, not the
	// exact next_run_at (which depends on the real clock).
	interval := 60
	cr := &stubCheckRepo{due: []check.Snapshot{{ID: 7, ScheduleType: check.ScheduleTypeInterval, IntervalSec: &interval}}}
	crr := &stubCheckRunRepo{}
	uc := NewTickUseCase(cr, crr) // default clock

	n, err := uc.RunBatch(context.Background(), "t1", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 created with default clock, got %d", n)
	}
}
