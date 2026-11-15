package http

import (
	"context"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	sliquery "orbitjob/internal/admin/app/sli/query"
	sloquery "orbitjob/internal/admin/app/slo/query"
)

type capturedSLISearch struct{ input sliquery.ListInput }

func (s *capturedSLISearch) List(_ context.Context, in sliquery.ListInput) (sliquery.ListResult, error) {
	s.input = in
	return sliquery.ListResult{}, nil
}

type capturedSLOSearch struct{ input sloquery.ListInput }

func (s *capturedSLOSearch) List(_ context.Context, in sloquery.ListInput) (sloquery.ListResult, error) {
	s.input = in
	return sloquery.ListResult{}, nil
}

func TestMonitoringHandlersForwardLiteralSearchAndAuthorizedScope(t *testing.T) {
	checks, slis, slos := &stubListChecksUseCase{}, &capturedSLISearch{}, &capturedSLOSearch{}
	h := NewHandler(nil, nil, nil)
	h.SetListChecksUseCase(checks)
	h.SetListSLIsUseCase(slis)
	h.SetListSLOsUseCase(slos)
	router := gin.New()
	router.Use(testPrincipalMiddleware(usecaseTestTenant, "key", "group-a"))
	h.Register(router)
	const literal = "API_%+& \u641c\u7d22"
	for _, path := range []string{"/checks", "/slis", "/slos"} {
		serveQuery(t, router, "/api/v1"+path, url.Values{"search": {literal}, "limit": {"7"}, "offset": {"21"}})
	}
	if checks.in.Search != literal || checks.in.ResourceGroupID != "group-a" || checks.in.Offset != 21 || checks.in.Limit != 7 {
		t.Fatalf("check query=%+v", checks.in)
	}
	if slis.input.Search != literal || slis.input.ResourceGroupID != "group-a" || slis.input.Offset != 21 || slis.input.Limit != 7 {
		t.Fatalf("SLI query=%+v", slis.input)
	}
	if slos.input.Search != literal || slos.input.ResourceGroupID != "group-a" || slos.input.Offset != 21 || slos.input.Limit != 7 {
		t.Fatalf("SLO query=%+v", slos.input)
	}
}
