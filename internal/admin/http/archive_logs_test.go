package http

import (
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/core/domain/function"
	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/logarchive"
	"strings"
	"testing"
)

type archiveFixture struct{ read, cleaned bool }

func (f *archiveFixture) Resolve(_ context.Context, tenant string, run, fn int64, uuid string) (logarchive.Identity, error) {
	if tenant != usecaseTestTenant {
		return logarchive.Identity{}, logarchive.ErrMissing
	}
	return logarchive.Identity{TenantID: tenant, RunID: run, FunctionID: fn, FunctionRunID: uuid}, nil
}
func (f *archiveFixture) RequestCleanup(context.Context, logarchive.Identity, string) error {
	f.cleaned = true
	return nil
}
func (f *archiveFixture) Read(context.Context, logarchive.Identity, logarchive.Query) (logarchive.Page, error) {
	f.read = true
	return logarchive.Page{State: "sealed", Records: []logarchive.Record{}, Gaps: []logarchive.Gap{}}, nil
}

func TestArchivePermissionsAndBoundedQueries(t *testing.T) {
	const instance = "/api/v1/instances/11/logs/archive"
	const fn = "/api/v1/functions/9/runs/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa/logs/archive"
	for _, tc := range []struct {
		name, method, path, grant, arn, keyGroup, actualGroup string
		deny                                                  bool
		status                                                int
	}{
		{"independent read", "GET", instance, "instance:Get", "orbitjob:*:*:*/*", "", "", false, 403},
		{"exact own read", "GET", instance, "instance:ReadLogs", "orbitjob:*:*:instance/11", "", "", false, 200},
		{"other exact read", "GET", instance, "instance:ReadLogs", "orbitjob:*:*:instance/12", "", "", false, 403},
		{"no pod locator", "GET", instance + "?pod=foreign", "instance:ReadLogs", "orbitjob:*:*:*/*", "", "", false, 400},
		{"cursor bound", "GET", instance + "?after=-1", "instance:ReadLogs", "orbitjob:*:*:*/*", "", "", false, 400},
		{"page bound", "GET", instance + "?limit=1001", "instance:ReadLogs", "orbitjob:*:*:*/*", "", "", false, 400},
		{"foreign tenant", "GET", instance + "?tenant_id=foreign", "instance:ReadLogs", "orbitjob:*:*:*/*", "", "", false, 403},
		{"read cannot delete", "DELETE", instance, "instance:ReadLogs", "orbitjob:*:*:*/*", "", "", false, 403},
		{"independent delete", "DELETE", instance, "instance:DeleteLogs", "orbitjob:*:*:instance/11", "", "", false, 202},
		{"download independent", "GET", instance + "/download", "instance:Get", "orbitjob:*:*:*/*", "", "", false, 403},
		{"download authorized", "GET", instance + "/download", "instance:ReadLogs", "orbitjob:*:*:instance/11", "", "", false, 200},
		{"group cannot tunnel", "GET", instance, "instance:ReadLogs", "orbitjob:*:*:*/*", "group", "group", false, 403},
		{"actual function group", "GET", fn, "function:ReadLogs", "orbitjob:*:group:function/9", "group", "group", false, 200},
		{"foreign function group", "GET", fn, "function:ReadLogs", "orbitjob:*:*:*/*", "group", "other", false, 403},
		{"actual function deny", "GET", fn, "function:ReadLogs", "orbitjob:*:*:*/*", "", "group", true, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			h := NewHandler(nil, nil, nil)
			backend := &archiveFixture{}
			h.SetArchiveBackend(backend, backend)
			reader := &fakeFunctionStore{}
			reader.seed(function.Definition{ID: 9, TenantID: usecaseTestTenant, ResourceGroupID: tc.actualGroup})
			h.SetFunctionManagementUseCase(nil, reader, nil)
			r := gin.New()
			r.Use(testPrincipalMiddleware(usecaseTestTenant, "key", tc.keyGroup))
			r.Use(func(c *gin.Context) {
				statements := []policy.Statement{{Effect: policy.EffectAllow, Action: []string{tc.grant}, Resource: []string{tc.arn}}}
				if tc.deny {
					statements = append(statements, policy.Statement{Effect: policy.EffectDeny, Action: []string{"function:ReadLogs"}, Resource: []string{"orbitjob:*:group:function/9"}})
				}
				middleware.SetDocuments(c, []policy.Document{{Statement: statements}})
			})
			h.Register(r)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.status {
				t.Fatalf("HTTP%d want%d: %s", w.Code, tc.status, w.Body.String())
			}
			if backend.read != (tc.status == 200) || backend.cleaned != (tc.status == 202) {
				t.Fatal("denied/invalid operation reached archive backend")
			}
		})
	}
}

type pagedArchiveFixture struct {
	archiveFixture
	pages []logarchive.Page
	index int
}

func (f *pagedArchiveFixture) Read(context.Context, logarchive.Identity, logarchive.Query) (logarchive.Page, error) {
	if f.index >= len(f.pages) {
		return logarchive.Page{}, fmt.Errorf("unexpected extra archive page")
	}
	p := f.pages[f.index]
	f.index++
	return p, nil
}

func TestArchiveDownloadIncludesLaterPageGapsAndState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gap := logarchive.Gap{From: 2, Through: 2, Reason: "retained segment is corrupt"}
	backend := &pagedArchiveFixture{pages: []logarchive.Page{
		{State: "sealed", Reason: "retained evidence", Records: []logarchive.Record{{Sequence: 1, Kind: "stdout", Text: "before gap\n"}}, Next: 1, HasMore: true},
		{State: "partial", Reason: "segment unavailable", Gaps: []logarchive.Gap{gap}, Records: []logarchive.Record{{Sequence: 3, Kind: "stdout", Text: "after gap\n"}}, Next: 3, HasMore: true},
		{State: "partial", Reason: "segment unavailable", Gaps: []logarchive.Gap{gap}, Next: 4},
	}}
	h := NewHandler(nil, nil, nil)
	h.SetArchiveBackend(backend, backend)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/archive/download", nil)
	c.Set("authorized_archive_identity", logarchive.Identity{RunID: 11})
	h.readArchive(true)(c)
	body := w.Body.String()
	if !strings.Contains(body, "# Collection: partial") || strings.Count(body, "# OUTPUT GAP 2-2:") != 1 || !strings.Contains(body, "after gap") || !strings.Contains(body, "# END OF RETAINED LOG EXPORT") {
		t.Fatalf("later evidence loss was hidden or duplicated: %s", body)
	}
}

func TestArchiveDownloadStopsOnNonAdvancingCursor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	backend := &pagedArchiveFixture{pages: []logarchive.Page{{State: "sealed", Next: 1, HasMore: true}, {State: "sealed", Next: 1, HasMore: true}}}
	h := NewHandler(nil, nil, nil)
	h.SetArchiveBackend(backend, backend)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/archive/download", nil)
	c.Set("authorized_archive_identity", logarchive.Identity{RunID: 11})
	h.readArchive(true)(c)
	if !strings.Contains(w.Body.String(), "cursor did not advance") || strings.Contains(w.Body.String(), "# END OF RETAINED LOG EXPORT") {
		t.Fatalf("stalled export looked complete: %s", w.Body.String())
	}
}
