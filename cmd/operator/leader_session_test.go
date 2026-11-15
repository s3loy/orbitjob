package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func receiveEpoch(t *testing.T, events <-chan int, want int) {
	t.Helper()
	select {
	case got := <-events:
		if got != want {
			t.Fatalf("epoch = %d, want %d", got, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("epoch %d did not start or stop", want)
	}
}

func TestLeaderSessionsReacquireOnlyAfterPreviousWorkersDrain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	firstLeader, loseLeadership := context.WithCancel(ctx)
	defer loseLeadership()
	started := make(chan int, 2)
	stopping := make(chan int, 2)
	drain := make(chan struct{})
	var calls, active atomic.Int32
	var firstExited atomic.Bool
	campaign := func(context.Context, string) (context.Context, error) {
		call := calls.Add(1)
		if call == 1 {
			return firstLeader, nil
		}
		if !firstExited.Load() {
			t.Error("reacquired while the previous epoch still had a worker")
		}
		return ctx, nil
	}
	worker := func(session context.Context) {
		epoch := int(calls.Load())
		if got := active.Add(1); got != 1 {
			t.Errorf("concurrent epochs = %d, want 1", got)
		}
		started <- epoch
		<-session.Done()
		stopping <- epoch
		if epoch == 1 {
			<-drain
			firstExited.Store(true)
		}
		active.Add(-1)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		runLeaderSessions(ctx, quietLogger(), campaign, time.Millisecond, worker)
	}()
	receiveEpoch(t, started, 1)
	loseLeadership()
	receiveEpoch(t, stopping, 1)
	if got := calls.Load(); got != 1 {
		t.Errorf("campaigns before worker exit = %d, want 1", got)
	}
	close(drain)
	receiveEpoch(t, started, 2)
	cancel()
	receiveEpoch(t, stopping, 2)
	if !loopFinished(done, time.Second) {
		t.Fatal("parent cancellation did not finish the active epoch")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("campaigns after parent cancellation = %d, want 2", got)
	}
}

func TestLeaderSessionsRetryCampaignFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var attempts atomic.Int32
	started := make(chan int, 1)
	campaign := func(context.Context, string) (context.Context, error) {
		if attempts.Add(1) == 1 {
			return nil, errors.New("temporary API outage")
		}
		return ctx, nil
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		runLeaderSessions(ctx, quietLogger(), campaign, time.Millisecond, func(session context.Context) {
			started <- int(attempts.Load())
			<-session.Done()
		})
	}()
	receiveEpoch(t, started, 2)
	cancel()
	if !loopFinished(done, time.Second) {
		t.Fatal("recovered session did not stop")
	}
}

func TestLeaderSessionsParentCancellationInterruptsRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var attempts atomic.Int32
	failed := make(chan struct{})
	campaign := func(context.Context, string) (context.Context, error) {
		attempts.Add(1)
		close(failed)
		return nil, errors.New("temporary API outage")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		runLeaderSessions(ctx, quietLogger(), campaign, time.Hour)
	}()
	<-failed
	cancel()
	if !loopFinished(done, time.Second) {
		t.Fatal("parent cancellation did not interrupt retry backoff")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("campaign attempts = %d, want 1", got)
	}
}

func TestLeaderSessionsParentCancellationStopsAnIndependentLeaderContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan int, 1)
	var attempts atomic.Int32
	campaign := func(context.Context, string) (context.Context, error) {
		attempts.Add(1)
		return context.Background(), nil
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		runLeaderSessions(ctx, quietLogger(), campaign, time.Millisecond, func(session context.Context) {
			started <- 1
			<-session.Done()
		})
	}()
	receiveEpoch(t, started, 1)
	cancel()
	if !loopFinished(done, time.Second) {
		t.Fatal("parent cancellation did not cancel and join workers")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("parent cancellation started a new campaign: %d", got)
	}
}
