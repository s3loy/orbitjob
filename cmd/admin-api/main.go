package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

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
	adminhttp "orbitjob/internal/admin/http"
	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/admin/kube"
	adminpostgres "orbitjob/internal/admin/store/postgres"
	"orbitjob/internal/core/app/functioninvoke"
	corepostgres "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/platform/config"
	platformlogger "orbitjob/internal/platform/logger"
)

// Seams for the Kubernetes client the manual trigger path needs.
var (
	inClusterConfigFn  = rest.InClusterConfig
	newDynamicClientFn = func(cfg *rest.Config) (dynamic.Interface, error) {
		return dynamic.NewForConfig(cfg)
	}
)

// newJobRunPublisher builds the clients the trigger paths publish custom
// resources through. The API cannot write the ledger itself --
// orbitjob_admin holds SELECT only on the control-plane tables -- so these
// clients are the whole of its trigger and cancel paths, and a process
// without them can start but cannot trigger or cancel anything. Build them
// before serving.
func newJobRunPublisher() (kube.JobRunPublisher, kube.WorkflowRunPublisher, error) {
	cfg, err := inClusterConfigFn()
	if err != nil {
		return kube.JobRunPublisher{}, kube.WorkflowRunPublisher{}, fmt.Errorf("build in-cluster config: %w", err)
	}
	client, err := newDynamicClientFn(cfg)
	if err != nil {
		return kube.JobRunPublisher{}, kube.WorkflowRunPublisher{}, fmt.Errorf("build dynamic client: %w", err)
	}
	return kube.JobRunPublisher{Client: client}, kube.WorkflowRunPublisher{Client: client}, nil
}

