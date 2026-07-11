//go:build integration

package postgres

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"orbitjob/internal/core/app/dispatch"
	"orbitjob/internal/core/app/execute"
	"orbitjob/internal/core/app/schedule"
	"orbitjob/internal/platform/postgrestest"

	domaininstance "orbitjob/internal/core/domain/instance"
	domainjob "orbitjob/internal/core/domain/job"
)

// ═══════════════════════════════════════════════════════════════
// 1. Concurrent CreateJob — 多 goroutine 同时创建 job（写竞争）
// ═══════════════════════════════════════════════════════════════

func BenchmarkConcurrentCreateJob(b *testing.B) {
	db := postgrestest.BenchDB(b)

	scales := []struct {
		name      string
		goroutines int
		opsPerG    int
	}{
		{"g=4_n=250", 4, 250},
		{"g=16_n=250", 16, 250},
		{"g=64_n=100", 64, 100},
	}

	for _, sc := range scales {
		b.Run(sc.name, func(b *testing.B) {
			repo := NewJobRepository(db)
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				b.StopTimer()
				postgrestest.BenchTruncate(b, db)
				benchSeedTenant(b, db, "00000000000000000000000001")
				var created int64
				var wg sync.WaitGroup

				spec := domainjob.CreateSpec{
					Name: "concurrent-create", TenantID: "00000000000000000000000001",
					TriggerType: "manual", HandlerType: "exec",
					TimeoutSec: 60, MisfirePolicy: "skip", ConcurrencyPolicy: "allow",
				}

				b.StartTimer()
				for g := 0; g < sc.goroutines; g++ {
					wg.Add(1)
					go func(gid int) {
						defer wg.Done()
						for i := 0; i < sc.opsPerG; i++ {
							spec.Name = fmt.Sprintf("cc-g%d-n%d", gid, i)
							_, err := repo.Create(context.Background(), spec)
							if err == nil {
								atomic.AddInt64(&created, 1)
							}
						}
					}(g)
				}
				wg.Wait()
				b.StopTimer()
				b.ReportMetric(float64(created), "created")
			}
		})
	}
}

// ═══════════════════════════════════════════════════════════════
// 2. Concurrent Schedule — 多 scheduler 同时扫描 + 调度 cron
// ═══════════════════════════════════════════════════════════════

func BenchmarkConcurrentSchedule(b *testing.B) {
	db := postgrestest.BenchDB(b)

	scales := []struct {
		name       string
		goroutines int
		cronJobs   int
		limit      int
	}{
		{"g=2_jobs=100", 2, 100, 50},
		{"g=4_jobs=500", 4, 500, 100},
		{"g=8_jobs=1000", 8, 1000, 200},
	}

	for _, sc := range scales {
		b.Run(sc.name, func(b *testing.B) {
			now := time.Now().UTC().Truncate(time.Second)
			past := now.Add(-5 * time.Minute)
			schedRepo := NewSchedulerRepository(db)
			schedUC := schedule.NewTickUseCase(schedRepo, ClassifyError)

			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				postgrestest.BenchTruncate(b, db)
				for i := 0; i < sc.cronJobs; i++ {
					benchSeedCronJob(b, db, fmt.Sprintf("sched-cron-%d", i), "00000000000000000000000001", i%10, past.Add(-time.Duration(i)*time.Second))
				}
				var scheduled int64
				var wg sync.WaitGroup

				b.StartTimer()
				for g := 0; g < sc.goroutines; g++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						counts, _ := schedUC.RunBatch(context.Background(), now, sc.limit)
						atomic.AddInt64(&scheduled, int64(counts.Scheduled))
					}()
				}
				wg.Wait()
				b.StopTimer()
				b.ReportMetric(float64(scheduled), "scheduled")
			}
		})
	}
}

// ═══════════════════════════════════════════════════════════════
// 3. Concurrent Dispatch — 多 dispatcher 竞争 SKIP LOCKED
// ═══════════════════════════════════════════════════════════════

