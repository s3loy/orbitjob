// Command operator runs the OrbitJob Kubernetes control plane: it projects
// ScheduledJob definitions into immutable revisions, turns job runs into
// Kubernetes Jobs, and writes observed execution state back to PostgreSQL.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"syscall"
	"time"

	adminpostgres "orbitjob/internal/admin/store/postgres"
	"orbitjob/internal/core/app/checkobserve"
	"orbitjob/internal/core/app/checkschedule"
	"orbitjob/internal/core/app/controlplane"
	"orbitjob/internal/core/app/execution"
	"orbitjob/internal/core/app/functionsync"
	"orbitjob/internal/core/domain/workflow"
	corepostgres "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/operator"
	"orbitjob/internal/platform/config"
	"orbitjob/internal/platform/election"
	"orbitjob/internal/platform/health"
	platformlogger "orbitjob/internal/platform/logger"
)

const (
	envAppEnv           = "APP_ENV"
	envScheduleInterval = "OPERATOR_SCHEDULE_INTERVAL_SEC"
	envRetention        = "OPERATOR_RETENTION_INTERVAL_MIN"
	envOperatorTenant   = "OPERATOR_TENANT_DEFAULT"
	envOperatorPort     = "OPERATOR_HEALTH_PORT"
	envPodNamespace     = "POD_NAMESPACE"
	envPodName          = "POD_NAME"

	defaultHealthPort        = "8082"
	defaultScheduleInterval  = "5"
	defaultRetentionInterval = "5"
	defaultPodNamespace      = "orbitjob-system"
	resyncInterval           = 10 * time.Minute
	startupPingLimit         = 5 * time.Second
)

var (
	newLoggerFn = platformlogger.New
	openDBFn    = adminpostgres.Open
	inClusterFn = operator.NewInCluster
	runFn       = func(ctx context.Context, c operator.Controller) error { return c.Run(ctx) }
)

// workflowPrunerStore narrows the workflow ledger store to the retainer's
// pruner contract: the store lists narrow prunable rows and deletes one run
// steps-then-row atomically, the retainer wants workflow.Run identities and a
// deleted flag. Wiring-only: the store's prune contract (steps first, the row
// only after its last step is gone) is what the adapter must not disturb.
type workflowPrunerStore struct {
	store *corepostgres.WorkflowRunRepository
}

func (p workflowPrunerStore) PrunableRuns(
	ctx context.Context, tenantID, sourceUID string, keepSuccessful, keepFailed int,
) ([]workflow.Run, error) {
	rows, err := p.store.PrunableRuns(ctx, tenantID, sourceUID, keepSuccessful, keepFailed)
	if err != nil {
		return nil, err
	}
	out := make([]workflow.Run, 0, len(rows))
	for _, row := range rows {
		out = append(out, workflow.Run{
			ID:            row.ID,
			SourceUID:     sourceUID,
			OccurrenceKey: row.OccurrenceKey,
			Phase:         row.Phase,
		})
	}
	return out, nil
}

