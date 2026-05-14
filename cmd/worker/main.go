package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	adminpostgres "orbitjob/internal/admin/store/postgres"
	"orbitjob/internal/core/app/execute"
	"orbitjob/internal/core/app/execute/handler"
	domainworker "orbitjob/internal/core/domain/worker"
	corepostgres "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/platform/config"
	"orbitjob/internal/platform/health"
	platformlogger "orbitjob/internal/platform/logger"
	"orbitjob/internal/platform/metrics"
	platformticker "orbitjob/internal/platform/ticker"
)

type runtimeConfig struct {
	TenantID          string
	WorkerID          string
	HealthPort        string
	PollInterval      time.Duration
	HeartbeatInterval time.Duration
	LeaseDuration     time.Duration
	Capacity          int
	CapacityMax       int
	LeaseMin          time.Duration
	LeaseMax          time.Duration
	LeaseDecay        float64
	Labels            map[string]any
}

type tickRunner interface {
	RunOnce(ctx context.Context, tenantID, workerID string, limit int, leaseDuration time.Duration, labels map[string]any) (int, error)
}

type heartbeater interface {
	UpsertHeartbeat(ctx context.Context, spec domainworker.HeartbeatSpec) (domainworker.Snapshot, error)
	GetByID(ctx context.Context, tenantID, workerID string) (domainworker.Snapshot, error)
}

type workerTicker interface {
	Chan() <-chan time.Time
	Stop()
}

const startupDBPingTimeout = 5 * time.Second

var (
	loadDotenvFn  = config.LoadDotenv
	newLoggerFn   = platformlogger.New
	openDBFn      = adminpostgres.Open
	pingDBFn      = func(ctx context.Context, db *sql.DB) error { return db.PingContext(ctx) }
	buildHTTPClientFn = func() *http.Client {
		return &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
				DialContext: (&net.Dialer{
					Timeout:   5 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
			},
			Timeout: 30 * time.Second,
		}
	}
	buildRunnerFn = func(db *sql.DB, httpClient *http.Client) tickRunner {
		repo := corepostgres.NewExecutorRepository(db)
		handlers := map[string]execute.Handler{
			"exec": &handler.Exec{},
			"http": handler.NewHTTP(httpClient),
		}
		return execute.NewTickUseCase(repo, handlers)
	}
	buildHeartbeaterFn = func(db *sql.DB) heartbeater {
		return corepostgres.NewWorkerRepository(db)
	}
	runLoopFn = runLoop
)

var newWallClockTicker = func(d time.Duration) workerTicker { return platformticker.New(d) }

// adaptiveTickRunner wraps a tickRunner with adaptive capacity and dynamic lease.
type adaptiveTickRunner struct {
	inner        tickRunner
	db           *sql.DB
	adaptiveCap  *execute.AdaptiveCapacity
	dynamicLease *execute.DynamicLease
	cfg          runtimeConfig
}

func (r *adaptiveTickRunner) RunOnce(
	ctx context.Context,
	tenantID, workerID string,
	limit int, leaseDuration time.Duration,
	labels map[string]any,
) (int, error) {
	// Probe RTT.
	probeStart := time.Now()
	_ = r.db.PingContext(ctx)
	probeRtt := time.Since(probeStart)
	metrics.WorkerProbeRTTSeconds.WithLabelValues(workerID, tenantID).Observe(probeRtt.Seconds())

	// Gather signals for adaptive capacity.
	var queueDepth, activeWorkers int64
	_ = r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM job_instances WHERE tenant_id = $1 AND status = 'dispatched'`, tenantID).Scan(&queueDepth)
	_ = r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workers WHERE tenant_id = $1 AND status = 'online'`, tenantID).Scan(&activeWorkers)

	// Adaptive capacity.
	currentLimit := limit
	if r.adaptiveCap != nil {
		currentLimit = r.adaptiveCap.Update(probeRtt, queueDepth, activeWorkers, workerID, tenantID)
	}

	// Dynamic lease.
	currentLease := leaseDuration
	if r.dynamicLease != nil {
		currentLease = r.dynamicLease.Current()
	}

	return r.inner.RunOnce(ctx, tenantID, workerID, currentLimit, currentLease, labels)
}

