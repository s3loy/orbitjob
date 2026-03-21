package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"orbitjob/internal/job"
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

func TestHandler_RegisterAndCreateJob(t *testing.T) {
	gin.SetMode(gin.TestMode)

	createdAt := time.Date(2026, 3, 22, 1, 0, 0, 0, time.UTC)
	useCase := &stubCreateJobUseCase{
		out: job.Job{
			ID:        1,
			Name:      "demo-job",
			TenantID:  "default",
			Status:    "active",
			CreatedAt: createdAt,
			UpdatedAt: createdAt,
		},
	}

	handler := NewHandler(useCase)
	router := gin.New()
	handler.Register(router)

	body := `{
		"name":"demo-job",
		"trigger_type":"manual",
		"handler_type":"http",
		"handler_payload":{"url":"https://example.com/hook"}
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("expected status=%d, got %d, body=%s", http.StatusCreated, resp.Code, resp.Body.String())
	}
	if !useCase.called {
		t.Fatalf("expected use case to be called")
	}
	if useCase.in.Name != "demo-job" {
		t.Fatalf("expected input name=%q, got %q", "demo-job", useCase.in.Name)
	}
	if useCase.in.TriggerType != job.TriggerTypeManual {
		t.Fatalf("expected input trigger_type=%q, got %q", job.TriggerTypeManual, useCase.in.TriggerType)
	}
	if useCase.in.HandlerType != "http" {
		t.Fatalf("expected input handler_type=%q, got %q", "http", useCase.in.HandlerType)
	}

	var out job.Job
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.ID != useCase.out.ID {
		t.Fatalf("expected response id=%d, got %d", useCase.out.ID, out.ID)
	}
	if out.Name != useCase.out.Name {
		t.Fatalf("expected response name=%q, got %q", useCase.out.Name, out.Name)
	}
}

func TestHandler_CreateJob_BindError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	useCase := &stubCreateJobUseCase{}
	handler := NewHandler(useCase)
	router := gin.New()
	handler.Register(router)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", bytes.NewBufferString(`{"trigger_type":"manual"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status=%d, got %d", http.StatusBadRequest, resp.Code)
	}
	if useCase.called {
		t.Fatalf("expected use case not to be called on bind error")
	}
}

func TestHandler_CreateJob_UseCaseError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	useCase := &stubCreateJobUseCase{
		err: errors.New("invalid cron_expr"),
	}
	handler := NewHandler(useCase)
	router := gin.New()
	handler.Register(router)

	body := `{
		"name":"demo-job",
		"trigger_type":"manual",
		"handler_type":"http"
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status=%d, got %d", http.StatusBadRequest, resp.Code)
	}
	if !useCase.called {
		t.Fatalf("expected use case to be called")
	}
}
