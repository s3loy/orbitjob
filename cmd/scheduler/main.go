// Command scheduler runs the SLO evaluation loop over the active tenants.
//
// Everything that creates or executes runs is Kubernetes control-plane work and
// lives in the operator process: ScheduledJob occurrences fire from its schedule
// loop, and check occurrences fire from its check loop, so one identity drives
// all scheduling and execution. What is left here is the one loop that only
// reads accumulated state: evaluating SLI snapshots against error budgets and
// raising burn alerts.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	adminpostgres "orbitjob/internal/admin/store/postgres"
	"orbitjob/internal/core/app/sloevaluate"
	corepostgres "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/platform/config"
	"orbitjob/internal/platform/election"
	"orbitjob/internal/platform/health"
	platformlogger "orbitjob/internal/platform/logger"
)

const (
	startupDBPingTimeout = 5 * time.Second

	// An error budget is evaluated over a window, not a deadline, so the loop
	// runs on a cadence far slower than the control plane it observes.
	sloEvalInterval = 5 * time.Minute
	sloEvalTimeout  = 2 * time.Minute

	defaultHealthPort = "6060"
)

var (
	loadDotenvFn = config.LoadDotenv
	newLoggerFn  = platformlogger.New
	openDBFn     = adminpostgres.Open
	pingDBFn     = func(ctx context.Context, db *sql.DB) error { return db.PingContext(ctx) }
	// buildTenantListerFn returns the tenant enumeration the loop iterates.
	// There is no unscoped read of tenants: row level security hides them until
	// a tenant context is set, so the lister calls the SECURITY DEFINER helper.
	buildTenantListerFn = func(db *sql.DB) tenantLister {
		return corepostgres.NewSchedulerRepository(db)
	}
	buildElectionFn = func(cfg election.EtcdConfig) (election.Coordinator, error) {
		return election.NewEtcd(cfg)
	}
)

// tenantLister enumerates the tenants a loop has work for.
type tenantLister interface {
	ListActiveTenantIDs(ctx context.Context) ([]string, error)
}

// forEachActiveTenant discovers active tenants and invokes f for each one.
// Errors from listing or from individual tenants are logged but not returned,
// so background loops keep running even if one tenant fails.
func forEachActiveTenant(ctx context.Context, lister tenantLister, f func(context.Context, string) error) {
	ids, err := lister.ListActiveTenantIDs(ctx)
	if err != nil {
		slog.Error("list active tenant ids failed", "error", err.Error())
		return
	}
	if len(ids) == 0 {
		return
	}
	for _, id := range ids {
		if err := f(ctx, id); err != nil {
			slog.Error("tenant operation failed", "tenant_id", id, "error", err.Error())
		}
	}
}

// runSLOEvaluationLoop evaluates every active tenant's SLIs against its budgets.
func runSLOEvaluationLoop(ctx context.Context, tenants tenantLister, evaluator *sloevaluate.EvaluateUseCase) {
	ticker := time.NewTicker(sloEvalInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			forEachActiveTenant(ctx, tenants, func(ctx context.Context, tenantID string) error {
				evalCtx, cancel := context.WithTimeout(ctx, sloEvalTimeout)
				err := evaluator.EvaluateAll(evalCtx, tenantID)
				cancel()
				return err
			})
		}
	}
}

func healthPort() string {
	if port := os.Getenv("SCHEDULER_HEALTH_PORT"); port != "" {
		return port
	}
	return defaultHealthPort
}

func run(ctx context.Context) error {
	if err := loadDotenvFn(); err != nil {
		return err
	}

	slog.SetDefault(newLoggerFn(os.Getenv("APP_ENV")))

	dsn, _, err := config.ResolveDatabaseDSN("RUNTIME_DSN", "SCHEDULER_DSN", "DATABASE_DSN")
	if err != nil {
		return err
	}

	db, err := openDBFn(dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	pingCtx, cancelPing := context.WithTimeout(ctx, startupDBPingTimeout)
	defer cancelPing()
	if err := pingDBFn(pingCtx, db); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	healthCtx, healthCancel := context.WithCancel(context.Background())
	defer healthCancel()
	go health.StartComponentHealthServer(healthCtx, db, healthPort(), "scheduler")

	// Optional: etcd leader election for distributed HA. The loop is singleton
	// work, so it only starts once this instance holds the lease.
	var coordinator election.Coordinator
	runCtx := ctx
	if os.Getenv("ETCD_ENABLED") == "true" {
		endpoints := os.Getenv("ETCD_ENDPOINTS")
		if endpoints == "" {
			return fmt.Errorf("ETCD_ENABLED=true but ETCD_ENDPOINTS is empty")
		}
		coordinator, err = buildElectionFn(election.EtcdConfig{Endpoints: strings.Split(endpoints, ",")})
		if err != nil {
			return fmt.Errorf("init election: %w", err)
		}
		defer func() { _ = coordinator.Close() }()

		leaderCtx, err := coordinator.Campaign(ctx, "scheduler-leader")
		if err != nil {
			return fmt.Errorf("campaign: %w", err)
		}
		slog.Info("scheduler became leader")
		runCtx = leaderCtx
	}

	// Worker context: cancelled when leadership ends so the background loop
	// shuts down deterministically before the process returns.
	workerCtx, workerCancel := context.WithCancel(runCtx)
	defer workerCancel()

	tenants := buildTenantListerFn(db)
	evaluator := sloevaluate.NewEvaluateUseCase(
		corepostgres.NewSLORepository(db),
		corepostgres.NewSLISnapshotRepository(db),
		corepostgres.NewBudgetRepository(db),
		corepostgres.NewBudgetAlertRepository(db),
	)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runSLOEvaluationLoop(workerCtx, tenants, evaluator)
	}()

	<-runCtx.Done()
	if coordinator != nil {
		slog.Warn("scheduler lost leadership, exiting")
	}
	workerCancel()
	wg.Wait()
	return nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}
