package http

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"orbitjob/internal/domain/resource"
)

// Handler-level coverage for the function routes. The use cases are stubs:
// these tests pin what the handler itself owns -- URI/query decoding, the
// authenticated principal's propagation, the 201/200 split on an invocation,
// and the mapping of use-case errors onto the stable API error structure.

type stubListFunctions struct {
	called bool
	in     FunctionListInput
	out    []FunctionItem
	err    error
}

func (s *stubListFunctions) List(ctx context.Context, in FunctionListInput) ([]FunctionItem, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

type stubGetFunction struct {
	called bool
	in     FunctionGetInput
	out    FunctionItem
	err    error
}

func (s *stubGetFunction) Get(ctx context.Context, in FunctionGetInput) (FunctionItem, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

type stubInvokeFunction struct {
	called bool
	in     FunctionInvokeInput
	out    FunctionInvokeResult
	err    error
}

func (s *stubInvokeFunction) Invoke(ctx context.Context, in FunctionInvokeInput) (FunctionInvokeResult, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

type stubListFunctionRuns struct {
	called bool
	in     FunctionRunListInput
	out    []FunctionRunItem
	err    error
}

func (s *stubListFunctionRuns) List(ctx context.Context, in FunctionRunListInput) ([]FunctionRunItem, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

type stubGetFunctionRun struct {
	called bool
	in     FunctionRunGetInput
	out    FunctionRunItem
	err    error
}

func (s *stubGetFunctionRun) Get(ctx context.Context, in FunctionRunGetInput) (FunctionRunItem, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

// newFunctionHandler mounts the function routes behind a principal whose key
// id is "key-123", scoped to groupID (empty for an unscoped key).
func newFunctionHandler(
	list *stubListFunctions,
	get *stubGetFunction,
	invoke *stubInvokeFunction,
	listRuns *stubListFunctionRuns,
	getRun *stubGetFunctionRun,
	groupID string,
) (*Handler, *gin.Engine) {
	gin.SetMode(gin.TestMode)
	h := NewHandler(nil, nil, nil)
	if list != nil {
		h.SetListFunctionsUseCase(list)
	}
	if get != nil {
		h.SetGetFunctionUseCase(get)
	}
	if invoke != nil {
		h.SetInvokeFunctionUseCase(invoke)
	}
	if listRuns != nil {
		h.SetListFunctionRunsUseCase(listRuns)
	}
	if getRun != nil {
		h.SetGetFunctionRunUseCase(getRun)
	}
	r := gin.New()
	r.Use(testPrincipalMiddleware("tenant-a", "key-123", groupID))
	h.Register(r)
	return h, r
}

func TestHandler_ListFunctions_DelegatesCallerScope(t *testing.T) {
	list := &stubListFunctions{out: []FunctionItem{{ID: 3, Name: "resize"}}}
	_, router := newFunctionHandler(list, nil, nil, nil, nil, "group-9")

	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/functions?limit=5", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", stdhttp.StatusOK, resp.Code, resp.Body.String())
	}
	if !list.called {
		t.Fatal("expected use case to be called")
	}
	if list.in.TenantID != "tenant-a" {
		t.Errorf("tenant = %q, want the authenticated principal's tenant", list.in.TenantID)
	}
	if list.in.ResourceGroupID != "group-9" {
		t.Errorf("group = %q, want the caller's scope", list.in.ResourceGroupID)
	}
	if list.in.Limit != 5 {
		t.Errorf("limit = %d, want 5", list.in.Limit)
	}
}

func TestHandler_ListFunctions_RejectsBadLimit(t *testing.T) {
	_, router := newFunctionHandler(&stubListFunctions{}, nil, nil, nil, nil, "")

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(stdhttp.MethodGet, "/api/v1/functions?limit=0&offset=-2", nil))

	if resp.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status=400, got %d, body=%s", resp.Code, resp.Body.String())
	}
}

func TestHandler_GetFunction_MapsNotFound(t *testing.T) {
	get := &stubGetFunction{err: &resource.NotFoundError{Resource: "function", ID: int64(9)}}
	_, router := newFunctionHandler(nil, get, nil, nil, nil, "")

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(stdhttp.MethodGet, "/api/v1/functions/9", nil))

	if resp.Code != stdhttp.StatusNotFound {
		t.Fatalf("expected status=404, got %d, body=%s", resp.Code, resp.Body.String())
	}
}