func BenchmarkConcurrentDispatch(b *testing.B) {
	db := postgrestest.BenchDB(b)

	scales := []struct {
		name       string
		goroutines int
		instances  int
		limit      int
	}{
		{"g=4_inst=200", 4, 200, 20},
		{"g=16_inst=1000", 16, 1000, 20},
		{"g=32_inst=5000", 32, 5000, 20},
	}

	for _, sc := range scales {
		b.Run(sc.name, func(b *testing.B) {
			now := time.Now().UTC().Truncate(time.Second)
			past := now.Add(-5 * time.Minute)
			leaseExpiresAt := now.Add(30 * time.Second)
			claimSpec := domaininstance.ClaimSpec{
				TenantID: "00000000000000000000000001", Now: now, LeaseExpiresAt: leaseExpiresAt,
			}

			dispRepo := NewDispatchRepository(db)
			dispUC := dispatch.NewTickUseCase(dispRepo)

			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				postgrestest.BenchTruncate(b, db)
				jobID := postgrestest.BenchSeedJob(b, db, "disp-job", "00000000000000000000000001", "exec", 5)
				for i := 0; i < sc.instances; i++ {
					benchSeedPending(b, db, "00000000000000000000000001", jobID, i%10, past.Add(-time.Duration(sc.instances-i)*time.Second))
				}
				var dispatched int64
				var wg sync.WaitGroup

				b.StartTimer()
				for g := 0; g < sc.goroutines; g++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						n, _ := dispUC.RunBatch(context.Background(), claimSpec, sc.limit)
						atomic.AddInt64(&dispatched, int64(n))
					}()
				}
				wg.Wait()
				b.StopTimer()
				b.ReportMetric(float64(dispatched), "dispatched")
			}
		})
	}
}

// ═══════════════════════════════════════════════════════════════
// 4. Concurrent Claim — 多 worker 从 dispatched 队列抢占
// ═══════════════════════════════════════════════════════════════

func BenchmarkConcurrentClaim(b *testing.B) {
	db := postgrestest.BenchDB(b)

	scales := []struct {
		name      string
		workers   int
		instances int
		batchSize int
	}{
		{"workers=10_batch=5", 10, 200, 5},
		{"workers=50_batch=10", 50, 2000, 10},
		{"workers=100_batch=10", 100, 5000, 10},
	}

	for _, sc := range scales {
		b.Run(sc.name, func(b *testing.B) {
			now := time.Now().UTC().Truncate(time.Second)
			past := now.Add(-10 * time.Minute)
			leaseExpiresAt := now.Add(30 * time.Second)
			repo := NewExecutorRepository(db)

			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				postgrestest.BenchTruncate(b, db)
				jobID := postgrestest.BenchSeedJob(b, db, "claim-job", "00000000000000000000000001", "exec", 5)
				for i := 0; i < sc.instances; i++ {
					benchSeedDispatchedInstance(b, db, "00000000000000000000000001", jobID, i%10,
						past.Add(-time.Duration(sc.instances-i)*time.Second))
				}
				var claimed int64
				var wg sync.WaitGroup

				b.StartTimer()
				for w := 0; w < sc.workers; w++ {
					wg.Add(1)
					go func(wid int) {
						defer wg.Done()
						name := fmt.Sprintf("worker-%d", wid)
						tasks, _ := repo.ClaimNextDispatched(context.Background(),
							"00000000000000000000000001", name, sc.batchSize, leaseExpiresAt, now, nil)
						atomic.AddInt64(&claimed, int64(len(tasks)))
					}(w)
				}
				wg.Wait()
				b.StopTimer()
				b.ReportMetric(float64(claimed), "claimed")
			}
		})
	}
}

// ═══════════════════════════════════════════════════════════════
// 5. Concurrent Complete — 多 worker 同时完成 instance
// ═══════════════════════════════════════════════════════════════

