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
	"orbitjob/internal/core/app/schedule"
	domain "orbitjob/internal/core/domain"
	corepostgres "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/platform/config"
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

func runLoop(
	ctx context.Context,
	runner tickRunner,
	probe func(context.Context) (time.Duration, error),
	cfg runtimeConfig,
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

	runDiscoveryBatch := func(ctx context.Context, limit int) (int, domain.ErrorClass) {
		counts, _ := runner.RunBatch(ctx, nowFn().UTC(), limit)
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

	for {
		start := time.Now()
		now := nowFn().UTC()

		switch curPhase {
		case schedule.PhaseDiscovery:
			metrics.SchedulerPhase.Set(0)
			d := schedule.NewDiscovery(probeWithTimeout, runDiscoveryBatch, state)
			curPhase = d.Run(ctx)
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
					mainTicker.Stop()
					return
				case <-mainTicker.Chan():
				}
				continue
			}

			_, dbPressure := schedule.UpdateSteady(state, pRTT, errClass, state.MaxBatchSize)
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
				metrics.SchedulerTickDuration.Observe(time.Since(start).Seconds())
				continue
			}
			breaker.Update(schedule.BreakerSignals{})
			if breaker.State() == schedule.PhaseHalfOpen {
				curPhase = schedule.PhaseHalfOpen
				metrics.SchedulerTickDuration.Observe(time.Since(start).Seconds())
				continue
			}

		case schedule.PhaseHalfOpen:
			metrics.SchedulerPhase.Set(3)
			hLimit := min(5, state.Limit/10)
			if hLimit < 1 {
				hLimit = 1
			}

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

		select {
		case <-ctx.Done():
			slog.Info("scheduler draining, running final tick")
			drainCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if counts, err := runner.RunBatch(drainCtx, nowFn().UTC(), state.Limit); err != nil {
				metrics.SchedulerCronErrors.Inc()
				slog.Error("scheduler drain tick failed", "error", err.Error())
			} else {
				metrics.SchedulerInstancesCreated.Add(float64(counts.Scheduled))
				slog.Info("scheduler drain tick completed", "handled_due_jobs", counts.Handled)
			}
			mainTicker.Stop()
			return
		case <-mainTicker.Chan():
		}
	}
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
	probe := func(ctx context.Context) (time.Duration, error) {
		start := time.Now()
		if err := db.PingContext(ctx); err != nil {
			return 0, err
		}
		return time.Since(start), nil
	}
	runLoopFn(ctx, runner, probe, cfg, newWallClockTicker, time.Now)

	return nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}
