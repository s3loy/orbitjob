//go:build integration

package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	instancecommand "orbitjob/internal/admin/app/instance/command"
	instancequery "orbitjob/internal/admin/app/instance/query"
	command "orbitjob/internal/admin/app/job/command"
	adminhttp "orbitjob/internal/admin/http"
	"orbitjob/internal/admin/http/middleware"
	adminpostgres "orbitjob/internal/admin/store/postgres"
	domaininstance "orbitjob/internal/core/domain/instance"
	domainjob "orbitjob/internal/core/domain/job"
	"orbitjob/internal/core/store/postgres"
	"orbitjob/internal/platform/postgrestest"
)

func TestManualTrigger_Idempotent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	db := postgrestest.Open(t)

	tenantID := "manual-trigger-tenant-0001" // 26 characters to match tenants.id
	createTenant(t, ctx, db, tenantID)

	job := createManualJob(t, ctx, db, tenantID)
	handler := newTestAdminHandler(t, db)
	router := newTestRouter(tenantID, handler)

	idempotencyKey := "idem-key-12345"
	url := "/api/v1/jobs/" + strconv.FormatInt(job.ID, 10) + "/trigger"

	req1 := httptest.NewRequest(http.MethodPost, url, nil)
	req1.Header.Set("X-OrbitJob-Idempotency-Key", idempotencyKey)
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusCreated {
		t.Fatalf("expected first trigger status %d, got %d: %s", http.StatusCreated, w1.Code, w1.Body.String())
	}

	var resp1 triggerResponse
	if err := json.Unmarshal(w1.Body.Bytes(), &resp1); err != nil {
		t.Fatalf("unmarshal first trigger response: %v", err)
	}
	if !resp1.Created {
		t.Fatalf("expected first trigger created=true, got false")
	}
	if resp1.RunID == "" {
		t.Fatalf("expected first trigger run_id to be set")
	}

	req2 := httptest.NewRequest(http.MethodPost, url, nil)
	req2.Header.Set("X-OrbitJob-Idempotency-Key", idempotencyKey)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected second trigger status %d, got %d: %s", http.StatusOK, w2.Code, w2.Body.String())
	}

	var resp2 triggerResponse
	if err := json.Unmarshal(w2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("unmarshal second trigger response: %v", err)
	}
	if resp2.Created {
		t.Fatalf("expected second trigger created=false, got true")
	}
	if resp2.RunID != resp1.RunID {
		t.Fatalf("expected same run_id for idempotent triggers, got %q and %q", resp1.RunID, resp2.RunID)
	}
}

func TestCancelInstance_Pending(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	db := postgrestest.Open(t)

	tenantID := "cancel-pending-tenant-0001" // 26 characters to match tenants.id
	createTenant(t, ctx, db, tenantID)

	job := createManualJob(t, ctx, db, tenantID)
	handler := newTestAdminHandler(t, db)
	router := newTestRouter(tenantID, handler)

	triggerURL := "/api/v1/jobs/" + strconv.FormatInt(job.ID, 10) + "/trigger"
	triggerReq := httptest.NewRequest(http.MethodPost, triggerURL, nil)
	triggerW := httptest.NewRecorder()
	router.ServeHTTP(triggerW, triggerReq)
	if triggerW.Code != http.StatusCreated {
		t.Fatalf("expected trigger status %d, got %d: %s", http.StatusCreated, triggerW.Code, triggerW.Body.String())
	}

	var triggerResp triggerResponse
	if err := json.Unmarshal(triggerW.Body.Bytes(), &triggerResp); err != nil {
		t.Fatalf("unmarshal trigger response: %v", err)
	}
	if triggerResp.Status != domaininstance.StatusPending {
		t.Fatalf("expected instance status %q after trigger, got %q", domaininstance.StatusPending, triggerResp.Status)
	}

	version := instanceVersion(t, ctx, db, tenantID, triggerResp.RunID)

	cancelURL := "/api/v1/instances/" + triggerResp.RunID + "/cancel"
	body, err := json.Marshal(map[string]int{"version": version})
	if err != nil {
		t.Fatalf("marshal cancel body: %v", err)
	}
	cancelReq := httptest.NewRequest(http.MethodPost, cancelURL, bytes.NewReader(body))
	cancelW := httptest.NewRecorder()
	router.ServeHTTP(cancelW, cancelReq)
	if cancelW.Code != http.StatusOK {
		t.Fatalf("expected cancel status %d, got %d: %s", http.StatusOK, cancelW.Code, cancelW.Body.String())
	}

	var cancelResp domaininstance.Snapshot
	if err := json.Unmarshal(cancelW.Body.Bytes(), &cancelResp); err != nil {
		t.Fatalf("unmarshal cancel response: %v", err)
	}
	if cancelResp.Status != domaininstance.StatusCanceled {
		t.Fatalf("expected cancel response status %q, got %q", domaininstance.StatusCanceled, cancelResp.Status)
	}

	finalStatus := instanceStatus(t, ctx, db, tenantID, triggerResp.RunID)
	if finalStatus != domaininstance.StatusCanceled {
		t.Fatalf("expected final instance status %q, got %q", domaininstance.StatusCanceled, finalStatus)
	}
}

