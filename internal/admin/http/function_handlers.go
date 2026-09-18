package http

import (
	"context"
	stdhttp "net/http"

	"github.com/gin-gonic/gin"

	"orbitjob/internal/admin/http/apperror"
	"orbitjob/internal/core/app/functioninvoke"
)

// The function routes serve the definitions read-only plus invoke in v1: a
// function is a tenant-owned row, and the actions its mutation routes would
// need land together with those routes. The invoke path is the manual
// trigger's CR-first contract with a Function trigger value: the operator
// materializes the ledger row, so nothing here writes the ledger.

type listFunctionsUseCase interface {
	List(ctx context.Context, in FunctionListInput) ([]FunctionItem, error)
}

type getFunctionUseCase interface {
	Get(ctx context.Context, in FunctionGetInput) (FunctionItem, error)
}

// invokeFunctionUseCase is the core invoke use case; the handler maps the
// route's request and the use case's result onto the API's response shape.
type invokeFunctionUseCase interface {
	Invoke(ctx context.Context, in functioninvoke.Input) (functioninvoke.Result, error)
}

type listFunctionRunsUseCase interface {
	List(ctx context.Context, in FunctionRunListInput) ([]FunctionRunItem, error)
}

type getFunctionRunUseCase interface {
	Get(ctx context.Context, in FunctionRunGetInput) (FunctionRunItem, error)
}

type functionListResponse struct {
	Items []FunctionItem `json:"items"`
}

type functionRunListResponse struct {
	Items []FunctionRunItem `json:"items"`
}

// FunctionInvokeResult mirrors the manual trigger's contract: a reference to
// the custom resource, not a ledger row. Phase is whatever the resource
// currently reports, empty until the operator has observed it once -- and an
// expired wait reports the last observed phase rather than pretending the run
// finished.
type FunctionInvokeResult struct {
	Namespace     string `json:"namespace"`
	Name          string `json:"name"`
	OccurrenceKey string `json:"occurrence_key"`
	Trigger       string `json:"trigger"`
	Phase         string `json:"phase"`
	Created       bool   `json:"created"`
}

func (h *Handler) SetListFunctionsUseCase(uc listFunctionsUseCase)   { h.listFunctionsUC = uc }
func (h *Handler) SetGetFunctionUseCase(uc getFunctionUseCase)       { h.getFunctionUC = uc }
func (h *Handler) SetInvokeFunctionUseCase(uc invokeFunctionUseCase) { h.invokeFunctionUC = uc }
func (h *Handler) SetListFunctionRunsUseCase(uc listFunctionRunsUseCase) {
	h.listFunctionRunsUC = uc
}
func (h *Handler) SetGetFunctionRunUseCase(uc getFunctionRunUseCase) { h.getFunctionRunUC = uc }

// ListFunctions lists the caller's tenant's function definitions, narrowed to
// the caller's group when the key is scoped.
func (h *Handler) ListFunctions(c *gin.Context) {
	var req ListFunctionsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID, ok := requireTenantID(c, req.TenantID)
	if !ok {
		return
	}
	out, err := h.listFunctionsUC.List(c.Request.Context(), FunctionListInput{
		TenantID:        tenantID,
		ResourceGroupID: groupFrom(c),
		Limit:           req.Limit,
		Offset:          req.Offset,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, functionListResponse{Items: out})
}

// GetFunction reads one function definition by id.
func (h *Handler) GetFunction(c *gin.Context) {
	var req GetFunctionRequest
	if err := c.ShouldBindUri(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}
	if err := c.ShouldBindQuery(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID, ok := requireTenantID(c, req.TenantID)
	if !ok {
		return
	}
	out, err := h.getFunctionUC.Get(c.Request.Context(), FunctionGetInput{
		ID:              req.ID,
		TenantID:        tenantID,
		ResourceGroupID: groupFrom(c),
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, out)
}

// InvokeFunction handles function invocation requests.
//
// An invocation publishes a JobRun custom resource, not a ledger row -- the
// operator owns the run table and writes the row in its own transaction, so
// the run's actor is the authenticated key and the API never gains INSERT on
// the ledger. The request carries no body on purpose: v1 functions are
// parameterless, and parameters live in the definition, whose change is a new
// revision.
func (h *Handler) InvokeFunction(c *gin.Context) {
	var req InvokeFunctionRequest
	if err := c.ShouldBindUri(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}
	if err := c.ShouldBindQuery(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID, ok := requireTenantID(c, "")
	if !ok {
		return
	}

	var idempotencyKey string
	if key, err := parseIdempotencyKey(c); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toAPIError(err))
		return
	} else if key != nil {
		idempotencyKey = *key
	}

	out, err := h.invokeFunctionUC.Invoke(c.Request.Context(), functioninvoke.Input{
		FunctionID:      req.ID,
		TenantID:        tenantID,
		ActorID:         actorID(c),
		IdempotencyKey:  idempotencyKey,
		ResourceGroupID: groupFrom(c),
		WaitSeconds:     req.WaitSeconds,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	resp := FunctionInvokeResult{
		Namespace:     out.Namespace,
		Name:          out.Name,
		OccurrenceKey: out.OccurrenceKey,
		Trigger:       out.Trigger,
		Phase:         out.Phase,
		Created:       out.Created,
	}
	if out.Created {
		c.JSON(stdhttp.StatusCreated, resp)
		return
	}
	c.JSON(stdhttp.StatusOK, resp)
}

// ListFunctionRuns lists a function's terminal invocations from the
// function_runs read model, newest first.
func (h *Handler) ListFunctionRuns(c *gin.Context) {
	var req ListFunctionRunsRequest
	if err := c.ShouldBindUri(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}
	if err := c.ShouldBindQuery(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID, ok := requireTenantID(c, req.TenantID)
	if !ok {
		return
	}
	out, err := h.listFunctionRunsUC.List(c.Request.Context(), FunctionRunListInput{
		FunctionID:      req.ID,
		TenantID:        tenantID,
		ResourceGroupID: groupFrom(c),
		Limit:           req.Limit,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, functionRunListResponse{Items: out})
}

// GetFunctionRun reads one terminal invocation by its deterministic run id.
func (h *Handler) GetFunctionRun(c *gin.Context) {
	var req GetFunctionRunRequest
	if err := c.ShouldBindUri(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID, ok := requireTenantID(c, "")
	if !ok {
		return
	}
	out, err := h.getFunctionRunUC.Get(c.Request.Context(), FunctionRunGetInput{
		FunctionID:      req.ID,
		RunID:           req.RunID,
		TenantID:        tenantID,
		ResourceGroupID: groupFrom(c),
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, out)
}
