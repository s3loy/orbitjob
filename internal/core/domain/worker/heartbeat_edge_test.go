package worker

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeHeartbeat_EdgeCases(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	t.Run("worker_id too long", func(t *testing.T) {
		_, err := NormalizeHeartbeat(now, HeartbeatInput{
			WorkerID:       strings.Repeat("w", 65),
			LeaseExpiresAt: now.Add(time.Minute),
		})
		if err == nil {
			t.Fatal("expected validation error, got nil")
		}
	})

	t.Run("lease_expires_at zero", func(t *testing.T) {
		_, err := NormalizeHeartbeat(now, HeartbeatInput{
			WorkerID: "worker-a",
		})
		if err == nil {
			t.Fatal("expected validation error for zero lease_expires_at, got nil")
		}
	})

	t.Run("now zero", func(t *testing.T) {
		_, err := NormalizeHeartbeat(time.Time{}, HeartbeatInput{
			WorkerID:       "worker-a",
			LeaseExpiresAt: now,
		})
		if err == nil {
			t.Fatal("expected validation error for zero now, got nil")
		}
	})

	t.Run("non-json labels", func(t *testing.T) {
		_, err := NormalizeHeartbeat(now, HeartbeatInput{
			WorkerID:       "worker-a",
			LeaseExpiresAt: now.Add(time.Minute),
			Labels:         map[string]any{"fn": func() {}},
		})
		if err == nil {
			t.Fatal("expected validation error for non-JSON labels, got nil")
		}
	})
}

func TestNormalizeHeartbeat_ExplicitValues(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	lease := now.Add(60 * time.Second)

	t.Run("explicit tenant", func(t *testing.T) {
		spec, err := NormalizeHeartbeat(now, HeartbeatInput{
			TenantID:       "tenant-a",
			WorkerID:       "worker-1",
			LeaseExpiresAt: lease,
		})
		if err != nil {
			t.Fatalf("NormalizeHeartbeat() error = %v", err)
		}
		if spec.TenantID != "tenant-a" {
			t.Fatalf("expected tenant-a, got %q", spec.TenantID)
		}
	})

	t.Run("status offline", func(t *testing.T) {
		spec, err := NormalizeHeartbeat(now, HeartbeatInput{
			WorkerID:       "worker-1",
			Status:         StatusOffline,
			LeaseExpiresAt: lease,
		})
		if err != nil {
			t.Fatalf("NormalizeHeartbeat() error = %v", err)
		}
		if spec.Status != StatusOffline {
			t.Fatalf("expected offline, got %q", spec.Status)
		}
	})

	t.Run("status draining", func(t *testing.T) {
		spec, err := NormalizeHeartbeat(now, HeartbeatInput{
			WorkerID:       "worker-1",
			Status:         StatusDraining,
			LeaseExpiresAt: lease,
		})
		if err != nil {
			t.Fatalf("NormalizeHeartbeat() error = %v", err)
		}
		if spec.Status != StatusDraining {
			t.Fatalf("expected draining, got %q", spec.Status)
		}
	})

	t.Run("capacity greater than one", func(t *testing.T) {
		spec, err := NormalizeHeartbeat(now, HeartbeatInput{
			WorkerID:       "worker-1",
			Capacity:       8,
			LeaseExpiresAt: lease,
		})
		if err != nil {
			t.Fatalf("NormalizeHeartbeat() error = %v", err)
		}
		if spec.Capacity != 8 {
			t.Fatalf("expected capacity=8, got %d", spec.Capacity)
		}
	})

	t.Run("nil labels", func(t *testing.T) {
		spec, err := NormalizeHeartbeat(now, HeartbeatInput{
			WorkerID:       "worker-1",
			LeaseExpiresAt: lease,
		})
		if err != nil {
			t.Fatalf("NormalizeHeartbeat() error = %v", err)
		}
		if spec.Labels == nil {
			t.Fatal("expected non-nil labels map for nil input")
		}
		if len(spec.Labels) != 0 {
			t.Fatalf("expected empty labels, got %d entries", len(spec.Labels))
		}
	})
}
