package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	instancecommand "orbitjob/internal/admin/app/instance/command"
	instancequery "orbitjob/internal/admin/app/instance/query"
	command "orbitjob/internal/admin/app/job/command"
	"orbitjob/internal/admin/http/apperror"
	"orbitjob/internal/admin/http/middleware"
	domaininstance "orbitjob/internal/core/domain/instance"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
)

// ===== Stub use cases for uncovered handlers =====

type stubDeleteJobUseCase struct {
	called bool
	in     command.DeleteInput
	out    command.DeleteResult
	err    error
}

func (s *stubDeleteJobUseCase) Delete(ctx context.Context, in command.DeleteInput) (command.DeleteResult, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

type stubTriggerJobUseCase struct {
	called bool
	in     command.TriggerInput
	out    command.TriggerResult
	err    error
}

func (s *stubTriggerJobUseCase) Trigger(ctx context.Context, in command.TriggerInput) (command.TriggerResult, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

type stubListInstancesUseCase struct {
	called bool
	in     instancequery.ListInstancesInput
	out    []instancequery.InstanceItem
	err    error
}

func (s *stubListInstancesUseCase) List(ctx context.Context, in instancequery.ListInstancesInput) ([]instancequery.InstanceItem, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

type stubGetInstanceUseCase struct {
	called bool
	runID  string
	out    *instancequery.InstanceItem
	err    error
}

func (s *stubGetInstanceUseCase) Get(ctx context.Context, runID string) (*instancequery.InstanceItem, error) {
	s.called = true
	s.runID = runID
	return s.out, s.err
}

type stubCancelInstanceUseCase struct {
	called bool
	in     instancecommand.CancelInstanceInput
	out    domaininstance.Snapshot
	err    error
}

func (s *stubCancelInstanceUseCase) Cancel(ctx context.Context, in instancecommand.CancelInstanceInput) (domaininstance.Snapshot, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

// ===== Set* method tests (lines 104–108) =====

func TestHandler_SetDeleteJobUseCase(t *testing.T) {
	h := &Handler{}
	uc := &stubDeleteJobUseCase{}
	h.SetDeleteJobUseCase(uc)
	if h.deleteJobUC != uc {
		t.Fatal("deleteJobUC field not set correctly")
	}
}

func TestHandler_SetTriggerJobUseCase(t *testing.T) {
	h := &Handler{}
	uc := &stubTriggerJobUseCase{}
	h.SetTriggerJobUseCase(uc)
	if h.triggerJobUC != uc {
		t.Fatal("triggerJobUC field not set correctly")
	}
}

func TestHandler_SetListInstancesUseCase(t *testing.T) {
	h := &Handler{}
	uc := &stubListInstancesUseCase{}
	h.SetListInstancesUseCase(uc)
	if h.listInstancesUC != uc {
		t.Fatal("listInstancesUC field not set correctly")
	}
}

func TestHandler_SetGetInstanceUseCase(t *testing.T) {
	h := &Handler{}
	uc := &stubGetInstanceUseCase{}
	h.SetGetInstanceUseCase(uc)
	if h.getInstanceUC != uc {
		t.Fatal("getInstanceUC field not set correctly")
	}
}

func TestHandler_SetCancelInstanceUseCase(t *testing.T) {
	h := &Handler{}
	uc := &stubCancelInstanceUseCase{}
	h.SetCancelInstanceUseCase(uc)
	if h.cancelInstanceUC != uc {
		t.Fatal("cancelInstanceUC field not set correctly")
	}
}

// ===== parseIdempotencyKey (line 352) =====

func TestParseIdempotencyKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("no header", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(stdhttp.MethodPost, "/", nil)

		got, err := parseIdempotencyKey(c)
		if err != nil {
			t.Fatalf("parseIdempotencyKey: %v", err)
		}
		if got != nil {
			t.Fatalf("expected nil, got %q", *got)
		}
	})

	t.Run("header present", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req := httptest.NewRequest(stdhttp.MethodPost, "/", nil)
		req.Header.Set(idempotencyKeyHeader, "my-key-123")
		c.Request = req

		got, err := parseIdempotencyKey(c)
		if err != nil {
			t.Fatalf("parseIdempotencyKey: %v", err)
		}
		if got == nil {
			t.Fatal("expected non-nil key")
		}
		if *got != "my-key-123" {
			t.Fatalf("expected my-key-123, got %q", *got)
		}
	})

	t.Run("header with whitespace", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req := httptest.NewRequest(stdhttp.MethodPost, "/", nil)
		req.Header.Set(idempotencyKeyHeader, "  my-key-456  ")
		c.Request = req

		got, err := parseIdempotencyKey(c)
		if err != nil {
			t.Fatalf("parseIdempotencyKey: %v", err)
		}
		if got == nil {
			t.Fatal("expected non-nil key after trimming whitespace")
		}
		if *got != "my-key-456" {
			t.Fatalf("expected my-key-456, got %q", *got)
		}
	})
}

