package http

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"orbitjob/internal/core/app/functioninvoke"
	domainfunction "orbitjob/internal/core/domain/function"
	"orbitjob/internal/domain/executionpolicy"
	"orbitjob/internal/domain/queryfilter"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
)

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
	lister    functionLister
	revisions functioninvoke.RevisionSource
}

func NewListFunctionsUseCase(lister functionLister) *ListFunctionsUseCase {
	return &ListFunctionsUseCase{lister: lister}
}

func (uc *ListFunctionsUseCase) WithReadiness(revisions functioninvoke.RevisionSource) *ListFunctionsUseCase {
	uc.revisions = revisions
	return uc
}

// FunctionListInput is the normalized list input. ResourceGroupID is the
// caller's key scope; a function row carries an optional group, so a scoped
// caller sees only its group's functions instead of being refused outright --
// the checks read model's exact semantics.
type FunctionListInput struct {
	Search          string
	TenantID        string
	ResourceGroupID string
	Limit           int
	Offset          int
}

// FunctionItem is the API's function definition shape.
type FunctionItem struct {
	ExecutionPolicyVersion int32                      `json:"execution_policy_version,omitempty"`
	RetrySafe              bool                       `json:"retry_safe,omitempty"`
	Resources              *executionpolicy.Resources `json:"resources,omitempty"`
	ID                     int64                      `json:"id"`
	TenantID               string                     `json:"tenant_id"`
	ResourceGroupID        string                     `json:"resource_group_id"`
	Name                   string                     `json:"name"`
	Description            string                     `json:"description"`
	Status                 string                     `json:"status"`
	Image                  string                     `json:"image"`
	Command                []string                   `json:"command"`
	Args                   []string                   `json:"args"`
	TimeoutSeconds         int                        `json:"timeout_seconds"`
	RetryLimit             int                        `json:"retry_limit"`
	HistorySuccess         int                        `json:"history_success"`
	HistoryFailed          int                        `json:"history_failed"`
	Labels                 map[string]any             `json:"labels"`
	Version                int                        `json:"version"`
	ActiveRevisionID       *int64                     `json:"active_revision_id"`
	ActiveRevisionVersion  *int                       `json:"active_revision_version"`
	Ready                  bool                       `json:"ready"`
	CreatedAt              time.Time                  `json:"created_at"`
	UpdatedAt              time.Time                  `json:"updated_at"`
}

