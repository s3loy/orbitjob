package http

import (
	"context"
	stdhttp "net/http"
	"strings"

	"github.com/gin-gonic/gin"

	checkcommand "orbitjob/internal/admin/app/check/command"
	checkquery "orbitjob/internal/admin/app/check/query"
	checkrunquery "orbitjob/internal/admin/app/checkrun/query"
	instancecommand "orbitjob/internal/admin/app/instance/command"
	instancequery "orbitjob/internal/admin/app/instance/query"
	command "orbitjob/internal/admin/app/job/command"
	query "orbitjob/internal/admin/app/job/query"
	"orbitjob/internal/admin/http/middleware"
	domaincheck "orbitjob/internal/core/domain/check"
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

type createCheckUseCase interface {
	Create(ctx context.Context, in checkcommand.CreateInput) (checkcommand.CreateResult, error)
}

type listChecksUseCase interface {
	List(ctx context.Context, in checkquery.ListChecksInput) (checkquery.ListChecksResult, error)
}

type getCheckUseCase interface {
	Get(ctx context.Context, tenantID string, id int64) (checkquery.GetResult, error)
}

type pauseCheckUseCase interface {
	Pause(ctx context.Context, in checkcommand.ChangeStatusInput) (checkcommand.ChangeStatusResult, error)
}

type resumeCheckUseCase interface {
	Resume(ctx context.Context, in checkcommand.ChangeStatusInput) (checkcommand.ChangeStatusResult, error)
}

type deleteCheckUseCase interface {
	Delete(ctx context.Context, in checkcommand.DeleteInput) error
}

type listCheckRunsUseCase interface {
	List(ctx context.Context, in checkrunquery.ListCheckRunsInput) (checkrunquery.ListCheckRunsResult, error)
}

type getCheckRunUseCase interface {
	Get(ctx context.Context, tenantID string, id int64) (checkrunquery.GetResult, error)
}

type checkListResponse struct {
	Items []checkquery.ListItem `json:"items"`
}

type checkRunListResponse struct {
	Items []checkrunquery.ListItem `json:"items"`
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
	cancelInstanceUC    cancelInstanceUseCase
	createCheckUC       createCheckUseCase
	listChecksUC        listChecksUseCase
	getCheckUC          getCheckUseCase
	pauseCheckUC        pauseCheckUseCase
	resumeCheckUC       resumeCheckUseCase
	deleteCheckUC       deleteCheckUseCase
	listCheckRunsUC     listCheckRunsUseCase
	getCheckRunUC       getCheckRunUseCase
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

func (h *Handler) SetDeleteJobUseCase(uc deleteJobUseCase)           { h.deleteJobUC = uc }
func (h *Handler) SetTriggerJobUseCase(uc triggerJobUseCase)         { h.triggerJobUC = uc }
func (h *Handler) SetListInstancesUseCase(uc listInstancesUseCase)   { h.listInstancesUC = uc }
func (h *Handler) SetGetInstanceUseCase(uc getInstanceUseCase)       { h.getInstanceUC = uc }
func (h *Handler) SetCancelInstanceUseCase(uc cancelInstanceUseCase) { h.cancelInstanceUC = uc }
func (h *Handler) SetCreateCheckUseCase(uc createCheckUseCase)       { h.createCheckUC = uc }
func (h *Handler) SetListChecksUseCase(uc listChecksUseCase)         { h.listChecksUC = uc }
func (h *Handler) SetGetCheckUseCase(uc getCheckUseCase)             { h.getCheckUC = uc }
func (h *Handler) SetPauseCheckUseCase(uc pauseCheckUseCase)         { h.pauseCheckUC = uc }
func (h *Handler) SetResumeCheckUseCase(uc resumeCheckUseCase)       { h.resumeCheckUC = uc }
func (h *Handler) SetDeleteCheckUseCase(uc deleteCheckUseCase)       { h.deleteCheckUC = uc }
func (h *Handler) SetListCheckRunsUseCase(uc listCheckRunsUseCase)   { h.listCheckRunsUC = uc }
func (h *Handler) SetGetCheckRunUseCase(uc getCheckRunUseCase)       { h.getCheckRunUC = uc }

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
const maxIdempotencyKeyLen = 128

func parseIdempotencyKey(c *gin.Context) (*string, error) {
	key := strings.TrimSpace(c.GetHeader(idempotencyKeyHeader))
	if key == "" {
		return nil, nil
	}
	if len(key) > maxIdempotencyKeyLen {
		return nil, validation.New("idempotency_key", "must be <= 128 characters")
	}
	return &key, nil
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
	if key, err := parseIdempotencyKey(c); err != nil {
		writeAPIError(c, stdhttp.StatusBadRequest, toAPIError(err))
		return
	} else if key != nil {
		reqCtx = middleware.WithIdempotencyKey(reqCtx, *key)
	}

	out, err := h.triggerJobUC.Trigger(reqCtx, in)
	if err != nil {
		apiErr := toAPIError(err)
		switch apiErr.Code {
		case ErrCodeValidation:
			writeAPIError(c, stdhttp.StatusBadRequest, apiErr)
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

// CreateCheck handles check creation requests.
func (h *Handler) CreateCheck(c *gin.Context) {
	var req CreateCheckRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAPIError(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	in := req.ToCreateInput()
	in.TenantID = middleware.GetTenantID(c)
	out, err := h.createCheckUC.Create(c.Request.Context(), in)
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

// ListChecks handles check list queries.
func (h *Handler) ListChecks(c *gin.Context) {
	var req ListChecksRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		writeAPIError(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	in := req.ToListInput()
	in.TenantID = middleware.GetTenantID(c)
	out, err := h.listChecksUC.List(c.Request.Context(), in)
	if err != nil {
		if validation.Is(err) {
			writeAPIError(c, stdhttp.StatusBadRequest, toAPIError(err))
			return
		}
		_ = c.Error(err)
		writeAPIError(c, stdhttp.StatusInternalServerError, toAPIError(err))
		return
	}

	c.JSON(stdhttp.StatusOK, checkListResponse{Items: out.Items})
}

// GetCheck handles one check detail query.
func (h *Handler) GetCheck(c *gin.Context) {
	var req GetCheckRequest
	if err := c.ShouldBindUri(&req); err != nil {
		writeAPIError(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}
	if err := c.ShouldBindQuery(&req); err != nil {
		writeAPIError(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	out, err := h.getCheckUC.Get(c.Request.Context(), middleware.GetTenantID(c), req.ID)
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

// PauseCheck handles check pause requests.
func (h *Handler) PauseCheck(c *gin.Context) {
	h.changeCheckStatus(c, domaincheck.ActionPause)
}

// ResumeCheck handles check resume requests.
func (h *Handler) ResumeCheck(c *gin.Context) {
	h.changeCheckStatus(c, domaincheck.ActionResume)
}

func (h *Handler) changeCheckStatus(c *gin.Context, action string) {
	var pathReq checkIDURI
	if err := c.ShouldBindUri(&pathReq); err != nil {
		writeAPIError(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	req := ChangeCheckStatusRequest{
		ID:       pathReq.ID,
		TenantID: middleware.GetTenantID(c),
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAPIError(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	var out checkcommand.ChangeStatusResult
	var err error
	switch action {
	case domaincheck.ActionPause:
		out, err = h.pauseCheckUC.Pause(c.Request.Context(), req.ToChangeStatusInput())
	case domaincheck.ActionResume:
		out, err = h.resumeCheckUC.Resume(c.Request.Context(), req.ToChangeStatusInput())
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

// DeleteCheck handles soft-delete requests for checks.
func (h *Handler) DeleteCheck(c *gin.Context) {
	var pathReq checkIDURI
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

	err := h.deleteCheckUC.Delete(c.Request.Context(), checkcommand.DeleteInput{
		ID:       pathReq.ID,
		TenantID: middleware.GetTenantID(c),
		Version:  body.Version,
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

	c.JSON(stdhttp.StatusOK, gin.H{"deleted": true})
}

// ListCheckRuns handles check run list queries.
func (h *Handler) ListCheckRuns(c *gin.Context) {
	var req ListCheckRunsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		writeAPIError(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	in := req.ToListInput()
	in.TenantID = middleware.GetTenantID(c)
	out, err := h.listCheckRunsUC.List(c.Request.Context(), in)
	if err != nil {
		if validation.Is(err) {
			writeAPIError(c, stdhttp.StatusBadRequest, toAPIError(err))
			return
		}
		_ = c.Error(err)
		writeAPIError(c, stdhttp.StatusInternalServerError, toAPIError(err))
		return
	}

	c.JSON(stdhttp.StatusOK, checkRunListResponse{Items: out.Items})
}

// GetCheckRun handles one check run detail query.
func (h *Handler) GetCheckRun(c *gin.Context) {
	var req GetCheckRunRequest
	if err := c.ShouldBindUri(&req); err != nil {
		writeAPIError(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	out, err := h.getCheckRunUC.Get(c.Request.Context(), middleware.GetTenantID(c), req.ID)
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
