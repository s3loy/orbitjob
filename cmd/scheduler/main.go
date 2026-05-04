package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
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

type tickRunner interface {
	RunBatch(ctx context.Context, now time.Time, limit int) (int, error)
}

type schedulerTicker interface {
	Chan() <-chan time.Time
	Stop()
}

type wallClockTicker struct {
	t *time.Ticker
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

func (w wallClockTicker) Chan() <-chan time.Time {
	return w.t.C
}

func (w wallClockTicker) Stop() {
	w.t.Stop()
}

func newWallClockTicker(interval time.Duration) schedulerTicker {
	return wallClockTicker{t: time.NewTicker(interval)}
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
		now := nowFn().UTC()
		handled, err := runner.RunBatch(ctx, now, cfg.BatchSize)
		if err != nil {
			slog.Error("scheduler tick failed", "error", err.Error())
		} else {
			slog.Info("scheduler tick completed", "handled_due_jobs", handled)
		}

		select {
		case <-ctx.Done():
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
	db.SetMaxOpenConns(25)
	defer func() {
		_ = db.Close()
	}()

	pingCtx, cancel := context.WithTimeout(ctx, startupDBPingTimeout)
	defer cancel()
	if err := pingDBFn(pingCtx, db); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

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
