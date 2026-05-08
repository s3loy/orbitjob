package command

import (
	"context"
	"fmt"

	domaininstance "orbitjob/internal/core/domain/instance"
)

type instanceCanceler interface {
	Cancel(ctx context.Context, runID string, version int) (domaininstance.Snapshot, error)
}

type instanceVersionReader interface {
	GetByRunID(ctx context.Context, runID string) (domaininstance.Snapshot, error)
}

type CancelInstanceUseCase struct {
	reader instanceVersionReader
	repo   instanceCanceler
}

func NewCancelInstanceUseCase(reader instanceVersionReader, repo instanceCanceler) *CancelInstanceUseCase {
	return &CancelInstanceUseCase{reader: reader, repo: repo}
}

type CancelInstanceInput struct {
	RunID   string
	Version int
}

func (uc *CancelInstanceUseCase) Cancel(ctx context.Context, in CancelInstanceInput) (domaininstance.Snapshot, error) {
	current, err := uc.reader.GetByRunID(ctx, in.RunID)
	if err != nil {
		return domaininstance.Snapshot{}, fmt.Errorf("read instance for cancel: %w", err)
	}
	if current.Status != domaininstance.StatusDispatched && current.Status != domaininstance.StatusRunning {
		return domaininstance.Snapshot{}, fmt.Errorf("cannot cancel instance in status %q", current.Status)
	}

	out, err := uc.repo.Cancel(ctx, in.RunID, current.Version)
	if err != nil {
		return domaininstance.Snapshot{}, fmt.Errorf("cancel instance: %w", err)
	}
	return out, nil
}
