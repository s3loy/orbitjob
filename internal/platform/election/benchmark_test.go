package election

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// benchCoord is injected by benchmark_memory_test.go or benchmark_etcd_test.go.
var benchCoord Coordinator

// cleanupKeys is set by benchmark_etcd_test.go to delete etcd keys by prefix.
var cleanupKeys func(prefix string)

func skipIfNoImpl(b *testing.B) {
	if benchCoord == nil {
		b.Skip("benchmark implementation not injected (missing build tag)")
	}
}

// ---------------------------------------------------------------------------
// Campaign — measure first-time leader election latency.
// Each iteration uses a fresh election name to avoid contention.
// ---------------------------------------------------------------------------

func BenchmarkCampaign(b *testing.B) {
	skipIfNoImpl(b)

	seed := time.Now().UnixNano()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		name := fmt.Sprintf("bench-campaign-%d-%d-%d", seed, b.N, i)
		ctx, err := benchCoord.Campaign(context.Background(), name)
		if err != nil {
			b.Fatalf("campaign: %v", err)
		}
		_ = ctx
	}
	b.StopTimer()
	if cleanupKeys != nil {
		cleanupKeys("bench-campaign-")
	}
}

// ---------------------------------------------------------------------------
// TryLock — no contention, unique lock per iteration.
// ---------------------------------------------------------------------------

func BenchmarkTryLock(b *testing.B) {
	skipIfNoImpl(b)

	seed := time.Now().UnixNano()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		name := fmt.Sprintf("bench-lock-%d-%d-%d", seed, b.N, i)
		unlock, err := benchCoord.TryLock(context.Background(), name)
		if err != nil {
			b.Fatalf("try lock: %v", err)
		}
		b.StopTimer()
		_ = unlock()
		b.StartTimer()
	}
	b.StopTimer()
	if cleanupKeys != nil {
		cleanupKeys("bench-lock-")
	}
}

// ---------------------------------------------------------------------------
// TryLockContention — multiple goroutines compete for one lock.
// ---------------------------------------------------------------------------

func BenchmarkTryLockContention(b *testing.B) {
	skipIfNoImpl(b)

	levels := []int{2, 4, 8}
	for _, c := range levels {
		b.Run(fmt.Sprintf("concurrency=%d", c), func(b *testing.B) {
			lockName := fmt.Sprintf("bench-contention-%d", time.Now().UnixNano())

			b.SetParallelism(c)
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					unlock, err := benchCoord.TryLock(context.Background(), lockName)
					if err == nil {
						// Simulate brief critical section.
						time.Sleep(time.Millisecond)
						_ = unlock()
					}
				}
			})
		})
	}
	if cleanupKeys != nil {
		cleanupKeys("bench-contention-")
	}
}
