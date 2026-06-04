package http

import (
	"bytes"
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	checkcommand "orbitjob/internal/admin/app/check/command"
	checkquery "orbitjob/internal/admin/app/check/query"
	checkrunquery "orbitjob/internal/admin/app/checkrun/query"
	"orbitjob/internal/domain/resource"
)

// ---------------------------------------------------------------------------
// stubs
// ---------------------------------------------------------------------------

type stubCreateCheckUseCase struct {
	called bool
	in     checkcommand.CreateInput
	out    checkcommand.CreateResult
	err    error
}

func (s *stubCreateCheckUseCase) Create(ctx context.Context, in checkcommand.CreateInput) (checkcommand.CreateResult, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

type stubListChecksUseCase struct {
	called bool
	in     checkquery.ListChecksInput
	out    checkquery.ListChecksResult
	err    error
}

func (s *stubListChecksUseCase) List(ctx context.Context, in checkquery.ListChecksInput) (checkquery.ListChecksResult, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

type stubGetCheckUseCase struct {
	called bool
	out    checkquery.GetResult
	err    error
}

func (s *stubGetCheckUseCase) Get(ctx context.Context, tenantID string, id int64) (checkquery.GetResult, error) {
	s.called = true
	return s.out, s.err
}

type stubPauseCheckUseCase struct {
	called bool
	in     checkcommand.ChangeStatusInput
	out    checkcommand.ChangeStatusResult
	err    error
}

func (s *stubPauseCheckUseCase) Pause(ctx context.Context, in checkcommand.ChangeStatusInput) (checkcommand.ChangeStatusResult, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

type stubResumeCheckUseCase struct {
	called bool
	in     checkcommand.ChangeStatusInput
	out    checkcommand.ChangeStatusResult
	err    error
}

func (s *stubResumeCheckUseCase) Resume(ctx context.Context, in checkcommand.ChangeStatusInput) (checkcommand.ChangeStatusResult, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

type stubDeleteCheckUseCase struct {
	called bool
	in     checkcommand.DeleteInput
	err    error
}

func (s *stubDeleteCheckUseCase) Delete(ctx context.Context, in checkcommand.DeleteInput) error {
	s.called = true
	s.in = in
	return s.err
}

type stubListCheckRunsUseCase struct {
	called bool
	in     checkrunquery.ListCheckRunsInput
	out    checkrunquery.ListCheckRunsResult
	err    error
}

func (s *stubListCheckRunsUseCase) List(ctx context.Context, in checkrunquery.ListCheckRunsInput) (checkrunquery.ListCheckRunsResult, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

type stubGetCheckRunUseCase struct {
	called bool
	out    checkrunquery.GetResult
	err    error
}

func (s *stubGetCheckRunUseCase) Get(ctx context.Context, tenantID string, id int64) (checkrunquery.GetResult, error) {
	s.called = true
	return s.out, s.err
}

func newCheckHandler(t *testing.T,
	createUC *stubCreateCheckUseCase,
	listUC *stubListChecksUseCase,
	getUC *stubGetCheckUseCase,
	pauseUC *stubPauseCheckUseCase,
	resumeUC *stubResumeCheckUseCase,
	deleteUC *stubDeleteCheckUseCase,
	listRunsUC *stubListCheckRunsUseCase,
	getRunUC *stubGetCheckRunUseCase,
) (*Handler, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := NewHandler(nil, nil, nil, nil, nil)
	if createUC != nil {
		h.SetCreateCheckUseCase(createUC)
	}
	if listUC != nil {
		h.SetListChecksUseCase(listUC)
	}
	if getUC != nil {
		h.SetGetCheckUseCase(getUC)
	}
	if pauseUC != nil {
		h.SetPauseCheckUseCase(pauseUC)
	}
	if resumeUC != nil {
		h.SetResumeCheckUseCase(resumeUC)
	}
	if deleteUC != nil {
		h.SetDeleteCheckUseCase(deleteUC)
	}
	if listRunsUC != nil {
		h.SetListCheckRunsUseCase(listRunsUC)
	}
	if getRunUC != nil {
		h.SetGetCheckRunUseCase(getRunUC)
	}
	r := gin.New()
	r.Use(testTenantMiddleware("default"))
	h.Register(r)
	return h, r
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

func TestHandler_CreateCheck(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	createUC := &stubCreateCheckUseCase{
		out: checkcommand.CreateResult{
			ID:        1,
			Name:      "api-health",
			TenantID:  "default",
			Status:    "active",
			CheckType: "http_health",
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	_, router := newCheckHandler(t, createUC, nil, nil, nil, nil, nil, nil, nil)

	body := `{"name":"api-health","check_type":"http_health","check_config":{"url":"http://localhost/healthz"},"assertion_rules":[{"metric":"status_code","operator":"==","threshold":200,"severity":"warning"}],"cron_expr":"*/5 * * * *"}`
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/checks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusCreated {
		t.Fatalf("expected status=%d, got %d, body=%s", stdhttp.StatusCreated, resp.Code, resp.Body.String())
	}
	if !createUC.called {
		t.Fatal("expected use case to be called")
	}
	if createUC.in.Name != "api-health" {
		t.Errorf("name = %q, want api-health", createUC.in.Name)
	}
}

func TestHandler_CreateCheck_ValidationError(t *testing.T) {
	_, router := newCheckHandler(t, &stubCreateCheckUseCase{}, nil, nil, nil, nil, nil, nil, nil)

	body := `{"name":""}`
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/checks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusBadRequest {
		t.Errorf("expected status=%d, got %d", stdhttp.StatusBadRequest, resp.Code)
	}
}

func TestHandler_ListChecks(t *testing.T) {
	listUC := &stubListChecksUseCase{
		out: checkquery.ListChecksResult{
			Items: []checkquery.ListItem{
				{ID: 1, Name: "api-health", Status: "active", CheckType: "http_health"},
			},
			Total: 1,
		},
	}
	_, router := newCheckHandler(t, nil, listUC, nil, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/checks?status=active&limit=10", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", stdhttp.StatusOK, resp.Code, resp.Body.String())
	}
	if !listUC.called {
		t.Fatal("expected use case to be called")
	}

	var out checkListResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(out.Items))
	}
}

func TestHandler_GetCheck(t *testing.T) {
	now := time.Now()
	getUC := &stubGetCheckUseCase{
		out: checkquery.GetResult{
			ID:        1,
			Name:      "api-health",
			Status:    "active",
			CheckType: "http_health",
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	_, router := newCheckHandler(t, nil, nil, getUC, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/checks/1", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", stdhttp.StatusOK, resp.Code, resp.Body.String())
	}
	if !getUC.called {
		t.Fatal("expected use case to be called")
	}
}

func TestHandler_GetCheck_NotFound(t *testing.T) {
	getUC := &stubGetCheckUseCase{
		err: &resource.NotFoundError{Resource: "check", ID: 99},
	}
	_, router := newCheckHandler(t, nil, nil, getUC, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/checks/99", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusNotFound {
		t.Errorf("expected status=%d, got %d", stdhttp.StatusNotFound, resp.Code)
	}
}

func TestHandler_PauseCheck(t *testing.T) {
	pauseUC := &stubPauseCheckUseCase{
		out: checkcommand.ChangeStatusResult{ID: 1, Status: "paused", Version: 2},
	}
	_, router := newCheckHandler(t, nil, nil, nil, pauseUC, nil, nil, nil, nil)

	body := `{"version":1}`
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/checks/1/pause", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", stdhttp.StatusOK, resp.Code, resp.Body.String())
	}
	if !pauseUC.called {
		t.Fatal("expected use case to be called")
	}
	if pauseUC.in.Version != 1 {
		t.Errorf("version = %d, want 1", pauseUC.in.Version)
	}
}

func TestHandler_DeleteCheck(t *testing.T) {
	deleteUC := &stubDeleteCheckUseCase{}
	_, router := newCheckHandler(t, nil, nil, nil, nil, nil, deleteUC, nil, nil)

	body := `{"version":1}`
	req := httptest.NewRequest(stdhttp.MethodDelete, "/api/v1/checks/1", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", stdhttp.StatusOK, resp.Code, resp.Body.String())
	}
	if !deleteUC.called {
		t.Fatal("expected use case to be called")
	}
	if deleteUC.in.Version != 1 {
		t.Errorf("version = %d, want 1", deleteUC.in.Version)
	}
}

func TestHandler_DeleteCheck_NotFound(t *testing.T) {
	deleteUC := &stubDeleteCheckUseCase{
		err: &resource.NotFoundError{Resource: "check", ID: 99},
	}
	_, router := newCheckHandler(t, nil, nil, nil, nil, nil, deleteUC, nil, nil)

	body := `{"version":1}`
	req := httptest.NewRequest(stdhttp.MethodDelete, "/api/v1/checks/99", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusNotFound {
		t.Errorf("expected status=%d, got %d", stdhttp.StatusNotFound, resp.Code)
	}
}

func TestHandler_ListCheckRuns(t *testing.T) {
	now := time.Now()
	sev := "ok"
	listUC := &stubListCheckRunsUseCase{
		out: checkrunquery.ListCheckRunsResult{
			Items: []checkrunquery.ListItem{
				{ID: 1, RunID: "run-1", CheckID: 1, Status: "success", Severity: &sev, CreatedAt: now},
			},
			Total: 1,
		},
	}
	_, router := newCheckHandler(t, nil, nil, nil, nil, nil, nil, listUC, nil)

	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/check-runs?check_id=1&limit=10", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", stdhttp.StatusOK, resp.Code, resp.Body.String())
	}
	if !listUC.called {
		t.Fatal("expected use case to be called")
	}
}

func TestHandler_GetCheckRun(t *testing.T) {
	now := time.Now()
	sev := "ok"
	getUC := &stubGetCheckRunUseCase{
		out: checkrunquery.GetResult{
			ID:          1,
			RunID:       "run-1",
			CheckID:     1,
			Status:      "success",
			Severity:    &sev,
			ScheduledAt: now,
		},
	}
	_, router := newCheckHandler(t, nil, nil, nil, nil, nil, nil, nil, getUC)

	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/check-runs/1", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", stdhttp.StatusOK, resp.Code, resp.Body.String())
	}
	if !getUC.called {
		t.Fatal("expected use case to be called")
	}
}
