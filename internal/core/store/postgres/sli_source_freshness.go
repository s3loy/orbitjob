package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"

	"github.com/robfig/cron/v3"
	"orbitjob/internal/core/domain/check"
)

// Known Check schedules supply an expected sampling cadence. Arbitrary manual
// Job sources have no invented cadence; their last timestamps remain visible.
func sliSourceFreshness(ctx context.Context, tx *sql.Tx, tenant, source string, last *time.Time) (bool, *time.Time, error) {
	checkID, ok := check.CheckIDFromSourceUID(source)
	if !ok {
		return true, nil, nil
	}
	var kind, zone, status string
	var expression sql.NullString
	var interval sql.NullInt64
	var timeout, retries int64
	var deleted sql.NullTime
	err := tx.QueryRowContext(ctx, `SELECT schedule_type,cron_expr,timezone,interval_sec,timeout_sec,retry_limit,status,deleted_at FROM checks WHERE tenant_id=$1 AND id=$2`, tenant, checkID).Scan(&kind, &expression, &zone, &interval, &timeout, &retries, &status, &deleted)
	if err == sql.ErrNoRows {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, err
	}
	if deleted.Valid || status != "active" {
		return false, nil, nil
	}
	if last == nil {
		return true, nil, nil
	}
	if timeout < 1 || retries < 0 || retries > 100 || timeout > (math.MaxInt64-30)/(retries+1) {
		return false, nil, fmt.Errorf("invalid Check sampling budget")
	}
	graceSeconds := timeout*(retries+1) + 30
	var deadline time.Time
	if kind == "interval" {
		if !interval.Valid || interval.Int64 < 1 || interval.Int64 > (math.MaxInt64-graceSeconds)/2 {
			return false, nil, fmt.Errorf("invalid Check sampling interval")
		}
		seconds := 2*interval.Int64 + graceSeconds
		if seconds > math.MaxInt64/int64(time.Second) {
			return false, nil, fmt.Errorf("check sampling interval exceeds clock range")
		}
		deadline = last.Add(time.Duration(seconds) * time.Second)
	} else if kind == "cron" && expression.Valid {
		location, err := time.LoadLocation(zone)
		if err != nil {
			return false, nil, err
		}
		schedule, err := cron.ParseStandard(expression.String)
		if err != nil {
			return false, nil, err
		}
		next := schedule.Next(last.In(location))
		if next.IsZero() {
			return false, nil, fmt.Errorf("check has no future sampling occurrence")
		}
		next = schedule.Next(next)
		if next.IsZero() {
			return false, nil, fmt.Errorf("check has no second sampling occurrence")
		}
		if graceSeconds > math.MaxInt64/int64(time.Second) {
			return false, nil, fmt.Errorf("check sampling grace exceeds clock range")
		}
		deadline = next.Add(time.Duration(graceSeconds) * time.Second).UTC()
	} else {
		return false, nil, fmt.Errorf("check sampling configuration is unavailable")
	}
	return true, &deadline, nil
}
