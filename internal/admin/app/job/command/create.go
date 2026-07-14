package command

import (
	"context"
	"time"

	domainjob "orbitjob/internal/core/domain/job"
	"orbitjob/internal/platform/metrics"
)

type jobCreator interface {
	Create(ctx context.Context, in domainjob.CreateSpec) (domainjob.Snapshot, error)
	CountActiveByTenant(ctx context.Context, tenantID string) (int, error)
}

type tenantQuotaReader interface {
	GetQuota(ctx context.Context, tenantID string) (map[string]any, error)
}

type clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now().UTC()
}

type CreateJobUseCase struct {
	repo        jobCreator
	quotaReader tenantQuotaReader
	clock       clock
}

func NewCreateJobUseCase(repo jobCreator, quotaReader tenantQuotaReader) *CreateJobUseCase {
	return &CreateJobUseCase{
		repo:        repo,
		quotaReader: quotaReader,
		clock:       realClock{},
	}
}

func (uc *CreateJobUseCase) Create(ctx context.Context, in CreateInput) (CreateResult, error) {
	if uc.quotaReader != nil {
		quotas, err := uc.quotaReader.GetQuota(ctx, in.TenantID)
		if err != nil {
			return CreateResult{}, err
		}
		if maxJobs, ok := getMaxJobs(quotas); ok {
			count, err := uc.repo.CountActiveByTenant(ctx, in.TenantID)
			if err != nil {
				return CreateResult{}, err
			}
			if count >= maxJobs {
				return CreateResult{}, domainjob.NewQuotaExceededError("max_jobs", maxJobs)
			}
		}
	}

	spec, err := domainjob.NormalizeCreate(uc.clock.Now(), domainjob.CreateInput{
		Name:                 in.Name,
		TenantID:             in.TenantID,
		Priority:             in.Priority,
		PartitionKey:         in.PartitionKey,
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
		return CreateResult{}, err
	}

	out, err := uc.repo.Create(ctx, spec)
	if err == nil {
		metrics.JobsTotal.WithLabelValues(spec.TenantID, spec.TriggerType).Inc()
	}

	return CreateResult{
		ID:        out.ID,
		Name:      out.Name,
		TenantID:  out.TenantID,
		Status:    out.Status,
		NextRunAt: out.NextRunAt,
		CreatedAt: out.CreatedAt,
		UpdatedAt: out.UpdatedAt,
	}, err
}

func getMaxJobs(quotas map[string]any) (int, bool) {
	if quotas == nil {
		return 0, false
	}
	v, ok := quotas["max_jobs"]
	if !ok {
		return 0, false
	}
	n, ok := toFloatInt(v)
	if !ok || n <= 0 {
		return 0, false
	}
	return n, true
}

func toFloatInt(v any) (int, bool) {
	switch x := v.(type) {
	case float64:
		return int(x), true
	case float32:
		return int(x), true
	case int:
		return x, true
	case int64:
		return int(x), true
	default:
		return 0, false
	}
}
