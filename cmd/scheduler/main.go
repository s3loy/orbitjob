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
	"orbitjob/internal/core/app/checkschedule"
	"orbitjob/internal/core/app/sloevaluate"
	"orbitjob/internal/core/app/schedule"
	domain "orbitjob/internal/core/domain"
	corepostgres "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/platform/config"
	"orbitjob/internal/platform/election"
	"orbitjob/internal/platform/health"
	platformlogger "orbitjob/internal/platform/logger"
	"orbitjob/internal/platform/metrics"
	platformticker "orbitjob/internal/platform/ticker"
)

type runtimeConfig struct {
	BatchSizeMax int
	TickInterval time.Duration
	HealthPort   string
}

type tickRunner interface {
	RunBatch(ctx context.Context, now time.Time, limit int) (schedule.BatchCounts, error)
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
		repo := corepostgres.NewSchedulerRepository(db)
		return schedule.NewTickUseCase(repo, corepostgres.ClassifyError)
	}
	buildElectionFn = func(cfg election.EtcdConfig) (election.Coordinator, error) {
		return election.NewEtcd(cfg)
	}
	runLoopFn = runLoop
)

var newWallClockTicker = func(d time.Duration) schedulerTicker { return platformticker.New(d) }

func loadSchedulerRuntimeConfig() (runtimeConfig, error) {
	batchSizeMax, err := config.LoadPositiveIntEnv("SCHEDULER_BATCH_SIZE_MAX", 500)
	if err != nil {
		return runtimeConfig{}, err
	}

	tickIntervalSec, err := config.LoadPositiveIntEnv("SCHEDULER_TICK_INTERVAL_SEC", 5)
	if err != nil {
		return runtimeConfig{}, err
	}

	healthPort := os.Getenv("SCHEDULER_HEALTH_PORT")
	if healthPort == "" {
		healthPort = "6060"
	}

	return runtimeConfig{
		BatchSizeMax: batchSizeMax,
		TickInterval: time.Duration(tickIntervalSec) * time.Second,
		HealthPort:   healthPort,
	}, nil
}

func drainAndReturn(runner tickRunner, limit int, nowFn func() time.Time) {
	slog.Info("scheduler draining, running final tick")
	drainCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if counts, err := runner.RunBatch(drainCtx, nowFn().UTC(), limit); err != nil {
		metrics.SchedulerCronErrors.Inc()
		slog.Error("scheduler drain tick failed", "error", err.Error())
	} else {
		metrics.SchedulerInstancesCreated.Add(float64(counts.Scheduled))
		slog.Info("scheduler drain tick completed", "handled_due_jobs", counts.Handled)
	}
}

