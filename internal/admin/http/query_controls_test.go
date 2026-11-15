package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	runquery "orbitjob/internal/admin/app/run/query"
	domainfunction "orbitjob/internal/core/domain/function"
)

func TestFunctionSearchScopeBeforePaginationStableTies(t *testing.T) {
	store := &fakeFunctionStore{}
	instant := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	store.seed(
		domainfunction.Definition{ID: 1, TenantID: usecaseTestTenant, ResourceGroupID: "a", Name: "API one", CreatedAt: instant},
		domainfunction.Definition{ID: 2, TenantID: usecaseTestTenant, ResourceGroupID: "a", Name: "other", Description: "API two", CreatedAt: instant},
		domainfunction.Definition{ID: 3, TenantID: usecaseTestTenant, ResourceGroupID: "b", Name: "API foreign group", CreatedAt: instant.Add(time.Hour)},
		domainfunction.Definition{ID: 4, TenantID: usecaseTestTenantB, ResourceGroupID: "a", Name: "API foreign tenant", CreatedAt: instant.Add(time.Hour)},
		domainfunction.Definition{ID: 5, TenantID: usecaseTestTenant, ResourceGroupID: "a", Name: "not matching", CreatedAt: instant.Add(time.Hour)},
	)
	uc := NewListFunctionsUseCase(store)
	for offset, want := range []int64{2, 1} {
		rows, err := uc.List(context.Background(), FunctionListInput{TenantID: usecaseTestTenant, ResourceGroupID: "a", Search: "api", Limit: 1, Offset: offset})
		if err != nil || len(rows) != 1 || rows[0].ID != want {
			t.Fatalf("page %d: %+v %v", offset, rows, err)
		}
	}
	rows, err := uc.List(context.Background(), FunctionListInput{TenantID: usecaseTestTenant, ResourceGroupID: "a", Search: "api", Limit: 1, Offset: 2})
	if err != nil || len(rows) != 0 {
		t.Fatalf("last page: %+v %v", rows, err)
	}
}

type summaryHTTPRepo struct {
	calls int
	seen  runquery.ListInput
}

func (r *summaryHTTPRepo) List(context.Context, runquery.ListInput) ([]runquery.ListItem, error) {
	return []runquery.ListItem{}, nil
}
func (r *summaryHTTPRepo) Summary(_ context.Context, in runquery.ListInput) (runquery.Summary, error) {
	r.calls++
	r.seen = in
	return runquery.Summary{Total: 25, Phases: map[string]int64{"Running": 25}}, nil
}
func TestSummaryHTTPRangeScopeAndTenant(t *testing.T) {
	for _, group := range []string{"", "scoped"} {
		repo := &summaryHTTPRepo{}
		h := NewHandler(nil, nil, nil)
		h.SetListInstancesUseCase(runquery.NewListRunsUseCase(repo))
		router := gin.New()
		router.Use(testPrincipalMiddleware(usecaseTestTenant, "key", group))
		h.Register(router)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/api/v1/instances/summary?phase=Running&source_uid=uid&occurrence_key=occ&created_from=2026-10-19T08%3A00%3A00%2B08%3A00&created_before=2026-10-20T00%3A00%3A00Z", nil))
		if group != "" {
			if resp.Code != 403 || repo.calls != 0 {
				t.Fatalf("scoped summary: %d calls %d", resp.Code, repo.calls)
			}
			continue
		}
		if resp.Code != 200 || repo.seen.TenantID != usecaseTestTenant || repo.seen.CreatedFrom != "2026-10-19T00:00:00Z" || repo.seen.SourceUID != "uid" || repo.seen.OccurrenceKey != "occ" {
			t.Fatalf("summary=%d %s in=%+v", resp.Code, resp.Body, repo.seen)
		}
		var out runquery.Summary
		if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil || out.Total != 25 {
			t.Fatalf("summary %v %+v", err, out)
		}
		resp = httptest.NewRecorder()
		router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/api/v1/instances/summary?tenant_id="+usecaseTestTenantB, nil))
		if resp.Code != 403 || repo.calls != 1 {
			t.Fatalf("foreign tenant summary=%d calls=%d", resp.Code, repo.calls)
		}
		resp = httptest.NewRecorder()
		router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/api/v1/instances/summary?created_from=bad", nil))
		if resp.Code != 400 || repo.calls != 1 {
			t.Fatalf("bad range summary=%d calls=%d", resp.Code, repo.calls)
		}
	}
}
