package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	instancecommand "orbitjob/internal/admin/app/instance/command"
	instancequery "orbitjob/internal/admin/app/instance/query"
	command "orbitjob/internal/admin/app/job/command"
	query "orbitjob/internal/admin/app/job/query"
	adminhttp "orbitjob/internal/admin/http"
	"orbitjob/internal/admin/http/middleware"
	adminpostgres "orbitjob/internal/admin/store/postgres"
	"orbitjob/internal/core/app/dispatch"
	"orbitjob/internal/core/app/execute"
	"orbitjob/internal/core/app/execute/handler"
	"orbitjob/internal/core/app/schedule"
	domaininstance "orbitjob/internal/core/domain/instance"
	domainworker "orbitjob/internal/core/domain/worker"
	corepostgres "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/platform/config"
	platformlogger "orbitjob/internal/platform/logger"
)

func main() {
	if err := config.LoadDotenv(); err != nil {
		log.Fatal(err)
	}
	logger := platformlogger.New(os.Getenv("APP_ENV"))
	slog.SetDefault(logger)

	dsn := os.Getenv("DEV_DSN")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_DSN")
	}
	if dsn == "" {
		log.Fatal("DATABASE_DSN or DEV_DSN is required")
	}

	db, err := adminpostgres.Open(dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Fatal(err)
	}

	shutdownCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup

	// --- Scheduler ---
	slog.Info("starting scheduler")
	schedCfg := loadDevSchedulerConfig()
	wg.Add(1)
	go runDevScheduler(shutdownCtx, &wg, db, schedCfg)

	// --- Dispatcher ---
	slog.Info("starting dispatcher")
	dispatchCfg := loadDevDispatcherConfig()
	wg.Add(1)
	go runDevDispatcher(shutdownCtx, &wg, db, dispatchCfg)

	// --- Worker ---
	slog.Info("starting worker")
	workerCfg := loadDevWorkerConfig()
	wg.Add(1)
	go runDevWorker(shutdownCtx, &wg, db, workerCfg)

	// --- Admin API ---
	adminPort := os.Getenv("ADMIN_PORT")
	if adminPort == "" {
		adminPort = os.Getenv("PORT")
	}
	if adminPort == "" {
		adminPort = "8080"
	}
	srv := setupDevAdminServer(db)
	wg.Add(1)
	go runDevAdmin(shutdownCtx, &wg, srv, adminPort)

	slog.Info("devserver running", "components", "scheduler dispatcher worker admin", "admin_port", adminPort)

	// Wait for shutdown signal then gracefully terminate.
	<-shutdownCtx.Done()

	slog.Info("shutting down")
	stop()
	wg.Wait()
	slog.Info("devserver stopped")
}

// --- Scheduler helpers ---

type devSchedulerConfig struct {
	BatchSize    int
	TickInterval time.Duration
}

func loadDevSchedulerConfig() devSchedulerConfig {
	return devSchedulerConfig{
		BatchSize:    devPositiveInt("SCHEDULER_BATCH_SIZE_MAX", 100),
		TickInterval: time.Duration(devPositiveInt("SCHEDULER_TICK_INTERVAL_SEC", 5)) * time.Second,
	}
}

func runDevScheduler(ctx context.Context, wg *sync.WaitGroup, db *sql.DB, cfg devSchedulerConfig) {
	defer wg.Done()
	repo := corepostgres.NewSchedulerRepository(db)
	runner := schedule.NewTickUseCase(repo, corepostgres.ClassifyError)

	ticker := time.NewTicker(cfg.TickInterval)
	defer ticker.Stop()

	for {
		now := time.Now().UTC()
		counts, err := runner.RunBatch(ctx, now, cfg.BatchSize)
		if err != nil {
			slog.Error("scheduler tick failed", "error", err)
		} else {
			slog.Info("scheduler tick completed", "handled_due_jobs", counts.Handled)
		}

		select {
		case <-ctx.Done():
			slog.Info("scheduler draining, running final tick")
			drainCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			now := time.Now().UTC()
			if counts, err := runner.RunBatch(drainCtx, now, cfg.BatchSize); err != nil {
				slog.Error("scheduler drain tick failed", "error", err)
			} else {
				slog.Info("scheduler drain tick completed", "handled_due_jobs", counts.Handled)
			}
			cancel()
			return
		case <-ticker.C:
		}
	}
}

// --- Dispatcher helpers ---

type devDispatcherConfig struct {
	TenantID      string
	MultiTenant   bool
	BatchSize     int
	TickInterval  time.Duration
	LeaseDuration time.Duration
}

func loadDevDispatcherConfig() devDispatcherConfig {
	tenant := os.Getenv("DISPATCHER_TENANT_ID")
	multiTenant := tenant == ""
	return devDispatcherConfig{
		TenantID:      tenant,
		MultiTenant:   multiTenant,
		BatchSize:     devPositiveInt("DISPATCHER_BATCH_SIZE", 50),
		TickInterval:  time.Duration(devPositiveInt("DISPATCHER_TICK_INTERVAL_SEC", 2)) * time.Second,
		LeaseDuration: time.Duration(devPositiveInt("DISPATCHER_LEASE_DURATION_SEC", 30)) * time.Second,
	}
}

