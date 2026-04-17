package http

import (
	"reflect"
	"testing"
)

type pointerParameterModel struct {
	ID       int64  `uri:"id" binding:"required,min=1"`
	TenantID string `form:"tenant_id" binding:"omitempty,max=64"`
	TraceID  string `header:"X-Trace-ID" binding:"omitempty,max=128"`
	Ignored  string `json:"ignored"`
}

type dashTagParameterModel struct {
	Ignored string `form:"-"`
}

type requestSchemaModel struct {
	Name string `json:"name" binding:"required,max=10"`
}

type responseSchemaModel struct {
	Name      string  `json:"name"`
	Optional  *string `json:"optional"`
	OmitEmpty string  `json:"omit_empty,omitempty"`
	hidden    string  `json:"hidden"`
}

func TestBuildOpenAPIDocument_SkipsDisabledAdminRoutes(t *testing.T) {
	doc := buildOpenAPIDocument(nil, false)

	if _, ok := doc.Paths["/api/v1/jobs"]; ok {
		t.Fatalf("expected /api/v1/jobs to be skipped when handler is nil and includeAllAdminRoutes=false")
	}
	if _, ok := doc.Paths["/healthz"]; !ok {
		t.Fatalf("expected service path /healthz to remain present")
	}
}

func TestOperationDefinitionBuild_ResponseWithoutModel(t *testing.T) {
	registry := newSchemaRegistry()
	op := operationDefinition{
		id:      "noContentOperation",
		summary: "No content response",
		responses: []responseDefinition{
			{statusCode: 204, description: "No content", model: nil},
		},
	}.build(registry)

	response := op.Responses["204"]
	if response.Content != nil {
		t.Fatalf("expected no content map for nil response model, got %+v", response.Content)
	}
	if !hasTraceHeader(response) {
		t.Fatalf("expected trace header for response without model")
	}
}

func TestParametersFromModel_WithPointerInputAndDefaults(t *testing.T) {
	params := parametersFromModel(&pointerParameterModel{})
	if len(params) != 3 {
		t.Fatalf("expected 3 parameters, got %d: %+v", len(params), params)
	}

	tenantSchema := parameterSchema(params, "tenant_id", "query")
	if tenantSchema.Default != "default" {
		t.Fatalf("expected tenant_id default=default, got %+v", tenantSchema.Default)
	}

	idSchema := parameterSchema(params, "id", "path")
	if idSchema.Minimum == nil || *idSchema.Minimum != 1 {
		t.Fatalf("expected path id minimum=1, got %+v", idSchema.Minimum)
	}
}

func TestOpenAPIParameterTag_UnsupportedOrDashTag(t *testing.T) {
	typeOfPointerModel := reflect.TypeOf(pointerParameterModel{})
	if _, _, ok := openAPIParameterTag(typeOfPointerModel.Field(3)); ok {
		t.Fatalf("expected field without uri/form/header tag to be ignored")
	}

	typeOfDashModel := reflect.TypeOf(dashTagParameterModel{})
	if _, _, ok := openAPIParameterTag(typeOfDashModel.Field(0)); ok {
		t.Fatalf("expected field with form:- to be ignored")
	}
}

func TestSchemaForType_MapVariantsAndAnonymousName(t *testing.T) {
	registry := newSchemaRegistry()

	schemaAny := registry.schemaForType(reflect.TypeOf(map[string]any{}), schemaModeResponse)
	if schemaAny.Type != "object" || schemaAny.AdditionalProperties != true {
		t.Fatalf("expected map[string]any schema additionalProperties=true, got %+v", schemaAny)
	}

	schemaTyped := registry.schemaForType(reflect.TypeOf(map[string]int{}), schemaModeResponse)
	if schemaTyped.Type != "object" {
		t.Fatalf("expected map[string]int schema type object, got %+v", schemaTyped)
	}
	if _, ok := schemaTyped.AdditionalProperties.(Schema); !ok {
		t.Fatalf("expected typed additionalProperties schema, got %T", schemaTyped.AdditionalProperties)
	}

	schemaNonStringKey := registry.schemaForType(reflect.TypeOf(map[int]string{}), schemaModeResponse)
	if schemaNonStringKey.Type != "object" {
		t.Fatalf("expected map[int]string fallback object schema, got %+v", schemaNonStringKey)
	}

	if got := schemaComponentName(reflect.TypeOf(struct{}{})); got != "AnonymousSchema" {
		t.Fatalf("expected anonymous schema name, got %q", got)
	}
}

func TestStructSchema_RequestAndResponseModes(t *testing.T) {
	registry := newSchemaRegistry()

	request := registry.structSchema(reflect.TypeOf(requestSchemaModel{}), schemaModeRequest)
	if !containsString(request.Required, "name") {
		t.Fatalf("expected request required fields to include name, got %+v", request.Required)
	}
	if got := request.Properties["name"].MaxLength; got == nil || *got != 10 {
		t.Fatalf("expected request maxLength=10, got %+v", got)
	}

	response := registry.structSchema(reflect.TypeOf(responseSchemaModel{}), schemaModeResponse)
	if !containsString(response.Required, "name") {
		t.Fatalf("expected response required fields to include name, got %+v", response.Required)
	}
	if containsString(response.Required, "optional") {
		t.Fatalf("expected pointer field optional not to be required in response schema")
	}
	if containsString(response.Required, "omit_empty") {
		t.Fatalf("expected omitempty field omit_empty not to be required in response schema")
	}
}

