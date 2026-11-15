package checkschedule

import (
	"context"
	"errors"
	"orbitjob/internal/core/domain/check"
	"orbitjob/internal/core/domain/jobrun"
	"testing"
	"time"
)

func TestCompletedOccurrenceRepairsFailedCursor(t *testing.T) {
	now := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	f := newFixture([]check.Snapshot{intervalCheck(1, 60, now.Add(-time.Second))}, now)
	f.checks.updateErr = errors.New("cursor temporarily unavailable")
	if _, err := f.uc.RunBatch(context.Background(), testScope, 10); err != nil {
		t.Fatal(err)
	}
	for k, v := range f.runs.occurrences {
		v.Phase = jobrun.Succeeded
		f.runs.occurrences[k] = v
	}
	f.checks.updated = nil
	f.checks.updateErr = nil
	if _, err := f.uc.RunBatch(context.Background(), testScope, 10); err != nil {
		t.Fatal(err)
	}
	if len(f.checks.updated) != 1 || !f.checks.updated[0].nextRunAt.After(now) {
		t.Fatalf("cursor was not repaired: %+v", f.checks.updated)
	}
}
func TestCronCursorKeepsTimezoneAfterFirstOccurrence(t *testing.T) {
	now := time.Date(2026, 10, 24, 1, 0, 0, 0, time.UTC)
	expr := "0 9 * * *"
	c := intervalCheck(1, 60, now)
	c.ScheduleType = check.ScheduleTypeCron
	c.CronExpr = &expr
	c.Timezone = "Asia/Shanghai"
	if got := nextRunAt(c, now); !got.Equal(now.Add(24 * time.Hour)) {
		t.Fatalf("next=%s want next 09:00 Shanghai", got)
	}
}
func TestCheckPublicationCarriesDeadline(t *testing.T) {
	now := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	c := intervalCheck(1, 60, now)
	c.TimeoutSec = 37
	f := newFixture([]check.Snapshot{c}, now)
	if _, err := f.uc.RunBatch(context.Background(), testScope, 10); err != nil {
		t.Fatal(err)
	}
	if len(f.publish.published) != 1 || f.publish.published[0].Spec.TimeoutSeconds != 37 {
		t.Fatalf("published=%+v", f.publish.published)
	}
}
