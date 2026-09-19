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
	jobcommand "orbitjob/internal/admin/app/job/command"
	jobquery "orbitjob/internal/admin/app/job/query"
	policycommand "orbitjob/internal/admin/app/policy/command"
	policyquery "orbitjob/internal/admin/app/policy/query"
	resourcegroupcommand "orbitjob/internal/admin/app/resourcegroup/command"
	resourcegroupquery "orbitjob/internal/admin/app/resourcegroup/query"
	runcommand "orbitjob/internal/admin/app/run/command"
	runquery "orbitjob/internal/admin/app/run/query"
	slicommand "orbitjob/internal/admin/app/sli/command"
	sliquery "orbitjob/internal/admin/app/sli/query"
	slocommand "orbitjob/internal/admin/app/slo/command"
	sloquery "orbitjob/internal/admin/app/slo/query"
	sloalertquery "orbitjob/internal/admin/app/sloalert/query"
	slobudgetquery "orbitjob/internal/admin/app/slobudget/query"
	tenantcommand "orbitjob/internal/admin/app/tenant/command"
	tenantquery "orbitjob/internal/admin/app/tenant/query"
	"orbitjob/internal/admin/bootstrap"
	"orbitjob/internal/admin/http/apperror"
	"orbitjob/internal/admin/http/middleware"
	domaincheck "orbitjob/internal/core/domain/check"
	"orbitjob/internal/domain/validation"
	"orbitjob/internal/platform/metrics"
)

// A job definition is a ScheduledJob Custom Resource projected into an
// immutable revision. The API server reads the revision; it neither creates nor
// mutates the definition, so there is no create, update, pause, resume or delete
// capability here. Declarations are applied as Custom Resources.
type listJobsUseCase interface {
	List(ctx context.Context, in jobquery.ListInput) ([]jobquery.ListItem, error)
}

type getJobUseCase interface {
	Get(ctx context.Context, in jobquery.GetInput) (jobquery.GetItem, error)
}

// triggerJobUseCase publishes a JobRun Custom Resource for a manual trigger.
// The operator owns the ledger write, so the API's trigger path cannot create a
// run row even by accident.
type triggerJobUseCase interface {
	Trigger(ctx context.Context, in jobcommand.TriggerInput) (jobcommand.TriggerResult, error)
}

type listInstancesUseCase interface {
	List(ctx context.Context, in runquery.ListInput) ([]runquery.ListItem, error)
}

type getInstanceUseCase interface {
	Get(ctx context.Context, in runquery.GetInput) (runquery.GetItem, error)
}

// cancelRunUseCase patches a stop intent onto a run's JobRun Custom Resource.
// The operator owns the stop and every ledger write that follows, so the
// API's cancel path has no database writes at all.
type cancelRunUseCase interface {
	Cancel(ctx context.Context, in runcommand.CancelInput) (runcommand.CancelResult, error)
}

// listAttemptsUseCase reads the ledger's attempt trail for one run. Each attempt
// is one Kubernetes Job; the trail is bounded by the definition's retry policy,
// so it needs no page parameters of its own.
type listAttemptsUseCase interface {
	List(ctx context.Context, in runquery.GetInput) ([]runquery.AttemptItem, error)
}

type createCheckUseCase interface {
	Create(ctx context.Context, in checkcommand.CreateInput) (checkcommand.CreateResult, error)
}

type listChecksUseCase interface {
	List(ctx context.Context, in checkquery.ListChecksInput) (checkquery.ListChecksResult, error)
}

