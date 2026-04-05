package postgrestest

import (
	"context"
	"database/sql"
	"time"
)

// withAdvisoryLock serializes shared test-database access across go test processes.
func withAdvisoryLock(dsn string, classID, objectID int, fn func(db *sql.DB) error) (err error) {
	if err := validateDSN(dsn); err != nil {
		return err
	}

	db, err := open(dsn)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := db.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), advisoryLockWaitTimeout)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return err
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := conn.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1, $2)`, classID, objectID); err != nil {
		return err
	}

	locked := true
	defer func() {
		if !locked {
			return
		}

		unlockCtx, unlockCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer unlockCancel()
		if _, unlockErr := conn.ExecContext(
			unlockCtx,
			`SELECT pg_advisory_unlock($1, $2)`,
			classID,
			objectID,
		); err == nil && unlockErr != nil {
			err = unlockErr
		}
	}()

	err = fn(db)
	if err != nil {
		return err
	}

	locked = false
	unlockCtx, unlockCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer unlockCancel()
	if _, err := conn.ExecContext(unlockCtx, `SELECT pg_advisory_unlock($1, $2)`, classID, objectID); err != nil {
		return err
	}

	return nil
}
