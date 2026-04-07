package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	command "orbitjob/internal/admin/app/job/command"
	query "orbitjob/internal/admin/app/job/query"
	domainjob "orbitjob/internal/core/domain/job"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
)

type stubUpdateJobUseCase struct {
	called bool
	in     command.UpdateInput
	out    command.UpdateResult
	err    error
}

func (s *stubUpdateJobUseCase) Update(
	ctx context.Context,
	in command.UpdateInput,
) (command.UpdateResult, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

func currentJobForUpdateTests() query.GetItem {
	cronExpr := "0 1 * * *"

	return query.GetItem{
		ID:                   42,
		Name:                 "legacy-report",
		TenantID:             "tenant-a",
		Version:              4,
		TriggerType:          domainjob.TriggerTypeCron,
		CronExpr:             &cronExpr,
		Timezone:             "Asia/Shanghai",
		HandlerType:          "http",
		HandlerPayload:       map[string]any{"url": "https://example.com/legacy"},
		TimeoutSec:           300,
		RetryLimit:           5,
		RetryBackoffSec:      20,
		RetryBackoffStrategy: domainjob.RetryBackoffExponential,
		ConcurrencyPolicy:    domainjob.ConcurrencyForbid,
		MisfirePolicy:        domainjob.MisfireFireNow,
		Status:               "active",
	}
}

func TestHandler_RegisterAndUpdateJob(t *testing.T) {
	gin.SetMode(gin.TestMode)

	createdAt := time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC)
	getUseCase := &stubGetJobUseCase{
		out: currentJobForUpdateTests(),
	}
	useCase := &stubUpdateJobUseCase{
		out: command.UpdateResult{
			ID:        42,
			Name:      "nightly-report",
			TenantID:  "tenant-a",
			Status:    "active",
			Version:   5,
			CreatedAt: createdAt,
			UpdatedAt: createdAt,
		},
	}

	handler := NewHandler(nil, nil, getUseCase, useCase, nil)
	router := gin.New()
	handler.Register(router)

	body := `{
		"version": 4,
		"name":"nightly-report"
	}`
	req := httptest.NewRequest(stdhttp.MethodPut, "/api/v1/jobs/42?tenant_id=tenant-a",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(actorIDHeader, "control-plane-user")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", stdhttp.StatusOK, resp.Code, resp.Body.String())
	}
	if !useCase.called {
		t.Fatalf("expected use case to be called")
	}
	if !getUseCase.called {
		t.Fatalf("expected get use case to be called")
	}
	if useCase.in.ID != 42 {
		t.Fatalf("expected id=%d, got %d", 42, useCase.in.ID)
	}
	if useCase.in.TenantID != "tenant-a" {
		t.Fatalf("expected tenant_id=%q, got %q", "tenant-a", useCase.in.TenantID)
	}
	if useCase.in.ChangedBy != "control-plane-user" {
		t.Fatalf("expected changed_by=%q, got %q", "control-plane-user", useCase.in.ChangedBy)
	}
	if useCase.in.Version != 4 {
		t.Fatalf("expected version=%d, got %d", 4, useCase.in.Version)
	}
	if useCase.in.Name != "nightly-report" {
		t.Fatalf("expected name=%q, got %q", "nightly-report", useCase.in.Name)
	}
	if useCase.in.TriggerType != domainjob.TriggerTypeCron {
		t.Fatalf("expected trigger_type=%q, got %q", domainjob.TriggerTypeCron, useCase.in.TriggerType)
	}
	if useCase.in.TimeoutSec != 300 {
		t.Fatalf("expected timeout_sec=%d, got %d", 300, useCase.in.TimeoutSec)
	}
	if useCase.in.RetryBackoffStrategy != domainjob.RetryBackoffExponential {
		t.Fatalf("expected retry_backoff_strategy=%q, got %q", domainjob.RetryBackoffExponential, useCase.in.RetryBackoffStrategy)
	}
	if useCase.in.ConcurrencyPolicy != domainjob.ConcurrencyForbid {
		t.Fatalf("expected concurrency_policy=%q, got %q", domainjob.ConcurrencyForbid, useCase.in.ConcurrencyPolicy)
	}
	if useCase.in.HandlerPayload["url"] != "https://example.com/legacy" {
		t.Fatalf("expected existing handler payload to be preserved")
	}

	var out command.UpdateResult
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.Version != 5 {
		t.Fatalf("expected response version=%d, got %d", 5, out.Version)
	}
}

