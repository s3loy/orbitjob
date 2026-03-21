package httpapi

import (
	"context"
	"net/http"

	"orbitjob/internal/job"

	"github.com/gin-gonic/gin"
)

// createJobUseCase defines the application capability required by the HTTP handler.
type createJobUseCase interface {
	Create(ctx context.Context, in job.CreateJobInput) (job.Job, error)
}

// Handler wires HTTP endpoints to application use cases
type Handler struct {
	createJobUC createJobUseCase
}

func NewHandler(createJobUC createJobUseCase) *Handler {
	return &Handler{
		createJobUC: createJobUC,
	}
}

// Register mounts HTTP routes for the admin API.
func (h *Handler) Register(r gin.IRouter) {
	r.POST("/api/v1/jobs", h.CreateJob)
}

// CreateJob handles job creation requests
func (h *Handler) CreateJob(c *gin.Context) {
	var req CreateJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	out, err := h.createJobUC.Create(c.Request.Context(), req.ToCreateJobInput())

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}
	c.JSON(http.StatusCreated, out)
}
