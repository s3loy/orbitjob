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

func TestHandler_RegisterAndUpdateJob(t *testing.T) {
	gin.SetMode(gin.TestMode)

	createdAt := time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC)
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

	handler := NewHandler(nil, nil, nil, useCase)
	router := gin.New()
	handler.Register(router)

	body := `{
		"version": 4,
		"name":"nightly-report",
		"trigger_type":"manual",
		"handler_type":"http"
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
	if useCase.in.TriggerType != domainjob.TriggerTypeManual {
		t.Fatalf("expected trigger_type=%q, got %q", domainjob.TriggerTypeManual, useCase.in.TriggerType)
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

	useCase := &stubUpdateJobUseCase{}
	handler := NewHandler(nil, nil, nil, useCase)
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
}

func TestHandler_UpdateJob_MissingActor(t *testing.T) {
	gin.SetMode(gin.TestMode)

	useCase := &stubUpdateJobUseCase{}
	handler := NewHandler(nil, nil, nil, useCase)
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
}

func TestHandler_UpdateJob_ValidationError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	useCase := &stubUpdateJobUseCase{
		err: &validation.Error{
			Field:   "cron_expr",
			Message: "is required for cron jobs",
		},
	}
	handler := NewHandler(nil, nil, nil, useCase)
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

	useCase := &stubUpdateJobUseCase{
		err: &resource.NotFoundError{
			Resource: "job",
			ID:       42,
		},
	}
	handler := NewHandler(nil, nil, nil, useCase)
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
}

func TestHandler_UpdateJob_Conflict(t *testing.T) {
	gin.SetMode(gin.TestMode)

	useCase := &stubUpdateJobUseCase{
		err: &resource.ConflictError{
			Resource: "job",
			Field:    "version",
			Message:  "stale job version",
		},
	}
	handler := NewHandler(nil, nil, nil, useCase)
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

	useCase := &stubUpdateJobUseCase{
		err: errors.New("update job: db down"),
	}
	handler := NewHandler(nil, nil, nil, useCase)
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
