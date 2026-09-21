package http

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"orbitjob/internal/domain/resource"
)

// Handler-level coverage for the workflow routes, over stubbed use cases: the
// routes' own contract -- URI decoding, principal propagation, the body-less
// cancel, the 201/200 trigger split, error mapping -- is what is pinned here.

type stubListWorkflows struct {
	called bool
	in     WorkflowListInput
	out    []WorkflowListItem
	err    error
}

func (s *stubListWorkflows) List(ctx context.Context, in WorkflowListInput) ([]WorkflowListItem, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

type stubGetWorkflow struct {
	called bool
	in     WorkflowGetInput
	out    WorkflowGetItem
	err    error
}

func (s *stubGetWorkflow) Get(ctx context.Context, in WorkflowGetInput) (WorkflowGetItem, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

type stubTriggerWorkflow struct {
	called bool
	in     WorkflowTriggerInput
	out    WorkflowTriggerResult
	err    error
}

func (s *stubTriggerWorkflow) Trigger(ctx context.Context, in WorkflowTriggerInput) (WorkflowTriggerResult, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

type stubListWorkflowRuns struct {
	called bool
	in     WorkflowRunListInput
	out    []WorkflowRunItem
	err    error
}

func (s *stubListWorkflowRuns) List(ctx context.Context, in WorkflowRunListInput) ([]WorkflowRunItem, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

type stubGetWorkflowRun struct {
	called bool
	in     WorkflowRunGetInput
	out    WorkflowRunGetItem
	err    error
}

func (s *stubGetWorkflowRun) Get(ctx context.Context, in WorkflowRunGetInput) (WorkflowRunGetItem, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

type stubCancelWorkflowRun struct {
	called bool
	in     WorkflowCancelInput
	out    WorkflowCancelResult
	err    error
}

func (s *stubCancelWorkflowRun) Cancel(ctx context.Context, in WorkflowCancelInput) (WorkflowCancelResult, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

func newWorkflowHandler(
	list *stubListWorkflows,
	get *stubGetWorkflow,
	trigger *stubTriggerWorkflow,
	listRuns *stubListWorkflowRuns,
	getRun *stubGetWorkflowRun,
	cancel *stubCancelWorkflowRun,
	groupID string,
) (*Handler, *gin.Engine) {
	gin.SetMode(gin.TestMode)
	h := NewHandler(nil, nil, nil)
	if list != nil {
		h.SetListWorkflowsUseCase(list)
	}
	if get != nil {
		h.SetGetWorkflowUseCase(get)
	}
	if trigger != nil {
		h.SetTriggerWorkflowUseCase(trigger)
	}
	if listRuns != nil {
		h.SetListWorkflowRunsUseCase(listRuns)
	}
	if getRun != nil {
		h.SetGetWorkflowRunUseCase(getRun)
	}
	if cancel != nil {
		h.SetCancelWorkflowRunUseCase(cancel)
	}
	r := gin.New()
	r.Use(testPrincipalMiddleware("tenant-a", "key-123", groupID))
	h.Register(r)
	return h, r
}

func TestHandler_ListWorkflows_Delegates(t *testing.T) {
	list := &stubListWorkflows{out: []WorkflowListItem{{ID: 5, Name: "nightly-dag"}}}
	_, router := newWorkflowHandler(list, nil, nil, nil, nil, nil, "")

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(stdhttp.MethodGet, "/api/v1/workflows", nil))

	if resp.Code != stdhttp.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", stdhttp.StatusOK, resp.Code, resp.Body.String())
	}
	if !list.called || list.in.TenantID != "tenant-a" {
		t.Fatalf("unexpected delegation: called=%v in=%+v", list.called, list.in)
	}
}

func TestHandler_GetWorkflow_MapsNotFound(t *testing.T) {
	get := &stubGetWorkflow{err: &resource.NotFoundError{Resource: "workflow", ID: int64(5)}}
	_, router := newWorkflowHandler(nil, get, nil, nil, nil, nil, "")

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(stdhttp.MethodGet, "/api/v1/workflows/5", nil))

	if resp.Code != stdhttp.StatusNotFound {
		t.Fatalf("expected status=404, got %d, body=%s", resp.Code, resp.Body.String())
	}
}

func TestHandler_TriggerWorkflow_RecordsAuthenticatedKey(t *testing.T) {
	trigger := &stubTriggerWorkflow{out: WorkflowTriggerResult{
		Namespace: "orbitjob", Name: "nightly-dag-1a2b3c4d",
		OccurrenceKey: "abc", Trigger: "Manual", Created: true,
	}}
	_, router := newWorkflowHandler(nil, nil, trigger, nil, nil, nil, "")

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/workflows/5/runs", nil)
	req.Header.Set("X-OrbitJob-Idempotency-Key", "idem-9")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusCreated {
		t.Fatalf("expected status=201, got %d, body=%s", resp.Code, resp.Body.String())
	}
	if trigger.in.WorkflowID != 5 {
		t.Errorf("workflow id = %d, want 5", trigger.in.WorkflowID)
	}
	if trigger.in.ActorID != "key-123" {
		t.Errorf("actor = %q, want the authenticated key's id", trigger.in.ActorID)
	}
	if trigger.in.IdempotencyKey != "idem-9" {
		t.Errorf("idempotency key = %q, want idem-9", trigger.in.IdempotencyKey)
	}
}

func TestHandler_TriggerWorkflow_ReplayAnswers200(t *testing.T) {
	trigger := &stubTriggerWorkflow{out: WorkflowTriggerResult{Created: false}}
	_, router := newWorkflowHandler(nil, nil, trigger, nil, nil, nil, "")

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(stdhttp.MethodPost, "/api/v1/workflows/5/runs", nil))

	if resp.Code != stdhttp.StatusOK {
		t.Fatalf("expected status=200, got %d, body=%s", resp.Code, resp.Body.String())
	}
}

func TestHandler_TriggerWorkflow_MapsSuspendedConflict(t *testing.T) {
	trigger := &stubTriggerWorkflow{err: &resource.ConflictError{
		Resource: "workflow", ID: int64(5), Field: "suspend",
		Message: "cannot trigger a suspended workflow",
	}}
	_, router := newWorkflowHandler(nil, nil, trigger, nil, nil, nil, "")

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(stdhttp.MethodPost, "/api/v1/workflows/5/runs", nil))

	if resp.Code != stdhttp.StatusConflict {
		t.Fatalf("expected status=409, got %d, body=%s", resp.Code, resp.Body.String())
	}
}

func TestHandler_CancelWorkflowRun_CarriesNoBody(t *testing.T) {
	cancel := &stubCancelWorkflowRun{out: WorkflowCancelResult{
		Namespace: "orbitjob", Name: "nightly-dag-1a2b3c4d", OccurrenceKey: "abc", Phase: "Running",
	}}
	_, router := newWorkflowHandler(nil, nil, nil, nil, nil, cancel, "")

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(stdhttp.MethodPost, "/api/v1/workflows/5/runs/12/cancel", nil))

	if resp.Code != stdhttp.StatusOK {
		t.Fatalf("expected status=200, got %d, body=%s", resp.Code, resp.Body.String())
	}
	if cancel.in.WorkflowID != 5 || cancel.in.RunID != 12 {
		t.Fatalf("unexpected input: %+v", cancel.in)
	}
	if cancel.in.TenantID != "tenant-a" {
		t.Errorf("tenant = %q, want the authenticated principal's tenant", cancel.in.TenantID)
	}
}

