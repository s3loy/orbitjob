package command

import (
	"context"
	"errors"
	"testing"
	"time"

	domainjob "orbitjob/internal/core/domain/job"
)

func TestGetMaxJobs(t *testing.T) {
	tests := []struct {
		name   string
		quotas map[string]any
		want   int
		wantOK bool
	}{
		{"nil quotas", nil, 0, false},
		{"empty quotas", map[string]any{}, 0, false},
		{"max_jobs present float64", map[string]any{"max_jobs": float64(10)}, 10, true},
		{"max_jobs present float32", map[string]any{"max_jobs": float32(5)}, 5, true},
		{"max_jobs present int", map[string]any{"max_jobs": int(3)}, 3, true},
		{"max_jobs present int64", map[string]any{"max_jobs": int64(7)}, 7, true},
		{"max_jobs zero", map[string]any{"max_jobs": float64(0)}, 0, false},
		{"max_jobs negative", map[string]any{"max_jobs": float64(-1)}, 0, false},
		{"max_jobs string (invalid)", map[string]any{"max_jobs": "ten"}, 0, false},
		{"max_jobs bool (invalid)", map[string]any{"max_jobs": true}, 0, false},
		{"other key only", map[string]any{"max_concurrent": float64(5)}, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := getMaxJobs(tt.quotas)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestToFloatInt(t *testing.T) {
	tests := []struct {
		name   string
		v      any
		want   int
		wantOK bool
	}{
		{"float64", float64(3.0), 3, true},
		{"float32", float32(2.0), 2, true},
		{"int", int(5), 5, true},
		{"int64", int64(7), 7, true},
		{"string", "10", 0, false},
		{"bool", true, 0, false},
		{"nil", nil, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := toFloatInt(tt.v)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}

type stubQuotaReader struct {
	quotas map[string]any
	err    error
}

func (r *stubQuotaReader) GetQuota(_ context.Context, _ string) (map[string]any, error) {
	return r.quotas, r.err
}

type countingRepo struct {
	*testRepo
	activeCount int
	countErr    error
}

func (r *countingRepo) CountActiveByTenant(_ context.Context, _ string) (int, error) {
	return r.activeCount, r.countErr
}

func TestCreateJobUseCase_Create_QuotaExceeded(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	quotaReader := &stubQuotaReader{
		quotas: map[string]any{"max_jobs": float64(5)},
	}
	repo := &countingRepo{
		testRepo:    &testRepo{},
		activeCount: 5, // already at max
	}
	uc := &CreateJobUseCase{
		repo:        repo,
		quotaReader: quotaReader,
		clock:       fixedClock{t: now},
	}

	_, err := uc.Create(context.Background(), CreateInput{
		Name:        "new-job",
		TriggerType: domainjob.TriggerTypeManual,
		HandlerType: "exec",
	})
	if err == nil {
		t.Fatal("expected quota exceeded error, got nil")
	}
}

func TestCreateJobUseCase_Create_QuotaOK(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	quotaReader := &stubQuotaReader{
		quotas: map[string]any{"max_jobs": float64(5)},
	}
	repo := &countingRepo{
		testRepo: &testRepo{
			out: domainjob.Snapshot{ID: 2, Name: "new-job", TenantID: "default", Status: "active"},
		},
		activeCount: 3, // under limit
	}
	uc := &CreateJobUseCase{
		repo:        repo,
		quotaReader: quotaReader,
		clock:       fixedClock{t: now},
	}

	out, err := uc.Create(context.Background(), CreateInput{
		Name:        "new-job",
		TriggerType: domainjob.TriggerTypeManual,
		HandlerType: "exec",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if out.ID != 2 {
		t.Fatalf("expected ID=2, got %d", out.ID)
	}
}

func TestCreateJobUseCase_Create_QuotaReaderError(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	quotaReader := &stubQuotaReader{err: errors.New("db down")}
	repo := &countingRepo{testRepo: &testRepo{}}
	uc := &CreateJobUseCase{
		repo:        repo,
		quotaReader: quotaReader,
		clock:       fixedClock{t: now},
	}

	_, err := uc.Create(context.Background(), CreateInput{
		Name:        "new-job",
		TriggerType: domainjob.TriggerTypeManual,
		HandlerType: "exec",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCreateJobUseCase_Create_CountActiveError(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	quotaReader := &stubQuotaReader{
		quotas: map[string]any{"max_jobs": float64(5)},
	}
	repo := &countingRepo{
		testRepo: &testRepo{},
		countErr: errors.New("count failed"),
	}
	uc := &CreateJobUseCase{
		repo:        repo,
		quotaReader: quotaReader,
		clock:       fixedClock{t: now},
	}

	_, err := uc.Create(context.Background(), CreateInput{
		Name:        "new-job",
		TriggerType: domainjob.TriggerTypeManual,
		HandlerType: "exec",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
