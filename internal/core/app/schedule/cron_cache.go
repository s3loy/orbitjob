package schedule

import (
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// cronParser accepts both 5-field (minute) and 6-field (second) expressions.
var cronParser = cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

type cronCacheEntry struct {
	schedule cron.Schedule
	location *time.Location
}

var cronCache sync.Map // key: "cronExpr|timezone"

// getCachedSchedule returns a pre-parsed cron.Schedule and *time.Location.
// Results are cached globally: identical (cron_expr, timezone) pairs share
// the same parsed instances across all jobs and all ticks.
func getCachedSchedule(cronExpr, timezone string) (cron.Schedule, *time.Location, error) {
	key := cronExpr + "|" + timezone
	if v, ok := cronCache.Load(key); ok {
		e := v.(cronCacheEntry)
		return e.schedule, e.location, nil
	}

	loc, err := time.LoadLocation(strings.TrimSpace(timezone))
	if err != nil {
		return nil, nil, err
	}
	schedule, err := cronParser.Parse(strings.TrimSpace(cronExpr))
	if err != nil {
		return nil, nil, err
	}

	// Only cache valid entries. Multiple concurrent calls for the same key
	// may race here; Store is atomic and the last writer wins — harmless
	// since both produce identical values.
	cronCache.Store(key, cronCacheEntry{schedule: schedule, location: loc})
	return schedule, loc, nil
}
