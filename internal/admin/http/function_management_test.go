package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	functioncommand "orbitjob/internal/admin/app/function/command"
	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/core/app/functioninvoke"
	"orbitjob/internal/core/domain/function"
	"orbitjob/internal/core/domain/policy"
)

type fakeFunctionManager struct {
	input   functioncommand.Input
	calls   int
	created bool
	err     error
}

func (m *fakeFunctionManager) Create(_ context.Context, in functioncommand.Input) (functioncommand.CreateResult, error) {
	m.input, m.calls = in, m.calls+1
	in.Definition.ID, in.Definition.Version = 9, 1
	return functioncommand.CreateResult{Definition: in.Definition, Created: m.created}, m.err
}
func (m *fakeFunctionManager) Update(_ context.Context, in functioncommand.Input) (function.Definition, error) {
	m.input, m.calls = in, m.calls+1
	in.Definition.Version++
	return in.Definition, m.err
}

func managementRouter(manager *fakeFunctionManager, rowGroup, keyGroup string, docs []policy.Document) *gin.Engine {
	h := NewHandler(nil, nil, nil)
	reader := &fakeFunctionStore{}
	reader.seed(function.Definition{ID: 9, TenantID: usecaseTestTenant, ResourceGroupID: rowGroup})
	h.SetFunctionManagementUseCase(manager, reader, nil)
	r := gin.New()
	r.Use(testPrincipalMiddleware(usecaseTestTenant, "key-123", keyGroup))
	if docs != nil {
		r.Use(func(c *gin.Context) { middleware.SetDocuments(c, docs); c.Next() })
	}
	h.Register(r)
	return r
}

func functionWriteRequest(method, path, body, key string) *http.Request {
	r := httptest.NewRequest(method, "/api/v1"+path, bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")
	if key != "" {
		r.Header.Set(idempotencyKeyHeader, key)
	}
	return r
}

func TestCreateFunctionRequiresStableIdentityAndRejectsScopeInjection(t *testing.T) {
	for _, tc := range []struct {
		name, body, key string
		status          int
	}{
		{"new", `{"name":"charge","image":"app:v1"}`, "receipt", 201},
		{"no identity", `{"name":"charge","image":"app:v1"}`, "", 400},
		{"injected group", `{"name":"charge","image":"app:v1","resource_group_id":"foreign"}`, "receipt", 400},
		{"injected tenant", `{"name":"charge","image":"app:v1","tenant_id":"` + usecaseTestTenant + `"}`, "receipt", 400},
		{"missing image", `{"name":"charge"}`, "receipt", 400},
		{"trailing body", `{"name":"charge","image":"app:v1"}{}`, "receipt", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager := &fakeFunctionManager{created: true}
			r := managementRouter(manager, "", "group-9", nil)
			response := httptest.NewRecorder()
			r.ServeHTTP(response, functionWriteRequest(http.MethodPost, "/functions", tc.body, tc.key))
			if response.Code != tc.status {
				t.Fatalf("status = %d: %s", response.Code, response.Body.String())
			}
			if tc.status == 201 {
				if manager.input.ResourceGroupID != "group-9" || manager.input.ActorID != "key-123" || manager.input.Definition.TimeoutSeconds != 60 || manager.input.Definition.HistorySuccess != 3 {
					t.Fatalf("input = %+v", manager.input)
				}
			} else if manager.calls != 0 {
				t.Fatalf("writes = %d", manager.calls)
			}
		})
	}
}

func TestCreateFunctionReplayReturnsExistingResource(t *testing.T) {
	manager := &fakeFunctionManager{created: false}
	response := httptest.NewRecorder()
	managementRouter(manager, "", "", nil).ServeHTTP(response, functionWriteRequest(http.MethodPost, "/functions", `{"name":"charge","image":"app:v1"}`, "receipt"))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	var item FunctionItem
	if err := json.Unmarshal(response.Body.Bytes(), &item); err != nil || item.ID != 9 || item.Ready || item.ActiveRevisionID != nil {
		t.Fatalf("item = %+v, error = %v", item, err)
	}
}

func TestCreateFunctionReplayDoesNotCallAnEditedOrPausedSnapshotReady(t *testing.T) {
	for _, current := range []function.Definition{
		{ID: 9, TenantID: usecaseTestTenant, Version: 2, Status: function.StatusActive},
		{ID: 9, TenantID: usecaseTestTenant, Version: 1, Status: function.StatusPaused},
	} {
		h := NewHandler(nil, nil, nil)
		store := &fakeFunctionStore{}
		store.seed(current)
		h.SetFunctionManagementUseCase(&fakeFunctionManager{}, store, readinessSource{rev: functioninvoke.Revision{ID: 51, DefinitionVersion: 1}, found: true})
		router := gin.New()
		router.Use(testPrincipalMiddleware(usecaseTestTenant, "key-123", ""))
		h.Register(router)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, functionWriteRequest(http.MethodPost, "/functions", `{"name":"original","image":"app:v1"}`, "receipt"))
		var item FunctionItem
		if err := json.Unmarshal(response.Body.Bytes(), &item); err != nil || response.Code != http.StatusOK || item.Ready || item.Version != 1 || item.Image != "app:v1" || item.ActiveRevisionVersion == nil || *item.ActiveRevisionVersion != 1 {
			t.Fatalf("replay = %+v, status = %d, error = %v", item, response.Code, err)
		}
	}
}

