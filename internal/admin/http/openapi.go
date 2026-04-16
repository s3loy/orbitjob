package http

import (
	stdhttp "net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	command "orbitjob/internal/admin/app/job/command"
	query "orbitjob/internal/admin/app/job/query"
)

const adminAPIPrefix = "/api/v1"

const traceIDHeaderName = "X-Trace-ID"

type routeDefinition struct {
	method   string
	path     string
	enabled  func(*Handler) bool
	register func(gin.IRouter, *Handler)
	spec     operationDefinition
}

type operationDefinition struct {
	id                  string
	summary             string
	description         string
	tags                []string
	parameterModels     []any
	requestBodyModel    any
	requestBodyRequired bool
	responses           []responseDefinition
}

type responseDefinition struct {
	statusCode  int
	description string
	model       any
	contentType string
}

type OpenAPIDocument struct {
	OpenAPI    string              `json:"openapi"`
	Info       OpenAPIInfo         `json:"info"`
	Paths      map[string]PathItem `json:"paths"`
	Components *OpenAPIComponents  `json:"components,omitempty"`
}

type OpenAPIInfo struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

type OpenAPIComponents struct {
	Schemas map[string]Schema `json:"schemas,omitempty"`
}

type PathItem struct {
	Get  *Operation `json:"get,omitempty"`
	Post *Operation `json:"post,omitempty"`
	Put  *Operation `json:"put,omitempty"`
}

type Operation struct {
	OperationID string              `json:"operationId,omitempty"`
	Summary     string              `json:"summary,omitempty"`
	Description string              `json:"description,omitempty"`
	Tags        []string            `json:"tags,omitempty"`
	Parameters  []Parameter         `json:"parameters,omitempty"`
	RequestBody *RequestBody        `json:"requestBody,omitempty"`
	Responses   map[string]Response `json:"responses"`
}

type Parameter struct {
	Name     string `json:"name"`
	In       string `json:"in"`
	Required bool   `json:"required,omitempty"`
	Schema   Schema `json:"schema"`
}

type RequestBody struct {
	Required bool                 `json:"required,omitempty"`
	Content  map[string]MediaType `json:"content"`
}

type Response struct {
	Description string               `json:"description"`
	Headers     map[string]Header    `json:"headers,omitempty"`
	Content     map[string]MediaType `json:"content,omitempty"`
}

type Header struct {
	Description string `json:"description,omitempty"`
	Schema      Schema `json:"schema"`
}

type MediaType struct {
	Schema Schema `json:"schema"`
}

type Schema struct {
	Ref                  string            `json:"$ref,omitempty"`
	Type                 string            `json:"type,omitempty"`
	Format               string            `json:"format,omitempty"`
	Description          string            `json:"description,omitempty"`
	Properties           map[string]Schema `json:"properties,omitempty"`
	Items                *Schema           `json:"items,omitempty"`
	Required             []string          `json:"required,omitempty"`
	Enum                 []string          `json:"enum,omitempty"`
	Default              any               `json:"default,omitempty"`
	Nullable             bool              `json:"nullable,omitempty"`
	AdditionalProperties any               `json:"additionalProperties,omitempty"`
	Minimum              *float64          `json:"minimum,omitempty"`
	Maximum              *float64          `json:"maximum,omitempty"`
	MinLength            *int              `json:"minLength,omitempty"`
	MaxLength            *int              `json:"maxLength,omitempty"`
}

type schemaMode string

const (
	schemaModeRequest  schemaMode = "request"
	schemaModeResponse schemaMode = "response"
)

type schemaRegistry struct {
	components map[string]Schema
	seen       map[reflect.Type]string
}

type traceIDHeaderRequest struct {
	TraceID string `header:"X-Trace-ID" binding:"omitempty,max=128"`
}

type healthzResponse struct {
	Status string `json:"status"`
}