func newFunctionItem(def domainfunction.Definition) FunctionItem {
	return FunctionItem{
		ID:                     def.ID,
		ExecutionPolicyVersion: def.ExecutionPolicyVersion, RetrySafe: def.RetrySafe, Resources: def.Resources,
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
	search, err := queryfilter.Search(in.Search)
	if err != nil {
		return nil, err
	}
	if in.Offset < 0 {
		return nil, validation.New("offset", "must be >= 0")
	}
	if uc.lister == nil {
		return nil, fmt.Errorf("function lister is required")
	}

	defs, err := uc.lister.ListForTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	search = strings.ToLower(search)
	sort.SliceStable(defs, func(i, j int) bool {
		if defs[i].CreatedAt.Equal(defs[j].CreatedAt) {
			return defs[i].ID > defs[j].ID
		}
		return defs[i].CreatedAt.After(defs[j].CreatedAt)
	})
	limit := normalizeListLimit(in.Limit)
	out := make([]FunctionItem, 0, len(defs))
	skipped := 0
	for _, def := range defs {
		if !visibleToGroup(def.ResourceGroupID, in.ResourceGroupID) || (search != "" && !strings.Contains(strings.ToLower(def.Name), search) && !strings.Contains(strings.ToLower(def.Description), search)) {
			continue
		}
		if skipped < in.Offset {
			skipped++
			continue
		}
		if len(out) == limit {
			break
		}
		item, err := functionItemWithReadiness(ctx, def, uc.revisions)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

// ==================== Get ====================

// GetFunctionUseCase reads one function definition.
type GetFunctionUseCase struct {
	reader    functionReader
	revisions functioninvoke.RevisionSource
}

func NewGetFunctionUseCase(reader functionReader) *GetFunctionUseCase {
	return &GetFunctionUseCase{reader: reader}
}

func (uc *GetFunctionUseCase) WithReadiness(revisions functioninvoke.RevisionSource) *GetFunctionUseCase {
	uc.revisions = revisions
	return uc
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
	return functionItemWithReadiness(ctx, def, uc.revisions)
}

func functionItemWithReadiness(ctx context.Context, def domainfunction.Definition, revisions functioninvoke.RevisionSource) (FunctionItem, error) {
	item := newFunctionItem(def)
	if revisions == nil {
		return item, nil
	}
	rev, found, err := revisions.ActiveRevision(ctx, def.TenantID, domainfunction.SourceUID(def.ID))
	if err != nil {
		return FunctionItem{}, fmt.Errorf("read function readiness: %w", err)
	}
	if found {
		item.ActiveRevisionID, item.ActiveRevisionVersion = &rev.ID, &rev.DefinitionVersion
		item.Ready = rev.DefinitionVersion == def.Version && def.Status == domainfunction.StatusActive
	}
	return item, nil
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
	Offset          int
	CreatedFrom     string
	CreatedBefore   string
	FunctionID      int64
	TenantID        string
	ResourceGroupID string
	Limit           int
}

// FunctionRunItem is one read-model row. StartedAt, FinishedAt and DurationMs
// are nullable at the boundary: a canceled invocation may have been stopped
// before its first attempt began.
type FunctionRunItem struct {
	ID            int64                    `json:"id"`
	RunID         string                   `json:"run_id"`
	OccurrenceKey *string                  `json:"occurrence_key"`
	FunctionID    int64                    `json:"function_id"`
	Status        string                   `json:"status"`
	TriggeredAt   time.Time                `json:"triggered_at"`
	StartedAt     *time.Time               `json:"started_at"`
	FinishedAt    *time.Time               `json:"finished_at"`
	DurationMs    *int                     `json:"duration_ms"`
	Version       int                      `json:"version"`
	CreatedAt     time.Time                `json:"created_at"`
	Attempts      []domainfunction.Attempt `json:"attempts" nullable:"true"`
	LastAttempt   *int                     `json:"last_attempt"`
}

func newFunctionRunItem(row domainfunction.FunctionRun) FunctionRunItem {
	return FunctionRunItem{
		ID:            row.ID,
		RunID:         row.RunID,
		OccurrenceKey: row.OccurrenceKey,
		FunctionID:    row.FunctionID,
		Status:        row.Status,
		TriggeredAt:   row.TriggeredAt,
		StartedAt:     row.StartedAt,
		FinishedAt:    row.FinishedAt,
		DurationMs:    row.DurationMs,
		Version:       row.Version,
		CreatedAt:     row.CreatedAt,
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
	from, before, err := queryfilter.Range(in.CreatedFrom, in.CreatedBefore)
	if err != nil {
		return nil, err
	}
	if in.Offset < 0 {
		return nil, validation.New("offset", "must be >= 0")
	}
	var rows []domainfunction.FunctionRun
	if reader, ok := uc.runs.(interface {
		RunsByFunctionPage(context.Context, string, int64, int, int, string, string) ([]domainfunction.FunctionRun, error)
	}); ok {
		rows, err = reader.RunsByFunctionPage(ctx, tenantID, in.FunctionID, normalizeListLimit(in.Limit), in.Offset, from, before)
	} else if in.Offset != 0 || from != "" || before != "" {
		return nil, fmt.Errorf("paged function history reader is required")
	} else {
		rows, err = uc.runs.RunsByFunction(ctx, tenantID, in.FunctionID, normalizeListLimit(in.Limit))
	}
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
	runID, err := uuid.Parse(in.RunID)
	if err != nil || len(in.RunID) != 36 {
		return FunctionRunItem{}, validation.New("run_id", "must be a UUID")
	}

	tenantID, err := normalizeTenantID(in.TenantID)
	if err != nil {
		return FunctionRunItem{}, err
	}
	row, found, err := uc.runs.RunByRunID(ctx, tenantID, in.FunctionID, runID.String())
	if err != nil {
		return FunctionRunItem{}, err
	}
	if !found {
		return FunctionRunItem{}, &resource.NotFoundError{Resource: "function run", ID: in.RunID}
	}
	out := newFunctionRunItem(row)
	if reader, ok := uc.runs.(interface {
		AttemptsByRunID(context.Context, string, int64, string) ([]domainfunction.Attempt, bool, error)
	}); ok {
		attempts, known, readErr := reader.AttemptsByRunID(ctx, tenantID, in.FunctionID, runID.String())
		if readErr != nil {
			return FunctionRunItem{}, readErr
		}
		if known {
			out.Attempts = attempts
			if out.Attempts == nil {
				out.Attempts = []domainfunction.Attempt{}
			}
			for _, attempt := range attempts {
				if out.LastAttempt == nil || attempt.AttemptNumber > *out.LastAttempt {
					number := attempt.AttemptNumber
					out.LastAttempt = &number
				}
			}
		}
	}
	return out, nil
}
