package http

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"

	jobquery "orbitjob/internal/admin/app/job/query"
	runquery "orbitjob/internal/admin/app/run/query"
)

type capturedJobList struct {
	in     jobquery.ListInput
	called bool
}

func (s *capturedJobList) List(_ context.Context, in jobquery.ListInput) ([]jobquery.ListItem, error) {
	s.in = in
	s.called = true
	return []jobquery.ListItem{}, nil
}

func serveQuery(t *testing.T, handler stdhttp.Handler, path string, query url.Values) {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(stdhttp.MethodGet, path+"?"+query.Encode(), nil))
	if response.Code != stdhttp.StatusOK {
		t.Fatalf("%s: HTTP %d: %s", path, response.Code, response.Body.String())
	}
}

// Exercise the registered HTTP route, not just the request DTO converter:
// the real Jobs handler previously discarded Search while rebuilding its input.
func TestHandler_ListJobsForwardsLiteralSearch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, literal := range []string{"%", "API_%+& \u641c\u7d22"} {
		t.Run(literal, func(t *testing.T) {
			uc := &capturedJobList{}
			handler := NewHandler(uc, nil, nil)
			router := gin.New()
			router.Use(testPrincipalMiddleware(usecaseTestTenant, "key", ""))
			handler.Register(router)
			serveQuery(t, router, "/api/v1/jobs", url.Values{"search": {literal}, "namespace": {"prod"}, "source_uid": {"uid-1"}, "limit": {"7"}, "offset": {"21"}})
			if !uc.called || uc.in.Search != literal || uc.in.Namespace != "prod" || uc.in.SourceUID != "uid-1" || uc.in.TenantID != usecaseTestTenant || uc.in.Limit != 7 || uc.in.Offset != 21 {
				t.Fatalf("handler lost a literal query predicate: called=%v input=%+v", uc.called, uc.in)
			}
		})
	}
}

func TestHandler_FunctionWorkflowSearchForwarding(t *testing.T) {
	const literal = "API_%+& \u641c\u7d22"
	functions := &stubListFunctions{}
	_, functionRouter := newFunctionHandler(functions, nil, nil, nil, nil, "group-9")
	workflows := &stubListWorkflows{}
	_, workflowRouter := newWorkflowHandler(workflows, nil, nil, nil, nil, nil, "")
	query := url.Values{"search": {literal}, "limit": {"7"}, "offset": {"21"}}
	serveQuery(t, functionRouter, "/api/v1/functions", query)
	serveQuery(t, workflowRouter, "/api/v1/workflows", query)
	if !functions.called || functions.in.Search != literal || functions.in.Offset != 21 || functions.in.Limit != 7 || functions.in.ResourceGroupID != "group-9" {
		t.Fatalf("function query lost: %+v", functions.in)
	}
	if !workflows.called || workflows.in.Search != literal || workflows.in.Offset != 21 || workflows.in.Limit != 7 {
		t.Fatalf("workflow query lost: %+v", workflows.in)
	}
}

type capturedSummaryList struct {
	stubListRunsUseCase
	summaryInput  runquery.ListInput
	summaryCalled bool
}

func (s *capturedSummaryList) Summary(_ context.Context, in runquery.ListInput) (runquery.Summary, error) {
	s.summaryInput = in
	s.summaryCalled = true
	return runquery.Summary{Phases: map[string]int64{}}, nil
}
func TestHandler_AllHistoryBoundsAndPaginationForwarding(t *testing.T) {
	const from = "2026-10-19T08:00:00.123456+08:00"
	const before = "2026-10-20T00:00:00.654321Z"
	query := url.Values{"created_from": {from}, "created_before": {before}, "limit": {"7"}, "offset": {"21"}}
	functions := &stubListFunctionRuns{}
	_, functionRouter := newFunctionHandler(nil, nil, nil, functions, nil, "group-9")
	workflows := &stubListWorkflowRuns{}
	_, workflowRouter := newWorkflowHandler(nil, nil, nil, workflows, nil, nil, "")
	instances := &capturedSummaryList{}
	handler := NewHandler(nil, nil, nil)
	handler.SetListInstancesUseCase(instances)
	router := gin.New()
	router.Use(testPrincipalMiddleware(usecaseTestTenant, "key", ""))
	handler.Register(router)
	serveQuery(t, functionRouter, "/api/v1/functions/27/runs", query)
	serveQuery(t, workflowRouter, "/api/v1/workflows/412/runs", query)
	if !functions.called || functions.in.CreatedFrom != from || functions.in.CreatedBefore != before || functions.in.FunctionID != 27 || functions.in.Offset != 21 || functions.in.Limit != 7 || functions.in.ResourceGroupID != "group-9" {
		t.Fatalf("function history query lost: %+v", functions.in)
	}
	if !workflows.called || workflows.in.CreatedFrom != from || workflows.in.CreatedBefore != before || workflows.in.WorkflowID != 412 || workflows.in.Offset != 21 || workflows.in.Limit != 7 {
		t.Fatalf("workflow history query lost: %+v", workflows.in)
	}
	query.Set("source_uid", "source-exact")
	query.Set("occurrence_key", "occurrence-exact")
	query.Set("phase", "Running")
	serveQuery(t, router, "/api/v1/instances", query)
	serveQuery(t, router, "/api/v1/instances/summary", query)
	for label, in := range map[string]runquery.ListInput{"list": instances.in, "summary": instances.summaryInput} {
		if in.CreatedFrom != from || in.CreatedBefore != before || in.SourceUID != "source-exact" || in.OccurrenceKey != "occurrence-exact" || in.Phase != "Running" || in.TenantID != usecaseTestTenant {
			t.Fatalf("%s history predicate lost: %+v", label, in)
		}
	}
	if !instances.called || !instances.summaryCalled || instances.in.Offset != 21 || instances.in.Limit != 7 || instances.summaryInput.Offset != 0 || instances.summaryInput.Limit != 0 {
		t.Fatalf("ledger pagination divergence: list=%+v summary=%+v", instances.in, instances.summaryInput)
	}
}
