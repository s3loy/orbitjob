package http

import (
	"fmt"
	stdhttp "net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	apikeycommand "orbitjob/internal/admin/app/apikey/command"
	checkcommand "orbitjob/internal/admin/app/check/command"
	checkquery "orbitjob/internal/admin/app/check/query"
	checkrunquery "orbitjob/internal/admin/app/checkrun/query"
	jobcommand "orbitjob/internal/admin/app/job/command"
	jobquery "orbitjob/internal/admin/app/job/query"
	policycommand "orbitjob/internal/admin/app/policy/command"
	policyquery "orbitjob/internal/admin/app/policy/query"
	resourcegroupcommand "orbitjob/internal/admin/app/resourcegroup/command"
	runcommand "orbitjob/internal/admin/app/run/command"
	runquery "orbitjob/internal/admin/app/run/query"
	slicommand "orbitjob/internal/admin/app/sli/command"
	sliquery "orbitjob/internal/admin/app/sli/query"
	slocommand "orbitjob/internal/admin/app/slo/command"
	sloquery "orbitjob/internal/admin/app/slo/query"
	sloalertquery "orbitjob/internal/admin/app/sloalert/query"
	slobudgetquery "orbitjob/internal/admin/app/slobudget/query"
	tenantcommand "orbitjob/internal/admin/app/tenant/command"
	tenantquery "orbitjob/internal/admin/app/tenant/query"
	"orbitjob/internal/admin/http/apperror"
	"orbitjob/internal/core/domain/policy"
)

const adminAPIPrefix = "/api/v1"

const traceIDHeaderName = "X-Trace-ID"

type routeDefinition struct {
	method string
	path   string
	// permission is the action a caller must hold to reach this route. It is
	// empty only for the three public endpoints; TestEveryRouteDeclaresAPermission
	// fails the build if a new route is added without one.
	permission string
	// resource resolves the ARN the request targets. Required whenever
	// permission is set.
	resource func(*gin.Context) (policy.ARN, error)
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
	headers     map[string]Header
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
	Get    *Operation `json:"get,omitempty"`
	Post   *Operation `json:"post,omitempty"`
	Put    *Operation `json:"put,omitempty"`
	Delete *Operation `json:"delete,omitempty"`
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
	// owners maps a component name to the Go type that claimed it. Distinct
	// types routinely share a bare name -- job/query.GetItem, run/query.GetItem
	// and sloalert/query.GetItem are three different shapes all called GetItem --
	// and without this the second one silently overwrites the first, leaving
	// every $ref pointing at a schema that is not the one the handler returns.
	owners map[string]reflect.Type
}

type traceIDHeaderRequest struct {
	TraceID string `header:"X-Trace-ID" binding:"omitempty,max=128"`
}

