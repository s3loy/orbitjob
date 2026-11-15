package main

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"orbitjob/internal/core/app/slimaintenance"
)

type sliMaintenanceDeps struct {
	Pruner   slimaintenance.Pruner
	Tenants  []string
	Interval string
}

// The caller supplies the operator Lease context and configured tenant scope.
func runSLIMaintenanceLoop(ctx context.Context, logger *slog.Logger, deps sliMaintenanceDeps) {
	minutes, err := strconv.Atoi(deps.Interval)
	if err != nil || minutes < 1 || int64(minutes) > int64(time.Duration(1<<63-1)/time.Minute) {
		logger.Error("invalid SLI maintenance interval, loop disabled", "value", deps.Interval)
		return
	}
	maintenance := slimaintenance.Maintainer{Pruner: deps.Pruner, Tenants: deps.Tenants}
	ticker := time.NewTicker(time.Duration(minutes) * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			counts, err := maintenance.Sweep(ctx)
			if err != nil {
				logger.Error("SLI maintenance sweep failed", "error", err, "removed_plans", counts.RemovedPlans, "removed_events", counts.RemovedEvents)
			}
			if counts.RemovedPlans > 0 || counts.RemovedEvents > 0 {
				logger.Info("retired SLI replay details", "plans", counts.RemovedPlans, "events", counts.RemovedEvents)
			}
		}
	}
}
