//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	query "orbitjob/internal/admin/app/job/query"

	_ "github.com/lib/pq"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func adminBenchDB(b *testing.B) *sql.DB {
	b.Helper()

	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		b.Skip("TEST_DATABASE_DSN is not set")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		b.Fatalf("open db: %v", err)
	}
	b.Cleanup(func() { _ = db.Close() })

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		b.Fatalf("ping db: %v", err)
	}

	return db
}

func adminTruncate(b *testing.B, db *sql.DB) {
	b.Helper()
	_, err := db.ExecContext(context.Background(), `
		TRUNCATE TABLE audit_events, job_instance_attempts, job_instances, workers, jobs RESTART IDENTITY CASCADE
	`)
	if err != nil {
		b.Fatalf("truncate: %v", err)
	}
}

func adminSeedJob(b *testing.B, db *sql.DB, name, tenantID string, priority int) int64 {
	b.Helper()
	var id int64
	err := db.QueryRowContext(context.Background(), `
		INSERT INTO jobs (name, tenant_id, trigger_type, handler_type, handler_payload, timeout_sec, status, priority)
		VALUES ($1, $2, 'manual', 'http', '{}'::jsonb, 60, 'active', $3)
		RETURNING id
	`, name, tenantID, priority).Scan(&id)
	if err != nil {
		b.Fatalf("seed job: %v", err)
	}
	return id
}

func adminSeedJobWithPayload(b *testing.B, db *sql.DB, name, tenantID string, payload string) int64 {
	b.Helper()
	var id int64
	err := db.QueryRowContext(context.Background(), `
		INSERT INTO jobs (name, tenant_id, trigger_type, handler_type, handler_payload, timeout_sec, status, priority)
		VALUES ($1, $2, 'manual', 'http', $3::jsonb, 60, 'active', 5)
		RETURNING id
	`, name, tenantID, payload).Scan(&id)
	if err != nil {
		b.Fatalf("seed job with payload: %v", err)
	}
	return id
}

// ---------------------------------------------------------------------------
// List — paginated read with offset
// ---------------------------------------------------------------------------

func BenchmarkListJobs(b *testing.B) {
	db := adminBenchDB(b)

	scales := []struct {
		name  string
		rows  int
		limit int
		offset int
	}{
		{"rows=100_limit=10_offset=0", 100, 10, 0},
		{"rows=100_limit=10_offset=50", 100, 10, 50},
		{"rows=1000_limit=50_offset=0", 1000, 50, 0},
		{"rows=1000_limit=50_offset=500", 1000, 50, 500},
		{"rows=1000_limit=50_offset=950", 1000, 50, 950},
	}

	for _, sc := range scales {
		b.Run(sc.name, func(b *testing.B) {
			b.StopTimer()
			adminTruncate(b, db)
			for i := 0; i < sc.rows; i++ {
				adminSeedJob(b, db, fmt.Sprintf("job-%d", i), "tenant-list", i%10)
			}
			repo := NewJobRepository(db)
			b.StartTimer()

			b.ReportAllocs()
			for b.Loop() {
				_, _ = repo.List(context.Background(), query.ListInput{
					TenantID: "tenant-list",
					Limit:    sc.limit,
					Offset:   sc.offset,
				})
			}
		})
	}
}

func BenchmarkListJobs_WithStatusFilter(b *testing.B) {
	db := adminBenchDB(b)

	b.StopTimer()
	adminTruncate(b, db)
	for i := 0; i < 500; i++ {
		adminSeedJob(b, db, fmt.Sprintf("job-%d", i), "tenant-filter", i%10)
	}
	// Seed some paused jobs too
	db.ExecContext(context.Background(), `
		INSERT INTO jobs (name, tenant_id, trigger_type, handler_type, handler_payload, timeout_sec, status, priority)
		VALUES ('paused-job', 'tenant-filter', 'manual', 'http', '{}'::jsonb, 60, 'paused', 5)
	`)
	repo := NewJobRepository(db)
	b.StartTimer()

	b.Run("status=active", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, _ = repo.List(context.Background(), query.ListInput{
				TenantID: "tenant-filter",
				Status:   "active",
				Limit:    10,
				Offset:   0,
			})
		}
	})
}

// ---------------------------------------------------------------------------
// Get — single-row lookup with payload deserialization
// ---------------------------------------------------------------------------

func BenchmarkGetJob(b *testing.B) {
	db := adminBenchDB(b)

	tests := []struct {
		name    string
		payload string
	}{
		{"small_payload", `{"url":"https://example.com/hook"}`},
		{"medium_payload", fmt.Sprintf(`{"url":"https://example.com/hook","body":{"data":"%s"}}`, strings.Repeat("x", 512))},
		{"large_payload_4kb", fmt.Sprintf(`{"data":"%s"}`, strings.Repeat("x", 4000))},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.StopTimer()
			adminTruncate(b, db)
			jobID := adminSeedJobWithPayload(b, db, "get-bench", "tenant-get", tt.payload)
			repo := NewJobRepository(db)
			b.StartTimer()

			b.ReportAllocs()
			for b.Loop() {
				_, _ = repo.Get(context.Background(), query.GetInput{
					TenantID: "tenant-get",
					ID:       jobID,
				})
			}
		})
	}
}
