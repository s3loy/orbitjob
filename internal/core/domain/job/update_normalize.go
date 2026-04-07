package job

import "time"

// NormalizeUpdate validates and normalizes mutable job fields for update operations.
func NormalizeUpdate(now time.Time, in UpdateInput) (UpdateSpec, error) {
	if in.ID < 1 {
		return UpdateSpec{}, validationError("id", "must be >= 1")
	}
	if in.Version < 1 {
		return UpdateSpec{}, validationError("version", "must be >= 1")
	}

	createSpec, err := NormalizeCreate(now, CreateInput{
		Name:                 in.Name,
		TenantID:             in.TenantID,
		TriggerType:          in.TriggerType,
		CronExpr:             in.CronExpr,
		Timezone:             in.Timezone,
		HandlerType:          in.HandlerType,
		HandlerPayload:       in.HandlerPayload,
		TimeoutSec:           in.TimeoutSec,
		RetryLimit:           in.RetryLimit,
		RetryBackoffSec:      in.RetryBackoffSec,
		RetryBackoffStrategy: in.RetryBackoffStrategy,
		ConcurrencyPolicy:    in.ConcurrencyPolicy,
		MisfirePolicy:        in.MisfirePolicy,
	})
	if err != nil {
		return UpdateSpec{}, err
	}

	return UpdateSpec{
		ID:         in.ID,
		Version:    in.Version,
		CreateSpec: createSpec,
	}, nil
}
