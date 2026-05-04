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

func TestDecideSchedule_InvalidTimezone_ReturnsError(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	_, err := DecideSchedule(now, DueCronJob{
		CronExpr:      "*/5 * * * *",
		Timezone:      "Mars/Olympus",
		MisfirePolicy: "fire_now",
		NextRunAt:     now,
	})
	if err == nil {
		t.Fatalf("expected error for invalid timezone")
	}
}

func TestDecideSchedule_InvalidCron_ReturnsError(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	_, err := DecideSchedule(now, DueCronJob{
		CronExpr:      "not-a-cron",
		Timezone:      "UTC",
		MisfirePolicy: "fire_now",
		NextRunAt:     now,
	})
	if err == nil {
		t.Fatalf("expected error for invalid cron")
	}
}

func TestDecideSchedule_FutureNextRun_DoesNotCreateInstance(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	next := now.Add(2 * time.Minute)
	decision, err := DecideSchedule(now, DueCronJob{
		CronExpr:      "*/5 * * * *",
		Timezone:      "UTC",
		MisfirePolicy: "skip",
		NextRunAt:     next,
	})
	if err != nil {
		t.Fatalf("DecideSchedule() error = %v", err)
	}
	if decision.CreateInstance {
		t.Fatalf("expected CreateInstance=false when next_run_at is in the future")
	}
	if decision.ScheduledAt != nil {
		t.Fatalf("expected ScheduledAt=nil")
	}
	if decision.NextRunAt == nil || !decision.NextRunAt.Equal(next) {
		t.Fatalf("expected next_run_at to remain unchanged")
	}
}

func TestDecideSchedule_DefaultTimezoneAndFallbackPolicy(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 3, 0, 0, time.UTC)
	next := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	decision, err := DecideSchedule(now, DueCronJob{
		CronExpr:      "*/5 * * * *",
		Timezone:      " ",
		MisfirePolicy: "unknown",
		NextRunAt:     next,
	})
	if err != nil {
		t.Fatalf("DecideSchedule() error = %v", err)
	}
	if !decision.CreateInstance {
		t.Fatalf("expected CreateInstance=true for fallback policy")
	}
	if decision.ScheduledAt == nil || !decision.ScheduledAt.Equal(now) {
		t.Fatalf("expected scheduled_at=now for fallback policy")
	}
	if decision.NextRunAt == nil || !decision.NextRunAt.After(now) {
		t.Fatalf("expected next_run_at after now")
	}
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkDecideSchedule(b *testing.B) {
	now := time.Date(2026, 4, 19, 12, 3, 0, 0, time.UTC)

	tests := []struct {
		name string
		job  DueCronJob
	}{
		{"skip_misfire", DueCronJob{
			CronExpr: "0 * * * *", Timezone: "UTC", MisfirePolicy: "skip",
			NextRunAt: time.Date(2026, 4, 19, 11, 0, 0, 0, time.UTC),
		}},
		{"fire_now", DueCronJob{
			CronExpr: "*/5 * * * *", Timezone: "UTC", MisfirePolicy: "fire_now",
			NextRunAt: time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC),
		}},
		{"catch_up", DueCronJob{
			CronExpr: "*/5 * * * *", Timezone: "UTC", MisfirePolicy: "catch_up",
			NextRunAt: time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC),
		}},
		{"future_next_run", DueCronJob{
			CronExpr: "*/5 * * * *", Timezone: "UTC", MisfirePolicy: "skip",
			NextRunAt: now.Add(2 * time.Minute),
		}},
		{"with_timezone", DueCronJob{
			CronExpr: "0 9 * * *", Timezone: "Asia/Shanghai", MisfirePolicy: "fire_now",
			NextRunAt: time.Date(2026, 4, 19, 0, 0, 0, 0, time.UTC),
		}},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_, _ = DecideSchedule(now, tt.job)
			}
		})
	}
}
