package http

import "testing"

func TestHandler_OpenAPIDocument(t *testing.T) {
	handler := NewHandler(
		&stubCreateJobUseCase{},
		&stubListJobsUseCase{},
		&stubGetJobUseCase{},
		&stubUpdateJobUseCase{},
		&stubChangeStatusUseCase{},
	)

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

	jobsPath, ok := doc.Paths["/api/v1/jobs"]
	if !ok {
		t.Fatal("expected /api/v1/jobs path to be documented")
	}
	if jobsPath.Get == nil {
		t.Fatal("expected GET /api/v1/jobs operation")
	}
	if jobsPath.Post == nil {
		t.Fatal("expected POST /api/v1/jobs operation")
	}
	if !hasParameter(jobsPath.Get.Parameters, "tenant_id", "query") {
		t.Fatalf("expected tenant_id query parameter, got %+v", jobsPath.Get.Parameters)
	}
	if got := parameterSchema(jobsPath.Get.Parameters, "tenant_id", "query").Default; got != "default" {
		t.Fatalf("expected tenant_id default=%q, got %+v", "default", got)
	}
	if got := parameterSchema(jobsPath.Get.Parameters, "limit", "query").Default; got != 50 {
		t.Fatalf("expected limit default=%d, got %+v", 50, got)
	}
	if !hasTraceHeader(jobsPath.Get.Responses["200"]) {
		t.Fatalf("expected %s response header on GET /api/v1/jobs", traceIDHeaderName)
	}

	createSchema, ok := doc.Components.Schemas["CreateJobRequest"]
	if !ok {
		t.Fatal("expected CreateJobRequest schema")
	}
	if !containsString(createSchema.Required, "name") {
		t.Fatalf("expected name to be required, got %+v", createSchema.Required)
	}
	if !containsString(createSchema.Required, "trigger_type") {
		t.Fatalf("expected trigger_type to be required, got %+v", createSchema.Required)
	}
	if !containsString(createSchema.Required, "handler_type") {
		t.Fatalf("expected handler_type to be required, got %+v", createSchema.Required)
	}
	if got := createSchema.Properties["trigger_type"].Enum; len(got) != 2 || got[0] != "cron" || got[1] != "manual" {
		t.Fatalf("expected trigger_type enum [cron manual], got %+v", got)
	}
	if got := createSchema.Properties["timezone"].Default; got != "UTC" {
		t.Fatalf("expected timezone default=%q, got %+v", "UTC", got)
	}
	if got := createSchema.Properties["timeout_sec"].Default; got != 60 {
		t.Fatalf("expected timeout_sec default=%d, got %+v", 60, got)
	}
	if createSchema.Properties["cron_expr"].Description == "" {
		t.Fatal("expected cron_expr to contain conditional validation description")
	}

	updatePath, ok := doc.Paths["/api/v1/jobs/{id}"]
	if !ok {
		t.Fatal("expected /api/v1/jobs/{id} path to be documented")
	}
	if updatePath.Put == nil {
		t.Fatal("expected PUT /api/v1/jobs/{id} operation")
	}
	if updatePath.Put.RequestBody == nil {
		t.Fatal("expected update operation request body")
	}
	if got := updatePath.Put.RequestBody.Content["application/json"].Schema.Ref; got != "#/components/schemas/UpdateJobRequest" {
		t.Fatalf("expected update request schema ref, got %q", got)
	}
	if !hasParameter(updatePath.Put.Parameters, "X-Actor-ID", "header") {
		t.Fatalf("expected X-Actor-ID header parameter, got %+v", updatePath.Put.Parameters)
	}
	if !hasParameter(updatePath.Put.Parameters, traceIDHeaderName, "header") {
		t.Fatalf("expected %s header parameter, got %+v", traceIDHeaderName, updatePath.Put.Parameters)
	}
	if got := updatePath.Put.Responses["409"].Content["application/json"].Schema.Ref; got != "#/components/schemas/ErrorResponse" {
		t.Fatalf("expected conflict response schema ref, got %q", got)
	}
	if !hasTraceHeader(updatePath.Put.Responses["409"]) {
		t.Fatalf("expected %s response header on 409", traceIDHeaderName)
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
