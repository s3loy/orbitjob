package instance

import (
	"math"
	"strings"
	"time"
)

type CompleteInput struct {
	TenantID             string
	InstanceID           int64
	WorkerID             string
	Success              bool
	ResultCode           string
	ErrorMsg             string
	Now                  time.Time
	Attempt              int
	MaxAttempt           int
	RetryBackoffSec      int
	RetryBackoffStrategy string
}

type CompleteSpec struct {
	TenantID   string
	InstanceID int64
	WorkerID   string
	Status     string
	Attempt    int
	ResultCode *string
	ErrorMsg   *string
	FinishedAt time.Time
	RetryAt    *time.Time
}

func NormalizeComplete(in CompleteInput) (CompleteSpec, error) {
	tenantID := strings.TrimSpace(in.TenantID)
	if tenantID == "" {
		tenantID = DefaultTenantID
	}
	if len(tenantID) > 64 {
		return CompleteSpec{}, validationError("tenant_id", "must be <= 64 characters")
	}

	if in.InstanceID < 1 {
		return CompleteSpec{}, validationError("instance_id", "must be >= 1")
	}

	workerID := strings.TrimSpace(in.WorkerID)
	if workerID == "" {
		return CompleteSpec{}, validationError("worker_id", "is required")
	}
	if len(workerID) > 64 {
		return CompleteSpec{}, validationError("worker_id", "must be <= 64 characters")
	}

	if in.Now.IsZero() {
		return CompleteSpec{}, validationError("now", "is required")
	}

	resultCode := normalizeResultCode(in.ResultCode)
	errorMsg := normalizeErrorMsg(in.ErrorMsg)

	now := in.Now.UTC()

	var status string
	var retryAt *time.Time

	switch {
	case in.Success:
		status = StatusSuccess
		errorMsg = nil
	case in.Attempt < in.MaxAttempt:
		status = StatusRetryWait
		t := ComputeRetryAt(now, in.Attempt, in.RetryBackoffSec, in.RetryBackoffStrategy)
		retryAt = &t
	default:
		status = StatusFailed
	}

	return CompleteSpec{
		TenantID:   tenantID,
		InstanceID: in.InstanceID,
		WorkerID:   workerID,
		Status:     status,
		Attempt:    in.Attempt,
		ResultCode: resultCode,
		ErrorMsg:   errorMsg,
		FinishedAt: now,
		RetryAt:    retryAt,
	}, nil
}

func ComputeRetryAt(now time.Time, attempt, backoffSec int, strategy string) time.Time {
	if backoffSec <= 0 {
		return now
	}
	switch strategy {
	case "exponential":
		shift := min(max(attempt-1, 0), 30)
		multiplier := math.Pow(2, float64(shift))
		return now.Add(time.Duration(float64(backoffSec)*multiplier) * time.Second)
	default:
		return now.Add(time.Duration(backoffSec) * time.Second)
	}
}

func normalizeResultCode(s string) *string {
	v := strings.TrimSpace(s)
	if v == "" {
		return nil
	}
	if len(v) > 32 {
		v = v[:32]
	}
	return &v
}

func normalizeErrorMsg(s string) *string {
	v := strings.TrimSpace(s)
	if v == "" {
		return nil
	}
	return &v
}
