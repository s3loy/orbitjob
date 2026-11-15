package http

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	app "orbitjob/internal/admin/app/sourcecontrol"
	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/sourcecontrol"
)

type sourceControlStub struct {
	target        app.Target
	state         sourcecontrol.State
	reads, writes int
	change        app.Change
	actor         string
	err           error
}

func (s *sourceControlStub) Resolve(context.Context, string, string, int64) (app.Target, error) {
	return s.target, nil
}
func (s *sourceControlStub) Get(context.Context, app.Target) (sourcecontrol.State, error) {
	s.reads++
	return s.state, s.err
}
func (s *sourceControlStub) Update(_ context.Context, _ app.Target, c app.Change, actor string) (sourcecontrol.State, error) {
	s.writes++
	s.change, s.actor = c, actor
	return s.state, s.err
}

func sourceControlRouter(t *testing.T, stub *sourceControlStub, group string, docs []policy.Document) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := NewHandler(nil, nil, nil)
	h.SetSourceControlService(stub)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		middleware.SetPrincipal(c, middleware.Principal{Kind: middleware.KindTenant, TenantID: "tenant-a", ResourceGroupID: group, KeyID: "authenticated-key"})
		middleware.SetDocuments(c, docs)
		c.Next()
	})
	h.Register(r)
	return r
}

func sourceControlGrant(action, arn string) policy.Document {
	return policy.Document{Statement: []policy.Statement{{Effect: policy.EffectAllow, Action: []string{action}, Resource: []string{arn}}}}
}
func sourceControlHTTPStub() *sourceControlStub {
	return &sourceControlStub{target: app.Target{TenantID: "tenant-a", Kind: "functions", ID: 7, Mode: "function", SourceUID: "function-7", ResourceGroupID: "billing"}, state: sourcecontrol.State{Mode: "function", SourceUID: "function-7"}}
}

func TestSourceControlGetDoesNotGrantManagement(t *testing.T) {
	s := sourceControlHTTPStub()
	r := sourceControlRouter(t, s, "", []policy.Document{sourceControlGrant("function:Get", "orbitjob:tenant-a:billing:function/7")})
	response := httptest.NewRecorder()
	r.ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/functions/7/execution-controls", nil))
	var state SourceControlResponse
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil || response.Code != 200 || state.CanManage || s.reads != 1 {
		t.Fatalf("GET status=%d body=%s state=%+v err=%v", response.Code, response.Body, state, err)
	}
	request := httptest.NewRequest("PATCH", "/api/v1/functions/7/execution-controls", strings.NewReader(`{"version":0,"schedule_paused":false,"execution_paused":true}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	r.ServeHTTP(response, request)
	if response.Code != 403 || s.writes != 0 {
		t.Fatalf("read permission mutated controls: status=%d writes=%d", response.Code, s.writes)
	}
}

func TestSourceControlUsesActualParentGroupAndDeny(t *testing.T) {
	for _, group := range []string{"", "billing", "another-group"} {
		t.Run(group, func(t *testing.T) {
			s := sourceControlHTTPStub()
			docs := []policy.Document{sourceControlGrant("*", "orbitjob:tenant-a:*:function/*"), {Statement: []policy.Statement{{Effect: policy.EffectDeny, Action: []string{"function:ManageExecution"}, Resource: []string{"orbitjob:tenant-a:billing:function/7"}}}}}
			r := sourceControlRouter(t, s, group, docs)
			request := httptest.NewRequest("PATCH", "/api/v1/functions/7/execution-controls", strings.NewReader(`{"version":0,"schedule_paused":false,"execution_paused":true}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Resource-Group", "unrelated")
			response := httptest.NewRecorder()
			r.ServeHTTP(response, request)
			want := 403
			if group == "another-group" {
				want = 404
			}
			if response.Code != want || s.writes != 0 {
				t.Fatalf("Deny/group bypass: status=%d writes=%d body=%s", response.Code, s.writes, response.Body)
			}
		})
	}
}

func TestSourceControlPatchValidatesOnlyControlsAndAuthenticatesActor(t *testing.T) {
	for _, c := range []struct {
		body   string
		status int
	}{
		{`{"version":0,"schedule_paused":false,"execution_paused":true}`, 200},
		{`{"version":0,"execution_paused":true}`, 400},
		{`{"version":0,"version":0,"schedule_paused":false,"execution_paused":true}`, 400},
		{`{"version":0,"schedule_paused":false,"execution_paused":true,"source_uid":"other-source"}`, 400},
		{`{"version":0,"schedule_paused":false,"execution_paused":true,"tenant_id":"tenant-b"}`, 400},
	} {
		t.Run(c.body, func(t *testing.T) {
			s := sourceControlHTTPStub()
			r := sourceControlRouter(t, s, "billing", []policy.Document{sourceControlGrant("function:ManageExecution", "orbitjob:tenant-a:billing:function/7")})
			request := httptest.NewRequest("PATCH", "/api/v1/functions/7/execution-controls", strings.NewReader(c.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Actor-ID", "untrusted-actor")
			response := httptest.NewRecorder()
			r.ServeHTTP(response, request)
			if response.Code != c.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body)
			}
			if c.status == 200 && (s.writes != 1 || s.actor != "authenticated-key" || !s.change.ExecutionPaused || s.change.Version != 0) {
				t.Fatalf("wrong mutation %+v", s)
			}
			if c.status != 200 && s.writes != 0 {
				t.Fatal("invalid body reached writer")
			}
		})
	}
}

func TestSourceControlConflictIsReturnedWithoutRetry(t *testing.T) {
	s := sourceControlHTTPStub()
	s.err = &resource.ConflictError{Resource: "source_control", ID: "function-7", Message: "version changed"}
	r := sourceControlRouter(t, s, "", []policy.Document{sourceControlGrant("function:ManageExecution", "orbitjob:tenant-a:billing:function/7")})
	request := httptest.NewRequest("PATCH", "/api/v1/functions/7/execution-controls", strings.NewReader(`{"version":0,"schedule_paused":false,"execution_paused":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	if response.Code != 409 || s.writes != 1 || s.reads != 0 {
		t.Fatalf("conflict retried: status=%d writes=%d reads=%d", response.Code, s.writes, s.reads)
	}
}
