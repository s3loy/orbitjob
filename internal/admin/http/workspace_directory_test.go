package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	query "orbitjob/internal/admin/app/workspace/query"
	"orbitjob/internal/admin/http/middleware"
)

type directoryReaderFake struct {
	input query.DirectoryInput
	calls int
	err   error
}

func (f *directoryReaderFake) ListWorkspaceDirectory(_ context.Context, in query.DirectoryInput) ([]query.WorkspaceListItem, error) {
	f.input = in
	f.calls++
	return nil, f.err
}
func workspaceRouter(reader *directoryReaderFake, p *middleware.Principal, withDocs bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if p != nil {
			middleware.SetPrincipal(c, *p)
			if withDocs {
				middleware.SetDocuments(c, nil)
			}
		}
		c.Next()
	})
	h := NewHandler(nil, nil, nil)
	h.SetWorkspaceDirectoryLister(query.NewLister(reader))
	h.Register(r)
	return r
}
func TestWorkspaceDirectoryAuthenticationAndIgnoredTargetSelector(t *testing.T) {
	reader := &directoryReaderFake{}
	own := usecaseTestTenant
	principal := &middleware.Principal{Kind: middleware.KindTenant, TenantID: own}
	for _, tc := range []struct {
		principal *middleware.Principal
		docs      bool
		status    int
	}{{nil, false, 401}, {principal, false, 500}, {principal, true, 200}} {
		w := httptest.NewRecorder()
		workspaceRouter(reader, tc.principal, tc.docs).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/workspaces?tenant_id="+usecaseTestTenantB, nil))
		if w.Code != tc.status {
			t.Fatalf("auth status=%d body=%s", w.Code, w.Body)
		}
		if w.Code == 200 {
			var result query.WorkspaceListResult
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Items == nil || result.HasMore || reader.input.Scope.All || len(reader.input.Scope.Include) != 1 || reader.input.Scope.Include[0] != own {
				t.Fatalf("selector widened discovery: %+v %+v", reader.input, result)
			}
		}
	}
}
func TestWorkspaceDirectoryUnicodeAndPaginationValidation(t *testing.T) {
	reader := &directoryReaderFake{}
	p := &middleware.Principal{Kind: middleware.KindTenant, TenantID: usecaseTestTenant}
	r := workspaceRouter(reader, p, true)
	for _, tc := range []struct {
		query  string
		status int
	}{{"limit=0", 400}, {"limit=-1", 400}, {"limit=101", 400}, {"offset=-1", 400}, {"offset=2147483648", 400}, {"search=" + url.QueryEscape(strings.Repeat("\u4e2d", 129)), 400}, {"search=" + url.QueryEscape("  "+strings.Repeat("\u4e2d", 128)+"  "), 200}} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/workspaces?"+tc.query, nil))
		if w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.query, w.Code, w.Body)
		}
	}
	reader.err = errors.New("directory unavailable")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/workspaces", nil))
	if w.Code != 500 {
		t.Fatalf("false successful empty directory: %d %s", w.Code, w.Body)
	}
	op := ServiceOpenAPIDocument().Paths["/api/v1/workspaces"].Get
	if op == nil || op.Permission != "" || len(op.Security) != 2 {
		t.Fatalf("directory auth contract: %+v", op)
	}
	for _, parameter := range op.Parameters {
		if parameter.Name == "limit" && parameter.Schema.Default != float64(20) {
			t.Fatalf("default page contract: %+v", parameter)
		}
	}
}

func TestWorkspaceDirectoryNULSearchRejectedBeforeReading(t *testing.T) {
	reader := &directoryReaderFake{}
	p := &middleware.Principal{Kind: middleware.KindTenant, TenantID: usecaseTestTenant}
	r := workspaceRouter(reader, p, true)
	for _, search := range []string{"%00", "before%00after"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/workspaces?search="+search, nil))
		if w.Code != 400 || reader.calls != 0 {
			t.Fatalf("search=%s status=%d calls=%d body=%s", search, w.Code, reader.calls, w.Body)
		}
	}
}