func newRouter(handler *adminhttp.Handler, auth *middleware.Auth, rl *middleware.RateLimiter) *gin.Engine {
	r := gin.Default()
	// Front of the chain so rejections by later middleware (401, 429) are
	// still counted with the status the client received.
	r.Use(middleware.RequestMetrics())
	r.Use(middleware.TraceMiddleware())
	if auth != nil {
		r.Use(auth.Middleware())
	}
	if rl != nil {
		r.Use(rl.Middleware())
	}

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	r.GET("/openapi.json", func(c *gin.Context) {
		c.JSON(http.StatusOK, adminhttp.ServiceOpenAPIDocument())
	})

	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	if handler != nil {
		handler.Register(r)
	}

	return r
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := config.LoadDotenv(); err != nil {
		log.Fatal(err)
	}
	if os.Getenv("GIN_MODE") == "" && os.Getenv("APP_ENV") == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	logger := platformlogger.New(os.Getenv("APP_ENV"))
	slog.SetDefault(logger)

	dsn, _, err := config.ResolveDatabaseDSN("ADMIN_DSN", "DATABASE_DSN")
	if err != nil {
		log.Fatal(err)
	}

	db, err := adminpostgres.Open(dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	pingCtx, pingCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer pingCancel()
	if err := db.PingContext(pingCtx); err != nil {
		log.Fatal(err)
	}

	publisher, workflowPublisher, err := newJobRunPublisher()
	if err != nil {
		log.Fatal(err)
	}
	handler := buildHandler(db, publisher, workflowPublisher)

	auth := middleware.NewAuth(db)
	// Wire the policy loader: without it, requests authenticate but carry no
	// grants, so every guarded route denies. It wraps the same pool and reads
	// the same documents table the policy endpoints use.
	auth.Documents = adminpostgres.NewPolicyRepository(db)
	rl := middleware.NewRateLimiter(ctx)

	addr := ":" + os.Getenv("PORT")
	if addr == ":" {
		addr = ":8080"
	}
	srv := &http.Server{
		Addr:         addr,
		Handler:      newRouter(handler, auth, rl),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("admin-api listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	slog.Info("admin-api shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("admin-api shutdown error", "error", err)
	}
}

// buildHandler constructs every use case over its store and wires the result
// into one Handler. A use case that is never set leaves its routes behind the
// enabled-gate and unregistered, so this function is the whole answer to
// "which routes does this binary serve".
func buildHandler(
	db *sql.DB,
	jobPublisher kube.JobRunPublisher,
	workflowPublisher kube.WorkflowRunPublisher,
) *adminhttp.Handler {
	// Job definitions are read here; they are declared in Kubernetes. There is
	// no create, update or delete use case any more: the declaration is the
	// CR, and the API only reads the revisions projected from it.
	jobRepo := adminpostgres.NewJobRepository(db)
	listJobsUC := jobquery.NewListJobsUseCase(jobRepo)
	getJobUC := jobquery.NewGetJobUseCase(jobRepo)

	// A manual trigger publishes a JobRun Custom Resource. The operator, which
	// owns the control-plane tables, creates the run row in the same
	// transaction as the attempt and the audit entry, so the API never writes
	// the ledger.
	triggerJobUC := jobcommand.NewTriggerJobUseCase(jobRepo, jobPublisher)

	runRepo := adminpostgres.NewRunRepository(db)
	listInstancesUC := runquery.NewListRunsUseCase(runRepo)
	getInstanceUC := runquery.NewGetRunUseCase(runRepo)
	// A cancel request reads the run tenant-scoped, then patches
	// spec.cancelRequested on its JobRun Custom Resource. The operator does
	// the stop and the ledger writes, so the API needs no write grant.
	cancelRunUC := runcommand.NewCancelRunUseCase(runRepo, jobPublisher)
	listAttemptsUC := runquery.NewListAttemptsUseCase(runRepo)

	// Check use cases.
	checkWriteRepo := corepostgres.NewCheckRepository(db)
	checkReadRepo := adminpostgres.NewCheckRepository(db)
	createCheckUC := checkcommand.NewCreateCheckUseCase(checkWriteRepo)
	listChecksUC := checkquery.NewListChecksUseCase(checkReadRepo)
	getCheckUC := checkquery.NewGetCheckUseCase(checkReadRepo)
	pauseCheckUC := checkcommand.NewPauseCheckUseCase(checkWriteRepo)
	resumeCheckUC := checkcommand.NewResumeCheckUseCase(checkWriteRepo)
	deleteCheckUC := checkcommand.NewDeleteCheckUseCase(checkWriteRepo)

	// Check run use cases.
	checkRunReadRepo := adminpostgres.NewCheckRunRepository(db)
	listCheckRunsUC := checkrunquery.NewListCheckRunsUseCase(checkRunReadRepo)
	getCheckRunUC := checkrunquery.NewGetCheckRunUseCase(checkRunReadRepo)

	handler := adminhttp.NewHandler(listJobsUC, getJobUC, triggerJobUC)
	handler.SetListInstancesUseCase(listInstancesUC)
	handler.SetGetInstanceUseCase(getInstanceUC)
	handler.SetCancelRunUseCase(cancelRunUC)
	handler.SetListAttemptsUseCase(listAttemptsUC)
	handler.SetCreateCheckUseCase(createCheckUC)
	handler.SetListChecksUseCase(listChecksUC)
	handler.SetGetCheckUseCase(getCheckUC)
	handler.SetPauseCheckUseCase(pauseCheckUC)
	handler.SetResumeCheckUseCase(resumeCheckUC)
	handler.SetDeleteCheckUseCase(deleteCheckUC)
	handler.SetListCheckRunsUseCase(listCheckRunsUC)
	handler.SetGetCheckRunUseCase(getCheckRunUC)

	// SLO/SLI use cases.
	sliWriteRepo := corepostgres.NewSLIRepository(db)
	sliReadRepo := adminpostgres.NewSLIReadRepository(db)
	createSLIUC := slicommand.NewCreateSLIUseCase(sliWriteRepo)
	listSLIsUC := sliquery.NewListSLIsUseCase(sliReadRepo)
	getSLIUC := sliquery.NewGetSLIUseCase(sliReadRepo)
	deleteSLIUC := slicommand.NewDeleteSLIUseCase(sliWriteRepo)

	sloWriteRepo := corepostgres.NewSLORepository(db)
	sloReadRepo := adminpostgres.NewSLOReadRepository(db)
	createSLOUC := slocommand.NewCreateSLOUseCase(sloWriteRepo)
	listSLOsUC := sloquery.NewListSLOsUseCase(sloReadRepo)
	getSLOUC := sloquery.NewGetSLOUseCase(sloReadRepo, adminpostgres.NewBudgetReadRepository(db))
	statusSLOUC := slocommand.NewChangeSLOStatusUseCase(sloWriteRepo)
	deleteSLOUC := slocommand.NewDeleteSLOUseCase(sloWriteRepo)

	budgetReadRepo := adminpostgres.NewBudgetReadRepository(db)
	getBudgetUC := slobudgetquery.NewGetBudgetUseCase(budgetReadRepo)
	listBudgetHistoryUC := slobudgetquery.NewListBudgetHistoryUseCase(budgetReadRepo)

	alertReadRepo := adminpostgres.NewBudgetAlertReadRepository(db)
	getAlertUC := sloalertquery.NewGetAlertUseCase(alertReadRepo)
	listAlertsUC := sloalertquery.NewListAlertsUseCase(alertReadRepo)

	handler.SetCreateSLIUseCase(createSLIUC)
	handler.SetListSLIsUseCase(listSLIsUC)
	handler.SetGetSLIUseCase(getSLIUC)
	handler.SetDeleteSLIUseCase(deleteSLIUC)
	handler.SetCreateSLOUseCase(createSLOUC)
	handler.SetListSLOsUseCase(listSLOsUC)
	handler.SetGetSLOUseCase(getSLOUC)
	handler.SetChangeSLOStatusUseCase(statusSLOUC)
	handler.SetDeleteSLOUseCase(deleteSLOUC)
	handler.SetGetBudgetUseCase(getBudgetUC)
	handler.SetListBudgetHistoryUseCase(listBudgetHistoryUC)
	handler.SetGetAlertUseCase(getAlertUC)
	handler.SetListAlertsUseCase(listAlertsUC)

	// Tenant use cases.
	tenantRepo := adminpostgres.NewTenantRepository(db)
	createTenantUC := tenantcommand.NewCreator(tenantRepo)
	listTenantsUC := tenantquery.NewLister(tenantRepo)
	getTenantUC := tenantquery.NewGetter(tenantRepo)

	handler.SetCreateTenantUseCase(createTenantUC)
	handler.SetListTenantsUseCase(listTenantsUC)
	handler.SetGetTenantUseCase(getTenantUC)

	// API key, policy and resource group use cases.
	apiKeyRepo := adminpostgres.NewAPIKeyRepository(db)
	policyRepo := adminpostgres.NewPolicyRepository(db)
	groupRepo := adminpostgres.NewResourceGroupRepository(db)

	// The audit trail is not wired separately: each repository writes the grant
	// and the record of it in one transaction, so there is no way to end up
	// with a live credential nobody can account for.
	createAPIKeyUC := apikeycommand.NewCreator(apiKeyRepo).
		WithPolicies(policyRepo).
		WithGroups(groupRepo)
	listAPIKeysUC := apikeyquery.NewLister(apiKeyRepo)
	revokeAPIKeyUC := apikeycommand.NewRevoker(apiKeyRepo)

	createPolicyUC := policycommand.NewCreator(policyRepo)
	listPoliciesUC := policyquery.NewLister(policyRepo)
	getPolicyUC := policyquery.NewGetter(policyRepo)
	deletePolicyUC := policycommand.NewDeleter(policyRepo)

	createGroupUC := resourcegroupcommand.NewCreator(groupRepo)
	listGroupsUC := resourcegroupquery.NewLister(groupRepo)

	handler.SetCreateAPIKeyUseCase(createAPIKeyUC)
	handler.SetListAPIKeysUseCase(listAPIKeysUC)
	handler.SetRevokeAPIKeyUseCase(revokeAPIKeyUC)
	handler.SetCreatePolicyUseCase(createPolicyUC)
	handler.SetListPoliciesUseCase(listPoliciesUC)
	handler.SetGetPolicyUseCase(getPolicyUC)
	handler.SetDeletePolicyUseCase(deletePolicyUC)
	handler.SetCreateGroupUseCase(createGroupUC)
	handler.SetListGroupsUseCase(listGroupsUC)

	// Function use cases. The definitions are rows this API serves read-only
	// plus invoke; the revisions the invoke path pins are the ones the
	// operator's sync loop materializes from those rows.
	functionRepo := corepostgres.NewFunctionRepository(db)
	functionRunRepo := corepostgres.NewFunctionRunRepository(db)
	revisions := corepostgres.NewControlPlaneRepository(db)
	workflowRunRepo := corepostgres.NewWorkflowRunRepository(db)
	workflowDefinitions := workflowDefinitionSource{
		definitions: corepostgres.NewWorkflowDefinitionRepository(db),
	}

	handler.SetListFunctionsUseCase(adminhttp.NewListFunctionsUseCase(functionRepo))
	handler.SetGetFunctionUseCase(adminhttp.NewGetFunctionUseCase(functionRepo))
	handler.SetInvokeFunctionUseCase(functioninvoke.New(
		functionRepo,
		controlPlaneRevisions{revisions},
		jobPublisher,
	))
	handler.SetListFunctionRunsUseCase(adminhttp.NewListFunctionRunsUseCase(functionRunRepo, functionRepo))
	handler.SetGetFunctionRunUseCase(adminhttp.NewGetFunctionRunUseCase(functionRunRepo, functionRepo))

	// Workflow use cases. The definitions are WorkflowJob custom resources
	// read here through their projection; runs live in the workflow ledger the
	// operator's walker advances.
	handler.SetListWorkflowsUseCase(adminhttp.NewListWorkflowsUseCase(workflowDefinitions))
	handler.SetGetWorkflowUseCase(adminhttp.NewGetWorkflowUseCase(workflowDefinitions))
	handler.SetTriggerWorkflowUseCase(adminhttp.NewTriggerWorkflowUseCase(workflowDefinitions, workflowPublisher))
	handler.SetListWorkflowRunsUseCase(adminhttp.NewListWorkflowRunsUseCase(workflowDefinitions, workflowRunRepo))
	handler.SetGetWorkflowRunUseCase(adminhttp.NewGetWorkflowRunUseCase(workflowDefinitions, workflowRunRepo))
	handler.SetCancelWorkflowRunUseCase(adminhttp.NewCancelWorkflowRunUseCase(workflowDefinitions, workflowRunRepo, workflowPublisher))

	return handler
}
