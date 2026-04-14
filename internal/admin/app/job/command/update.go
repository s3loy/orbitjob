package command

import (
	"context"

	domainjob "orbitjob/internal/core/domain/job"
)

type jobUpdater interface {
	Update(ctx context.Context, in domainjob.UpdateSpec, changedBy string) (domainjob.Snapshot, error)
}

type UpdateJobUseCase struct {
	repo  jobUpdater
	clock clock
}

func NewUpdateJobUseCase(repo jobUpdater) *UpdateJobUseCase {
	return &UpdateJobUseCase{
		repo:  repo,
		clock: realClock{},
	}
}

func (uc *UpdateJobUseCase) Update(ctx context.Context, in UpdateInput) (UpdateResult, error) {
	spec, err := domainjob.NormalizeUpdate(uc.clock.Now(), domainjob.UpdateInput{
		ID:                   in.ID,
		TenantID:             in.TenantID,
		Version:              in.Version,
		Name:                 in.Name,
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
		return UpdateResult{}, err
	}

	out, err := uc.repo.Update(ctx, spec, in.ChangedBy)
	if err != nil {
		return UpdateResult{}, err
	}

	return UpdateResult{
		ID:        out.ID,
		Name:      out.Name,
		TenantID:  out.TenantID,
		Status:    out.Status,
		Version:   out.Version,
		NextRunAt: out.NextRunAt,
		CreatedAt: out.CreatedAt,
		UpdatedAt: out.UpdatedAt,
	}, nil
}
