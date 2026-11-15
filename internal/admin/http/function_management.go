package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	"bytes"
	functioncommand "orbitjob/internal/admin/app/function/command"
	"orbitjob/internal/admin/http/apperror"
	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/core/app/functioninvoke"
	"orbitjob/internal/core/domain/function"
	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/domain/executionpolicy"
	"orbitjob/internal/domain/validation"
	"orbitjob/internal/platform/strictjson"
)

type functionManager interface {
	Create(context.Context, functioncommand.Input) (functioncommand.CreateResult, error)
	Update(context.Context, functioncommand.Input) (function.Definition, error)
}

func (h *Handler) SetFunctionManagementUseCase(manager functionManager, reader functionReader, revisions functioninvoke.RevisionSource) {
	h.functionManager, h.functionManagementReader, h.functionManagementReadiness = manager, reader, revisions
}

// FunctionDefinitionRequest is a complete container definition. Omitted
// optional settings use the same defaults on creation and replacement.
type FunctionDefinitionRequest struct {
	ExecutionPolicyVersion int32                      `json:"execution_policy_version,omitempty" binding:"min=0,max=1"`
	RetrySafe              bool                       `json:"retry_safe,omitempty"`
	Resources              *executionpolicy.Resources `json:"resources,omitempty"`
	Name                   string                     `json:"name" binding:"required,max=128"`
	Description            string                     `json:"description"`
	Status                 string                     `json:"status" binding:"omitempty,oneof=active paused"`
	Image                  string                     `json:"image" binding:"required,max=255"`
	Command                []string                   `json:"command"`
	Args                   []string                   `json:"args"`
	TimeoutSeconds         *int                       `json:"timeout_seconds" binding:"omitempty,min=1,max=2147483647"`
	RetryLimit             int                        `json:"retry_limit" binding:"min=0,max=2147483646"`
	HistorySuccess         *int                       `json:"history_success" binding:"omitempty,min=0,max=2147483646"`
	HistoryFailed          *int                       `json:"history_failed" binding:"omitempty,min=0,max=2147483646"`
	Labels                 map[string]any             `json:"labels"`
}

type UpdateFunctionRequest struct {
	FunctionDefinitionRequest
	ExpectedVersion int `json:"expected_version" binding:"required,min=1"`
}

type requiredFunctionIdempotencyHeader struct {
	IdempotencyKey string `header:"X-OrbitJob-Idempotency-Key" binding:"required,max=128"`
}

func (req FunctionDefinitionRequest) definition(tenantID string) function.Definition {
	status := req.Status
	if status == "" {
		status = function.StatusActive
	}
	timeout, success, failed := function.DefaultTimeoutSeconds, function.DefaultHistorySuccess, function.DefaultHistoryFailed
	if req.TimeoutSeconds != nil {
		timeout = *req.TimeoutSeconds
	}
	if req.HistorySuccess != nil {
		success = *req.HistorySuccess
	}
	if req.HistoryFailed != nil {
		failed = *req.HistoryFailed
	}
	return function.Definition{TenantID: tenantID, Name: req.Name, Description: req.Description,
		Status: status, Image: req.Image, Command: req.Command, Args: req.Args,
		ExecutionPolicyVersion: req.ExecutionPolicyVersion, RetrySafe: req.RetrySafe, Resources: req.Resources,
		TimeoutSeconds: timeout, RetryLimit: req.RetryLimit, HistorySuccess: success,
		HistoryFailed: failed, Labels: req.Labels}
}

func decodeFunctionDefinition(c *gin.Context, req any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, strictjson.MaxBytes)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil || strictjson.Validate(body) != nil {
		writeAPIError(c, validation.New("body", "use one bounded JSON object without duplicate fields"))
		return false
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(req); err != nil {
		writeAPIError(c, validation.New("body", "invalid typed Function definition"))
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		writeAPIError(c, validation.New("body", "exactly one Function definition is required"))
		return false
	}
	if err := binding.Validator.ValidateStruct(req); err != nil {
		apperror.Write(c, http.StatusBadRequest, toBindAPIError(err))
		return false
	}
	return true
}

func (h *Handler) CreateFunction(c *gin.Context) {
	tenantID, ok := requireTenantID(c, c.Query("tenant_id"))
	if !ok {
		return
	}
	var req FunctionDefinitionRequest
	if !decodeFunctionDefinition(c, &req) {
		return
	}
	key, err := parseIdempotencyKey(c)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	if key == nil {
		writeAPIError(c, validation.New("idempotency_key", "is required for function creation"))
		return
	}
	out, err := h.functionManager.Create(c.Request.Context(), functioncommand.Input{
		Definition: req.definition(tenantID), ActorID: actorID(c), ResourceGroupID: groupFrom(c), IdempotencyKey: *key,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}
	status := http.StatusCreated
	if !out.Created {
		status = http.StatusOK
	}
	item, err := functionItemWithReadiness(c.Request.Context(), out.Definition, h.functionManagementReadiness)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	if !out.Created && item.Ready {
		// A replay confirms the original write without exposing later edits.
		// Current pause/version still govern whether that snapshot is callable.
		item.Ready = false
		if h.functionManagementReader != nil {
			current, found, err := h.functionManagementReader.GetForTenant(c.Request.Context(), tenantID, out.Definition.ID)
			if err != nil {
				writeAPIError(c, err)
				return
			}
			item.Ready = found && current.Version == item.Version && current.Status == function.StatusActive && current.ResourceGroupID == out.Definition.ResourceGroupID
		}
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(status, item)
}

func (h *Handler) UpdateFunction(c *gin.Context) {
	tenantID, ok := requireTenantID(c, c.Query("tenant_id"))
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		writeAPIError(c, validation.New("id", "must be >= 1"))
		return
	}
	var req UpdateFunctionRequest
	if !decodeFunctionDefinition(c, &req) {
		return
	}
	d := req.definition(tenantID)
	d.ID, d.Version = id, req.ExpectedVersion
	out, err := h.functionManager.Update(c.Request.Context(), functioncommand.Input{
		Definition: d, ActorID: actorID(c), ResourceGroupID: groupFrom(c),
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}
	item, err := functionItemWithReadiness(c.Request.Context(), out, h.functionManagementReadiness)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, item)
}

// Resolve the persisted row's group, even for unscoped credentials, so a
// group-specific explicit Deny cannot be bypassed through a wildcard target.
func (h *Handler) functionResource(c *gin.Context) (policy.ARN, error) {
	tenantID, err := requestTenant(c)
	if err != nil {
		return policy.ARN{}, err
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		return policy.ARN{}, validation.New("id", "must be >= 1")
	}
	d, found, err := h.functionManagementReader.GetForTenant(c.Request.Context(), tenantID, id)
	if err != nil {
		return policy.ARN{}, fmt.Errorf("%w: function lookup failed", middleware.ErrAuthorizationLookup)
	}
	if !found || !visibleToGroup(d.ResourceGroupID, groupFrom(c)) {
		return policy.ARN{}, middleware.ErrResourceUnavailable
	}
	group := d.ResourceGroupID
	if group == "" {
		group = "*"
	}
	return policy.ARN{Tenant: tenantID, Group: group, Type: "function", ID: strconv.FormatInt(id, 10)}, nil
}
