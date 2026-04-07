package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	command "orbitjob/internal/admin/app/job/command"
	query "orbitjob/internal/admin/app/job/query"
	adminhttp "orbitjob/internal/admin/http"
	domainjob "orbitjob/internal/core/domain/job"

	"github.com/gin-gonic/gin"
)

type stubCreateJobUseCase struct {
	called bool
	in     command.CreateInput
	out    command.CreateResult
	err    error
}

func (s *stubCreateJobUseCase) Create(ctx context.Context, in command.CreateInput) (command.CreateResult, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

type stubListJobsUseCase struct {
	called bool
	in     query.ListInput
	out    []query.ListItem
	err    error
}

func (s *stubListJobsUseCase) List(ctx context.Context, in query.ListInput) ([]query.ListItem, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

type stubGetJobUseCase struct {
	called bool
	in     query.GetInput
	out    query.GetItem
	err    error
}

func (s *stubGetJobUseCase) Get(ctx context.Context, in query.GetInput) (query.GetItem, error) {
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
		out: command.CreateResult{
			ID:       1,
			Name:     "demo-job",
			TenantID: "default",
			Status:   "active",
		},
	}

	handler := adminhttp.NewHandler(createUC, nil, nil, nil, nil)
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
		out: []query.ListItem{
			{
				ID:              1,
				Name:            "demo-job",
				TenantID:        "default",
				TriggerType:     domainjob.TriggerTypeManual,
				ScheduleSummary: "manual",
				HandlerType:     "http",
				Status:          query.StatusActive,
			},
		},
	}

	handler := adminhttp.NewHandler(nil, listUC, nil, nil, nil)
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
	if listUC.in.Status != query.StatusActive {
		t.Fatalf("expected status=%q, got %q", query.StatusActive, listUC.in.Status)
	}

	var out struct {
		Items []query.ListItem `json:"items"`
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

func TestNewRouter_GetJobRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)

	getUC := &stubGetJobUseCase{
		out: query.GetItem{
			ID:              1,
			Name:            "demo-job",
			TenantID:        "default",
			Version:         1,
			TriggerType:     domainjob.TriggerTypeManual,
			ScheduleSummary: "manual",
			HandlerType:     "http",
			Status:          query.StatusActive,
		},
	}

	handler := adminhttp.NewHandler(nil, nil, getUC, nil, nil)
	router := newRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/1?tenant_id=default", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", http.StatusOK, resp.Code, resp.Body.String())
	}
	if !getUC.called {
		t.Fatalf("expected get use case to be called")
	}
	if getUC.in.ID != 1 {
		t.Fatalf("expected id=%d, got %d", 1, getUC.in.ID)
	}
	if getUC.in.TenantID != "default" {
		t.Fatalf("expected tenant_id=%q, got %q", "default", getUC.in.TenantID)
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