func runLoop(
	ctx context.Context,
	runner tickRunner,
	probe func(context.Context) (time.Duration, error),
	queueDepth func(context.Context) (int64, error),
	cfg *runtimeConfig,
	newTicker func(time.Duration) schedulerTicker,
	nowFn func() time.Time,
) {
	state := &schedule.ControllerState{
		MaxBatchSize: cfg.BatchSizeMax,
	}
	breakerCfg := schedule.BreakerConfig{
		Path1Consecutive: 2,
		Path2Consecutive: 3,
		Path2Ratio:       5.0,
		Path2MinAbs:      500 * time.Millisecond,
		BackoffBase:      5 * time.Second,
		BackoffMax:       300 * time.Second,
	}
	breaker := schedule.NewBreaker(breakerCfg)

	probeWithTimeout := func(ctx context.Context) (time.Duration, error) {
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		return probe(probeCtx)
	}

	discoveryIterations := 0
	runDiscoveryBatch := func(ctx context.Context, limit int) (int, domain.ErrorClass) {
		discoveryIterations++
		counts, err := runner.RunBatch(ctx, nowFn().UTC(), limit)
		if err != nil {
			return 0, domain.BackoffWorthy
		}
		metrics.SchedulerInstancesCreated.Add(float64(counts.Scheduled))
		class := domain.ClassNone
		if counts.Fatal > 0 {
			class = domain.FatalWorthy
		} else if counts.Backoff > 0 {
			class = domain.BackoffWorthy
		}
		return counts.Handled, class
	}

	curPhase := schedule.PhaseDiscovery
	lastCounts := schedule.BatchCounts{}
	mainTicker := newTicker(cfg.TickInterval)
	defer mainTicker.Stop()
	currentTickInterval := cfg.TickInterval

	// Idle backoff state.
	idleCount := 0
	const idleThreshold = 3
	const longInterval = 30 * time.Second
	isLongInterval := false

	resetToShortInterval := func() {
		if isLongInterval {
			mainTicker.Stop()
			mainTicker = newTicker(cfg.TickInterval)
			currentTickInterval = cfg.TickInterval
			isLongInterval = false
			metrics.SchedulerIntervalMode.Set(0)
			slog.Info("scheduler switching back to short interval", "interval_sec", cfg.TickInterval.Seconds())
		}
	}

	for {
		// Hot reload: recreate ticker if interval changed.
		if cfg.TickInterval != currentTickInterval {
			mainTicker.Stop()
			mainTicker = newTicker(cfg.TickInterval)
			currentTickInterval = cfg.TickInterval
			isLongInterval = false
			metrics.SchedulerIntervalMode.Set(0)
			slog.Info("scheduler tick interval reloaded", "interval_sec", cfg.TickInterval.Seconds())
		}

		start := time.Now()
		now := nowFn().UTC()

		// Sample queue depth for backpressure, but skip when idling to reduce DB load.
		var qd int64
		if queueDepth != nil && idleCount < idleThreshold {
			qd, _ = queueDepth(ctx)
			metrics.SchedulerQueueDepth.Set(float64(qd))
		}

		switch curPhase {
		case schedule.PhaseDiscovery:
			metrics.SchedulerPhase.Set(0)
			discoveryIterations = 0
			discoveryStart := time.Now()
			d := schedule.NewDiscovery(probeWithTimeout, runDiscoveryBatch, state)
			curPhase = d.Run(ctx)
			metrics.SchedulerDiscoveryDuration.Observe(time.Since(discoveryStart).Seconds())
			metrics.SchedulerDiscoveryIterations.Add(float64(discoveryIterations))
			metrics.SchedulerLimit.Set(float64(state.Limit))

		case schedule.PhaseSteady:
			metrics.SchedulerPhase.Set(1)
			pRTT, pErr := probeWithTimeout(ctx)

			var errClass domain.ErrorClass
			if pErr != nil {
				errClass = domain.BackoffWorthy
			}

			errorRate := 0.0
			if lastCounts.Handled > 0 {
				errorRate = float64(lastCounts.Backoff) / float64(lastCounts.Handled)
			}

			bPhase := breaker.Update(schedule.BreakerSignals{
				ErrorRate:    errorRate,
				ProbeRTT:     pRTT,
				LongtermRtt:  state.LongtermRtt,
				FatalTrigger: lastCounts.Fatal > 0,
			})
			if bPhase != schedule.PhaseSteady {
				curPhase = bPhase
				metrics.SchedulerBreakerTransitions.Inc()
				metrics.SchedulerTickDuration.Observe(time.Since(start).Seconds())
				select {
				case <-ctx.Done():
					return
				case <-mainTicker.Chan():
				}
				continue
			}

			_, dbPressure := schedule.UpdateSteady(state, pRTT, errClass, state.MaxBatchSize, qd)
			metrics.SchedulerLimit.Set(float64(state.Limit))
			metrics.SchedulerDBPressure.Set(dbPressure)
			metrics.SchedulerProbeRTT.Observe(pRTT.Seconds())

			var runErr error
			lastCounts, runErr = runner.RunBatch(ctx, now, state.Limit)
			if runErr != nil {
				slog.Error("steady tick error", "error", runErr.Error())
			}
			metrics.SchedulerInstancesCreated.Add(float64(lastCounts.Scheduled))

			if lastCounts.Fatal > 0 {
				breaker.Update(schedule.BreakerSignals{FatalTrigger: true})
				metrics.SchedulerBreakerTransitions.Inc()
				curPhase = schedule.PhaseProtect
			}

		case schedule.PhaseProtect:
			metrics.SchedulerPhase.Set(2)
			if breaker.State() == schedule.PhaseHalfOpen {
				curPhase = schedule.PhaseHalfOpen
				lastCounts = schedule.BatchCounts{}
				break
			}
			breaker.Update(schedule.BreakerSignals{})
			if breaker.State() == schedule.PhaseHalfOpen {
				curPhase = schedule.PhaseHalfOpen
				lastCounts = schedule.BatchCounts{}
				break
			}
			// Degraded mode: schedule at most 1 job per tick instead of full halt.
			counts, runErr := runner.RunBatch(ctx, now, 1)
			if runErr != nil {
				slog.Error("protect tick error", "error", runErr.Error())
			} else if counts.Scheduled > 0 {
				slog.Info("protect tick scheduled", "scheduled", counts.Scheduled)
			}
			metrics.SchedulerInstancesCreated.Add(float64(counts.Scheduled))
			lastCounts = counts

		case schedule.PhaseHalfOpen:
			metrics.SchedulerPhase.Set(3)
			hLimit := max(min(20, state.Limit/4), 1)

			counts, runErr := runner.RunBatch(ctx, now, hLimit)
			if runErr != nil {
				slog.Error("halfopen probe error", "error", runErr.Error())
			}
			metrics.SchedulerInstancesCreated.Add(float64(counts.Scheduled))

			if counts.Fatal > 0 || counts.Backoff > 0 {
				breaker.HalfOpenFailure()
				metrics.SchedulerBreakerTransitions.Inc()
				curPhase = schedule.PhaseProtect
			} else {
				var newLimit int
				curPhase, newLimit = breaker.HalfOpenSuccess(state.Limit)
				state.Limit = newLimit
				state.LongtermRtt = 0
				metrics.SchedulerBreakerTransitions.Inc()
			}
		}

		metrics.SchedulerTickDuration.Observe(time.Since(start).Seconds())

		// Idle detection: if no jobs handled, count idle tick.
		handled := lastCounts.Handled
		if curPhase == schedule.PhaseDiscovery {
			// Discovery phase doesn't set lastCounts; skip idle logic there.
			handled = -1
		}

		select {
		case <-ctx.Done():
			drainAndReturn(runner, state.Limit, nowFn)
			return
		case <-mainTicker.Chan():
			if handled == 0 {
				idleCount++
				metrics.SchedulerIdleTicksTotal.Inc()
				if idleCount >= idleThreshold {
					mainTicker.Stop()
					mainTicker = newTicker(longInterval)
					isLongInterval = true
					metrics.SchedulerIntervalMode.Set(1)
					slog.Info("scheduler switching to long interval", "interval_sec", longInterval.Seconds())
				}
			} else if handled > 0 {
				idleCount = 0
				resetToShortInterval()
			}
		}
	}
}

