package http

import (
	"bytes"
	"encoding/json"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	command "orbitjob/internal/admin/app/job/command"
	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
)

func TestHandler_UpdateJob_WorksWithoutQueryTenantID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	getUseCase := &stubGetJobUseCase{
		out: currentJobForUpdateTests(),
	}
	updateUseCase := &stubUpdateJobUseCase{
		out: command.UpdateResult{
			ID:      42,
			Name:    "updated-job",
			Status:  "active",
			Version: 5,
		},
	}
	handler := NewHandler(nil, nil, getUseCase, updateUseCase, nil)
	router := gin.New()
	router.Use(testTenantMiddleware("tenant-a"))
	handler.Register(router)

	req := httptest.NewRequest(stdhttp.MethodPut,
		"/api/v1/jobs/42",
		bytes.NewBufferString(`{"version":4,"name":"updated-job"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(actorIDHeader, "control-plane-user")

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", stdhttp.StatusOK, resp.Code, resp.Body.String())
	}
	if !getUseCase.called || !updateUseCase.called {
		t.Fatalf("expected both use cases to be called")
	}
	if updateUseCase.in.TenantID != "tenant-a" {
		t.Fatalf("expected tenant_id=%q, got %q", "tenant-a", updateUseCase.in.TenantID)
	}
}

func TestHandler_UpdateJob_CurrentJobValidationError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	getUseCase := &stubGetJobUseCase{
		err: validation.New("tenant_id", "must be <= 64 characters"),
	}
	updateUseCase := &stubUpdateJobUseCase{}
	handler := NewHandler(nil, nil, getUseCase, updateUseCase, nil)
	router := gin.New()
	router.Use(testTenantMiddleware("tenant-a"))
	handler.Register(router)

	req := httptest.NewRequest(stdhttp.MethodPut,
		"/api/v1/jobs/42",
		bytes.NewBufferString(`{"version":1}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(actorIDHeader, "control-plane-user")

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusBadRequest, resp.Code)
	}
	if updateUseCase.called {
		t.Fatalf("expected update use case not to be called when current job lookup fails validation")
	}
}

func TestHandler_UpdateJob_CurrentJobInternalError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	getUseCase := &stubGetJobUseCase{err: errors.New("query job detail: db down")}
	updateUseCase := &stubUpdateJobUseCase{}
	handler := NewHandler(nil, nil, getUseCase, updateUseCase, nil)
	router := gin.New()
	router.Use(testTenantMiddleware("tenant-a"))
	handler.Register(router)

	req := httptest.NewRequest(stdhttp.MethodPut,
		"/api/v1/jobs/42",
		bytes.NewBufferString(`{"version":1}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(actorIDHeader, "control-plane-user")

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusInternalServerError, resp.Code)
	}
	if updateUseCase.called {
		t.Fatalf("expected update use case not to be called when current job lookup fails internally")
	}
}

func TestHandler_UpdateJob_UpdateUseCaseNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	getUseCase := &stubGetJobUseCase{out: currentJobForUpdateTests()}
	updateUseCase := &stubUpdateJobUseCase{
		err: &resource.NotFoundError{Resource: "job", ID: 42},
	}
	handler := NewHandler(nil, nil, getUseCase, updateUseCase, nil)
	router := gin.New()
	router.Use(testTenantMiddleware("tenant-a"))
	handler.Register(router)

	req := httptest.NewRequest(stdhttp.MethodPut,
		"/api/v1/jobs/42",
		bytes.NewBufferString(`{"version":1}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(actorIDHeader, "control-plane-user")

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusNotFound {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusNotFound, resp.Code)
	}
}

func TestHandler_ChangeJobStatus_WorksWithoutQueryTenantID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	statusUseCase := &stubChangeStatusUseCase{
		out: command.ChangeStatusResult{
			ID:      42,
			Name:    "demo-job",
			Status:  "paused",
			Version: 2,
		},
	}
	handler := NewHandler(nil, nil, nil, nil, statusUseCase)
	router := gin.New()
	router.Use(testTenantMiddleware("tenant-a"))
	handler.Register(router)

	req := httptest.NewRequest(stdhttp.MethodPost,
		"/api/v1/jobs/42/pause",
		bytes.NewBufferString(`{"version":1}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(actorIDHeader, "control-plane-user")

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", stdhttp.StatusOK, resp.Code, resp.Body.String())
	}
	if !statusUseCase.pauseCalled {
		t.Fatal("expected Pause to be called")
	}
	if statusUseCase.in.TenantID != "tenant-a" {
		t.Fatalf("expected tenant_id=%q, got %q", "tenant-a", statusUseCase.in.TenantID)
	}
}

func TestHandler_ChangeJobStatus_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	statusUseCase := &stubChangeStatusUseCase{
		err: &resource.NotFoundError{Resource: "job", ID: 42},
	}
	handler := NewHandler(nil, nil, nil, nil, statusUseCase)
	router := gin.New()
	router.Use(testTenantMiddleware("tenant-a"))
	handler.Register(router)

	req := httptest.NewRequest(stdhttp.MethodPost,
		"/api/v1/jobs/42/pause",
		bytes.NewBufferString(`{"version":1}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(actorIDHeader, "control-plane-user")

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusNotFound {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusNotFound, resp.Code)
	}
}

func TestHandler_ChangeJobStatus_UnsupportedAction(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewHandler(nil, nil, nil, nil, &stubChangeStatusUseCase{})

	resp := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(resp)
	ctx.Request = httptest.NewRequest(stdhttp.MethodPost,
		"/api/v1/jobs/42/pause",
		bytes.NewBufferString(`{"version":1}`),
	)
	ctx.Request = ctx.Request.WithContext(
		middleware.WithTenantID(ctx.Request.Context(), "tenant-a", middleware.TenantSourceHeader),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Request.Header.Set(actorIDHeader, "control-plane-user")
	ctx.Params = gin.Params{{Key: "id", Value: "42"}}

	handler.changeJobStatus(ctx, "unsupported")

	if resp.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusInternalServerError, resp.Code)
	}

	var out struct {
		Error struct {
			Code  string `json:"code"`
			Field string `json:"field"`
		} `json:"error"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.Error.Code != string(ErrCodeValidation) {
		t.Fatalf("expected validation error code, got %q", out.Error.Code)
	}
	if out.Error.Field != "action" {
		t.Fatalf("expected field=action, got %q", out.Error.Field)
	}
}
