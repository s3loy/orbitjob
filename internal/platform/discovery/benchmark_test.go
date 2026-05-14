package discovery

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// benchReg is injected by benchmark_memory_test.go or benchmark_etcd_test.go.
var benchReg Registry

// cleanupKeys is set by benchmark_etcd_test.go to delete etcd keys by prefix.
var cleanupKeys func(prefix string)

func skipIfNoImpl(b *testing.B) {
	if benchReg == nil {
		b.Skip("benchmark implementation not injected (missing build tag)")
	}
}

// ---------------------------------------------------------------------------
// Register — single instance registration latency.
// ---------------------------------------------------------------------------

func BenchmarkRegister(b *testing.B) {
	skipIfNoImpl(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := fmt.Sprintf("bench-reg-%d-%d", b.N, i)
		ka, err := benchReg.Register(context.Background(), "bench-service", id, 30*time.Second)
		if err != nil {
			b.Fatalf("register: %v", err)
		}
		b.StopTimer()
		_ = ka(context.Background())
		b.StartTimer()
	}
	b.StopTimer()
	if cleanupKeys != nil {
		cleanupKeys("bench-service/")
	}
}

// ---------------------------------------------------------------------------
// ListInstances — query latency at different instance counts.
// ---------------------------------------------------------------------------

func BenchmarkListInstances(b *testing.B) {
	skipIfNoImpl(b)

	scales := []int{10, 100, 1000}
	for _, n := range scales {
		b.Run(fmt.Sprintf("instances=%d", n), func(b *testing.B) {
			svcName := fmt.Sprintf("bench-list-%d", n)
			b.StopTimer()
			ctx := context.Background()
			for i := 0; i < n; i++ {
				ka, err := benchReg.Register(ctx, svcName, fmt.Sprintf("inst-%d", i), 30*time.Second)
				if err != nil {
					b.Fatalf("register instance %d: %v", i, err)
				}
				_ = ka(ctx)
			}
			b.StartTimer()

			for i := 0; i < b.N; i++ {
				_, _ = benchReg.ListInstances(ctx, svcName)
			}
			b.StopTimer()
		})
	}
	if cleanupKeys != nil {
		cleanupKeys("bench-list-")
	}
}

// ---------------------------------------------------------------------------
// WatchLatency — time from Register to event delivery.
// ---------------------------------------------------------------------------

func BenchmarkWatchLatency(b *testing.B) {
	skipIfNoImpl(b)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Establish watch before benchmarking so events are not lost.
	events, err := benchReg.WatchInstances(ctx, "bench-service")
	if err != nil {
		b.Fatalf("watch: %v", err)
	}

	// Drain any initial events.
	drain(events, 100*time.Millisecond)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := fmt.Sprintf("bench-watch-%d-%d", b.N, i)

		ka, err := benchReg.Register(context.Background(), "bench-service", id, 30*time.Second)
		if err != nil {
			b.Fatalf("register: %v", err)
		}
		_ = ka(context.Background())

		select {
		case <-events:
			// Event received.
		case <-time.After(5 * time.Second):
			b.Fatal("watch timeout")
		}
	}
	b.StopTimer()
	if cleanupKeys != nil {
		cleanupKeys("bench-service/")
	}
}

func drain(ch <-chan Event, timeout time.Duration) {
	deadline := time.After(timeout)
	for {
		select {
		case <-ch:
		case <-deadline:
			return
		}
	}
}
