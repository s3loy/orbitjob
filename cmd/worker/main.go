package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/google/uuid"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	adminpostgres "orbitjob/internal/admin/store/postgres"
	"orbitjob/internal/core/app/checkexecute"
	"orbitjob/internal/core/app/evaluate"
	"orbitjob/internal/core/app/execute"
	"orbitjob/internal/core/app/execute/handler"
	containerhandler "orbitjob/internal/core/app/execute/handler/container"
	"orbitjob/internal/core/app/sloevaluate"
	domainworker "orbitjob/internal/core/domain/worker"
	corepostgres "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/platform/config"
	"orbitjob/internal/platform/discovery"
	"orbitjob/internal/platform/health"
	platformlogger "orbitjob/internal/platform/logger"
	"orbitjob/internal/platform/metrics"
	platformticker "orbitjob/internal/platform/ticker"
)

type runtimeConfig struct {
	TenantID                 string
	MultiTenant              bool
	WorkerID                 string
	HealthPort               string
	pollInterval             atomic.Int64
	heartbeatInterval        atomic.Int64
	leaseDuration            atomic.Int64
	capacity                 atomic.Int64
	CapacityMax              int
	LeaseMin                 time.Duration
	LeaseMax                 time.Duration
	LeaseDecay               float64
	Labels                   map[string]any
	ContainerEnabled         bool
	ContainerNamespace       string
	ContainerPollInterval    time.Duration
	AdaptiveCapacityEnabled  bool

	tenantLister tenantLister
}

// Hot-reloaded fields (pollInterval/heartbeatInterval/leaseDuration/capacity)
// are atomic: the etcd config watcher writes them from a goroutine while
// runLoop, heartbeatLoop, the check worker, and sendHeartbeat read them.
// The remaining fields are set once at load and never mutated.
func (c *runtimeConfig) PollInterval() time.Duration {
	return time.Duration(c.pollInterval.Load())
}
func (c *runtimeConfig) HeartbeatInterval() time.Duration {
	return time.Duration(c.heartbeatInterval.Load())
}
func (c *runtimeConfig) LeaseDuration() time.Duration {
	return time.Duration(c.leaseDuration.Load())
}
func (c *runtimeConfig) Capacity() int                        { return int(c.capacity.Load()) }
func (c *runtimeConfig) SetPollInterval(d time.Duration)      { c.pollInterval.Store(int64(d)) }
func (c *runtimeConfig) SetHeartbeatInterval(d time.Duration) { c.heartbeatInterval.Store(int64(d)) }
func (c *runtimeConfig) SetLeaseDuration(d time.Duration)     { c.leaseDuration.Store(int64(d)) }
func (c *runtimeConfig) SetCapacity(n int)                    { c.capacity.Store(int64(n)) }

type tenantLister interface {
	ListActiveTenantIDs(ctx context.Context) ([]string, error)
}

type tickRunner interface {
	SubmitNext(ctx context.Context, pool *execute.WorkerPool, tenantID, workerID string, limit int, leaseDuration time.Duration, labels map[string]any) (int, error)
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
	loadDotenvFn      = config.LoadDotenv
	newLoggerFn       = platformlogger.New
	openDBFn          = adminpostgres.Open
	pingDBFn          = func(ctx context.Context, db *sql.DB) error { return db.PingContext(ctx) }
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
	buildRunnerFn = func(db *sql.DB, httpClient *http.Client, cfg *runtimeConfig, k8sClient kubernetes.Interface) tickRunner {
		repo := corepostgres.NewExecutorRepository(db)
		handlers := map[string]execute.Handler{
			"exec":      &handler.Exec{},
			"http":      handler.NewHTTP(httpClient),
			"webhook":   handler.NewWebhook(httpClient),
			"pg_notify": handler.NewPGNotify(db),
		}
		if cfg.ContainerEnabled {
			handlers["container"] = containerhandler.New(containerhandler.Config{
				Client:           k8sClient,
				Namespace:        cfg.ContainerNamespace,
				RequireDigest:    os.Getenv("APP_ENV") == "production",
				TTLAfterFinished: 10 * time.Minute,
				PollInterval:     cfg.ContainerPollInterval,
			})
		}
		maps.Copy(handlers, handler.GetRegistered())
		return execute.NewTickUseCase(repo, handlers)
	}
	inClusterConfigFn     = rest.InClusterConfig
	newKubernetesClientFn = func(cfg *rest.Config) (kubernetes.Interface, error) {
		return kubernetes.NewForConfig(cfg)
	}
	buildHeartbeaterFn = func(db *sql.DB) heartbeater {
		return corepostgres.NewWorkerRepository(db)
	}
	buildRegistryFn = func(endpoints []string) (discovery.Registry, error) {
		return discovery.NewEtcd(endpoints)
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
	cfg          *runtimeConfig
}

func (r *adaptiveTickRunner) SubmitNext(
	ctx context.Context,
	pool *execute.WorkerPool,
	tenantID, workerID string,
	limit int, leaseDuration time.Duration,
	labels map[string]any,
) (int, error) {
	// Probe RTT.
	probeStart := time.Now()
	if err := r.db.PingContext(ctx); err != nil {
		slog.Warn("worker db probe failed", "error", err.Error())
	}
	probeRtt := time.Since(probeStart)
	metrics.WorkerProbeRTTSeconds.WithLabelValues(workerID, tenantID).Observe(probeRtt.Seconds())

	// Gather signals for adaptive capacity.
	var queueDepth, activeWorkers int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM job_instances WHERE tenant_id = $1 AND status = 'dispatched'`, tenantID).Scan(&queueDepth); err != nil {
		slog.Warn("worker queue depth query failed", "error", err.Error())
	}
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workers WHERE tenant_id = $1 AND status = 'online'`, tenantID).Scan(&activeWorkers); err != nil {
		slog.Warn("worker active workers query failed", "error", err.Error())
	}

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

	return r.inner.SubmitNext(ctx, pool, tenantID, workerID, currentLimit, currentLease, labels)
}