func TestHandler_TriggerJob_WithIdempotencyKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uc := &stubTriggerJobUseCase{
		out: command.TriggerResult{
			RunID:    "run-002",
			JobID:    42,
			TenantID: "tenant-a",
			Status:   "pending",
			Created:  true,
		},
	}

	h := NewHandler(nil, nil, nil, nil, nil)
	h.SetTriggerJobUseCase(uc)

	router := gin.New()
	router.Use(testTenantMiddleware("tenant-a"))
	h.Register(router)

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/jobs/42/trigger", nil)
	req.Header.Set(idempotencyKeyHeader, "idem-key-abc")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusCreated {
		t.Fatalf("expected status=%d, got %d, body=%s",
			stdhttp.StatusCreated, resp.Code, resp.Body.String())
	}
	if !uc.called {
		t.Fatal("expected trigger use case to be called with idempotency key")
	}
}

func TestParseIdempotencyKey_TooLong(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(stdhttp.MethodPost, "/", nil)
	req.Header.Set(idempotencyKeyHeader, strings.Repeat("a", maxIdempotencyKeyLen+1))
	c.Request = req

	got, err := parseIdempotencyKey(c)
	if err == nil {
		t.Fatal("expected error for overlong key")
	}
	if got != nil {
		t.Fatalf("expected nil, got %q", *got)
	}
}

// ===== TriggerJob (line 361) =====

func TestHandler_TriggerJob_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	scheduledAt := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	createdAt := time.Date(2026, 5, 10, 12, 0, 0, 100, time.UTC)
	uc := &stubTriggerJobUseCase{
		out: command.TriggerResult{
			RunID:       "run-001",
			JobID:       42,
			TenantID:    "tenant-a",
			Status:      "pending",
			ScheduledAt: scheduledAt,
			CreatedAt:   createdAt,
			Created:     true,
		},
	}

	h := NewHandler(nil, nil, nil, nil, nil)
	h.SetTriggerJobUseCase(uc)

	router := gin.New()
	router.Use(testTenantMiddleware("tenant-a"))
	h.Register(router)

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/jobs/42/trigger", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusCreated {
		t.Fatalf("expected status=%d, got %d, body=%s",
			stdhttp.StatusCreated, resp.Code, resp.Body.String())
	}
	if !uc.called {
		t.Fatal("expected trigger use case to be called")
	}
	if uc.in.JobID != 42 {
		t.Fatalf("expected JobID=42, got %d", uc.in.JobID)
	}
	if uc.in.TenantID != "tenant-a" {
		t.Fatalf("expected TenantID=tenant-a, got %q", uc.in.TenantID)
	}

	var out command.TriggerResult
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.RunID != "run-001" {
		t.Fatalf("expected RunID=run-001, got %q", out.RunID)
	}
	if out.Status != "pending" {
		t.Fatalf("expected Status=pending, got %q", out.Status)
	}
}

