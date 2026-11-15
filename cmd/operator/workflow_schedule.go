package main

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"orbitjob/internal/operator"
)

type workflowScheduleDeps struct {
	Revisions operator.WorkflowScheduleRevisions
	Workflows operator.WorkflowRunStore
	Publisher operator.WorkflowSchedulePublisher
	Tenants   []string
	Interval  string
}

func runWorkflowScheduleLoop(ctx context.Context, logger *slog.Logger, deps workflowScheduleDeps) {
	seconds, err := strconv.Atoi(deps.Interval)
	if err != nil || seconds < 1 {
		logger.Error("invalid interval, workflow schedule loop disabled", "value", deps.Interval)
		return
	}
	scheduler := operator.WorkflowScheduler{Revisions: deps.Revisions, Workflows: deps.Workflows, Publisher: deps.Publisher, Tenants: deps.Tenants}
	ticker := time.NewTicker(time.Duration(seconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			created, err := scheduler.Tick(ctx)
			if err != nil {
				logger.Error("workflow scheduling tick failed", "error", err)
			}
			if created > 0 {
				logger.Info("scheduled workflow runs", "count", created)
			}
		}
	}
}
