package main

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

type leaderCampaign func(context.Context, string) (context.Context, error)

// runLeaderSessions retries the existing Lease campaign after a lost epoch has
// fully drained. Fixed-delay retries bound API load without blocking shutdown.
func runLeaderSessions(ctx context.Context, logger *slog.Logger, campaign leaderCampaign, retryDelay time.Duration, workers ...func(context.Context)) {
	if retryDelay <= 0 {
		retryDelay = 2 * time.Second
	}
	for ctx.Err() == nil {
		leaderCtx, err := campaign(ctx, "operator-singleton")
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			logger.Error("leader election failed, retrying", "error", err, "retry_after", retryDelay)
		} else {
			if leaderCtx.Err() == nil {
				logger.Info("became leader, starting scheduling, checks, workflows, function sync and retention")
				runLeaderSession(ctx, leaderCtx, workers)
			}
			if ctx.Err() != nil {
				return
			}
			logger.Warn("lost leadership, singleton loops stopped; retrying", "retry_after", retryDelay)
		}
		timer := time.NewTimer(retryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func runLeaderSession(ctx, leaderCtx context.Context, workers []func(context.Context)) {
	sessionCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(leaderCtx, cancel)
	var running sync.WaitGroup
	for _, worker := range workers {
		running.Go(func() { worker(sessionCtx) })
	}
	select {
	case <-ctx.Done():
	case <-leaderCtx.Done():
	}
	cancel()
	stop()
	running.Wait()
}
