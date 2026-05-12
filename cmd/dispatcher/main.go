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
	"orbitjob/internal/core/app/dispatch"
	domaininstance "orbitjob/internal/core/domain/instance"
	corepostgres "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/platform/config"
	"orbitjob/internal/platform/health"
	platformlogger "orbitjob/internal/platform/logger"
	"orbitjob/internal/platform/metrics"
	platformticker "orbitjob/internal/platform/ticker"
)

type runtimeConfig struct {
	TenantID      string
	BatchSize     int
	TickInterval  time.Duration
	LeaseDuration time.Duration
	HealthPort    string
}

type tickRunner interface {
	RunBatch(ctx context.Context, spec domaininstance.ClaimSpec, limit int) (int, error)
	ListActiveTenantIDs(ctx context.Context) ([]string, error)
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
		repo := corepostgres.NewDispatchRepository(db)
		return dispatch.NewTickUseCase(repo)
	}
	runLoopFn = runLoop
)

var newWallClockTicker = func(d time.Duration) schedulerTicker { return platformticker.New(d) }

func loadDispatcherRuntimeConfig() (runtimeConfig, error) {
	tenantID := os.Getenv("DISPATCHER_TENANT_ID")

	batchSize, err := config.LoadPositiveIntEnv("DISPATCHER_BATCH_SIZE", 50)
	if err != nil {
		return runtimeConfig{}, err
	}

	tickIntervalSec, err := config.LoadPositiveIntEnv("DISPATCHER_TICK_INTERVAL_SEC", 2)
	if err != nil {
		return runtimeConfig{}, err
	}

	leaseDurationSec, err := config.LoadPositiveIntEnv("DISPATCHER_LEASE_DURATION_SEC", 30)
	if err != nil {
		return runtimeConfig{}, err
	}

	healthPort := os.Getenv("DISPATCHER_HEALTH_PORT")
	if healthPort == "" {
		healthPort = "6061"
	}

	return runtimeConfig{
		TenantID:      tenantID,
		BatchSize:     batchSize,
		TickInterval:  time.Duration(tickIntervalSec) * time.Second,
		LeaseDuration: time.Duration(leaseDurationSec) * time.Second,
		HealthPort:    healthPort,
	}, nil
}

func tenantIDs(cfg runtimeConfig, runner tickRunner, ctx context.Context) []string {
	if cfg.TenantID != "" {
		return []string{cfg.TenantID}
	}
	ids, err := runner.ListActiveTenantIDs(ctx)
	if err != nil {
		slog.Error("list active tenant ids failed, falling back to default", "error", err.Error())
		return []string{"default"}
	}
	if len(ids) == 0 {
		return []string{"default"}
	}
	return ids
}

func dispatchTick(ctx context.Context, runner tickRunner, cfg runtimeConfig, now time.Time) int {
	var total int
	tickStart := time.Now()
	for _, tid := range tenantIDs(cfg, runner, ctx) {
		spec := domaininstance.ClaimSpec{
			TenantID:       tid,
			LeaseExpiresAt: now.Add(cfg.LeaseDuration),
			Now:            now,
		}
		handled, err := runner.RunBatch(ctx, spec, cfg.BatchSize)
		if err != nil {
			slog.Error("dispatcher tick failed", "tenant_id", tid, "error", err.Error())
			continue
		}
		total += handled
	}
	metrics.DispatcherTickDuration.WithLabelValues(cfg.TenantID).Observe(time.Since(tickStart).Seconds())
	return total
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
		handled := dispatchTick(ctx, runner, cfg, now)
		if handled > 0 {
			slog.Info("dispatcher tick completed", "dispatched", handled)
		}

		select {
		case <-ctx.Done():
			slog.Info("dispatcher draining, running final tick")
			drainCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			now := nowFn().UTC()
			if handled := dispatchTick(drainCtx, runner, cfg, now); handled > 0 {
				slog.Info("dispatcher drain tick completed", "dispatched", handled)
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

	dsn := os.Getenv("DISPATCHER_DSN")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_DSN")
	}
	if dsn == "" {
		return fmt.Errorf("DATABASE_DSN is required")
	}

	cfg, err := loadDispatcherRuntimeConfig()
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
	go health.StartComponentHealthServer(healthCtx, db, cfg.HealthPort, "dispatcher")

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
