package http

import (
	"context"
	stdhttp "net/http"
	"strings"

	"github.com/gin-gonic/gin"

	command "orbitjob/internal/admin/app/job/command"
	instancecommand "orbitjob/internal/admin/app/instance/command"
	instancequery "orbitjob/internal/admin/app/instance/query"
	query "orbitjob/internal/admin/app/job/query"
	"orbitjob/internal/admin/http/middleware"
	domaininstance "orbitjob/internal/core/domain/instance"
	"orbitjob/internal/domain/resource"
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

type deleteJobUseCase interface {
	Delete(ctx context.Context, in command.DeleteInput) (command.DeleteResult, error)
}

type triggerJobUseCase interface {
	Trigger(ctx context.Context, in command.TriggerInput) (command.TriggerResult, error)
}

type listInstancesUseCase interface {
	List(ctx context.Context, in instancequery.ListInstancesInput) ([]instancequery.InstanceItem, error)
}

type getInstanceUseCase interface {
	Get(ctx context.Context, runID string) (*instancequery.InstanceItem, error)
}

type cancelInstanceUseCase interface {
	Cancel(ctx context.Context, in instancecommand.CancelInstanceInput) (domaininstance.Snapshot, error)
}

type jobListResponse struct {
	Items []query.ListItem `json:"items"`
}

type instanceListResponse struct {
	Items []instancequery.InstanceItem `json:"items"`
}

type errorResponse struct {
	Error APIError `json:"error"`
}

// Handler wires HTTP endpoints to application use cases.
type Handler struct {
	createJobUC      createJobUseCase
	listJobsUC       listJobsUseCase
	getJobUC         getJobUseCase
	updateJobUC      updateJobUseCase
	statusJobUC      changeJobStatusUseCase
	deleteJobUC      deleteJobUseCase
	triggerJobUC     triggerJobUseCase
	listInstancesUC  listInstancesUseCase
	getInstanceUC    getInstanceUseCase
	cancelInstanceUC cancelInstanceUseCase
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

func (h *Handler) SetDeleteJobUseCase(uc deleteJobUseCase)        { h.deleteJobUC = uc }
func (h *Handler) SetTriggerJobUseCase(uc triggerJobUseCase)       { h.triggerJobUC = uc }
func (h *Handler) SetListInstancesUseCase(uc listInstancesUseCase) { h.listInstancesUC = uc }
func (h *Handler) SetGetInstanceUseCase(uc getInstanceUseCase)     { h.getInstanceUC = uc }
func (h *Handler) SetCancelInstanceUseCase(uc cancelInstanceUseCase) { h.cancelInstanceUC = uc }

// Register mounts HTTP routes for the admin API.
func (h *Handler) Register(r gin.IRouter) {
	v1 := r.Group(adminAPIPrefix)
	for _, route := range adminAPIRoutes() {
		if route.enabled != nil && !route.enabled(h) {
			continue
		}
		route.register(v1, h)
	}
}

// CreateJob handles job creation requests.
func (h *Handler) CreateJob(c *gin.Context) {
	var req CreateJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAPIError(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	in := req.ToCreateInput()
	in.TenantID = middleware.GetTenantID(c)
	out, err := h.createJobUC.Create(c.Request.Context(), in)
	if err != nil {
		if validation.Is(err) {
			writeAPIError(c, stdhttp.StatusBadRequest, toAPIError(err))
			return
		}

		_ = c.Error(err)
		writeAPIError(c, stdhttp.StatusInternalServerError, toAPIError(err))
		return
	}

	c.JSON(stdhttp.StatusCreated, out)
}

// ListJobs handles job list queries.
func (h *Handler) ListJobs(c *gin.Context) {
	var req ListJobsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		writeAPIError(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	in := req.ToListInput()
	in.TenantID = middleware.GetTenantID(c)
	out, err := h.listJobsUC.List(c.Request.Context(), in)
	if err != nil {
		if validation.Is(err) {
			writeAPIError(c, stdhttp.StatusBadRequest, toAPIError(err))
			return
		}

		_ = c.Error(err)
		writeAPIError(c, stdhttp.StatusInternalServerError, toAPIError(err))
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
		writeAPIError(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}
	if err := c.ShouldBindQuery(&req); err != nil {
		writeAPIError(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	in := req.ToGetInput()
	in.TenantID = middleware.GetTenantID(c)
	out, err := h.getJobUC.Get(c.Request.Context(), in)
	if err != nil {
		if validation.Is(err) {
			writeAPIError(c, stdhttp.StatusBadRequest, toAPIError(err))
			return
		}
		apiErr := toAPIError(err)
		if apiErr.Code == ErrCodeNotFound {
			writeAPIError(c, stdhttp.StatusNotFound, apiErr)
			return
		}

		_ = c.Error(err)
		writeAPIError(c, stdhttp.StatusInternalServerError, apiErr)
		return
	}

	c.JSON(stdhttp.StatusOK, out)
}

// UpdateJob handles mutable job updates.
func (h *Handler) UpdateJob(c *gin.Context) {
	var pathReq jobIDURI
	if err := c.ShouldBindUri(&pathReq); err != nil {
		writeAPIError(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	req := UpdateJobRequest{
		ID: pathReq.ID,
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAPIError(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID := middleware.GetTenantID(c)
	actorID, err := requiredActorID(c)
	if err != nil {
		writeAPIError(c, stdhttp.StatusBadRequest, toAPIError(err))
		return
	}

	current, err := h.getJobUC.Get(c.Request.Context(), query.GetInput{
		ID:       req.ID,
		TenantID: tenantID,
	})
	if err != nil {
		if validation.Is(err) {
			writeAPIError(c, stdhttp.StatusBadRequest, toAPIError(err))
			return
		}

		apiErr := toAPIError(err)
		if apiErr.Code == ErrCodeNotFound {
			writeAPIError(c, stdhttp.StatusNotFound, apiErr)
			return
		}

		_ = c.Error(err)
		writeAPIError(c, stdhttp.StatusInternalServerError, apiErr)
		return
	}

	req.TenantID = tenantID
	out, err := h.updateJobUC.Update(c.Request.Context(), req.ToUpdateInput(current, actorID))
	if err != nil {
		if validation.Is(err) {
			writeAPIError(c, stdhttp.StatusBadRequest, toAPIError(err))
			return
		}

		apiErr := toAPIError(err)
		switch apiErr.Code {
		case ErrCodeNotFound:
			writeAPIError(c, stdhttp.StatusNotFound, apiErr)
			return
		case ErrCodeConflict:
			writeAPIError(c, stdhttp.StatusConflict, apiErr)
			return
		default:
			_ = c.Error(err)
			writeAPIError(c, stdhttp.StatusInternalServerError, apiErr)
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
	var pathReq jobIDURI
	if err := c.ShouldBindUri(&pathReq); err != nil {
		writeAPIError(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	req := ChangeStatusRequest{
		ID:       pathReq.ID,
		TenantID: middleware.GetTenantID(c),
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAPIError(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	actorID, err := requiredActorID(c)
	if err != nil {
		writeAPIError(c, stdhttp.StatusBadRequest, toAPIError(err))
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
		writeAPIError(c, stdhttp.StatusInternalServerError, toAPIError(validation.New("action", "unsupported status action")))
		return
	}
	if err != nil {
		if validation.Is(err) {
			writeAPIError(c, stdhttp.StatusBadRequest, toAPIError(err))
			return
		}

		apiErr := toAPIError(err)
		switch apiErr.Code {
		case ErrCodeNotFound:
			writeAPIError(c, stdhttp.StatusNotFound, apiErr)
			return
		case ErrCodeConflict:
			writeAPIError(c, stdhttp.StatusConflict, apiErr)
			return
		default:
			_ = c.Error(err)
			writeAPIError(c, stdhttp.StatusInternalServerError, apiErr)
			return
		}
	}

	c.JSON(stdhttp.StatusOK, out)
}

const idempotencyKeyHeader = "X-OrbitJob-Idempotency-Key"

func parseIdempotencyKey(c *gin.Context) *string {
	key := strings.TrimSpace(c.GetHeader(idempotencyKeyHeader))
	if key == "" {
		return nil
	}
	return &key
}

// TriggerJob handles manual trigger requests.
func (h *Handler) TriggerJob(c *gin.Context) {
	var pathReq jobIDURI
	if err := c.ShouldBindUri(&pathReq); err != nil {
		writeAPIError(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID := middleware.GetTenantID(c)
	in := command.TriggerInput{
		JobID:    pathReq.ID,
		TenantID: tenantID,
	}

	reqCtx := c.Request.Context()
	if key := parseIdempotencyKey(c); key != nil {
		reqCtx = middleware.WithIdempotencyKey(reqCtx, *key)
	}

	out, err := h.triggerJobUC.Trigger(reqCtx, in)
	if err != nil {
		apiErr := toAPIError(err)
		if apiErr.Code == ErrCodeValidation {
			writeAPIError(c, stdhttp.StatusBadRequest, apiErr)
			return
		}
		_ = c.Error(err)
		writeAPIError(c, stdhttp.StatusInternalServerError, apiErr)
		return
	}

	c.JSON(stdhttp.StatusCreated, out)
}

// DeleteJob handles soft-delete requests.
func (h *Handler) DeleteJob(c *gin.Context) {
	var pathReq jobIDURI
	if err := c.ShouldBindUri(&pathReq); err != nil {
		writeAPIError(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID := middleware.GetTenantID(c)
	out, err := h.deleteJobUC.Delete(c.Request.Context(), command.DeleteInput{
		ID:       pathReq.ID,
		TenantID: tenantID,
	})
	if err != nil {
		apiErr := toAPIError(err)
		if apiErr.Code == ErrCodeNotFound {
			writeAPIError(c, stdhttp.StatusNotFound, apiErr)
			return
		}
		_ = c.Error(err)
		writeAPIError(c, stdhttp.StatusInternalServerError, apiErr)
		return
	}

	c.JSON(stdhttp.StatusOK, out)
}

// ListInstances handles instance listing queries.
func (h *Handler) ListInstances(c *gin.Context) {
	var req ListInstancesRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		writeAPIError(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID := middleware.GetTenantID(c)
	out, err := h.listInstancesUC.List(c.Request.Context(), instancequery.ListInstancesInput{
		TenantID: tenantID,
		Status:   req.Status,
		Limit:    req.Limit,
		Offset:   req.Offset,
	})
	if err != nil {
		_ = c.Error(err)
		writeAPIError(c, stdhttp.StatusInternalServerError, toAPIError(err))
		return
	}

	c.JSON(stdhttp.StatusOK, instanceListResponse{Items: out})
}

// GetInstance handles instance detail query by run_id.
func (h *Handler) GetInstance(c *gin.Context) {
	var pathReq instanceRunIDURI
	if err := c.ShouldBindUri(&pathReq); err != nil {
		writeAPIError(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	out, err := h.getInstanceUC.Get(c.Request.Context(), pathReq.RunID)
	if err != nil {
		if _, ok := err.(*resource.NotFoundError); ok {
			writeAPIError(c, stdhttp.StatusNotFound, toAPIError(err))
			return
		}
		_ = c.Error(err)
		writeAPIError(c, stdhttp.StatusInternalServerError, toAPIError(err))
		return
	}

	c.JSON(stdhttp.StatusOK, out)
}

// CancelInstance handles instance cancellation requests.
func (h *Handler) CancelInstance(c *gin.Context) {
	var pathReq instanceRunIDURI
	if err := c.ShouldBindUri(&pathReq); err != nil {
		writeAPIError(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	var body struct {
		Version int `json:"version" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		writeAPIError(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	out, err := h.cancelInstanceUC.Cancel(c.Request.Context(), instancecommand.CancelInstanceInput{
		RunID:   pathReq.RunID,
		Version: body.Version,
	})
	if err != nil {
		apiErr := toAPIError(err)
		switch apiErr.Code {
		case ErrCodeNotFound:
			writeAPIError(c, stdhttp.StatusNotFound, apiErr)
		case ErrCodeConflict:
			writeAPIError(c, stdhttp.StatusConflict, apiErr)
		default:
			_ = c.Error(err)
			writeAPIError(c, stdhttp.StatusInternalServerError, apiErr)
		}
		return
	}

	c.JSON(stdhttp.StatusOK, out)
}

func writeAPIError(c *gin.Context, statusCode int, apiErr APIError) {
	c.JSON(statusCode, errorResponse{
		Error: apiErr,
	})
}
