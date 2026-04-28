package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	adminpostgres "orbitjob/internal/admin/store/postgres"
	"orbitjob/internal/core/app/execute"
	"orbitjob/internal/core/app/execute/handler"
	domainworker "orbitjob/internal/core/domain/worker"
	corepostgres "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/platform/config"
	platformlogger "orbitjob/internal/platform/logger"
)

type runtimeConfig struct {
	TenantID          string
	WorkerID          string
	PollInterval      time.Duration
	HeartbeatInterval time.Duration
	LeaseDuration     time.Duration
	Capacity          int
	Labels            map[string]any
}

type tickRunner interface {
	RunOnce(ctx context.Context, tenantID, workerID string, leaseDuration time.Duration) (int, error)
}

type heartbeater interface {
	UpsertHeartbeat(ctx context.Context, spec domainworker.HeartbeatSpec) (domainworker.Snapshot, error)
}

type workerTicker interface {
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
		repo := corepostgres.NewExecutorRepository(db)
		handlers := map[string]execute.Handler{
			"exec": &handler.ExecHandler{},
			"http": handler.NewHTTPHandler(http.DefaultClient),
		}
		return execute.NewTickUseCase(repo, handlers)
	}
	buildHeartbeaterFn = func(db *sql.DB) heartbeater {
		return corepostgres.NewWorkerRepository(db)
	}
	runLoopFn = runLoop
)

func (w wallClockTicker) Chan() <-chan time.Time { return w.t.C }
func (w wallClockTicker) Stop()                  { w.t.Stop() }

func newWallClockTicker(interval time.Duration) workerTicker {
	return wallClockTicker{t: time.NewTicker(interval)}
}

func loadWorkerRuntimeConfig() (runtimeConfig, error) {
	workerID := strings.TrimSpace(os.Getenv("WORKER_ID"))
	if workerID == "" {
		return runtimeConfig{}, fmt.Errorf("WORKER_ID is required")
	}

	tenantID := os.Getenv("WORKER_TENANT_ID")
	if tenantID == "" {
		tenantID = "default"
	}

	pollIntervalSec, err := loadPositiveIntEnv("WORKER_POLL_INTERVAL_SEC", 2)
	if err != nil {
		return runtimeConfig{}, err
	}

	heartbeatIntervalSec, err := loadPositiveIntEnv("WORKER_HEARTBEAT_INTERVAL_SEC", 10)
	if err != nil {
		return runtimeConfig{}, err
	}

	leaseDurationSec, err := loadPositiveIntEnv("WORKER_LEASE_DURATION_SEC", 60)
	if err != nil {
		return runtimeConfig{}, err
	}

	capacity, err := loadPositiveIntEnv("WORKER_CAPACITY", 1)
	if err != nil {
		return runtimeConfig{}, err
	}

	labels, err := loadJSONMapEnv("WORKER_LABELS")
	if err != nil {
		return runtimeConfig{}, err
	}

	return runtimeConfig{
		TenantID:          tenantID,
		WorkerID:          workerID,
		PollInterval:      time.Duration(pollIntervalSec) * time.Second,
		HeartbeatInterval: time.Duration(heartbeatIntervalSec) * time.Second,
		LeaseDuration:     time.Duration(leaseDurationSec) * time.Second,
		Capacity:          capacity,
		Labels:            labels,
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

func loadJSONMapEnv(key string) (map[string]any, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return map[string]any{}, nil
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, fmt.Errorf("%s must be valid JSON object: %w", key, err)
	}
	return m, nil
}

func runLoop(
	ctx context.Context,
	runner tickRunner,
	hb heartbeater,
	cfg runtimeConfig,
	newTicker func(time.Duration) workerTicker,
	nowFn func() time.Time,
) {
	go heartbeatLoop(ctx, hb, cfg, newTicker, nowFn)

	ticker := newTicker(cfg.PollInterval)
	defer ticker.Stop()

	for {
		n, err := runner.RunOnce(ctx, cfg.TenantID, cfg.WorkerID, cfg.LeaseDuration)
		if err != nil {
			slog.Error("worker tick failed", "error", err.Error())
		} else if n > 0 {
			slog.Info("worker executed task", "handled", n)
			select {
			case <-ctx.Done():
				return
			default:
				continue
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.Chan():
		}
	}
}

func heartbeatLoop(
	ctx context.Context,
	hb heartbeater,
	cfg runtimeConfig,
	newTicker func(time.Duration) workerTicker,
	nowFn func() time.Time,
) {
	sendHeartbeat(ctx, hb, cfg, nowFn, domainworker.StatusOnline)

	ticker := newTicker(cfg.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			sendHeartbeat(shutdownCtx, hb, cfg, nowFn, domainworker.StatusOffline)
			cancel()
			return
		case <-ticker.Chan():
			sendHeartbeat(ctx, hb, cfg, nowFn, domainworker.StatusOnline)
		}
	}
}

func sendHeartbeat(
	ctx context.Context,
	hb heartbeater,
	cfg runtimeConfig,
	nowFn func() time.Time,
	status string,
) {
	now := nowFn()
	spec, err := domainworker.NormalizeHeartbeat(now, domainworker.HeartbeatInput{
		TenantID:       cfg.TenantID,
		WorkerID:       cfg.WorkerID,
		Status:         status,
		LeaseExpiresAt: now.Add(cfg.LeaseDuration),
		Capacity:       cfg.Capacity,
		Labels:         cfg.Labels,
	})
	if err != nil {
		slog.Error("normalize heartbeat failed", "error", err.Error())
		return
	}
	if _, err := hb.UpsertHeartbeat(ctx, spec); err != nil {
		slog.Error("heartbeat failed", "error", err.Error())
	}
}

func run(ctx context.Context) error {
	if err := loadDotenvFn(); err != nil {
		return err
	}

	slog.SetDefault(newLoggerFn(os.Getenv("APP_ENV")))

	dsn := os.Getenv("DATABASE_DSN")
	if dsn == "" {
		return fmt.Errorf("DATABASE_DSN is required")
	}

	cfg, err := loadWorkerRuntimeConfig()
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

	runner := buildRunnerFn(db)
	hb := buildHeartbeaterFn(db)

	slog.Info("worker starting",
		"worker_id", cfg.WorkerID,
		"tenant_id", cfg.TenantID,
		"poll_interval", cfg.PollInterval,
		"lease_duration", cfg.LeaseDuration,
		"capacity", cfg.Capacity,
	)

	runLoopFn(ctx, runner, hb, cfg, newWallClockTicker, time.Now)

	return nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}
