package instance

import (
	"strings"
	"time"
)

type CreateInput struct {
	TenantID         string
	JobID            int64
	TriggerSource    string
	ScheduledAt      time.Time
	Priority         int
	PartitionKey     *string
	IdempotencyKey   *string
	IdempotencyScope string
	RoutingKey       *string
	MaxAttempt       int
	TraceID          *string
}

type CreateSpec struct {
	TenantID         string
	JobID            int64
	TriggerSource    string
	ScheduledAt      time.Time
	Priority         int
	PartitionKey     *string
	IdempotencyKey   *string
	IdempotencyScope string
	RoutingKey       *string
	MaxAttempt       int
	TraceID          *string
}

func NormalizeCreate(in CreateInput) (CreateSpec, error) {
	tenantID := strings.TrimSpace(in.TenantID)
	if tenantID == "" {
		tenantID = DefaultTenantID
	}
	if len(tenantID) > 64 {
		return CreateSpec{}, validationError("tenant_id", "must be <= 64 characters")
	}
	if in.JobID < 1 {
		return CreateSpec{}, validationError("job_id", "must be >= 1")
	}

	triggerSource := strings.TrimSpace(in.TriggerSource)
	if triggerSource == "" {
		triggerSource = TriggerSourceSchedule
	}
	if triggerSource != TriggerSourceSchedule && triggerSource != TriggerSourceManual {
		return CreateSpec{}, validationError("trigger_source", "must be one of: schedule, manual")
	}
	if in.ScheduledAt.IsZero() {
		return CreateSpec{}, validationError("scheduled_at", "is required")
	}
	if in.Priority < 0 {
		return CreateSpec{}, validationError("priority", "must be >= 0")
	}

	partitionKey, err := normalizeOptionalString(in.PartitionKey, "partition_key", 64)
	if err != nil {
		return CreateSpec{}, err
	}
	idempotencyKey, err := normalizeOptionalString(in.IdempotencyKey, "idempotency_key", 128)
	if err != nil {
		return CreateSpec{}, err
	}

	idempotencyScope := strings.TrimSpace(in.IdempotencyScope)
	if idempotencyScope == "" {
		idempotencyScope = DefaultIdempotencyScope
	}
	if len(idempotencyScope) > 64 {
		return CreateSpec{}, validationError("idempotency_scope", "must be <= 64 characters")
	}

	routingKey, err := normalizeOptionalString(in.RoutingKey, "routing_key", 128)
	if err != nil {
		return CreateSpec{}, err
	}
	traceID, err := normalizeOptionalString(in.TraceID, "trace_id", 64)
	if err != nil {
		return CreateSpec{}, err
	}

	maxAttempt := in.MaxAttempt
	if maxAttempt == 0 {
		maxAttempt = 1
	}
	if maxAttempt < 1 {
		return CreateSpec{}, validationError("max_attempt", "must be >= 1")
	}

	return CreateSpec{
		TenantID:         tenantID,
		JobID:            in.JobID,
		TriggerSource:    triggerSource,
		ScheduledAt:      in.ScheduledAt.UTC(),
		Priority:         in.Priority,
		PartitionKey:     partitionKey,
		IdempotencyKey:   idempotencyKey,
		IdempotencyScope: idempotencyScope,
		RoutingKey:       routingKey,
		MaxAttempt:       maxAttempt,
		TraceID:          traceID,
	}, nil
}

func normalizeOptionalString(in *string, field string, maxLen int) (*string, error) {
	if in == nil {
		return nil, nil
	}

	value := strings.TrimSpace(*in)
	if value == "" {
		return nil, nil
	}
	if len(value) > maxLen {
		return nil, validationErrorf(field, "must be <= %d characters", maxLen)
	}

	return &value, nil
}