func (p workflowPrunerStore) DeleteRun(ctx context.Context, tenantID string, runID int64) (bool, error) {
	deleted, _, err := p.store.PruneRun(ctx, tenantID, runID)
	return deleted, err
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	logger := newLoggerFn(os.Getenv(envAppEnv))
	slog.SetDefault(logger)

	dsn, source, err := config.ResolveDatabaseDSN("OPERATOR_DSN", "RUNTIME_DSN", "DATABASE_DSN")
	if err != nil {
		return fmt.Errorf("resolve database dsn: %w", err)
	}
	logger.Info("database dsn resolved", "source", source)

	db, err := openDBFn(dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = db.Close() }()

	pingCtx, cancelPing := context.WithTimeout(ctx, startupPingLimit)
	defer cancelPing()
	if err := db.PingContext(pingCtx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	tenants, err := loadTenantResolver()
	if err != nil {
		return err
	}

	controller, err := inClusterFn(operator.Config{Resync: resyncInterval, Workers: defaultWorkers()})
	if err != nil {
		return fmt.Errorf("build in-cluster controller: %w", err)
	}
	if controller.Dynamic == nil {
		return fmt.Errorf("dynamic client is required")
	}

	repository := corepostgres.NewControlPlaneRepository(db)
	workflowRepo := corepostgres.NewWorkflowRunRepository(db)
	functionRunRepo := corepostgres.NewFunctionRunRepository(db)
	runtime := operator.Runtime{
		Dynamic:       controller.Dynamic,
		Jobs:          execution.Adapter{Client: execution.KubernetesJobClient{Client: controller.Kubernetes.BatchV1()}},
		Revisions:     repository,
		Runs:          repository,
		Tenants:       tenants,
		Workflows:     workflowRepo,
		Functions:     functionRunRepo,
		OutcomeReader: repository,
		Outcomes: checkobserve.NewRecorder(
			repository,
			corepostgres.NewCheckRunRepository(db),
			corepostgres.NewCheckRepository(db),
			corepostgres.NewSLIRepository(db),
			corepostgres.NewSLISnapshotRepository(db),
		),
	}
	controller.Reconcile = runtime.Handlers().Handle
	controller.Log = logger

	go health.StartComponentHealthServer(ctx, db, operator.EnvOr(envOperatorPort, defaultHealthPort), "operator")

	checkScopes, err := buildCheckScopes(tenants)
	if err != nil {
		return err
	}
	// Scheduling and retention are singleton work: two instances acting at once
	// would race on the same occurrences. Reconciliation stays multi-replica,
	// where the database constraints make concurrent writers converge.
	go runSingletonLoops(ctx, logger, controller, db, repository, workflowRepo, tenants.TenantIDs(), checkScopes, checkschedule.CheckSource(corepostgres.NewCheckRepository(db)))

	logger.Info("operator starting")
	if err := runFn(ctx, controller); err != nil {
		return fmt.Errorf("run operator: %w", err)
	}
	logger.Info("operator stopped")
	return nil
}

// tenantResolver is the resolver contract including its tenant listing, which
// the scheduler needs in order to know which tenants to scan.
type tenantResolver interface {
	operator.TenantResolver
	TenantIDs() []string
}

func loadTenantResolver() (tenantResolver, error) {
	raw := os.Getenv(operator.NamespaceTenantsTableEnv)
	if raw == "" {
		// A single-tenant installation still needs an explicit mapping; falling
		// back to a default tenant would let any namespace write into it.
		fallback := os.Getenv(envOperatorTenant)
		if fallback == "" {
			return nil, fmt.Errorf("%s is required", operator.NamespaceTenantsTableEnv)
		}
		return operator.DefaultTenantResolver{Tenant: fallback}, nil
	}
	mapping, err := operator.ParseNamespaceTenants(raw)
	if err != nil {
		return nil, err
	}
	return operator.NamespaceTenantResolver{Tenants: mapping}, nil
}

func defaultWorkers() int {
	return 2
}

// scheduleDeps are the dependencies the schedule loop needs.
type scheduleDeps struct {
	Revisions controlplane.RevisionSource
	Runs      controlplane.RunRecorder
	Publisher controlplane.RunPublisher
	Tenants   []string
	Interval  string
}

// checkDeps are the dependencies the check firing loop needs. They are stated
// as the narrow interfaces the loop actually uses, so the compiler keeps the
// loop from reaching beyond firing checks.
type checkDeps struct {
	Checks    checkschedule.CheckSource
	Revisions checkschedule.RevisionWriter
	Runs      checkschedule.RunRecorder
	Publisher checkschedule.RunPublisher
	Scopes    []checkschedule.Scope
	Interval  string
}

// workflowDeps are the dependencies the workflow advance loop needs: the
// ledger it advances, the revisions it resolves task references against, the
// publisher it creates step CRs and cancel requests with, and the projection
// closure that keeps a run's CR status honest between resyncs.
type workflowDeps struct {
	Workflows     operator.WorkflowRunStore
	Revisions     operator.WorkflowRevisionSource
	Publisher     operator.WorkflowPublisher
	ProjectStatus func(context.Context, string, workflow.Run) error
	Tenants       []string
	Interval      string
}

// functionSyncDeps are the dependencies the function revision-sync loop
// needs. The scopes are the same tenant→namespace derivation the check loop
// uses: a function invocation publishes its JobRun into the revision's
// source_namespace, so both projections must agree on that namespace.
type functionSyncDeps struct {
	Functions functionsync.FunctionSource
	Revisions functionsync.RevisionWriter
	Scopes    []checkschedule.Scope
	Interval  string
}

// workflowRetentionDeps are the dependencies the workflow retention loop
// needs. They are narrower than workflowDeps so the compiler enforces that
// retention cannot create or advance steps.
type workflowRetentionDeps struct {
	Definitions operator.WorkflowRevisionSource
	Workflows   operator.WorkflowRunStore
	Pruner      operator.WorkflowHistoryPruner
	Remover     operator.RunRemover
	Tenants     []string
	Interval    string
}

// buildCheckScopes resolves, per tenant, the namespace its check runs are
// published into. The JobRun custom resource and the Kubernetes Job both land
// there, so the namespace's tenancy mapping must resolve back to the same
// tenant. A tenant mapped from several namespaces gets its lexicographically
// first one, which keeps scope derivation deterministic.
func buildCheckScopes(resolver tenantResolver) ([]checkschedule.Scope, error) {
	switch r := resolver.(type) {
	case operator.NamespaceTenantResolver:
		first := make(map[string]string, len(r.Tenants))
		for ns, tenant := range r.Tenants {
			if current, ok := first[tenant]; !ok || ns < current {
				first[tenant] = ns
			}
		}
		scopes := make([]checkschedule.Scope, 0, len(first))
		for tenant, ns := range first {
			scopes = append(scopes, checkschedule.Scope{TenantID: tenant, Namespace: ns})
		}
		sort.Slice(scopes, func(i, j int) bool { return scopes[i].TenantID < scopes[j].TenantID })
		return scopes, nil
	case operator.DefaultTenantResolver:
		// A single-tenant installation names no workload namespaces, so check
		// runs live beside the operator itself.
		return []checkschedule.Scope{{
			TenantID:  r.Tenant,
			Namespace: operator.EnvOr(envPodNamespace, defaultPodNamespace),
		}}, nil
	default:
		return nil, fmt.Errorf("cannot derive check scopes from tenant resolver %T", resolver)
	}
}

// runRetentionLoop trims run history so neither the database nor the API server
// grows without bound. Its interval is in minutes (OPERATOR_RETENTION_INTERVAL_MIN),
// a coarser cadence than the second-based schedule and check loops because
// retention is housekeeping, not scheduling.
// runSingletonLoops runs the scheduler, check, workflow, function-sync and
// retention loops under a Lease. If leadership is lost, all loops stop: a
// leader that cannot renew must not keep acting on stale authority.
func runSingletonLoops(
	ctx context.Context, logger *slog.Logger, controller operator.Controller, db *sql.DB,
	repository *corepostgres.ControlPlaneRepository, workflowStore *corepostgres.WorkflowRunRepository,
	tenantIDs []string, checkScopes []checkschedule.Scope, checks checkschedule.CheckSource,
) {
	namespace := operator.EnvOr(envPodNamespace, defaultPodNamespace)
	coordinator, err := election.NewKubernetes(election.KubernetesConfig{
		Namespace: namespace,
		Identity:  os.Getenv(envPodName),
		Client:    controller.Kubernetes,
	})
	if err != nil {
		logger.Error("leader election unavailable, singleton loops disabled", "error", err)
		return
	}
	defer func() { _ = coordinator.Close() }()

	leaderCtx, err := coordinator.Campaign(ctx, "operator-singleton")
	if err != nil {
		logger.Error("leader election failed, singleton loops disabled", "error", err)
		return
	}
	logger.Info("became leader, starting scheduling, checks, workflows, function sync and retention")

	interval := operator.EnvOr(envScheduleInterval, defaultScheduleInterval)
	go runScheduleLoop(leaderCtx, logger, scheduleDeps{
		Revisions: repository,
		Runs:      repository,
		Publisher: operator.RunPublisher{Client: controller.Dynamic},
		Tenants:   tenantIDs,
		Interval:  interval,
	})
	go runCheckLoop(leaderCtx, logger, checkDeps{
		Checks:    checks,
		Revisions: repository,
		Runs:      repository,
		Publisher: operator.RunPublisher{Client: controller.Dynamic},
		Scopes:    checkScopes,
		Interval:  interval,
	})
	go runWorkflowLoop(leaderCtx, logger, workflowDeps{
		Workflows: workflowStore,
		Revisions: repository,
		Publisher: operator.RunPublisher{Client: controller.Dynamic},
		ProjectStatus: func(ctx context.Context, tenant string, run workflow.Run) error {
			return (operator.Runtime{
				Dynamic:   controller.Dynamic,
				Revisions: repository,
				Workflows: workflowStore,
			}).PatchWorkflowStatusByName(ctx, tenant, run)
		},
		Tenants:  tenantIDs,
		Interval: interval,
	})
	go runWorkflowRetentionLoop(leaderCtx, logger, workflowRetentionDeps{
		Definitions: repository,
		Workflows:   workflowStore,
		Pruner:      workflowPrunerStore{store: workflowStore},
		Remover:     operator.RunRemover{Client: controller.Dynamic},
		Tenants:     tenantIDs,
		Interval:    operator.EnvOr(envRetention, defaultRetentionInterval),
	})
	go runFunctionSyncLoop(leaderCtx, logger, functionSyncDeps{
		Functions: corepostgres.NewFunctionRepository(db),
		Revisions: repository,
		Scopes:    checkScopes,
		Interval:  interval,
	})
	runRetentionLoop(leaderCtx, logger, retentionDeps{
		Revisions: repository,
		Pruner:    repository,
		Remover:   operator.RunRemover{Client: controller.Dynamic},
		Tenants:   tenantIDs,
		Interval:  operator.EnvOr(envRetention, defaultRetentionInterval),
	})

	<-leaderCtx.Done()
	logger.Warn("lost leadership, singleton loops stopped")
}

// retentionDeps are the dependencies the retention loop needs. They are narrower
// than scheduleDeps so the compiler enforces that retention cannot publish runs.
type retentionDeps struct {
	Revisions controlplane.RevisionSource
	Pruner    controlplane.RunPruner
	Remover   controlplane.RunRemover
	Tenants   []string
	Interval  string
}

func runRetentionLoop(ctx context.Context, logger *slog.Logger, deps retentionDeps) {
	minutes, err := strconv.Atoi(deps.Interval)
	if err != nil || minutes < 1 {
		logger.Error("invalid retention interval, retention loop disabled", "value", deps.Interval)
		return
	}
	retainer := controlplane.Retainer{
		Definitions: deps.Revisions,
		History:     deps.Pruner,
		Remover:     deps.Remover,
		Tenants:     deps.Tenants,
	}
	// Retention is housekeeping, not a user-facing deadline: sweeping far less
	// often than scheduling keeps it off the hot path.
	ticker := time.NewTicker(time.Duration(minutes) * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			removed, err := retainer.Sweep(ctx)
			if err != nil {
				logger.Error("retention sweep failed", "error", err)
				continue
			}
			if removed > 0 {
				logger.Info("pruned run history", "count", removed)
			}
		}
	}
}

