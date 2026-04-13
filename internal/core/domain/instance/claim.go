package instance

import (
	"strings"
	"time"
)

type ClaimInput struct {
	TenantID       string
	WorkerID       string
	LeaseExpiresAt time.Time
	Now            time.Time
}

type ClaimSpec struct {
	TenantID       string
	WorkerID       string
	LeaseExpiresAt time.Time
	Now            time.Time
}

func NormalizeClaim(in ClaimInput) (ClaimSpec, error) {
	tenantID := strings.TrimSpace(in.TenantID)
	if tenantID == "" {
		tenantID = DefaultTenantID
	}
	if len(tenantID) > 64 {
		return ClaimSpec{}, validationError("tenant_id", "must be <= 64 characters")
	}

	workerID := strings.TrimSpace(in.WorkerID)
	if workerID == "" {
		return ClaimSpec{}, validationError("worker_id", "is required")
	}
	if len(workerID) > 64 {
		return ClaimSpec{}, validationError("worker_id", "must be <= 64 characters")
	}
	if in.LeaseExpiresAt.IsZero() {
		return ClaimSpec{}, validationError("lease_expires_at", "is required")
	}
	if in.Now.IsZero() {
		return ClaimSpec{}, validationError("now", "is required")
	}
	if !in.LeaseExpiresAt.After(in.Now) {
		return ClaimSpec{}, validationError("lease_expires_at", "must be after now")
	}

	return ClaimSpec{
		TenantID:       tenantID,
		WorkerID:       workerID,
		LeaseExpiresAt: in.LeaseExpiresAt.UTC(),
		Now:            in.Now.UTC(),
	}, nil
}
