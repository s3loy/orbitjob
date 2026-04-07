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

func TestNewRouter_UpdateJobRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)

	updateUC := &stubUpdateJobUseCase{
		out: command.UpdateResult{
			ID:      42,
			Name:    "nightly-report",
			Status:  "active",
			Version: 5,
		},
	}

	handler := adminhttp.NewHandler(nil, nil, nil, updateUC)
	router := newRouter(handler)

	body := `{
		"version": 4,
		"name":"nightly-report",
		"trigger_type":"manual",
		"handler_type":"http"
	}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/jobs/42?tenant_id=default",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Actor-ID", "control-plane-user")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", http.StatusOK, resp.Code, resp.Body.String())
	}
	if !updateUC.called {
		t.Fatalf("expected update use case to be called")
	}
	if updateUC.in.ID != 42 {
		t.Fatalf("expected id=%d, got %d", 42, updateUC.in.ID)
	}
	if updateUC.in.ChangedBy != "control-plane-user" {
		t.Fatalf("expected changed_by=%q, got %q", "control-plane-user", updateUC.in.ChangedBy)
	}
}