func runDevDispatcher(ctx context.Context, wg *sync.WaitGroup, db *sql.DB, cfg devDispatcherConfig) {
	defer wg.Done()
	repo := corepostgres.NewDispatchRepository(db)
	runner := dispatch.NewTickUseCase(repo)

	ticker := time.NewTicker(cfg.TickInterval)
	defer ticker.Stop()

	for {
		now := time.Now().UTC()
		ids := devTenantIDs(ctx, cfg.MultiTenant, cfg.TenantID, repo)
		for _, tenantID := range ids {
			spec := domaininstance.ClaimSpec{
				TenantID:       tenantID,
				LeaseExpiresAt: now.Add(cfg.LeaseDuration),
				Now:            now,
			}
			handled, err := runner.RunBatch(ctx, spec, cfg.BatchSize)
			if err != nil {
				slog.Error("dispatcher tick failed", "tenant_id", tenantID, "error", err)
			} else {
				slog.Info("dispatcher tick completed", "tenant_id", tenantID, "dispatched", handled)
			}
		}

		select {
		case <-ctx.Done():
			slog.Info("dispatcher draining, running final tick")
			drainCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			now := time.Now().UTC()
			for _, tenantID := range devTenantIDs(drainCtx, cfg.MultiTenant, cfg.TenantID, repo) {
				if handled, err := runner.RunBatch(drainCtx, domaininstance.ClaimSpec{TenantID: tenantID, LeaseExpiresAt: now.Add(cfg.LeaseDuration), Now: now}, cfg.BatchSize); err != nil {
					slog.Error("dispatcher drain tick failed", "tenant_id", tenantID, "error", err)
				} else {
					slog.Info("dispatcher drain tick completed", "tenant_id", tenantID, "dispatched", handled)
				}
			}
			cancel()
			return
		case <-ticker.C:
		}
	}
}

type devTenantLister interface {
	ListActiveTenantIDs(ctx context.Context) ([]string, error)
}

func devTenantIDs(ctx context.Context, multiTenant bool, tenantID string, lister devTenantLister) []string {
	if !multiTenant {
		return []string{tenantID}
	}
	ids, err := lister.ListActiveTenantIDs(ctx)
	if err != nil {
		slog.Error("list active tenant ids failed", "error", err)
		return nil
	}
	return ids
}

// --- Worker helpers ---

type devWorkerConfig struct {
	TenantID          string
	MultiTenant       bool
	WorkerID          string
	PollInterval      time.Duration
	HeartbeatInterval time.Duration
	LeaseDuration     time.Duration
	Capacity          int
	Labels            map[string]any
}

func loadDevWorkerConfig() devWorkerConfig {
	workerID := os.Getenv("WORKER_ID")
	if workerID == "" {
		hostname, _ := os.Hostname()
		workerID = hostname + "-dev-" + uuid.New().String()[:8]
	}
	tenant := os.Getenv("WORKER_TENANT_ID")
	multiTenant := tenant == ""
	poll := devPositiveInt("WORKER_POLL_INTERVAL_SEC", 2)
	hb := devPositiveInt("WORKER_HEARTBEAT_INTERVAL_SEC", 10)
	lease := devPositiveInt("WORKER_LEASE_DURATION_SEC", 60)
	capacity := devPositiveInt("WORKER_CAPACITY", 5)
	labels := loadDevJSONMapEnv("WORKER_LABELS")
	return devWorkerConfig{
		TenantID:          tenant,
		MultiTenant:       multiTenant,
		WorkerID:          workerID,
		PollInterval:      time.Duration(poll) * time.Second,
		HeartbeatInterval: time.Duration(hb) * time.Second,
		LeaseDuration:     time.Duration(lease) * time.Second,
		Capacity:          capacity,
		Labels:            labels,
	}
}

func runDevWorker(ctx context.Context, wg *sync.WaitGroup, db *sql.DB, cfg devWorkerConfig) {
	defer wg.Done()

	repo := corepostgres.NewExecutorRepository(db)
	handlers := map[string]execute.Handler{
		"exec":      &handler.Exec{},
		"http":      handler.NewHTTP(http.DefaultClient),
		"webhook":   handler.NewWebhook(http.DefaultClient),
		"pg_notify": handler.NewPGNotify(db),
	}
	maps.Copy(handlers, handler.GetRegistered())
	runner := execute.NewTickUseCase(repo, handlers)

	// Start heartbeat goroutine.
	var hbWg sync.WaitGroup
	hbWg.Add(1)
	go runDevHeartbeat(ctx, &hbWg, db, cfg)

	// Poll loop — run immediately on start, then on tick.
	runDevWorkerOnce(ctx, runner, cfg, repo)

	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			hbWg.Wait()
			return
		case <-ticker.C:
			runDevWorkerOnce(ctx, runner, cfg, repo)
		}
	}
}

