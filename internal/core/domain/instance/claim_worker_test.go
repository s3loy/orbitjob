package instance

import (
	"testing"
	"time"
)

func TestNormalizeWorkerClaim_Success(t *testing.T) {
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	lease := now.Add(60 * time.Second)
	in := WorkerClaimInput{
		TenantID:       "tenant-a",
		WorkerID:       "worker-1",
		Limit:          10,
		LeaseExpiresAt: lease,
		Now:            now,
	}
	spec, err := NormalizeWorkerClaim(in)
	if err != nil {
		t.Fatalf("NormalizeWorkerClaim() error = %v", err)
	}
	if spec.TenantID != "tenant-a" {
		t.Fatalf("expected tenant-a, got %q", spec.TenantID)
	}
	if spec.WorkerID != "worker-1" {
		t.Fatalf("expected worker-1, got %q", spec.WorkerID)
	}
	if spec.Limit != 10 {
		t.Fatalf("expected limit=10, got %d", spec.Limit)
	}
}

func TestNormalizeWorkerClaim_Defaults(t *testing.T) {
	now := time.Now().UTC()
	lease := now.Add(30 * time.Second)
	in := WorkerClaimInput{
		WorkerID:       "w",
		LeaseExpiresAt: lease,
		Now:            now,
	}
	spec, err := NormalizeWorkerClaim(in)
	if err != nil {
		t.Fatalf("NormalizeWorkerClaim() error = %v", err)
	}
	if spec.TenantID != DefaultTenantID {
		t.Fatalf("expected default tenant, got %q", spec.TenantID)
	}
	if spec.Limit != 1 {
		t.Fatalf("expected limit=1 (default), got %d", spec.Limit)
	}
}

func TestNormalizeWorkerClaim_EmptyWorkerID(t *testing.T) {
	now := time.Now().UTC()
	_, err := NormalizeWorkerClaim(WorkerClaimInput{
		LeaseExpiresAt: now.Add(60 * time.Second),
		Now:            now,
	})
	if err == nil {
		t.Fatal("expected error for empty worker_id")
	}
}

func TestNormalizeWorkerClaim_TenantIDTooLong(t *testing.T) {
	_, err := NormalizeWorkerClaim(WorkerClaimInput{
		TenantID:       string(make([]byte, 65)),
		WorkerID:       "w",
		LeaseExpiresAt: time.Now().Add(60 * time.Second),
		Now:            time.Now(),
	})
	if err == nil {
		t.Fatal("expected error for long tenant_id")
	}
}

func TestNormalizeWorkerClaim_WorkerIDTooLong(t *testing.T) {
	_, err := NormalizeWorkerClaim(WorkerClaimInput{
		WorkerID:       string(make([]byte, 65)),
		LeaseExpiresAt: time.Now().Add(60 * time.Second),
		Now:            time.Now(),
	})
	if err == nil {
		t.Fatal("expected error for long worker_id")
	}
}

func TestNormalizeWorkerClaim_LimitBounds(t *testing.T) {
	now := time.Now().UTC()
	lease := now.Add(60 * time.Second)

	// Limit 0 → default 1
	spec, err := NormalizeWorkerClaim(WorkerClaimInput{
		WorkerID:       "w",
		Limit:          0,
		LeaseExpiresAt: lease,
		Now:            now,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Limit != 1 {
		t.Fatalf("expected limit=1, got %d", spec.Limit)
	}

	// Limit > 100 → capped
	spec, err = NormalizeWorkerClaim(WorkerClaimInput{
		WorkerID:       "w",
		Limit:          200,
		LeaseExpiresAt: lease,
		Now:            now,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Limit != 100 {
		t.Fatalf("expected limit=100, got %d", spec.Limit)
	}
}

func TestNormalizeWorkerClaim_LeaseBeforeNow(t *testing.T) {
	now := time.Now().UTC()
	_, err := NormalizeWorkerClaim(WorkerClaimInput{
		WorkerID:       "w",
		LeaseExpiresAt: now.Add(-1 * time.Second),
		Now:            now,
	})
	if err == nil {
		t.Fatal("expected error for lease before now")
	}
}

func TestNormalizeWorkerClaim_ZeroNow(t *testing.T) {
	_, err := NormalizeWorkerClaim(WorkerClaimInput{
		WorkerID:       "w",
		LeaseExpiresAt: time.Now(),
	})
	if err == nil {
		t.Fatal("expected error for zero now")
	}
}

func TestNormalizeWorkerClaim_LeaseZero(t *testing.T) {
	_, err := NormalizeWorkerClaim(WorkerClaimInput{
		WorkerID: "w",
		Now:      time.Now(),
	})
	if err == nil {
		t.Fatal("expected error for zero lease")
	}
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkNormalizeWorkerClaim(b *testing.B) {
	now := time.Now().UTC()
	lease := now.Add(60 * time.Second)

	tests := []struct {
		name  string
		input WorkerClaimInput
	}{
		{"valid", WorkerClaimInput{
			TenantID: "tenant-a", WorkerID: "worker-1", Limit: 10,
			LeaseExpiresAt: lease, Now: now,
		}},
		{"default_tenant", WorkerClaimInput{
			WorkerID: "w", Limit: 5, LeaseExpiresAt: lease, Now: now,
		}},
		{"limit_clamp_low", WorkerClaimInput{
			WorkerID: "w", Limit: 0, LeaseExpiresAt: lease, Now: now,
		}},
		{"limit_clamp_high", WorkerClaimInput{
			WorkerID: "w", Limit: 200, LeaseExpiresAt: lease, Now: now,
		}},
		{"validation_error", WorkerClaimInput{}},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_, _ = NormalizeWorkerClaim(tt.input)
			}
		})
	}
}
