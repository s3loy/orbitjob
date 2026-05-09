package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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

func traceMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		traceID := c.GetHeader("X-Trace-ID")
		if traceID == "" {
			traceID = uuid.New().String()
		}
		c.Set("trace_id", traceID)
		c.Header("X-Trace-ID", traceID)
		c.Next()
	}
}

func newRouter(handler *adminhttp.Handler, auth *middleware.Auth, rl *middleware.RateLimiter) *gin.Engine {
	r := gin.Default()
	r.Use(traceMiddleware())
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
	if err := config.LoadDotenv(); err != nil {
		log.Fatal(err)
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
	db.SetMaxOpenConns(25)
	defer func() {
		_ = db.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		log.Fatal(err)
	}

	writeRepo := corepostgres.NewJobRepository(db)
	readRepo := adminpostgres.NewJobRepository(db)
	createJobUC := command.NewCreateJobUseCase(writeRepo)
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
	rl := middleware.NewRateLimiter()

	if err := newRouter(handler, auth, rl).Run(":8080"); err != nil {
		log.Fatal(err)
	}
}