func watchSchedulerConfig(ctx context.Context, watcher config.Watcher, cfg *runtimeConfig) {
	go func() {
		_ = watcher.Watch(ctx, "/orbitjob/config/scheduler/batch_size_max", func(v string) {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				cfg.BatchSizeMax = n
				slog.Info("scheduler config updated", "key", "batch_size_max", "value", n)
			}
		})
	}()
	_ = watcher.Watch(ctx, "/orbitjob/config/scheduler/tick_interval_sec", func(v string) {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.TickInterval = time.Duration(n) * time.Second
			slog.Info("scheduler config updated", "key", "tick_interval_sec", "value", n)
		}
	})
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
	defer func() { _ = db.Close() }()

	pingCtx, cancel := context.WithTimeout(ctx, startupDBPingTimeout)
	defer cancel()
	if err := pingDBFn(pingCtx, db); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	healthCtx, healthCancel := context.WithCancel(context.Background())
	defer healthCancel()
	go health.StartComponentHealthServer(healthCtx, db, cfg.HealthPort, "scheduler")

	runner := buildRunnerFn(db)
	repo := corepostgres.NewSchedulerRepository(db)
	probe := func(ctx context.Context) (time.Duration, error) {
		start := time.Now()
		if err := db.PingContext(ctx); err != nil {
			return 0, err
		}
		return time.Since(start), nil
	}
	queueDepth := func(ctx context.Context) (int64, error) {
		return repo.CountActiveInstances(ctx)
	}

	// Worker context: cancelled when the scheduler leaves its main loop so
	// background goroutines shut down deterministically in tests and deployments.
	workerCtx, workerCancel := context.WithCancel(ctx)
	defer workerCancel()

	// Initialize check scheduler with a fixed interval to avoid data race on cfg.TickInterval.
	checkRepo := corepostgres.NewCheckRepository(db)
	checkRunRepo := corepostgres.NewCheckRunRepository(db)
	checkScheduler := checkschedule.NewTickUseCase(checkRepo, checkRunRepo)
	checkTickInterval := 5 * time.Second
	go func() {
		ticker := time.NewTicker(checkTickInterval)
		defer ticker.Stop()
		for {
			select {
			case <-workerCtx.Done():
				return
			case <-ticker.C:
				if _, err := checkScheduler.RunBatch(workerCtx, "default", 50); err != nil {
					slog.Error("check scheduler tick failed", "error", err.Error())
				}
			}
		}
	}()

	// Initialize SLO evaluator.
	sloRepo := corepostgres.NewSLORepository(db)
	snapshotRepo := corepostgres.NewSLISnapshotRepository(db)
	budgetRepo := corepostgres.NewBudgetRepository(db)
	alertRepo := corepostgres.NewBudgetAlertRepository(db)
	sloEvaluator := sloevaluate.NewEvaluateUseCase(sloRepo, snapshotRepo, budgetRepo, alertRepo)

	var evalWg sync.WaitGroup
	startEvaluator := func() {
		evalWg.Add(1)
		go func() {
			defer evalWg.Done()
			ticker := time.NewTicker(5 * time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-workerCtx.Done():
					return
				case <-ticker.C:
					evalCtx, cancel := context.WithTimeout(workerCtx, 2*time.Minute)
					if err := sloEvaluator.EvaluateAll(evalCtx, "default"); err != nil {
						slog.Error("slo evaluation failed", "error", err.Error())
					}
					cancel()
				}
			}
		}()
	}

	// Optional: etcd leader election for distributed HA.
	if os.Getenv("ETCD_ENABLED") == "true" {
		ep := os.Getenv("ETCD_ENDPOINTS")
		if ep == "" {
			return fmt.Errorf("ETCD_ENABLED=true but ETCD_ENDPOINTS is empty")
		}
		coord, err := buildElectionFn(election.EtcdConfig{
			Endpoints: strings.Split(ep, ","),
		})
		if err != nil {
			return fmt.Errorf("init election: %w", err)
		}
		defer func() { _ = coord.Close() }()

		leaderCtx, err := coord.Campaign(ctx, "scheduler-leader")
		if err != nil {
			return fmt.Errorf("campaign: %w", err)
		}
		slog.Info("scheduler became leader")

		// Start config watcher for hot reload.
		watcher, err := config.NewEtcdWatcher(strings.Split(ep, ","))
		if err != nil {
			slog.Warn("scheduler config watcher failed to start", "error", err.Error())
		} else {
			defer func() { _ = watcher.Close() }()
			go watchSchedulerConfig(leaderCtx, watcher, &cfg)
		}

		startEvaluator()
		runLoopFn(leaderCtx, runner, probe, queueDepth, &cfg, newWallClockTicker, time.Now)
		slog.Warn("scheduler lost leadership, exiting")
		workerCancel()
		evalWg.Wait()
		return nil
	}

	// PG-only mode: run directly (single-instance behaviour).
	startEvaluator()
	runLoopFn(ctx, runner, probe, queueDepth, &cfg, newWallClockTicker, time.Now)
	workerCancel()
	evalWg.Wait()
	return nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}
