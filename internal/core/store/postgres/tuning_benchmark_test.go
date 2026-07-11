//go:build integration

package postgres

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"orbitjob/internal/platform/postgrestest"

	domaininstance "orbitjob/internal/core/domain/instance"
)

// ═══════════════════════════════════════════════════════════════
// Tuning 1: Connection Pool Size
// Hypothesis: 25 max conns is the bottleneck at 32+ goroutines
// Test: vary MaxOpenConns from 25 → 50 → 100
// ═══════════════════════════════════════════════════════════════

func BenchmarkTuning_PoolSize(b *testing.B) {
	db := postgrestest.BenchDB(b)

	poolSizes := []int{25, 50, 100}
	workers := 50
	instances := 2000
	batchSize := 10

	now := time.Now().UTC().Truncate(time.Second)
	past := now.Add(-10 * time.Minute)
	leaseExpiresAt := now.Add(30 * time.Second)
	repo := NewExecutorRepository(db)

	for _, poolSize := range poolSizes {
		b.Run(fmt.Sprintf("pool=%d", poolSize), func(b *testing.B) {
			db.SetMaxOpenConns(poolSize)
			db.SetMaxIdleConns(poolSize / 2)

			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				postgrestest.BenchTruncate(b, db)
				jobID := postgrestest.BenchSeedJob(b, db, "pool-job", "00000000000000000000000001", "exec", 5)
				for i := 0; i < instances; i++ {
					benchSeedDispatchedInstance(b, db, "00000000000000000000000001", jobID, i%10,
						past.Add(-time.Duration(instances-i)*time.Second))
				}
				var claimed int64
				var wg sync.WaitGroup

				b.StartTimer()
				for w := 0; w < workers; w++ {
					wg.Add(1)
					go func(wid int) {
						defer wg.Done()
						name := fmt.Sprintf("pool-w%d", wid)
						tasks, _ := repo.ClaimNextDispatched(context.Background(),
							"00000000000000000000000001", name, batchSize, leaseExpiresAt, now, nil)
						atomic.AddInt64(&claimed, int64(len(tasks)))
					}(w)
				}
				wg.Wait()
				b.StopTimer()
				b.ReportMetric(float64(claimed), "claimed")
			}
		})
	}
	// Restore default
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
}

// ═══════════════════════════════════════════════════════════════
// Tuning 2: Claim Batch Size
// Hypothesis: batch=5 causes too many round-trips
// Test: vary batch from 5 → 20 → 50
// ═══════════════════════════════════════════════════════════════

func BenchmarkTuning_ClaimBatch(b *testing.B) {
	db := postgrestest.BenchDB(b)
	// Use larger pool to isolate batch size effect
	db.SetMaxOpenConns(50)
	db.SetMaxIdleConns(25)

	batchSizes := []int{5, 10, 20, 50}
	workers := 50
	instances := 2000

	now := time.Now().UTC().Truncate(time.Second)
	past := now.Add(-10 * time.Minute)
	leaseExpiresAt := now.Add(30 * time.Second)
	repo := NewExecutorRepository(db)

	for _, batchSize := range batchSizes {
		b.Run(fmt.Sprintf("batch=%d", batchSize), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				postgrestest.BenchTruncate(b, db)
				jobID := postgrestest.BenchSeedJob(b, db, "batch-job", "00000000000000000000000001", "exec", 5)
				for i := 0; i < instances; i++ {
					benchSeedDispatchedInstance(b, db, "00000000000000000000000001", jobID, i%10,
						past.Add(-time.Duration(instances-i)*time.Second))
				}
				var claimed int64
				var wg sync.WaitGroup

				b.StartTimer()
				for w := 0; w < workers; w++ {
					wg.Add(1)
					go func(wid int) {
						defer wg.Done()
						name := fmt.Sprintf("batch-w%d", wid)
						tasks, _ := repo.ClaimNextDispatched(context.Background(),
							"00000000000000000000000001", name, batchSize, leaseExpiresAt, now, nil)
						atomic.AddInt64(&claimed, int64(len(tasks)))
					}(w)
				}
				wg.Wait()
				b.StopTimer()
				b.ReportMetric(float64(claimed), "claimed")
			}
		})
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
}

// ═══════════════════════════════════════════════════════════════
// Tuning 3: Dispatch Batch Size (FOR UPDATE SKIP LOCKED LIMIT)
// Hypothesis: larger LIMIT reduces contention
// Test: vary dispatch batch from 10 → 50 → 200
// ═══════════════════════════════════════════════════════════════