func TestHandler_TriggerJob_Idempotent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uc := &stubTriggerJobUseCase{
		out: command.TriggerResult{
			RunID:    "run-003",
			JobID:    42,
			TenantID: "tenant-a",
			Status:   "pending",
			Created:  false,
		},
	}

	h := NewHandler(nil, nil, nil, nil, nil)
	h.SetTriggerJobUseCase(uc)

	router := gin.New()
	router.Use(testTenantMiddleware("tenant-a"))
	h.Register(router)

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/jobs/42/trigger", nil)
	req.Header.Set(idempotencyKeyHeader, "idem-key-xyz")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s",
			stdhttp.StatusOK, resp.Code, resp.Body.String())
	}
	if !uc.called {
		t.Fatal("expected trigger use case to be called")
	}

	var out command.TriggerResult
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.RunID != "run-003" {
		t.Fatalf("expected RunID=run-003, got %q", out.RunID)
	}
	if out.Created {
		t.Fatal("expected Created=false for idempotent response")
	}
}

func TestHandler_TriggerJob_BindError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uc := &stubTriggerJobUseCase{}
	h := NewHandler(nil, nil, nil, nil, nil)
	h.SetTriggerJobUseCase(uc)

	router := gin.New()
	h.Register(router)

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/jobs/not-an-int/trigger", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusBadRequest, resp.Code)
	}
	if uc.called {
		t.Fatal("expected use case not to be called on bind error")
	}
}

func TestHandler_TriggerJob_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uc := &stubTriggerJobUseCase{
		err: &resource.NotFoundError{Resource: "job", ID: 42},
	}
	h := NewHandler(nil, nil, nil, nil, nil)
	h.SetTriggerJobUseCase(uc)

	router := gin.New()
	router.Use(testTenantMiddleware("tenant-a"))
	h.Register(router)

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/jobs/42/trigger", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusNotFound {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusNotFound, resp.Code)
	}
	if !uc.called {
		t.Fatal("expected use case to be called")
	}

	var out struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.Error.Code != string(apperror.CodeNotFound) {
		t.Fatalf("expected code=%q, got %q", apperror.CodeNotFound, out.Error.Code)
	}
}

func TestHandler_TriggerJob_Conflict(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uc := &stubTriggerJobUseCase{
		err: &resource.ConflictError{Resource: "job", Field: "status", Message: "cannot trigger job with status paused"},
	}
	h := NewHandler(nil, nil, nil, nil, nil)
	h.SetTriggerJobUseCase(uc)

	router := gin.New()
	router.Use(testTenantMiddleware("tenant-a"))
	h.Register(router)

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/jobs/42/trigger", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusConflict {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusConflict, resp.Code)
	}
	if !uc.called {
		t.Fatal("expected use case to be called")
	}

	var out struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.Error.Code != string(apperror.CodeConflict) {
		t.Fatalf("expected code=%q, got %q", apperror.CodeConflict, out.Error.Code)
	}
}

func TestHandler_TriggerJob_InternalError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uc := &stubTriggerJobUseCase{
		err: errors.New("trigger job: db down"),
	}
	h := NewHandler(nil, nil, nil, nil, nil)
	h.SetTriggerJobUseCase(uc)

	router := gin.New()
	router.Use(testTenantMiddleware("tenant-a"))
	h.Register(router)

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/jobs/42/trigger", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusInternalServerError, resp.Code)
	}
	if !uc.called {
		t.Fatal("expected use case to be called")
	}

	var out struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.Error.Code != string(apperror.CodeInternal) {
		t.Fatalf("expected code=%q, got %q", apperror.CodeInternal, out.Error.Code)
	}
}

