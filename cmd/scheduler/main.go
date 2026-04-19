package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strconv"
	"time"

	adminpostgres "orbitjob/internal/admin/store/postgres"
	"orbitjob/internal/core/app/schedule"
	corepostgres "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/platform/config"
	platformlogger "orbitjob/internal/platform/logger"
)

type runtimeConfig struct {
	BatchSize    int
	TickInterval time.Duration
}

func loadSchedulerRuntimeConfig() (runtimeConfig, error) {
	batchSize, err := loadPositiveIntEnv("SCHEDULER_BATCH_SIZE", 100)
	if err != nil {
		return runtimeConfig{}, err
	}

	tickIntervalSec, err := loadPositiveIntEnv("SCHEDULER_TICK_INTERVAL_SEC", 5)
	if err != nil {
		return runtimeConfig{}, err
	}

	return runtimeConfig{
		BatchSize:    batchSize,
		TickInterval: time.Duration(tickIntervalSec) * time.Second,
	}, nil
}

func loadPositiveIntEnv(key string, defaultValue int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return defaultValue, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	if value < 1 {
		return 0, fmt.Errorf("%s must be >= 1", key)
	}

	return value, nil
}

func main() {
	if err := config.LoadDotenv(); err != nil {
		log.Fatal(err)
	}

	slog.SetDefault(platformlogger.New(os.Getenv("APP_ENV")))

	dsn := os.Getenv("DATABASE_DSN")
	if dsn == "" {
		log.Fatal("DATABASE_DSN is required")
	}

	cfg, err := loadSchedulerRuntimeConfig()
	if err != nil {
		log.Fatal(err)
	}

	db, err := adminpostgres.Open(dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		_ = db.Close()
	}()

	repo := corepostgres.NewSchedulerRepository(db)
	uc := schedule.NewTickUseCase(repo)
	ticker := time.NewTicker(cfg.TickInterval)
	defer ticker.Stop()

	for {
		now := time.Now().UTC()
		handled, err := uc.RunBatch(context.Background(), now, cfg.BatchSize)
		if err != nil {
			slog.Error("scheduler tick failed", "error", err.Error())
		} else {
			slog.Info("scheduler tick completed", "handled_due_jobs", handled)
		}

		<-ticker.C
	}
}
