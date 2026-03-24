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

	domainjob "orbitjob/internal/domain/job"
	"orbitjob/internal/job"
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

	handler := NewHandler(useCase, nil)
	router := gin.New()
	handler.Register(router)

	body := `{
                "name":"demo-job",
                "trigger_type":"manual",
                "handler_type":"http",
                "handler_payload":{"url":"https://example.com/hook"}
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
	if !useCase.called {
		t.Fatalf("expected use case to be called")
	}
	if useCase.in.Name != "demo-job" {
		t.Fatalf("expected input name=%q, got %q", "demo-job", useCase.in.Name)
	}
	if useCase.in.TriggerType != domainjob.TriggerTypeManual {
		t.Fatalf("expected input trigger_type=%q, got %q", domainjob.TriggerTypeManual,
			useCase.in.TriggerType)
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

func TestHandler_RegisterAndListJobs(t *testing.T) {
	gin.SetMode(gin.TestMode)

	nextRunAt := time.Date(2026, 3, 22, 2, 0, 0, 0, time.UTC)
	createdAt := time.Date(2026, 3, 22, 1, 0, 0, 0, time.UTC)
	useCase := &stubListJobsUseCase{
		out: []job.JobListItem{
			{
				ID:                1,
				Name:              "demo-job",
				TenantID:          "tenant-a",
				TriggerType:       job.TriggerTypeCron,
				ScheduleSummary:   "cron: */5 * * * * (UTC)",
				HandlerType:       "http",
				ConcurrencyPolicy: job.ConcurrencyAllow,
				MisfirePolicy:     job.MisfireSkip,
				Status:            job.JobStatusActive,
				NextRunAt:         &nextRunAt,
				CreatedAt:         createdAt,
				UpdatedAt:         createdAt,
			},
		},
	}

	handler := NewHandler(nil, useCase)
	router := gin.New()
	handler.Register(router)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/jobs?tenant_id=tenant-a&status=active&limit=20&offset=40", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", http.StatusOK, resp.Code,
			resp.Body.String())
	}
	if !useCase.called {
		t.Fatalf("expected use case to be called")
	}
	if useCase.in.TenantID != "tenant-a" {
		t.Fatalf("expected tenant_id=%q, got %q", "tenant-a", useCase.in.TenantID)
	}
	if useCase.in.Status != job.JobStatusActive {
		t.Fatalf("expected status=%q, got %q", job.JobStatusActive, useCase.in.Status)
	}
	if useCase.in.Limit != 20 {
		t.Fatalf("expected limit=%d, got %d", 20, useCase.in.Limit)
	}
	if useCase.in.Offset != 40 {
		t.Fatalf("expected offset=%d, got %d", 40, useCase.in.Offset)
	}

	var out jobListResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(out.Items))
	}
	if out.Items[0].ID != 1 {
		t.Fatalf("expected response id=%d, got %d", 1, out.Items[0].ID)
	}
	if out.Items[0].ScheduleSummary != "cron: */5 * * * * (UTC)" {
		t.Fatalf("expected schedule_summary=%q, got %q",
			"cron: */5 * * * * (UTC)", out.Items[0].ScheduleSummary)
	}
}

func TestHandler_CreateJob_BindError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	useCase := &stubCreateJobUseCase{}
	handler := NewHandler(useCase, nil)
	router := gin.New()
	handler.Register(router)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs",
		bytes.NewBufferString(`{"trigger_type":"manual"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status=%d, got %d", http.StatusBadRequest, resp.Code)
	}
	if useCase.called {
		t.Fatalf("expected use case not to be called on bind error")
	}

	var out struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Field   string `json:"field"`
		} `json:"error"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if out.Error.Code != "VALIDATION_ERROR" {
		t.Fatalf("expected code VALIDATION_ERROR, got %q", out.Error.Code)
	}
	if out.Error.Message == "" {
		t.Fatal("expected validation error message to be non-empty")
	}
	if out.Error.Code == "INTERNAL_ERROR" {
		t.Fatal("bind error must not be mapped to INTERNAL_ERROR")
	}
}

func TestHandler_ListJobs_BindError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	useCase := &stubListJobsUseCase{}
	handler := NewHandler(nil, useCase)
	router := gin.New()
	handler.Register(router)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs?limit=bad", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status=%d, got %d", http.StatusBadRequest, resp.Code)
	}
	if useCase.called {
		t.Fatalf("expected use case not to be called on bind error")
	}

	var out struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Field   string `json:"field"`
		} `json:"error"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if out.Error.Code != "VALIDATION_ERROR" {
		t.Fatalf("expected code VALIDATION_ERROR, got %q", out.Error.Code)
	}
	if out.Error.Message == "" {
		t.Fatal("expected validation error message to be non-empty")
	}
	if out.Error.Code == "INTERNAL_ERROR" {
		t.Fatal("bind error must not be mapped to INTERNAL_ERROR")
	}
}

