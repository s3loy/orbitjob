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

	"orbitjob/internal/core/app/execute"
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

	createInput := domainjob.CreateInput{
		Name:        "e2e-cron-job",
		TenantID:    tenantID,
		TriggerType: domainjob.TriggerTypeCron,
		CronExpr:    &cronExpr,
		HandlerType: domainjob.HandlerTypeHTTP,
		HandlerPayload: map[string]any{
			"url":    "http://example.com/webhook",
			"method": "POST",
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

	// Stand up a local HTTP target. The built-in HTTP handler blocks loopback
	// addresses for SSRF protection, so this test uses a tiny test handler that
	// calls the server directly while still exercising the rest of the closed
	// loop through real repositories and use-cases.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer ts.Close()

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
	err = db.QueryRowContext(ctx, `
		SELECT id, version FROM job_instances
		WHERE tenant_id = $1 AND job_id = $2 AND status = 'pending'
	`, tenantID, job.ID).Scan(&instanceID, &instanceVersion)
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
	testHandler := &testHTTPHandler{client: ts.Client(), baseURL: ts.URL}
	result := testHandler.Execute(ctx, task)
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

	// Verify an audit event was recorded for the completion.
	var auditCount int
	err = db.QueryRowContext(ctx, `
		SELECT count(*) FROM audit_events
		WHERE tenant_id = $1 AND resource_type = 'instance' AND resource_id = $2
	`, tenantID, fmt.Sprintf("%d", instanceID)).Scan(&auditCount)
	if err != nil {
		t.Fatalf("count audit events: %v", err)
	}
	if auditCount < 1 {
		t.Fatalf("expected at least one audit event for instance, got %d", auditCount)
	}
}

func createTenant(t *testing.T, ctx context.Context, db *sql.DB, tenantID string) {
	t.Helper()

	_, err := db.ExecContext(ctx, `
		INSERT INTO tenants (id, slug, name, status)
		VALUES ($1, $2, $3, 'active')
		ON CONFLICT (id) DO NOTHING
	`, tenantID, tenantID, tenantID)
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
}

// testHTTPHandler is a minimal execute.Handler that calls a local httptest
// server. It is used because the built-in handler.HTTP rejects loopback URLs
// as part of its SSRF protection.
type testHTTPHandler struct {
	client  *http.Client
	baseURL string
}

func (h *testHTTPHandler) Execute(ctx context.Context, task execute.AssignedTask) execute.Result {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.baseURL, nil)
	if err != nil {
		return execute.Result{
			Success:    false,
			ResultCode: "build_request_failed",
			ErrorMsg:   err.Error(),
		}
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return execute.Result{
			Success:    false,
			ResultCode: "request_failed",
			ErrorMsg:   err.Error(),
		}
	}
	defer func() { _ = resp.Body.Close() }()

	resultCode := fmt.Sprintf("%d", resp.StatusCode)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return execute.Result{Success: true, ResultCode: resultCode}
	}
	return execute.Result{
		Success:    false,
		ResultCode: resultCode,
	}
}
