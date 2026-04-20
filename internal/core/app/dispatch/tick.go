package dispatch

import (
	"context"

	domaininstance "orbitjob/internal/core/domain/instance"
)

type dispatcher interface {
	DispatchOne(
		ctx context.Context,
		spec domaininstance.ClaimSpec,
		decide func(domaininstance.DispatchInput) domaininstance.DispatchDecision,
	) (_ domaininstance.Snapshot, found bool, _ error)
}

// TickUseCase executes one bounded dispatch batch.
type TickUseCase struct {
	repo dispatcher
}

func NewTickUseCase(repo dispatcher) *TickUseCase {
	return &TickUseCase{repo: repo}
}

// RunBatch handles at most limit claimable instances in one tick.
func (uc *TickUseCase) RunBatch(ctx context.Context, spec domaininstance.ClaimSpec, limit int) (int, error) {
	if limit < 1 {
		limit = 1
	}

	handled := 0
	for i := 0; i < limit; i++ {
		_, found, err := uc.repo.DispatchOne(ctx, spec, domaininstance.DecideDispatch)
		if err != nil {
			return handled, err
		}
		if !found {
			break
		}
		handled++
	}

	return handled, nil
}