func TestFunctionWritesNeedIndependentPermissionsAndExactPersistedGroup(t *testing.T) {
	allow := policy.Statement{Effect: policy.EffectAllow, Action: []string{"function:Create", "function:Update"}, Resource: []string{"orbitjob:self:*:function/*"}}
	readOnly := policy.Statement{Effect: policy.EffectAllow, Action: []string{"function:Get", "function:Invoke"}, Resource: []string{"orbitjob:self:*:function/*"}}
	deny := policy.Statement{Effect: policy.EffectDeny, Action: []string{"function:Update"}, Resource: []string{"orbitjob:self:group-a:function/9"}}
	for _, tc := range []struct {
		name, method, path, rowGroup, keyGroup string
		statements                             []policy.Statement
	}{
		{"read cannot create", "POST", "/functions", "", "", []policy.Statement{readOnly}},
		{"read cannot update", "PUT", "/functions/9", "", "", []policy.Statement{readOnly}},
		{"group Deny on unscoped key", "PUT", "/functions/9", "group-a", "", []policy.Statement{allow, deny}},
		{"group boundary", "PUT", "/functions/9", "group-a", "group-b", []policy.Statement{allow}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager := &fakeFunctionManager{}
			response := httptest.NewRecorder()
			managementRouter(manager, tc.rowGroup, tc.keyGroup, []policy.Document{{Statement: tc.statements}}).ServeHTTP(response,
				functionWriteRequest(tc.method, tc.path, `{"name":"charge","image":"app:v1","expected_version":4}`, "receipt"))
			if response.Code != http.StatusForbidden || manager.calls != 0 {
				t.Fatalf("status = %d, writes = %d: %s", response.Code, manager.calls, response.Body.String())
			}
		})
	}
}

func TestUpdateFunctionCarriesReviewedVersionAndPauseState(t *testing.T) {
	manager := &fakeFunctionManager{}
	response := httptest.NewRecorder()
	managementRouter(manager, "group-a", "group-a", nil).ServeHTTP(response, functionWriteRequest(http.MethodPut, "/functions/9",
		`{"name":"charge","image":"app:v2","status":"paused","expected_version":4,"history_success":0}`, ""))
	if response.Code != http.StatusOK || manager.input.Definition.Version != 4 || manager.input.Definition.Status != function.StatusPaused || manager.input.Definition.HistorySuccess != 0 {
		t.Fatalf("status = %d, input = %+v: %s", response.Code, manager.input, response.Body.String())
	}
}

type readinessSource struct {
	rev   functioninvoke.Revision
	found bool
	err   error
}

func (s readinessSource) ActiveRevision(context.Context, string, string) (functioninvoke.Revision, bool, error) {
	return s.rev, s.found, s.err
}

func TestFunctionReadinessDistinguishesDefinitionAndActiveRevision(t *testing.T) {
	def := function.Definition{ID: 9, TenantID: usecaseTestTenant, Version: 3, Status: function.StatusActive}
	for _, version := range []int{2, 3} {
		item, err := functionItemWithReadiness(context.Background(), def, readinessSource{rev: functioninvoke.Revision{ID: 51, DefinitionVersion: version}, found: true})
		if err != nil || item.ActiveRevisionID == nil || *item.ActiveRevisionID != 51 || item.ActiveRevisionVersion == nil || *item.ActiveRevisionVersion != version || item.Ready != (version == 3) {
			t.Fatalf("version = %d, item = %+v, error = %v", version, item, err)
		}
	}
	if _, err := functionItemWithReadiness(context.Background(), def, readinessSource{err: errors.New("database unavailable")}); err == nil {
		t.Fatal("lookup failure cannot report an unready healthy result")
	}
}

func TestInvokeFunctionForwardsExpectedVersion(t *testing.T) {
	invoke := &stubInvokeFunction{}
	_, router := newFunctionHandler(nil, nil, invoke, nil, nil, "")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/functions/9/invoke?expected_version=3", nil))
	if response.Code != http.StatusOK || invoke.in.ExpectedVersion != 3 {
		t.Fatalf("status = %d, input = %+v", response.Code, invoke.in)
	}
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/functions/9/invoke?expected_version=0", nil))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("zero precondition status = %d", response.Code)
	}
}
