//go:build integration

package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"orbitjob/internal/core/app/dispatch"
	"orbitjob/internal/core/app/execute"
	"orbitjob/internal/core/app/schedule"
	"orbitjob/internal/platform/postgrestest"

	domaininstance "orbitjob/internal/core/domain/instance"
)

// ---------------------------------------------------------------------------
// Pipeline: Schedule → Dispatch (end-to-end tick latency)
// ---------------------------------------------------------------------------

func BenchmarkPipelineScheduleDispatch(b *testing.B) {
	db := postgrestest.BenchDB(b)

	now := time.Now().UTC().Truncate(time.Second)
	schedRepo := NewSchedulerRepository(db)
	dispRepo := NewDispatchRepository(db)
	schedUC := schedule.NewTickUseCase(schedRepo)
	dispUC := dispatch.NewTickUseCase(dispRepo)

	claimSpec := domaininstance.ClaimSpec{
		TenantID:       "tenant-pipe",
		Now:            now,
		LeaseExpiresAt: now.Add(30 * time.Second),
	}

	scales := []struct {
		name       string
		cronJobs   int
		schedLimit int
		dispLimit  int
	}{
		{"jobs=1", 1, 10, 10},
		{"jobs=10", 10, 20, 20},
		{"jobs=100", 100, 200, 200},
	}

	for _, sc := range scales {
		b.Run(sc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				postgrestest.BenchTruncate(b, db)
				past := now.Add(-5 * time.Minute)
				for i := 0; i < sc.cronJobs; i++ {
					seedTime := past.Add(-time.Duration(i) * time.Second)
					benchSeedCronJob(b, db, fmt.Sprintf("pipe-cron-%d", i), "tenant-pipe", i%10, seedTime)
				}
				b.StartTimer()

				// Phase 1: Schedule
				scheduled, _ := schedUC.RunBatch(context.Background(), now, sc.schedLimit)

				// Phase 2: Dispatch
				dispatched, _ := dispUC.RunBatch(context.Background(), claimSpec, sc.dispLimit)

				b.ReportMetric(float64(scheduled), "scheduled")
				b.ReportMetric(float64(dispatched), "dispatched")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Pipeline: Claim → Complete (worker execution cycle)
// ---------------------------------------------------------------------------

func BenchmarkPipelineClaimComplete(b *testing.B) {
	db := postgrestest.BenchDB(b)

	now := time.Now().UTC().Truncate(time.Second)
	execRepo := NewExecutorRepository(db)

	// Mock handler that succeeds immediately
	mockHandler := &benchExecHandler{}
	handlers := map[string]execute.Handler{"http": mockHandler}
	execUC := execute.NewTickUseCase(execRepo, handlers)

	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		postgrestest.BenchTruncate(b, db)
		jobID := postgrestest.BenchSeedJob(b, db, "pipe-exec", "tenant-pipe", "http", 5)
		benchSeedDispatchedInstance(b, db, "tenant-pipe", jobID, 5, now.Add(-time.Minute))
		b.StartTimer()

		_, _ = execUC.RunOnce(context.Background(), "tenant-pipe", "worker-1", 1, 60*time.Second)
	}
}

// Mock handler for pipeline benchmark
type benchExecHandler struct{}

func (h *benchExecHandler) Execute(_ context.Context, _ execute.AssignedTask) execute.Result {
	return execute.Result{Success: true, ResultCode: "0"}
}

// ---------------------------------------------------------------------------
// Full Pipeline: Schedule → Dispatch → Claim+Complete (throughput)
// ---------------------------------------------------------------------------

func BenchmarkPipelineFull(b *testing.B) {
	db := postgrestest.BenchDB(b)
	postgrestest.BenchTruncate(b, db)

	now := time.Now().UTC().Truncate(time.Second)
	past := now.Add(-5 * time.Minute)
	leaseExpiresAt := now.Add(60 * time.Second)
	claimSpec := domaininstance.ClaimSpec{
		TenantID:       "tenant-full",
		Now:            now,
		LeaseExpiresAt: leaseExpiresAt,
	}

	schedRepo := NewSchedulerRepository(db)
	dispRepo := NewDispatchRepository(db)
	execRepo := NewExecutorRepository(db)

	schedUC := schedule.NewTickUseCase(schedRepo)
	dispUC := dispatch.NewTickUseCase(dispRepo)
	execUC := execute.NewTickUseCase(execRepo, map[string]execute.Handler{
		"http": &benchExecHandler{},
	})

	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		postgrestest.BenchTruncate(b, db)
		for i := 0; i < 20; i++ {
			seedTime := past.Add(-time.Duration(i) * time.Second)
			benchSeedCronJob(b, db, fmt.Sprintf("full-cron-%d", i), "tenant-full", i%10, seedTime)
		}
		b.StartTimer()

		// Phase 1: Schedule
		scheduled, _ := schedUC.RunBatch(context.Background(), now, 50)
		b.ReportMetric(float64(scheduled), "scheduled")

		// Phase 2: Dispatch
		dispatched, _ := dispUC.RunBatch(context.Background(), claimSpec, 50)
		b.ReportMetric(float64(dispatched), "dispatched")

		// Phase 3: Execute
		completed, _ := execUC.RunOnce(context.Background(), "tenant-full", "worker-1", 1, 60*time.Second)
		b.ReportMetric(float64(completed), "completed")
	}
}
