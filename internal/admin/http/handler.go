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

type updateJobUseCase interface {
	Update(ctx context.Context, in command.UpdateInput) (command.UpdateResult, error)
}

type changeJobStatusUseCase interface {
	Pause(ctx context.Context, in command.ChangeStatusInput) (command.ChangeStatusResult, error)
	Resume(ctx context.Context, in command.ChangeStatusInput) (command.ChangeStatusResult, error)
}

type jobListResponse struct {
	Items []query.ListItem `json:"items"`
}

// Handler wires HTTP endpoints to application use cases.
type Handler struct {
	createJobUC createJobUseCase
	listJobsUC  listJobsUseCase
	getJobUC    getJobUseCase
	updateJobUC updateJobUseCase
	statusJobUC changeJobStatusUseCase
}

func NewHandler(
	createJobUC createJobUseCase,
	listJobsUC listJobsUseCase,
	getJobUC getJobUseCase,
	updateJobUC updateJobUseCase,
	statusJobUC changeJobStatusUseCase,
) *Handler {
	return &Handler{
		createJobUC: createJobUC,
		listJobsUC:  listJobsUC,
		getJobUC:    getJobUC,
		updateJobUC: updateJobUC,
		statusJobUC: statusJobUC,
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
	if h.updateJobUC != nil && h.getJobUC != nil {
		v1.PUT("/jobs/:id", h.UpdateJob)
	}
	if h.statusJobUC != nil {
		v1.POST("/jobs/:id/pause", h.PauseJob)
		v1.POST("/jobs/:id/resume", h.ResumeJob)
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

// UpdateJob handles mutable job updates.
func (h *Handler) UpdateJob(c *gin.Context) {
	var pathReq struct {
		ID int64 `uri:"id" binding:"required,min=1"`
	}
	if err := c.ShouldBindUri(&pathReq); err != nil {
		c.JSON(stdhttp.StatusBadRequest, gin.H{"error": toBindAPIError(err)})
		return
	}
	req := UpdateJobRequest{
		ID:       pathReq.ID,
		TenantID: c.Query("tenant_id"),
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(stdhttp.StatusBadRequest, gin.H{"error": toBindAPIError(err)})
		return
	}

	actorID, err := requiredActorID(c)
	if err != nil {
		c.JSON(stdhttp.StatusBadRequest, gin.H{"error": toAPIError(err)})
		return
	}

	current, err := h.getJobUC.Get(c.Request.Context(), query.GetInput{
		ID:       req.ID,
		TenantID: req.TenantID,
	})
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

	out, err := h.updateJobUC.Update(c.Request.Context(), req.ToUpdateInput(current, actorID))
	if err != nil {
		if validation.Is(err) {
			c.JSON(stdhttp.StatusBadRequest, gin.H{"error": toAPIError(err)})
			return
		}

		apiErr := toAPIError(err)
		switch apiErr.Code {
		case ErrCodeNotFound:
			c.JSON(stdhttp.StatusNotFound, gin.H{"error": apiErr})
			return
		case ErrCodeConflict:
			c.JSON(stdhttp.StatusConflict, gin.H{"error": apiErr})
			return
		default:
			_ = c.Error(err)
			c.JSON(stdhttp.StatusInternalServerError, gin.H{"error": apiErr})
			return
		}
	}

	c.JSON(stdhttp.StatusOK, out)
}

// PauseJob handles job pause requests.
func (h *Handler) PauseJob(c *gin.Context) {
	h.changeJobStatus(c, domainPause)
}

// ResumeJob handles job resume requests.
func (h *Handler) ResumeJob(c *gin.Context) {
	h.changeJobStatus(c, domainResume)
}

const (
	domainPause  = "pause"
	domainResume = "resume"
)

func (h *Handler) changeJobStatus(c *gin.Context, action string) {
	var pathReq struct {
		ID int64 `uri:"id" binding:"required,min=1"`
	}
	if err := c.ShouldBindUri(&pathReq); err != nil {
		c.JSON(stdhttp.StatusBadRequest, gin.H{"error": toBindAPIError(err)})
		return
	}

	req := ChangeStatusRequest{
		ID:       pathReq.ID,
		TenantID: c.Query("tenant_id"),
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(stdhttp.StatusBadRequest, gin.H{"error": toBindAPIError(err)})
		return
	}

	actorID, err := requiredActorID(c)
	if err != nil {
		c.JSON(stdhttp.StatusBadRequest, gin.H{"error": toAPIError(err)})
		return
	}

	var (
		out command.ChangeStatusResult
	)
	switch action {
	case domainPause:
		out, err = h.statusJobUC.Pause(c.Request.Context(), req.ToChangeStatusInput(actorID))
	case domainResume:
		out, err = h.statusJobUC.Resume(c.Request.Context(), req.ToChangeStatusInput(actorID))
	default:
		c.JSON(stdhttp.StatusInternalServerError, gin.H{"error": toAPIError(validation.New("action", "unsupported status action"))})
		return
	}
	if err != nil {
		if validation.Is(err) {
			c.JSON(stdhttp.StatusBadRequest, gin.H{"error": toAPIError(err)})
			return
		}

		apiErr := toAPIError(err)
		switch apiErr.Code {
		case ErrCodeNotFound:
			c.JSON(stdhttp.StatusNotFound, gin.H{"error": apiErr})
			return
		case ErrCodeConflict:
			c.JSON(stdhttp.StatusConflict, gin.H{"error": apiErr})
			return
		default:
			_ = c.Error(err)
			c.JSON(stdhttp.StatusInternalServerError, gin.H{"error": apiErr})
			return
		}
	}

	c.JSON(stdhttp.StatusOK, out)
}
