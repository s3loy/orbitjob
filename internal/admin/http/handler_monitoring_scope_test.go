package http

import (
	"context"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	checkrunquery "orbitjob/internal/admin/app/checkrun/query"
	alertquery "orbitjob/internal/admin/app/sloalert/query"
	budgetquery "orbitjob/internal/admin/app/slobudget/query"
	"orbitjob/internal/platform/scan"
	"testing"
)

type scopedMonitoringGet[T any] func(context.Context, string, string, int64) (T, error)

func (f scopedMonitoringGet[T]) Get(ctx context.Context, tenant, group string, id int64) (T, error) {
	return f(ctx, tenant, group, id)
}

type scopedMonitoringList[I, O any] func(context.Context, I) (O, error)

func (f scopedMonitoringList[I, O]) List(ctx context.Context, in I) (O, error) { return f(ctx, in) }

// The repositories enforce scope through the parent Check/SLO, including empty
// results and counts. HTTP binding must forward trusted identity to those reads.
func TestMonitoringReadsPreserveTrustedGroupScope(t *testing.T) {
	tenant, actor, group := scan.GenerateID(), scan.GenerateID(), scan.GenerateID()
	calls := 0
	checkScope := func(gotTenant, gotGroup string) {
		t.Helper()
		calls++
		if gotTenant != tenant || gotGroup != group {
			t.Fatalf("query scope=%s/%s want=%s/%s", gotTenant, gotGroup, tenant, group)
		}
	}
	h := &Handler{
		getCheckRunUC: scopedMonitoringGet[checkrunquery.GetResult](func(_ context.Context, tenant, group string, _ int64) (checkrunquery.GetResult, error) {
			checkScope(tenant, group)
			return checkrunquery.GetResult{}, nil
		}),
		listCheckRunsUC: scopedMonitoringList[checkrunquery.ListCheckRunsInput, checkrunquery.ListCheckRunsResult](func(_ context.Context, in checkrunquery.ListCheckRunsInput) (checkrunquery.ListCheckRunsResult, error) {
			checkScope(in.TenantID, in.ResourceGroupID)
			return checkrunquery.ListCheckRunsResult{}, nil
		}),
		getBudgetUC: scopedMonitoringGet[budgetquery.GetItem](func(_ context.Context, tenant, group string, _ int64) (budgetquery.GetItem, error) {
			checkScope(tenant, group)
			return budgetquery.GetItem{}, nil
		}),
		listBudgetHistoryUC: scopedMonitoringList[budgetquery.ListInput, budgetquery.ListResult](func(_ context.Context, in budgetquery.ListInput) (budgetquery.ListResult, error) {
			checkScope(in.TenantID, in.ResourceGroupID)
			return budgetquery.ListResult{}, nil
		}),
		getAlertUC: scopedMonitoringGet[alertquery.GetItem](func(_ context.Context, tenant, group string, _ int64) (alertquery.GetItem, error) {
			checkScope(tenant, group)
			return alertquery.GetItem{}, nil
		}),
		listAlertsUC: scopedMonitoringList[alertquery.ListInput, alertquery.ListResult](func(_ context.Context, in alertquery.ListInput) (alertquery.ListResult, error) {
			checkScope(in.TenantID, in.ResourceGroupID)
			return alertquery.ListResult{}, nil
		}),
	}
	cases := []struct {
		path, url string
		handle    gin.HandlerFunc
	}{
		{"/check-runs", "/check-runs", h.ListCheckRuns},
		{"/check-runs/:id", "/check-runs/1", h.GetCheckRun},
		{"/slos/:id/budget", "/slos/1/budget", h.GetSLOBudget},
		{"/slos/:id/budgets", "/slos/1/budgets", h.ListSLOBudgets},
		{"/slo-alerts", "/slo-alerts", h.ListSLOAlerts},
		{"/slo-alerts/:id", "/slo-alerts/1", h.GetSLOAlert},
	}
	for _, tc := range cases {
		t.Run(tc.url, func(t *testing.T) {
			before := calls
			router := gin.New()
			router.Use(testPrincipalMiddleware(tenant, actor, group))
			router.GET(tc.path, tc.handle)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tc.url, nil))
			if response.Code != http.StatusOK || calls != before+1 {
				t.Fatalf("got %d: %s", response.Code, response.Body.String())
			}
		})
	}
}
