package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	adminpostgres "orbitjob/internal/admin/store/postgres"
	"orbitjob/internal/core/app/schedule"
	corepostgres "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/platform/config"
	"orbitjob/internal/platform/health"
	platformlogger "orbitjob/internal/platform/logger"
	"orbitjob/internal/platform/metrics"
	platformticker "orbitjob/internal/platform/ticker"
)

type runtimeConfig struct {
	BatchSize    int
	TickInterval time.Duration
	HealthPort   string
}

type tickRunner interface {
	RunBatch(ctx context.Context, now time.Time, limit int) (int, error)
}

type schedulerTicker interface {
	Chan() <-chan time.Time
	Stop()
}

const startupDBPingTimeout = 5 * time.Second

var (
	loadDotenvFn  = config.LoadDotenv
	newLoggerFn   = platformlogger.New
	openDBFn      = adminpostgres.Open
	pingDBFn      = func(ctx context.Context, db *sql.DB) error { return db.PingContext(ctx) }
	buildRunnerFn = func(db *sql.DB) tickRunner {
		repo := corepostgres.NewSchedulerRepository(db)
		return schedule.NewTickUseCase(repo)
	}
	runLoopFn = runLoop
)

var newWallClockTicker = func(d time.Duration) schedulerTicker { return platformticker.New(d) }

func loadSchedulerRuntimeConfig() (runtimeConfig, error) {
	batchSize, err := config.LoadPositiveIntEnv("SCHEDULER_BATCH_SIZE", 100)
	if err != nil {
		return runtimeConfig{}, err
	}

	tickIntervalSec, err := config.LoadPositiveIntEnv("SCHEDULER_TICK_INTERVAL_SEC", 5)
	if err != nil {
		return runtimeConfig{}, err
	}

	healthPort := os.Getenv("SCHEDULER_HEALTH_PORT")
	if healthPort == "" {
		healthPort = "6060"
	}

	return runtimeConfig{
		BatchSize:    batchSize,
		TickInterval: time.Duration(tickIntervalSec) * time.Second,
		HealthPort:   healthPort,
	}, nil
}

func runLoop(
	ctx context.Context,
	runner tickRunner,
	cfg runtimeConfig,
	newTicker func(time.Duration) schedulerTicker,
	nowFn func() time.Time,
) {
	ticker := newTicker(cfg.TickInterval)
	defer ticker.Stop()

	for {
		start := time.Now()
		now := start.UTC()
		handled, err := runner.RunBatch(ctx, now, cfg.BatchSize)
		metrics.SchedulerTickDuration.Observe(time.Since(start).Seconds())

		if err != nil {
			metrics.SchedulerCronErrors.Inc()
			slog.Error("scheduler tick failed", "error", err.Error())
		} else if handled > 0 {
			metrics.SchedulerInstancesCreated.Add(float64(handled))
			slog.Info("scheduler tick completed", "handled_due_jobs", handled)
		}

		select {
		case <-ctx.Done():
			slog.Info("scheduler draining, running final tick")
			drainStart := time.Now()
			drainCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			now := nowFn().UTC()
			if handled, err := runner.RunBatch(drainCtx, now, cfg.BatchSize); err != nil {
				metrics.SchedulerCronErrors.Inc()
				slog.Error("scheduler drain tick failed", "error", err.Error())
			} else {
				metrics.SchedulerTickDuration.Observe(time.Since(drainStart).Seconds())
				metrics.SchedulerInstancesCreated.Add(float64(handled))
				slog.Info("scheduler drain tick completed", "handled_due_jobs", handled)
			}
			cancel()
			return
		case <-ticker.Chan():
		}
	}
}

func run(ctx context.Context) error {
	if err := loadDotenvFn(); err != nil {
		return err
	}

	slog.SetDefault(newLoggerFn(os.Getenv("APP_ENV")))

	dsn := os.Getenv("SCHEDULER_DSN")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_DSN")
	}
	if dsn == "" {
		return fmt.Errorf("DATABASE_DSN is required")
	}

	cfg, err := loadSchedulerRuntimeConfig()
	if err != nil {
		return err
	}

	db, err := openDBFn(dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	pingCtx, cancel := context.WithTimeout(ctx, startupDBPingTimeout)
	defer cancel()
	if err := pingDBFn(pingCtx, db); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	// Health HTTP server
	healthCtx, healthCancel := context.WithCancel(context.Background())
	defer healthCancel()
	go health.StartComponentHealthServer(healthCtx, db, cfg.HealthPort, "scheduler")

	runner := buildRunnerFn(db)
	runLoopFn(ctx, runner, cfg, newWallClockTicker, time.Now)

	return nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}
