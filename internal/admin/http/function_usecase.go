package http

import (
	"context"
	"fmt"
	"strings"
	"time"

	domainfunction "orbitjob/internal/core/domain/function"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
)

// The function use cases live beside the handlers that call them until the
// storage workstream lands its repositories; they consume the store contracts
// through the narrow consumer-side interfaces below, so the implementations
// slot in without touching this file.

// functionReader reads one live function definition tenant-scoped. It is the
// FunctionStore contract's GetForTenant.
type functionReader interface {
	GetForTenant(ctx context.Context, tenantID string, id int64) (def domainfunction.Definition, found bool, err error)
}

// functionLister lists a tenant's live function definitions. It is the
// FunctionStore contract's ListForTenant; the group filter for scoped callers
// is applied here, on the results, because visibility is a read-model question
// the row store answers uniformly.
type functionLister interface {
	ListForTenant(ctx context.Context, tenantID string) ([]domainfunction.Definition, error)
}

// functionRunLister lists a function's terminal invocations from the
// function_runs read model. It is the FunctionRunStore contract's
// RunsByFunction.
type functionRunLister interface {
	RunsByFunction(ctx context.Context, tenantID string, functionID int64, limit int) ([]domainfunction.FunctionRun, error)
}

// functionRunGetter reads one terminal invocation by its deterministic run id.
// The function_runs read model keys history by run_id, so a single invocation
// is addressable without its function's list. No contract declares this yet --
// the FunctionRunStore carries the list only -- so the storage workstream owes
// this method an implementation beside RunsByFunction.
type functionRunGetter interface {
	RunByRunID(ctx context.Context, tenantID string, functionID int64, runID string) (domainfunction.FunctionRun, bool, error)
}

// ==================== List ====================

// ListFunctionsUseCase lists the caller's tenant's function definitions.
type ListFunctionsUseCase struct {
	lister functionLister
}

func NewListFunctionsUseCase(lister functionLister) *ListFunctionsUseCase {
	return &ListFunctionsUseCase{lister: lister}
}

// FunctionListInput is the normalized list input. ResourceGroupID is the
// caller's key scope; a function row carries an optional group, so a scoped
// caller sees only its group's functions instead of being refused outright --
// the checks read model's exact semantics.
type FunctionListInput struct {
	TenantID        string
	ResourceGroupID string
	Limit           int
	Offset          int
}

// FunctionItem is the API's function definition shape.
type FunctionItem struct {
	ID              int64          `json:"id"`
	TenantID        string         `json:"tenant_id"`
	ResourceGroupID string         `json:"resource_group_id"`
	Name            string         `json:"name"`
	Description     string         `json:"description"`
	Status          string         `json:"status"`
	Image           string         `json:"image"`
	Command         []string       `json:"command"`
	Args            []string       `json:"args"`
	TimeoutSeconds  int            `json:"timeout_seconds"`
	RetryLimit      int            `json:"retry_limit"`
	HistorySuccess  int            `json:"history_success"`
	HistoryFailed   int            `json:"history_failed"`
	Labels          map[string]any `json:"labels"`
	Version         int            `json:"version"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

func newFunctionItem(def domainfunction.Definition) FunctionItem {
	return FunctionItem{
		ID:              def.ID,
		TenantID:        def.TenantID,
		ResourceGroupID: def.ResourceGroupID,
		Name:            def.Name,
		Description:     def.Description,
		Status:          def.Status,
		Image:           def.Image,
		Command:         def.Command,
		Args:            def.Args,
		TimeoutSeconds:  def.TimeoutSeconds,
		RetryLimit:      def.RetryLimit,
		HistorySuccess:  def.HistorySuccess,
		HistoryFailed:   def.HistoryFailed,
		Labels:          def.Labels,
		Version:         def.Version,
		CreatedAt:       def.CreatedAt,
		UpdatedAt:       def.UpdatedAt,
	}
}

// List returns the tenant's functions, newest first is the store's contract,
// narrowed to the caller's group when the key is scoped.
func (uc *ListFunctionsUseCase) List(ctx context.Context, in FunctionListInput) ([]FunctionItem, error) {
	tenantID, err := normalizeTenantID(in.TenantID)
	if err != nil {
		return nil, err
	}
	if uc.lister == nil {
		return nil, fmt.Errorf("function lister is required")
	}

	defs, err := uc.lister.ListForTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	limit := normalizeListLimit(in.Limit)
	out := make([]FunctionItem, 0, len(defs))
	skipped := 0
	for _, def := range defs {
		if !visibleToGroup(def.ResourceGroupID, in.ResourceGroupID) {
			continue
		}
		if skipped < in.Offset {
			skipped++
			continue
		}
		if len(out) == limit {
			break
		}
		out = append(out, newFunctionItem(def))
	}
	return out, nil
}

// ==================== Get ====================

// GetFunctionUseCase reads one function definition.
type GetFunctionUseCase struct {
	reader functionReader
}

func NewGetFunctionUseCase(reader functionReader) *GetFunctionUseCase {
	return &GetFunctionUseCase{reader: reader}
}

type FunctionGetInput struct {
	ID              int64
	TenantID        string
	ResourceGroupID string
}

// Get returns one function, or NotFound when it does not exist or is not in a
// scoped caller's group -- saying anything else would leak whether the
// function exists.
func (uc *GetFunctionUseCase) Get(ctx context.Context, in FunctionGetInput) (FunctionItem, error) {
	def, err := uc.get(ctx, in)
	if err != nil {
		return FunctionItem{}, err
	}
	return newFunctionItem(def), nil
}

func (uc *GetFunctionUseCase) get(ctx context.Context, in FunctionGetInput) (domainfunction.Definition, error) {
	tenantID, err := normalizeTenantID(in.TenantID)
	if err != nil {
		return domainfunction.Definition{}, err
	}
	if in.ID < 1 {
		return domainfunction.Definition{}, validation.New("id", "must be >= 1")
	}
	if uc.reader == nil {
		return domainfunction.Definition{}, fmt.Errorf("function reader is required")
	}

	def, found, err := uc.reader.GetForTenant(ctx, tenantID, in.ID)
	if err != nil {
		return domainfunction.Definition{}, err
	}
	if !found || !visibleToGroup(def.ResourceGroupID, in.ResourceGroupID) {
		return domainfunction.Definition{}, &resource.NotFoundError{Resource: "function", ID: in.ID}
	}
	return def, nil
}

// ==================== Function runs (read model) ====================

// ListFunctionRunsUseCase lists a function's terminal invocations from the
// function_runs read model. The model records outcomes only -- there is no
// pending or running state, because live progress lives on the JobRun custom
// resource and the ledger row.
type ListFunctionRunsUseCase struct {
	runs   functionRunLister
	reader functionReader
}

func NewListFunctionRunsUseCase(runs functionRunLister, reader functionReader) *ListFunctionRunsUseCase {
	return &ListFunctionRunsUseCase{runs: runs, reader: reader}
}

type FunctionRunListInput struct {
	FunctionID      int64
	TenantID        string
	ResourceGroupID string
	Limit           int
}

// FunctionRunItem is one read-model row. StartedAt, FinishedAt and DurationMs
// are nullable at the boundary: a canceled invocation may have been stopped
// before its first attempt began.
type FunctionRunItem struct {
	ID          int64      `json:"id"`
	RunID       string     `json:"run_id"`
	FunctionID  int64      `json:"function_id"`
	Status      string     `json:"status"`
	TriggeredAt time.Time  `json:"triggered_at"`
	StartedAt   *time.Time `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	DurationMs  *int       `json:"duration_ms"`
	Version     int        `json:"version"`
	CreatedAt   time.Time  `json:"created_at"`
}

