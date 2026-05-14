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
	"strings"
	"sync"
	"syscall"
	"time"

	adminpostgres "orbitjob/internal/admin/store/postgres"
	"orbitjob/internal/core/app/dispatch"
	domaininstance "orbitjob/internal/core/domain/instance"
	corepostgres "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/platform/config"
	"orbitjob/internal/platform/election"
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
	QuickTick(ctx context.Context, spec domaininstance.ClaimSpec, limit int) (int, error)
	RunHousekeeping(ctx context.Context, now time.Time) error
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
	buildElectionFn = func(cfg election.EtcdConfig) (election.Coordinator, error) {
		return election.NewEtcd(cfg)
	}
	runLoopFn          = runLoop
	newEventListenerFn = corepostgres.NewEventListener
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

func quickTick(ctx context.Context, runner tickRunner, cfg *runtimeConfig, now time.Time, ids []string, coord election.Coordinator) int {
	var total int
	for _, tid := range ids {
		lockName := tenantLockName(tid)
		handled, err := withTenantLock(ctx, coord, lockName, func() (int, error) {
			spec := domaininstance.ClaimSpec{
				TenantID:       tid,
				LeaseExpiresAt: now.Add(cfg.LeaseDuration),
				Now:            now,
			}
			return runner.QuickTick(ctx, spec, cfg.BatchSize)
		})
		if err != nil {
			slog.Error("dispatcher quick tick failed", "tenant_id", tid, "error", err.Error())
			continue
		}
		total += handled
	}
	return total
}

func fullTick(ctx context.Context, runner tickRunner, cfg *runtimeConfig, now time.Time, ids []string, coord election.Coordinator) int {
	if err := runner.RunHousekeeping(ctx, now); err != nil {
		slog.Error("dispatcher housekeeping failed", "error", err.Error())
	}

	var total int
	tickStart := time.Now()
	for _, tid := range ids {
		lockName := tenantLockName(tid)
		handled, err := withTenantLock(ctx, coord, lockName, func() (int, error) {
			spec := domaininstance.ClaimSpec{
				TenantID:       tid,
				LeaseExpiresAt: now.Add(cfg.LeaseDuration),
				Now:            now,
			}
			return runner.QuickTick(ctx, spec, cfg.BatchSize)
		})
		if err != nil {
			slog.Error("dispatcher full tick failed", "tenant_id", tid, "error", err.Error())
			continue
		}
		total += handled
	}
	metrics.DispatcherTickDuration.WithLabelValues(cfg.TenantID).Observe(time.Since(tickStart).Seconds())
	return total
}

// tenantLockName returns the etcd lock key for a tenant.
func tenantLockName(tenantID string) string {
	return "/orbitjob/tenants/" + tenantID + "/dispatcher/lock/housekeeping"
}

// withTenantLock executes fn under an optional etcd distributed lock.
// When coord is nil (PG-only mode), fn runs directly.
// When the lock is held by another dispatcher, ErrLocked is swallowed
// and (0, nil) is returned so the tenant is skipped gracefully.
func withTenantLock(ctx context.Context, coord election.Coordinator, lockName string, fn func() (int, error)) (int, error) {
	if coord == nil {
		return fn()
	}
	unlock, err := coord.TryLock(ctx, lockName)
	if err != nil {
		if err == election.ErrLocked {
			metrics.DispatcherLockContentionTotal.WithLabelValues(lockName).Inc()
			slog.Debug("dispatcher lock held by another instance", "lock", lockName)
			return 0, nil
		}
		return 0, err
	}
	defer func() {
		if uerr := unlock(); uerr != nil {
			slog.Error("dispatcher unlock failed", "lock", lockName, "error", uerr.Error())
		}
	}()
	return fn()
}