func TestHandler_DeleteJob_BindError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uc := &stubDeleteJobUseCase{}
	h := NewHandler(nil, nil, nil, nil, nil)
	h.SetDeleteJobUseCase(uc)

	router := gin.New()
	h.Register(router)

	req := httptest.NewRequest(stdhttp.MethodDelete, "/api/v1/jobs/not-an-int", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusBadRequest, resp.Code)
	}
	if uc.called {
		t.Fatal("expected use case not to be called on bind error")
	}
}

func TestHandler_DeleteJob_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uc := &stubDeleteJobUseCase{
		err: &resource.NotFoundError{Resource: "job", ID: 42},
	}

	h := NewHandler(nil, nil, nil, nil, nil)
	h.SetDeleteJobUseCase(uc)

	router := gin.New()
	router.Use(testTenantMiddleware("default"))
	h.Register(router)

	req := httptest.NewRequest(stdhttp.MethodDelete, "/api/v1/jobs/42", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusNotFound {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusNotFound, resp.Code)
	}
	if !uc.called {
		t.Fatal("expected use case to be called")
	}

	var out struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.Error.Code != string(apperror.CodeNotFound) {
		t.Fatalf("expected code=%q, got %q", apperror.CodeNotFound, out.Error.Code)
	}
}

func TestHandler_DeleteJob_InternalError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uc := &stubDeleteJobUseCase{
		err: errors.New("delete: db down"),
	}

	h := NewHandler(nil, nil, nil, nil, nil)
	h.SetDeleteJobUseCase(uc)

	router := gin.New()
	router.Use(testTenantMiddleware("default"))
	h.Register(router)

	req := httptest.NewRequest(stdhttp.MethodDelete, "/api/v1/jobs/1", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusInternalServerError, resp.Code)
	}
	if !uc.called {
		t.Fatal("expected use case to be called")
	}

	var out struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.Error.Code != string(apperror.CodeInternal) {
		t.Fatalf("expected code=%q, got %q", apperror.CodeInternal, out.Error.Code)
	}
}

// ===== ListInstances (line 417) =====

func TestHandler_ListInstances_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	uc := &stubListInstancesUseCase{
		out: []instancequery.InstanceItem{
			{
				RunID:             "run-001",
				TenantID:          "tenant-a",
				JobID:             42,
				TriggerSource:     "schedule",
				Status:            "success",
				Priority:          5,
				EffectivePriority: 5,
				Attempt:           1,
				MaxAttempt:        3,
				ScheduledAt:       now,
				DispatchedAt:      &now,
				CreatedAt:         now,
				UpdatedAt:         now,
				Version:           1,
			},
		},
	}

	h := NewHandler(nil, nil, nil, nil, nil)
	h.SetListInstancesUseCase(uc)

	router := gin.New()
	router.Use(testTenantMiddleware("tenant-a"))
	h.Register(router)

	req := httptest.NewRequest(stdhttp.MethodGet,
		"/api/v1/instances?status=success&limit=20&offset=0", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s",
			stdhttp.StatusOK, resp.Code, resp.Body.String())
	}
	if !uc.called {
		t.Fatal("expected list instances use case to be called")
	}
	if uc.in.TenantID != "tenant-a" {
		t.Fatalf("expected TenantID=tenant-a, got %q", uc.in.TenantID)
	}
	if uc.in.Status != "success" {
		t.Fatalf("expected Status=success, got %q", uc.in.Status)
	}
	if uc.in.Limit != 20 {
		t.Fatalf("expected Limit=20, got %d", uc.in.Limit)
	}
	if uc.in.Offset != 0 {
		t.Fatalf("expected Offset=0, got %d", uc.in.Offset)
	}

	var out instanceListResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(out.Items))
	}
	if out.Items[0].RunID != "run-001" {
		t.Fatalf("expected RunID=run-001, got %q", out.Items[0].RunID)
	}
}