// runScheduleLoop fires due definitions on a fixed interval. It lives in the
// operator process so one identity drives both scheduling and execution, which
// keeps the writer epoch single-valued during the migration from the legacy
// runtime; it is written against interfaces so it can move to its own process.
func runScheduleLoop(ctx context.Context, logger *slog.Logger, deps scheduleDeps) {
	seconds, err := strconv.Atoi(deps.Interval)
	if err != nil || seconds < 1 {
		logger.Error("invalid schedule interval, schedule loop disabled", "value", deps.Interval)
		return
	}
	scheduler := controlplane.Scheduler{
		Revisions: deps.Revisions,
		Runs:      deps.Runs,
		Publisher: deps.Publisher,
		Tenants:   deps.Tenants,
		// Who asked for the run. A scheduled occurrence has no human behind it,
		// and the ledger's actor column is NOT NULL and non-empty, so the writer
		// states the scheduling identity explicitly rather than relying on the
		// package default. It is a role name, not the pod name: a pod is
		// replaced on every deploy and the ledger outlives it.
		Actor: controlplane.ActorScheduler,
	}
	ticker := time.NewTicker(time.Duration(seconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			created, err := scheduler.Tick(ctx)
			if err != nil {
				logger.Error("schedule tick failed", "error", err)
				continue
			}
			if created > 0 {
				logger.Info("scheduled job runs", "count", created)
			}
		}
	}
}

