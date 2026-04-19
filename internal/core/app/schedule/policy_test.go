package schedule

import (
	"testing"
	"time"
)

func TestDecideSchedule_SkipMisfire_DoesNotCreateInstance(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	next := time.Date(2026, 4, 19, 11, 0, 0, 0, time.UTC)
	decision, err := DecideSchedule(now, DueCronJob{
		CronExpr:      "0 * * * *",
		Timezone:      "UTC",
		MisfirePolicy: "skip",
		NextRunAt:     next,
	})
	if err != nil {
		t.Fatalf("DecideSchedule() error = %v", err)
	}
	if decision.CreateInstance {
		t.Fatalf("expected CreateInstance=false for skip misfire")
	}
	if decision.NextRunAt == nil || !decision.NextRunAt.After(now) {
		t.Fatalf("expected next_run_at to advance beyond now")
	}
}

func TestDecideSchedule_FireNow_CreatesAtNow(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 3, 0, 0, time.UTC)
	next := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	decision, err := DecideSchedule(now, DueCronJob{
		CronExpr:      "*/5 * * * *",
		Timezone:      "UTC",
		MisfirePolicy: "fire_now",
		NextRunAt:     next,
	})
	if err != nil {
		t.Fatalf("DecideSchedule() error = %v", err)
	}
	if !decision.CreateInstance {
		t.Fatalf("expected CreateInstance=true")
	}
	if decision.ScheduledAt == nil || !decision.ScheduledAt.Equal(now) {
		t.Fatalf("expected scheduled_at=now")
	}
	if decision.NextRunAt == nil || !decision.NextRunAt.After(now) {
		t.Fatalf("expected next_run_at after now")
	}
}

func TestDecideSchedule_CatchUp_CreatesAtMissedSlot(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 3, 0, 0, time.UTC)
	next := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	decision, err := DecideSchedule(now, DueCronJob{
		CronExpr:      "*/5 * * * *",
		Timezone:      "UTC",
		MisfirePolicy: "catch_up",
		NextRunAt:     next,
	})
	if err != nil {
		t.Fatalf("DecideSchedule() error = %v", err)
	}
	if !decision.CreateInstance {
		t.Fatalf("expected CreateInstance=true")
	}
	if decision.ScheduledAt == nil || !decision.ScheduledAt.Equal(next) {
		t.Fatalf("expected scheduled_at=missed slot")
	}
}