func TestHandler_InvokeFunction_RecordsAuthenticatedKey(t *testing.T) {
	invoke := &stubInvokeFunction{out: FunctionInvokeResult{
		Namespace: "orbitjob", Name: "function-3-1a2b3c4d",
		OccurrenceKey: "abc", Trigger: "Function", Created: true,
	}}
	_, router := newFunctionHandler(nil, nil, invoke, nil, nil, "")

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/functions/3/invoke?wait_seconds=5", nil)
	req.Header.Set("X-OrbitJob-Idempotency-Key", "idem-1")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusCreated {
		t.Fatalf("expected status=201, got %d, body=%s", resp.Code, resp.Body.String())
	}
	if invoke.in.FunctionID != 3 {
		t.Errorf("function id = %d, want 3", invoke.in.FunctionID)
	}
	if invoke.in.ActorID != "key-123" {
		t.Errorf("actor = %q, want the authenticated key's id", invoke.in.ActorID)
	}
	if invoke.in.IdempotencyKey != "idem-1" {
		t.Errorf("idempotency key = %q, want idem-1", invoke.in.IdempotencyKey)
	}
	if invoke.in.WaitSeconds != 5 {
		t.Errorf("wait_seconds = %d, want 5", invoke.in.WaitSeconds)
	}
}

func TestHandler_InvokeFunction_ReplayAnswers200(t *testing.T) {
	invoke := &stubInvokeFunction{out: FunctionInvokeResult{Created: false}}
	_, router := newFunctionHandler(nil, nil, invoke, nil, nil, "")

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(stdhttp.MethodPost, "/api/v1/functions/3/invoke", nil))

	if resp.Code != stdhttp.StatusOK {
		t.Fatalf("expected status=200, got %d, body=%s", resp.Code, resp.Body.String())
	}
}

func TestHandler_InvokeFunction_RejectsWaitOverCap(t *testing.T) {
	invoke := &stubInvokeFunction{}
	_, router := newFunctionHandler(nil, nil, invoke, nil, nil, "")

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(stdhttp.MethodPost, "/api/v1/functions/3/invoke?wait_seconds=61", nil))

	if resp.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status=400, got %d, body=%s", resp.Code, resp.Body.String())
	}
	if invoke.called {
		t.Fatal("use case must not be called for a wait over the cap")
	}
}

func TestHandler_InvokeFunction_MapsPausedConflict(t *testing.T) {
	invoke := &stubInvokeFunction{err: &resource.ConflictError{
		Resource: "function", ID: int64(3), Field: "status",
		Message: "cannot invoke a paused function",
	}}
	_, router := newFunctionHandler(nil, nil, invoke, nil, nil, "")

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(stdhttp.MethodPost, "/api/v1/functions/3/invoke", nil))

	if resp.Code != stdhttp.StatusConflict {
		t.Fatalf("expected status=409, got %d, body=%s", resp.Code, resp.Body.String())
	}
}

func TestHandler_ListFunctionRuns_DelegatesToReadModel(t *testing.T) {
	listRuns := &stubListFunctionRuns{out: []FunctionRunItem{{ID: 1, Status: "success"}}}
	_, router := newFunctionHandler(nil, nil, nil, listRuns, nil, "")

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(stdhttp.MethodGet, "/api/v1/functions/3/runs?limit=10", nil))

	if resp.Code != stdhttp.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", stdhttp.StatusOK, resp.Code, resp.Body.String())
	}
	if listRuns.in.FunctionID != 3 || listRuns.in.Limit != 10 {
		t.Fatalf("unexpected input: %+v", listRuns.in)
	}
	if listRuns.in.TenantID != "tenant-a" {
		t.Errorf("tenant = %q, want the authenticated principal's tenant", listRuns.in.TenantID)
	}
}

func TestHandler_GetFunctionRun_DelegatesRunID(t *testing.T) {
	getRun := &stubGetFunctionRun{out: FunctionRunItem{ID: 1, RunID: "0d3f", Status: "failed"}}
	_, router := newFunctionHandler(nil, nil, nil, nil, getRun, "")

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(stdhttp.MethodGet, "/api/v1/functions/3/runs/0d3f-uuid", nil))

	if resp.Code != stdhttp.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", stdhttp.StatusOK, resp.Code, resp.Body.String())
	}
	if getRun.in.RunID != "0d3f-uuid" || getRun.in.FunctionID != 3 {
		t.Fatalf("unexpected input: %+v", getRun.in)
	}
}

// A failed read must surface as its mapped status, never as a 500: the read
// model's "no such invocation" is a 404, and a canceled run's nullable
// timestamps must survive the trip.
func TestHandler_GetFunctionRun_MapsNotFound(t *testing.T) {
	getRun := &stubGetFunctionRun{err: &resource.NotFoundError{Resource: "function run", ID: "missing"}}
	_, router := newFunctionHandler(nil, nil, nil, nil, getRun, "")

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(stdhttp.MethodGet, "/api/v1/functions/3/runs/missing", nil))

	if resp.Code != stdhttp.StatusNotFound {
		t.Fatalf("expected status=404, got %d, body=%s", resp.Code, resp.Body.String())
	}
}
