//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"orbitjob/internal/core/app/schedule"
	domainjob "orbitjob/internal/core/domain/job"
	"orbitjob/internal/platform/postgrestest"
)

func TestSchedulerRepository_ScheduleOneDueCron_FireNow(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewSchedulerRepository(db)
	now := time.Now().UTC().Truncate(time.Second)

	jobID := seedDueCronJob(t, db, dueJobSeed{
		TenantID:      "tenant-scheduler-fire-now",
		Name:          "cron-fire-now",
		Priority:      8,
		RetryLimit:    2,
		CronExpr:      "*/5 * * * *",
		Timezone:      "UTC",
		MisfirePolicy: domainjob.MisfireFireNow,
		NextRunAt:     now.Add(-time.Minute),
	})

	result, found, err := repo.ScheduleOneDueCron(context.Background(), now, schedule.DecideSchedule)
	if err != nil {
		t.Fatalf("ScheduleOneDueCron() error = %v", err)
	}
	if !found {
		t.Fatalf("expected found=true")
	}
	if !result.Created {
		t.Fatalf("expected Created=true")
	}
	if result.JobID != jobID {
		t.Fatalf("expected job_id=%d, got %d", jobID, result.JobID)
	}
	if result.RunID == "" {
		t.Fatalf("expected run_id to be set")
	}

	assertScheduledInstance(t, db, "tenant-scheduler-fire-now", jobID, now, 8, 3, result.RunID)
	assertJobCursorAdvanced(t, db, "tenant-scheduler-fire-now", jobID, now, true, now)
}

func TestSchedulerRepository_ScheduleOneDueCron_SkipMisfire(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewSchedulerRepository(db)
	now := time.Now().UTC().Truncate(time.Second)

	jobID := seedDueCronJob(t, db, dueJobSeed{
		TenantID:      "tenant-scheduler-skip",
		Name:          "cron-skip",
		Priority:      3,
		RetryLimit:    1,
		CronExpr:      "*/5 * * * *",
		Timezone:      "UTC",
		MisfirePolicy: domainjob.MisfireSkip,
		NextRunAt:     now.Add(-time.Minute),
	})

	result, found, err := repo.ScheduleOneDueCron(context.Background(), now, schedule.DecideSchedule)
	if err != nil {
		t.Fatalf("ScheduleOneDueCron() error = %v", err)
	}
	if !found {
		t.Fatalf("expected found=true")
	}
	if result.Created {
		t.Fatalf("expected Created=false for skip misfire")
	}

	assertNoScheduledInstance(t, db, "tenant-scheduler-skip", jobID)
	assertJobCursorAdvanced(t, db, "tenant-scheduler-skip", jobID, now, false, time.Time{})
}

func TestSchedulerRepository_ScheduleOneDueCron_CatchUpMisfire(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewSchedulerRepository(db)
	now := time.Now().UTC().Truncate(time.Second)
	missedSlot := now.Add(-time.Minute)

	jobID := seedDueCronJob(t, db, dueJobSeed{
		TenantID:      "tenant-scheduler-catch-up",
		Name:          "cron-catch-up",
		Priority:      6,
		RetryLimit:    3,
		CronExpr:      "*/5 * * * *",
		Timezone:      "UTC",
		MisfirePolicy: domainjob.MisfireCatchUp,
		NextRunAt:     missedSlot,
	})

	result, found, err := repo.ScheduleOneDueCron(context.Background(), now, schedule.DecideSchedule)
	if err != nil {
		t.Fatalf("ScheduleOneDueCron() error = %v", err)
	}
	if !found {
		t.Fatalf("expected found=true")
	}
	if !result.Created {
		t.Fatalf("expected Created=true for catch_up misfire")
	}
	if result.JobID != jobID {
		t.Fatalf("expected job_id=%d, got %d", jobID, result.JobID)
	}
	if result.RunID == "" {
		t.Fatalf("expected run_id to be set")
	}

	assertScheduledInstance(t, db, "tenant-scheduler-catch-up", jobID, missedSlot, 6, 4, result.RunID)
	assertJobCursorAdvanced(t, db, "tenant-scheduler-catch-up", jobID, missedSlot, true, missedSlot)
}

func TestSchedulerRepository_ScheduleOneDueCron_NoCandidate(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewSchedulerRepository(db)
	now := time.Now().UTC().Truncate(time.Second)

	_ = seedDueCronJob(t, db, dueJobSeed{
		TenantID:      "tenant-scheduler-future",
		Name:          "cron-future",
		Priority:      1,
		RetryLimit:    0,
		CronExpr:      "*/5 * * * *",
		Timezone:      "UTC",
		MisfirePolicy: domainjob.MisfireFireNow,
		NextRunAt:     now.Add(time.Hour),
	})

	result, found, err := repo.ScheduleOneDueCron(context.Background(), now, schedule.DecideSchedule)
	if err != nil {
		t.Fatalf("ScheduleOneDueCron() error = %v", err)
	}
	if found {
		t.Fatalf("expected found=false")
	}
	if result.Created {
		t.Fatalf("expected Created=false")
	}
}

