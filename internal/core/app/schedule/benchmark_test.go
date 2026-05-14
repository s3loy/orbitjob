package schedule

import (
	"context"
	"testing"
	"time"

	domain "orbitjob/internal/core/domain"
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

func (m *benchSchedulerRepo) ScheduleBatch(
	ctx context.Context, _ time.Time, limit int,
	_ func(time.Time, DueCronJob) (ScheduleDecision, error),
	classifyError func(error) domain.ErrorClass,
) (BatchCounts, error) {
	counts := BatchCounts{}
	for i := 0; i < limit; i++ {
		result, found, err := m.ScheduleOneDueCron(ctx, time.Time{}, nil)
		if err != nil {
			class := classifyError(err)
			switch class {
			case domain.FatalWorthy:
				counts.Fatal++
				counts.Handled++
				return counts, nil
			case domain.BackoffWorthy:
				counts.Backoff++
				counts.Handled++
				continue
			case domain.SkipWorthy:
				counts.Skipped++
				counts.Handled++
				continue
			default:
				return counts, err
			}
		}
		if !found {
			break
		}
		counts.Handled++
		if result.Created {
			counts.Scheduled++
		}
	}
	return counts, nil
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
				uc := NewTickUseCase(repo, testClassify)
				_, _ = uc.RunBatch(context.Background(), now, tt.limit)
			}
		})
	}
}

func BenchmarkScheduleTick_NoDueJobs(b *testing.B) {
	repo := &benchSchedulerRepo{results: nil} // returns found=false immediately
	uc := NewTickUseCase(repo, testClassify)
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = uc.RunBatch(context.Background(), now, 100)
	}
}