func TestHandler_GetWorkflowRun_MapsMissingCRConflict(t *testing.T) {
	getRun := &stubGetWorkflowRun{err: &resource.ConflictError{
		Resource: "workflow run resource", ID: "orbitjob/nightly-dag-1a2b3c4d",
		Message: "the WorkflowRun custom resource is missing",
	}}
	_, router := newWorkflowHandler(nil, nil, nil, nil, getRun, nil, "")

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(stdhttp.MethodGet, "/api/v1/workflows/5/runs/12", nil))

	if resp.Code != stdhttp.StatusConflict {
		t.Fatalf("expected status=409, got %d, body=%s", resp.Code, resp.Body.String())
	}
}

func TestHandler_ListWorkflowRuns_DelegatesUnderWorkflowPrefix(t *testing.T) {
	listRuns := &stubListWorkflowRuns{out: []WorkflowRunItem{{ID: 12, Phase: "Running"}}}
	_, router := newWorkflowHandler(nil, nil, nil, listRuns, nil, nil, "")

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(stdhttp.MethodGet, "/api/v1/workflows/5/runs?limit=10&offset=2", nil))

	if resp.Code != stdhttp.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", stdhttp.StatusOK, resp.Code, resp.Body.String())
	}
	if listRuns.in.WorkflowID != 5 || listRuns.in.Limit != 10 || listRuns.in.Offset != 2 {
		t.Fatalf("unexpected input: %+v", listRuns.in)
	}
}
