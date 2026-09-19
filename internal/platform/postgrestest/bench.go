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
//
// It mirrors Open: the handle is scoped to a schema of its own, built from
// db/migrations, and dropped when the benchmark finishes. The previous version
// returned a handle on the bare DSN, which resolved to whatever the server's
// default search_path named -- so a benchmark read and wrote tables shared with
// every other run, and the truncation helper below had no schema it could own.
func BenchDB(b *testing.B) *sql.DB {
	b.Helper()

	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		b.Skip("TEST_DATABASE_DSN is not set")
	}

	benchDSN, schemaName, err := testDSN(dsn, b.Name())
	if err != nil {
		b.Fatalf("scope benchmark dsn: %v", redactDSN(err.Error()))
	}

	db, err := open(benchDSN)
	if err != nil {
		b.Fatalf("open benchmark db: %v", redactDSN(err.Error()))
	}
	b.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		b.Fatalf("ping benchmark db: %v", redactDSN(err.Error()))
	}

	if err := withAdvisoryLock(
		benchDSN,
		sharedSchemaLockClassID,
		lockObjectID(benchDSN),
		func(_ *sql.DB) error {
			return applySchemaWithDB(benchDSN, db)
		},
	); err != nil {
		b.Fatalf("prepare benchmark schema: %v", redactDSN(err.Error()))
	}
	b.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()

		if err := dropSchema(cleanupCtx, db, schemaName); err != nil {
			b.Errorf("drop benchmark schema %q: %v", schemaName, err)
		}
	})

	return db
}

// BenchTruncate empties every table in the benchmark's schema and restarts its
// sequences.
func BenchTruncate(b *testing.B, db *sql.DB) {
	b.Helper()
	if err := resetTestData(context.Background(), db); err != nil {
		b.Fatalf("truncate: %v", err)
	}
}