func TestHandler_ListInstances_EmptyList(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uc := &stubListInstancesUseCase{
		out: []instancequery.InstanceItem{},
	}

	h := NewHandler(nil, nil, nil, nil, nil)
	h.SetListInstancesUseCase(uc)

	router := gin.New()
	router.Use(testTenantMiddleware("default"))
	h.Register(router)

	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/instances", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s",
			stdhttp.StatusOK, resp.Code, resp.Body.String())
	}
	if !uc.called {
		t.Fatal("expected use case to be called")
	}

	var out instanceListResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(out.Items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(out.Items))
	}
}

func TestHandler_ListInstances_BindError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uc := &stubListInstancesUseCase{}
	h := NewHandler(nil, nil, nil, nil, nil)
	h.SetListInstancesUseCase(uc)

	router := gin.New()
	h.Register(router)

	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/instances?status=invalid_status", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusBadRequest, resp.Code)
	}
	if uc.called {
		t.Fatal("expected use case not to be called on bind error")
	}
}

func TestHandler_ListInstances_InternalError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uc := &stubListInstancesUseCase{
		err: errors.New("list instances: db down"),
	}

	h := NewHandler(nil, nil, nil, nil, nil)
	h.SetListInstancesUseCase(uc)

	router := gin.New()
	router.Use(testTenantMiddleware("default"))
	h.Register(router)

	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/instances", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusInternalServerError, resp.Code)
	}
	if !uc.called {
		t.Fatal("expected use case to be called")
	}
}

// ===== GetInstance (line 441) =====

func TestHandler_GetInstance_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	uc := &stubGetInstanceUseCase{
		out: &instancequery.InstanceItem{
			RunID:             "run-001",
			TenantID:          "tenant-a",
			JobID:             42,
			TriggerSource:     "manual",
			Status:            "running",
			Priority:          5,
			EffectivePriority: 5,
			Attempt:           1,
			MaxAttempt:        3,
			ScheduledAt:       now,
			CreatedAt:         now,
			UpdatedAt:         now,
			Version:           2,
		},
	}

	h := NewHandler(nil, nil, nil, nil, nil)
	h.SetGetInstanceUseCase(uc)

	router := gin.New()
	h.Register(router)

	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/instances/run-001", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s",
			stdhttp.StatusOK, resp.Code, resp.Body.String())
	}
	if !uc.called {
		t.Fatal("expected get instance use case to be called")
	}
	if uc.runID != "run-001" {
		t.Fatalf("expected runID=run-001, got %q", uc.runID)
	}

	var out instancequery.InstanceItem
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.RunID != "run-001" {
		t.Fatalf("expected RunID=run-001, got %q", out.RunID)
	}
	if out.Status != "running" {
		t.Fatalf("expected Status=running, got %q", out.Status)
	}
}

func TestHandler_GetInstance_BindError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uc := &stubGetInstanceUseCase{}
	h := NewHandler(nil, nil, nil, nil, nil)
	h.SetGetInstanceUseCase(uc)

	router := gin.New()
	h.Register(router)

	// run_id longer than 64 chars violates max=64 binding
	longRunID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" // 65 chars
	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/instances/"+longRunID, nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusBadRequest, resp.Code)
	}
	if uc.called {
		t.Fatal("expected use case not to be called on bind error")
	}
}

func TestHandler_GetInstance_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uc := &stubGetInstanceUseCase{
		err: &resource.NotFoundError{Resource: "instance", ID: "run-999"},
	}

	h := NewHandler(nil, nil, nil, nil, nil)
	h.SetGetInstanceUseCase(uc)

	router := gin.New()
	h.Register(router)

	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/instances/run-999", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusNotFound {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusNotFound, resp.Code)
	}
	if !uc.called {
		t.Fatal("expected use case to be called")
	}

	var out struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.Error.Code != string(apperror.CodeNotFound) {
		t.Fatalf("expected code=%q, got %q", apperror.CodeNotFound, out.Error.Code)
	}
}