// runWorkflowLoop advances open workflow runs on the same fixed interval as
// the job schedule loop. It shares the operator's identity and Lease for the
// same reason the check loop does: one writer epoch for every run the platform
// creates, whatever the trigger.
func runWorkflowLoop(ctx context.Context, logger *slog.Logger, deps workflowDeps) {
	if deps.Workflows == nil {
		logger.Error("workflow ledger missing, workflow loop disabled")
		return
	}
	seconds, err := strconv.Atoi(deps.Interval)
	if err != nil || seconds < 1 {
		logger.Error("invalid schedule interval, workflow loop disabled", "value", deps.Interval)
		return
	}
	walker := operator.WorkflowWalker{
		Workflows:     deps.Workflows,
		Revisions:     deps.Revisions,
		Publisher:     deps.Publisher,
		ProjectStatus: deps.ProjectStatus,
		Tenants:       deps.Tenants,
		// Who asked for the step. The workflow's actor asked for the run; the
		// walker is what advanced it, and the ledger records the role name.
		Actor: operator.ActorWorkflowWalker,
	}
	ticker := time.NewTicker(time.Duration(seconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			created, err := walker.Tick(ctx)
			if err != nil {
				logger.Error("workflow tick failed", "error", err)
				continue
			}
			if created > 0 {
				logger.Info("fired workflow steps", "count", created)
			}
		}
	}
}

