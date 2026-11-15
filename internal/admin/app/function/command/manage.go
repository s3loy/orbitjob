// Package command manages tenant-owned Function configuration rows.
package command

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"orbitjob/internal/core/domain/function"
	"orbitjob/internal/domain/validation"
	"orbitjob/internal/platform/metrics"
)

type Repository interface {
	CreateManaged(context.Context, function.ManagedWrite) (function.Definition, bool, error)
	UpdateManaged(context.Context, function.ManagedWrite) (function.Definition, error)
}

type Manager struct{ repo Repository }

func NewManager(repo Repository) *Manager { return &Manager{repo: repo} }

type Input struct {
	Definition      function.Definition
	ActorID         string
	ResourceGroupID string
	IdempotencyKey  string
}

type CreateResult struct {
	Definition function.Definition
	Created    bool
}

func (m *Manager) Create(ctx context.Context, in Input) (CreateResult, error) {
	in.Definition.ID, in.Definition.Version = 0, 1
	// Request-owned labels never select the authorization scope.
	in.Definition.ResourceGroupID = in.ResourceGroupID
	write, err := normalize(in)
	if err != nil {
		return CreateResult{}, err
	}
	if write.IdempotencyKey == "" {
		return CreateResult{}, validation.New("idempotency_key", "is required for function creation")
	}
	encoded, err := json.Marshal(write.Definition)
	if err != nil {
		return CreateResult{}, validation.New("definition", "must contain JSON values")
	}
	hash := sha256.Sum256(encoded)
	write.PayloadHash = hex.EncodeToString(hash[:])
	if m.repo == nil {
		return CreateResult{}, fmt.Errorf("function management repository is required")
	}
	d, created, err := m.repo.CreateManaged(ctx, write)
	if err != nil {
		return CreateResult{}, err
	}
	outcome := "created"
	if !created {
		outcome = "replayed"
	}
	metrics.FunctionDefinitionWritesTotal.WithLabelValues(outcome).Inc()
	return CreateResult{Definition: d, Created: created}, nil
}

func (m *Manager) Update(ctx context.Context, in Input) (function.Definition, error) {
	if in.Definition.ID < 1 {
		return function.Definition{}, validation.New("id", "must be >= 1")
	}
	if in.Definition.Version < 1 {
		return function.Definition{}, validation.New("expected_version", "must be >= 1")
	}
	// Updates cannot move a definition into a different group.
	in.Definition.ResourceGroupID = ""
	write, err := normalize(in)
	if err != nil {
		return function.Definition{}, err
	}
	if m.repo == nil {
		return function.Definition{}, fmt.Errorf("function management repository is required")
	}
	d, err := m.repo.UpdateManaged(ctx, write)
	if err != nil {
		return function.Definition{}, err
	}
	metrics.FunctionDefinitionWritesTotal.WithLabelValues("updated").Inc()
	return d, nil
}

func normalize(in Input) (function.ManagedWrite, error) {
	d, err := function.NormalizeManagedDefinition(in.Definition)
	if err != nil {
		return function.ManagedWrite{}, err
	}
	actor := strings.TrimSpace(in.ActorID)
	if actor == "" || len(actor) > 64 {
		return function.ManagedWrite{}, validation.New("actor_id", "must contain between 1 and 64 characters")
	}
	key := strings.TrimSpace(in.IdempotencyKey)
	if len(key) > 128 {
		return function.ManagedWrite{}, validation.New("idempotency_key", "must be <= 128 characters")
	}
	if in.ResourceGroupID != "" && len(in.ResourceGroupID) != 26 {
		return function.ManagedWrite{}, validation.New("resource_group_id", "must be a 26-character group id")
	}
	return function.ManagedWrite{Definition: d, ActorID: actor, IdempotencyKey: key, CallerGroupID: in.ResourceGroupID}, nil
}
