package checkschedule

import (
	"context"
	"errors"
	"fmt"
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

func TestBeforeActivationSkipsOnlyUnacceptedSlot(t *testing.T) {
	now := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	for _, schedule := range []string{check.ScheduleTypeInterval, check.ScheduleTypeCron} {
		t.Run(schedule, func(t *testing.T) {
			c := intervalCheck(1, 30, now.Add(-time.Second))
			if schedule == check.ScheduleTypeCron {
				expr := "0 9 * * *"
				c.ScheduleType, c.CronExpr, c.Timezone = schedule, &expr, "Asia/Shanghai"
			}
			f := newFixture([]check.Snapshot{c}, now)
			f.runs.createErr = fmt.Errorf("admission: %w", &jobrun.AdmissionBlockedError{Reason: "before_revision_activation"})
			if created, err := f.uc.RunBatch(t.Context(), testScope, 10); err != nil || created != 0 {
				t.Fatalf("created=%d err=%v", created, err)
			}
			if len(f.runs.order) != 0 || len(f.publish.published) != 0 {
				t.Fatal("ineligible slot created or published a run")
			}
			if len(f.checks.updated) != 1 {
				t.Fatalf("cursor updates=%+v", f.checks.updated)
			}
			update := f.checks.updated[0]
			if update.version != c.Version || !update.previous.Equal(*c.NextRunAt) || !update.nextRunAt.Equal(nextRunAt(c, now)) || !update.nextRunAt.After(now) {
				t.Fatalf("cursor update=%+v", update)
			}
		})
	}
}

func TestOtherAdmissionFailuresPreserveCheckCursor(t *testing.T) {
	now := time.Now().UTC()
	for _, reason := range []string{"schedule_paused", "execution_paused", "tenant_suspended", "source_deleted", "concurrency"} {
		t.Run(reason, func(t *testing.T) {
			f := newFixture([]check.Snapshot{intervalCheck(1, 30, now.Add(-time.Second))}, now)
			f.runs.createErr = &jobrun.AdmissionBlockedError{Reason: reason}
			if created, err := f.uc.RunBatch(t.Context(), testScope, 10); err != nil || created != 0 {
				t.Fatalf("created=%d err=%v", created, err)
			}
			if len(f.checks.updated) != 0 || len(f.runs.order) != 0 || len(f.publish.published) != 0 {
				t.Fatal("blocked admission changed scheduling or execution state")
			}
		})
	}
}

func TestBeforeActivationCursorFailureRemainsRetryable(t *testing.T) {
	now := time.Now().UTC()
	c := intervalCheck(1, 30, now.Add(-time.Second))
	f := newFixture([]check.Snapshot{c}, now)
	f.runs.createErr = &jobrun.AdmissionBlockedError{Reason: "before_revision_activation"}
	f.checks.updateErr = errors.New("cursor temporarily unavailable")
	if _, err := f.uc.fireCheck(t.Context(), testScope, c, now); err == nil {
		t.Fatal("failed skip cursor write was hidden")
	}
	f.checks.updateErr = nil
	if created, err := f.uc.fireCheck(t.Context(), testScope, c, now); err != nil || created != 0 {
		t.Fatalf("retry created=%d err=%v", created, err)
	}
	if len(f.checks.updated) != 2 || !f.checks.updated[0].nextRunAt.Equal(f.checks.updated[1].nextRunAt) || len(f.runs.order) != 0 {
		t.Fatal("retry changed the skipped slot or created execution")
	}
	cursor := f.checks.updated[1].nextRunAt
	c.NextRunAt = &cursor
	f.checks.due, f.now, f.runs.createErr = []check.Snapshot{c}, cursor, nil
	if created, err := f.uc.RunBatch(t.Context(), testScope, 10); err != nil || created != 1 {
		t.Fatalf("eligible tick created=%d err=%v", created, err)
	}
	if len(f.runs.order) != 1 || len(f.publish.published) != 1 {
		t.Fatal("eligible tick did not create exactly one occurrence")
	}
}
