//go:build integration

package migrations

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestBaselineReappliesCleanly is property 6. The production path tracks applied
// versions in schema_migrations, so the runner never re-executes the file; but
// the file is also applied directly by hand, and every CREATE in it has to be
// safe to run twice. If a second application errors, the file is not idempotent
// and a manual re-run leaves a half-built schema.
func TestBaselineReappliesCleanly(t *testing.T) {
	applyAllMigrations(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db := ownerDB(t)

	count := func(query string) int {
		t.Helper()
		var n int
		if err := db.QueryRowContext(ctx, query).Scan(&n); err != nil {
			t.Fatalf("count %q: %v", query, err)
		}
		return n
	}
	relations := `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
	              WHERE n.nspname = 'public' AND c.relkind IN ('r','p')`

	relationsBefore := count(relations)
	policiesBefore := count(`SELECT count(*) FROM policies`)

	sqlBytes, err := os.ReadFile("0001_baseline.up.sql")
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	if _, err := db.ExecContext(ctx, string(sqlBytes)); err != nil {
		t.Fatalf("second application of the baseline failed: %v", err)
	}

	if after := count(relations); after != relationsBefore {
		t.Errorf("relations after re-apply = %d, want %d; a CREATE was not guarded", after, relationsBefore)
	}
	if after := count(`SELECT count(*) FROM policies`); after != policiesBefore {
		t.Errorf("platform presets after re-apply = %d, want %d; the seed INSERT is not idempotent", after, policiesBefore)
	}

	// The schema still satisfies its own assertion after a second application.
	if _, err := db.ExecContext(ctx, baselineStructuralAssertion(t)); err != nil {
		t.Fatalf("assertion rejected the schema after re-apply: %v", err)
	}
}