func TestInlineSchemaForType_CoverageBranches(t *testing.T) {
	if got := inlineSchemaForType(reflect.TypeOf(true)); got.Type != "boolean" {
		t.Fatalf("expected boolean schema, got %+v", got)
	}
	if got := inlineSchemaForType(reflect.TypeOf(int64(1))); got.Type != "integer" || got.Format != "int64" {
		t.Fatalf("expected int64 schema, got %+v", got)
	}
	if got := inlineSchemaForType(reflect.TypeOf(uint64(1))); got.Type != "integer" || got.Format != "int64" {
		t.Fatalf("expected uint64 schema, got %+v", got)
	}
	if got := inlineSchemaForType(reflect.TypeOf(1.2)); got.Type != "number" {
		t.Fatalf("expected number schema, got %+v", got)
	}
	if got := inlineSchemaForType(reflect.TypeOf("x")); got.Type != "string" {
		t.Fatalf("expected string schema, got %+v", got)
	}

	sliceSchema := inlineSchemaForType(reflect.TypeOf([]int{}))
	if sliceSchema.Type != "array" || sliceSchema.Items == nil || sliceSchema.Items.Type != "integer" {
		t.Fatalf("expected array schema with integer items, got %+v", sliceSchema)
	}

	mapSchema := inlineSchemaForType(reflect.TypeOf(map[string]int{}))
	if mapSchema.Type != "object" || mapSchema.AdditionalProperties != true {
		t.Fatalf("expected object schema with additionalProperties=true for inline map, got %+v", mapSchema)
	}

	unknownSchema := inlineSchemaForType(reflect.TypeOf(struct{ Value complex64 }{}).Field(0).Type)
	if unknownSchema.Type != "" || unknownSchema.Ref != "" {
		t.Fatalf("expected fallback empty schema for unsupported type, got %+v", unknownSchema)
	}
}

func TestApplyBindingRules_ValidAndInvalidValues(t *testing.T) {
	stringSchema := Schema{Type: "string"}
	applyBindingRules(&stringSchema, "oneof=a b,min=2,max=5")
	if len(stringSchema.Enum) != 2 || stringSchema.Enum[0] != "a" || stringSchema.Enum[1] != "b" {
		t.Fatalf("expected enum [a b], got %+v", stringSchema.Enum)
	}
	if stringSchema.MinLength == nil || *stringSchema.MinLength != 2 {
		t.Fatalf("expected minLength=2, got %+v", stringSchema.MinLength)
	}
	if stringSchema.MaxLength == nil || *stringSchema.MaxLength != 5 {
		t.Fatalf("expected maxLength=5, got %+v", stringSchema.MaxLength)
	}

	numericSchema := Schema{Type: "integer"}
	applyBindingRules(&numericSchema, "min=1,max=9")
	if numericSchema.Minimum == nil || *numericSchema.Minimum != 1 {
		t.Fatalf("expected minimum=1, got %+v", numericSchema.Minimum)
	}
	if numericSchema.Maximum == nil || *numericSchema.Maximum != 9 {
		t.Fatalf("expected maximum=9, got %+v", numericSchema.Maximum)
	}

	invalidSchema := Schema{Type: "integer"}
	applyBindingRules(&invalidSchema, "min=bad,max=oops")
	if invalidSchema.Minimum != nil || invalidSchema.Maximum != nil {
		t.Fatalf("expected invalid min/max to be ignored, got %+v", invalidSchema)
	}

	emptySchema := Schema{Type: "string"}
	applyBindingRules(&emptySchema, "")
	if emptySchema.MinLength != nil || emptySchema.MaxLength != nil || len(emptySchema.Enum) != 0 {
		t.Fatalf("expected empty binding to keep schema unchanged, got %+v", emptySchema)
	}
}

func TestApplySchemaDefaults_NoMutationForOtherSchemas(t *testing.T) {
	registry := newSchemaRegistry()
	schema := Schema{Properties: map[string]Schema{"name": {Type: "string"}}}

	registry.applySchemaDefaults("OtherSchema", &schema)
	if schema.Properties["name"].Default != nil {
		t.Fatalf("expected non-CreateJobRequest schemas to remain unchanged, got %+v", schema)
	}
}

func TestApplySchemaDefaults_CreateJobRequest_PreservesLengthConstraints(t *testing.T) {
	registry := newSchemaRegistry()

	_ = registry.schemaForModel(CreateJobRequest{}, schemaModeRequest)

	schema, ok := registry.components["CreateJobRequest"]
	if !ok {
		t.Fatalf("expected CreateJobRequest schema component")
	}

	tenant := schema.Properties["tenant_id"]
	if tenant.MaxLength == nil || *tenant.MaxLength != 64 {
		t.Fatalf("expected tenant_id maxLength=64, got %+v", tenant.MaxLength)
	}
	if tenant.Default != "default" {
		t.Fatalf("expected tenant_id default=default, got %+v", tenant.Default)
	}

	timezone := schema.Properties["timezone"]
	if timezone.MaxLength == nil || *timezone.MaxLength != 64 {
		t.Fatalf("expected timezone maxLength=64, got %+v", timezone.MaxLength)
	}
	if timezone.Default != "UTC" {
		t.Fatalf("expected timezone default=UTC, got %+v", timezone.Default)
	}
}

func TestApplyStandardResponseHeaders_OverwritesAndKeepsMap(t *testing.T) {
	response := Response{Headers: map[string]Header{"Existing": {Schema: Schema{Type: "string"}}}}
	applyStandardResponseHeaders(&response)

	header, ok := response.Headers[traceIDHeaderName]
	if !ok {
		t.Fatalf("expected trace header to be present, got %+v", response.Headers)
	}
	if header.Schema.Type != "string" {
		t.Fatalf("expected trace header schema type string, got %+v", header.Schema)
	}
}
