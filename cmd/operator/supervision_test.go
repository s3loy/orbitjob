package main

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

func TestSingletonSupervisorRecampaignsAfterTermReturns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	calls := 0
	runLeaderSessions(ctx, slog.Default(), func(parent context.Context, _ string) (context.Context, error) {
		calls++
		if calls == 3 {
			cancel()
		}
		term, stop := context.WithCancel(parent)
		stop()
		return term, nil
	}, time.Millisecond)
	if calls != 3 {
		t.Fatalf("returned term stranded singleton loops: calls=%d", calls)
	}
}
