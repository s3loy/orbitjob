package schedule

import (
	"strings"
	"time"
)

// DueCronJob is the minimum input needed to decide one scheduler tick action.
type DueCronJob struct {
	CronExpr      string
	Timezone      string
	MisfirePolicy string
	NextRunAt     time.Time
}

// ScheduleDecision describes whether an instance should be created and how to move next_run_at.
type ScheduleDecision struct {
	CreateInstance bool
	ScheduledAt    *time.Time
	NextRunAt      *time.Time
}

// DecideSchedule computes one scheduling decision for a due cron job.
func DecideSchedule(now time.Time, job DueCronJob) (ScheduleDecision, error) {
	tz := defaultIfEmpty(job.Timezone, "UTC")
	schedule, loc, err := getCachedSchedule(job.CronExpr, tz)
	if err != nil {
		return ScheduleDecision{}, err
	}

	nowInLoc := now.In(loc)
	nextInLoc := job.NextRunAt.In(loc)
	if nextInLoc.After(nowInLoc) {
		next := nextInLoc.UTC()
		return ScheduleDecision{CreateInstance: false, NextRunAt: &next}, nil
	}

	switch strings.TrimSpace(job.MisfirePolicy) {
	case "skip":
		next := schedule.Next(nowInLoc).UTC()
		return ScheduleDecision{CreateInstance: false, NextRunAt: &next}, nil
	case "catch_up":
		scheduledAt := nextInLoc.UTC()
		next := schedule.Next(nextInLoc).UTC()
		return ScheduleDecision{CreateInstance: true, ScheduledAt: &scheduledAt, NextRunAt: &next}, nil
	default: // fire_now + fallback
		scheduledAt := now.UTC()
		next := schedule.Next(nowInLoc).UTC()
		return ScheduleDecision{CreateInstance: true, ScheduledAt: &scheduledAt, NextRunAt: &next}, nil
	}
}

func defaultIfEmpty(in string, fallback string) string {
	if strings.TrimSpace(in) == "" {
		return fallback
	}
	return in
}
