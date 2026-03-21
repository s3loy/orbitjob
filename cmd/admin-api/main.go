package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"orbitjob/internal/application/jobapp"
	"orbitjob/internal/config"
	"orbitjob/internal/store/postgres"
	httpapi "orbitjob/internal/transport/http"
)

func newRouter(handler *httpapi.Handler) *gin.Engine {
	r := gin.Default()

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	if handler != nil {
		handler.Register(r)
	}

	return r
}

func main() {
	if err := config.LoadDotenv(); err != nil {
		log.Fatal(err)
	}

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
	uc := jobapp.NewCreateJobUseCase(repo)
	handler := httpapi.NewHandler(uc)

	if err := newRouter(handler).Run(":8080"); err != nil {
		log.Fatal(err)
	}
}
