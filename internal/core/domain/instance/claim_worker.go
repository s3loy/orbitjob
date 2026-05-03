package instance

import (
	"strings"
	"time"
)

type WorkerClaimInput struct {
	TenantID       string
	WorkerID       string
	Limit          int
	LeaseExpiresAt time.Time
	Now            time.Time
}

type WorkerClaimSpec struct {
	TenantID       string
	WorkerID       string
	Limit          int
	LeaseExpiresAt time.Time
	Now            time.Time
}

func NormalizeWorkerClaim(in WorkerClaimInput) (WorkerClaimSpec, error) {
	tenantID := strings.TrimSpace(in.TenantID)
	if tenantID == "" {
		tenantID = DefaultTenantID
	}
	if len(tenantID) > 64 {
		return WorkerClaimSpec{}, validationError("tenant_id", "must be <= 64 characters")
	}

	workerID := strings.TrimSpace(in.WorkerID)
	if workerID == "" {
		return WorkerClaimSpec{}, validationError("worker_id", "is required")
	}
	if len(workerID) > 64 {
		return WorkerClaimSpec{}, validationError("worker_id", "must be <= 64 characters")
	}

	limit := in.Limit
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}

	if in.LeaseExpiresAt.IsZero() {
		return WorkerClaimSpec{}, validationError("lease_expires_at", "is required")
	}
	if in.Now.IsZero() {
		return WorkerClaimSpec{}, validationError("now", "is required")
	}
	if !in.LeaseExpiresAt.After(in.Now) {
		return WorkerClaimSpec{}, validationError("lease_expires_at", "must be after now")
	}

	return WorkerClaimSpec{
		TenantID:       tenantID,
		WorkerID:       workerID,
		Limit:          limit,
		LeaseExpiresAt: in.LeaseExpiresAt.UTC(),
		Now:            in.Now.UTC(),
	}, nil
}