func loadWorkerRuntimeConfig() (*runtimeConfig, error) {
	workerID := strings.TrimSpace(os.Getenv("WORKER_ID"))
	if workerID == "" {
		hostname, _ := os.Hostname()
		workerID = hostname + "-" + shortUUID()
	}

	tenantID := os.Getenv("WORKER_TENANT_ID")
	multiTenant := tenantID == ""
	if tenantID == "" {
		tenantID = ""
	}

	pollIntervalSec, err := loadPositiveIntEnv("WORKER_POLL_INTERVAL_SEC", 2)
	if err != nil {
		return nil, err
	}

	heartbeatIntervalSec, err := loadPositiveIntEnv("WORKER_HEARTBEAT_INTERVAL_SEC", 10)
	if err != nil {
		return nil, err
	}

	leaseDurationSec, err := loadPositiveIntEnv("WORKER_LEASE_DURATION_SEC", 60)
	if err != nil {
		return nil, err
	}

	capacity, err := loadPositiveIntEnv("WORKER_CAPACITY", 5)
	if err != nil {
		return nil, err
	}

	capacityMax, err := loadPositiveIntEnv("WORKER_CAPACITY_MAX", 20)
	if err != nil {
		return nil, err
	}
	if capacityMax < capacity {
		capacityMax = capacity
	}

	leaseMinSec, err := loadPositiveIntEnv("WORKER_LEASE_MIN_SEC", 10)
	if err != nil {
		return nil, err
	}

	leaseMaxSec, err := loadPositiveIntEnv("WORKER_LEASE_DURATION_MAX", 300)
	if err != nil {
		return nil, err
	}

	leaseDecay, err := loadFloatEnv("WORKER_LEASE_EMA_DECAY", 0.1)
	if err != nil {
		return nil, err
	}

	labels, err := loadJSONMapEnv("WORKER_LABELS")
	if err != nil {
		return nil, err
	}

	containerEnabledRaw := os.Getenv("WORKER_CONTAINER_ENABLED")
	if containerEnabledRaw == "" {
		containerEnabledRaw = "false"
	}
	containerEnabled, err := strconv.ParseBool(containerEnabledRaw)
	if err != nil {
		return nil, fmt.Errorf("WORKER_CONTAINER_ENABLED must be a boolean: %w", err)
	}
	containerNamespace := strings.TrimSpace(os.Getenv("WORKER_CONTAINER_NAMESPACE"))
	containerPollIntervalMs, err := loadPositiveIntEnv("WORKER_CONTAINER_POLL_INTERVAL_MS", 1000)
	if err != nil {
		return nil, err
	}
	if containerEnabled {
		if containerNamespace == "" {
			return nil, fmt.Errorf("WORKER_CONTAINER_NAMESPACE is required when container execution is enabled")
		}
		labels["handler:container"] = "container"
	}

	adaptiveEnabledRaw := os.Getenv("WORKER_ADAPTIVE_CAPACITY_ENABLED")
	if adaptiveEnabledRaw == "" {
		adaptiveEnabledRaw = "true"
	}
	adaptiveCapacityEnabled, err := strconv.ParseBool(adaptiveEnabledRaw)
	if err != nil {
		return nil, fmt.Errorf("WORKER_ADAPTIVE_CAPACITY_ENABLED must be a boolean: %w", err)
	}

	healthPort := os.Getenv("WORKER_HEALTH_PORT")
	if healthPort == "" {
		healthPort = "6062"
	}

	cfg := &runtimeConfig{
		TenantID:                tenantID,
		MultiTenant:             multiTenant,
		WorkerID:                workerID,
		HealthPort:              healthPort,
		CapacityMax:             capacityMax,
		LeaseMin:                time.Duration(leaseMinSec) * time.Second,
		LeaseMax:                time.Duration(leaseMaxSec) * time.Second,
		LeaseDecay:              leaseDecay,
		Labels:                  labels,
		ContainerEnabled:        containerEnabled,
		ContainerNamespace:      containerNamespace,
		ContainerPollInterval:   time.Duration(containerPollIntervalMs) * time.Millisecond,
		AdaptiveCapacityEnabled: adaptiveCapacityEnabled,
	}
	cfg.SetPollInterval(time.Duration(pollIntervalSec) * time.Second)
	cfg.SetHeartbeatInterval(time.Duration(heartbeatIntervalSec) * time.Second)
	cfg.SetLeaseDuration(time.Duration(leaseDurationSec) * time.Second)
	cfg.SetCapacity(capacity)
	return cfg, nil
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

func tenantIDsForWorker(ctx context.Context, cfg *runtimeConfig) []string {
	if !cfg.MultiTenant {
		return []string{cfg.TenantID}
	}
	if cfg.tenantLister == nil {
		return nil
	}
	ids, err := cfg.tenantLister.ListActiveTenantIDs(ctx)
	if err != nil {
		slog.Error("list active tenant ids failed", "error", err.Error())
		return nil
	}
	return ids
}

func runLoop(
	ctx context.Context,
	runner tickRunner,
	hb heartbeater,
	cfg *runtimeConfig,
	newTicker func(time.Duration) workerTicker,
	nowFn func() time.Time,
) {
	pool := execute.NewWorkerPool(cfg.CapacityMax)

	loopDone := make(chan struct{})
	go heartbeatLoop(ctx, loopDone, hb, cfg, newTicker, nowFn)

	ticker := newTicker(cfg.PollInterval())
	defer ticker.Stop()

	idleCount := 0
	const idleThreshold = 3
	const longInterval = 30 * time.Second
	isLongInterval := false

	resetToShortInterval := func() {
		if isLongInterval {
			ticker.Stop()
			ticker = newTicker(cfg.PollInterval())
			isLongInterval = false
			metrics.WorkerIntervalMode.WithLabelValues(cfg.WorkerID, cfg.TenantID).Set(0)
			slog.Info("worker switching back to short interval", "interval_sec", cfg.PollInterval().Seconds())
		}
	}

	currentPollInterval := cfg.PollInterval()

	for {
		// Hot reload: recreate ticker if poll interval changed.
		if cfg.PollInterval() != currentPollInterval {
			ticker.Stop()
			ticker = newTicker(cfg.PollInterval())
			currentPollInterval = cfg.PollInterval()
			isLongInterval = false
			slog.Info("worker poll interval reloaded", "interval_sec", cfg.PollInterval().Seconds())
		}

		ids := tenantIDsForWorker(ctx, cfg)
		handled := 0
		for _, tenantID := range ids {
			n, err := runner.SubmitNext(ctx, pool, tenantID, cfg.WorkerID, cfg.Capacity(), cfg.LeaseDuration(), cfg.Labels)
			if err != nil {
				slog.Error("worker tick failed", "tenant_id", tenantID, "error", err.Error())
			} else {
				handled += n
			}
		}
		if handled > 0 {
			slog.Info("worker submitted tasks", "handled", handled)
			metrics.WorkerPoolSubmittedTotal.WithLabelValues(cfg.WorkerID, cfg.TenantID).Add(float64(handled))
			idleCount = 0
			resetToShortInterval()
			select {
			case <-ctx.Done():
				close(loopDone)
				pool.Stop()
				slog.Info("worker drain complete, shutting down")
				return
			default:
				continue
			}
		}

		metrics.WorkerPoolActiveTasks.WithLabelValues(cfg.WorkerID, cfg.TenantID).Set(float64(pool.Active()))

		select {
		case <-ctx.Done():
			close(loopDone)
			pool.Stop()
			slog.Info("worker drain complete, shutting down")
			return
		case <-ticker.Chan():
			idleCount++
			metrics.WorkerIdleTicksTotal.WithLabelValues(cfg.WorkerID, cfg.TenantID).Inc()
			if idleCount >= idleThreshold {
				ticker.Stop()
				ticker = newTicker(longInterval)
				isLongInterval = true
				metrics.WorkerIntervalMode.WithLabelValues(cfg.WorkerID, cfg.TenantID).Set(1)
				slog.Info("worker switching to long interval", "interval_sec", longInterval.Seconds())
			}
		}
	}
}

const shutdownDeadline = 45 * time.Second

func heartbeatLoop(
	ctx context.Context,
	loopDone <-chan struct{},
	hb heartbeater,
	cfg *runtimeConfig,
	newTicker func(time.Duration) workerTicker,
	nowFn func() time.Time,
) {
	sendHeartbeats(ctx, hb, cfg, nowFn, domainworker.StatusOnline)

	ticker := newTicker(cfg.HeartbeatInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			drainCtx, drainCancel := context.WithTimeout(context.Background(), 3*time.Second)
			sendHeartbeats(drainCtx, hb, cfg, nowFn, domainworker.StatusDraining)
			drainCancel()

			select {
			case <-loopDone:
			case <-time.After(shutdownDeadline):
				slog.Warn("shutdown deadline exceeded, forcing offline")
			}

			offCtx, offCancel := context.WithTimeout(context.Background(), 3*time.Second)
			sendHeartbeats(offCtx, hb, cfg, nowFn, domainworker.StatusOffline)
			offCancel()
			return
		case <-ticker.Chan():
			sendHeartbeats(ctx, hb, cfg, nowFn, domainworker.StatusOnline)
		}
	}
}

