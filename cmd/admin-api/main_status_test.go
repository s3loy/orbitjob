package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	command "orbitjob/internal/admin/app/job/command"
	adminhttp "orbitjob/internal/admin/http"

	"github.com/gin-gonic/gin"
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

func TestNewRouter_PauseJobRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)

	statusUC := &stubChangeStatusUseCase{
		out: command.ChangeStatusResult{
			ID:      42,
			Status:  "paused",
			Version: 5,
		},
	}

	handler := adminhttp.NewHandler(nil, nil, nil, nil, statusUC)
	router := newRouter(handler)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/42/pause?tenant_id=tenant-a",
		bytes.NewBufferString(`{"version":4}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Actor-ID", "control-plane-user")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", http.StatusOK, resp.Code, resp.Body.String())
	}
	if !statusUC.pauseCalled {
		t.Fatal("expected Pause to be called")
	}
}

func TestNewRouter_ResumeJobRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)

	statusUC := &stubChangeStatusUseCase{
		out: command.ChangeStatusResult{
			ID:      42,
			Status:  "active",
			Version: 6,
		},
	}

	handler := adminhttp.NewHandler(nil, nil, nil, nil, statusUC)
	router := newRouter(handler)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/42/resume?tenant_id=tenant-a",
		bytes.NewBufferString(`{"version":5}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Actor-ID", "control-plane-user")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", http.StatusOK, resp.Code, resp.Body.String())
	}
	if !statusUC.resumeCalled {
		t.Fatal("expected Resume to be called")
	}
}
