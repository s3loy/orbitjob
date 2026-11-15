package http

import (
	"reflect"

	"orbitjob/internal/admin/definition"
	checkdomain "orbitjob/internal/core/domain/check"
	functiondomain "orbitjob/internal/core/domain/function"
	slidomain "orbitjob/internal/core/domain/sli"
	slodomain "orbitjob/internal/core/domain/slo"
	"orbitjob/internal/domain/workflowschedule"
)

// DTO binding tags remain authoritative for structural constraints. These
// additions describe validation/defaults owned by the domain or typed decoder.
func applyInputSchemaMetadata(t reflect.Type, mode schemaMode, schema *Schema) {
	if mode != schemaModeRequest {
		return
	}
	switch t {
	case reflect.TypeOf(CreateCheckRequest{}):
		schema.Required = append(schema.Required, "check_config")
		schema.Properties["check_config"] = Schema{
			Type: "object", AdditionalProperties: true, Required: []string{"url"},
			Description: "HTTP probe configuration. Only url, method and expected_status affect execution; extra stored keys are inert metadata. The probe has no request body, header injection or arbitrary response parser.",
			Properties: map[string]Schema{
				"url":             {Type: "string", Format: "uri", MinLength: intPointer(1), MaxLength: intPointer(checkdomain.MaxProbeURLLength), Description: "Required absolute http or https URL with a host."},
				"method":          {Type: "string", Nullable: true, Default: checkdomain.DefaultProbeMethod, Description: "GET, HEAD or OPTIONS; normalized by trimming and uppercasing. An empty or null value uses GET."},
				"expected_status": {Type: "integer", Nullable: true, Default: checkdomain.DefaultExpectedStatus, Minimum: numberPointer(checkdomain.MinExpectedStatus), Maximum: numberPointer(checkdomain.MaxExpectedStatus)},
			},
		}
		describeProperty(schema, "schedule_type", "cron requires cron_expr; interval requires interval_sec. Inactive schedule fields are ignored.", checkdomain.ScheduleTypeCron)
		describeProperty(schema, "cron_expr", "Required for cron schedules; robfig ParseStandard syntax, at most 128 characters.", nil)
		describeProperty(schema, "interval_sec", "Required for interval schedules; every occurrence is a Kubernetes Job, with a minimum interval of 30 seconds.", nil)
		describeProperty(schema, "timezone", "IANA time zone used when evaluating the schedule.", checkdomain.DefaultTimezone)
		describeProperty(schema, "timeout_sec", "Omitted or zero uses the domain default.", checkdomain.DefaultTimeoutSec)
		describeProperty(schema, "retry_limit", "Omitted or zero uses the domain default.", checkdomain.DefaultRetryLimit)
		describeProperty(schema, "priority", "Omitted or zero uses the domain default.", checkdomain.DefaultPriority)
		describeProperty(schema, "assertion_rules", "Stored rule metadata. Current Kubernetes probes decide outcomes from their exit status; these rules do not add a response-body assertion engine.", nil)
	case reflect.TypeOf(checkdomain.AssertionRule{}):
		schema.Required = []string{"metric", "operator", "severity"}
		metric := schema.Properties["metric"]
		metric.MinLength, metric.MaxLength = intPointer(1), intPointer(64)
		schema.Properties["metric"] = metric
		setPropertyEnum(schema, "operator", []string{">", "<", "==", "!=", ">=", "<="})
		setPropertyEnum(schema, "severity", []string{"warning", "critical"})
	case reflect.TypeOf(CreateSLIRequest{}):
		schema.Required = append(schema.Required, "source_config")
		schema.Properties["source_config"] = Schema{Type: "object", AdditionalProperties: true, Required: []string{"source_uid"},
			Description: "Select the exact source definition identity recorded in the run ledger; extra stored keys do not narrow the reader.",
			Properties:  map[string]Schema{"source_uid": {Type: "string", MinLength: intPointer(1), Description: "ScheduledJob CR UID, Function source UID, or check-<check ID>. Use the actual selected source, not its display name or revision ID."}},
		}
		describeProperty(schema, "source_type", "SLI events derive from recorded run outcomes.", slidomain.SourceTypeJobRun)
		describeProperty(schema, "aggregation", "Aggregation mode for recorded observations.", slidomain.AggregationRatio)
		criteria := schema.Properties["good_event_criteria"]
		criteria.Description = "Validated at creation. Availability and quality require a nonempty object; latency requires duration_ms; custom may omit criteria. Only status and duration_ms are supported. Unsupported keys such as severity or probe output, invalid statuses and invalid numeric conditions are rejected with HTTP 400. Every supplied criterion must match the recorded outcome; a duration criterion requires actual retained timing."
		criteria.Properties = map[string]Schema{
			"status":      {Type: "string", Description: "Expected outcome status: exactly success or failed."},
			"duration_ms": {Description: "Finite, nonnegative number for equality, or an object containing exactly op and value. The operator is one of <, <=, >, >=, ==, != and value is a finite, nonnegative number; for example {\"op\":\"<=\",\"value\":1000}. Requires actual retained timing."},
		}
		schema.Properties["good_event_criteria"] = criteria
	case reflect.TypeOf(CreateSLORequest{}):
		describeProperty(schema, "window_type", "Budget evaluation window type.", slodomain.WindowTypeRolling)
		describeProperty(schema, "window_duration", "Positive Go duration, at most 8760h; for example 24h or 720h. The d suffix is not supported.", nil)
		describeProperty(schema, "alert_fast_burn_rate", "Omitted or zero uses the default. Must exceed the effective slow burn rate.", slodomain.DefaultFastBurnRate)
		describeProperty(schema, "alert_slow_burn_rate", "Omitted or zero uses the default. Must be less than the effective fast burn rate.", slodomain.DefaultSlowBurnRate)
	case reflect.TypeOf(CreatePolicyRequest{}):
		statement := Schema{Type: "object", AdditionalProperties: false, Required: []string{"effect", "action", "resource"}, Properties: map[string]Schema{
			"effect":   {Type: "string", Enum: []string{"Allow", "Deny"}},
			"action":   {Type: "array", MinItems: intPointer(1), Items: &Schema{Type: "string"}, Description: "Explicit known actions such as job:List or job:Get. Wildcards belong only to platform presets; tenant-authored policies cannot widen the caller's effective grants."},
			"resource": {Type: "array", MinItems: intPointer(1), Items: &Schema{Type: "string"}, Description: "orbitjob:<tenant>:<group>:<type>/<id> ARN patterns. self resolves to the owning tenant. All statements are evaluated with explicit Deny and credential boundaries."},
		}}
		schema.Properties["document"] = Schema{Type: "object", AdditionalProperties: false, Required: []string{"statement"}, Description: "Exactly one supported lowercase policy document. Duplicate members, case variants, conditions and unsupported fields are rejected rather than ignored. No credential-policy rebinding endpoint is provided.", Properties: map[string]Schema{
			"version":   {Type: "string", Description: "Optional document version label; this string does not enable extra policy semantics."},
			"statement": {Type: "array", MinItems: intPointer(1), Items: &statement},
		}}
	case reflect.TypeOf(FunctionDefinitionRequest{}), reflect.TypeOf(UpdateFunctionRequest{}):
		schema.AdditionalProperties = false
		describeProperty(schema, "status", "active permits invocations of the ready version; paused stops new invocations without canceling existing runs.", functiondomain.StatusActive)
		describeProperty(schema, "timeout_seconds", "Omitted or null uses the default.", functiondomain.DefaultTimeoutSeconds)
		describeProperty(schema, "retry_limit", "Retries after the initial attempt.", 0)
		describeProperty(schema, "history_success", "Retained terminal successes; zero explicitly retains none.", functiondomain.DefaultHistorySuccess)
		describeProperty(schema, "history_failed", "Retained terminal failures; zero explicitly retains none.", functiondomain.DefaultHistoryFailed)
		describeProperty(schema, "command", "Container command array; preserve each literal argument. No implicit shell expansion is added.", nil)
		describeProperty(schema, "args", "Container argument array; values are passed literally.", nil)
	case reflect.TypeOf(definition.ApplyRequest{}):
		schema.AdditionalProperties = false
		requestID := schema.Properties["request_id"]
		requestID.Format = "uuid"
		schema.Properties["request_id"] = requestID
		describeProperty(schema, "request_id", "Stable UUID for this exact write intent; retain it through unknown outcomes and reconciliation.", nil)
		describeProperty(schema, "spec_json", "JSON-encoded typed ScheduledJobSpec or WorkflowJobSpec. This is a string, not a nested object or full Kubernetes manifest. Unknown spec fields are rejected and integers retain precision. The typed apply request has an additional 256 KiB decoder limit.", nil)
		describeProperty(schema, "dry_run", "Run Kubernetes admission without persistence; review the canonical spec_json and manifest_digest before saving.", false)
		describeProperty(schema, "manifest_digest", "Exact digest returned by the reviewed dry-run. Required for actual writes; missing or changed digest is rejected.", nil)
		describeProperty(schema, "expected_uid", "For an update, send the observed immutable UID together with expected_resource_version. Leave both empty for creation.", nil)
		describeProperty(schema, "expected_resource_version", "Observed resourceVersion paired with expected_uid; stale or replaced resources conflict.", nil)
	case reflect.TypeOf(WorkflowSchedulePreviewRequest{}):
		describeProperty(schema, "time_zone", "IANA time zone; response instants are UTC.", "UTC")
		describeProperty(schema, "count", "Omitted or zero uses the default.", workflowschedule.DefaultPreviewCount)
	case reflect.TypeOf(VersionRequest{}), reflect.TypeOf(ChangeCheckStatusRequest{}), reflect.TypeOf(ChangeSLOStatusRequest{}):
		describeProperty(schema, "version", "Current observed resource version. Stale Check/SLO writes return 409; the current SLI delete store returns 404 for either a stale version or an absent row. Reload and review before submitting a new intent.", nil)
	case reflect.TypeOf(CreateAPIKeyRequest{}):
		describeProperty(schema, "policies", "Policy IDs to bind at creation. Omitted or empty grants no guarded operation; authentication alone provides no management access.", nil)
		describeProperty(schema, "boundary_policy_id", "Optional policy ID capping all bound grants. The caller may not grant beyond its own effective permissions.", nil)
		describeProperty(schema, "resource_group_id", "Optional resource group ID in the target tenant. Group scope independently limits row visibility.", nil)
	}
}

func describeProperty(schema *Schema, name, description string, defaultValue any) {
	property := schema.Properties[name]
	property.Description = description
	if defaultValue != nil {
		property.Default = defaultValue
	}
	schema.Properties[name] = property
}

func setPropertyEnum(schema *Schema, name string, values []string) {
	property := schema.Properties[name]
	property.Enum = values
	schema.Properties[name] = property
}

func intPointer(value int) *int { return &value }

func numberPointer(value int) *float64 {
	number := float64(value)
	return &number
}