func sendHeartbeats(
	ctx context.Context,
	hb heartbeater,
	cfg *runtimeConfig,
	nowFn func() time.Time,
	status string,
) {
	for _, tenantID := range tenantIDsForWorker(ctx, cfg) {
		sendHeartbeat(ctx, hb, cfg, nowFn, tenantID, status)
	}
}

func sendHeartbeat(
	ctx context.Context,
	hb heartbeater,
	cfg *runtimeConfig,
	nowFn func() time.Time,
	tenantID string,
	status string,
) {
	now := nowFn()
	spec, err := domainworker.NormalizeHeartbeat(now, domainworker.HeartbeatInput{
		TenantID:       tenantID,
		WorkerID:       cfg.WorkerID,
		Status:         status,
		LeaseExpiresAt: now.Add(cfg.LeaseDuration()),
		Capacity:       cfg.Capacity(),
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

	dsn, _, err := config.ResolveDatabaseDSN("RUNTIME_DSN", "WORKER_DSN", "DATABASE_DSN")
	if err != nil {
		return err
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
	var k8sClient kubernetes.Interface
	if cfg.ContainerEnabled {
		kubeConfig, err := inClusterConfigFn()
		if err != nil {
			return fmt.Errorf("load in-cluster Kubernetes config: %w", err)
		}
		k8sClient, err = newKubernetesClientFn(kubeConfig)
		if err != nil {
			return fmt.Errorf("create Kubernetes client: %w", err)
		}
	}
	inner := buildRunnerFn(db, httpClient, cfg, k8sClient)
	hb := buildHeartbeaterFn(db)
	cfg.tenantLister = corepostgres.NewExecutorRepository(db)

	// Attach dynamic lease to the inner runner so execution durations are recorded.
	dynamicLease := execute.NewDynamicLease(cfg.LeaseMin, cfg.LeaseMax, cfg.LeaseDecay)
	if uc, ok := inner.(*execute.TickUseCase); ok {
		uc.SetDynamicLease(dynamicLease)
	}

	// Wrap with adaptive capacity unless qualification/load-test mode requests a
	// fixed capacity. Adaptive capacity starts conservative and can take minutes
	// to ramp, which prevents sustained high-throughput runs from reaching the
	// configured WORKER_CAPACITY_MAX.
	var adaptiveCap *execute.AdaptiveCapacity
	if cfg.AdaptiveCapacityEnabled {
		adaptiveCap = execute.NewAdaptiveCapacity(cfg.CapacityMax, 3)
	}
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
		"poll_interval", cfg.PollInterval(),
		"lease_duration", cfg.LeaseDuration(),
		"capacity", cfg.Capacity(),
		"capacity_max", cfg.CapacityMax,
	)

	// Initialize check worker.
	checkRepo := corepostgres.NewCheckRepository(db)
	checkRunRepo := corepostgres.NewCheckRunRepository(db)
	// Wire SLI recorder for automatic SLO tracking.
	sliRepo := corepostgres.NewSLIRepository(db)
	snapshotRepo := corepostgres.NewSLISnapshotRepository(db)
	sliRecorder := sloevaluate.NewCheckRunRecorder(sliRepo, snapshotRepo)
	checkWorker := checkexecute.NewTickUseCase(checkRepo, checkRunRepo, evaluate.NewEvaluator(), sliRecorder)
	go func() {
		ticker := time.NewTicker(cfg.PollInterval())
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				for _, tenantID := range tenantIDsForWorker(ctx, cfg) {
					if _, err := checkWorker.RunBatch(ctx, tenantID, cfg.Capacity()); err != nil {
						slog.Error("check worker tick failed", "tenant_id", tenantID, "error", err.Error())
					}
				}
			}
		}
	}()

	// Optional: etcd service registration for worker discovery.
	var reg discovery.Registry
	if os.Getenv("ETCD_ENABLED") == "true" {
		ep := os.Getenv("ETCD_ENDPOINTS")
		if ep == "" {
			return fmt.Errorf("ETCD_ENABLED=true but ETCD_ENDPOINTS is empty")
		}
		r, err := buildRegistryFn(strings.Split(ep, ","))
		if err != nil {
			return fmt.Errorf("init registry: %w", err)
		}
		reg = r
		defer func() { _ = reg.Close() }()

		keepAlive, err := reg.Register(ctx, "worker", cfg.WorkerID, cfg.LeaseDuration())
		if err != nil {
			return fmt.Errorf("register worker: %w", err)
		}
		defer func() {
			if err := reg.Deregister(context.Background(), "worker", cfg.WorkerID); err != nil {
				slog.Error("worker deregister failed", "error", err.Error())
			}
		}()

		// Start background keepalive goroutine.
		go func() {
			ticker := time.NewTicker(cfg.HeartbeatInterval())
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := keepAlive(ctx); err != nil {
						slog.Error("worker keepalive failed", "error", err.Error())
					}
				}
			}
		}()

		// Start config watcher for hot reload.
		watcher, err := config.NewEtcdWatcher(strings.Split(ep, ","))
		if err != nil {
			slog.Warn("worker config watcher failed to start", "error", err.Error())
		} else {
			defer func() { _ = watcher.Close() }()
			go watchWorkerConfig(ctx, watcher, cfg)
		}

		slog.Info("worker etcd registration enabled", "worker_id", cfg.WorkerID)
	}

	runLoopFn(ctx, runner, hb, cfg, newWallClockTicker, time.Now)

	return nil
}

