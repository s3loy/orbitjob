//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"

	domainworker "orbitjob/internal/core/domain/worker"
	"orbitjob/internal/platform/postgrestest"
)

func TestWorkerRepository_UpsertHeartbeat(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewWorkerRepository(db)

	firstNow := time.Now().UTC().Truncate(time.Second)
	firstSpec, err := domainworker.NormalizeHeartbeat(firstNow, domainworker.HeartbeatInput{
		TenantID:       "tenant-worker",
		WorkerID:       "worker-a",
		Status:         domainworker.StatusOnline,
		LeaseExpiresAt: firstNow.Add(30 * time.Second),
		Capacity:       2,
		Labels:         map[string]any{"queue": "video"},
	})
	if err != nil {
		t.Fatalf("NormalizeHeartbeat(first) error = %v", err)
	}

	firstOut, err := repo.UpsertHeartbeat(context.Background(), firstSpec)
	if err != nil {
		t.Fatalf("UpsertHeartbeat(first) error = %v", err)
	}
	if firstOut.Status != domainworker.StatusOnline {
		t.Fatalf("expected status=%q, got %q", domainworker.StatusOnline, firstOut.Status)
	}
	if firstOut.Capacity != 2 {
		t.Fatalf("expected capacity=%d, got %d", 2, firstOut.Capacity)
	}
	if firstOut.Labels["queue"] != "video" {
		t.Fatalf("expected labels.queue=%q, got %#v", "video", firstOut.Labels["queue"])
	}

	secondNow := firstNow.Add(10 * time.Second)
	secondSpec, err := domainworker.NormalizeHeartbeat(secondNow, domainworker.HeartbeatInput{
		TenantID:       "tenant-worker",
		WorkerID:       "worker-a",
		Status:         domainworker.StatusDraining,
		LeaseExpiresAt: secondNow.Add(45 * time.Second),
		Capacity:       4,
		Labels:         map[string]any{"queue": "image"},
	})
	if err != nil {
		t.Fatalf("NormalizeHeartbeat(second) error = %v", err)
	}

	secondOut, err := repo.UpsertHeartbeat(context.Background(), secondSpec)
	if err != nil {
		t.Fatalf("UpsertHeartbeat(second) error = %v", err)
	}
	if secondOut.Status != domainworker.StatusDraining {
		t.Fatalf("expected status=%q, got %q", domainworker.StatusDraining, secondOut.Status)
	}
	if secondOut.Capacity != 4 {
		t.Fatalf("expected capacity=%d, got %d", 4, secondOut.Capacity)
	}
	if secondOut.Labels["queue"] != "image" {
		t.Fatalf("expected labels.queue=%q, got %#v", "image", secondOut.Labels["queue"])
	}
	if !secondOut.CreatedAt.Equal(firstOut.CreatedAt) {
		t.Fatalf("expected created_at to stay unchanged")
	}

	var rowCount int
	err = db.QueryRowContext(context.Background(), `
		SELECT count(*)
		FROM workers
		WHERE tenant_id = $1 AND worker_id = $2
	`, "tenant-worker", "worker-a").Scan(&rowCount)
	if err != nil {
		t.Fatalf("count workers: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("expected 1 worker row, got %d", rowCount)
	}
}
