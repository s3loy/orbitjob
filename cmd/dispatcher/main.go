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
	"strconv"
	"syscall"
	"time"

	adminpostgres "orbitjob/internal/admin/store/postgres"
	"orbitjob/internal/core/app/dispatch"
	domaininstance "orbitjob/internal/core/domain/instance"
	corepostgres "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/platform/config"
	platformlogger "orbitjob/internal/platform/logger"
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
		repo := corepostgres.NewDispatchRepository(db)
		return dispatch.NewTickUseCase(repo)
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

func loadDispatcherRuntimeConfig() (runtimeConfig, error) {
	tenantID := os.Getenv("DISPATCHER_TENANT_ID")
	if tenantID == "" {
		tenantID = "default"
	}

	batchSize, err := loadPositiveIntEnv("DISPATCHER_BATCH_SIZE", 50)
	if err != nil {
		return runtimeConfig{}, err
	}

	tickIntervalSec, err := loadPositiveIntEnv("DISPATCHER_TICK_INTERVAL_SEC", 2)
	if err != nil {
		return runtimeConfig{}, err
	}

	leaseDurationSec, err := loadPositiveIntEnv("DISPATCHER_LEASE_DURATION_SEC", 30)
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
		spec := domaininstance.ClaimSpec{
			TenantID:       cfg.TenantID,
			LeaseExpiresAt: now.Add(cfg.LeaseDuration),
			Now:            now,
		}
		handled, err := runner.RunBatch(ctx, spec, cfg.BatchSize)
		if err != nil {
			slog.Error("dispatcher tick failed", "error", err.Error())
		} else {
			slog.Info("dispatcher tick completed", "dispatched", handled)
		}

		select {
		case <-ctx.Done():
			slog.Info("dispatcher draining, running final tick")
			drainCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			now := nowFn().UTC()
			spec := domaininstance.ClaimSpec{
				TenantID:       cfg.TenantID,
				LeaseExpiresAt: now.Add(cfg.LeaseDuration),
				Now:            now,
			}
			if handled, err := runner.RunBatch(drainCtx, spec, cfg.BatchSize); err != nil {
				slog.Error("dispatcher drain tick failed", "error", err.Error())
			} else {
				slog.Info("dispatcher drain tick completed", "dispatched", handled)
			}
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
	db.SetMaxOpenConns(25)
	defer func() { _ = db.Close() }()

	pingCtx, cancel := context.WithTimeout(ctx, startupDBPingTimeout)
	defer cancel()
	if err := pingDBFn(pingCtx, db); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	// Health HTTP server
	healthCtx, healthCancel := context.WithCancel(context.Background())
	defer healthCancel()
	go startComponentHealthServer(healthCtx, db, cfg.HealthPort, "dispatcher")

	runner := buildRunnerFn(db)
	runLoopFn(ctx, runner, cfg, newWallClockTicker, time.Now)

	return nil
}

func startComponentHealthServer(ctx context.Context, db *sql.DB, port, component string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := db.PingContext(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("db ping failed"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})
	srv := &http.Server{Addr: ":" + port, Handler: mux}
	go func() {
		slog.Info(component+" health server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			slog.Error(component+" health server error", "error", err)
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}