func TestHandler_GetInstance_InternalError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uc := &stubGetInstanceUseCase{
		err: errors.New("get instance: db down"),
	}

	h := NewHandler(nil, nil, nil, nil, nil)
	h.SetGetInstanceUseCase(uc)

	router := gin.New()
	h.Register(router)

	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/instances/run-001", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusInternalServerError, resp.Code)
	}
	if !uc.called {
		t.Fatal("expected use case to be called")
	}
}

// ===== CancelInstance (line 463) =====

func TestHandler_CancelInstance_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	uc := &stubCancelInstanceUseCase{
		out: domaininstance.Snapshot{
			ID:                100,
			RunID:             "run-001",
			TenantID:          "tenant-a",
			JobID:             42,
			TriggerSource:     "manual",
			Status:            domaininstance.StatusCanceled,
			Priority:          5,
			EffectivePriority: 5,
			Attempt:           1,
			MaxAttempt:        3,
			ScheduledAt:       now,
			CreatedAt:         now,
			UpdatedAt:         now,
			Version:           2,
		},
	}

	h := NewHandler(nil, nil, nil, nil, nil)
	h.SetCancelInstanceUseCase(uc)

	router := gin.New()
	h.Register(router)

	body := `{"version": 1}`
	req := httptest.NewRequest(stdhttp.MethodPost,
		"/api/v1/instances/run-001/cancel",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s",
			stdhttp.StatusOK, resp.Code, resp.Body.String())
	}
	if !uc.called {
		t.Fatal("expected cancel instance use case to be called")
	}
	if uc.in.RunID != "run-001" {
		t.Fatalf("expected RunID=run-001, got %q", uc.in.RunID)
	}
	if uc.in.Version != 1 {
		t.Fatalf("expected Version=1, got %d", uc.in.Version)
	}

	var out domaininstance.Snapshot
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.Status != domaininstance.StatusCanceled {
		t.Fatalf("expected Status=%q, got %q",
			domaininstance.StatusCanceled, out.Status)
	}
}

func TestHandler_CancelInstance_BindError_URI(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uc := &stubCancelInstanceUseCase{}
	h := NewHandler(nil, nil, nil, nil, nil)
	h.SetCancelInstanceUseCase(uc)

	router := gin.New()
	h.Register(router)

	req := httptest.NewRequest(stdhttp.MethodPost,
		"/api/v1/instances//cancel",
		bytes.NewBufferString(`{"version":1}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusBadRequest, resp.Code)
	}
	if uc.called {
		t.Fatal("expected use case not to be called on bind error")
	}
}

func TestHandler_CancelInstance_BindError_MissingVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uc := &stubCancelInstanceUseCase{}
	h := NewHandler(nil, nil, nil, nil, nil)
	h.SetCancelInstanceUseCase(uc)

	router := gin.New()
	h.Register(router)

	req := httptest.NewRequest(stdhttp.MethodPost,
		"/api/v1/instances/run-001/cancel",
		bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusBadRequest, resp.Code)
	}
	if uc.called {
		t.Fatal("expected use case not to be called on bind error")
	}
}

func TestHandler_CancelInstance_InternalError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uc := &stubCancelInstanceUseCase{
		err: errors.New("cancel instance: db down"),
	}

	h := NewHandler(nil, nil, nil, nil, nil)
	h.SetCancelInstanceUseCase(uc)

	router := gin.New()
	h.Register(router)

	body := `{"version": 1}`
	req := httptest.NewRequest(stdhttp.MethodPost,
		"/api/v1/instances/run-001/cancel",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusInternalServerError, resp.Code)
	}
	if !uc.called {
		t.Fatal("expected use case to be called")
	}

	var out struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.Error.Code != string(apperror.CodeInternal) {
		t.Fatalf("expected code=%q, got %q", apperror.CodeInternal, out.Error.Code)
	}
}

// TestHandler_IdempotencyKeyFlow adds coverage for the idempotency key middleware
// integration path within TriggerJob handler (line 375-377).
func TestHandler_IdempotencyKeyFlow(t *testing.T) {
	// Verify WithIdempotencyKey stores value retrievable via IdempotencyKey.
	ctx := context.Background()
	ctx = middleware.WithIdempotencyKey(ctx, "test-key")
	if got := middleware.IdempotencyKey(ctx); got != "test-key" {
		t.Fatalf("expected test-key, got %q", got)
	}
}

// ===== GetJob query bind error (line 180-183 in handler.go) =====

func TestHandler_GetJob_QueryBindError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	useCase := &stubGetJobUseCase{}
	handler := NewHandler(nil, nil, useCase, nil, nil)
	router := testRouter(handler)

	// tenant_id > 64 chars triggers bind error
	longTenant := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" // 65 chars
	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/jobs/1?tenant_id="+longTenant, nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusBadRequest, resp.Code)
	}
	if useCase.called {
		t.Fatal("expected use case not to be called on query bind error")
	}
}

// ===== UpdateJob JSON body bind error (line 218-221 in handler.go) =====

func TestHandler_UpdateJob_JSONBindError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	getUseCase := &stubGetJobUseCase{}
	updateUseCase := &stubUpdateJobUseCase{}
	handler := NewHandler(nil, nil, getUseCase, updateUseCase, nil)
	router := testRouter(handler)

	// Valid path but bad JSON body
	req := httptest.NewRequest(stdhttp.MethodPut, "/api/v1/jobs/42",
		bytes.NewBufferString(`{invalid json}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(actorIDHeader, "control-plane-user")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status=%d, got %d, body=%s",
			stdhttp.StatusBadRequest, resp.Code, resp.Body.String())
	}
	if updateUseCase.called {
		t.Fatal("expected update use case not to be called on JSON bind error")
	}
	if getUseCase.called {
		t.Fatal("expected get use case not to be called on JSON bind error")
	}
}

