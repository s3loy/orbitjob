//go:build integration

package postgrestest

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"orbitjob/internal/platform/config"
)

const (
	expectedLockClassID  = 32117
	expectedLockObjectID = 260326
)

func TestApplySchemaWaitsForSharedDatabaseLock(t *testing.T) {
	dsn := packageTestDSN(t)
	holderDB := openTestDB(t, dsn, "holder")
	observerDB := openTestDB(t, dsn, "observer")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	holderConn := openTestConn(t, holderDB, ctx, "holder")
	unlock := mustAcquireAdvisoryLock(t, holderConn, expectedLockClassID, expectedLockObjectID)
	t.Cleanup(unlock)

	done := runSchemaApply(t, dsn)

	waiting, err := waitForWaitingLock(ctx, observerDB, expectedLockClassID, expectedLockObjectID)
	if err != nil {
		t.Fatalf("waitForWaitingLock() error = %v", err)
	}
	if !waiting {
		failWaitingForSharedLock(t, done)
	}

	unlock()
	assertSchemaApplyFinished(t, done)
}

func TestOpenIsolatesParallelTests(t *testing.T) {
	ready := make(chan struct{}, 2)
	start := make(chan struct{})

	go func() {
		<-ready
		<-ready
		close(start)
	}()

	for i := range 2 {
		t.Run(fmt.Sprintf("parallel_%d", i), func(t *testing.T) {
			t.Parallel()

			db := Open(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			if _, err := db.ExecContext(ctx, `
				INSERT INTO jobs (name, tenant_id, trigger_type, handler_type)
				VALUES ($1, $2, $3, $4)
			`, "same-name", "default", "manual", "http"); err != nil {
				t.Fatalf("insert job: %v", err)
			}

			ready <- struct{}{}
			<-start

			var count int
			if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs`).Scan(&count); err != nil {
				t.Fatalf("count jobs: %v", err)
			}
			if count != 1 {
				t.Fatalf("expected isolated jobs table count=1, got %d", count)
			}
		})
	}
}

func TestOpenCleansUpTestSchema(t *testing.T) {
	baseDSN := DSN(t)
	childFullName := t.Name() + "/child"

	_, schemaName, err := testDSN(baseDSN, childFullName)
	if err != nil {
		t.Fatalf("testDSN() error = %v", err)
	}

	t.Run("child", func(t *testing.T) {
		db := Open(t)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if _, err := db.ExecContext(ctx, `
			INSERT INTO jobs (name, tenant_id, trigger_type, handler_type)
			VALUES ($1, $2, $3, $4)
		`, "cleanup-check", "default", "manual", "http"); err != nil {
			t.Fatalf("insert job: %v", err)
		}
	})

	db, err := open(baseDSN)
	if err != nil {
		t.Fatalf("open package db: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var exists bool
	if err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_namespace
			WHERE nspname = $1
		)
	`, schemaName).Scan(&exists); err != nil {
		t.Fatalf("query schema existence: %v", err)
	}
	if exists {
		t.Fatalf("expected test schema %q to be dropped during cleanup", schemaName)
	}
}

func waitForWaitingLock(parent context.Context, db *sql.DB, classID, objectID int) (bool, error) {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(parent, 500*time.Millisecond)

		var waiting bool
		err := db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM pg_locks
				WHERE locktype = 'advisory'
				  AND classid = $1
				  AND objid = $2
				  AND granted = false
			)
		`, classID, objectID).Scan(&waiting)
		cancel()
		if err != nil {
			return false, err
		}
		if waiting {
			return true, nil
		}

		time.Sleep(50 * time.Millisecond)
	}

	return false, nil
}

func packageTestDSN(t *testing.T) string {
	t.Helper()

	if err := config.LoadDotenv(); err != nil {
		t.Fatalf("LoadDotenv() error = %v", err)
	}

	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}

	packagedDSN, _, err := packageDSN(dsn)
	if err != nil {
		t.Fatalf("packageDSN() error = %v", err)
	}

	return packagedDSN
}

func openTestDB(t *testing.T, dsn string, name string) *sql.DB {
	t.Helper()

	db, err := open(dsn)
	if err != nil {
		t.Fatalf("open %s db: %v", name, err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return db
}

func openTestConn(t *testing.T, db *sql.DB, ctx context.Context, name string) *sql.Conn {
	t.Helper()

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("%s Conn() error = %v", name, err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return conn
}

func mustAcquireAdvisoryLock(t *testing.T, conn *sql.Conn, classID int, objectID int) func() {
	t.Helper()

	lockCtx, lockCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer lockCancel()

	if _, err := conn.ExecContext(lockCtx,
		`SELECT pg_advisory_lock($1, $2)`,
		classID,
		objectID,
	); err != nil {
		t.Fatalf("pg_advisory_lock() error = %v", err)
	}

	locked := true
	return func() {
		if !locked {
			return
		}

		locked = false
		unlockCtx, unlockCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer unlockCancel()
		if _, err := conn.ExecContext(unlockCtx,
			`SELECT pg_advisory_unlock($1, $2)`,
			classID,
			objectID,
		); err != nil {
			t.Fatalf("pg_advisory_unlock() error = %v", err)
		}
	}
}

func runSchemaApply(t *testing.T, dsn string) <-chan error {
	t.Helper()

	done := make(chan error, 1)
	go func() {
		done <- withAdvisoryLock(
			dsn,
			sharedSchemaLockClassID,
			sharedSchemaLockObjectID,
			func(db *sql.DB) error {
				return applySchemaWithDB(dsn, db)
			},
		)
	}()

	return done
}

func failWaitingForSharedLock(t *testing.T, done <-chan error) {
	t.Helper()

	select {
	case err := <-done:
		t.Fatalf("schema setup finished before waiting on shared database lock: %v", err)
	default:
		t.Fatalf("schema setup did not request the shared database lock")
	}
}

func assertSchemaApplyFinished(t *testing.T, done <-chan error) {
	t.Helper()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("schema setup error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("schema setup did not finish after releasing the shared database lock")
	}
}
