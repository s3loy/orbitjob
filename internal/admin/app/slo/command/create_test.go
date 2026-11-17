package command

import (
	"context"
	"errors"
	"testing"
	"time"

	"orbitjob/internal/core/domain/slo"
	"orbitjob/internal/domain/validation"

	"orbitjob/internal/platform/scan"
)

type stubSLOWriter struct {
	in  slo.CreateSpec
	out slo.Snapshot
	err error
}

func (s *stubSLOWriter) Create(_ context.Context, _ string, spec slo.CreateSpec) (slo.Snapshot, error) {
	s.in = spec
	return s.out, s.err
}

type stubSLODeleter struct {
	lastID int64
	err    error
}

func (s *stubSLODeleter) Delete(_ context.Context, _, _ string, id int64, _ int) error {
	s.lastID = id
	return s.err
}

type stubSLOStatusChanger struct {
	lastStatus string
	out        slo.Snapshot
	err        error
}

func (s *stubSLOStatusChanger) ChangeStatus(_ context.Context, _, _ string, _ int64, _ int, status string) (slo.Snapshot, error) {
	s.lastStatus = status
	return s.out, s.err
}

func validSLOInput() CreateInput {
	return CreateInput{
		Name: "slo-1", TenantID: sloCreateFixtureTenantID, SLIID: 1, Target: 0.99,
		WindowType:        slo.WindowTypeRolling,
		WindowDuration:    30 * 24 * time.Hour,
		AlertFastBurnRate: 0.1,
		AlertSlowBurnRate: 0.02,
	}
}

