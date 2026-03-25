package http

import (
	"context"
	stdhttp "net/http"

	"github.com/gin-gonic/gin"

	command "orbitjob/internal/admin/app/job/command"
	query "orbitjob/internal/admin/app/job/query"
	domainjob "orbitjob/internal/domain/job"
	"orbitjob/internal/job"
)

// createJobUseCase defines the application capability required by the HTTP handler.
type createJobUseCase interface {
	Create(ctx context.Context, in domainjob.CreateInput) (command.CreateResult, error)
}

type listJobsUseCase interface {
	List(ctx context.Context, in query.ListInput) ([]query.ListItem, error)
}

type jobListResponse struct {
	Items []query.ListItem `json:"items"`
}

// Handler wires HTTP endpoints to application use cases.
type Handler struct {
	createJobUC createJobUseCase
	listJobsUC  listJobsUseCase
}

func NewHandler(createJobUC createJobUseCase, listJobsUC listJobsUseCase) *Handler {
	return &Handler{
		createJobUC: createJobUC,
		listJobsUC:  listJobsUC,
	}
}

// Register mounts HTTP routes for the admin API.
func (h *Handler) Register(r gin.IRouter) {
	v1 := r.Group("/api/v1")

	if h.listJobsUC != nil {
		v1.GET("/jobs", h.ListJobs)
	}
	if h.createJobUC != nil {
		v1.POST("/jobs", h.CreateJob)
	}
}

// CreateJob handles job creation requests.
func (h *Handler) CreateJob(c *gin.Context) {
	var req CreateJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(stdhttp.StatusBadRequest, gin.H{"error": toBindAPIError(err)})
		return
	}

	out, err := h.createJobUC.Create(c.Request.Context(), req.ToCreateInput())
	if err != nil {
		if domainjob.IsValidationError(err) {
			c.JSON(stdhttp.StatusBadRequest, gin.H{"error": toAPIError(err)})
			return
		}

		_ = c.Error(err)
		c.JSON(stdhttp.StatusInternalServerError, gin.H{"error": toAPIError(err)})
		return
	}

	c.JSON(stdhttp.StatusCreated, out)
}

// ListJobs handles job list queries.
func (h *Handler) ListJobs(c *gin.Context) {
	var req ListJobsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(stdhttp.StatusBadRequest, gin.H{"error": toBindAPIError(err)})
		return
	}

	out, err := h.listJobsUC.List(c.Request.Context(), req.ToListInput())
	if err != nil {
		if job.IsValidationError(err) {
			c.JSON(stdhttp.StatusBadRequest, gin.H{"error": toAPIError(err)})
			return
		}

		_ = c.Error(err)
		c.JSON(stdhttp.StatusInternalServerError, gin.H{"error": toAPIError(err)})
		return
	}

	c.JSON(stdhttp.StatusOK, jobListResponse{
		Items: out,
	})
}
