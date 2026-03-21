package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"orbitjob/internal/job"
	httpapi "orbitjob/internal/transport/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type stubCreateJobUseCase struct {
	called bool
	in     job.CreateJobInput
	out    job.Job
	err    error
}

func (s *stubCreateJobUseCase) Create(ctx context.Context, in job.CreateJobInput) (job.Job, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

func TestNewRouter_Healthz(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := newRouter(nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status=%d, got %d", http.StatusOK, resp.Code)
	}
	if !strings.Contains(resp.Body.String(), `"status":"ok"`) {
		t.Fatalf("expected healthz body, got %s", resp.Body.String())
	}
}

func TestNewRouter_CreateJobRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uc := &stubCreateJobUseCase{
		out: job.Job{
			ID:       1,
			Name:     "demo-job",
			TenantID: "default",
			Status:   "active",
		},
	}

	handler := httpapi.NewHandler(uc)
	router := newRouter(handler)

	body := `{
                "name":"demo-job",
                "trigger_type":"manual",
                "handler_type":"http"
        }`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("expected status=%d, got %d, body=%s", http.StatusCreated, resp.Code,
			resp.Body.String())
	}
	if !uc.called {
		t.Fatalf("expected use case to be called")
	}
	if uc.in.Name != "demo-job" {
		t.Fatalf("expected input name=%q, got %q", "demo-job", uc.in.Name)
	}
	if uc.in.TriggerType != job.TriggerTypeManual {
		t.Fatalf("expected input trigger_type=%q, got %q", job.TriggerTypeManual,
			uc.in.TriggerType)
	}
}