func BenchmarkConcurrentComplete(b *testing.B) {
	db := postgrestest.BenchDB(b)

	scales := []struct {
		name      string
		workers   int
		instances int
	}{
		{"workers=10", 10, 100},
		{"workers=50", 50, 500},
		{"workers=100", 100, 1000},
	}

	for _, sc := range scales {
		b.Run(sc.name, func(b *testing.B) {
			now := time.Now().UTC().Truncate(time.Second)
			startedAt := now.Add(-30 * time.Second)
			repo := NewExecutorRepository(db)

			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				postgrestest.BenchTruncate(b, db)
				jobID := postgrestest.BenchSeedJob(b, db, "complete-job", "00000000000000000000000001", "exec", 5)

				// Pre-seed running instances for each worker
				type seeded struct {
					instanceID int64
					workerID   string
				}
				var seeds []seeded
				for w := 0; w < sc.workers; w++ {
					wid := fmt.Sprintf("worker-%d", w)
					for i := 0; i < sc.instances/sc.workers; i++ {
						id := benchSeedRunningInstance(b, db, "00000000000000000000000001", jobID, 5,
							startedAt.Add(-time.Duration(i)*time.Second), wid, startedAt)
						seeds = append(seeds, seeded{id, wid})
					}
				}

				var completed int64
				var wg sync.WaitGroup

				b.StartTimer()
				for _, s := range seeds {
					wg.Add(1)
					go func(s seeded) {
						defer wg.Done()
						err := repo.CompleteInstance(context.Background(),
							benchCompleteSpec("00000000000000000000000001", s.instanceID, s.workerID, now))
						if err == nil {
							atomic.AddInt64(&completed, 1)
						}
					}(s)
				}
				wg.Wait()
				b.StopTimer()
				b.ReportMetric(float64(completed), "completed")
			}
		})
	}
}

// ═══════════════════════════════════════════════════════════════
// 6. Full Pipeline Concurrent — Scheduler + Dispatcher + Worker 并行
// ═══════════════════════════════════════════════════════════════

func BenchmarkPipelineConcurrent(b *testing.B) {
	db := postgrestest.BenchDB(b)

	scales := []struct {
		name       string
		cronJobs   int
		schedulers int
		dispatchers int
		workers    int
	}{
		{"small=20j_2s_2d_2w", 20, 2, 2, 2},
		{"medium=200j_4s_4d_8w", 200, 4, 4, 8},
		{"large=1000j_4s_4d_16w", 1000, 4, 4, 16},
	}

	for _, sc := range scales {
		b.Run(sc.name, func(b *testing.B) {
			now := time.Now().UTC().Truncate(time.Second)
			past := now.Add(-5 * time.Minute)
			leaseExpiresAt := now.Add(60 * time.Second)
			claimSpec := domaininstance.ClaimSpec{
				TenantID: "00000000000000000000000001", Now: now, LeaseExpiresAt: leaseExpiresAt,
			}

			schedRepo := NewSchedulerRepository(db)
			dispRepo := NewDispatchRepository(db)
			execRepo := NewExecutorRepository(db)
			schedUC := schedule.NewTickUseCase(schedRepo, ClassifyError)
			dispUC := dispatch.NewTickUseCase(dispRepo)
			execUC := execute.NewTickUseCase(execRepo, map[string]execute.Handler{
				"exec": &benchExecHandler{},
			})

			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				postgrestest.BenchTruncate(b, db)
				for i := 0; i < sc.cronJobs; i++ {
					benchSeedCronJob(b, db, fmt.Sprintf("mix-cron-%d", i), "00000000000000000000000001", i%10,
						past.Add(-time.Duration(i)*time.Second))
				}
				var schedCount, dispCount, compCount int64
				var wg sync.WaitGroup

				b.StartTimer()

				// Phase 1: concurrent schedulers
				for s := 0; s < sc.schedulers; s++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						counts, _ := schedUC.RunBatch(context.Background(), now, 100)
						atomic.AddInt64(&schedCount, int64(counts.Scheduled))
					}()
				}
				wg.Wait()

				// Phase 2: concurrent dispatchers
				for d := 0; d < sc.dispatchers; d++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						n, _ := dispUC.RunBatch(context.Background(), claimSpec, 50)
						atomic.AddInt64(&dispCount, int64(n))
					}()
				}
				wg.Wait()

				// Phase 3: concurrent workers claiming + completing
				for w := 0; w < sc.workers; w++ {
					wg.Add(1)
					go func(wid int) {
						defer wg.Done()
						name := fmt.Sprintf("worker-%d", wid)
						n, _ := execUC.RunOnce(context.Background(), "00000000000000000000000001", name, 3, 60*time.Second, nil)
						atomic.AddInt64(&compCount, int64(n))
					}(w)
				}
				wg.Wait()

				b.StopTimer()
				b.ReportMetric(float64(schedCount), "scheduled")
				b.ReportMetric(float64(dispCount), "dispatched")
				b.ReportMetric(float64(compCount), "completed")
			}
		})
	}
}