func serviceAPIRoutes() []routeDefinition {
	return []routeDefinition{
		{
			method: stdhttp.MethodGet,
			path:   "/healthz",
			spec: operationDefinition{
				id:          "getHealthz",
				summary:     "Health check",
				tags:        []string{"System"},
				description: "Liveness probe endpoint. Returns HTTP 200 when service is up.",
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Service is healthy", model: healthzResponse{}},
				},
			},
		},
		{
			method: stdhttp.MethodGet,
			path:   "/metrics",
			spec: operationDefinition{
				id:          "getMetrics",
				summary:     "Prometheus metrics",
				tags:        []string{"System"},
				description: "Prometheus scrape endpoint in text exposition format.",
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Metrics payload", model: "", contentType: "text/plain"},
				},
			},
		},
		{
			method: stdhttp.MethodGet,
			path:   "/openapi.json",
			spec: operationDefinition{
				id:          "getOpenAPIDocument",
				summary:     "OpenAPI document",
				tags:        []string{"System"},
				description: "Machine-readable OpenAPI 3 document generated from code-first route metadata.",
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "OpenAPI 3 document", model: map[string]any{}},
				},
			},
		},
	}
}

func adminAPIRoutes() []routeDefinition {
	errorModel := errorResponse{}

	return []routeDefinition{
		{
			method: stdhttp.MethodGet,
			path:   "/jobs",
			enabled: func(h *Handler) bool {
				return h != nil && h.listJobsUC != nil
			},
			register: func(r gin.IRouter, h *Handler) {
				r.GET("/jobs", h.ListJobs)
			},
			spec: operationDefinition{
				id:              "listJobs",
				summary:         "List jobs",
				description:     "List jobs for one tenant. tenant_id defaults to \"default\" and limit defaults to 50 when omitted.",
				tags:            []string{"Jobs"},
				parameterModels: []any{ListJobsRequest{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Job list", model: jobListResponse{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method: stdhttp.MethodGet,
			path:   "/jobs/:id",
			enabled: func(h *Handler) bool {
				return h != nil && h.getJobUC != nil
			},
			register: func(r gin.IRouter, h *Handler) {
				r.GET("/jobs/:id", h.GetJob)
			},
			spec: operationDefinition{
				id:              "getJob",
				summary:         "Get one job",
				description:     "Get one job by id. tenant_id defaults to \"default\" when omitted.",
				tags:            []string{"Jobs"},
				parameterModels: []any{GetJobRequest{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Job detail", model: query.GetItem{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "Job not found", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method: stdhttp.MethodPut,
			path:   "/jobs/:id",
			enabled: func(h *Handler) bool {
				return h != nil && h.getJobUC != nil && h.updateJobUC != nil
			},
			register: func(r gin.IRouter, h *Handler) {
				r.PUT("/jobs/:id", h.UpdateJob)
			},
			spec: operationDefinition{
				id:                  "updateJob",
				summary:             "Update one job",
				description:         "Merge-style update: unspecified fields keep existing values. When trigger_type is changed to manual without cron_expr, existing cron_expr is cleared.",
				tags:                []string{"Jobs"},
				parameterModels:     []any{jobIDURI{}, tenantQueryRequest{}, actorIDHeaderRequest{}},
				requestBodyModel:    UpdateJobRequest{},
				requestBodyRequired: true,
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Updated job", model: command.UpdateResult{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "Job not found", model: errorModel},
					{statusCode: stdhttp.StatusConflict, description: "Version conflict", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method: stdhttp.MethodPost,
			path:   "/jobs/:id/pause",
			enabled: func(h *Handler) bool {
				return h != nil && h.statusJobUC != nil
			},
			register: func(r gin.IRouter, h *Handler) {
				r.POST("/jobs/:id/pause", h.PauseJob)
			},
			spec: operationDefinition{
				id:                  "pauseJob",
				summary:             "Pause one job",
				description:         "Pause an active job definition using optimistic locking by version.",
				tags:                []string{"Jobs"},
				parameterModels:     []any{jobIDURI{}, tenantQueryRequest{}, actorIDHeaderRequest{}},
				requestBodyModel:    ChangeStatusRequest{},
				requestBodyRequired: true,
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Paused job", model: command.ChangeStatusResult{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "Job not found", model: errorModel},
					{statusCode: stdhttp.StatusConflict, description: "Version conflict", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method: stdhttp.MethodPost,
			path:   "/jobs/:id/resume",
			enabled: func(h *Handler) bool {
				return h != nil && h.statusJobUC != nil
			},
			register: func(r gin.IRouter, h *Handler) {
				r.POST("/jobs/:id/resume", h.ResumeJob)
			},
			spec: operationDefinition{
				id:                  "resumeJob",
				summary:             "Resume one job",
				description:         "Resume a paused job definition using optimistic locking by version.",
				tags:                []string{"Jobs"},
				parameterModels:     []any{jobIDURI{}, tenantQueryRequest{}, actorIDHeaderRequest{}},
				requestBodyModel:    ChangeStatusRequest{},
				requestBodyRequired: true,
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Resumed job", model: command.ChangeStatusResult{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "Job not found", model: errorModel},
					{statusCode: stdhttp.StatusConflict, description: "Version conflict", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method: stdhttp.MethodPost,
			path:   "/jobs",
			enabled: func(h *Handler) bool {
				return h != nil && h.createJobUC != nil
			},
			register: func(r gin.IRouter, h *Handler) {
				r.POST("/jobs", h.CreateJob)
			},
			spec: operationDefinition{
				id:                  "createJob",
				summary:             "Create one job",
				description:         "Create a job definition. cron_expr is required when trigger_type=cron, and must be empty when trigger_type=manual.",
				tags:                []string{"Jobs"},
				requestBodyModel:    CreateJobRequest{},
				requestBodyRequired: true,
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusCreated, description: "Created job", model: command.CreateResult{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
	}
}

// ServiceOpenAPIDocument builds the full service-level OpenAPI document.
func ServiceOpenAPIDocument() OpenAPIDocument {
	return buildOpenAPIDocument(nil, true)
}

// OpenAPIDocument builds the OpenAPI document that matches currently enabled admin routes.
func (h *Handler) OpenAPIDocument() OpenAPIDocument {
	return buildOpenAPIDocument(h, false)
}

func buildOpenAPIDocument(h *Handler, includeAllAdminRoutes bool) OpenAPIDocument {
	registry := newSchemaRegistry()
	doc := OpenAPIDocument{
		OpenAPI: "3.0.3",
		Info: OpenAPIInfo{
			Title:       "OrbitJob API",
			Version:     "0.1.0",
			Description: "Code-first OpenAPI document generated from OrbitJob HTTP route metadata and DTOs.",
		},
		Paths: map[string]PathItem{},
	}

	for _, route := range serviceAPIRoutes() {
		setPathItemOperation(doc.Paths, toOpenAPIPath(route.path), route.method, route.spec.build(registry))
	}

	for _, route := range adminAPIRoutes() {
		if !includeAllAdminRoutes && route.enabled != nil && !route.enabled(h) {
			continue
		}

		path := toOpenAPIPath(adminAPIPrefix + route.path)
		setPathItemOperation(doc.Paths, path, route.method, route.spec.build(registry))
	}

	if len(registry.components) > 0 {
		doc.Components = &OpenAPIComponents{
			Schemas: registry.components,
		}
	}

	return doc
}

func setPathItemOperation(paths map[string]PathItem, path string, method string, operation Operation) {
	pathItem := paths[path]

	switch method {
	case stdhttp.MethodGet:
		pathItem.Get = &operation
	case stdhttp.MethodPost:
		pathItem.Post = &operation
	case stdhttp.MethodPut:
		pathItem.Put = &operation
	}

	paths[path] = pathItem
}

func (d operationDefinition) build(registry *schemaRegistry) Operation {
	op := Operation{
		OperationID: d.id,
		Summary:     d.summary,
		Description: d.description,
		Tags:        d.tags,
		Responses:   map[string]Response{},
	}

	op.Parameters = append(op.Parameters, parametersFromModel(traceIDHeaderRequest{})...)

	for _, model := range d.parameterModels {
		op.Parameters = append(op.Parameters, parametersFromModel(model)...)
	}

	if d.requestBodyModel != nil {
		op.RequestBody = &RequestBody{
			Required: d.requestBodyRequired,
			Content: map[string]MediaType{
				"application/json": {
					Schema: registry.schemaForModel(d.requestBodyModel, schemaModeRequest),
				},
			},
		}
	}

	for _, response := range d.responses {
		item := Response{
			Description: response.description,
		}
		if response.model != nil {
			contentType := response.contentType
			if strings.TrimSpace(contentType) == "" {
				contentType = "application/json"
			}

			item.Content = map[string]MediaType{
				contentType: {
					Schema: registry.schemaForModel(response.model, schemaModeResponse),
				},
			}
		}
		applyStandardResponseHeaders(&item)
		op.Responses[strconv.Itoa(response.statusCode)] = item
	}

	return op
}

func applyStandardResponseHeaders(response *Response) {
	if response.Headers == nil {
		response.Headers = map[string]Header{}
	}

	response.Headers[traceIDHeaderName] = Header{
		Description: "Trace identifier echoed back to clients. Generated when request does not provide one.",
		Schema: Schema{
			Type: "string",
		},
	}
}

func parametersFromModel(model any) []Parameter {
	t := reflect.TypeOf(model)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	parameters := make([]Parameter, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}

		name, location, ok := openAPIParameterTag(field)
		if !ok {
			continue
		}

		parameter := Parameter{
			Name:     name,
			In:       location,
			Required: location == "path" || hasBindingRule(field.Tag.Get("binding"), "required"),
			Schema:   inlineSchemaForType(field.Type),
		}
		applyBindingRules(&parameter.Schema, field.Tag.Get("binding"))
		applyParameterDefaults(&parameter)
		parameters = append(parameters, parameter)
	}

	return parameters
}

func openAPIParameterTag(field reflect.StructField) (string, string, bool) {
	for _, candidate := range []struct {
		tag      string
		location string
	}{
		{tag: "uri", location: "path"},
		{tag: "form", location: "query"},
		{tag: "header", location: "header"},
	} {
		value := field.Tag.Get(candidate.tag)
		if value == "" || value == "-" {
			continue
		}

		name, _, _ := strings.Cut(value, ",")
		return name, candidate.location, true
	}

	return "", "", false
}

func applyParameterDefaults(parameter *Parameter) {
	if parameter.In != "query" {
		return
	}

	switch parameter.Name {
	case "tenant_id":
		parameter.Schema.Default = "default"
	case "limit":
		parameter.Schema.Default = query.DefaultListLimit
	case "offset":
		parameter.Schema.Default = 0
	}
}

func newSchemaRegistry() *schemaRegistry {
	return &schemaRegistry{
		components: map[string]Schema{},
		seen:       map[reflect.Type]string{},
	}
}

func (r *schemaRegistry) schemaForModel(model any, mode schemaMode) Schema {
	return r.schemaForType(reflect.TypeOf(model), mode)
}

func (r *schemaRegistry) schemaForType(t reflect.Type, mode schemaMode) Schema {
	if t == nil {
		return Schema{}
	}

	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if isTimeType(t) {
		return Schema{
			Type:   "string",
			Format: "date-time",
		}
	}

	switch t.Kind() {
	case reflect.Struct:
		if name, ok := r.seen[t]; ok {
			return Schema{Ref: "#/components/schemas/" + name}
		}

		name := schemaComponentName(t)
		r.seen[t] = name
		r.components[name] = Schema{}
		schema := r.structSchema(t, mode)
		r.applySchemaDefaults(name, &schema)
		r.components[name] = schema
		return Schema{Ref: "#/components/schemas/" + name}
	case reflect.Slice, reflect.Array:
		items := r.schemaForType(t.Elem(), mode)
		return Schema{
			Type:  "array",
			Items: &items,
		}
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return Schema{Type: "object"}
		}
		if isAnyType(t.Elem()) {
			return Schema{
				Type:                 "object",
				AdditionalProperties: true,
			}
		}

		return Schema{
			Type:                 "object",
			AdditionalProperties: r.schemaForType(t.Elem(), mode),
		}
	default:
		return inlineSchemaForType(t)
	}
}

func (r *schemaRegistry) applySchemaDefaults(name string, schema *Schema) {
	if name != "CreateJobRequest" {
		return
	}

	if property, ok := schema.Properties["tenant_id"]; ok {
		property.Default = "default"
		schema.Properties["tenant_id"] = property
	}
	if property, ok := schema.Properties["timezone"]; ok {
		property.Default = "UTC"
		schema.Properties["timezone"] = property
	}
	if property, ok := schema.Properties["timeout_sec"]; ok {
		property.Default = 60
		schema.Properties["timeout_sec"] = property
	}
	if property, ok := schema.Properties["retry_backoff_strategy"]; ok {
		property.Default = "fixed"
		schema.Properties["retry_backoff_strategy"] = property
	}
	if property, ok := schema.Properties["concurrency_policy"]; ok {
		property.Default = "allow"
		schema.Properties["concurrency_policy"] = property
	}
	if property, ok := schema.Properties["misfire_policy"]; ok {
		property.Default = "skip"
		schema.Properties["misfire_policy"] = property
	}
	if property, ok := schema.Properties["cron_expr"]; ok {
		property.Description = "Required when trigger_type=cron; must be empty when trigger_type=manual."
		schema.Properties["cron_expr"] = property
	}
}

func (r *schemaRegistry) structSchema(t reflect.Type, mode schemaMode) Schema {
	schema := Schema{
		Type:       "object",
		Properties: map[string]Schema{},
	}

	required := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}

		jsonTag := field.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}

		name, options, _ := strings.Cut(jsonTag, ",")
		if name == "" {
			continue
		}

		property := r.schemaForType(field.Type, mode)
		if field.Type.Kind() == reflect.Pointer {
			property.Nullable = true
		}
		applyBindingRules(&property, field.Tag.Get("binding"))
		schema.Properties[name] = property

		switch mode {
		case schemaModeRequest:
			if hasBindingRule(field.Tag.Get("binding"), "required") {
				required = append(required, name)
			}
		case schemaModeResponse:
			if !fieldHasOption(options, "omitempty") && field.Type.Kind() != reflect.Pointer {
				required = append(required, name)
			}
		}
	}

	if len(required) > 0 {
		schema.Required = required
	}

	return schema
}

func inlineSchemaForType(t reflect.Type) Schema {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if isTimeType(t) {
		return Schema{
			Type:   "string",
			Format: "date-time",
		}
	}

	switch t.Kind() {
	case reflect.Bool:
		return Schema{Type: "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		schema := Schema{Type: "integer"}
		if t.Kind() == reflect.Int64 {
			schema.Format = "int64"
		}
		return schema
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		schema := Schema{Type: "integer"}
		if t.Kind() == reflect.Uint64 {
			schema.Format = "int64"
		}
		return schema
	case reflect.Float32, reflect.Float64:
		return Schema{Type: "number"}
	case reflect.String:
		return Schema{Type: "string"}
	case reflect.Slice, reflect.Array:
		items := inlineSchemaForType(t.Elem())
		return Schema{
			Type:  "array",
			Items: &items,
		}
	case reflect.Map:
		return Schema{
			Type:                 "object",
			AdditionalProperties: true,
		}
	default:
		return Schema{}
	}
}

func applyBindingRules(schema *Schema, binding string) {
	if binding == "" {
		return
	}

	for _, rule := range strings.Split(binding, ",") {
		switch {
		case strings.HasPrefix(rule, "oneof="):
			schema.Enum = strings.Fields(strings.TrimPrefix(rule, "oneof="))
		case strings.HasPrefix(rule, "min="):
			value, err := strconv.Atoi(strings.TrimPrefix(rule, "min="))
			if err != nil {
				continue
			}
			switch schema.Type {
			case "integer", "number":
				minimum := float64(value)
				schema.Minimum = &minimum
			case "string":
				minLength := value
				schema.MinLength = &minLength
			}
		case strings.HasPrefix(rule, "max="):
			value, err := strconv.Atoi(strings.TrimPrefix(rule, "max="))
			if err != nil {
				continue
			}
			switch schema.Type {
			case "integer", "number":
				maximum := float64(value)
				schema.Maximum = &maximum
			case "string":
				maxLength := value
				schema.MaxLength = &maxLength
			}
		}
	}
}

func hasBindingRule(binding string, want string) bool {
	for _, rule := range strings.Split(binding, ",") {
		if rule == want {
			return true
		}
	}
	return false
}

func fieldHasOption(options string, want string) bool {
	for _, option := range strings.Split(options, ",") {
		if option == want {
			return true
		}
	}
	return false
}

func toOpenAPIPath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") {
			parts[i] = "{" + strings.TrimPrefix(part, ":") + "}"
		}
	}
	return strings.Join(parts, "/")
}

func schemaComponentName(t reflect.Type) string {
	name := t.Name()
	if name == "" {
		return "AnonymousSchema"
	}

	runes := []rune(name)
	if runes[0] >= 'a' && runes[0] <= 'z' {
		runes[0] = runes[0] - ('a' - 'A')
	}

	return string(runes)
}

func isTimeType(t reflect.Type) bool {
	return t.PkgPath() == "time" && t.Name() == "Time"
}

func isAnyType(t reflect.Type) bool {
	return t.Kind() == reflect.Interface && t.NumMethod() == 0
}
