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

// withSchedulerRLS enables FORCE ROW LEVEL SECURITY with the production-shaped
// tenant policy on jobs and job_instances. The test connection is the table
// owner, so FORCE is required to reproduce the runtime role's visibility.
// Returns a disable function; also registered with t.Cleanup as a safety net
// because the package shares one schema.
func withSchedulerRLS(t *testing.T, db *sql.DB) func() {
	t.Helper()
	enable := []string{
		`ALTER TABLE jobs ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE jobs FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS jobs_tenant_v020 ON jobs`,
		`CREATE POLICY jobs_tenant_v020 ON jobs USING (tenant_id = current_setting('app.tenant_id', true))`,
		`ALTER TABLE job_instances ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE job_instances FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS job_instances_tenant_v020 ON job_instances`,
		`CREATE POLICY job_instances_tenant_v020 ON job_instances USING (tenant_id = current_setting('app.tenant_id', true))`,
	}
	disable := []string{
		`ALTER TABLE jobs NO FORCE ROW LEVEL SECURITY`,
		`ALTER TABLE jobs DISABLE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS jobs_tenant_v020 ON jobs`,
		`ALTER TABLE job_instances NO FORCE ROW LEVEL SECURITY`,
		`ALTER TABLE job_instances DISABLE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS job_instances_tenant_v020 ON job_instances`,
	}
	for _, stmt := range enable {
		if _, err := db.ExecContext(context.Background(), stmt); err != nil {
			t.Fatalf("enable RLS: %v (%s)", err, stmt)
		}
	}
	off := func() {
		for _, stmt := range disable {
			if _, err := db.ExecContext(context.Background(), stmt); err != nil {
				t.Errorf("disable RLS: %v (%s)", err, stmt)
			}
		}
	}
	t.Cleanup(off)
	return off
}

// Regression test for the RLS claim blind spot: a cross-tenant claim without
// the tenant GUC sees zero rows and cron scheduling silently stalls.
func TestSchedulerRepository_ScheduleBatch_UnderRLS(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewSchedulerRepository(db)
	now := time.Now().UTC().Truncate(time.Second)

	jobA := seedDueCronJob(t, db, dueJobSeed{
		TenantID: "tenant-rls-a", Name: "cron-rls-a", Priority: 5, RetryLimit: 1,
		CronExpr: "*/5 * * * *", Timezone: "UTC", MisfirePolicy: domainjob.MisfireFireNow,
		NextRunAt: now.Add(-time.Minute),
	})
	jobB := seedDueCronJob(t, db, dueJobSeed{
		TenantID: "tenant-rls-b", Name: "cron-rls-b", Priority: 5, RetryLimit: 1,
		CronExpr: "*/5 * * * *", Timezone: "UTC", MisfirePolicy: domainjob.MisfireFireNow,
		NextRunAt: now.Add(-time.Minute),
	})

	off := withSchedulerRLS(t, db)

	counts, err := repo.ScheduleBatch(context.Background(), now, 10, schedule.DecideSchedule, ClassifyError)
	if err != nil {
		t.Fatalf("ScheduleBatch() error = %v", err)
	}
	if counts.Scheduled != 2 {
		t.Fatalf("expected 2 scheduled under RLS, got %+v", counts)
	}

	off()
	assertOnePendingScheduledInstance(t, db, "tenant-rls-a", jobA)
	assertOnePendingScheduledInstance(t, db, "tenant-rls-b", jobB)
}

func assertOnePendingScheduledInstance(t *testing.T, db *sql.DB, tenantID string, jobID int64) {
	t.Helper()
	var count int
	err := db.QueryRowContext(context.Background(), `
		SELECT count(*) FROM job_instances
		WHERE tenant_id = $1 AND job_id = $2 AND status = 'pending' AND trigger_source = 'schedule'
	`, tenantID, jobID).Scan(&count)
	if err != nil {
		t.Fatalf("count instances: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one pending scheduled instance for %s/%d, got %d", tenantID, jobID, count)
	}
}

func TestSchedulerRepository_ScheduleOneDueCron_UnderRLS(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewSchedulerRepository(db)
	now := time.Now().UTC().Truncate(time.Second)

	jobID := seedDueCronJob(t, db, dueJobSeed{
		TenantID: "tenant-rls-one", Name: "cron-rls-one", Priority: 8, RetryLimit: 2,
		CronExpr: "*/5 * * * *", Timezone: "UTC", MisfirePolicy: domainjob.MisfireFireNow,
		NextRunAt: now.Add(-time.Minute),
	})

	off := withSchedulerRLS(t, db)

	result, found, err := repo.ScheduleOneDueCron(context.Background(), now, schedule.DecideSchedule)
	if err != nil {
		t.Fatalf("ScheduleOneDueCron() error = %v", err)
	}
	if !found {
		t.Fatal("expected found=true under RLS")
	}
	if !result.Created || result.JobID != jobID {
		t.Fatalf("unexpected result: %+v", result)
	}

	off()
	assertScheduledInstance(t, db, "tenant-rls-one", jobID, now, 8, 3, result.RunID)
}

func TestSchedulerRepository_CountActiveInstances_UnderRLS(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewSchedulerRepository(db)
	now := time.Now().UTC().Truncate(time.Second)

	seedDueCronJob(t, db, dueJobSeed{
		TenantID: "tenant-rls-count", Name: "cron-rls-count", Priority: 5, RetryLimit: 1,
		CronExpr: "*/5 * * * *", Timezone: "UTC", MisfirePolicy: domainjob.MisfireFireNow,
		NextRunAt: now.Add(-time.Minute),
	})

	off := withSchedulerRLS(t, db)

	// Schedule one instance so the tenant has an active row; the count query
	// must see it under RLS.
	counts, err := repo.ScheduleBatch(context.Background(), now, 10, schedule.DecideSchedule, ClassifyError)
	if err != nil {
		t.Fatalf("ScheduleBatch() error = %v", err)
	}
	if counts.Scheduled != 1 {
		t.Fatalf("expected 1 scheduled, got %+v", counts)
	}

	total, err := repo.CountActiveInstances(context.Background())
	if err != nil {
		t.Fatalf("CountActiveInstances() error = %v", err)
	}
	if total != 1 {
		t.Fatalf("expected total=1 under RLS, got %d", total)
	}

	off()
}
