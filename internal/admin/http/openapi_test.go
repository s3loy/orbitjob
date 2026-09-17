package http

import (
	"context"
	"strings"
	"testing"

	jobcommand "orbitjob/internal/admin/app/job/command"
	query "orbitjob/internal/admin/app/job/query"
)

// stubListJobsForDoc is a non-nil list use case, which is what makes the
// handler document and register the /jobs read routes. The trigger stub does
// the same for the manual-run route.
type stubListJobsForDoc struct{}

func (s *stubListJobsForDoc) List(context.Context, query.ListInput) ([]query.ListItem, error) {
	return nil, nil
}

type stubTriggerJobForDoc struct{}

func (s *stubTriggerJobForDoc) Trigger(context.Context, jobcommand.TriggerInput) (jobcommand.TriggerResult, error) {
	return jobcommand.TriggerResult{}, nil
}

func TestHandler_OpenAPIDocument(t *testing.T) {
	handler := NewHandler(&stubListJobsForDoc{}, nil, &stubTriggerJobForDoc{})

	doc := handler.OpenAPIDocument()

	if doc.OpenAPI != "3.0.3" {
		t.Fatalf("expected openapi version=%q, got %q", "3.0.3", doc.OpenAPI)
	}
	if doc.Components == nil {
		t.Fatal("expected components to be present")
	}

	healthzPath, ok := doc.Paths["/healthz"]
	if !ok || healthzPath.Get == nil {
		t.Fatal("expected GET /healthz operation")
	}
	if !hasParameter(healthzPath.Get.Parameters, traceIDHeaderName, "header") {
		t.Fatalf("expected %s header parameter on /healthz", traceIDHeaderName)
	}
	if !hasTraceHeader(healthzPath.Get.Responses["200"]) {
		t.Fatalf("expected %s response header on /healthz", traceIDHeaderName)
	}

	metricsPath, ok := doc.Paths["/metrics"]
	if !ok || metricsPath.Get == nil {
		t.Fatal("expected GET /metrics operation")
	}
	if _, ok := metricsPath.Get.Responses["200"].Content["text/plain"]; !ok {
		t.Fatalf("expected /metrics response content type text/plain, got %+v", metricsPath.Get.Responses["200"].Content)
	}

	// A job definition is a projected Custom Resource: the read side is GET
	// only. Creation and mutation happen in Kubernetes, so a POST on the
	// collection must stay undocumented rather than imply a route that would
	// write the ledger.
	jobsPath, ok := doc.Paths["/api/v1/jobs"]
	if !ok {
		t.Fatal("expected /api/v1/jobs path to be documented")
	}
	if jobsPath.Get == nil {
		t.Fatal("expected GET /api/v1/jobs operation")
	}
	if jobsPath.Post != nil {
		t.Fatal("POST /api/v1/jobs must not be documented: definitions are not created over HTTP")
	}
	if !hasParameter(jobsPath.Get.Parameters, "tenant_id", "query") {
		t.Fatalf("expected tenant_id query parameter, got %+v", jobsPath.Get.Parameters)
	}
	if got := parameterSchema(jobsPath.Get.Parameters, "tenant_id", "query").Default; got != nil {
		t.Fatalf("expected no tenant_id default, got %+v", got)
	}
	if got := parameterSchema(jobsPath.Get.Parameters, "limit", "query").Default; got != 50 {
		t.Fatalf("expected limit default=%d, got %+v", 50, got)
	}
	if !hasTraceHeader(jobsPath.Get.Responses["200"]) {
		t.Fatalf("expected %s response header on GET /api/v1/jobs", traceIDHeaderName)
	}

	triggerPath, ok := doc.Paths["/api/v1/jobs/{id}/trigger"]
	if !ok {
		t.Fatal("expected /api/v1/jobs/{id}/trigger path to be documented")
	}
	if triggerPath.Post == nil {
		t.Fatal("expected POST /api/v1/jobs/{id}/trigger operation")
	}
	if !hasParameter(triggerPath.Post.Parameters, "id", "path") {
		t.Fatalf("expected id path parameter, got %+v", triggerPath.Post.Parameters)
	}
	if !hasTraceHeader(triggerPath.Post.Responses["200"]) {
		t.Fatalf("expected %s response header on POST trigger", traceIDHeaderName)
	}
}

