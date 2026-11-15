package http

import (
	"strconv"
	"strings"

	"orbitjob/internal/admin/http/apperror"
)

type deletedResponse struct {
	Deleted bool `json:"deleted"`
}

// Shared metadata describes the middleware mounted in main, Handler.Register
// and requestTenant. It never changes registration or authorization behavior.
func applyAdminOperationMetadata(route routeDefinition, op *Operation, registry *schemaRegistry) {
	errorSchema := registry.schemaForModel(apperror.ErrorResponse{}, schemaModeResponse)
	failures := map[int]string{
		400: "Malformed request or invalid input; inspect error.code, error.message and optional error.field",
		401: "Missing, invalid, expired or revoked API key",
		413: "Request body exceeds the shared 1 MiB limit",
		500: "Internal error, including unavailable authorization context or permission loading",
	}
	if route.permission != "" || route.path == "/identity" {
		failures[403] = "Authenticated credential lacks the exact target grant, violates its boundary, or cannot select this tenant or resource group"
	}
	for status, description := range failures {
		key := strconv.Itoa(status)
		if _, exists := op.Responses[key]; exists {
			continue
		}
		response := Response{Description: description, Content: map[string]MediaType{"application/json": {Schema: errorSchema}}}
		applyStandardResponseHeaders(&response)
		op.Responses[key] = response
	}
	if op.OperationID == "listSLOBudgets" {
		op.Parameters = append(op.Parameters, parametersFromModel(sloIDURI{})...)
	}
	switch op.OperationID {
	case "deleteCheck", "deleteSLI", "deleteSLO":
		response := op.Responses["200"]
		response.Content = map[string]MediaType{"application/json": {Schema: registry.schemaForModel(deletedResponse{}, schemaModeResponse)}}
		op.Responses["200"] = response
		if op.OperationID == "deleteSLI" {
			op.Description += " The current SLI store returns 404 for either an absent row or a stale version; reload before deciding whether a new delete is appropriate."
		} else {
			conflict := Response{Description: "Version conflict; reload the resource before submitting a new delete intent", Content: map[string]MediaType{"application/json": {Schema: errorSchema}}}
			applyStandardResponseHeaders(&conflict)
			op.Responses["409"] = conflict
		}
	}

	if operationSelectsTenant(route) && !operationHasQuery(op, "tenant_id") {
		op.Parameters = append(op.Parameters, Parameter{Name: "tenant_id", In: "query", Schema: Schema{Type: "string"}})
	}
	for i := range op.Parameters {
		parameter := &op.Parameters[i]
		if parameter.In != "query" {
			continue
		}
		switch parameter.Name {
		case "tenant_id":
			parameter.Schema.Description = "Explicit target tenant for platform credentials; required for tenant-scoped operations with a platform key. Tenant credentials may omit it or repeat their own tenant; selecting another tenant is forbidden. Explicit /tenants/{id}/... management routes use the path target instead. Conflicting body/query selectors are refused."
		case "action":
			parameter.Schema.Description = "Repeat action once per exact capability probe, paired by position with resource; at most 64 pairs. Known concrete actions only, without action wildcards."
		case "resource":
			parameter.Schema.Description = "Repeat resource once per action, in the same order, using an orbitjob:<tenant>:<group>:<type>/<id> ARN. Ordinary targets must match the selected tenant; supported explicit management collection probes may target their own collection."
		case "tail_lines":
			parameter.Schema.Description = "Maximum retained lines in the task-container snapshot; omitted or zero uses 200."
		case "limit_bytes":
			parameter.Schema.Description = "Maximum UTF-8 output bytes; omitted or zero uses 32768. Inspect truncated to detect a bounded result."
		case "wait_seconds":
			parameter.Schema.Description = "Zero or omitted returns asynchronously. A positive value waits up to this many seconds for a terminal CR phase; timeout does not mean failure or cancellation."
		case "expected_version":
			parameter.Schema.Description = "Reviewed Function version; must match both saved definition and active executable revision. A mismatch returns 409 without publishing an older revision."
		}
	}
	if op.Description == "" {
		switch op.OperationID {
		case "getIdentity":
			op.Description = "Authenticate the caller and return credential labels, configured namespace mapping and exact requested policy decisions. Requires no resource grant just to identify the caller. Platform credentials may omit tenant_id for identity only; tenant-specific metadata and ordinary capability targets require an explicit target. Pair repeated action and resource parameters in order, at most 64 pairs. Allowed capability probes do not bypass independent resource-group or runtime checks. Namespace mappings describe configuration, not cluster readiness."
		case "listTenantPolicies":
			op.Description = "List the explicit path tenant's policies and platform presets after policy:List is authorized on that target collection. Ordinary workload queries retain their original tenant scope."
		case "listTenantResourceGroups":
			op.Description = "List resource groups for the explicit path tenant after group:List is authorized on that target collection. Selecting a management target does not switch ordinary workload query scope."
		}
	}

	if op.RequestBody != nil {
		media := op.RequestBody.Content["application/json"]
		media.Example = operationRequestExample(op.OperationID)
		op.RequestBody.Content["application/json"] = media
	}
}

