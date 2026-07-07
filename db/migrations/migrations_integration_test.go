//go:build integration

package migrations

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"orbitjob/internal/platform/config"
)

// TestMigrationsAreIdempotent runs every *.up.sql migration twice against the
// test database. The second run must not fail with "already exists" or similar
// errors, proving all migration statements are idempotent.
func TestMigrationsAreIdempotent(t *testing.T) {
	if err := config.LoadDotenv(); err != nil {
		t.Fatalf("load .env: %v", err)
	}

	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	files, err := filepath.Glob("*.up.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatal("no .up.sql migration files found")
	}

	for _, f := range files {
		content, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if _, err := db.ExecContext(ctx, string(content)); err != nil {
			t.Fatalf("first run of %s failed: %v", f, err)
		}
	}

	for _, f := range files {
		content, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if _, err := db.ExecContext(ctx, string(content)); err != nil {
			t.Fatalf("second run of %s failed (not idempotent): %v", f, err)
		}
	}
}
