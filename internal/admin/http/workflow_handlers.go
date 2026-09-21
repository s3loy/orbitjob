package http

import (
	"context"
	stdhttp "net/http"

	"github.com/gin-gonic/gin"

	"orbitjob/internal/admin/http/apperror"
)

// The workflow routes mirror the jobs routes one resource kind up: a
// WorkflowJob definition is declared as a Custom Resource and read here
// through its projection, and the one mutation is a manual trigger that
// publishes a WorkflowRun custom resource the operator materializes. Cancels
// patch cancelRequested on the same kind of resource and the operator fans
// them out.

type listWorkflowsUseCase interface {
	List(ctx context.Context, in WorkflowListInput) ([]WorkflowListItem, error)
}

type getWorkflowUseCase interface {
	Get(ctx context.Context, in WorkflowGetInput) (WorkflowGetItem, error)
}

// triggerWorkflowUseCase publishes a WorkflowRun custom resource for a manual
// workflow trigger.
type triggerWorkflowUseCase interface {
	Trigger(ctx context.Context, in WorkflowTriggerInput) (WorkflowTriggerResult, error)
}

type listWorkflowRunsUseCase interface {
	List(ctx context.Context, in WorkflowRunListInput) ([]WorkflowRunItem, error)
}

type getWorkflowRunUseCase interface {
	Get(ctx context.Context, in WorkflowRunGetInput) (WorkflowRunGetItem, error)
}

// cancelWorkflowRunUseCase patches a stop intent onto a workflow run's
// WorkflowRun custom resource; the operator fans it out over the workflow's
// non-terminal step runs.
type cancelWorkflowRunUseCase interface {
	Cancel(ctx context.Context, in WorkflowCancelInput) (WorkflowCancelResult, error)
}

type workflowListResponse struct {
	Items []WorkflowListItem `json:"items"`
}

type workflowRunListResponse struct {
	Items []WorkflowRunItem `json:"items"`
}

func (h *Handler) SetListWorkflowsUseCase(uc listWorkflowsUseCase)     { h.listWorkflowsUC = uc }
func (h *Handler) SetGetWorkflowUseCase(uc getWorkflowUseCase)         { h.getWorkflowUC = uc }
func (h *Handler) SetTriggerWorkflowUseCase(uc triggerWorkflowUseCase) { h.triggerWorkflowUC = uc }
func (h *Handler) SetListWorkflowRunsUseCase(uc listWorkflowRunsUseCase) {
	h.listWorkflowRunsUC = uc
}
func (h *Handler) SetGetWorkflowRunUseCase(uc getWorkflowRunUseCase) { h.getWorkflowRunUC = uc }
func (h *Handler) SetCancelWorkflowRunUseCase(uc cancelWorkflowRunUseCase) {
	h.cancelWorkflowRunUC = uc
}

// ListWorkflows lists the active revision of every workflow definition in the
// caller's tenant.
func (h *Handler) ListWorkflows(c *gin.Context) {
	var req ListWorkflowsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID, ok := requireTenantID(c, req.TenantID)
	if !ok {
		return
	}
	out, err := h.listWorkflowsUC.List(c.Request.Context(), WorkflowListInput{
		TenantID:        tenantID,
		ResourceGroupID: groupFrom(c),
		Limit:           req.Limit,
		Offset:          req.Offset,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, workflowListResponse{Items: out})
}

// GetWorkflow reads one workflow definition's active revision by revision id.
func (h *Handler) GetWorkflow(c *gin.Context) {
	var req GetWorkflowRequest
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
	out, err := h.getWorkflowUC.Get(c.Request.Context(), WorkflowGetInput{
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

// TriggerWorkflow handles manual workflow trigger requests.
//
// A manual trigger publishes a WorkflowRun custom resource; the operator
// writes the workflow run row row-first and fans the DAG out from it. The
// actor is the authenticated key -- "who triggered it" has to name the
// credential that actually called.
func (h *Handler) TriggerWorkflow(c *gin.Context) {
	var pathReq jobIDURI
	if err := c.ShouldBindUri(&pathReq); err != nil {
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

	out, err := h.triggerWorkflowUC.Trigger(c.Request.Context(), WorkflowTriggerInput{
		WorkflowID:      pathReq.ID,
		TenantID:        tenantID,
		ActorID:         actorID(c),
		IdempotencyKey:  idempotencyKey,
		ResourceGroupID: groupFrom(c),
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	if out.Created {
		c.JSON(stdhttp.StatusCreated, out)
		return
	}
	c.JSON(stdhttp.StatusOK, out)
}

// ListWorkflowRuns lists one workflow's runs from the workflow ledger, newest
// first.
func (h *Handler) ListWorkflowRuns(c *gin.Context) {
	var req ListWorkflowRunsRequest
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
	out, err := h.listWorkflowRunsUC.List(c.Request.Context(), WorkflowRunListInput{
		WorkflowID:      req.ID,
		TenantID:        tenantID,
		ResourceGroupID: groupFrom(c),
		Limit:           req.Limit,
		Offset:          req.Offset,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, workflowRunListResponse{Items: out})
}

// GetWorkflowRun reads one workflow run together with its step runs.
func (h *Handler) GetWorkflowRun(c *gin.Context) {
	var pathReq workflowRunIDURI
	if err := c.ShouldBindUri(&pathReq); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID, ok := requireTenantID(c, "")
	if !ok {
		return
	}
	out, err := h.getWorkflowRunUC.Get(c.Request.Context(), WorkflowRunGetInput{
		WorkflowID:      pathReq.ID,
		RunID:           pathReq.RunID,
		TenantID:        tenantID,
		ResourceGroupID: groupFrom(c),
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, out)
}

// CancelWorkflowRun requests a stop for one workflow run. The request carries
// no body on purpose, mirroring the run cancel route: the effect travels
// through the WorkflowRun custom resource and the operator records the ledger
// transitions, so an actor the caller typed would not be evidence of anything.
func (h *Handler) CancelWorkflowRun(c *gin.Context) {
	var pathReq workflowRunIDURI
	if err := c.ShouldBindUri(&pathReq); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID, ok := requireTenantID(c, "")
	if !ok {
		return
	}

	out, err := h.cancelWorkflowRunUC.Cancel(c.Request.Context(), WorkflowCancelInput{
		WorkflowID:      pathReq.ID,
		RunID:           pathReq.RunID,
		TenantID:        tenantID,
		ResourceGroupID: groupFrom(c),
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, out)
}
