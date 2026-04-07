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
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
)

type stubChangeStatusUseCase struct {
	pauseCalled  bool
	resumeCalled bool
	in           command.ChangeStatusInput
	out          command.ChangeStatusResult
	err          error
}

func (s *stubChangeStatusUseCase) Pause(
	ctx context.Context,
	in command.ChangeStatusInput,
) (command.ChangeStatusResult, error) {
	s.pauseCalled = true
	s.in = in
	return s.out, s.err
}

func (s *stubChangeStatusUseCase) Resume(
	ctx context.Context,
	in command.ChangeStatusInput,
) (command.ChangeStatusResult, error) {
	s.resumeCalled = true
	s.in = in
	return s.out, s.err
}

func TestHandler_RegisterAndPauseJob(t *testing.T) {
	gin.SetMode(gin.TestMode)

	useCase := &stubChangeStatusUseCase{
		out: command.ChangeStatusResult{
			ID:        42,
			Name:      "nightly-report",
			TenantID:  "tenant-a",
			Status:    "paused",
			Version:   5,
			UpdatedAt: time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC),
		},
	}
	handler := NewHandler(nil, nil, nil, nil, useCase)
	router := gin.New()
	handler.Register(router)

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/jobs/42/pause?tenant_id=tenant-a",
		bytes.NewBufferString(`{"version":4}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(actorIDHeader, "control-plane-user")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", stdhttp.StatusOK, resp.Code, resp.Body.String())
	}
	if !useCase.pauseCalled {
		t.Fatal("expected Pause to be called")
	}
	if useCase.in.ID != 42 || useCase.in.TenantID != "tenant-a" || useCase.in.Version != 4 {
		t.Fatalf("unexpected input: %+v", useCase.in)
	}
	if useCase.in.ChangedBy != "control-plane-user" {
		t.Fatalf("expected changed_by=%q, got %q", "control-plane-user", useCase.in.ChangedBy)
	}

	var out command.ChangeStatusResult
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.Status != "paused" {
		t.Fatalf("expected status=%q, got %q", "paused", out.Status)
	}
}

func TestHandler_RegisterAndResumeJob(t *testing.T) {
	gin.SetMode(gin.TestMode)

	useCase := &stubChangeStatusUseCase{
		out: command.ChangeStatusResult{
			ID:      42,
			Status:  "active",
			Version: 6,
		},
	}
	handler := NewHandler(nil, nil, nil, nil, useCase)
	router := gin.New()
	handler.Register(router)

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/jobs/42/resume?tenant_id=tenant-a",
		bytes.NewBufferString(`{"version":5}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(actorIDHeader, "control-plane-user")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", stdhttp.StatusOK, resp.Code, resp.Body.String())
	}
	if !useCase.resumeCalled {
		t.Fatal("expected Resume to be called")
	}
}

func TestHandler_ChangeJobStatus_MissingActor(t *testing.T) {
	gin.SetMode(gin.TestMode)

	useCase := &stubChangeStatusUseCase{}
	handler := NewHandler(nil, nil, nil, nil, useCase)
	router := gin.New()
	handler.Register(router)

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/jobs/42/pause",
		bytes.NewBufferString(`{"version":4}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusBadRequest, resp.Code)
	}
	if useCase.pauseCalled || useCase.resumeCalled {
		t.Fatal("expected use case not to be called")
	}
}

func TestHandler_ChangeJobStatus_Conflict(t *testing.T) {
	gin.SetMode(gin.TestMode)

	useCase := &stubChangeStatusUseCase{
		err: &resource.ConflictError{
			Resource: "job",
			Field:    "version",
			Message:  "stale job version",
		},
	}
	handler := NewHandler(nil, nil, nil, nil, useCase)
	router := gin.New()
	handler.Register(router)

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/jobs/42/pause",
		bytes.NewBufferString(`{"version":4}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(actorIDHeader, "control-plane-user")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusConflict {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusConflict, resp.Code)
	}
}

func TestHandler_ChangeJobStatus_ValidationError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	useCase := &stubChangeStatusUseCase{
		err: &validation.Error{
			Field:   "status",
			Message: "only active jobs can be paused",
		},
	}
	handler := NewHandler(nil, nil, nil, nil, useCase)
	router := gin.New()
	handler.Register(router)

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/jobs/42/pause",
		bytes.NewBufferString(`{"version":4}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(actorIDHeader, "control-plane-user")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusBadRequest, resp.Code)
	}
}

func TestHandler_ChangeJobStatus_InternalError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	useCase := &stubChangeStatusUseCase{
		err: errors.New("change job status: db down"),
	}
	handler := NewHandler(nil, nil, nil, nil, useCase)
	router := gin.New()
	handler.Register(router)

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/jobs/42/pause",
		bytes.NewBufferString(`{"version":4}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(actorIDHeader, "control-plane-user")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusInternalServerError, resp.Code)
	}
}
