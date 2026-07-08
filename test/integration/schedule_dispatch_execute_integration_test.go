//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"orbitjob/internal/core/app/execute/handler"
	"orbitjob/internal/core/app/schedule"
	domaininstance "orbitjob/internal/core/domain/instance"
	domainjob "orbitjob/internal/core/domain/job"
	"orbitjob/internal/core/store/postgres"
	"orbitjob/internal/platform/postgrestest"
)

func TestCronJob_EndToEnd(t *testing.T) {
	ctx := context.Background()
	db := postgrestest.Open(t)

	// Use a 26-character tenant ID to match the tenants.id CHAR(26) column.
	tenantID := "e2e-tenant-000000000000001"
	createTenant(t, ctx, db, tenantID)

	now := time.Now().UTC().Truncate(time.Second)
	// Create the job as if it were scheduled a couple of minutes ago so that
	// next_run_at falls in the past relative to the scheduler tick.
	jobNow := now.Add(-2 * time.Minute)
	cronExpr := "* * * * *"

	// Stand up a local HTTP target. The real handler.HTTP will call it once the
	// loopback override environment variable is set.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST request, got %s", r.Method)
		}
		if r.Header.Get("X-Test") != "e2e" {
			t.Errorf("expected X-Test header to be forwarded")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer ts.Close()

	createInput := domainjob.CreateInput{
		Name:        "e2e-cron-job",
		TenantID:    tenantID,
		TriggerType: domainjob.TriggerTypeCron,
		CronExpr:    &cronExpr,
		HandlerType: domainjob.HandlerTypeHTTP,
		HandlerPayload: map[string]any{
			"url":     ts.URL,
			"method":  "POST",
			"body":    `{"ok":true}`,
			"headers": map[string]any{"X-Test": "e2e"},
		},
		MisfirePolicy: domainjob.MisfireFireNow,
	}

	jobSpec, err := domainjob.NormalizeCreate(jobNow, createInput)
	if err != nil {
		t.Fatalf("normalize create job: %v", err)
	}

	jobRepo := postgres.NewJobRepository(db)
	job, err := jobRepo.Create(ctx, jobSpec)
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if job.Status != domainjob.StatusActive {
		t.Fatalf("expected job status %q, got %q", domainjob.StatusActive, job.Status)
	}

	// Scheduler tick: creates a pending instance from the due cron job.
	schedRepo := postgres.NewSchedulerRepository(db)
	scheduler := schedule.NewTickUseCase(schedRepo, postgres.ClassifyError)
	counts, err := scheduler.RunBatch(ctx, now, 10)
	if err != nil {
		t.Fatalf("scheduler RunBatch: %v", err)
	}
	if counts.Scheduled != 1 {
		t.Fatalf("expected 1 scheduled instance, got %+v", counts)
	}

	var instanceID int64
	var instanceVersion int
	var runID string
	err = db.QueryRowContext(ctx, `
		SELECT id, version, run_id::text FROM job_instances
		WHERE tenant_id = $1 AND job_id = $2 AND status = 'pending'
	`, tenantID, job.ID).Scan(&instanceID, &instanceVersion, &runID)
	if err != nil {
		t.Fatalf("query pending instance: %v", err)
	}
	if instanceVersion != 1 {
		t.Fatalf("expected initial instance version=1, got %d", instanceVersion)
	}

	// Dispatcher tick: transitions the pending instance to dispatched.
	dispatchRepo := postgres.NewDispatchRepository(db)
	handled, err := dispatchRepo.DispatchBatch(ctx, domaininstance.ClaimSpec{
		TenantID:       tenantID,
		LeaseExpiresAt: now.Add(30 * time.Second),
		Now:            now,
	}, 10, domaininstance.DecideDispatch)
	if err != nil {
		t.Fatalf("dispatcher DispatchBatch: %v", err)
	}
	if handled != 1 {
		t.Fatalf("expected 1 dispatched instance, got %d", handled)
	}

	var dispatchedStatus string
	err = db.QueryRowContext(ctx, `
		SELECT status FROM job_instances WHERE tenant_id = $1 AND id = $2
	`, tenantID, instanceID).Scan(&dispatchedStatus)
	if err != nil {
		t.Fatalf("query dispatched instance: %v", err)
	}
	if dispatchedStatus != domaininstance.StatusDispatched {
		t.Fatalf("expected status %q after dispatch, got %q", domaininstance.StatusDispatched, dispatchedStatus)
	}

	// Worker claim: transitions the dispatched instance to running.
	execRepo := postgres.NewExecutorRepository(db)
	tasks, err := execRepo.ClaimNextDispatched(
		ctx,
		tenantID,
		"worker-1",
		1,
		now.Add(30*time.Second),
		now,
		nil,
	)
	if err != nil {
		t.Fatalf("executor ClaimNextDispatched: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 claimed task, got %d", len(tasks))
	}
	task := tasks[0]
	if task.JobID != job.ID {
		t.Fatalf("expected task job_id=%d, got %d", job.ID, task.JobID)
	}
	if task.HandlerType != domainjob.HandlerTypeHTTP {
		t.Fatalf("expected task handler_type=%q, got %q", domainjob.HandlerTypeHTTP, task.HandlerType)
	}

	var runningStatus string
	err = db.QueryRowContext(ctx, `
		SELECT status FROM job_instances WHERE tenant_id = $1 AND id = $2
	`, tenantID, instanceID).Scan(&runningStatus)
	if err != nil {
		t.Fatalf("query running instance: %v", err)
	}
	if runningStatus != domaininstance.StatusRunning {
		t.Fatalf("expected status %q after claim, got %q", domaininstance.StatusRunning, runningStatus)
	}

	// Execute the handler against the test server.
	handler.SetAllowLoopbackForTest(true)
	t.Cleanup(func() { handler.SetAllowLoopbackForTest(false) })
	result := handler.NewHTTP(nil).Execute(ctx, task)
	if !result.Success {
		t.Fatalf("expected handler success, got result_code=%q error=%q", result.ResultCode, result.ErrorMsg)
	}

	// Complete the instance as successful.
	completeSpec, err := domaininstance.NormalizeComplete(domaininstance.CompleteInput{
		TenantID:             tenantID,
		InstanceID:           task.InstanceID,
		WorkerID:             "worker-1",
		Success:              true,
		ResultCode:           result.ResultCode,
		Now:                  now,
		StartedAt:            task.StartedAt,
		Attempt:              task.Attempt,
		MaxAttempt:           task.MaxAttempt,
		RetryBackoffSec:      task.RetryBackoffSec,
		RetryBackoffStrategy: task.RetryBackoffStrategy,
	})
	if err != nil {
		t.Fatalf("normalize complete: %v", err)
	}
	if err := execRepo.CompleteInstance(ctx, completeSpec); err != nil {
		t.Fatalf("complete instance: %v", err)
	}

	// Assert the instance finished as success.
	var finalStatus string
	var finishedAt time.Time
	err = db.QueryRowContext(ctx, `
		SELECT status, finished_at FROM job_instances WHERE tenant_id = $1 AND id = $2
	`, tenantID, instanceID).Scan(&finalStatus, &finishedAt)
	if err != nil {
		t.Fatalf("query final instance: %v", err)
	}
	if finalStatus != domaininstance.StatusSuccess {
		t.Fatalf("expected final status %q, got %q", domaininstance.StatusSuccess, finalStatus)
	}
	if finishedAt.IsZero() {
		t.Fatalf("expected finished_at to be set")
	}

	// Verify audit events include the expected lifecycle events.
	auditTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin audit tx: %v", err)
	}
	defer func() { _ = auditTx.Rollback() }()

	_, err = auditTx.ExecContext(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID)
	if err != nil {
		t.Fatalf("set tenant config for audit query: %v", err)
	}
	rows, err := auditTx.QueryContext(ctx, `
		SELECT event_type FROM audit_events
		WHERE tenant_id = $1
		  AND resource_type = 'instance'
		  AND resource_id IN ($2, $3)
	`, tenantID, runID, fmt.Sprintf("%d", instanceID))
	if err != nil {
		t.Fatalf("query audit events: %v", err)
	}

	required := map[string]bool{
		"instance.created":          false,
		"instance.completed":        false,
		"instance.status_changed": false,
	}
	for rows.Next() {
		var eventType string
		if err := rows.Scan(&eventType); err != nil {
			_ = rows.Close()
			t.Fatalf("scan audit event type: %v", err)
		}
		if _, ok := required[eventType]; ok {
			required[eventType] = true
		}
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate audit events: %v", err)
	}
	for eventType, seen := range required {
		if !seen {
			t.Fatalf("expected audit event %q not found", eventType)
		}
	}
	_ = auditTx.Rollback()
}

func createTenant(t *testing.T, ctx context.Context, db *sql.DB, tenantID string) {
	t.Helper()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tenant tx: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID)
	if err != nil {
		t.Fatalf("set tenant config: %v", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO tenants (id, slug, name, status)
		VALUES ($1, $2, $3, 'active')
		ON CONFLICT (id) DO NOTHING
	`, tenantID, tenantID, tenantID)
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("commit tenant tx: %v", err)
	}
}
