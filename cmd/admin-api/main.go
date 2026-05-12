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

	command "orbitjob/internal/admin/app/job/command"
	instancecommand "orbitjob/internal/admin/app/instance/command"
	instancequery "orbitjob/internal/admin/app/instance/query"
	query "orbitjob/internal/admin/app/job/query"
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
	triggerJobUC := command.NewTriggerJobUseCase(readRepo, corepostgres.NewInstanceRepository(db))

	instanceReadRepo := adminpostgres.NewInstanceRepository(db)
	instanceWriteRepo := corepostgres.NewInstanceRepository(db)
	listInstancesUC := instancequery.NewListInstancesUseCase(instanceReadRepo)
	getInstanceUC := instancequery.NewGetInstanceUseCase(instanceReadRepo)
	cancelInstanceUC := instancecommand.NewCancelInstanceUseCase(instanceReadRepo, instanceWriteRepo)

	handler := adminhttp.NewHandler(createJobUC, listJobsUC, getJobUC, updateJobUC, changeStatusUC)
	handler.SetDeleteJobUseCase(deleteJobUC)
	handler.SetTriggerJobUseCase(triggerJobUC)
	handler.SetListInstancesUseCase(listInstancesUC)
	handler.SetGetInstanceUseCase(getInstanceUC)
	handler.SetCancelInstanceUseCase(cancelInstanceUC)
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
