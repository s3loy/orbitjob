package httpapi

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"orbitjob/internal/job"
)

// createJobUseCase defines the application capability required by the HTTP handler.
type createJobUseCase interface {
	Create(ctx context.Context, in job.CreateJobInput) (job.Job, error)
}

type errorResponse struct {
	Error string `json:"error"`
}

// Handler wires HTTP endpoints to application use cases.
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
	v1 := r.Group("/api/v1")
	v1.POST("/jobs", h.CreateJob)
}

// CreateJob handles job creation requests.
func (h *Handler) CreateJob(c *gin.Context) {
	var req CreateJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse{
			Error: err.Error(),
		})
		return
	}

	out, err := h.createJobUC.Create(c.Request.Context(), req.ToCreateJobInput())
	if err != nil {
		if job.IsValidationError(err) {
			c.JSON(http.StatusBadRequest, errorResponse{
				Error: err.Error(),
			})
			return
		}

		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, errorResponse{
			Error: "internal server error",
		})
		return
	}

	c.JSON(http.StatusCreated, out)
}
