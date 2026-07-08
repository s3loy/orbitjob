package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	checkcommand "orbitjob/internal/admin/app/check/command"
	checkquery "orbitjob/internal/admin/app/check/query"
	checkrunquery "orbitjob/internal/admin/app/checkrun/query"
	apikeycommand "orbitjob/internal/admin/app/apikey/command"
	apikeyquery "orbitjob/internal/admin/app/apikey/query"
	command "orbitjob/internal/admin/app/job/command"
	instancecommand "orbitjob/internal/admin/app/instance/command"
	instancequery "orbitjob/internal/admin/app/instance/query"
	query "orbitjob/internal/admin/app/job/query"
	tenantcommand "orbitjob/internal/admin/app/tenant/command"
	tenantquery "orbitjob/internal/admin/app/tenant/query"
	slicommand "orbitjob/internal/admin/app/sli/command"
	sliquery "orbitjob/internal/admin/app/sli/query"
	slocommand "orbitjob/internal/admin/app/slo/command"
	sloquery "orbitjob/internal/admin/app/slo/query"
	slobudgetquery "orbitjob/internal/admin/app/slobudget/query"
	sloalertquery "orbitjob/internal/admin/app/sloalert/query"
	adminhttp "orbitjob/internal/admin/http"
	"orbitjob/internal/admin/http/middleware"
	adminpostgres "orbitjob/internal/admin/store/postgres"
	corepostgres "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/platform/config"
	platformlogger "orbitjob/internal/platform/logger"
)

func newRouter(handler *adminhttp.Handler, auth *middleware.Auth, rl *middleware.RateLimiter) *gin.Engine {
	r := gin.Default()
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

	dsn := os.Getenv("ADMIN_DSN")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_DSN")
	}
	if dsn == "" {
		log.Fatal("DATABASE_DSN is required")
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

	writeRepo := corepostgres.NewJobRepository(db)
	readRepo := adminpostgres.NewJobRepository(db)
	createJobUC := command.NewCreateJobUseCase(writeRepo, readRepo)
	updateJobUC := command.NewUpdateJobUseCase(writeRepo)
	changeStatusUC := command.NewChangeStatusUseCase(readRepo, writeRepo)
	listJobsUC := query.NewListJobsUseCase(readRepo)
	getJobUC := query.NewGetJobUseCase(readRepo)

	deleteJobUC := command.NewDeleteJobUseCase(writeRepo)
	triggerJobUC := command.NewTriggerJobUseCase(readRepo, corepostgres.NewInstanceRepository(db), corepostgres.NewInstanceRepository(db))

	instanceReadRepo := adminpostgres.NewInstanceRepository(db)
	instanceWriteRepo := corepostgres.NewInstanceRepository(db)
	listInstancesUC := instancequery.NewListInstancesUseCase(instanceReadRepo)
	getInstanceUC := instancequery.NewGetInstanceUseCase(instanceReadRepo)
	cancelInstanceUC := instancecommand.NewCancelInstanceUseCase(instanceReadRepo, instanceWriteRepo)
	listAttemptsUC := instancequery.NewListAttemptsUseCase(instanceReadRepo)

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

	handler := adminhttp.NewHandler(createJobUC, listJobsUC, getJobUC, updateJobUC, changeStatusUC)
	handler.SetDeleteJobUseCase(deleteJobUC)
	handler.SetTriggerJobUseCase(triggerJobUC)
	handler.SetListInstancesUseCase(listInstancesUC)
	handler.SetGetInstanceUseCase(getInstanceUC)
	handler.SetCancelInstanceUseCase(cancelInstanceUC)
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

	// API key use cases.
	apiKeyRepo := adminpostgres.NewAPIKeyRepository(db)
	createAPIKeyUC := apikeycommand.NewCreator(apiKeyRepo)
	listAPIKeysUC := apikeyquery.NewLister(apiKeyRepo)
	revokeAPIKeyUC := apikeycommand.NewRevoker(apiKeyRepo)

	handler.SetCreateAPIKeyUseCase(createAPIKeyUC)
	handler.SetListAPIKeysUseCase(listAPIKeysUC)
	handler.SetRevokeAPIKeyUseCase(revokeAPIKeyUC)

	auth := middleware.NewAuth(db)
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