func runDevWorkerOnce(ctx context.Context, runner *execute.TickUseCase, cfg devWorkerConfig, lister devTenantLister) {
	for _, tenantID := range devTenantIDs(ctx, cfg.MultiTenant, cfg.TenantID, lister) {
		n, err := runner.RunOnce(ctx, tenantID, cfg.WorkerID, cfg.Capacity, cfg.LeaseDuration, cfg.Labels)
		if err != nil {
			slog.Error("worker tick failed", "tenant_id", tenantID, "error", err)
		} else if n > 0 {
			slog.Info("worker executed task", "tenant_id", tenantID, "handled", n)
		}
	}
}

func runDevHeartbeat(ctx context.Context, wg *sync.WaitGroup, db *sql.DB, cfg devWorkerConfig) {
	defer wg.Done()

	hb := corepostgres.NewWorkerRepository(db)
	lister := corepostgres.NewExecutorRepository(db)

	sendHb := func(hbCtx context.Context, status string) {
		for _, tenantID := range devTenantIDs(hbCtx, cfg.MultiTenant, cfg.TenantID, lister) {
			spec, err := domainworker.NormalizeHeartbeat(time.Now(), domainworker.HeartbeatInput{
				TenantID:       tenantID,
				WorkerID:       cfg.WorkerID,
				Status:         status,
				LeaseExpiresAt: time.Now().Add(cfg.LeaseDuration),
				Capacity:       cfg.Capacity,
			})
			if err != nil {
				slog.Error("normalize heartbeat failed", "error", err)
				return
			}
			if _, err := hb.UpsertHeartbeat(hbCtx, spec); err != nil {
				slog.Error("heartbeat failed", "error", err)
			}
		}
	}

	sendHb(ctx, domainworker.StatusOnline)

	ticker := time.NewTicker(cfg.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			sendHb(shutdownCtx, domainworker.StatusOffline)
			cancel()
			return
		case <-ticker.C:
			sendHb(ctx, domainworker.StatusOnline)
		}
	}
}

// --- Admin API helpers ---

func setupDevAdminServer(db *sql.DB) *http.Server {
	writeRepo := corepostgres.NewJobRepository(db)
	readRepo := adminpostgres.NewJobRepository(db)
	createJobUC := command.NewCreateJobUseCase(writeRepo, readRepo)
	updateJobUC := command.NewUpdateJobUseCase(writeRepo)
	changeStatusUC := command.NewChangeStatusUseCase(readRepo, writeRepo)
	listJobsUC := query.NewListJobsUseCase(readRepo)
	getJobUC := query.NewGetJobUseCase(readRepo)
	h := adminhttp.NewHandler(createJobUC, listJobsUC, getJobUC, updateJobUC, changeStatusUC)
	deleteJobUC := command.NewDeleteJobUseCase(writeRepo)
	triggerJobUC := command.NewTriggerJobUseCase(readRepo, corepostgres.NewInstanceRepository(db), corepostgres.NewInstanceRepository(db))
	instanceReadRepo := adminpostgres.NewInstanceRepository(db)
	instanceWriteRepo := corepostgres.NewInstanceRepository(db)
	h.SetDeleteJobUseCase(deleteJobUC)
	h.SetTriggerJobUseCase(triggerJobUC)
	h.SetListInstancesUseCase(instancequery.NewListInstancesUseCase(instanceReadRepo))
	h.SetGetInstanceUseCase(instancequery.NewGetInstanceUseCase(instanceReadRepo))
	h.SetCancelInstanceUseCase(instancecommand.NewCancelInstanceUseCase(instanceReadRepo, instanceWriteRepo))
	h.SetListAttemptsUseCase(instancequery.NewListAttemptsUseCase(instanceReadRepo))
	auth := middleware.NewAuth(db)

	r := gin.Default()
	r.Use(middleware.TraceMiddleware())
	if auth != nil {
		r.Use(auth.Middleware())
	}
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.GET("/openapi.json", func(c *gin.Context) {
		c.JSON(http.StatusOK, adminhttp.ServiceOpenAPIDocument())
	})
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))
	if h != nil {
		h.Register(r)
	}

	return &http.Server{Handler: r}
}

func runDevAdmin(ctx context.Context, wg *sync.WaitGroup, srv *http.Server, port string) {
	defer wg.Done()

	srv.Addr = ":" + port

	go func() {
		slog.Info("admin API listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("admin API listen failed", "error", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("admin API shutdown failed", "error", err)
	}
}

// --- Shared utilities ---

func loadDevPositiveInt(key string, defaultValue int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return defaultValue, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	if value < 1 {
		return defaultValue, fmt.Errorf("%s must be >= 1", key)
	}
	return value, nil
}

// devPositiveInt loads a positive int env var for the devserver, falling back
// to defaultValue (with a warning) on parse error. The devserver keeps running
// with defaults instead of aborting, and never returns 0 so tickers stay valid.
func devPositiveInt(key string, defaultValue int) int {
	v, err := loadDevPositiveInt(key, defaultValue)
	if err != nil {
		slog.Warn("invalid env, using default", "key", key, "default", defaultValue, "error", err)
	}
	return v
}

func loadDevJSONMapEnv(key string) map[string]any {
	raw := os.Getenv(key)
	if raw == "" {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return map[string]any{}
	}
	return m
}