type getCheckUseCase interface {
	Get(ctx context.Context, tenantID, resourceGroupID string, id int64) (checkquery.GetResult, error)
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
	Get(ctx context.Context, tenantID, resourceGroupID string, id int64) (sliquery.GetItem, error)
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
	Get(ctx context.Context, tenantID, resourceGroupID string, id int64) (sloquery.GetItem, error)
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

type createPolicyUseCase interface {
	Create(ctx context.Context, in policycommand.CreateInput) (policycommand.CreateResult, error)
}

type listPoliciesUseCase interface {
	List(ctx context.Context, in policyquery.ListInput) ([]policyquery.ListItem, error)
}

type getPolicyUseCase interface {
	Get(ctx context.Context, in policyquery.GetInput) (policyquery.GetResult, error)
}

type deletePolicyUseCase interface {
	Delete(ctx context.Context, in policycommand.DeleteInput) error
}

type createResourceGroupUseCase interface {
	Create(ctx context.Context, in resourcegroupcommand.CreateInput) (resourcegroupcommand.CreateResult, error)
}

type listResourceGroupsUseCase interface {
	List(ctx context.Context, in resourcegroupquery.ListInput) ([]resourcegroupquery.ListItem, error)
}

type checkListResponse struct {
	Items []checkquery.ListItem `json:"items"`
}

type checkRunListResponse struct {
	Items []checkrunquery.ListItem `json:"items"`
}

type jobListResponse struct {
	Items []jobquery.ListItem `json:"items"`
}

type runListResponse struct {
	Items []runquery.ListItem `json:"items"`
}

type attemptListResponse struct {
	Items []runquery.AttemptItem `json:"items"`
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

type policyListResponse struct {
	Items []policyquery.ListItem `json:"items"`
}

type resourceGroupListResponse struct {
	Items []resourcegroupquery.ListItem `json:"items"`
}

// Handler wires HTTP endpoints to application use cases.
type Handler struct {
	listJobsUC          listJobsUseCase
	getJobUC            getJobUseCase
	triggerJobUC        triggerJobUseCase
	listInstancesUC     listInstancesUseCase
	getInstanceUC       getInstanceUseCase
	cancelRunUC         cancelRunUseCase
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
	createPolicyUC      createPolicyUseCase
	listPoliciesUC      listPoliciesUseCase
	getPolicyUC         getPolicyUseCase
	deletePolicyUC      deletePolicyUseCase
	createGroupUC       createResourceGroupUseCase
	listGroupsUC        listResourceGroupsUseCase
	listFunctionsUC     listFunctionsUseCase
	getFunctionUC       getFunctionUseCase
	invokeFunctionUC    invokeFunctionUseCase
	listFunctionRunsUC  listFunctionRunsUseCase
	getFunctionRunUC    getFunctionRunUseCase
	listWorkflowsUC     listWorkflowsUseCase
	getWorkflowUC       getWorkflowUseCase
	triggerWorkflowUC   triggerWorkflowUseCase
	listWorkflowRunsUC  listWorkflowRunsUseCase
	getWorkflowRunUC    getWorkflowRunUseCase
	cancelWorkflowRunUC cancelWorkflowRunUseCase
}

func NewHandler(
	listJobsUC listJobsUseCase,
	getJobUC getJobUseCase,
	triggerJobUC triggerJobUseCase,
) *Handler {
	return &Handler{
		listJobsUC:   listJobsUC,
		getJobUC:     getJobUC,
		triggerJobUC: triggerJobUC,
	}
}

func (h *Handler) SetListInstancesUseCase(uc listInstancesUseCase)     { h.listInstancesUC = uc }
func (h *Handler) SetGetInstanceUseCase(uc getInstanceUseCase)         { h.getInstanceUC = uc }
func (h *Handler) SetCancelRunUseCase(uc cancelRunUseCase)             { h.cancelRunUC = uc }
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
func (h *Handler) SetGetAlertUseCase(uc getAlertUseCase)         { h.getAlertUC = uc }
func (h *Handler) SetListAlertsUseCase(uc listAlertsUseCase)     { h.listAlertsUC = uc }
func (h *Handler) SetCreateTenantUseCase(uc createTenantUseCase) { h.createTenantUC = uc }
func (h *Handler) SetListTenantsUseCase(uc listTenantsUseCase)   { h.listTenantsUC = uc }
func (h *Handler) SetGetTenantUseCase(uc getTenantUseCase)       { h.getTenantUC = uc }
func (h *Handler) SetCreateAPIKeyUseCase(uc createAPIKeyUseCase) { h.createAPIKeyUC = uc }
func (h *Handler) SetListAPIKeysUseCase(uc listAPIKeysUseCase)   { h.listAPIKeysUC = uc }
func (h *Handler) SetRevokeAPIKeyUseCase(uc revokeAPIKeyUseCase) { h.revokeAPIKeyUC = uc }
func (h *Handler) SetCreatePolicyUseCase(uc createPolicyUseCase) { h.createPolicyUC = uc }
func (h *Handler) SetListPoliciesUseCase(uc listPoliciesUseCase) { h.listPoliciesUC = uc }
func (h *Handler) SetGetPolicyUseCase(uc getPolicyUseCase)       { h.getPolicyUC = uc }
func (h *Handler) SetDeletePolicyUseCase(uc deletePolicyUseCase) { h.deletePolicyUC = uc }
func (h *Handler) SetCreateGroupUseCase(uc createResourceGroupUseCase) {
	h.createGroupUC = uc
}
func (h *Handler) SetListGroupsUseCase(uc listResourceGroupsUseCase) { h.listGroupsUC = uc }

// Register mounts HTTP routes for the admin API.
func (h *Handler) Register(r gin.IRouter) {
	v1 := r.Group(adminAPIPrefix)
	// Bound every body before a handler can read it. The guard applies to the
	// whole group so a new route cannot be added without it.
	v1.Use(limitRequestBody())
	for _, route := range adminAPIRoutes() {
		if route.enabled != nil && !route.enabled(h) {
			continue
		}
		// Routes that declare a permission are registered behind the
		// authorization guard; the three public endpoints are not.
		if guard := guardFor(route); guard != nil {
			route.register(permissionRouter{IRouter: v1, guard: guard}, h)
			continue
		}
		route.register(v1, h)
	}
}

// ListJobs lists the active revision of every definition in the caller's
// tenant.
func (h *Handler) ListJobs(c *gin.Context) {
	var req ListJobsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID, ok := requireTenantID(c, req.TenantID)
	if !ok {
		return
	}
	out, err := h.listJobsUC.List(c.Request.Context(), jobquery.ListInput{
		TenantID:        tenantID,
		ResourceGroupID: groupFrom(c),
		Limit:           req.Limit,
		Offset:          req.Offset,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, jobListResponse{Items: out})
}

// GetJob reads one definition's active revision by revision id.
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

	tenantID, ok := requireTenantID(c, req.TenantID)
	if !ok {
		return
	}
	out, err := h.getJobUC.Get(c.Request.Context(), jobquery.GetInput{
		ID:              req.ID,
		TenantID:        tenantID,
		ResourceGroupID: groupFrom(c),
	})
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

// requireTenantID resolves the tenant a tenant-scoped request acts on.
//
// The authenticated tenant wins. A request may name a different tenant only
// when the caller is a platform principal: the authorization guard on a
// tenant-scoped route resolves its ARN from the caller's own tenant, so a
// tenant key that named another tenant would be authorized against its own
// tenant and then read or write the other one. Refusing that here is what makes
// the guard's answer mean what it says.
func requireTenantID(c *gin.Context, fromRequest string) (string, bool) {
	tenantID, err := middleware.ResolveTenantID(c, "")
	if err != nil {
		writeForbidden(c, "tenant required")
		return "", false
	}
	if fromRequest == "" || fromRequest == tenantID {
		return tenantID, true
	}
	if p, ok := middleware.PrincipalFrom(c); ok && p.IsPlatform() {
		return fromRequest, true
	}
	writeForbidden(c, "tenant mismatch")
	return "", false
}

func writeForbidden(c *gin.Context, message string) {
	apperror.Write(c, stdhttp.StatusForbidden, apperror.APIError{
		Code:    apperror.CodeForbidden,
		Message: message,
	})
}

// groupFrom returns the resource group the caller's key is scoped to, or the
// empty string when it is not scoped. Create paths stamp it on the row they
// write and list paths use it to filter; both read it from here so the two
// cannot disagree about what the caller's scope is.
func groupFrom(c *gin.Context) string {
	p, ok := middleware.PrincipalFrom(c)
	if !ok {
		return ""
	}
	return p.ResourceGroupID
}

// requireTenantIDFor resolves the tenant a request targets, honouring an
// explicit tenant in the URL.
//
// It deliberately performs no authorization of its own. Whether a caller may
// act on the named tenant is decided by the route's Require guard, which
// resolves the target ARN from that same path parameter and evaluates it
// against the caller's policies. Duplicating that decision here -- for instance
// with a hardcoded "must be an admin key" rule -- would create a second,
// weaker answer to the same question.
func requireTenantIDFor(c *gin.Context, fromRequest string) (string, bool) {
	authenticated, ok := requireTenantID(c, "")
	if !ok {
		return "", false
	}
	if fromRequest == "" || fromRequest == authenticated {
		return authenticated, true
	}
	return fromRequest, true
}

// callerFrom assembles what the escalation guards need to know about the
// principal making this request.
func callerFrom(c *gin.Context) apikeycommand.Caller {
	p, ok := middleware.PrincipalFrom(c)
	if !ok {
		return apikeycommand.Caller{}
	}
	docs, _ := middleware.Documents(c)
	return apikeycommand.Caller{
		TenantID:         p.TenantID,
		KeyID:            p.KeyID,
		ResourceGroupID:  p.ResourceGroupID,
		Documents:        docs,
		IsPlatform:       p.IsPlatform(),
		BoundaryPolicyID: p.BoundaryPolicyID,
	}
}

// TriggerJob handles manual trigger requests.
//
// A manual trigger publishes a JobRun custom resource, not a ledger row. The
// operator owns the run table and writes the row in its own transaction, so the
// run's actor travels on the resource and the API never gains INSERT on the
// ledger. The actor is the authenticated key, not a client-supplied header:
// "who triggered it" has to name the credential that actually called.
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

	var idempotencyKey string
	if key, err := parseIdempotencyKey(c); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toAPIError(err))
		return
	} else if key != nil {
		idempotencyKey = *key
	}

	out, err := h.triggerJobUC.Trigger(c.Request.Context(), jobcommand.TriggerInput{
		JobID:           pathReq.ID,
		TenantID:        tenantID,
		ActorID:         actorID(c),
		IdempotencyKey:  idempotencyKey,
		ResourceGroupID: groupFrom(c),
	})
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

// ListInstances lists runs from the ledger, newest first, optionally narrowed
// to one phase.
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
	out, err := h.listInstancesUC.List(c.Request.Context(), runquery.ListInput{
		TenantID:        tenantID,
		Phase:           req.Phase,
		ResourceGroupID: groupFrom(c),
		Limit:           req.Limit,
		Offset:          req.Offset,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, runListResponse{Items: out})
}

// GetInstance reads one run by its ledger id, together with its attempt trail.
// One call answers the ledger's question, so a client does not have to fetch the
// run and then its attempts.
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

	out, err := h.getInstanceUC.Get(c.Request.Context(), runquery.GetInput{
		ID:              pathReq.RunID,
		TenantID:        tenantID,
		ResourceGroupID: groupFrom(c),
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, out)
}

// CancelInstance requests a stop for one run. The request carries no body on
// purpose: there is nothing left for a caller to state, and in particular no
// actor field -- the cancel's effect travels through the JobRun Custom
// Resource and the operator records the ledger transitions, so an actor the
// caller typed would not be evidence of anything.
func (h *Handler) CancelInstance(c *gin.Context) {
	var pathReq instanceRunIDURI
	if err := c.ShouldBindUri(&pathReq); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID, ok := requireTenantID(c, "")
	if !ok {
		return
	}

	out, err := h.cancelRunUC.Cancel(c.Request.Context(), runcommand.CancelInput{
		RunID:           pathReq.RunID,
		TenantID:        tenantID,
		ResourceGroupID: groupFrom(c),
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, out)
}

// ListAttempts lists the per-attempt execution trail for a run. Each row is one
// platform attempt, which maps to exactly one Kubernetes Job, so the trail
// answers both "how many times" and "which Jobs".
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
	out, err := h.listAttemptsUC.List(c.Request.Context(), runquery.GetInput{
		ID:              pathReq.RunID,
		TenantID:        tenantID,
		ResourceGroupID: groupFrom(c),
	})
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
	in.ResourceGroupID = groupFrom(c)
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
	in.ResourceGroupID = groupFrom(c)
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
	out, err := h.getCheckUC.Get(c.Request.Context(), tenantID, groupFrom(c), req.ID)
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
	req.ResourceGroupID = groupFrom(c)
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
		ID:              pathReq.ID,
		TenantID:        tenantID,
		ResourceGroupID: groupFrom(c),
		Version:         body.Version,
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
	in.ResourceGroupID = groupFrom(c)
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
	in.ResourceGroupID = groupFrom(c)
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
	out, err := h.getSLIUC.Get(c.Request.Context(), tenantID, groupFrom(c), req.ID)
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
		TenantID:        tenantID,
		ResourceGroupID: groupFrom(c),
		ID:              pathReq.ID,
		Version:         body.Version,
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
	in.ResourceGroupID = groupFrom(c)
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
	in.ResourceGroupID = groupFrom(c)
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
	out, err := h.getSLOUC.Get(c.Request.Context(), tenantID, groupFrom(c), req.ID)
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
		ID:              pathReq.ID,
		Version:         body.Version,
		ResourceGroupID: groupFrom(c),
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
		TenantID:        tenantID,
		ResourceGroupID: groupFrom(c),
		ID:              pathReq.ID,
		Version:         body.Version,
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
	_, ok := requireTenantID(c, "")
	if !ok {
		return
	}

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

	// The tenant in the path is the tenant the key belongs to. Naming one other
	// than the caller's own is a management operation, permitted only with an
	// admin key -- previously this id was parsed and then discarded, so every
	// key was minted for the caller's own tenant regardless of the URL.
	tenantID, ok := requireTenantIDFor(c, pathReq.ID)
	if !ok {
		return
	}

	var req CreateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	out, err := h.createAPIKeyUC.Create(c.Request.Context(), req.ToCreateInput(tenantID, callerFrom(c)))
	if err != nil {
		writeAPIError(c, err)
		return
	}

	metrics.APIKeysTotal.WithLabelValues(tenantID).Inc()
	c.JSON(stdhttp.StatusCreated, out)
}

// ListAPIKeys handles API key list queries.
func (h *Handler) ListAPIKeys(c *gin.Context) {
	var pathReq TenantURI
	if err := c.ShouldBindUri(&pathReq); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID, ok := requireTenantIDFor(c, pathReq.ID)
	if !ok {
		return
	}

	out, err := h.listAPIKeysUC.List(c.Request.Context(), apikeyquery.ListInput{TenantID: tenantID})
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
		err = h.revokeAPIKeyUC.Revoke(c.Request.Context(), apikeycommand.RevokeInput{
			ID:       pathReq.ID,
			TenantID: tenantID,
			ActorID:  actorID(c),
		})
	}
	if err != nil {
		writeAPIError(c, err)
		return
	}

	metrics.APIKeysRevokedTotal.WithLabelValues(tenantID).Inc()
	c.JSON(stdhttp.StatusOK, apikeycommand.APIKeyRevokeResult{Revoked: true})
}

