package postgrestest

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// BenchDB opens a PostgreSQL connection for benchmarks.
func BenchDB(b *testing.B) *sql.DB {
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

// BenchTruncate truncates all data tables and restarts sequences.
func BenchTruncate(b *testing.B, db *sql.DB) {
	b.Helper()
	_, err := db.ExecContext(context.Background(), `
		TRUNCATE TABLE audit_events, job_instance_attempts, job_instances, workers, jobs RESTART IDENTITY CASCADE
	`)
	if err != nil {
		b.Fatalf("truncate: %v", err)
	}
}

// BenchSeedJob inserts a minimal manual job and returns its ID.
func BenchSeedJob(b *testing.B, db *sql.DB, name, tenantID, handlerType string, priority int) int64 {
	b.Helper()
	var id int64
	err := db.QueryRowContext(context.Background(), `
		INSERT INTO jobs (name, tenant_id, trigger_type, handler_type, handler_payload, timeout_sec, status, priority)
		VALUES ($1, $2, 'manual', $3, '{}'::jsonb, 60, 'active', $4)
		RETURNING id
	`, name, tenantID, handlerType, priority).Scan(&id)
	if err != nil {
		b.Fatalf("seed job: %v", err)
	}
	return id
}
