package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/platform/metrics"
)

type executionIsolationReader interface {
	ExecutionIsolationSummary(context.Context, string) ([]jobrun.IsolationSummary, error)
}

type executionIsolationSnapshot map[jobrun.Phase]jobrun.IsolationSummary

// readExecutionIsolation publishes no partial scope as a complete inventory.
// Reads include inactive sources and tenants still assigned to this operator.
func readExecutionIsolation(ctx context.Context, reader executionIsolationReader, tenants []string) (executionIsolationSnapshot, error) {
	out := executionIsolationSnapshot{
		jobrun.ExecutionUnknown: {Phase: jobrun.ExecutionUnknown},
		jobrun.CancelUnknown:    {Phase: jobrun.CancelUnknown},
	}
	seen := make(map[string]bool, len(tenants))
	var failures []error
	for _, tenant := range tenants {
		if ctx.Err() != nil {
			return nil, errors.Join(append(failures, ctx.Err())...)
		}
		if seen[tenant] {
			continue
		}
		seen[tenant] = true
		tenantCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		rows, err := reader.ExecutionIsolationSummary(tenantCtx, tenant)
		cancel()
		if err != nil {
			failures = append(failures, fmt.Errorf("tenant %s: %w", tenant, err))
			continue
		}
		for _, item := range rows {
			current, ok := out[item.Phase]
			if !ok || item.Count < 1 || item.OldestAt.IsZero() {
				failures = append(failures, fmt.Errorf("tenant %s: invalid execution isolation summary", tenant))
				continue
			}
			current.Count += item.Count
			if current.OldestAt.IsZero() || item.OldestAt.Before(current.OldestAt) {
				current.OldestAt = item.OldestAt
			}
			out[item.Phase] = current
		}
	}
	if err := errors.Join(failures...); err != nil {
		return nil, err
	}
	return out, nil
}

// Every replica observes the ledger independently of Lease leadership. The
// metrics therefore remain fresh on standby replicas and never sum replica
// snapshots into an inflated obligation count.
func runExecutionIsolationLoop(ctx context.Context, logger *slog.Logger, reader executionIsolationReader, tenants []string) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for ctx.Err() == nil {
		snapshot, err := readExecutionIsolation(ctx, reader, tenants)
		if err != nil {
			metrics.ExecutionIsolationCollectionSuccess.Set(0)
			if ctx.Err() == nil {
				logger.Error("execution isolation observation failed", "error", err)
			}
		} else {
			for phase, item := range snapshot {
				metrics.ExecutionIsolatedRuns.WithLabelValues(string(phase)).Set(float64(item.Count))
				oldest := float64(0)
				if !item.OldestAt.IsZero() {
					oldest = float64(item.OldestAt.Unix())
				}
				metrics.ExecutionIsolationOldest.WithLabelValues(string(phase)).Set(oldest)
			}
			metrics.ExecutionIsolationObserved.Set(float64(time.Now().Unix()))
			metrics.ExecutionIsolationCollectionSuccess.Set(1)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
