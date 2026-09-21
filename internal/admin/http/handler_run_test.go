package http

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	runquery "orbitjob/internal/admin/app/run/query"
	"orbitjob/internal/domain/resource"
)

// Unit coverage for the run-list handler, restored after the deleted
// handler_coverage_test.go took it away. ListInstances is a GET: the "body" it
// decodes is the query string, so the decoding cases here are bad query
// parameters.

type stubListRunsUseCase struct {
	called bool
	in     runquery.ListInput
	out    []runquery.ListItem
	err    error
}

func (s *stubListRunsUseCase) List(ctx context.Context, in runquery.ListInput) ([]runquery.ListItem, error) {
	s.called = true
	s.in = in
	return s.out, s.err
}

// newRunListHandler mounts the run list behind a key scoped to groupID (empty
// for an unscoped key).
func newRunListHandler(listUC *stubListRunsUseCase, groupID string) (*Handler, *gin.Engine) {
	gin.SetMode(gin.TestMode)
	h := NewHandler(nil, nil, nil)
	h.SetListInstancesUseCase(listUC)
	r := gin.New()
	r.Use(testPrincipalMiddleware("tenant-a", "key-123", groupID))
	h.Register(r)
	return h, r
}

func TestHandler_ListInstances_DelegatesToPrincipalTenantAndScope(t *testing.T) {
	listUC := &stubListRunsUseCase{
		out: []runquery.ListItem{
			{ID: 1, TenantID: "tenant-a", Phase: "Running", Actor: "key-123", Attempt: 1, MaxAttempts: 3},
		},
	}
	_, router := newRunListHandler(listUC, "rg-7")

	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/instances?phase=Running&limit=10&offset=5", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != stdhttp.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", stdhttp.StatusOK, resp.Code, resp.Body.String())
	}
	if !listUC.called {
		t.Fatal("expected use case to be called")
	}
	if listUC.in.TenantID != "tenant-a" {
		t.Errorf("tenant = %q, want the authenticated principal's tenant", listUC.in.TenantID)
	}
	if listUC.in.ResourceGroupID != "rg-7" {
		t.Errorf("resource group = %q, want the authenticated principal's scope", listUC.in.ResourceGroupID)
	}
	if listUC.in.Phase != "Running" {
		t.Errorf("phase = %q, want Running", listUC.in.Phase)
	}
	if listUC.in.Limit != 10 || listUC.in.Offset != 5 {
		t.Errorf("page = %d/%d, want 10/5", listUC.in.Limit, listUC.in.Offset)
	}

	var out runListResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Items) != 1 || out.Items[0].ID != 1 {
		t.Fatalf("unexpected items: %+v", out.Items)
	}
}

func TestHandler_ListInstances_UnknownPrincipalScopePassesEmpty(t *testing.T) {
	// An unscoped key must reach the use case with an empty scope: the zero
	// value, not a placeholder the use case could mistake for a group id.
	listUC := &stubListRunsUseCase{}
	_, router := newRunListHandler(listUC, "")

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(stdhttp.MethodGet, "/api/v1/instances", nil))

	if resp.Code != stdhttp.StatusOK {
		t.Fatalf("expected status=%d, got %d, body=%s", stdhttp.StatusOK, resp.Code, resp.Body.String())
	}
	if listUC.in.ResourceGroupID != "" {
		t.Fatalf("resource group = %q, want empty for an unscoped caller", listUC.in.ResourceGroupID)
	}
}

func TestHandler_ListInstances_InvalidQueryReturns400(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{"unknown phase", "?phase=Finished"},
		{"limit above maximum", "?limit=101"},
		{"negative offset", "?offset=-1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			listUC := &stubListRunsUseCase{}
			_, router := newRunListHandler(listUC, "")

			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, httptest.NewRequest(stdhttp.MethodGet, "/api/v1/instances"+tc.query, nil))

			if resp.Code != stdhttp.StatusBadRequest {
				t.Fatalf("expected status=%d, got %d, body=%s", stdhttp.StatusBadRequest, resp.Code, resp.Body.String())
			}
			if listUC.called {
				t.Fatal("use case must not be called for an undecodable query")
			}
		})
	}
}

func TestHandler_ListInstances_StatusMapping(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{
			// A run has no group of its own, so a scoped caller is refused
			// outright rather than filtered.
			name: "scope error maps to 403",
			err:  &resource.ScopeError{Resource: "run", Scope: "rg-7"},
			want: stdhttp.StatusForbidden,
		},
		{
			name: "not found maps to 404",
			err:  &resource.NotFoundError{Resource: "run", ID: 99},
			want: stdhttp.StatusNotFound,
		},
		{
			name: "conflict maps to 409",
			err:  &resource.ConflictError{Resource: "run", ID: 99},
			want: stdhttp.StatusConflict,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			listUC := &stubListRunsUseCase{err: tc.err}
			_, router := newRunListHandler(listUC, "")

			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, httptest.NewRequest(stdhttp.MethodGet, "/api/v1/instances", nil))

			if resp.Code != tc.want {
				t.Fatalf("expected status=%d, got %d, body=%s", tc.want, resp.Code, resp.Body.String())
			}
		})
	}
}
