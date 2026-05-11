//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"orbitjob/internal/core/app/schedule"
	"orbitjob/internal/platform/postgrestest"

	domaininstance "orbitjob/internal/core/domain/instance"
	domainjob "orbitjob/internal/core/domain/job"

	_ "github.com/lib/pq"
)

// ---------------------------------------------------------------------------
// helpers (shared helpers in postgrestest.BenchDB / BenchTruncate / BenchSeedJob)
// ---------------------------------------------------------------------------

func benchSeedCronJob(b *testing.B, db *sql.DB, name, tenantID string, priority int, nextRunAt time.Time) int64 {
	b.Helper()
	var id int64
	err := db.QueryRowContext(context.Background(), `
		INSERT INTO jobs (name, tenant_id, trigger_type, cron_expr, timezone, handler_type, handler_payload, timeout_sec, status, priority, next_run_at, misfire_policy)
		VALUES ($1, $2, 'cron', '*/5 * * * *', 'UTC', 'http', '{}'::jsonb, 60, 'active', $3, $4, 'fire_now')
		RETURNING id
	`, name, tenantID, priority, nextRunAt).Scan(&id)
	if err != nil {
		b.Fatalf("seed cron job: %v", err)
	}
	return id
}

func benchSeedDispatchedInstance(b *testing.B, db *sql.DB, tenantID string, jobID int64, priority int, scheduledAt time.Time) int64 {
	b.Helper()
	var id int64
	err := db.QueryRowContext(context.Background(), `
		INSERT INTO job_instances (tenant_id, job_id, status, priority, effective_priority, scheduled_at, dispatched_at, attempt, max_attempt)
		VALUES ($1, $2, 'dispatched', $3, $3, $4, $4, 1, 1)
		RETURNING id
	`, tenantID, jobID, priority, scheduledAt).Scan(&id)
	if err != nil {
		b.Fatalf("seed dispatched instance: %v", err)
	}
	return id
}

func benchSeedRoutedDispatchedInstance(b *testing.B, db *sql.DB, tenantID string, jobID int64, priority int, scheduledAt time.Time, routingKey string) int64 {
	b.Helper()
	var id int64
	err := db.QueryRowContext(context.Background(), `
		INSERT INTO job_instances (tenant_id, job_id, status, priority, effective_priority, scheduled_at, dispatched_at, routing_key, attempt, max_attempt)
		VALUES ($1, $2, 'dispatched', $3, $3, $4, $4, $5, 1, 1)
		RETURNING id
	`, tenantID, jobID, priority, scheduledAt, routingKey).Scan(&id)
	if err != nil {
		b.Fatalf("seed routed dispatched instance: %v", err)
	}
	return id
}

func benchSeedRunningInstance(b *testing.B, db *sql.DB, tenantID string, jobID int64, priority int, scheduledAt time.Time, workerID string, startedAt time.Time) int64 {
	b.Helper()
	var id int64
	err := db.QueryRowContext(context.Background(), `
		INSERT INTO job_instances (tenant_id, job_id, status, priority, effective_priority, scheduled_at, worker_id, started_at, attempt, max_attempt)
		VALUES ($1, $2, 'running', $3, $3, $4, $5, $6, 1, 1)
		RETURNING id
	`, tenantID, jobID, priority, scheduledAt, workerID, startedAt).Scan(&id)
	if err != nil {
		b.Fatalf("seed running instance: %v", err)
	}
	return id
}

func benchSeedWorker(b *testing.B, db *sql.DB, tenantID, workerID, status string, leaseExpiresAt time.Time) {
	b.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO workers (worker_id, tenant_id, status, last_heartbeat_at, lease_expires_at, capacity, labels)
		VALUES ($1, $2, $3, $4, $5, 1, '{}'::jsonb)
	`, workerID, tenantID, status, leaseExpiresAt.Add(-time.Minute), leaseExpiresAt)
	if err != nil {
		b.Fatalf("seed worker: %v", err)
	}
}

func benchSeedTenant(b *testing.B, db *sql.DB, tenantID string) {
	b.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO tenants (id, name, status)
		VALUES ($1, $1, 'active')
		ON CONFLICT (id) DO NOTHING
	`, tenantID)
	if err != nil {
		b.Fatalf("seed tenant: %v", err)
	}
}