// ═══════════════════════════════════════════════════════════════
// 7. Stress — 持续并发创建 + 调度 + 分发
// ═══════════════════════════════════════════════════════════════

func BenchmarkStressMixed(b *testing.B) {
	if testing.Short() {
		b.Skip("stress test skipped in short mode")
	}
	db := postgrestest.BenchDB(b)
	postgrestest.BenchTruncate(b, db)

	now := time.Now().UTC().Truncate(time.Second)
	past := now.Add(-2 * time.Minute)
	leaseExpiresAt := now.Add(60 * time.Second)
	claimSpec := domaininstance.ClaimSpec{
		TenantID: "00000000000000000000000001", Now: now, LeaseExpiresAt: leaseExpiresAt,
	}

	jobRepo := NewJobRepository(db)
	schedRepo := NewSchedulerRepository(db)
	dispRepo := NewDispatchRepository(db)
	execRepo := NewExecutorRepository(db)
	schedUC := schedule.NewTickUseCase(schedRepo, ClassifyError)
	dispUC := dispatch.NewTickUseCase(dispRepo)
	execUC := execute.NewTickUseCase(execRepo, map[string]execute.Handler{
		"exec": &benchExecHandler{},
	})

	// Pre-seed 500 cron jobs so there's always work
	for i := 0; i < 500; i++ {
		benchSeedCronJob(b, db, fmt.Sprintf("stress-cron-%d", i), "00000000000000000000000001", i%10,
			past.Add(-time.Duration(i)*time.Minute))
	}

	var created, schedCount, dispCount, compCount int64
	stopCh := make(chan struct{})

	// Background creators
	go func() {
		spec := domainjob.CreateSpec{
			Name: "stress-create", TenantID: "00000000000000000000000001",
			TriggerType: "manual", HandlerType: "exec",
			TimeoutSec: 60, MisfirePolicy: "skip", ConcurrencyPolicy: "allow",
		}
		for i := 0; ; i++ {
			select {
			case <-stopCh:
				return
			default:
				spec.Name = fmt.Sprintf("stress-create-%d", i)
				if _, err := jobRepo.Create(context.Background(), spec); err == nil {
					atomic.AddInt64(&created, 1)
				}
			}
		}
	}()

	b.ReportAllocs()
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup

		// Schedule
		for s := 0; s < 2; s++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				counts, _ := schedUC.RunBatch(context.Background(), now, 50)
				atomic.AddInt64(&schedCount, int64(counts.Scheduled))
			}()
		}

		// Dispatch
		for d := 0; d < 4; d++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				n, _ := dispUC.RunBatch(context.Background(), claimSpec, 20)
				atomic.AddInt64(&dispCount, int64(n))
			}()
		}

		// Execute
		for w := 0; w < 8; w++ {
			wg.Add(1)
			go func(wid int) {
				defer wg.Done()
				name := fmt.Sprintf("stress-%d", wid)
				n, _ := execUC.RunOnce(context.Background(), "00000000000000000000000001", name, 1, 60*time.Second, nil)
				atomic.AddInt64(&compCount, int64(n))
			}(w)
		}

		wg.Wait()
	}
	b.StopTimer()
	close(stopCh)

	b.ReportMetric(float64(created), "created")
	b.ReportMetric(float64(schedCount), "scheduled")
	b.ReportMetric(float64(dispCount), "dispatched")
	b.ReportMetric(float64(compCount), "completed")
}
