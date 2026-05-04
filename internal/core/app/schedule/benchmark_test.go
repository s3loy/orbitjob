package schedule

import (
	"context"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Mock repository
// ---------------------------------------------------------------------------

type benchSchedulerRepo struct {
	results []ScheduledOneResult
	errors  []error
	idx     int
}

func (m *benchSchedulerRepo) ScheduleOneDueCron(
	_ context.Context, _ time.Time,
	_ func(time.Time, DueCronJob) (ScheduleDecision, error),
) (ScheduledOneResult, bool, error) {
	if m.idx >= len(m.results) {
		return ScheduledOneResult{}, false, nil
	}
	r := m.results[m.idx]
	var err error
	if m.idx < len(m.errors) {
		err = m.errors[m.idx]
	}
	m.idx++
	return r, true, err
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkScheduleTick(b *testing.B) {
	fakeResults := make([]ScheduledOneResult, 100)
	for i := range fakeResults {
		fakeResults[i] = ScheduledOneResult{JobID: int64(i + 1), TenantID: "default", Created: true}
	}

	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		limit int
	}{
		{"limit=1", 1},
		{"limit=10", 10},
		{"limit=100", 100},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				repo := &benchSchedulerRepo{results: fakeResults}
				uc := NewTickUseCase(repo)
				_, _ = uc.RunBatch(context.Background(), now, tt.limit)
			}
		})
	}
}

func BenchmarkScheduleTick_NoDueJobs(b *testing.B) {
	repo := &benchSchedulerRepo{results: nil} // returns found=false immediately
	uc := NewTickUseCase(repo)
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = uc.RunBatch(context.Background(), now, 100)
	}
}