func benchCompleteSpec(tenantID string, instanceID int64, workerID string, now time.Time) domaininstance.CompleteSpec {
	resultCode := "0"
	return domaininstance.CompleteSpec{
		TenantID:   tenantID,
		InstanceID: instanceID,
		WorkerID:   workerID,
		Status:     domaininstance.StatusSuccess,
		Attempt:    1,
		ResultCode: &resultCode,
		FinishedAt: now,
	}
}

func benchSeedPending(b *testing.B, db *sql.DB, tenantID string, jobID int64, priority int, scheduledAt time.Time) int64 {
	b.Helper()
	var id int64
	err := db.QueryRowContext(context.Background(), `
		INSERT INTO job_instances (tenant_id, job_id, status, priority, effective_priority, scheduled_at, attempt, max_attempt)
		VALUES ($1, $2, 'pending', $3, $3, $4, 1, 1)
		RETURNING id
	`, tenantID, jobID, priority, scheduledAt).Scan(&id)
	if err != nil {
		b.Fatalf("seed pending: %v", err)
	}
	return id
}

func benchSeedOrphanDispatched(b *testing.B, db *sql.DB, tenantID string, jobID int64, dispatchedAt, leaseExpiresAt time.Time) int64 {
	b.Helper()
	var id int64
	err := db.QueryRowContext(context.Background(), `
		INSERT INTO job_instances (tenant_id, job_id, status, priority, effective_priority, scheduled_at, dispatched_at, lease_expires_at, attempt, max_attempt)
		VALUES ($1, $2, 'dispatched', 5, 5, $3, $4, $5, 1, 1)
		RETURNING id
	`, tenantID, jobID, dispatchedAt.Add(-time.Minute), dispatchedAt, leaseExpiresAt).Scan(&id)
	if err != nil {
		b.Fatalf("seed orphan dispatched: %v", err)
	}
	return id
}

func benchSeedOrphanRunning(b *testing.B, db *sql.DB, tenantID string, jobID int64, workerID string, startedAt, leaseExpiresAt time.Time) int64 {
	b.Helper()
	var id int64
	err := db.QueryRowContext(context.Background(), `
		INSERT INTO job_instances (tenant_id, job_id, status, priority, effective_priority, scheduled_at, worker_id, started_at, lease_expires_at, attempt, max_attempt)
		VALUES ($1, $2, 'running', 5, 5, $3, $4, $5, $6, 1, 3)
		RETURNING id
	`, tenantID, jobID, startedAt.Add(-time.Minute), workerID, startedAt, leaseExpiresAt).Scan(&id)
	if err != nil {
		b.Fatalf("seed orphan running: %v", err)
	}
	return id
}

// ---------------------------------------------------------------------------
// ClaimNextDispatched — single-operation latency
// ---------------------------------------------------------------------------

func BenchmarkClaimNextDispatched_RoutingKey(b *testing.B) {
	db := postgrestest.BenchDB(b)
	postgrestest.BenchTruncate(b, db)

	now := time.Now().UTC().Truncate(time.Second)
	jobID := postgrestest.BenchSeedJob(b, db, "claim-route-bench", "tenant-claim", "http", 5)
	leaseExpiresAt := now.Add(30 * time.Second)
	repo := NewExecutorRepository(db)
	labels := map[string]any{"queue": "video"}

	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		postgrestest.BenchTruncate(b, db)
		jobID = postgrestest.BenchSeedJob(b, db, "claim-route-bench", "tenant-claim", "http", 5)
		benchSeedRoutedDispatchedInstance(b, db, "tenant-claim", jobID, 5, now.Add(-time.Minute), "video")
		benchSeedRoutedDispatchedInstance(b, db, "tenant-claim", jobID, 4, now.Add(-2*time.Minute), "batch")
		b.StartTimer()

		_, _ = repo.ClaimNextDispatched(context.Background(), "tenant-claim", "worker-1", 1, leaseExpiresAt, now, labels)
	}
}


