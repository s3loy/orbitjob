package postgres

import (
	"context"
	"database/sql"
	"strconv"
)

// SLI source locks precede event-run locks. Compatibility facts have no source
// identity, so their writers and retirement use only this run-scoped lock.
func lockSLIEventRun(ctx context.Context, tx *sql.Tx, tenant string, runID int64) error {
	key := "sli-event-run:" + tenant + ":" + strconv.FormatInt(runID, 10)
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key)
	return err
}