func watchWorkerConfig(ctx context.Context, watcher config.Watcher, cfg *runtimeConfig) {
	go func() {
		_ = watcher.Watch(ctx, "/orbitjob/config/worker/poll_interval_sec", func(v string) {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				cfg.SetPollInterval(time.Duration(n) * time.Second)
				slog.Info("worker config updated", "key", "poll_interval_sec", "value", n)
			}
		})
	}()
	go func() {
		_ = watcher.Watch(ctx, "/orbitjob/config/worker/heartbeat_interval_sec", func(v string) {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				cfg.SetHeartbeatInterval(time.Duration(n) * time.Second)
				slog.Info("worker config updated", "key", "heartbeat_interval_sec", "value", n)
			}
		})
	}()
	go func() {
		_ = watcher.Watch(ctx, "/orbitjob/config/worker/lease_duration_sec", func(v string) {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				cfg.SetLeaseDuration(time.Duration(n) * time.Second)
				slog.Info("worker config updated", "key", "lease_duration_sec", "value", n)
			}
		})
	}()
	go func() {
		_ = watcher.Watch(ctx, "/orbitjob/config/worker/capacity", func(v string) {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				cfg.SetCapacity(n)
				slog.Info("worker config updated", "key", "capacity", "value", n)
			}
		})
	}()
}

// shortUUID returns an 8-character hex prefix used for default worker IDs
// when WORKER_ID is unset. It is not a security primitive.
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