// ---------------------------------------------------------------------------
// ClaimNextDispatched — concurrent (SKIP LOCKED contention)
// ---------------------------------------------------------------------------

func BenchmarkClaimNextDispatched_Concurrent(b *testing.B) {
	db := postgrestest.BenchDB(b)
	postgrestest.BenchTruncate(b, db)

	now := time.Now().UTC().Truncate(time.Second)
	leaseExpiresAt := now.Add(30 * time.Second)

	sizes := []struct {
		name      string
		instances int
		workers   int
	}{
		{"instances=100_workers=10", 100, 10},
		{"instances=1000_workers=50", 1000, 50},
	}

	for _, sz := range sizes {
		b.Run(sz.name, func(b *testing.B) {
			b.StopTimer()
			postgrestest.BenchTruncate(b, db)
			jobID := postgrestest.BenchSeedJob(b, db, "claim-conc", "tenant-conc", "http", 5)
			for i := 0; i < sz.instances; i++ {
				benchSeedDispatchedInstance(b, db, "tenant-conc", jobID, i%10, now.Add(-time.Duration(sz.instances-i)*time.Minute))
			}
			repo := NewExecutorRepository(db)
			var total int64

			b.ReportAllocs()
			b.StartTimer()
			// SetParallelism multiplies GOMAXPROCS — compensate to get target worker count.
				if sz.workers < runtime.GOMAXPROCS(0) {
					b.SetParallelism(1)
				} else {
					b.SetParallelism(sz.workers / runtime.GOMAXPROCS(0))
				}
			b.RunParallel(func(pb *testing.PB) {
				workerID := fmt.Sprintf("worker-%d", atomic.AddInt64(&total, 1)%int64(sz.workers))
				for pb.Next() {
					tasks, _ := repo.ClaimNextDispatched(context.Background(), "tenant-conc", workerID, 10, leaseExpiresAt, now, nil)
					atomic.AddInt64(&total, int64(len(tasks)))
				}
			})
			b.StopTimer()
			b.ReportMetric(float64(total), "claims")
		})
	}
}

// ---------------------------------------------------------------------------
// CreateJob — single-operation latency
// ---------------------------------------------------------------------------

