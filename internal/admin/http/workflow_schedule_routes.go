package http

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"orbitjob/internal/admin/http/apperror"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
	"orbitjob/internal/domain/workflowschedule"
)

type WorkflowSchedulePreviewRequest struct {
	Schedule string `json:"schedule" binding:"required,max=256"`
	TimeZone string `json:"time_zone" binding:"max=128"`
	Count    int    `json:"count" binding:"omitempty,min=1,max=20"`
}

type WorkflowSchedulePreviewResult struct {
	TimeZone string      `json:"time_zone"`
	Upcoming []time.Time `json:"upcoming"`
}

func workflowScheduleRoutes() []routeDefinition {
	return []routeDefinition{{
		method: http.MethodPost, path: "/workflows/schedule-preview", permission: "workflow:Apply", resource: tenantScoped("workflow"),
		enabled:  func(h *Handler) bool { return h != nil },
		register: func(r gin.IRouter, h *Handler) { r.POST("/workflows/schedule-preview", h.PreviewWorkflowSchedule) },
		spec: operationDefinition{id: "previewWorkflowSchedule", summary: "Preview upcoming Workflow schedule times", description: "Validates five-field cron and an IANA timezone (default UTC) using the automatic scheduler parser. Returns strictly future UTC instants; DST gaps are skipped and repeated local times are separate instants. Requires ungrouped workflow Apply access. No workload is created.", tags: []string{"Workflows"}, requestBodyModel: WorkflowSchedulePreviewRequest{}, requestBodyRequired: true, responses: []responseDefinition{
			{statusCode: 200, description: "Upcoming UTC instants", model: WorkflowSchedulePreviewResult{}},
			{statusCode: 400, description: "Invalid cron, timezone or count", model: apperror.ErrorResponse{}},
			{statusCode: 403, description: "Denied", model: apperror.ErrorResponse{}},
		}},
	}}
}

func (h *Handler) PreviewWorkflowSchedule(c *gin.Context) {
	if _, ok := requireTenantID(c, ""); !ok {
		return
	}
	if err := resource.RequireUnscoped(groupFrom(c), "workflow schedule"); err != nil {
		writeAPIError(c, err)
		return
	}
	var req WorkflowSchedulePreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apperror.Write(c, http.StatusBadRequest, toBindAPIError(err))
		return
	}
	if req.Count == 0 {
		req.Count = workflowschedule.DefaultPreviewCount
	}
	upcoming, err := workflowschedule.Preview(req.Schedule, req.TimeZone, time.Now().UTC(), req.Count)
	if err != nil {
		writeAPIError(c, validation.New("schedule", err.Error()))
		return
	}
	c.JSON(http.StatusOK, WorkflowSchedulePreviewResult{TimeZone: workflowschedule.Zone(req.TimeZone), Upcoming: upcoming})
}