// CreatePolicy handles policy creation.
//
// The tenant is the caller's own: a policy is authored inside a tenant and
// bound by that tenant. Cross-tenant policy authoring is not a feature, so the
// tenant is never taken from the request body.
func (h *Handler) CreatePolicy(c *gin.Context) {
	tenantID, ok := requireTenantID(c, "")
	if !ok {
		return
	}

	var req CreatePolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	out, err := h.createPolicyUC.Create(c.Request.Context(), req.ToCreateInput(tenantID, actorID(c)))
	if err != nil {
		writeAPIError(c, err)
		return
	}

	metrics.PoliciesTotal.WithLabelValues(tenantID).Inc()
	c.JSON(stdhttp.StatusCreated, out)
}

// ListPolicies handles policy list queries.
func (h *Handler) ListPolicies(c *gin.Context) {
	tenantID, ok := requireTenantID(c, "")
	if !ok {
		return
	}

	out, err := h.listPoliciesUC.List(c.Request.Context(), policyquery.ListInput{TenantID: tenantID})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, policyListResponse{Items: out})
}

// GetPolicy handles reading one policy.
func (h *Handler) GetPolicy(c *gin.Context) {
	var pathReq PolicyURI
	if err := c.ShouldBindUri(&pathReq); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID, ok := requireTenantID(c, "")
	if !ok {
		return
	}

	out, err := h.getPolicyUC.Get(c.Request.Context(), policyquery.GetInput{
		TenantID: tenantID,
		ID:       pathReq.ID,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, out)
}

// DeletePolicy handles policy deletion.
func (h *Handler) DeletePolicy(c *gin.Context) {
	var pathReq PolicyURI
	if err := c.ShouldBindUri(&pathReq); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	tenantID, ok := requireTenantID(c, "")
	if !ok {
		return
	}

	err := h.deletePolicyUC.Delete(c.Request.Context(), policycommand.DeleteInput{
		TenantID: tenantID,
		ID:       pathReq.ID,
		ActorID:  actorID(c),
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.Status(stdhttp.StatusNoContent)
}

// CreateResourceGroup handles resource group creation.
func (h *Handler) CreateResourceGroup(c *gin.Context) {
	tenantID, ok := requireTenantID(c, "")
	if !ok {
		return
	}

	var req CreateResourceGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apperror.Write(c, stdhttp.StatusBadRequest, toBindAPIError(err))
		return
	}

	out, err := h.createGroupUC.Create(c.Request.Context(), req.ToCreateInput(tenantID, actorID(c)))
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusCreated, out)
}

// ListResourceGroups handles resource group list queries.
func (h *Handler) ListResourceGroups(c *gin.Context) {
	tenantID, ok := requireTenantID(c, "")
	if !ok {
		return
	}

	out, err := h.listGroupsUC.List(c.Request.Context(), resourcegroupquery.ListInput{TenantID: tenantID})
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(stdhttp.StatusOK, resourceGroupListResponse{Items: out})
}