func TestHandler_CreateJob_UseCaseError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	useCase := &stubCreateJobUseCase{
		err: &job.ValidationError{
			Field:   "cron_expr",
			Message: "invalid cron_expr",
		},
	}
	handler := NewHandler(useCase, nil)
	router := gin.New()
	handler.Register(router)

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

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status=%d, got %d", http.StatusBadRequest, resp.Code)
	}
	if !useCase.called {
		t.Fatalf("expected use case to be called")
	}
}

func TestHandler_ListJobs_UseCaseError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	useCase := &stubListJobsUseCase{
		err: &job.ValidationError{
			Field:   "tenant_id",
			Message: "must be <= 64 characters",
		},
	}
	handler := NewHandler(nil, useCase)
	router := gin.New()
	handler.Register(router)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status=%d, got %d", http.StatusBadRequest, resp.Code)
	}
	if !useCase.called {
		t.Fatalf("expected use case to be called")
	}
}

func TestHandler_CreateJob_InternalError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	useCase := &stubCreateJobUseCase{
		err: errors.New("insert job: db down"),
	}
	handler := NewHandler(useCase, nil)
	router := gin.New()
	handler.Register(router)

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

	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected status=%d, got %d", http.StatusInternalServerError, resp.Code)
	}
	if !useCase.called {
		t.Fatalf("expected use case to be called")
	}
}

func TestHandler_ListJobs_InternalError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	useCase := &stubListJobsUseCase{
		err: errors.New("query job list: db down"),
	}
	handler := NewHandler(nil, useCase)
	router := gin.New()
	handler.Register(router)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected status=%d, got %d", http.StatusInternalServerError, resp.Code)
	}
	if !useCase.called {
		t.Fatalf("expected use case to be called")
	}
}

func TestHandler_CreateJob_ValidationErrorResponseFormat(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uc := &stubCreateJobUseCase{
		err: &job.ValidationError{
			Field:   "cron_expr",
			Message: "is required for cron jobs",
		},
	}
	handler := NewHandler(uc, nil)
	router := gin.New()
	handler.Register(router)

	body := `{
                "name":"demo",
                "trigger_type":"cron",
                "handler_type":"http"
        }`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, resp.Code)
	}

	var out struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Field   string `json:"field"`
		} `json:"error"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if out.Error.Code != "VALIDATION_ERROR" {
		t.Fatalf("expected code VALIDATION_ERROR, got %q", out.Error.Code)
	}
	if out.Error.Field != "cron_expr" {
		t.Fatalf("expected field cron_expr, got %q", out.Error.Field)
	}
}

func TestHandler_CreateJob_InternalErrorResponseFormat(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uc := &stubCreateJobUseCase{
		err: errors.New("insert job: db down"),
	}
	handler := NewHandler(uc, nil)
	router := gin.New()
	handler.Register(router)

	body := `{
                "name":"demo",
                "trigger_type":"manual",
                "handler_type":"http"
        }`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, resp.Code)
	}

	var out struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if out.Error.Code != "INTERNAL_ERROR" {
		t.Fatalf("expected code INTERNAL_ERROR, got %q", out.Error.Code)
	}
	if out.Error.Message == "insert job: db down" {
		t.Fatalf("internal error message must not leak raw error: %q", out.Error.Message)
	}
}
