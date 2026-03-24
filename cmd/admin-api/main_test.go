package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpapi "orbitjob/internal/admin/transport/http"
	domainjob "orbitjob/internal/domain/job"
	"orbitjob/internal/job"

	"github.com/gin-gonic/gin"
)

type stubCreateJobUseCase struct {
	called bool
	in     domainjob.CreateInput
	out    job.Job
	err    error
}

func (s *stubCreateJobUseCase) Create(ctx context.Context, in domainjob.CreateInput) (job.Job, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

type stubListJobsUseCase struct {
	called bool
	in     job.ListJobsQuery
	out    []job.JobListItem
	err    error
}

func (s *stubListJobsUseCase) List(ctx context.Context, in job.ListJobsQuery) ([]job.JobListItem, error) {
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

	createUC := &stubCreateJobUseCase{
		out: job.Job{
			ID:       1,
			Name:     "demo-job",
			TenantID: "default",
			Status:   "active",
		},
	}

	handler := httpapi.NewHandler(createUC, nil)
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
	if !createUC.called {
		t.Fatalf("expected use case to be called")
	}
	if createUC.in.Name != "demo-job" {
		t.Fatalf("expected input name=%q, got %q", "demo-job", createUC.in.Name)
	}
	if createUC.in.TriggerType != domainjob.TriggerTypeManual {
		t.Fatalf("expected input trigger_type=%q, got %q", domainjob.TriggerTypeManual,
			createUC.in.TriggerType)
	}
}

func TestNewRouter_ListJobsRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)

	listUC := &stubListJobsUseCase{
		out: []job.JobListItem{
			{
				ID:              1,
				Name:            "demo-job",
				TenantID:        "default",
				TriggerType:     job.TriggerTypeManual,
				ScheduleSummary: "manual",
				HandlerType:     "http",
				Status:          job.JobStatusActive,
			},
		},
	}

	handler := httpapi.NewHandler(nil, listUC)
	router := newRouter(handler)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/jobs?tenant_id=default&status=active&limit=10&offset=0", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", http.StatusOK, resp.Code,
			resp.Body.String())
	}
	if !listUC.called {
		t.Fatalf("expected list use case to be called")
	}
	if listUC.in.TenantID != "default" {
		t.Fatalf("expected tenant_id=%q, got %q", "default", listUC.in.TenantID)
	}
	if listUC.in.Status != job.JobStatusActive {
		t.Fatalf("expected status=%q, got %q", job.JobStatusActive, listUC.in.Status)
	}

	var out struct {
		Items []job.JobListItem `json:"items"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(out.Items))
	}
	if out.Items[0].ID != 1 {
		t.Fatalf("expected item id=1, got %d", out.Items[0].ID)
	}
}

func TestTraceMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(traceMiddleware())
	router.GET("/test", func(c *gin.Context) {
		traceID, _ := c.Get("trace_id")
		c.JSON(http.StatusOK, gin.H{"trace_id": traceID})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}

	var out struct {
		TraceID string `json:"trace_id"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if out.TraceID == "" {
		t.Fatal("trace_id should not be empty")
	}

	if resp.Header().Get("X-Trace-ID") == "" {
		t.Fatal("X-Trace-ID header should be set in response")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req2.Header.Set("X-Trace-ID", "my-trace-123")
	resp2 := httptest.NewRecorder()
	router.ServeHTTP(resp2, req2)

	var out2 struct {
		TraceID string `json:"trace_id"`
	}
	if err := json.Unmarshal(resp2.Body.Bytes(), &out2); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if out2.TraceID != "my-trace-123" {
		t.Fatalf("expected trace_id=my-trace-123, got %s", out2.TraceID)
	}

	if resp2.Header().Get("X-Trace-ID") != "my-trace-123" {
		t.Fatalf("expected X-Trace-ID=my-trace-123, got %s", resp2.Header().Get("X-Trace-ID"))
	}
}