func BenchmarkTuning_DispatchBatch(b *testing.B) {
	db := postgrestest.BenchDB(b)
	db.SetMaxOpenConns(50)
	db.SetMaxIdleConns(25)

	batchLimits := []int{10, 50, 200}
	goroutines := 32
	instances := 5000

	now := time.Now().UTC().Truncate(time.Second)
	past := now.Add(-5 * time.Minute)
	leaseExpiresAt := now.Add(30 * time.Second)
	claimSpec := domaininstance.ClaimSpec{
		TenantID: "00000000000000000000000001", Now: now, LeaseExpiresAt: leaseExpiresAt,
	}
	dispRepo := NewDispatchRepository(db)

	for _, limit := range batchLimits {
		b.Run(fmt.Sprintf("limit=%d", limit), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				postgrestest.BenchTruncate(b, db)
				benchSeedTenant(b, db, "00000000000000000000000001")
				jobID := postgrestest.BenchSeedJob(b, db, "disp-tune", "00000000000000000000000001", "exec", 5)
				for i := 0; i < instances; i++ {
					benchSeedPending(b, db, "00000000000000000000000001", jobID, i%10,
						past.Add(-time.Duration(instances-i)*time.Second))
				}
				var dispatched int64
				var wg sync.WaitGroup

				b.StartTimer()
				for g := 0; g < goroutines; g++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						_, found, _ := dispRepo.DispatchOne(context.Background(), claimSpec, domaininstance.DecideDispatch)
						if found {
							atomic.AddInt64(&dispatched, 1)
						}
					}()
				}
				wg.Wait()
				b.StopTimer()
				b.ReportMetric(float64(dispatched), "dispatched")
			}
		})
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
}

// ═══════════════════════════════════════════════════════════════
// Tuning 4: Pre-claimed connection (warm pool)
// Hypothesis: connection establishment overhead is significant
// Test: pre-warm connections vs cold start
// ═══════════════════════════════════════════════════════════════

func BenchmarkTuning_WarmPool(b *testing.B) {
	db := postgrestest.BenchDB(b)

	tests := []struct {
		name     string
		warmPool bool
		poolSize int
	}{
		{"cold_pool=25", false, 25},
		{"warm_pool=25", true, 25},
		{"cold_pool=50", false, 50},
		{"warm_pool=50", true, 50},
	}

	now := time.Now().UTC().Truncate(time.Second)
	past := now.Add(-10 * time.Minute)
	leaseExpiresAt := now.Add(30 * time.Second)
	repo := NewExecutorRepository(db)
	workers := 20
	instances := 500
	batchSize := 10

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			db.SetMaxOpenConns(tt.poolSize)
			db.SetMaxIdleConns(tt.poolSize)

			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				postgrestest.BenchTruncate(b, db)
				jobID := postgrestest.BenchSeedJob(b, db, "warm-job", "00000000000000000000000001", "exec", 5)
				for i := 0; i < instances; i++ {
					benchSeedDispatchedInstance(b, db, "00000000000000000000000001", jobID, i%10,
						past.Add(-time.Duration(instances-i)*time.Second))
				}

				if tt.warmPool {
					// Pre-warm: open connections by sending pings
					var warmWg sync.WaitGroup
					for w := 0; w < tt.poolSize; w++ {
						warmWg.Add(1)
						go func() {
							defer warmWg.Done()
							db.ExecContext(context.Background(), "SELECT 1")
						}()
					}
					warmWg.Wait()
				}

				var claimed int64
				var wg sync.WaitGroup

				b.StartTimer()
				for w := 0; w < workers; w++ {
					wg.Add(1)
					go func(wid int) {
						defer wg.Done()
						name := fmt.Sprintf("warm-w%d", wid)
						tasks, _ := repo.ClaimNextDispatched(context.Background(),
							"00000000000000000000000001", name, batchSize, leaseExpiresAt, now, nil)
						atomic.AddInt64(&claimed, int64(len(tasks)))
					}(w)
				}
				wg.Wait()
				b.StopTimer()
				b.ReportMetric(float64(claimed), "claimed")
			}
		})
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
}

// ═══════════════════════════════════════════════════════════════
// Tuning 5: Idle connection retention
// Hypothesis: MaxIdleConns=5 causes frequent connection churn
// Test: MaxIdleConns = MaxOpenConns/2 vs MaxOpenConns
// ═══════════════════════════════════════════════════════════════

func BenchmarkTuning_IdleConns(b *testing.B) {
	db := postgrestest.BenchDB(b)

	tests := []struct {
		name        string
		maxOpen     int
		maxIdle     int
	}{
		{"open=50_idle=5", 50, 5},
		{"open=50_idle=25", 50, 25},
		{"open=50_idle=50", 50, 50},
	}

	now := time.Now().UTC().Truncate(time.Second)
	past := now.Add(-10 * time.Minute)
	leaseExpiresAt := now.Add(30 * time.Second)
	repo := NewExecutorRepository(db)
	workers := 50
	instances := 2000
	batchSize := 10

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			db.SetMaxOpenConns(tt.maxOpen)
			db.SetMaxIdleConns(tt.maxIdle)

			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				postgrestest.BenchTruncate(b, db)
				jobID := postgrestest.BenchSeedJob(b, db, "idle-job", "00000000000000000000000001", "exec", 5)
				for i := 0; i < instances; i++ {
					benchSeedDispatchedInstance(b, db, "00000000000000000000000001", jobID, i%10,
						past.Add(-time.Duration(instances-i)*time.Second))
				}
				var claimed int64
				var wg sync.WaitGroup

				b.StartTimer()
				for w := 0; w < workers; w++ {
					wg.Add(1)
					go func(wid int) {
						defer wg.Done()
						name := fmt.Sprintf("idle-w%d", wid)
						tasks, _ := repo.ClaimNextDispatched(context.Background(),
							"00000000000000000000000001", name, batchSize, leaseExpiresAt, now, nil)
						atomic.AddInt64(&claimed, int64(len(tasks)))
					}(w)
				}
				wg.Wait()
				b.StopTimer()
				b.ReportMetric(float64(claimed), "claimed")
			}
		})
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
}
