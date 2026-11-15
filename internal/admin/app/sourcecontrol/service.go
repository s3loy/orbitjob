package sourcecontrol

import (
	"context"

	"orbitjob/internal/domain/sourcecontrol"
	"orbitjob/internal/domain/validation"
)

type Target struct {
	TenantID        string
	Kind            string
	ID              int64
	Mode            string
	SourceUID       string
	ResourceGroupID string
}

type Change struct {
	Version         int64
	SchedulePaused  bool
	ExecutionPaused bool
}

type Repository interface {
	Resolve(context.Context, string, string, int64) (Target, error)
	Read(context.Context, Target) (sourcecontrol.State, error)
	Update(context.Context, Target, Change, string) (sourcecontrol.State, error)
}

type Service struct{ repository Repository }

func NewService(repository Repository) *Service { return &Service{repository: repository} }

func (s *Service) Resolve(ctx context.Context, tenant, kind string, id int64) (Target, error) {
	if tenant == "" {
		return Target{}, validation.New("tenant_id", "is required")
	}
	if id < 1 {
		return Target{}, validation.New("id", "must be positive")
	}
	if kind != "jobs" && kind != "workflows" && kind != "functions" && kind != "checks" {
		return Target{}, validation.New("kind", "unsupported execution source")
	}
	return s.repository.Resolve(ctx, tenant, kind, id)
}

func (s *Service) Get(ctx context.Context, target Target) (sourcecontrol.State, error) {
	return s.repository.Read(ctx, target)
}

func (s *Service) Update(ctx context.Context, target Target, change Change, actor string) (sourcecontrol.State, error) {
	if change.Version < 0 {
		return sourcecontrol.State{}, validation.New("version", "must be nonnegative")
	}
	if actor == "" {
		return sourcecontrol.State{}, validation.New("actor", "authenticated actor is required")
	}
	return s.repository.Update(ctx, target, change, actor)
}
