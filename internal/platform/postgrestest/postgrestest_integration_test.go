//go:build integration

package postgrestest

import (
	"context"
	"database/sql"
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
	if err := config.LoadDotenv(); err != nil {
		t.Fatalf("LoadDotenv() error = %v", err)
	}

	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}
	dsn, _, err := packageDSN(dsn)
	if err != nil {
		t.Fatalf("packageDSN() error = %v", err)
	}

	holderDB, err := open(dsn)
	if err != nil {
		t.Fatalf("open holder db: %v", err)
	}
	t.Cleanup(func() { _ = holderDB.Close() })

	observerDB, err := open(dsn)
	if err != nil {
		t.Fatalf("open observer db: %v", err)
	}
	t.Cleanup(func() { _ = observerDB.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	holderConn, err := holderDB.Conn(ctx)
	if err != nil {
		t.Fatalf("holder Conn() error = %v", err)
	}
	t.Cleanup(func() { _ = holderConn.Close() })

	if _, err := holderConn.ExecContext(ctx,
		`SELECT pg_advisory_lock($1, $2)`,
		expectedLockClassID,
		expectedLockObjectID,
	); err != nil {
		t.Fatalf("pg_advisory_lock() error = %v", err)
	}

	locked := true
	t.Cleanup(func() {
		if !locked {
			return
		}
		unlockCtx, unlockCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer unlockCancel()
		if _, err := holderConn.ExecContext(unlockCtx,
			`SELECT pg_advisory_unlock($1, $2)`,
			expectedLockClassID,
			expectedLockObjectID,
		); err != nil {
			t.Fatalf("pg_advisory_unlock() error = %v", err)
		}
	})

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

	waiting, err := waitForWaitingLock(ctx, observerDB, expectedLockClassID, expectedLockObjectID)
	if err != nil {
		t.Fatalf("waitForWaitingLock() error = %v", err)
	}
	if !waiting {
		select {
		case err := <-done:
			t.Fatalf("schema setup finished before waiting on shared database lock: %v", err)
		default:
			t.Fatalf("schema setup did not request the shared database lock")
		}
	}

	if _, err := holderConn.ExecContext(ctx,
		`SELECT pg_advisory_unlock($1, $2)`,
		expectedLockClassID,
		expectedLockObjectID,
	); err != nil {
		t.Fatalf("pg_advisory_unlock() error = %v", err)
	}
	locked = false

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("schema setup error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("schema setup did not finish after releasing the shared database lock")
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