func loadWorkerRuntimeConfig() (runtimeConfig, error) {
	workerID := strings.TrimSpace(os.Getenv("WORKER_ID"))
	if workerID == "" {
		hostname, _ := os.Hostname()
		workerID = hostname + "-" + shortUUID()
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

	capacityMax, err := loadPositiveIntEnv("WORKER_CAPACITY_MAX", 10)
	if err != nil {
		return runtimeConfig{}, err
	}
	if capacityMax < capacity {
		capacityMax = capacity
	}

	leaseMinSec, err := loadPositiveIntEnv("WORKER_LEASE_MIN_SEC", 10)
	if err != nil {
		return runtimeConfig{}, err
	}

	leaseMaxSec, err := loadPositiveIntEnv("WORKER_LEASE_DURATION_MAX", 300)
	if err != nil {
		return runtimeConfig{}, err
	}

	leaseDecay, err := loadFloatEnv("WORKER_LEASE_EMA_DECAY", 0.1)
	if err != nil {
		return runtimeConfig{}, err
	}

	labels, err := loadJSONMapEnv("WORKER_LABELS")
	if err != nil {
		return runtimeConfig{}, err
	}

	healthPort := os.Getenv("WORKER_HEALTH_PORT")
	if healthPort == "" {
		healthPort = "6062"
	}

	return runtimeConfig{
		TenantID:          tenantID,
		WorkerID:          workerID,
		HealthPort:        healthPort,
		PollInterval:      time.Duration(pollIntervalSec) * time.Second,
		HeartbeatInterval: time.Duration(heartbeatIntervalSec) * time.Second,
		LeaseDuration:     time.Duration(leaseDurationSec) * time.Second,
		Capacity:          capacity,
		CapacityMax:       capacityMax,
		LeaseMin:          time.Duration(leaseMinSec) * time.Second,
		LeaseMax:          time.Duration(leaseMaxSec) * time.Second,
		LeaseDecay:        leaseDecay,
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

func loadFloatEnv(key string, defaultValue float64) (float64, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return defaultValue, nil
	}

	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a float: %w", key, err)
	}
	if value <= 0 || value > 1 {
		return 0, fmt.Errorf("%s must be in (0, 1]", key)
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
	loopDone := make(chan struct{})
	go heartbeatLoop(ctx, loopDone, hb, cfg, newTicker, nowFn)

	ticker := newTicker(cfg.PollInterval)
	defer ticker.Stop()

	idleCount := 0
	const idleThreshold = 3
	const longInterval = 30 * time.Second
	isLongInterval := false

	resetToShortInterval := func() {
		if isLongInterval {
			ticker.Stop()
			ticker = newTicker(cfg.PollInterval)
			isLongInterval = false
			slog.Info("worker switching back to short interval", "interval_sec", cfg.PollInterval.Seconds())
		}
	}

	for {
		n, err := runner.RunOnce(ctx, cfg.TenantID, cfg.WorkerID, cfg.Capacity, cfg.LeaseDuration, cfg.Labels)
		if err != nil {
			slog.Error("worker tick failed", "error", err.Error())
		} else if n > 0 {
			slog.Info("worker executed task", "handled", n)
			idleCount = 0
			resetToShortInterval()
			select {
			case <-ctx.Done():
				close(loopDone)
				slog.Info("worker drain complete, shutting down")
				return
			default:
				continue
			}
		}

		select {
		case <-ctx.Done():
			close(loopDone)
			slog.Info("worker drain complete, shutting down")
			return
		case <-ticker.Chan():
			idleCount++
			metrics.WorkerIdleTicksTotal.WithLabelValues(cfg.WorkerID, cfg.TenantID).Inc()
			if idleCount >= idleThreshold {
				ticker.Stop()
				ticker = newTicker(longInterval)
				isLongInterval = true
				slog.Info("worker switching to long interval", "interval_sec", longInterval.Seconds())
			}
		}
	}
}

const shutdownDeadline = 30 * time.Second

func heartbeatLoop(
	ctx context.Context,
	loopDone <-chan struct{},
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
			drainCtx, drainCancel := context.WithTimeout(context.Background(), 3*time.Second)
			sendHeartbeat(drainCtx, hb, cfg, nowFn, domainworker.StatusDraining)
			drainCancel()

			select {
			case <-loopDone:
			case <-time.After(shutdownDeadline):
				slog.Warn("shutdown deadline exceeded, forcing offline")
			}

			offCtx, offCancel := context.WithTimeout(context.Background(), 3*time.Second)
			sendHeartbeat(offCtx, hb, cfg, nowFn, domainworker.StatusOffline)
			offCancel()
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

	dsn := os.Getenv("WORKER_DSN")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_DSN")
	}
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

	httpClient := buildHTTPClientFn()
	inner := buildRunnerFn(db, httpClient)
	hb := buildHeartbeaterFn(db)

	// Attach dynamic lease to the inner runner so execution durations are recorded.
	dynamicLease := execute.NewDynamicLease(cfg.LeaseMin, cfg.LeaseMax, cfg.LeaseDecay)
	if uc, ok := inner.(*execute.TickUseCase); ok {
		uc.SetDynamicLease(dynamicLease)
	}

	// Wrap with adaptive capacity.
	adaptiveCap := execute.NewAdaptiveCapacity(cfg.CapacityMax, 3)
	runner := &adaptiveTickRunner{
		inner:        inner,
		db:           db,
		adaptiveCap:  adaptiveCap,
		dynamicLease: dynamicLease,
		cfg:          cfg,
	}

	healthCtx, healthCancel := context.WithCancel(context.Background())
	defer healthCancel()
	go health.StartComponentHealthServer(healthCtx, db, cfg.HealthPort, "worker")

	slog.Info("worker starting",
		"worker_id", cfg.WorkerID,
		"tenant_id", cfg.TenantID,
		"health_port", cfg.HealthPort,
		"poll_interval", cfg.PollInterval,
		"lease_duration", cfg.LeaseDuration,
		"capacity", cfg.Capacity,
		"capacity_max", cfg.CapacityMax,
	)

	runLoopFn(ctx, runner, hb, cfg, newWallClockTicker, time.Now)

	return nil
}

// startComponentHealthServer runs a minimal HTTP server with /healthz and /readyz.
// Shared pattern with scheduler and dispatcher.
func shortUUID() string {
	return uuid.New().String()[:8]
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}
