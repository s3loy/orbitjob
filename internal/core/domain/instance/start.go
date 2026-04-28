package instance

import (
	"strings"
	"time"
)

type StartInput struct {
	TenantID   string
	InstanceID int64
	WorkerID   string
	Now        time.Time
}

type StartSpec struct {
	TenantID   string
	InstanceID int64
	WorkerID   string
	StartedAt  time.Time
}

func NormalizeStart(in StartInput) (StartSpec, error) {
	tenantID := strings.TrimSpace(in.TenantID)
	if tenantID == "" {
		tenantID = DefaultTenantID
	}
	if len(tenantID) > 64 {
		return StartSpec{}, validationError("tenant_id", "must be <= 64 characters")
	}

	if in.InstanceID < 1 {
		return StartSpec{}, validationError("instance_id", "must be >= 1")
	}

	workerID := strings.TrimSpace(in.WorkerID)
	if workerID == "" {
		return StartSpec{}, validationError("worker_id", "is required")
	}
	if len(workerID) > 64 {
		return StartSpec{}, validationError("worker_id", "must be <= 64 characters")
	}

	if in.Now.IsZero() {
		return StartSpec{}, validationError("now", "is required")
	}

	return StartSpec{
		TenantID:   tenantID,
		InstanceID: in.InstanceID,
		WorkerID:   workerID,
		StartedAt:  in.Now.UTC(),
	}, nil
}