func runLoop(
	ctx context.Context,
	runner tickRunner,
	cfg *runtimeConfig,
	eventCh <-chan struct{},
	newTicker func(time.Duration) schedulerTicker,
	nowFn func() time.Time,
	coord election.Coordinator,
) {
	ticker := newTicker(cfg.TickInterval)
	defer ticker.Stop()

	idleCount := 0
	const idleThreshold = 3
	const longInterval = 30 * time.Second
	const tenantCacheTTL = 30 * time.Second
	isLongInterval := false

	var cachedTenantIDs []string
	var tenantCacheExpires time.Time

	getTenantIDs := func() []string {
		if cfg.TenantID != "" {
			return []string{cfg.TenantID}
		}
		if time.Now().Before(tenantCacheExpires) && len(cachedTenantIDs) > 0 {
			return cachedTenantIDs
		}
		ids, err := runner.ListActiveTenantIDs(ctx)
		if err != nil {
			slog.Error("list active tenant ids failed, falling back to default", "error", err.Error())
			return []string{"default"}
		}
		if len(ids) == 0 {
			ids = []string{"default"}
		}
		cachedTenantIDs = ids
		tenantCacheExpires = time.Now().Add(tenantCacheTTL)
		return ids
	}

	resetToShortInterval := func() {
		if isLongInterval {
			ticker.Stop()
			ticker = newTicker(cfg.TickInterval)
			isLongInterval = false
			slog.Info("dispatcher switching back to short interval", "interval_sec", cfg.TickInterval.Seconds())
		}
	}

	currentTickInterval := cfg.TickInterval

	for {
		// Hot reload: recreate ticker if interval changed.
		if cfg.TickInterval != currentTickInterval {
			ticker.Stop()
			ticker = newTicker(cfg.TickInterval)
			currentTickInterval = cfg.TickInterval
			isLongInterval = false
			slog.Info("dispatcher tick interval reloaded", "interval_sec", cfg.TickInterval.Seconds())
		}

		now := nowFn().UTC()
		ids := getTenantIDs()

		select {
		case <-eventCh:
			handled := quickTick(ctx, runner, cfg, now, ids, coord)
			if handled > 0 {
				slog.Info("dispatcher quick tick completed", "dispatched", handled)
				idleCount = 0
				resetToShortInterval()
			}
			metrics.DispatcherEventWakeTotal.WithLabelValues(cfg.TenantID).Inc()
			continue

		case <-ticker.Chan():
			handled := fullTick(ctx, runner, cfg, now, ids, coord)
			if handled > 0 {
				slog.Info("dispatcher full tick completed", "dispatched", handled)
				idleCount = 0
				resetToShortInterval()
			} else {
				idleCount++
				metrics.DispatcherIdleTicksTotal.WithLabelValues(cfg.TenantID).Inc()
				if idleCount >= idleThreshold {
					ticker.Stop()
					ticker = newTicker(longInterval)
					isLongInterval = true
					metrics.DispatcherLongIntervalTotal.WithLabelValues(cfg.TenantID).Inc()
					slog.Info("dispatcher switching to long interval", "interval_sec", longInterval.Seconds())
				}
			}

		case <-ctx.Done():
			slog.Info("dispatcher draining, running final tick")
			drainCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			now := nowFn().UTC()
			if handled := fullTick(drainCtx, runner, cfg, now, ids, coord); handled > 0 {
				slog.Info("dispatcher drain tick completed", "dispatched", handled)
			}
			cancel()
			return
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

	var eventCh <-chan struct{}
	var listener *corepostgres.EventListener
	var listenerMu sync.Mutex

	tryStartListener := func() bool {
		listenerMu.Lock()
		defer listenerMu.Unlock()
		if listener != nil {
			return true
		}
		el, err := newEventListenerFn(dsn)
		if err != nil {
			return false
		}
		listener = el
		eventCh = el.C()
		return true
	}

	if !tryStartListener() {
		slog.Error("failed to start event listener, falling back to polling only")
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if tryStartListener() {
						slog.Info("event listener started after retry")
						return
					}
				}
			}
		}()
	}

	defer func() {
		listenerMu.Lock()
		if listener != nil {
			_ = listener.Close()
		}
		listenerMu.Unlock()
	}()

	// Optional: etcd distributed lock for per-tenant dispatch isolation.
	var coord election.Coordinator
	if os.Getenv("ETCD_ENABLED") == "true" {
		ep := os.Getenv("ETCD_ENDPOINTS")
		if ep == "" {
			return fmt.Errorf("ETCD_ENABLED=true but ETCD_ENDPOINTS is empty")
		}
		c, err := buildElectionFn(election.EtcdConfig{
			Endpoints: strings.Split(ep, ","),
		})
		if err != nil {
			return fmt.Errorf("init election: %w", err)
		}
		defer func() { _ = c.Close() }()
		coord = c
		slog.Info("dispatcher etcd coordination enabled")

		// Start config watcher for hot reload.
		watcher, err := config.NewEtcdWatcher(strings.Split(ep, ","))
		if err != nil {
			slog.Warn("dispatcher config watcher failed to start", "error", err.Error())
		} else {
			defer func() { _ = watcher.Close() }()
			go watchDispatcherConfig(ctx, watcher, &cfg)
		}
	}

	runLoopFn(ctx, runner, &cfg, eventCh, newWallClockTicker, time.Now, coord)

	return nil
}

func watchDispatcherConfig(ctx context.Context, watcher config.Watcher, cfg *runtimeConfig) {
	go func() {
		_ = watcher.Watch(ctx, "/orbitjob/config/dispatcher/batch_size", func(v string) {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				cfg.BatchSize = n
				slog.Info("dispatcher config updated", "key", "batch_size", "value", n)
			}
		})
	}()
	go func() {
		_ = watcher.Watch(ctx, "/orbitjob/config/dispatcher/tick_interval_sec", func(v string) {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				cfg.TickInterval = time.Duration(n) * time.Second
				slog.Info("dispatcher config updated", "key", "tick_interval_sec", "value", n)
			}
		})
	}()
	_ = watcher.Watch(ctx, "/orbitjob/config/dispatcher/lease_duration_sec", func(v string) {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.LeaseDuration = time.Duration(n) * time.Second
			slog.Info("dispatcher config updated", "key", "lease_duration_sec", "value", n)
		}
	})
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}