// runWorkflowRetentionLoop trims workflow history on its own tick. The
// pruning itself is one atomic store operation per run — steps first, the
// workflow row after — so two intervals cannot race each other into an
// out-of-order delete.
func runWorkflowRetentionLoop(ctx context.Context, logger *slog.Logger, deps workflowRetentionDeps) {
	if deps.Workflows == nil || deps.Pruner == nil {
		logger.Error("workflow ledger or pruner missing, workflow retention disabled")
		return
	}
	minutes, err := strconv.Atoi(deps.Interval)
	if err != nil || minutes < 1 {
		logger.Error("invalid retention interval, workflow retention loop disabled", "value", deps.Interval)
		return
	}
	retainer := operator.WorkflowRetainer{
		Definitions: deps.Definitions,
		Workflows:   deps.Workflows,
		History:     deps.Pruner,
		Remover:     deps.Remover,
		Tenants:     deps.Tenants,
	}
	ticker := time.NewTicker(time.Duration(minutes) * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			removed, err := retainer.Sweep(ctx)
			if err != nil {
				logger.Error("workflow retention sweep failed", "error", err)
				continue
			}
			if removed > 0 {
				logger.Info("pruned workflow history", "count", removed)
			}
		}
	}
}

// runFunctionSyncLoop materializes one definition revision per active
// function row on the same fixed interval as the schedule loop. It is
// singleton work for the same reason scheduling is: two writers projecting
// the same generation race on the active-revision pointer the invoke path
// reads. A clean tick is silent — the steady state is an idempotent no-op the
// audit trail does not record — so only a failed tenant sync is logged.
func runFunctionSyncLoop(ctx context.Context, logger *slog.Logger, deps functionSyncDeps) {
	seconds, err := strconv.Atoi(deps.Interval)
	if err != nil || seconds < 1 {
		logger.Error("invalid schedule interval, function sync loop disabled", "value", deps.Interval)
		return
	}
	sync := &functionsync.Sync{Functions: deps.Functions, Revisions: deps.Revisions}
	ticker := time.NewTicker(time.Duration(seconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, scope := range deps.Scopes {
				if _, err := sync.RunTenant(ctx, functionsync.Scope{
					TenantID:  scope.TenantID,
					Namespace: scope.Namespace,
				}); err != nil {
					logger.Error("function sync tick failed", "tenant", scope.TenantID, "error", err)
				}
			}
		}
	}
}

// runCheckLoop fires due checks on the same fixed interval as the job
// schedule loop. It shares the operator's identity and Lease for the same
// reason: one writer epoch for every run the platform creates, whatever the
// trigger.
func runCheckLoop(ctx context.Context, logger *slog.Logger, deps checkDeps) {
	seconds, err := strconv.Atoi(deps.Interval)
	if err != nil || seconds < 1 {
		logger.Error("invalid check interval, check loop disabled", "value", deps.Interval)
		return
	}
	checks := checkschedule.NewTickUseCase(deps.Checks, deps.Revisions, deps.Runs, deps.Publisher)
	ticker := time.NewTicker(time.Duration(seconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, scope := range deps.Scopes {
				created, err := checks.RunBatch(ctx, scope, checkschedule.DefaultBatchSize)
				if err != nil {
					logger.Error("check tick failed", "tenant", scope.TenantID, "error", err)
					continue
				}
				if created > 0 {
					logger.Info("fired check runs", "tenant", scope.TenantID, "count", created)
				}
			}
		}
	}
}