type idempotencyKeyHeaderRequest struct {
	IdempotencyKey string `header:"X-OrbitJob-Idempotency-Key" binding:"omitempty,max=128"`
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
	errorModel := apperror.ErrorResponse{}

	unauthorizedResponse := responseDefinition{
		statusCode:  stdhttp.StatusUnauthorized,
		description: "Unauthorized",
		model:       errorModel,
	}
	rateLimitedResponse := responseDefinition{
		statusCode:  stdhttp.StatusTooManyRequests,
		description: "Rate limit exceeded",
		model:       errorModel,
		headers: map[string]Header{
			"Retry-After": {
				Description: "Minimum seconds until the request may be retried (default 1)",
				Schema:      Schema{Type: "string"},
			},
		},
	}

	routes := []routeDefinition{
		{
			method:     stdhttp.MethodGet,
			path:       "/jobs",
			permission: "job:List",
			resource:   tenantScoped("job"),
			enabled: func(h *Handler) bool {
				return h != nil && h.listJobsUC != nil
			},
			register: func(r gin.IRouter, h *Handler) {
				r.GET("/jobs", h.ListJobs)
			},
			spec: operationDefinition{
				id:              "listJobs",
				summary:         "List job definitions",
				description:     "List the active revision of every job definition in the tenant. A definition is a ScheduledJob custom resource, declared outside this API; there is no create route. limit defaults to 50 when omitted.",
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
			method:     stdhttp.MethodGet,
			path:       "/jobs/:id",
			permission: "job:Get",
			resource:   tenantScoped("job"),
			enabled: func(h *Handler) bool {
				return h != nil && h.getJobUC != nil
			},
			register: func(r gin.IRouter, h *Handler) {
				r.GET("/jobs/:id", h.GetJob)
			},
			spec: operationDefinition{
				id:              "getJob",
				summary:         "Get one job definition",
				description:     "Get the active revision of one definition by revision id. The revision is immutable: a later edit makes a new revision active.",
				tags:            []string{"Jobs"},
				parameterModels: []any{GetJobRequest{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Job detail", model: jobquery.GetItem{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "Job not found", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodPost,
			path:       "/jobs/:id/trigger",
			permission: "job:Trigger",
			resource:   tenantScoped("job"),
			enabled:    func(h *Handler) bool { return h != nil && h.triggerJobUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.POST("/jobs/:id/trigger", h.TriggerJob) },
			spec: operationDefinition{
				id:              "triggerJob",
				summary:         "Trigger a manual run",
				description:     "Publish a JobRun custom resource for a manual execution. The operator writes the run row together with its audit trail and records the calling credential as the run's actor, so \"who triggered it\" is answerable after the fact. Use the X-OrbitJob-Idempotency-Key header to prevent duplicate triggers.",
				tags:            []string{"Jobs"},
				parameterModels: []any{jobIDURI{}, idempotencyKeyHeaderRequest{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusCreated, description: "Created run", model: jobcommand.TriggerResult{}},
					{statusCode: stdhttp.StatusOK, description: "Existing run returned for idempotent trigger", model: jobcommand.TriggerResult{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "Job not found", model: errorModel},
					{statusCode: stdhttp.StatusConflict, description: "Definition is suspended", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodGet,
			path:       "/instances",
			permission: "instance:List",
			resource:   tenantScoped("instance"),
			enabled:    func(h *Handler) bool { return h != nil && h.listInstancesUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.GET("/instances", h.ListInstances) },
			spec: operationDefinition{
				id:              "listInstances",
				summary:         "List runs",
				description:     "List runs from the control-plane ledger, newest first. Each entry carries the run's phase, its attempt count against max_attempts, and the actor that triggered it. Optionally filter by phase.",
				tags:            []string{"Instances"},
				parameterModels: []any{ListInstancesRequest{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Run list", model: runListResponse{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodGet,
			path:       "/instances/:run_id",
			permission: "instance:Get",
			resource:   tenantScoped("instance"),
			enabled:    func(h *Handler) bool { return h != nil && h.getInstanceUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.GET("/instances/:run_id", h.GetInstance) },
			spec: operationDefinition{
				id:              "getInstance",
				summary:         "Get one run",
				description:     "Get one run from the ledger by run id, with its full attempt trail in the same response. Answers \"did it run, how many times, and who triggered it\": the body carries phase, attempt, max_attempts and actor.",
				tags:            []string{"Instances"},
				parameterModels: []any{instanceRunIDURI{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Run detail", model: runquery.GetItem{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "Run not found", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodPost,
			path:       "/instances/:run_id/cancel",
			permission: "instance:Cancel",
			resource:   tenantScoped("instance"),
			enabled:    func(h *Handler) bool { return h != nil && h.cancelRunUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.POST("/instances/:run_id/cancel", h.CancelInstance) },
			spec: operationDefinition{
				id:              "cancelInstance",
				summary:         "Request a run cancel",
				description:     "Request a stop for one run by patching spec.cancelRequested on its JobRun custom resource. The request carries no body. The run reaches the Canceled phase only once the operator has observed its Kubernetes Job gone; the response carries the phase observed at request time. Canceling a run that already finished succeeds and reports the terminal phase.",
				tags:            []string{"Instances"},
				parameterModels: []any{instanceRunIDURI{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Cancel request accepted, or the run was already terminal", model: runcommand.CancelResult{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "Run not found", model: errorModel},
					{statusCode: stdhttp.StatusConflict, description: "The run's JobRun custom resource is missing", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodGet,
			path:       "/instances/:run_id/attempts",
			permission: "instance:Get",
			resource:   tenantScoped("instance"),
			enabled:    func(h *Handler) bool { return h != nil && h.listAttemptsUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.GET("/instances/:run_id/attempts", h.ListAttempts) },
			spec: operationDefinition{
				id:              "listAttempts",
				summary:         "List run attempts",
				description:     "List the per-attempt trail for one run. Each attempt is one Kubernetes Job, so the trail answers \"how many times\" with the Job that carried each try. The trail is bounded by the definition's retry policy, so it takes no page parameters.",
				tags:            []string{"Instances"},
				parameterModels: []any{instanceRunIDURI{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Attempt list", model: attemptListResponse{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "Run not found", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		// ==================== Checks ====================
		{
			method:     stdhttp.MethodPost,
			path:       "/checks",
			permission: "check:Create",
			resource:   tenantScoped("check"),
			enabled:    func(h *Handler) bool { return h != nil && h.createCheckUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.POST("/checks", h.CreateCheck) },
			spec: operationDefinition{
				id:                  "createCheck",
				summary:             "Create one check",
				description:         "Create an inspection check definition.",
				tags:                []string{"Checks"},
				requestBodyModel:    CreateCheckRequest{},
				requestBodyRequired: true,
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusCreated, description: "Created check", model: checkcommand.CreateResult{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodGet,
			path:       "/checks",
			permission: "check:List",
			resource:   tenantScoped("check"),
			enabled:    func(h *Handler) bool { return h != nil && h.listChecksUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.GET("/checks", h.ListChecks) },
			spec: operationDefinition{
				id:              "listChecks",
				summary:         "List checks",
				description:     "List inspection checks for one tenant.",
				tags:            []string{"Checks"},
				parameterModels: []any{ListChecksRequest{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Check list", model: checkListResponse{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodGet,
			path:       "/checks/:id",
			permission: "check:Get",
			resource:   tenantScoped("check"),
			enabled:    func(h *Handler) bool { return h != nil && h.getCheckUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.GET("/checks/:id", h.GetCheck) },
			spec: operationDefinition{
				id:              "getCheck",
				summary:         "Get one check",
				description:     "Get one check by id.",
				tags:            []string{"Checks"},
				parameterModels: []any{GetCheckRequest{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Check detail", model: checkquery.GetResult{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "Check not found", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodPost,
			path:       "/checks/:id/pause",
			permission: "check:Pause",
			resource:   tenantScoped("check"),
			enabled:    func(h *Handler) bool { return h != nil && h.pauseCheckUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.POST("/checks/:id/pause", h.PauseCheck) },
			spec: operationDefinition{
				id:                  "pauseCheck",
				summary:             "Pause one check",
				description:         "Pause an active check definition using optimistic locking by version.",
				tags:                []string{"Checks"},
				parameterModels:     []any{checkIDURI{}, tenantQueryRequest{}},
				requestBodyModel:    ChangeCheckStatusRequest{},
				requestBodyRequired: true,
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Paused check", model: checkcommand.ChangeStatusResult{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "Check not found", model: errorModel},
					{statusCode: stdhttp.StatusConflict, description: "Version conflict", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodPost,
			path:       "/checks/:id/resume",
			permission: "check:Resume",
			resource:   tenantScoped("check"),
			enabled:    func(h *Handler) bool { return h != nil && h.resumeCheckUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.POST("/checks/:id/resume", h.ResumeCheck) },
			spec: operationDefinition{
				id:                  "resumeCheck",
				summary:             "Resume one check",
				description:         "Resume a paused check definition using optimistic locking by version.",
				tags:                []string{"Checks"},
				parameterModels:     []any{checkIDURI{}, tenantQueryRequest{}},
				requestBodyModel:    ChangeCheckStatusRequest{},
				requestBodyRequired: true,
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Resumed check", model: checkcommand.ChangeStatusResult{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "Check not found", model: errorModel},
					{statusCode: stdhttp.StatusConflict, description: "Version conflict", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodDelete,
			path:       "/checks/:id",
			permission: "check:Delete",
			resource:   tenantScoped("check"),
			enabled:    func(h *Handler) bool { return h != nil && h.deleteCheckUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.DELETE("/checks/:id", h.DeleteCheck) },
			spec: operationDefinition{
				id:                  "deleteCheck",
				summary:             "Delete one check",
				description:         "Soft-delete a check definition by setting deleted_at. The version body is required for optimistic locking.",
				tags:                []string{"Checks"},
				parameterModels:     []any{checkIDURI{}, tenantQueryRequest{}},
				requestBodyModel:    VersionRequest{},
				requestBodyRequired: true,
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Deleted check"},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "Check not found", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		// ==================== Check Runs ====================
		{
			method:     stdhttp.MethodGet,
			path:       "/check-runs",
			permission: "checkrun:List",
			resource:   tenantScoped("checkrun"),
			enabled:    func(h *Handler) bool { return h != nil && h.listCheckRunsUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.GET("/check-runs", h.ListCheckRuns) },
			spec: operationDefinition{
				id:              "listCheckRuns",
				summary:         "List check runs",
				description:     "List check execution runs for one tenant.",
				tags:            []string{"Check Runs"},
				parameterModels: []any{ListCheckRunsRequest{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Check run list", model: checkRunListResponse{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodGet,
			path:       "/check-runs/:id",
			permission: "checkrun:Get",
			resource:   tenantScoped("checkrun"),
			enabled:    func(h *Handler) bool { return h != nil && h.getCheckRunUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.GET("/check-runs/:id", h.GetCheckRun) },
			spec: operationDefinition{
				id:              "getCheckRun",
				summary:         "Get one check run",
				description:     "Get one check run by id.",
				tags:            []string{"Check Runs"},
				parameterModels: []any{GetCheckRunRequest{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Check run detail", model: checkrunquery.GetResult{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "Check run not found", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		// ==================== SLIs ====================
		{
			method:     stdhttp.MethodPost,
			path:       "/slis",
			permission: "sli:Create",
			resource:   tenantScoped("sli"),
			enabled:    func(h *Handler) bool { return h != nil && h.createSLIUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.POST("/slis", h.CreateSLI) },
			spec: operationDefinition{
				id:                  "createSLI",
				summary:             "Create one SLI",
				description:         "Create a service level indicator definition.",
				tags:                []string{"SLIs"},
				requestBodyModel:    CreateSLIRequest{},
				requestBodyRequired: true,
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusCreated, description: "Created SLI", model: slicommand.CreateResult{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodGet,
			path:       "/slis",
			permission: "sli:List",
			resource:   tenantScoped("sli"),
			enabled:    func(h *Handler) bool { return h != nil && h.listSLIsUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.GET("/slis", h.ListSLIs) },
			spec: operationDefinition{
				id:              "listSLIs",
				summary:         "List SLIs",
				description:     "List service level indicators for one tenant.",
				tags:            []string{"SLIs"},
				parameterModels: []any{ListSLIsRequest{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "SLI list", model: sliListResponse{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodGet,
			path:       "/slis/:id",
			permission: "sli:Get",
			resource:   tenantScoped("sli"),
			enabled:    func(h *Handler) bool { return h != nil && h.getSLIUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.GET("/slis/:id", h.GetSLI) },
			spec: operationDefinition{
				id:              "getSLI",
				summary:         "Get one SLI",
				description:     "Get one service level indicator by id.",
				tags:            []string{"SLIs"},
				parameterModels: []any{GetSLIRequest{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "SLI detail", model: sliquery.GetItem{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "SLI not found", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodDelete,
			path:       "/slis/:id",
			permission: "sli:Delete",
			resource:   tenantScoped("sli"),
			enabled:    func(h *Handler) bool { return h != nil && h.deleteSLIUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.DELETE("/slis/:id", h.DeleteSLI) },
			spec: operationDefinition{
				id:                  "deleteSLI",
				summary:             "Delete one SLI",
				description:         "Soft-delete a service level indicator by setting deleted_at. The version body is required for optimistic locking.",
				tags:                []string{"SLIs"},
				parameterModels:     []any{sliIDURI{}, tenantQueryRequest{}},
				requestBodyModel:    VersionRequest{},
				requestBodyRequired: true,
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Deleted SLI"},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "SLI not found", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		// ==================== SLOs ====================
		{
			method:     stdhttp.MethodPost,
			path:       "/slos",
			permission: "slo:Create",
			resource:   tenantScoped("slo"),
			enabled:    func(h *Handler) bool { return h != nil && h.createSLOUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.POST("/slos", h.CreateSLO) },
			spec: operationDefinition{
				id:                  "createSLO",
				summary:             "Create one SLO",
				description:         "Create a service level objective definition.",
				tags:                []string{"SLOs"},
				requestBodyModel:    CreateSLORequest{},
				requestBodyRequired: true,
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusCreated, description: "Created SLO", model: slocommand.CreateResult{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodGet,
			path:       "/slos",
			permission: "slo:List",
			resource:   tenantScoped("slo"),
			enabled:    func(h *Handler) bool { return h != nil && h.listSLOsUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.GET("/slos", h.ListSLOs) },
			spec: operationDefinition{
				id:              "listSLOs",
				summary:         "List SLOs",
				description:     "List service level objectives for one tenant.",
				tags:            []string{"SLOs"},
				parameterModels: []any{ListSLOsRequest{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "SLO list", model: sloListResponse{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodGet,
			path:       "/slos/:id",
			permission: "slo:Get",
			resource:   tenantScoped("slo"),
			enabled:    func(h *Handler) bool { return h != nil && h.getSLOUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.GET("/slos/:id", h.GetSLO) },
			spec: operationDefinition{
				id:              "getSLO",
				summary:         "Get one SLO",
				description:     "Get one service level objective by id.",
				tags:            []string{"SLOs"},
				parameterModels: []any{GetSLORequest{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "SLO detail", model: sloquery.GetItem{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "SLO not found", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodPost,
			path:       "/slos/:id/pause",
			permission: "slo:Pause",
			resource:   tenantScoped("slo"),
			enabled:    func(h *Handler) bool { return h != nil && h.statusSLOUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.POST("/slos/:id/pause", h.PauseSLO) },
			spec: operationDefinition{
				id:                  "pauseSLO",
				summary:             "Pause one SLO",
				description:         "Pause an active service level objective using optimistic locking by version.",
				tags:                []string{"SLOs"},
				parameterModels:     []any{sloIDURI{}, tenantQueryRequest{}},
				requestBodyModel:    ChangeSLOStatusRequest{},
				requestBodyRequired: true,
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Paused SLO", model: slocommand.ChangeStatusResult{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "SLO not found", model: errorModel},
					{statusCode: stdhttp.StatusConflict, description: "Version conflict", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodPost,
			path:       "/slos/:id/resume",
			permission: "slo:Resume",
			resource:   tenantScoped("slo"),
			enabled:    func(h *Handler) bool { return h != nil && h.statusSLOUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.POST("/slos/:id/resume", h.ResumeSLO) },
			spec: operationDefinition{
				id:                  "resumeSLO",
				summary:             "Resume one SLO",
				description:         "Resume a paused service level objective using optimistic locking by version.",
				tags:                []string{"SLOs"},
				parameterModels:     []any{sloIDURI{}, tenantQueryRequest{}},
				requestBodyModel:    ChangeSLOStatusRequest{},
				requestBodyRequired: true,
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Resumed SLO", model: slocommand.ChangeStatusResult{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "SLO not found", model: errorModel},
					{statusCode: stdhttp.StatusConflict, description: "Version conflict", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodDelete,
			path:       "/slos/:id",
			permission: "slo:Delete",
			resource:   tenantScoped("slo"),
			enabled:    func(h *Handler) bool { return h != nil && h.deleteSLOUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.DELETE("/slos/:id", h.DeleteSLO) },
			spec: operationDefinition{
				id:                  "deleteSLO",
				summary:             "Delete one SLO",
				description:         "Soft-delete a service level objective by setting deleted_at. The version body is required for optimistic locking.",
				tags:                []string{"SLOs"},
				parameterModels:     []any{sloIDURI{}, tenantQueryRequest{}},
				requestBodyModel:    VersionRequest{},
				requestBodyRequired: true,
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Deleted SLO"},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "SLO not found", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodGet,
			path:       "/slos/:id/budget",
			permission: "slo:GetBudget",
			resource:   tenantScoped("slo"),
			enabled:    func(h *Handler) bool { return h != nil && h.getBudgetUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.GET("/slos/:id/budget", h.GetSLOBudget) },
			spec: operationDefinition{
				id:              "getSLOBudget",
				summary:         "Get SLO budget",
				description:     "Get the current error budget for one SLO.",
				tags:            []string{"SLO Budgets"},
				parameterModels: []any{GetSLOBudgetRequest{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "SLO budget detail", model: slobudgetquery.GetItem{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "SLO not found", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodGet,
			path:       "/slos/:id/budgets",
			permission: "slo:ListBudgets",
			resource:   tenantScoped("slo"),
			enabled:    func(h *Handler) bool { return h != nil && h.listBudgetHistoryUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.GET("/slos/:id/budgets", h.ListSLOBudgets) },
			spec: operationDefinition{
				id:              "listSLOBudgets",
				summary:         "List SLO budget history",
				description:     "List error budget history entries for one SLO.",
				tags:            []string{"SLO Budgets"},
				parameterModels: []any{ListSLOBudgetsRequest{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "SLO budget history list", model: budgetListResponse{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		// ==================== SLO Alerts ====================
		{
			method:     stdhttp.MethodGet,
			path:       "/slo-alerts",
			permission: "sloalert:List",
			resource:   tenantScoped("sloalert"),
			enabled:    func(h *Handler) bool { return h != nil && h.listAlertsUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.GET("/slo-alerts", h.ListSLOAlerts) },
			spec: operationDefinition{
				id:              "listSLOAlerts",
				summary:         "List SLO alerts",
				description:     "List SLO alert entries for one tenant.",
				tags:            []string{"SLO Alerts"},
				parameterModels: []any{ListSLOAlertsRequest{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "SLO alert list", model: alertListResponse{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodGet,
			path:       "/slo-alerts/:id",
			permission: "sloalert:Get",
			resource:   tenantScoped("sloalert"),
			enabled:    func(h *Handler) bool { return h != nil && h.getAlertUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.GET("/slo-alerts/:id", h.GetSLOAlert) },
			spec: operationDefinition{
				id:              "getSLOAlert",
				summary:         "Get one SLO alert",
				description:     "Get one SLO alert by id.",
				tags:            []string{"SLO Alerts"},
				parameterModels: []any{GetSLOAlertRequest{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "SLO alert detail", model: sloalertquery.GetItem{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "SLO alert not found", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		// ==================== Tenants ====================
		{
			method:     stdhttp.MethodPost,
			path:       "/tenants",
			permission: "tenant:Create",
			resource:   tenantResource,
			enabled:    func(h *Handler) bool { return h != nil && h.createTenantUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.POST("/tenants", h.CreateTenant) },
			spec: operationDefinition{
				id:                  "createTenant",
				summary:             "Create one tenant",
				description:         "Create a new tenant. Status defaults to active when omitted.",
				tags:                []string{"Tenants"},
				requestBodyModel:    CreateTenantRequest{},
				requestBodyRequired: true,
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusCreated, description: "Created tenant", model: tenantcommand.TenantCreateResult{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodGet,
			path:       "/tenants",
			permission: "tenant:List",
			resource:   tenantResource,
			enabled:    func(h *Handler) bool { return h != nil && h.listTenantsUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.GET("/tenants", h.ListTenants) },
			spec: operationDefinition{
				id:              "listTenants",
				summary:         "List tenants",
				description:     "List tenants. Limit defaults to 50 when omitted.",
				tags:            []string{"Tenants"},
				parameterModels: []any{ListTenantsRequest{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Tenant list", model: tenantListResponse{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodGet,
			path:       "/tenants/:id",
			permission: "tenant:Get",
			resource:   tenantResource,
			enabled:    func(h *Handler) bool { return h != nil && h.getTenantUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.GET("/tenants/:id", h.GetTenant) },
			spec: operationDefinition{
				id:              "getTenant",
				summary:         "Get one tenant",
				description:     "Get one tenant by id.",
				tags:            []string{"Tenants"},
				parameterModels: []any{TenantURI{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Tenant detail", model: tenantquery.TenantGetResult{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "Tenant not found", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		// ==================== API Keys ====================
		{
			method:     stdhttp.MethodPost,
			path:       "/tenants/:id/api_keys",
			permission: "apikey:Create",
			resource:   apiKeyResource,
			enabled:    func(h *Handler) bool { return h != nil && h.createAPIKeyUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.POST("/tenants/:id/api_keys", h.CreateAPIKey) },
			spec: operationDefinition{
				id:                  "createAPIKey",
				summary:             "Create one API key",
				description:         "Create a new API key for a tenant. The full key is returned only once.",
				tags:                []string{"API Keys"},
				parameterModels:     []any{TenantURI{}},
				requestBodyModel:    CreateAPIKeyRequest{},
				requestBodyRequired: false,
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusCreated, description: "Created API key", model: apikeycommand.APIKeyCreateResult{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodGet,
			path:       "/tenants/:id/api_keys",
			permission: "apikey:List",
			resource:   apiKeyResource,
			enabled:    func(h *Handler) bool { return h != nil && h.listAPIKeysUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.GET("/tenants/:id/api_keys", h.ListAPIKeys) },
			spec: operationDefinition{
				id:              "listAPIKeys",
				summary:         "List API keys",
				description:     "List API keys for a tenant. Full keys and hashes are never exposed.",
				tags:            []string{"API Keys"},
				parameterModels: []any{TenantURI{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "API key list", model: apiKeyListResponse{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodPost,
			path:       "/api_keys/:id/revoke",
			permission: "apikey:Revoke",
			resource:   apiKeyResource,
			enabled:    func(h *Handler) bool { return h != nil && h.revokeAPIKeyUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.POST("/api_keys/:id/revoke", h.RevokeAPIKey) },
			spec: operationDefinition{
				id:              "revokeAPIKey",
				summary:         "Revoke one API key",
				description:     "Revoke an API key. The key must belong to the caller's tenant and not already be revoked.",
				tags:            []string{"API Keys"},
				parameterModels: []any{APIKeyURI{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Revoked API key", model: apikeycommand.APIKeyRevokeResult{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "API key not found", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		// ==================== Policies ====================
		{
			method:     stdhttp.MethodPost,
			path:       "/policies",
			permission: "policy:Create",
			resource:   tenantScoped("policy"),
			enabled:    func(h *Handler) bool { return h != nil && h.createPolicyUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.POST("/policies", h.CreatePolicy) },
			spec: operationDefinition{
				id:                  "createPolicy",
				summary:             "Create one policy",
				description:         "Create an authorization policy in the caller's tenant. Actions the platform does not implement, and the \"*\" wildcard, are rejected.",
				tags:                []string{"Policies"},
				requestBodyModel:    CreatePolicyRequest{},
				requestBodyRequired: true,
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusCreated, description: "Created policy", model: policycommand.CreateResult{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusForbidden, description: "Not permitted", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodGet,
			path:       "/policies",
			permission: "policy:List",
			resource:   tenantScoped("policy"),
			enabled:    func(h *Handler) bool { return h != nil && h.listPoliciesUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.GET("/policies", h.ListPolicies) },
			spec: operationDefinition{
				id:          "listPolicies",
				summary:     "List policies",
				description: "List the policies the caller's tenant may bind: its own, and the platform presets.",
				tags:        []string{"Policies"},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Policy list", model: policyListResponse{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodGet,
			path:       "/policies/:id",
			permission: "policy:Get",
			resource:   tenantScoped("policy"),
			enabled:    func(h *Handler) bool { return h != nil && h.getPolicyUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.GET("/policies/:id", h.GetPolicy) },
			spec: operationDefinition{
				id:              "getPolicy",
				summary:         "Get one policy",
				description:     "Read one policy belonging to the caller's tenant, or a platform preset.",
				tags:            []string{"Policies"},
				parameterModels: []any{PolicyURI{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Policy detail", model: policyquery.GetResult{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "Policy not found", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodDelete,
			path:       "/policies/:id",
			permission: "policy:Delete",
			resource:   tenantScoped("policy"),
			enabled:    func(h *Handler) bool { return h != nil && h.deletePolicyUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.DELETE("/policies/:id", h.DeletePolicy) },
			spec: operationDefinition{
				id:              "deletePolicy",
				summary:         "Delete one policy",
				description:     "Delete a policy the caller's tenant owns. Platform presets are never deletable, and a policy still bound to a key is refused rather than cascaded.",
				tags:            []string{"Policies"},
				parameterModels: []any{PolicyURI{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusNoContent, description: "Deleted policy"},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusForbidden, description: "Not permitted", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "Policy not found", model: errorModel},
					{statusCode: stdhttp.StatusConflict, description: "Policy still bound", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		// ==================== Resource Groups ====================
		{
			method:     stdhttp.MethodPost,
			path:       "/resource_groups",
			permission: "group:Create",
			resource:   tenantScoped("group"),
			enabled:    func(h *Handler) bool { return h != nil && h.createGroupUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.POST("/resource_groups", h.CreateResourceGroup) },
			spec: operationDefinition{
				id:                  "createResourceGroup",
				summary:             "Create one resource group",
				description:         "Create an isolation group inside the caller's tenant. Keys scoped to a group see only that group's resources.",
				tags:                []string{"Resource Groups"},
				requestBodyModel:    CreateResourceGroupRequest{},
				requestBodyRequired: true,
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusCreated, description: "Created resource group", model: resourcegroupcommand.CreateResult{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusConflict, description: "Slug already used", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodGet,
			path:       "/resource_groups",
			permission: "group:List",
			resource:   tenantScoped("group"),
			enabled:    func(h *Handler) bool { return h != nil && h.listGroupsUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.GET("/resource_groups", h.ListResourceGroups) },
			spec: operationDefinition{
				id:          "listResourceGroups",
				summary:     "List resource groups",
				description: "List the resource groups belonging to the caller's tenant.",
				tags:        []string{"Resource Groups"},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Resource group list", model: resourceGroupListResponse{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		// ==================== Functions ====================
		{
			method:     stdhttp.MethodGet,
			path:       "/functions",
			permission: "function:List",
			resource:   tenantScoped("function"),
			enabled:    func(h *Handler) bool { return h != nil && h.listFunctionsUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.GET("/functions", h.ListFunctions) },
			spec: operationDefinition{
				id:              "listFunctions",
				summary:         "List function definitions",
				description:     "List the function definitions in the tenant, newest first. A function is a tenant-owned definition row; a key scoped to a resource group sees only that group's functions. limit defaults to 50 when omitted.",
				tags:            []string{"Functions"},
				parameterModels: []any{ListFunctionsRequest{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Function list", model: functionListResponse{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodGet,
			path:       "/functions/:id",
			permission: "function:Get",
			resource:   tenantScoped("function"),
			enabled:    func(h *Handler) bool { return h != nil && h.getFunctionUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.GET("/functions/:id", h.GetFunction) },
			spec: operationDefinition{
				id:              "getFunction",
				summary:         "Get one function definition",
				description:     "Get one function definition by id. A function has no schedule: it is invoke-only, and every execution-shaping field is pinned into the revision each invocation runs.",
				tags:            []string{"Functions"},
				parameterModels: []any{GetFunctionRequest{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Function detail", model: FunctionItem{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "Function not found", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodPost,
			path:       "/functions/:id/invoke",
			permission: "function:Invoke",
			resource:   tenantScoped("function"),
			enabled:    func(h *Handler) bool { return h != nil && h.invokeFunctionUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.POST("/functions/:id/invoke", h.InvokeFunction) },
			spec: operationDefinition{
				id:              "invokeFunction",
				summary:         "Invoke a function",
				description:     "Invoke a function by publishing a JobRun custom resource, exactly as a manual job trigger does; the operator materializes the ledger run from the pinned revision. The request carries no body: v1 functions are parameterless, and parameters are baked into the definition. With wait_seconds (capped at 60) the call polls the custom resource until a terminal phase or the budget expires, then answers with the phase observed. Every invocation is a cold pod, so latency is honest seconds, not milliseconds -- and the only result channel is the exit outcome: the platform returns a phase and a duration, never logs or a response body. Invoking a paused function is a conflict. Use the X-OrbitJob-Idempotency-Key header to prevent duplicate invocations.",
				tags:            []string{"Functions"},
				parameterModels: []any{InvokeFunctionRequest{}, idempotencyKeyHeaderRequest{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusCreated, description: "Invocation published", model: FunctionInvokeResult{}},
					{statusCode: stdhttp.StatusOK, description: "Existing invocation returned for idempotent replay, or the wait expired and the async reference is repeated", model: FunctionInvokeResult{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "Function not found", model: errorModel},
					{statusCode: stdhttp.StatusConflict, description: "The function is paused, or has no active revision yet", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodGet,
			path:       "/functions/:id/runs",
			permission: "function:List",
			resource:   tenantScoped("function"),
			enabled:    func(h *Handler) bool { return h != nil && h.listFunctionRunsUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.GET("/functions/:id/runs", h.ListFunctionRuns) },
			spec: operationDefinition{
				id:              "listFunctionRuns",
				summary:         "List function invocations",
				description:     "List a function's terminal invocations from the function_runs read model, newest first. The model records outcomes only -- success, failed, canceled -- because live progress lives on the run's JobRun custom resource and the ledger row.",
				tags:            []string{"Functions"},
				parameterModels: []any{ListFunctionRunsRequest{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Function run list", model: functionRunListResponse{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "Function not found", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodGet,
			path:       "/functions/:id/runs/:run_id",
			permission: "function:Get",
			resource:   tenantScoped("function"),
			enabled:    func(h *Handler) bool { return h != nil && h.getFunctionRunUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.GET("/functions/:id/runs/:run_id", h.GetFunctionRun) },
			spec: operationDefinition{
				id:              "getFunctionRun",
				summary:         "Get one function invocation",
				description:     "Get one terminal invocation by its deterministic run id, which is derived from the ledger occurrence key so a replayed recording addresses the same row.",
				tags:            []string{"Functions"},
				parameterModels: []any{GetFunctionRunRequest{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Function run detail", model: FunctionRunItem{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "Function or run not found", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		// ==================== Workflows ====================
		{
			method:     stdhttp.MethodGet,
			path:       "/workflows",
			permission: "workflow:List",
			resource:   tenantScoped("workflow"),
			enabled:    func(h *Handler) bool { return h != nil && h.listWorkflowsUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.GET("/workflows", h.ListWorkflows) },
			spec: operationDefinition{
				id:              "listWorkflows",
				summary:         "List workflow definitions",
				description:     "List the active revision of every WorkflowJob definition in the tenant. A workflow definition is a WorkflowJob custom resource, declared outside this API and read here through its projection; there is no create route. limit defaults to 50 when omitted.",
				tags:            []string{"Workflows"},
				parameterModels: []any{ListWorkflowsRequest{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Workflow list", model: workflowListResponse{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusForbidden, description: "A group-scoped key cannot reach workflow definitions", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodGet,
			path:       "/workflows/:id",
			permission: "workflow:Get",
			resource:   tenantScoped("workflow"),
			enabled:    func(h *Handler) bool { return h != nil && h.getWorkflowUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.GET("/workflows/:id", h.GetWorkflow) },
			spec: operationDefinition{
				id:              "getWorkflow",
				summary:         "Get one workflow definition",
				description:     "Get the active revision of one workflow definition by revision id, with its task DAG: each task names the ScheduledJob definition it runs, the tasks that must finish first, and an optional condition over the terminal state of its dependencies.",
				tags:            []string{"Workflows"},
				parameterModels: []any{GetWorkflowRequest{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Workflow detail", model: WorkflowGetItem{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusForbidden, description: "A group-scoped key cannot reach workflow definitions", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "Workflow not found", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodPost,
			path:       "/workflows/:id/runs",
			permission: "workflow:Trigger",
			resource:   tenantScoped("workflow"),
			enabled:    func(h *Handler) bool { return h != nil && h.triggerWorkflowUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.POST("/workflows/:id/runs", h.TriggerWorkflow) },
			spec: operationDefinition{
				id:              "triggerWorkflow",
				summary:         "Trigger a manual workflow run",
				description:     "Publish a WorkflowRun custom resource for a manual workflow execution. The operator materializes the workflow run row from it, row-first, and then walks the DAG: each task is an ordinary run of the ScheduledJob definition the task names. The calling credential is recorded as the run's actor, so \"who triggered it\" is answerable after the fact. Triggering a suspended workflow is a conflict. Use the X-OrbitJob-Idempotency-Key header to prevent duplicate triggers.",
				tags:            []string{"Workflows"},
				parameterModels: []any{jobIDURI{}, idempotencyKeyHeaderRequest{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusCreated, description: "Created workflow run", model: WorkflowTriggerResult{}},
					{statusCode: stdhttp.StatusOK, description: "Existing run returned for idempotent trigger", model: WorkflowTriggerResult{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusForbidden, description: "A group-scoped key cannot trigger workflows", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "Workflow not found", model: errorModel},
					{statusCode: stdhttp.StatusConflict, description: "Workflow is suspended", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodGet,
			path:       "/workflows/:id/runs",
			permission: "workflow:List",
			resource:   tenantScoped("workflow"),
			enabled:    func(h *Handler) bool { return h != nil && h.listWorkflowRunsUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.GET("/workflows/:id/runs", h.ListWorkflowRuns) },
			spec: operationDefinition{
				id:              "listWorkflowRuns",
				summary:         "List workflow runs",
				description:     "List one workflow's runs from the workflow ledger, newest first. Each entry carries the workflow-level phase -- the DAG's lifecycle, not any single step's -- its trigger, and the actor that asked for it.",
				tags:            []string{"Workflows"},
				parameterModels: []any{ListWorkflowRunsRequest{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Workflow run list", model: workflowRunListResponse{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusForbidden, description: "A group-scoped key cannot reach workflow runs", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "Workflow not found", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodGet,
			path:       "/workflows/:id/runs/:run_id",
			permission: "workflow:Get",
			resource:   tenantScoped("workflow"),
			enabled:    func(h *Handler) bool { return h != nil && h.getWorkflowRunUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.GET("/workflows/:id/runs/:run_id", h.GetWorkflowRun) },
			spec: operationDefinition{
				id:              "getWorkflowRun",
				summary:         "Get one workflow run",
				description:     "Get one workflow run by ledger id, with its step runs in the same response. Each step is an ordinary run of the ScheduledJob definition its task names, grouped under the workflow run; a task the walker decided not to create has no step -- its skip reason lives on the run's task decisions, visible in the phase trail.",
				tags:            []string{"Workflows"},
				parameterModels: []any{workflowRunIDURI{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Workflow run detail", model: WorkflowRunGetItem{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusForbidden, description: "A group-scoped key cannot reach workflow runs", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "Workflow or run not found", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
		{
			method:     stdhttp.MethodPost,
			path:       "/workflows/:id/runs/:run_id/cancel",
			permission: "workflow:Trigger",
			resource:   tenantScoped("workflow"),
			enabled:    func(h *Handler) bool { return h != nil && h.cancelWorkflowRunUC != nil },
			register:   func(r gin.IRouter, h *Handler) { r.POST("/workflows/:id/runs/:run_id/cancel", h.CancelWorkflowRun) },
			spec: operationDefinition{
				id:              "cancelWorkflowRun",
				summary:         "Request a workflow run cancel",
				description:     "Request a stop for one workflow run by patching spec.cancelRequested on its WorkflowRun custom resource. The request carries no body. The operator fans the stop out over the workflow's non-terminal step runs; the workflow reaches Canceled only once its steps are observed terminal, and the response carries the phase observed at request time. Canceling a run that already finished succeeds and reports the terminal phase.",
				tags:            []string{"Workflows"},
				parameterModels: []any{workflowRunIDURI{}},
				responses: []responseDefinition{
					{statusCode: stdhttp.StatusOK, description: "Cancel request accepted, or the run was already terminal", model: WorkflowCancelResult{}},
					{statusCode: stdhttp.StatusBadRequest, description: "Invalid request", model: errorModel},
					{statusCode: stdhttp.StatusForbidden, description: "A group-scoped key cannot cancel workflow runs", model: errorModel},
					{statusCode: stdhttp.StatusNotFound, description: "Workflow or run not found", model: errorModel},
					{statusCode: stdhttp.StatusConflict, description: "The run's WorkflowRun custom resource is missing", model: errorModel},
					{statusCode: stdhttp.StatusInternalServerError, description: "Internal error", model: errorModel},
				},
			},
		},
	}

	for i := range routes {
		routes[i].spec.responses = append(routes[i].spec.responses, unauthorizedResponse, rateLimitedResponse)
	}

	return routes
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
	case stdhttp.MethodDelete:
		pathItem.Delete = &operation
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
		if len(response.headers) > 0 {
			if item.Headers == nil {
				item.Headers = map[string]Header{}
			}
			for name, header := range response.headers {
				item.Headers[name] = header
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
	case "limit":
		parameter.Schema.Default = jobquery.DefaultListLimit
	case "offset":
		parameter.Schema.Default = 0
	}
}

func newSchemaRegistry() *schemaRegistry {
	return &schemaRegistry{
		components: map[string]Schema{},
		seen:       map[reflect.Type]string{},
		owners:     map[string]reflect.Type{},
	}
}

// uniqueComponentName returns the component name for t, qualifying it with the
// parent package when a different type already owns the bare name. The qualifier
// keeps the spec unambiguous without renaming the schemas that never collide.
func (r *schemaRegistry) uniqueComponentName(t reflect.Type) string {
	name := schemaComponentName(t)
	if _, taken := r.owners[name]; !taken {
		r.owners[name] = t
		return name
	}

	qualified := packageQualifier(t) + name
	base := qualified
	for i := 2; ; i++ {
		if _, taken := r.owners[qualified]; !taken {
			r.owners[qualified] = t
			return qualified
		}
		qualified = fmt.Sprintf("%s%d", base, i)
	}
}

// packageQualifier names the package a type came from, using the parent segment
// when the final one is a layer word ("query", "command") that several packages
// share: run/query becomes "Run", job/command becomes "Job".
func packageQualifier(t reflect.Type) string {
	pkg := t.PkgPath()
	if pkg == "" {
		return ""
	}

	parts := strings.Split(pkg, "/")
	name := parts[len(parts)-1]
	switch name {
	case "query", "command", "http":
		if len(parts) >= 2 {
			name = parts[len(parts)-2]
		}
	}
	if name == "" {
		return ""
	}

	return strings.ToUpper(name[:1]) + name[1:]
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

		name := r.uniqueComponentName(t)
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
	if name != "APIError" {
		return
	}

	if property, ok := schema.Properties["code"]; ok {
		property.Enum = []string{
			string(apperror.CodeMalformedRequest),
			string(apperror.CodeValidation),
			string(apperror.CodeUnauthorized),
			string(apperror.CodeForbidden),
			string(apperror.CodeNotFound),
			string(apperror.CodeConflict),
			string(apperror.CodeRateLimited),
			string(apperror.CodeInternal),
			string(apperror.CodeServiceUnavailable),
		}
		schema.Properties["code"] = property
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
		case strings.HasPrefix(rule, "len="):
			value, err := strconv.Atoi(strings.TrimPrefix(rule, "len="))
			if err != nil {
				continue
			}
			if schema.Type == "string" {
				length := value
				schema.MinLength = &length
				schema.MaxLength = &length
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