type triggerResponse struct {
	RunID    string `json:"run_id"`
	JobID    int64  `json:"job_id"`
	TenantID string `json:"tenant_id"`
	Status   string `json:"status"`
	Created  bool   `json:"created"`
}

func createManualJob(t *testing.T, ctx context.Context, db *sql.DB, tenantID string) domainjob.Snapshot {
	t.Helper()

	spec, err := domainjob.NormalizeCreate(time.Now().UTC(), domainjob.CreateInput{
		Name:        "manual-trigger-job",
		TenantID:    tenantID,
		TriggerType: domainjob.TriggerTypeManual,
		HandlerType: domainjob.HandlerTypeHTTP,
		HandlerPayload: map[string]any{
			"url":    "http://localhost:9999",
			"method": "GET",
		},
	})
	if err != nil {
		t.Fatalf("normalize manual job: %v", err)
	}

	jobRepo := postgres.NewJobRepository(db)
	job, err := jobRepo.Create(ctx, spec)
	if err != nil {
		t.Fatalf("create manual job: %v", err)
	}
	if job.Status != domainjob.StatusActive {
		t.Fatalf("expected job status %q, got %q", domainjob.StatusActive, job.Status)
	}
	return job
}

func newTestAdminHandler(t *testing.T, db *sql.DB) *adminhttp.Handler {
	t.Helper()

	adminJobRepo := adminpostgres.NewJobRepository(db)
	coreInstanceRepo := postgres.NewInstanceRepository(db)
	adminInstanceRepo := adminpostgres.NewInstanceRepository(db)

	triggerUC := command.NewTriggerJobUseCase(adminJobRepo, coreInstanceRepo, coreInstanceRepo)
	cancelUC := instancecommand.NewCancelInstanceUseCase(adminInstanceRepo, coreInstanceRepo)

	handler := adminhttp.NewHandler(nil, nil, nil, nil, nil)
	handler.SetTriggerJobUseCase(triggerUC)
	handler.SetCancelInstanceUseCase(cancelUC)
	// Register additional use-cases only when needed; leaving them nil disables their routes.
	handler.SetListInstancesUseCase(instancequery.NewListInstancesUseCase(adminInstanceRepo))
	handler.SetGetInstanceUseCase(instancequery.NewGetInstanceUseCase(adminInstanceRepo))
	handler.SetListAttemptsUseCase(instancequery.NewListAttemptsUseCase(adminInstanceRepo))
	return handler
}

func newTestRouter(tenantID string, handler *adminhttp.Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		ctx := middleware.WithTenantID(c.Request.Context(), tenantID, middleware.TenantSourceHeader)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	handler.Register(r)
	return r
}

func instanceVersion(t *testing.T, ctx context.Context, db *sql.DB, tenantID, runID string) int {
	t.Helper()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin version tx: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID); err != nil {
		t.Fatalf("set tenant config: %v", err)
	}

	var version int
	err = tx.QueryRowContext(ctx, `
		SELECT version FROM job_instances WHERE tenant_id = $1 AND run_id = $2
	`, tenantID, runID).Scan(&version)
	if err != nil {
		t.Fatalf("query instance version: %v", err)
	}
	return version
}

func instanceStatus(t *testing.T, ctx context.Context, db *sql.DB, tenantID, runID string) string {
	t.Helper()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin status tx: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID); err != nil {
		t.Fatalf("set tenant config: %v", err)
	}

	var status string
	err = tx.QueryRowContext(ctx, `
		SELECT status FROM job_instances WHERE tenant_id = $1 AND run_id = $2
	`, tenantID, runID).Scan(&status)
	if err != nil {
		t.Fatalf("query instance status: %v", err)
	}
	return status
}