func TestServiceOpenAPIDocument_IncludesAdminRoutesWithoutHandler(t *testing.T) {
	doc := ServiceOpenAPIDocument()

	if _, ok := doc.Paths["/api/v1/jobs"]; !ok {
		t.Fatal("expected /api/v1/jobs path to be present in service OpenAPI document")
	}
	if _, ok := doc.Paths["/openapi.json"]; !ok {
		t.Fatal("expected /openapi.json path to be present in service OpenAPI document")
	}
}

func TestOpenAPIDocument_ErrorCodeEnum(t *testing.T) {
	doc := ServiceOpenAPIDocument()

	apiErrorSchema, ok := doc.Components.Schemas["APIError"]
	if !ok {
		t.Fatal("expected APIError schema")
	}
	codeSchema, ok := apiErrorSchema.Properties["code"]
	if !ok {
		t.Fatal("expected APIError.code property")
	}
	want := []string{
		"MALFORMED_REQUEST",
		"VALIDATION_ERROR",
		"UNAUTHORIZED",
		"FORBIDDEN",
		"NOT_FOUND",
		"CONFLICT",
		"RATE_LIMITED",
		"INTERNAL_ERROR",
		"SERVICE_UNAVAILABLE",
	}
	if len(codeSchema.Enum) != len(want) {
		t.Fatalf("expected code enum length %d, got %d: %v", len(want), len(codeSchema.Enum), codeSchema.Enum)
	}
	for i, v := range want {
		if codeSchema.Enum[i] != v {
			t.Fatalf("expected code enum[%d]=%q, got %q", i, v, codeSchema.Enum[i])
		}
	}
}

func TestOpenAPIDocument_AdminRoutesHaveUnauthorizedAndRateLimited(t *testing.T) {
	doc := ServiceOpenAPIDocument()

	for path, item := range doc.Paths {
		if !strings.HasPrefix(path, "/api/v1/") {
			continue
		}
		for method, op := range map[string]*Operation{
			"GET":    item.Get,
			"POST":   item.Post,
			"PUT":    item.Put,
			"DELETE": item.Delete,
		} {
			if op == nil {
				continue
			}
			if _, ok := op.Responses["401"]; !ok {
				t.Fatalf("expected 401 response for %s %s", method, path)
			}
			resp429, ok := op.Responses["429"]
			if !ok {
				t.Fatalf("expected 429 response for %s %s", method, path)
			}
			if resp429.Headers == nil {
				t.Fatalf("expected 429 headers for %s %s", method, path)
			}
			if _, ok := resp429.Headers["Retry-After"]; !ok {
				t.Fatalf("expected Retry-After header on 429 for %s %s", method, path)
			}
		}
	}
}

func TestOpenAPIDocument_ListAttempts(t *testing.T) {
	doc := ServiceOpenAPIDocument()

	attemptsPath, ok := doc.Paths["/api/v1/instances/{run_id}/attempts"]
	if !ok {
		t.Fatal("expected /api/v1/instances/{run_id}/attempts path to be documented")
	}
	if attemptsPath.Get == nil {
		t.Fatal("expected GET /api/v1/instances/{run_id}/attempts operation")
	}
	if !hasParameter(attemptsPath.Get.Parameters, "run_id", "path") {
		t.Fatalf("expected run_id path parameter, got %+v", attemptsPath.Get.Parameters)
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}

	return false
}

func hasParameter(parameters []Parameter, name string, location string) bool {
	for _, parameter := range parameters {
		if parameter.Name == name && parameter.In == location {
			return true
		}
	}

	return false
}

func parameterSchema(parameters []Parameter, name string, location string) Schema {
	for _, parameter := range parameters {
		if parameter.Name == name && parameter.In == location {
			return parameter.Schema
		}
	}

	return Schema{}
}

func hasTraceHeader(response Response) bool {
	if response.Headers == nil {
		return false
	}

	header, ok := response.Headers[traceIDHeaderName]
	if !ok {
		return false
	}

	return header.Schema.Type == "string"
}
