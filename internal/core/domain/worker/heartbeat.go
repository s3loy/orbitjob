package worker

import (
	"encoding/json"
	"strings"
	"time"
)

type HeartbeatInput struct {
	TenantID       string
	WorkerID       string
	Status         string
	LeaseExpiresAt time.Time
	Capacity       int
	Labels         map[string]any
}

type HeartbeatSpec struct {
	TenantID        string
	WorkerID        string
	Status          string
	LastHeartbeatAt time.Time
	LeaseExpiresAt  time.Time
	Capacity        int
	Labels          map[string]any
}

func NormalizeHeartbeat(now time.Time, in HeartbeatInput) (HeartbeatSpec, error) {
	tenantID := strings.TrimSpace(in.TenantID)
	if tenantID == "" {
		tenantID = DefaultTenantID
	}
	if len(tenantID) > 64 {
		return HeartbeatSpec{}, validationError("tenant_id", "must be <= 64 characters")
	}

	workerID := strings.TrimSpace(in.WorkerID)
	if workerID == "" {
		return HeartbeatSpec{}, validationError("worker_id", "is required")
	}
	if len(workerID) > 64 {
		return HeartbeatSpec{}, validationError("worker_id", "must be <= 64 characters")
	}

	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = StatusOnline
	}
	switch status {
	case StatusOnline, StatusOffline, StatusDraining:
	default:
		return HeartbeatSpec{}, validationError("status", "must be one of: online, offline, draining")
	}

	if now.IsZero() {
		return HeartbeatSpec{}, validationError("last_heartbeat_at", "is required")
	}
	if in.LeaseExpiresAt.IsZero() {
		return HeartbeatSpec{}, validationError("lease_expires_at", "is required")
	}
	if !in.LeaseExpiresAt.After(now) {
		return HeartbeatSpec{}, validationError("lease_expires_at", "must be after last_heartbeat_at")
	}

	capacity := in.Capacity
	if capacity == 0 {
		capacity = 1
	}
	if capacity < 1 {
		return HeartbeatSpec{}, validationError("capacity", "must be >= 1")
	}

	labels := cloneLabels(in.Labels)
	if _, err := json.Marshal(labels); err != nil {
		return HeartbeatSpec{}, &ValidationError{
			Field:   "labels",
			Message: "must be JSON serializable",
			Cause:   err,
		}
	}

	return HeartbeatSpec{
		TenantID:        tenantID,
		WorkerID:        workerID,
		Status:          status,
		LastHeartbeatAt: now.UTC(),
		LeaseExpiresAt:  in.LeaseExpiresAt.UTC(),
		Capacity:        capacity,
		Labels:          labels,
	}, nil
}

func cloneLabels(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}

	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
