package http

import (
	"context"
	stdhttp "net/http"

	"github.com/gin-gonic/gin"

	command "orbitjob/internal/admin/app/job/command"
	query "orbitjob/internal/admin/app/job/query"
	"orbitjob/internal/domain/validation"
)

// createJobUseCase defines the application capability required by the HTTP handler.
type createJobUseCase interface {
	Create(ctx context.Context, in command.CreateInput) (command.CreateResult, error)
}

type listJobsUseCase interface {
	List(ctx context.Context, in query.ListInput) ([]query.ListItem, error)
}

type getJobUseCase interface {
	Get(ctx context.Context, in query.GetInput) (query.GetItem, error)
}

type jobListResponse struct {
	Items []query.ListItem `json:"items"`
}

// Handler wires HTTP endpoints to application use cases.
type Handler struct {
	createJobUC createJobUseCase
	listJobsUC  listJobsUseCase
	getJobUC    getJobUseCase
}

func NewHandler(
	createJobUC createJobUseCase,
	listJobsUC listJobsUseCase,
	getJobUC getJobUseCase,
) *Handler {
	return &Handler{
		createJobUC: createJobUC,
		listJobsUC:  listJobsUC,
		getJobUC:    getJobUC,
	}
}

// Register mounts HTTP routes for the admin API.
func (h *Handler) Register(r gin.IRouter) {
	v1 := r.Group("/api/v1")

	if h.listJobsUC != nil {
		v1.GET("/jobs", h.ListJobs)
	}
	if h.getJobUC != nil {
		v1.GET("/jobs/:id", h.GetJob)
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
		if validation.Is(err) {
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
		if validation.Is(err) {
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

// GetJob handles one job detail query.
func (h *Handler) GetJob(c *gin.Context) {
	var req GetJobRequest
	if err := c.ShouldBindUri(&req); err != nil {
		c.JSON(stdhttp.StatusBadRequest, gin.H{"error": toBindAPIError(err)})
		return
	}
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(stdhttp.StatusBadRequest, gin.H{"error": toBindAPIError(err)})
		return
	}

	out, err := h.getJobUC.Get(c.Request.Context(), req.ToGetInput())
	if err != nil {
		if validation.Is(err) {
			c.JSON(stdhttp.StatusBadRequest, gin.H{"error": toAPIError(err)})
			return
		}
		apiErr := toAPIError(err)
		if apiErr.Code == ErrCodeNotFound {
			c.JSON(stdhttp.StatusNotFound, gin.H{"error": apiErr})
			return
		}

		_ = c.Error(err)
		c.JSON(stdhttp.StatusInternalServerError, gin.H{"error": apiErr})
		return
	}

	c.JSON(stdhttp.StatusOK, out)
}
