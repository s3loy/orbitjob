package command

import (
	"context"

	"orbitjob/internal/core/domain/apikey"
	"orbitjob/internal/core/domain/audit"
)

// RenameInput changes display metadata without changing credential authority.
type RenameInput struct {
	TenantID string
	ID       string
	Name     string
	ActorID  string
}

type APIKeyRenameResult struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type apiKeyRenamer interface {
	Rename(context.Context, string, string, string, audit.Event) error
}

type Renamer struct{ repo apiKeyRenamer }

func NewRenamer(repo apiKeyRenamer) *Renamer { return &Renamer{repo: repo} }

func (r *Renamer) Rename(ctx context.Context, in RenameInput) (APIKeyRenameResult, error) {
	if err := apikey.ValidateName(in.Name); err != nil {
		return APIKeyRenameResult{}, err
	}
	err := r.repo.Rename(ctx, in.TenantID, in.ID, in.Name, audit.Event{
		TenantID: in.TenantID, ActorID: in.ActorID, EventType: audit.EventUpdate,
		ResourceType: audit.ResourceAPIKey, ResourceID: in.ID,
		Diff: map[string]any{"name": in.Name},
	})
	if err != nil {
		return APIKeyRenameResult{}, err
	}
	return APIKeyRenameResult{ID: in.ID, Name: in.Name}, nil
}
