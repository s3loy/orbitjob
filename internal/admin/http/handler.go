package http

import (
	"context"
	stdhttp "net/http"
	"strings"

	"github.com/gin-gonic/gin"

	apikeycommand "orbitjob/internal/admin/app/apikey/command"
	apikeyquery "orbitjob/internal/admin/app/apikey/query"
	checkcommand "orbitjob/internal/admin/app/check/command"
	checkquery "orbitjob/internal/admin/app/check/query"
	checkrunquery "orbitjob/internal/admin/app/checkrun/query"
	instancecommand "orbitjob/internal/admin/app/instance/command"
	instancequery "orbitjob/internal/admin/app/instance/query"
	command "orbitjob/internal/admin/app/job/command"
	query "orbitjob/internal/admin/app/job/query"
	tenantcommand "orbitjob/internal/admin/app/tenant/command"
	tenantquery "orbitjob/internal/admin/app/tenant/query"
	slicommand "orbitjob/internal/admin/app/sli/command"
	sliquery "orbitjob/internal/admin/app/sli/query"
	slocommand "orbitjob/internal/admin/app/slo/command"
	sloquery "orbitjob/internal/admin/app/slo/query"
	sloalertquery "orbitjob/internal/admin/app/sloalert/query"
	slobudgetquery "orbitjob/internal/admin/app/slobudget/query"
	"orbitjob/internal/admin/bootstrap"
	"orbitjob/internal/admin/http/apperror"
	"orbitjob/internal/admin/http/middleware"
	domaincheck "orbitjob/internal/core/domain/check"
	domaininstance "orbitjob/internal/core/domain/instance"
	"orbitjob/internal/domain/validation"
	"orbitjob/internal/platform/metrics"
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
	Get(ctx context.Context, tenantID, runID string) (*instancequery.InstanceItem, error)
}

type cancelInstanceUseCase interface {
	Cancel(ctx context.Context, in instancecommand.CancelInstanceInput) (domaininstance.Snapshot, error)
}