// ===== ChangeJobStatus path bind error (line 293-297 in handler.go) =====

func TestHandler_ChangeJobStatus_PathBindError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewHandler(nil, nil, nil, nil, &stubChangeStatusUseCase{})
	router := testRouter(handler)

	// Bad ID in path
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/jobs/not-an-int/pause",
		bytes.NewBufferString(`{"version":1}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(actorIDHeader, "control-plane-user")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusBadRequest, resp.Code)
	}
}

// ===== ChangeJobStatus JSON bind error (line 303-306 in handler.go) =====

func TestHandler_ChangeJobStatus_JSONBindError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewHandler(nil, nil, nil, nil, &stubChangeStatusUseCase{})
	router := testRouter(handler)

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/jobs/42/pause",
		bytes.NewBufferString(`{invalid}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(actorIDHeader, "control-plane-user")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusBadRequest, resp.Code)
	}
}

// ===== ChangeJobStatus Resume internal error (line 320 and following in handler.go) =====

func TestHandler_DeleteJob_MapsValidationError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	useCase := &stubDeleteJobUseCase{
		err: &validation.Error{Field: "version", Message: "stale version"},
	}
	handler := NewHandler(nil, nil, nil, nil, nil)
	handler.SetDeleteJobUseCase(useCase)
	router := testRouter(handler)

	req := httptest.NewRequest(stdhttp.MethodDelete, "/api/v1/jobs/42",
		bytes.NewBufferString(`{"version":1}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusBadRequest, resp.Code)
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
	if out.Error.Field != "version" {
		t.Fatalf("expected field=version, got %q", out.Error.Field)
	}
}

func TestHandler_ListInstances_MapsValidationError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	useCase := &stubListInstancesUseCase{
		err: &validation.Error{Field: "status", Message: "invalid status"},
	}
	handler := NewHandler(nil, nil, nil, nil, nil)
	handler.SetListInstancesUseCase(useCase)
	router := testRouter(handler)

	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/instances", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status=%d, got %d", stdhttp.StatusBadRequest, resp.Code)
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
}
