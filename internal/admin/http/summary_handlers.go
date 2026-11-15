package http

import (
	"context"
	"fmt"
	stdhttp "net/http"

	"github.com/gin-gonic/gin"

	runquery "orbitjob/internal/admin/app/run/query"
	"orbitjob/internal/admin/http/apperror"
)

type summarizeInstancesUseCase interface {
	Summary(context.Context, runquery.ListInput) (runquery.Summary, error)
}

// SummarizeInstances counts the full matching ledger under the list grant.
func (h *Handler) SummarizeInstances(c *gin.Context) {
	var req SummarizeInstancesRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}
	tenantID, ok := requireTenantID(c, "")
	if !ok {
		return
	}
	uc, ok := h.listInstancesUC.(summarizeInstancesUseCase)
	if !ok {
		writeAPIError(c, fmt.Errorf("run summary use case is required"))
		return
	}
	out, err := uc.Summary(c.Request.Context(), runquery.ListInput{TenantID: tenantID, ResourceGroupID: groupFrom(c), Phase: req.Phase, SourceUID: req.SourceUID, OccurrenceKey: req.OccurrenceKey, CreatedFrom: req.CreatedFrom, CreatedBefore: req.CreatedBefore})
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, out)
}