func TestCreateSLO_Success(t *testing.T) {
	repo := &stubSLOWriter{out: slo.Snapshot{ID: 1, Name: "slo-1", Status: "active", Version: 1, CreatedAt: time.Now()}}
	uc := NewCreateSLOUseCase(repo)

	result, err := uc.Create(context.Background(), validSLOInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != 1 || result.Name != "slo-1" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if repo.in.Name != "slo-1" {
		t.Fatalf("expected repo input name=slo-1, got %+v", repo.in)
	}
}

func TestCreateSLO_NormalizeError(t *testing.T) {
	repo := &stubSLOWriter{}
	uc := NewCreateSLOUseCase(repo)

	in := validSLOInput()
	in.Name = ""
	_, err := uc.Create(context.Background(), in)
	if err == nil {
		t.Fatal("expected validation error for empty name")
	}
	if repo.in.Name != "" {
		t.Fatal("expected repo.Create not called on normalize error")
	}
}

func TestCreateSLO_RepoError(t *testing.T) {
	repo := &stubSLOWriter{err: errors.New("write failed")}
	uc := NewCreateSLOUseCase(repo)

	_, err := uc.Create(context.Background(), validSLOInput())
	if err == nil {
		t.Fatal("expected repo error")
	}
}

func TestDeleteSLO_Success(t *testing.T) {
	repo := &stubSLODeleter{}
	uc := NewDeleteSLOUseCase(repo)

	if err := uc.Delete(context.Background(), DeleteInput{TenantID: sloCreateFixtureTenantID, ID: 5, Version: 2}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.lastID != 5 {
		t.Fatalf("expected id 5, got %d", repo.lastID)
	}
}

func TestDeleteSLO_RepoError(t *testing.T) {
	repo := &stubSLODeleter{err: errors.New("not found")}
	uc := NewDeleteSLOUseCase(repo)

	if err := uc.Delete(context.Background(), DeleteInput{TenantID: sloCreateFixtureTenantID, ID: 5, Version: 2}); err == nil {
		t.Fatal("expected error")
	}
}

func TestDeleteSLO_InvalidID(t *testing.T) {
	repo := &stubSLODeleter{}
	uc := NewDeleteSLOUseCase(repo)

	if err := uc.Delete(context.Background(), DeleteInput{TenantID: sloCreateFixtureTenantID, ID: 0, Version: 2}); err == nil {
		t.Fatal("expected validation error for id<=0")
	}
	if repo.lastID != 0 {
		t.Fatal("expected repo.Delete not called for invalid id")
	}
}

func TestChangeSLOStatus_Success(t *testing.T) {
	repo := &stubSLOStatusChanger{out: slo.Snapshot{ID: 5, Status: slo.StatusPaused}}
	uc := NewChangeSLOStatusUseCase(repo)

	result, err := uc.ChangeStatus(context.Background(), sloCreateFixtureTenantID, ChangeStatusInput{ID: 5, Version: 2, Status: slo.StatusPaused})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != 5 || result.Status != slo.StatusPaused {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestChangeSLOStatus_InvalidID(t *testing.T) {
	repo := &stubSLOStatusChanger{}
	uc := NewChangeSLOStatusUseCase(repo)

	_, err := uc.ChangeStatus(context.Background(), sloCreateFixtureTenantID, ChangeStatusInput{ID: 0, Status: slo.StatusPaused})
	if err == nil {
		t.Fatal("expected validation error for id<=0")
	}
}

func TestChangeSLOStatus_InvalidStatus(t *testing.T) {
	repo := &stubSLOStatusChanger{}
	uc := NewChangeSLOStatusUseCase(repo)

	_, err := uc.ChangeStatus(context.Background(), sloCreateFixtureTenantID, ChangeStatusInput{ID: 5, Status: "bogus"})
	if err == nil {
		t.Fatal("expected validation error for invalid status")
	}
}

func TestChangeSLOStatus_RepoError(t *testing.T) {
	repo := &stubSLOStatusChanger{err: errors.New("stale version")}
	uc := NewChangeSLOStatusUseCase(repo)

	_, err := uc.ChangeStatus(context.Background(), sloCreateFixtureTenantID, ChangeStatusInput{ID: 5, Status: slo.StatusPaused})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPauseSLO_SetsPausedStatus(t *testing.T) {
	repo := &stubSLOStatusChanger{out: slo.Snapshot{ID: 5, Status: slo.StatusPaused}}
	uc := NewChangeSLOStatusUseCase(repo)

	_, err := uc.Pause(context.Background(), sloCreateFixtureTenantID, ChangeStatusInput{ID: 5, Version: 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.lastStatus != slo.StatusPaused {
		t.Fatalf("expected status paused, got %q", repo.lastStatus)
	}
}

func TestResumeSLO_SetsActiveStatus(t *testing.T) {
	repo := &stubSLOStatusChanger{out: slo.Snapshot{ID: 5, Status: slo.StatusActive}}
	uc := NewChangeSLOStatusUseCase(repo)

	_, err := uc.Resume(context.Background(), sloCreateFixtureTenantID, ChangeStatusInput{ID: 5, Version: 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.lastStatus != slo.StatusActive {
		t.Fatalf("expected status active, got %q", repo.lastStatus)
	}
}

var sloCreateFixtureTenantID = scan.GenerateID()

type precisionSLOWriter struct {
	calls    int
	duration time.Duration
}

func (s *precisionSLOWriter) Create(_ context.Context, _ string, spec slo.CreateSpec) (slo.Snapshot, error) {
	s.calls++
	s.duration = spec.WindowDuration
	return slo.Snapshot{ID: 1, WindowDuration: spec.WindowDuration}, nil
}

func TestCreateSLO_WindowDurationPrecision(t *testing.T) {
	for _, tc := range []struct {
		name     string
		duration time.Duration
		valid    bool
	}{
		{name: "zero", duration: 0},
		{name: "negative", duration: -time.Second},
		{name: "nanosecond", duration: time.Nanosecond},
		{name: "millisecond", duration: time.Millisecond},
		{name: "below one second", duration: 999 * time.Millisecond},
		{name: "fractional seconds", duration: 1500 * time.Millisecond},
		{name: "above maximum", duration: slo.MaxWindowDuration + time.Second},
		{name: "one second", duration: time.Second, valid: true},
		{name: "two seconds", duration: 2 * time.Second, valid: true},
		{name: "one day", duration: 24 * time.Hour, valid: true},
		{name: "maximum", duration: slo.MaxWindowDuration, valid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &precisionSLOWriter{}
			in := validSLOInput()
			in.WindowDuration = tc.duration
			out, err := NewCreateSLOUseCase(repo).Create(context.Background(), in)
			if tc.valid {
				if err != nil {
					t.Fatalf("valid duration rejected: %v", err)
				}
				if repo.calls != 1 || repo.duration != tc.duration || out.WindowDuration != tc.duration.String() {
					t.Fatalf("duration changed: calls=%d stored=%v output=%q, want %v", repo.calls, repo.duration, out.WindowDuration, tc.duration)
				}
				return
			}
			var validationErr *validation.Error
			if !validation.As(err, &validationErr) || validationErr.Field != "window_duration" {
				t.Fatalf("expected window_duration validation error, got %v", err)
			}
			if repo.calls != 0 {
				t.Fatalf("invalid duration reached repository %d times", repo.calls)
			}
		})
	}
}
