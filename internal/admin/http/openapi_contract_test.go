package http

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"orbitjob/internal/admin/definition"
	checkdomain "orbitjob/internal/core/domain/check"
	functiondomain "orbitjob/internal/core/domain/function"
	"orbitjob/internal/core/domain/policy"
	slidomain "orbitjob/internal/core/domain/sli"
	slodomain "orbitjob/internal/core/domain/slo"
)

func TestOpenAPIRegisteredRoutesAndPathParameters(t *testing.T) {
	doc := ServiceOpenAPIDocument()
	ids := map[string]string{}
	count := 0
	for _, route := range adminAPIRoutes() {
		// Register the real closure without invoking a handler or a backend.
		router := gin.New()
		route.register(router.Group(adminAPIPrefix), &Handler{})
		registered := router.Routes()
		if len(registered) != 1 || registered[0].Method != route.method || registered[0].Path != adminAPIPrefix+route.path {
			t.Fatalf("metadata disagrees with registered route: %+v vs %+v", route, registered)
		}
		path := toOpenAPIPath(registered[0].Path)
		item := doc.Paths[path]
		op := contractOperation(item, route.method)
		if op == nil || op.OperationID == "" || op.Summary == "" || op.Description == "" || len(op.Tags) == 0 {
			t.Fatalf("%s %s lacks usable operation documentation: %+v", route.method, path, op)
		}
		if previous, duplicate := ids[op.OperationID]; duplicate {
			t.Fatalf("duplicate operation ID %s at %s and %s", op.OperationID, previous, path)
		}
		ids[op.OperationID] = path
		seen := map[string]bool{}
		for _, parameter := range op.Parameters {
			key := parameter.In + ":" + parameter.Name
			if seen[key] {
				t.Fatalf("%s %s repeats %s", route.method, path, key)
			}
			seen[key] = true
			if parameter.In == "path" && (!parameter.Required || !strings.Contains(path, "{"+parameter.Name+"}")) {
				t.Fatalf("invalid path parameter %s on %s", parameter.Name, path)
			}
		}
		for _, segment := range strings.Split(path, "/") {
			if strings.HasPrefix(segment, "{") && !seen["path:"+strings.Trim(segment, "{}")] {
				t.Fatalf("%s lacks parameter schema for %s", path, segment)
			}
		}
		count++
	}
	t.Logf("checked %d registered admin operations and %d total paths", count, len(doc.Paths))
}

func TestOpenAPISharedFailuresAndTenantSelectors(t *testing.T) {
	doc := ServiceOpenAPIDocument()
	for _, route := range adminAPIRoutes() {
		op := contractOperation(doc.Paths[toOpenAPIPath(adminAPIPrefix+route.path)], route.method)
		for _, status := range []string{"400", "401", "413", "429", "500"} {
			response, ok := op.Responses[status]
			if !ok || response.Content["application/json"].Schema.Ref != "#/components/schemas/ErrorResponse" {
				t.Errorf("%s %s lacks shared %s error contract", route.method, route.path, status)
			}
		}
		if route.permission != "" || route.path == "/identity" {
			if _, ok := op.Responses["403"]; !ok {
				t.Errorf("%s %s lacks authorization refusal", route.method, route.path)
			}
		}
	}
	for _, path := range []string{
		"/jobs/{id}/trigger", "/instances", "/instances/{run_id}", "/instances/summary",
		"/instances/{run_id}/cancel", "/instances/{run_id}/attempts", "/slis", "/slis/{id}",
		"/slos", "/slos/{id}", "/slo-alerts/{id}", "/policies", "/resource_groups",
		"/functions/{id}/invoke", "/functions/{id}/runs/{run_id}",
		"/workflows/{id}/runs", "/workflows/{id}/runs/{run_id}/cancel", "/workflows/schedule-preview",
	} {
		item := doc.Paths[adminAPIPrefix+path]
		for _, op := range []*Operation{item.Get, item.Post, item.Put, item.Patch, item.Delete} {
			if op != nil && !hasParameter(op.Parameters, "tenant_id", "query") {
				t.Errorf("%s lacks the platform caller's tenant selector", op.OperationID)
			}
		}
	}
}

func TestOpenAPISchemasDescribeValidatedInputs(t *testing.T) {
	doc := ServiceOpenAPIDocument()
	schemas := doc.Components.Schemas
	slo := schemas["CreateSLORequest"]
	if min := slo.Properties["target"].Minimum; min == nil || *min != slodomain.MinTarget {
		t.Errorf("SLO target minimum lost its fractional bound: %+v", min)
	}
	if min := slo.Properties["alert_fast_burn_rate"].Minimum; min == nil || *min != 0.1 {
		t.Errorf("burn rate minimum lost its fractional bound: %+v", min)
	}
	check := schemas["CreateCheckRequest"]
	config := check.Properties["check_config"]
	if !containsString(check.Required, "check_config") || !containsString(config.Required, "url") || config.Properties["url"].Description == "" {
		t.Fatalf("check_config must expose its required probe URL: %+v", config)
	}
	sli := schemas["CreateSLIRequest"]
	if !containsString(sli.Required, "source_config") || !containsString(sli.Properties["source_config"].Required, "source_uid") {
		t.Fatalf("SLI source must expose the required definition UID: %+v", sli)
	}
	document := schemas["CreatePolicyRequest"].Properties["document"]
	if document.AdditionalProperties != false || !containsString(document.Required, "statement") {
		t.Fatalf("policy document must describe supported lowercase fields: %+v", document)
	}
	for _, name := range []string{"FunctionDefinitionRequest", "UpdateFunctionRequest", "ApplyRequest"} {
		if schemas[name].AdditionalProperties != false {
			t.Errorf("%s must expose the runtime unknown-field rejection", name)
		}
	}
}

