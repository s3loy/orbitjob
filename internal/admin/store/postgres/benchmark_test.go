//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	query "orbitjob/internal/admin/app/job/query"
	"orbitjob/internal/platform/postgrestest"

	_ "github.com/lib/pq"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// shared helpers: postgrestest.BenchDB / BenchTruncate / BenchSeedJob

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
	db := postgrestest.BenchDB(b)

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
			postgrestest.BenchTruncate(b, db)
			for i := 0; i < sc.rows; i++ {
				postgrestest.BenchSeedJob(b, db, fmt.Sprintf("job-%d", i), "tenant-list", "http", i%10)
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
	db := postgrestest.BenchDB(b)

	b.StopTimer()
	postgrestest.BenchTruncate(b, db)
	for i := 0; i < 500; i++ {
		postgrestest.BenchSeedJob(b, db, fmt.Sprintf("job-%d", i), "tenant-filter", "http", i%10)
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
	db := postgrestest.BenchDB(b)

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
			postgrestest.BenchTruncate(b, db)
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