func TestHandler_UpdateJob_BindError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	getUseCase := &stubGetJobUseCase{}
	useCase := &stubUpdateJobUseCase{}
	handler := NewHandler(nil, nil, getUseCase, useCase, nil)
	router := gin.New()
	handler.Register(router)

	req := httptest.NewRequest(stdhttp.MethodPut, "/api/v1/jobs/bad",
		bytes.NewBufferString(`{"version":1}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(actorIDHeader, "control-plane-user")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusBadRequest, resp.Code)
	}
	if useCase.called {
		t.Fatalf("expected use case not to be called on bind error")
	}
	if getUseCase.called {
		t.Fatalf("expected get use case not to be called on bind error")
	}
}

func TestHandler_UpdateJob_MissingActor(t *testing.T) {
	gin.SetMode(gin.TestMode)

	getUseCase := &stubGetJobUseCase{}
	useCase := &stubUpdateJobUseCase{}
	handler := NewHandler(nil, nil, getUseCase, useCase, nil)
	router := gin.New()
	handler.Register(router)

	body := `{
		"version": 1,
		"name":"demo-job",
		"trigger_type":"manual",
		"handler_type":"http"
	}`
	req := httptest.NewRequest(stdhttp.MethodPut, "/api/v1/jobs/42",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusBadRequest, resp.Code)
	}
	if useCase.called {
		t.Fatalf("expected use case not to be called when actor is missing")
	}
	if getUseCase.called {
		t.Fatalf("expected get use case not to be called when actor is missing")
	}
}

func TestHandler_UpdateJob_ValidationError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	getUseCase := &stubGetJobUseCase{
		out: currentJobForUpdateTests(),
	}
	useCase := &stubUpdateJobUseCase{
		err: &validation.Error{
			Field:   "cron_expr",
			Message: "is required for cron jobs",
		},
	}
	handler := NewHandler(nil, nil, getUseCase, useCase, nil)
	router := gin.New()
	handler.Register(router)

	body := `{
		"version": 1,
		"name":"demo-job",
		"trigger_type":"cron",
		"handler_type":"http"
	}`
	req := httptest.NewRequest(stdhttp.MethodPut, "/api/v1/jobs/42",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(actorIDHeader, "control-plane-user")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusBadRequest, resp.Code)
	}
}

func TestHandler_UpdateJob_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	getUseCase := &stubGetJobUseCase{
		err: &resource.NotFoundError{
			Resource: "job",
			ID:       42,
		},
	}
	useCase := &stubUpdateJobUseCase{}
	handler := NewHandler(nil, nil, getUseCase, useCase, nil)
	router := gin.New()
	handler.Register(router)

	body := `{
		"version": 1,
		"name":"demo-job",
		"trigger_type":"manual",
		"handler_type":"http"
	}`
	req := httptest.NewRequest(stdhttp.MethodPut, "/api/v1/jobs/42",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(actorIDHeader, "control-plane-user")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusNotFound {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusNotFound, resp.Code)
	}
	if useCase.called {
		t.Fatalf("expected update use case not to be called when current job is missing")
	}
}

func TestHandler_UpdateJob_Conflict(t *testing.T) {
	gin.SetMode(gin.TestMode)

	getUseCase := &stubGetJobUseCase{
		out: currentJobForUpdateTests(),
	}
	useCase := &stubUpdateJobUseCase{
		err: &resource.ConflictError{
			Resource: "job",
			Field:    "version",
			Message:  "stale job version",
		},
	}
	handler := NewHandler(nil, nil, getUseCase, useCase, nil)
	router := gin.New()
	handler.Register(router)

	body := `{
		"version": 1,
		"name":"demo-job",
		"trigger_type":"manual",
		"handler_type":"http"
	}`
	req := httptest.NewRequest(stdhttp.MethodPut, "/api/v1/jobs/42",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(actorIDHeader, "control-plane-user")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusConflict {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusConflict, resp.Code)
	}
}

func TestHandler_UpdateJob_InternalError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	getUseCase := &stubGetJobUseCase{
		out: currentJobForUpdateTests(),
	}
	useCase := &stubUpdateJobUseCase{
		err: errors.New("update job: db down"),
	}
	handler := NewHandler(nil, nil, getUseCase, useCase, nil)
	router := gin.New()
	handler.Register(router)

	body := `{
		"version": 1,
		"name":"demo-job",
		"trigger_type":"manual",
		"handler_type":"http"
	}`
	req := httptest.NewRequest(stdhttp.MethodPut, "/api/v1/jobs/42",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(actorIDHeader, "control-plane-user")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusInternalServerError, resp.Code)
	}
}