func TestOpenAPIRequestExamplesPassDomainValidation(t *testing.T) {
	doc := ServiceOpenAPIDocument()
	example := func(path string, target any) {
		t.Helper()
		op := doc.Paths[adminAPIPrefix+path].Post
		if op == nil || op.RequestBody == nil {
			t.Fatalf("missing POST body for %s", path)
		}
		media := op.RequestBody.Content["application/json"]
		if media.Example == nil {
			t.Fatalf("missing runnable request example for %s", path)
		}
		raw, err := json.Marshal(media.Example)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, target); err != nil {
			t.Fatalf("example does not match request DTO: %v", err)
		}
	}
	var check CreateCheckRequest
	example("/checks", &check)
	in := check.ToCreateInput()
	in.TenantID = "01J00000000000000000000000"
	if _, err := checkdomain.NormalizeCreate(time.Now().UTC(), checkdomain.CreateInput{
		Name: in.Name, TenantID: in.TenantID, CheckType: in.CheckType, CheckConfig: in.CheckConfig,
		ScheduleType: in.ScheduleType, IntervalSec: in.IntervalSec,
	}); err != nil {
		t.Fatalf("check example is unusable: %v", err)
	}
	var sli CreateSLIRequest
	example("/slis", &sli)
	if _, err := slidomain.NormalizeCreate(slidomain.CreateInput{Name: sli.Name, SLIType: sli.SLIType, SourceType: sli.SourceType, SourceConfig: sli.SourceConfig, GoodEventCriteria: sli.GoodEventCriteria}); err != nil {
		t.Fatalf("SLI example is unusable: %v", err)
	}
	var slo CreateSLORequest
	example("/slos", &slo)
	sloIn, err := slo.ToCreateInput()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := slodomain.NormalizeCreate(slodomain.CreateInput{Name: sloIn.Name, SLIID: sloIn.SLIID, Target: sloIn.Target, WindowDuration: sloIn.WindowDuration}); err != nil {
		t.Fatalf("SLO example is unusable: %v", err)
	}
	var p CreatePolicyRequest
	example("/policies", &p)
	if _, err := policy.ParseDocument(p.Document); err != nil {
		t.Fatalf("policy example is unusable: %v", err)
	}
	var function FunctionDefinitionRequest
	example("/functions", &function)
	if _, err := functiondomain.NormalizeManagedDefinition(function.definition(in.TenantID)); err != nil {
		t.Fatalf("Function example is unusable: %v", err)
	}
	for _, kind := range []string{"jobs", "workflows"} {
		op := doc.Paths[adminAPIPrefix+"/definitions/"+kind+"/{namespace}/{name}"].Put
		var request definition.ApplyRequest
		raw, err := json.Marshal(op.RequestBody.Content["application/json"].Example)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &request); err != nil {
			t.Fatal(err)
		}
		if !request.DryRun || request.RequestID == "" {
			t.Fatalf("typed example must validate before saving: %+v", request)
		}
		if _, _, err := definition.CanonicalSpec(kind, request.SpecJSON); err != nil {
			t.Fatalf("%s typed example is unusable: %v", kind, err)
		}
	}
}

func TestOpenAPIReferenceClosureAndDeletionReceipts(t *testing.T) {
	doc := ServiceOpenAPIDocument()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	var visit func(any)
	visit = func(value any) {
		switch value := value.(type) {
		case map[string]any:
			if reference, ok := value["$ref"].(string); ok {
				name, valid := strings.CutPrefix(reference, "#/components/schemas/")
				if _, found := doc.Components.Schemas[name]; !valid || !found {
					t.Errorf("unresolved schema reference %q", reference)
				}
			}
			for _, child := range value {
				visit(child)
			}
		case []any:
			for _, child := range value {
				visit(child)
			}
		}
	}
	visit(value)
	for _, path := range []string{"checks", "slis", "slos"} {
		op := doc.Paths[adminAPIPrefix+"/"+path+"/{id}"].Delete
		ref := op.Responses["200"].Content["application/json"].Schema.Ref
		name := strings.TrimPrefix(ref, "#/components/schemas/")
		if doc.Components.Schemas[name].Properties["deleted"].Type != "boolean" {
			t.Errorf("%s delete response loses its actual deleted receipt", path)
		}
		_, conflicts := op.Responses["409"]
		if conflicts != (path != "slis") {
			t.Errorf("%s stale-delete contract disagrees with its actual store", path)
		}
	}
}

func contractOperation(item PathItem, method string) *Operation {
	return map[string]*Operation{http.MethodGet: item.Get, http.MethodPost: item.Post, http.MethodPut: item.Put, http.MethodPatch: item.Patch, http.MethodDelete: item.Delete}[method]
}
