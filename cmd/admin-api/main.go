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

	httpapi "orbitjob/internal/admin/transport/http"
	"orbitjob/internal/application/jobapp"
	"orbitjob/internal/config"
	"orbitjob/internal/store/postgres"
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

func newRouter(handler *httpapi.Handler) *gin.Engine {
	r := gin.Default()
	r.Use(traceMiddleware())

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
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
	logger := config.InitLogger(os.Getenv("APP_ENV"))
	slog.SetDefault(logger)

	dsn := os.Getenv("DATABASE_DSN")
	if dsn == "" {
		log.Fatal("DATABASE_DSN is required")
	}

	db, err := postgres.Open(dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		_ = db.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		log.Fatal(err)
	}

	repo := postgres.NewJobRepository(db)
	createJobUC := jobapp.NewCreateJobUseCase(repo)
	listJobsUC := jobapp.NewListJobsUseCase(repo)
	handler := httpapi.NewHandler(createJobUC, listJobsUC)

	if err := newRouter(handler).Run(":8080"); err != nil {
		log.Fatal(err)
	}
}