func operationHasQuery(op *Operation, name string) bool {
	for _, parameter := range op.Parameters {
		if parameter.In == "query" && parameter.Name == name {
			return true
		}
	}
	return false
}

func operationSelectsTenant(route routeDefinition) bool {
	// Explicit management routes resolve their path/owner target, and workspace
	// discovery filters caller-visible labels without a business tenant selector.
	if strings.HasPrefix(route.path, "/auth/") || route.path == "/workspaces" || strings.HasPrefix(route.path, "/api_keys/") {
		return false
	}
	return !strings.HasPrefix(route.path, "/tenants/:id/")
}

func operationRequestExample(id string) any {
	switch id {
	case "createCheck":
		return map[string]any{"name": "Payments API health", "check_type": "http_health", "check_config": map[string]any{"url": "https://payments.example.com/health", "method": "GET", "expected_status": 200}, "schedule_type": "interval", "interval_sec": 60}
	case "createSLI":
		return map[string]any{"name": "Payments API readiness", "sli_type": "availability", "source_type": "job_run", "source_config": map[string]any{"source_uid": "check-1"}, "aggregation": "ratio", "good_event_criteria": map[string]any{"status": "success"}}
	case "createSLO":
		return map[string]any{"name": "Payments API availability", "sli_id": 1, "target": 0.99, "window_type": "rolling", "window_duration": "24h"}
	case "createPolicy", "createTenantPolicy":
		return map[string]any{"name": "Read job definitions", "document": map[string]any{"version": "2026-01-01", "statement": []any{map[string]any{"effect": "Allow", "action": []string{"job:List", "job:Get"}, "resource": []string{"orbitjob:self:*:job/*"}}}}}
	case "createTenant":
		return map[string]any{"name": "Commerce Operations", "slug": "commerce-ops"}
	case "createResourceGroup", "createTenantResourceGroup":
		return map[string]any{"name": "Reporting", "slug": "reporting"}
	case "createAPIKey":
		return map[string]any{"policies": []string{"REPLACE_WITH_POLICY_ID"}}
	case "renameAPIKey":
		return map[string]any{"name": "Production operator"}
	case "pauseCheck", "resumeCheck", "deleteCheck", "deleteSLI", "pauseSLO", "resumeSLO", "deleteSLO":
		return map[string]any{"version": 1}
	case "previewWorkflowSchedule":
		return map[string]any{"schedule": "0 9 * * 1-5", "time_zone": "Asia/Shanghai", "count": 5}
	case "createFunction":
		return map[string]any{"name": "disk-usage-snapshot", "image": "busybox:1.37.0", "command": []string{"df"}, "args": []string{"-P", "/"}}
	case "updateFunction":
		return map[string]any{"name": "disk-usage-snapshot", "image": "busybox:1.37.0", "command": []string{"df"}, "args": []string{"-P", "/"}, "status": "paused", "expected_version": 1}
	case "applyjobDefinition":
		return map[string]any{"request_id": "11111111-1111-4111-8111-111111111111", "dry_run": true, "spec_json": `{"schedule":"0 2 * * *","jobTemplate":{"image":"busybox:1.37.0","command":["df"],"args":["-P","/"]}}`}
	case "applyworkflowDefinition":
		return map[string]any{"request_id": "11111111-1111-4111-8111-111111111111", "dry_run": true, "spec_json": `{"tasks":[{"name":"extract","jobRef":{"name":"billing-extract"}},{"name":"publish","jobRef":{"name":"billing-publish"},"dependsOn":["extract"]}]}`}
	}
	return nil
}