func newFunctionRunItem(row domainfunction.FunctionRun) FunctionRunItem {
	return FunctionRunItem{
		ID:          row.ID,
		RunID:       row.RunID,
		FunctionID:  row.FunctionID,
		Status:      row.Status,
		TriggeredAt: row.TriggeredAt,
		StartedAt:   row.StartedAt,
		FinishedAt:  row.FinishedAt,
		DurationMs:  row.DurationMs,
		Version:     row.Version,
		CreatedAt:   row.CreatedAt,
	}
}

// List verifies the function exists and is visible, then answers with its
// invocation history, newest first.
func (uc *ListFunctionRunsUseCase) List(ctx context.Context, in FunctionRunListInput) ([]FunctionRunItem, error) {
	if _, err := (&GetFunctionUseCase{reader: uc.reader}).get(ctx, FunctionGetInput{
		ID:              in.FunctionID,
		TenantID:        in.TenantID,
		ResourceGroupID: in.ResourceGroupID,
	}); err != nil {
		return nil, err
	}
	if uc.runs == nil {
		return nil, fmt.Errorf("function run lister is required")
	}

	tenantID, err := normalizeTenantID(in.TenantID)
	if err != nil {
		return nil, err
	}
	rows, err := uc.runs.RunsByFunction(ctx, tenantID, in.FunctionID, normalizeListLimit(in.Limit))
	if err != nil {
		return nil, err
	}
	out := make([]FunctionRunItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, newFunctionRunItem(row))
	}
	return out, nil
}

// GetFunctionRunUseCase reads one terminal invocation by its deterministic run
// id.
type GetFunctionRunUseCase struct {
	runs   functionRunGetter
	reader functionReader
}

func NewGetFunctionRunUseCase(runs functionRunGetter, reader functionReader) *GetFunctionRunUseCase {
	return &GetFunctionRunUseCase{runs: runs, reader: reader}
}

type FunctionRunGetInput struct {
	FunctionID      int64
	RunID           string
	TenantID        string
	ResourceGroupID string
}

func (uc *GetFunctionRunUseCase) Get(ctx context.Context, in FunctionRunGetInput) (FunctionRunItem, error) {
	if _, err := (&GetFunctionUseCase{reader: uc.reader}).get(ctx, FunctionGetInput{
		ID:              in.FunctionID,
		TenantID:        in.TenantID,
		ResourceGroupID: in.ResourceGroupID,
	}); err != nil {
		return FunctionRunItem{}, err
	}
	if uc.runs == nil {
		return FunctionRunItem{}, fmt.Errorf("function run getter is required")
	}
	if strings.TrimSpace(in.RunID) == "" {
		return FunctionRunItem{}, validation.New("run_id", "is required")
	}

	tenantID, err := normalizeTenantID(in.TenantID)
	if err != nil {
		return FunctionRunItem{}, err
	}
	row, found, err := uc.runs.RunByRunID(ctx, tenantID, in.FunctionID, in.RunID)
	if err != nil {
		return FunctionRunItem{}, err
	}
	if !found {
		return FunctionRunItem{}, &resource.NotFoundError{Resource: "function run", ID: in.RunID}
	}
	return newFunctionRunItem(row), nil
}