type listAttemptsUseCase interface {
	List(ctx context.Context, tenantID, runID string) ([]instancequery.AttemptItem, error)
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

type createSLIUseCase interface {
	Create(ctx context.Context, in slicommand.CreateInput) (slicommand.CreateResult, error)
}

type listSLIsUseCase interface {
	List(ctx context.Context, in sliquery.ListInput) (sliquery.ListResult, error)
}

type getSLIUseCase interface {
	Get(ctx context.Context, tenantID string, id int64) (sliquery.GetItem, error)
}

type deleteSLIUseCase interface {
	Delete(ctx context.Context, in slicommand.DeleteInput) error
}

type createSLOUseCase interface {
	Create(ctx context.Context, in slocommand.CreateInput) (slocommand.CreateResult, error)
}

type listSLOsUseCase interface {
	List(ctx context.Context, in sloquery.ListInput) (sloquery.ListResult, error)
}

type getSLOUseCase interface {
	Get(ctx context.Context, tenantID string, id int64) (sloquery.GetItem, error)
}

type changeSLOStatusUseCase interface {
	Pause(ctx context.Context, tenantID string, in slocommand.ChangeStatusInput) (slocommand.ChangeStatusResult, error)
	Resume(ctx context.Context, tenantID string, in slocommand.ChangeStatusInput) (slocommand.ChangeStatusResult, error)
}

type deleteSLOUseCase interface {
	Delete(ctx context.Context, in slocommand.DeleteInput) error
}

type getBudgetUseCase interface {
	Get(ctx context.Context, tenantID string, sloID int64) (slobudgetquery.GetItem, error)
}

type listBudgetHistoryUseCase interface {
	List(ctx context.Context, in slobudgetquery.ListInput) (slobudgetquery.ListResult, error)
}

type getAlertUseCase interface {
	Get(ctx context.Context, tenantID string, id int64) (sloalertquery.GetItem, error)
}

type listAlertsUseCase interface {
	List(ctx context.Context, in sloalertquery.ListInput) (sloalertquery.ListResult, error)
}

type createTenantUseCase interface {
	Create(ctx context.Context, in tenantcommand.CreateInput) (tenantcommand.TenantCreateResult, error)
}

type listTenantsUseCase interface {
	List(ctx context.Context, in tenantquery.ListInput) ([]tenantquery.TenantListItem, error)
}

type getTenantUseCase interface {
	Get(ctx context.Context, in tenantquery.GetInput) (tenantquery.TenantGetResult, error)
}

type createAPIKeyUseCase interface {
	Create(ctx context.Context, in apikeycommand.CreateInput) (apikeycommand.APIKeyCreateResult, error)
}

type listAPIKeysUseCase interface {
	List(ctx context.Context, in apikeyquery.ListInput) ([]apikeyquery.APIKeyListItem, error)
}

type revokeAPIKeyUseCase interface {
	Revoke(ctx context.Context, in apikeycommand.RevokeInput) error
	RevokeAsAdmin(ctx context.Context, id string) error
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

type attemptListResponse struct {
	Items []instancequery.AttemptItem `json:"items"`
}

type sliListResponse struct {
	Items []sliquery.ListItem `json:"items"`
}

type sloListResponse struct {
	Items []sloquery.ListItem `json:"items"`
}

type budgetListResponse struct {
	Items []slobudgetquery.ListItem `json:"items"`
}

type alertListResponse struct {
	Items []sloalertquery.ListItem `json:"items"`
}

type tenantListResponse struct {
	Items []tenantquery.TenantListItem `json:"items"`
}

type apiKeyListResponse struct {
	Items []apikeyquery.APIKeyListItem `json:"items"`
}

// Handler wires HTTP endpoints to application use cases.
type Handler struct {
	createJobUC         createJobUseCase
	listJobsUC          listJobsUseCase
	getJobUC            getJobUseCase
	updateJobUC         updateJobUseCase
	statusJobUC         changeJobStatusUseCase
	deleteJobUC         deleteJobUseCase
	triggerJobUC        triggerJobUseCase
	listInstancesUC     listInstancesUseCase
	getInstanceUC       getInstanceUseCase
	cancelInstanceUC    cancelInstanceUseCase
	listAttemptsUC      listAttemptsUseCase
	createCheckUC       createCheckUseCase
	listChecksUC        listChecksUseCase
	getCheckUC          getCheckUseCase
	pauseCheckUC        pauseCheckUseCase
	resumeCheckUC       resumeCheckUseCase
	deleteCheckUC       deleteCheckUseCase
	listCheckRunsUC     listCheckRunsUseCase
	getCheckRunUC       getCheckRunUseCase
	createSLIUC         createSLIUseCase
	listSLIsUC          listSLIsUseCase
	getSLIUC            getSLIUseCase
	deleteSLIUC         deleteSLIUseCase
	createSLOUC         createSLOUseCase
	listSLOsUC          listSLOsUseCase
	getSLOUC            getSLOUseCase
	statusSLOUC         changeSLOStatusUseCase
	deleteSLOUC         deleteSLOUseCase
	getBudgetUC         getBudgetUseCase
	listBudgetHistoryUC listBudgetHistoryUseCase
	getAlertUC          getAlertUseCase
	listAlertsUC        listAlertsUseCase
	createTenantUC      createTenantUseCase
	listTenantsUC       listTenantsUseCase
	getTenantUC         getTenantUseCase
	createAPIKeyUC      createAPIKeyUseCase
	listAPIKeysUC       listAPIKeysUseCase
	revokeAPIKeyUC      revokeAPIKeyUseCase
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

func (h *Handler) SetDeleteJobUseCase(uc deleteJobUseCase)             { h.deleteJobUC = uc }
func (h *Handler) SetTriggerJobUseCase(uc triggerJobUseCase)           { h.triggerJobUC = uc }
func (h *Handler) SetListInstancesUseCase(uc listInstancesUseCase)     { h.listInstancesUC = uc }
func (h *Handler) SetGetInstanceUseCase(uc getInstanceUseCase)         { h.getInstanceUC = uc }
func (h *Handler) SetCancelInstanceUseCase(uc cancelInstanceUseCase)   { h.cancelInstanceUC = uc }
func (h *Handler) SetListAttemptsUseCase(uc listAttemptsUseCase)       { h.listAttemptsUC = uc }
func (h *Handler) SetCreateCheckUseCase(uc createCheckUseCase)         { h.createCheckUC = uc }
func (h *Handler) SetListChecksUseCase(uc listChecksUseCase)           { h.listChecksUC = uc }
func (h *Handler) SetGetCheckUseCase(uc getCheckUseCase)               { h.getCheckUC = uc }
func (h *Handler) SetPauseCheckUseCase(uc pauseCheckUseCase)           { h.pauseCheckUC = uc }
func (h *Handler) SetResumeCheckUseCase(uc resumeCheckUseCase)         { h.resumeCheckUC = uc }
func (h *Handler) SetDeleteCheckUseCase(uc deleteCheckUseCase)         { h.deleteCheckUC = uc }
func (h *Handler) SetListCheckRunsUseCase(uc listCheckRunsUseCase)     { h.listCheckRunsUC = uc }
func (h *Handler) SetGetCheckRunUseCase(uc getCheckRunUseCase)         { h.getCheckRunUC = uc }
func (h *Handler) SetCreateSLIUseCase(uc createSLIUseCase)             { h.createSLIUC = uc }
func (h *Handler) SetListSLIsUseCase(uc listSLIsUseCase)               { h.listSLIsUC = uc }
func (h *Handler) SetGetSLIUseCase(uc getSLIUseCase)                   { h.getSLIUC = uc }
func (h *Handler) SetDeleteSLIUseCase(uc deleteSLIUseCase)             { h.deleteSLIUC = uc }
func (h *Handler) SetCreateSLOUseCase(uc createSLOUseCase)             { h.createSLOUC = uc }
func (h *Handler) SetListSLOsUseCase(uc listSLOsUseCase)               { h.listSLOsUC = uc }
func (h *Handler) SetGetSLOUseCase(uc getSLOUseCase)                   { h.getSLOUC = uc }
func (h *Handler) SetChangeSLOStatusUseCase(uc changeSLOStatusUseCase) { h.statusSLOUC = uc }
func (h *Handler) SetDeleteSLOUseCase(uc deleteSLOUseCase)             { h.deleteSLOUC = uc }
func (h *Handler) SetGetBudgetUseCase(uc getBudgetUseCase)             { h.getBudgetUC = uc }
func (h *Handler) SetListBudgetHistoryUseCase(uc listBudgetHistoryUseCase) {
	h.listBudgetHistoryUC = uc
}
func (h *Handler) SetGetAlertUseCase(uc getAlertUseCase)     { h.getAlertUC = uc }
func (h *Handler) SetListAlertsUseCase(uc listAlertsUseCase) { h.listAlertsUC = uc }
func (h *Handler) SetCreateTenantUseCase(uc createTenantUseCase) { h.createTenantUC = uc }
func (h *Handler) SetListTenantsUseCase(uc listTenantsUseCase)   { h.listTenantsUC = uc }
func (h *Handler) SetGetTenantUseCase(uc getTenantUseCase)       { h.getTenantUC = uc }
func (h *Handler) SetCreateAPIKeyUseCase(uc createAPIKeyUseCase) { h.createAPIKeyUC = uc }
func (h *Handler) SetListAPIKeysUseCase(uc listAPIKeysUseCase)   { h.listAPIKeysUC = uc }
func (h *Handler) SetRevokeAPIKeyUseCase(uc revokeAPIKeyUseCase) { h.revokeAPIKeyUC = uc }

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
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	in := req.ToCreateInput()
	var ok bool
	in.TenantID, ok = requireTenantID(c, req.TenantID)
	if !ok {
		return
	}
	out, err := h.createJobUC.Create(c.Request.Context(), in)
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusCreated, out)
}

// ListJobs handles job list queries.
func (h *Handler) ListJobs(c *gin.Context) {
	var req ListJobsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	in := req.ToListInput()
	var ok bool
	in.TenantID, ok = requireTenantID(c, req.TenantID)
	if !ok {
		return
	}
	out, err := h.listJobsUC.List(c.Request.Context(), in)
	if err != nil {
		writeAPIError(c, err)
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
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}
	if err := c.ShouldBindQuery(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	in := req.ToGetInput()
	var ok bool
	in.TenantID, ok = requireTenantID(c, req.TenantID)
	if !ok {
		return
	}
	out, err := h.getJobUC.Get(c.Request.Context(), in)
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, out)
}

// UpdateJob handles mutable job updates.
func (h *Handler) UpdateJob(c *gin.Context) {
	var pathReq jobIDURI
	if err := c.ShouldBindUri(&pathReq); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	req := UpdateJobRequest{
		ID: pathReq.ID,
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID, ok := requireTenantID(c, "")
	if !ok {
		return
	}
	actorID, err := requiredActorID(c)
	if err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toAPIError(err))
		return
	}

	current, err := h.getJobUC.Get(c.Request.Context(), query.GetInput{
		ID:       req.ID,
		TenantID: tenantID,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	req.TenantID = tenantID
	out, err := h.updateJobUC.Update(c.Request.Context(), req.ToUpdateInput(current, actorID))
	if err != nil {
		writeAPIError(c, err)
		return
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
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	req := ChangeStatusRequest{
		ID: pathReq.ID,
	}
	var ok bool
	req.TenantID, ok = requireTenantID(c, "")
	if !ok {
		return
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	actorID, err := requiredActorID(c)
	if err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toAPIError(err))
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
		apiErr := apperror.APIError{Code: apperror.CodeInternal, Message: "unsupported status action"}
		apperror.Write(c, apperror.StatusForCode(apiErr.Code), apiErr)
		return
	}
	if err != nil {
		writeAPIError(c, err)
		return
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

// writeAPIError maps err to the stable API error structure, logs internal
// errors to gin's error list, and writes the response with the canonical
// HTTP status. It replaces the repetitive error-handling block in handlers.
func writeAPIError(c *gin.Context, err error) {
	apiErr := toAPIError(err)
	if apiErr.Code == apperror.CodeInternal {
		_ = c.Error(err)
	}
	apperror.Write(c, apperror.StatusForCode(apiErr.Code), apiErr)
}

// requireTenantID resolves a tenant from the explicit request value, falling
// back to the tenant established by authentication. If neither is present it
// writes a 403 Forbidden response and returns false.
func requireTenantID(c *gin.Context, fromRequest string) (string, bool) {
	tenantID, err := middleware.ResolveTenantID(c, fromRequest)
	if err != nil {
		apperror.Write(c, stdhttp.StatusForbidden, apperror.APIError{
			Code:    apperror.CodeForbidden,
			Message: "tenant required",
		})
		return "", false
	}
	return tenantID, true
}

// TriggerJob handles manual trigger requests.
func (h *Handler) TriggerJob(c *gin.Context) {
	var pathReq jobIDURI
	if err := c.ShouldBindUri(&pathReq); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID, ok := requireTenantID(c, "")
	if !ok {
		return
	}
	in := command.TriggerInput{
		JobID:    pathReq.ID,
		TenantID: tenantID,
	}

	reqCtx := c.Request.Context()
	if key, err := parseIdempotencyKey(c); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toAPIError(err))
		return
	} else if key != nil {
		reqCtx = middleware.WithIdempotencyKey(reqCtx, *key)
	}

	out, err := h.triggerJobUC.Trigger(reqCtx, in)
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

// DeleteJob handles soft-delete requests.
func (h *Handler) DeleteJob(c *gin.Context) {
	var pathReq jobIDURI
	if err := c.ShouldBindUri(&pathReq); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID, ok := requireTenantID(c, "")
	if !ok {
		return
	}
	out, err := h.deleteJobUC.Delete(c.Request.Context(), command.DeleteInput{
		ID:       pathReq.ID,
		TenantID: tenantID,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, out)
}

// ListInstances handles instance listing queries.
func (h *Handler) ListInstances(c *gin.Context) {
	var req ListInstancesRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID, ok := requireTenantID(c, "")
	if !ok {
		return
	}
	out, err := h.listInstancesUC.List(c.Request.Context(), instancequery.ListInstancesInput{
		TenantID: tenantID,
		Status:   req.Status,
		Limit:    req.Limit,
		Offset:   req.Offset,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, instanceListResponse{Items: out})
}

// GetInstance handles instance detail query by run_id.
func (h *Handler) GetInstance(c *gin.Context) {
	var pathReq instanceRunIDURI
	if err := c.ShouldBindUri(&pathReq); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID, ok := requireTenantID(c, "")
	if !ok {
		return
	}

	out, err := h.getInstanceUC.Get(c.Request.Context(), tenantID, pathReq.RunID)
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, out)
}

// CancelInstance handles instance cancellation requests for instances in
// pending, dispatched, running, or retry_wait status.
func (h *Handler) CancelInstance(c *gin.Context) {
	var pathReq instanceRunIDURI
	if err := c.ShouldBindUri(&pathReq); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	var body struct {
		Version int `json:"version" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID, ok := requireTenantID(c, "")
	if !ok {
		return
	}

	out, err := h.cancelInstanceUC.Cancel(c.Request.Context(), instancecommand.CancelInstanceInput{
		TenantID: tenantID,
		RunID:    pathReq.RunID,
		Version:  body.Version,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, out)
}

// ListAttempts lists the per-attempt execution trail for an instance.
func (h *Handler) ListAttempts(c *gin.Context) {
	var pathReq instanceRunIDURI
	if err := c.ShouldBindUri(&pathReq); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}
	tenantID, ok := requireTenantID(c, "")
	if !ok {
		return
	}
	out, err := h.listAttemptsUC.List(c.Request.Context(), tenantID, pathReq.RunID)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, attemptListResponse{Items: out})
}

// CreateCheck handles check creation requests.
func (h *Handler) CreateCheck(c *gin.Context) {
	var req CreateCheckRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	in := req.ToCreateInput()
	var ok bool
	in.TenantID, ok = requireTenantID(c, req.TenantID)
	if !ok {
		return
	}
	out, err := h.createCheckUC.Create(c.Request.Context(), in)
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusCreated, out)
}

// ListChecks handles check list queries.
func (h *Handler) ListChecks(c *gin.Context) {
	var req ListChecksRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	in := req.ToListInput()
	var ok bool
	in.TenantID, ok = requireTenantID(c, req.TenantID)
	if !ok {
		return
	}
	out, err := h.listChecksUC.List(c.Request.Context(), in)
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, checkListResponse{Items: out.Items})
}

// GetCheck handles one check detail query.
func (h *Handler) GetCheck(c *gin.Context) {
	var req GetCheckRequest
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
	out, err := h.getCheckUC.Get(c.Request.Context(), tenantID, req.ID)
	if err != nil {
		writeAPIError(c, err)
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
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	req := ChangeCheckStatusRequest{
		ID: pathReq.ID,
	}
	var ok bool
	req.TenantID, ok = requireTenantID(c, "")
	if !ok {
		return
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
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
		apiErr := apperror.APIError{Code: apperror.CodeInternal, Message: "unsupported status action"}
		apperror.Write(c, apperror.StatusForCode(apiErr.Code), apiErr)
		return
	}
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, out)
}

// DeleteCheck handles soft-delete requests for checks.
func (h *Handler) DeleteCheck(c *gin.Context) {
	var pathReq checkIDURI
	if err := c.ShouldBindUri(&pathReq); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	var body struct {
		Version int `json:"version" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID, ok := requireTenantID(c, "")
	if !ok {
		return
	}

	err := h.deleteCheckUC.Delete(c.Request.Context(), checkcommand.DeleteInput{
		ID:       pathReq.ID,
		TenantID: tenantID,
		Version:  body.Version,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, gin.H{"deleted": true})
}

// ListCheckRuns handles check run list queries.
func (h *Handler) ListCheckRuns(c *gin.Context) {
	var req ListCheckRunsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	in := req.ToListInput()
	var ok bool
	in.TenantID, ok = requireTenantID(c, req.TenantID)
	if !ok {
		return
	}
	out, err := h.listCheckRunsUC.List(c.Request.Context(), in)
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, checkRunListResponse{Items: out.Items})
}

// GetCheckRun handles one check run detail query.
func (h *Handler) GetCheckRun(c *gin.Context) {
	var req GetCheckRunRequest
	if err := c.ShouldBindUri(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID, ok := requireTenantID(c, "")
	if !ok {
		return
	}
	out, err := h.getCheckRunUC.Get(c.Request.Context(), tenantID, req.ID)
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, out)
}

// CreateSLI handles SLI creation requests.
func (h *Handler) CreateSLI(c *gin.Context) {
	var req CreateSLIRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	in := req.ToCreateInput()
	var ok bool
	in.TenantID, ok = requireTenantID(c, "")
	if !ok {
		return
	}
	out, err := h.createSLIUC.Create(c.Request.Context(), in)
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusCreated, out)
}

// ListSLIs handles SLI list queries.
func (h *Handler) ListSLIs(c *gin.Context) {
	var req ListSLIsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	in := req.ToListInput()
	var ok bool
	in.TenantID, ok = requireTenantID(c, req.TenantID)
	if !ok {
		return
	}
	out, err := h.listSLIsUC.List(c.Request.Context(), in)
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, sliListResponse{Items: out.Items})
}

// GetSLI handles one SLI detail query.
func (h *Handler) GetSLI(c *gin.Context) {
	var req GetSLIRequest
	if err := c.ShouldBindUri(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID, ok := requireTenantID(c, "")
	if !ok {
		return
	}
	out, err := h.getSLIUC.Get(c.Request.Context(), tenantID, req.ID)
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, out)
}

// DeleteSLI handles soft-delete requests for SLIs.
func (h *Handler) DeleteSLI(c *gin.Context) {
	var pathReq sliIDURI
	if err := c.ShouldBindUri(&pathReq); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	var body struct {
		Version int `json:"version" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID, ok := requireTenantID(c, "")
	if !ok {
		return
	}

	err := h.deleteSLIUC.Delete(c.Request.Context(), slicommand.DeleteInput{
		TenantID: tenantID,
		ID:       pathReq.ID,
		Version:  body.Version,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, gin.H{"deleted": true})
}

// CreateSLO handles SLO creation requests.
func (h *Handler) CreateSLO(c *gin.Context) {
	var req CreateSLORequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	in, err := req.ToCreateInput()
	if err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}
	var ok bool
	in.TenantID, ok = requireTenantID(c, "")
	if !ok {
		return
	}
	out, err := h.createSLOUC.Create(c.Request.Context(), in)
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusCreated, out)
}

// ListSLOs handles SLO list queries.
func (h *Handler) ListSLOs(c *gin.Context) {
	var req ListSLOsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	in := req.ToListInput()
	var ok bool
	in.TenantID, ok = requireTenantID(c, req.TenantID)
	if !ok {
		return
	}
	out, err := h.listSLOsUC.List(c.Request.Context(), in)
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, sloListResponse{Items: out.Items})
}

// GetSLO handles one SLO detail query.
func (h *Handler) GetSLO(c *gin.Context) {
	var req GetSLORequest
	if err := c.ShouldBindUri(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID, ok := requireTenantID(c, "")
	if !ok {
		return
	}
	out, err := h.getSLOUC.Get(c.Request.Context(), tenantID, req.ID)
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, out)
}

// PauseSLO handles SLO pause requests.
func (h *Handler) PauseSLO(c *gin.Context) {
	h.changeSLOStatus(c, "pause")
}

// ResumeSLO handles SLO resume requests.
func (h *Handler) ResumeSLO(c *gin.Context) {
	h.changeSLOStatus(c, "resume")
}

func (h *Handler) changeSLOStatus(c *gin.Context, action string) {
	var pathReq sloIDURI
	if err := c.ShouldBindUri(&pathReq); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	var body struct {
		Version int `json:"version" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	in := slocommand.ChangeStatusInput{
		ID:      pathReq.ID,
		Version: body.Version,
	}
	tenantID, ok := requireTenantID(c, "")
	if !ok {
		return
	}
	var out slocommand.ChangeStatusResult
	var err error
	switch action {
	case "pause":
		out, err = h.statusSLOUC.Pause(c.Request.Context(), tenantID, in)
	case "resume":
		out, err = h.statusSLOUC.Resume(c.Request.Context(), tenantID, in)
	default:
		apiErr := apperror.APIError{Code: apperror.CodeInternal, Message: "unsupported action"}
		apperror.Write(c, apperror.StatusForCode(apiErr.Code), apiErr)
		return
	}
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, out)
}

// DeleteSLO handles soft-delete requests for SLOs.
func (h *Handler) DeleteSLO(c *gin.Context) {
	var pathReq sloIDURI
	if err := c.ShouldBindUri(&pathReq); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	var body struct {
		Version int `json:"version" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID, ok := requireTenantID(c, "")
	if !ok {
		return
	}

	err := h.deleteSLOUC.Delete(c.Request.Context(), slocommand.DeleteInput{
		TenantID: tenantID,
		ID:       pathReq.ID,
		Version:  body.Version,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, gin.H{"deleted": true})
}

// GetSLOBudget handles SLO budget detail query.
func (h *Handler) GetSLOBudget(c *gin.Context) {
	var req GetSLOBudgetRequest
	if err := c.ShouldBindUri(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID, ok := requireTenantID(c, "")
	if !ok {
		return
	}
	out, err := h.getBudgetUC.Get(c.Request.Context(), tenantID, req.SLOID)
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, out)
}

// ListSLOBudgets handles SLO budget history list queries.
func (h *Handler) ListSLOBudgets(c *gin.Context) {
	var pathReq sloIDURI
	if err := c.ShouldBindUri(&pathReq); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	var req ListSLOBudgetsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	in := req.ToListInput()
	in.SLOID = pathReq.ID
	var ok bool
	in.TenantID, ok = requireTenantID(c, req.TenantID)
	if !ok {
		return
	}
	out, err := h.listBudgetHistoryUC.List(c.Request.Context(), in)
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, budgetListResponse{Items: out.Items})
}

// GetSLOAlert handles one SLO alert detail query.
func (h *Handler) GetSLOAlert(c *gin.Context) {
	var req GetSLOAlertRequest
	if err := c.ShouldBindUri(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID, ok := requireTenantID(c, "")
	if !ok {
		return
	}
	out, err := h.getAlertUC.Get(c.Request.Context(), tenantID, req.ID)
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, out)
}

// ListSLOAlerts handles SLO alert list queries.
func (h *Handler) ListSLOAlerts(c *gin.Context) {
	var req ListSLOAlertsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	in := req.ToListInput()
	var ok bool
	in.TenantID, ok = requireTenantID(c, req.TenantID)
	if !ok {
		return
	}
	out, err := h.listAlertsUC.List(c.Request.Context(), in)
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, alertListResponse{Items: out.Items})
}

// CreateTenant handles tenant creation requests.
func (h *Handler) CreateTenant(c *gin.Context) {
	var req CreateTenantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	out, err := h.createTenantUC.Create(c.Request.Context(), req.ToCreateInput())
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusCreated, out)
}

// ListTenants handles tenant list queries.
func (h *Handler) ListTenants(c *gin.Context) {
	var req ListTenantsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID, ok := requireTenantID(c, "")
	if !ok {
		return
	}
	in := req.ToListInput()
	in.TenantID = tenantID
	out, err := h.listTenantsUC.List(c.Request.Context(), in)
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, tenantListResponse{Items: out})
}

// GetTenant handles one tenant detail query.
func (h *Handler) GetTenant(c *gin.Context) {
	var req TenantURI
	if err := c.ShouldBindUri(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID, ok := requireTenantID(c, "")
	if !ok {
		return
	}
	out, err := h.getTenantUC.Get(c.Request.Context(), tenantquery.GetInput{TenantID: tenantID, ID: req.ID})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, out)
}

// CreateAPIKey handles API key creation requests.
func (h *Handler) CreateAPIKey(c *gin.Context) {
	var pathReq TenantURI
	if err := c.ShouldBindUri(&pathReq); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	var req CreateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	out, err := h.createAPIKeyUC.Create(c.Request.Context(), req.ToCreateInput(pathReq.ID))
	if err != nil {
		writeAPIError(c, err)
		return
	}

	metrics.APIKeysTotal.WithLabelValues(pathReq.ID).Inc()
	c.JSON(stdhttp.StatusCreated, out)
}

// ListAPIKeys handles API key list queries.
func (h *Handler) ListAPIKeys(c *gin.Context) {
	var pathReq TenantURI
	if err := c.ShouldBindUri(&pathReq); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	out, err := h.listAPIKeysUC.List(c.Request.Context(), apikeyquery.ListInput{TenantID: pathReq.ID})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, apiKeyListResponse{Items: out})
}

// RevokeAPIKey handles API key revocation requests.
func (h *Handler) RevokeAPIKey(c *gin.Context) {
	var pathReq APIKeyURI
	if err := c.ShouldBindUri(&pathReq); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID, ok := requireTenantID(c, "")
	if !ok {
		return
	}

	var err error
	if tenantID == bootstrap.DefaultTenantID {
		err = h.revokeAPIKeyUC.RevokeAsAdmin(c.Request.Context(), pathReq.ID)
	} else {
		err = h.revokeAPIKeyUC.Revoke(c.Request.Context(), apikeycommand.RevokeInput{ID: pathReq.ID, TenantID: tenantID})
	}
	if err != nil {
		writeAPIError(c, err)
		return
	}

	metrics.APIKeysRevokedTotal.WithLabelValues(tenantID).Inc()
	c.JSON(stdhttp.StatusOK, apikeycommand.APIKeyRevokeResult{Revoked: true})
}