func BenchmarkCreateJob(b *testing.B) {
	db := postgrestest.BenchDB(b)

	tests := []struct {
		name string
		spec domainjob.CreateSpec
	}{
		{"minimal", domainjob.CreateSpec{
			Name: "bench-job", TenantID: "default", TriggerType: "manual", HandlerType: "http",
			TimeoutSec: 60, MisfirePolicy: "skip", ConcurrencyPolicy: "allow",
		}},
		{"full", domainjob.CreateSpec{
			Name: "bench-job", TenantID: "tenant-a", Priority: 5, TriggerType: "manual",
			HandlerType: "http", TimeoutSec: 120, RetryLimit: 3, RetryBackoffSec: 10,
			RetryBackoffStrategy: "exponential", ConcurrencyPolicy: "forbid",
			MisfirePolicy: "skip",
			HandlerPayload: map[string]any{"url": "https://example.com/hook", "method": "POST"},
		}},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			repo := NewJobRepository(db)
			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				postgrestest.BenchTruncate(b, db)
				b.StartTimer()

				_, _ = repo.Create(context.Background(), tt.spec)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// RefreshEffectivePriority — bulk UPDATE latency at scale
// ---------------------------------------------------------------------------

func BenchmarkRefreshEffectivePriority(b *testing.B) {
	db := postgrestest.BenchDB(b)

	scales := []struct {
		name    string
		pending int
	}{
		{"pending=10", 10},
		{"pending=100", 100},
		{"pending=1000", 1000},
	}

	for _, sc := range scales {
		b.Run(sc.name, func(b *testing.B) {
			b.StopTimer()
			postgrestest.BenchTruncate(b, db)
			jobID := postgrestest.BenchSeedJob(b, db, "prio-bench", "tenant-prio", "http", 5)
			now := time.Now().UTC().Truncate(time.Second)
			for i := 0; i < sc.pending; i++ {
				benchSeedPending(b, db, "tenant-prio", jobID, i%10, now.Add(-time.Duration(sc.pending-i)*time.Minute))
			}
			repo := NewDispatchRepository(db)
			b.StartTimer()

			b.ReportAllocs()
			for b.Loop() {
				_, _ = repo.RefreshEffectivePriority(context.Background(), now)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// RecoverLeaseOrphans — at scale
// ---------------------------------------------------------------------------

func BenchmarkRecoverLeaseOrphans(b *testing.B) {
	db := postgrestest.BenchDB(b)

	scales := []struct {
		name           string
		orphanDispatch int
		orphanRunning  int
	}{
		{"orphans=0", 0, 0},
		{"orphans=10d_5r", 10, 5},
		{"orphans=100d_50r", 100, 50},
	}

	for _, sc := range scales {
		b.Run(sc.name, func(b *testing.B) {
			repo := NewDispatchRepository(db)
			now := time.Now().UTC().Truncate(time.Second)
			past := now.Add(-10 * time.Minute)

			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				postgrestest.BenchTruncate(b, db)
				jobID := postgrestest.BenchSeedJob(b, db, "orphan-bench", "tenant-orphan", "http", 5)
				for i := 0; i < sc.orphanDispatch; i++ {
					seedTime := past.Add(-time.Duration(i) * time.Second)
					benchSeedOrphanDispatched(b, db, "tenant-orphan", jobID, seedTime, past.Add(5*time.Minute))
				}
				for i := 0; i < sc.orphanRunning; i++ {
					seedTime := past.Add(-time.Duration(i+sc.orphanDispatch) * time.Second)
					benchSeedOrphanRunning(b, db, "tenant-orphan", jobID, fmt.Sprintf("worker-%d", i), seedTime, past.Add(5*time.Minute))
				}
				b.StartTimer()

				_, _, _ = repo.RecoverLeaseOrphans(context.Background(), now)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ScheduleOneDueCron — transactional schedule
// ---------------------------------------------------------------------------

func BenchmarkScheduleOneDueCron(b *testing.B) {
	db := postgrestest.BenchDB(b)

	now := time.Now().UTC().Truncate(time.Second)
	past := now.Add(-2 * time.Minute)
	repo := NewSchedulerRepository(db)

	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		postgrestest.BenchTruncate(b, db)
		benchSeedCronJob(b, db, "cron-bench", "tenant-cron", 5, past)
		b.StartTimer()

		_, _, _ = repo.ScheduleOneDueCron(context.Background(), now, schedule.DecideSchedule)
	}
}

// ---------------------------------------------------------------------------
// DispatchOne — full dispatch TX
// ---------------------------------------------------------------------------

func BenchmarkDispatchOne(b *testing.B) {
	db := postgrestest.BenchDB(b)

	now := time.Now().UTC().Truncate(time.Second)
	claimSpec := domaininstance.ClaimSpec{
		TenantID:       "tenant-disp",
		Now:            now,
		LeaseExpiresAt: now.Add(30 * time.Second),
	}

	tests := []struct {
		name             string
		concurrencyPolicy string
	}{
		{"allow_policy", "allow"},
		{"forbid_policy", "forbid"},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			repo := NewDispatchRepository(db)
			b.ReportAllocs()

			for b.Loop() {
				b.StopTimer()
				postgrestest.BenchTruncate(b, db)
				jobID := postgrestest.BenchSeedJob(b, db, "disp-bench", "tenant-disp", "http", 5)
				benchSeedPending(b, db, "tenant-disp", jobID, 5, now.Add(-time.Minute))
				b.StartTimer()

				_, _, _ = repo.DispatchOne(context.Background(), claimSpec, domaininstance.DecideDispatch)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// CreateInstance — INSERT + audit
// ---------------------------------------------------------------------------

func BenchmarkCompleteInstance(b *testing.B) {
	db := postgrestest.BenchDB(b)
	repo := NewExecutorRepository(db)
	now := time.Now().UTC().Truncate(time.Second)

	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		postgrestest.BenchTruncate(b, db)
		jobID := postgrestest.BenchSeedJob(b, db, "complete-bench", "tenant-complete", "exec", 5)
		instanceID := benchSeedRunningInstance(b, db, "tenant-complete", jobID, 5, now.Add(-time.Minute), "worker-1", now.Add(-30*time.Second))
		b.StartTimer()

		_ = repo.CompleteInstance(context.Background(), benchCompleteSpec("tenant-complete", instanceID, "worker-1", now))
	}
}

func BenchmarkExtendLease(b *testing.B) {
	db := postgrestest.BenchDB(b)
	repo := NewExecutorRepository(db)
	now := time.Now().UTC().Truncate(time.Second)
	newExpiry := now.Add(90 * time.Second)

	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		postgrestest.BenchTruncate(b, db)
		jobID := postgrestest.BenchSeedJob(b, db, "lease-bench", "tenant-lease", "exec", 5)
		instanceID := benchSeedRunningInstance(b, db, "tenant-lease", jobID, 5, now.Add(-time.Minute), "worker-1", now.Add(-30*time.Second))
		b.StartTimer()

		_ = repo.ExtendLease(context.Background(), "tenant-lease", instanceID, "worker-1", newExpiry)
	}
}

func BenchmarkRecoverExpiredWorkers(b *testing.B) {
	db := postgrestest.BenchDB(b)
	repo := NewDispatchRepository(db)
	now := time.Now().UTC().Truncate(time.Second)
	expired := now.Add(-time.Second)

	scales := []struct {
		name    string
		online  int
		draining int
	}{
		{"workers=10", 10, 0},
		{"workers=100", 80, 20},
	}

	for _, sc := range scales {
		b.Run(sc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				postgrestest.BenchTruncate(b, db)
				for i := 0; i < sc.online; i++ {
					benchSeedWorker(b, db, "tenant-workers", fmt.Sprintf("online-%d", i), "online", expired)
				}
				for i := 0; i < sc.draining; i++ {
					benchSeedWorker(b, db, "tenant-workers", fmt.Sprintf("draining-%d", i), "draining", expired)
				}
				b.StartTimer()

				_, _ = repo.RecoverExpiredWorkers(context.Background(), now)
			}
		})
	}
}

func BenchmarkListActiveTenantIDs(b *testing.B) {
	db := postgrestest.BenchDB(b)
	repo := NewDispatchRepository(db)

	scales := []struct {
		name   string
		tenants int
	}{
		{"tenants=10", 10},
		{"tenants=100", 100},
	}

	for _, sc := range scales {
		b.Run(sc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				postgrestest.BenchTruncate(b, db)
				_, err := db.ExecContext(context.Background(), `TRUNCATE TABLE tenants RESTART IDENTITY CASCADE`)
				if err != nil {
					b.Fatalf("truncate tenants: %v", err)
				}
				for i := 0; i < sc.tenants; i++ {
					benchSeedTenant(b, db, fmt.Sprintf("tenant-%03d", i))
				}
				b.StartTimer()

				_, _ = repo.ListActiveTenantIDs(context.Background())
			}
		})
	}
}