type dueJobSeed struct {
	TenantID      string
	Name          string
	Priority      int
	RetryLimit    int
	CronExpr      string
	Timezone      string
	MisfirePolicy string
	NextRunAt     time.Time
}

func seedDueCronJob(t *testing.T, db *sql.DB, in dueJobSeed) int64 {
	t.Helper()

	var id int64
	err := db.QueryRowContext(context.Background(), `
		INSERT INTO jobs (
			name,
			tenant_id,
			priority,
			trigger_type,
			cron_expr,
			timezone,
			handler_type,
			retry_limit,
			misfire_policy,
			next_run_at
		)
		VALUES ($1, $2, $3, 'cron', $4, $5, 'worker', $6, $7, $8)
		RETURNING id
	`,
		in.Name,
		in.TenantID,
		in.Priority,
		in.CronExpr,
		in.Timezone,
		in.RetryLimit,
		in.MisfirePolicy,
		in.NextRunAt,
	).Scan(&id)
	if err != nil {
		t.Fatalf("seed due cron job: %v", err)
	}

	return id
}

func assertScheduledInstance(
	t *testing.T,
	db *sql.DB,
	tenantID string,
	jobID int64,
	expectedScheduledAt time.Time,
	expectedPriority int,
	expectedMaxAttempt int,
	expectedRunID string,
) {
	t.Helper()

	var (
		count       int
		runID       string
		status      string
		triggerSrc  string
		scheduledAt time.Time
		priority    int
		attempt     int
		maxAttempt  int
	)

	err := db.QueryRowContext(context.Background(), `
		SELECT count(*)
		FROM job_instances
		WHERE tenant_id = $1 AND job_id = $2
	`, tenantID, jobID).Scan(&count)
	if err != nil {
		t.Fatalf("count job_instances: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one scheduled instance, got %d", count)
	}

	err = db.QueryRowContext(context.Background(), `
		SELECT run_id::text, status, trigger_source, scheduled_at, priority, attempt, max_attempt
		FROM job_instances
		WHERE tenant_id = $1 AND job_id = $2
		ORDER BY id DESC
		LIMIT 1
	`, tenantID, jobID).Scan(
		&runID,
		&status,
		&triggerSrc,
		&scheduledAt,
		&priority,
		&attempt,
		&maxAttempt,
	)
	if err != nil {
		t.Fatalf("load scheduled instance: %v", err)
	}
	if runID != expectedRunID {
		t.Fatalf("expected run_id=%q, got %q", expectedRunID, runID)
	}
	if status != "pending" {
		t.Fatalf("expected status=%q, got %q", "pending", status)
	}
	if triggerSrc != "schedule" {
		t.Fatalf("expected trigger_source=%q, got %q", "schedule", triggerSrc)
	}
	if !scheduledAt.Equal(expectedScheduledAt) {
		t.Fatalf("expected scheduled_at=%s, got %s", expectedScheduledAt, scheduledAt)
	}
	if priority != expectedPriority {
		t.Fatalf("expected priority=%d, got %d", expectedPriority, priority)
	}
	if attempt != 1 {
		t.Fatalf("expected attempt=%d, got %d", 1, attempt)
	}
	if maxAttempt != expectedMaxAttempt {
		t.Fatalf("expected max_attempt=%d, got %d", expectedMaxAttempt, maxAttempt)
	}
}

func assertNoScheduledInstance(t *testing.T, db *sql.DB, tenantID string, jobID int64) {
	t.Helper()

	var count int
	err := db.QueryRowContext(context.Background(), `
		SELECT count(*)
		FROM job_instances
		WHERE tenant_id = $1 AND job_id = $2
	`, tenantID, jobID).Scan(&count)
	if err != nil {
		t.Fatalf("count job_instances: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no scheduled instance, got %d", count)
	}
}

func assertJobCursorAdvanced(
	t *testing.T,
	db *sql.DB,
	tenantID string,
	jobID int64,
	nextRunAtMustBeAfter time.Time,
	expectLastScheduled bool,
	expectedLastScheduledAt time.Time,
) {
	t.Helper()

	var (
		nextRunAt     sql.NullTime
		lastScheduled sql.NullTime
	)

	err := db.QueryRowContext(context.Background(), `
		SELECT next_run_at, last_scheduled_at
		FROM jobs
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, jobID).Scan(&nextRunAt, &lastScheduled)
	if err != nil {
		t.Fatalf("load job cursor: %v", err)
	}

	if !nextRunAt.Valid {
		t.Fatalf("expected next_run_at to be set")
	}
	if !nextRunAt.Time.After(nextRunAtMustBeAfter) {
		t.Fatalf("expected next_run_at > %s, got next_run_at=%s", nextRunAtMustBeAfter, nextRunAt.Time)
	}

	if expectLastScheduled {
		if !lastScheduled.Valid {
			t.Fatalf("expected last_scheduled_at to be set")
		}
		if !lastScheduled.Time.Equal(expectedLastScheduledAt) {
			t.Fatalf("expected last_scheduled_at=%s, got %s", expectedLastScheduledAt, lastScheduled.Time)
		}
		return
	}

	if lastScheduled.Valid {
		t.Fatalf("expected last_scheduled_at to stay NULL, got %s", lastScheduled.Time)
	}
}
