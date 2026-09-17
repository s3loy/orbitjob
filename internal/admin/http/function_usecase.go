package http

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	domainfunction "orbitjob/internal/core/domain/function"
	"orbitjob/internal/core/domain/jobrun"
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

// functionRevisionReader resolves the active revision a function invocation
// pins. Invocations are CR-first exactly like a manual trigger: the CRD
// requires definitionRevision before publish, so the caller must know the
// revision id, and only a pre-materialized revision provides one. The row is
// job_definition_revisions at (source_mode='function', source_uid, is_active).
// The admin role holds SELECT on that table, so this is a read the API may
// already make; the method itself lands with the storage workstream.
type functionRevisionReader interface {
	ActiveRevision(ctx context.Context, tenantID, sourceUID string) (functionRevision, error)
}

// functionRevision is the slice of a pinned revision an invocation needs: the
// revision id the CR requires and the scheduling namespace the object is
// published into. A function definition row carries no namespace of its own;
// the namespace is the revision's source_namespace, the same value every
// projection of the tenant writes.
type functionRevision struct {
	ID        int64
	Namespace string
}

// jobRunPublisher is the slice of kube.JobRunPublisher the invoke path uses:
// it publishes the JobRun custom resource and reads it back while waiting.
type jobRunPublisher interface {
	Publish(ctx context.Context, run v1alpha1.JobRun) (v1alpha1.JobRun, bool, error)
	Get(ctx context.Context, namespace, name string) (v1alpha1.JobRun, error)
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
	for _, def := range defs {
		if !visibleToGroup(def.ResourceGroupID, in.ResourceGroupID) {
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

// ==================== Invoke ====================

// maxInvokeWaitSeconds caps the synchronous variant of an invocation. The
// platform's honest latency promise for a function is seconds -- every
// invocation is a cold pod and image pull dominates -- so a longer wait would
// hold an HTTP request open against work the caller can already poll.
const maxInvokeWaitSeconds = 60

// invokePollInterval is how often the synchronous variant re-reads the JobRun
// custom resource while waiting for a terminal phase.
const invokePollInterval = time.Second

// InvokeFunctionUseCase turns an HTTP invocation into a JobRun custom
// resource, exactly as a manual job trigger does. The operator materializes
// the ledger row from the pinned revision, so the use case never writes the
// ledger and cannot return a run row id -- it returns a reference to the
// custom resource.
type InvokeFunctionUseCase struct {
	reader       functionReader
	revisions    functionRevisionReader
	publisher    jobRunPublisher
	pollInterval time.Duration
}

func NewInvokeFunctionUseCase(reader functionReader, revisions functionRevisionReader, publisher jobRunPublisher) *InvokeFunctionUseCase {
	return &InvokeFunctionUseCase{
		reader:       reader,
		revisions:    revisions,
		publisher:    publisher,
		pollInterval: invokePollInterval,
	}
}

type FunctionInvokeInput struct {
	FunctionID int64
	TenantID   string
	// ActorID is the authenticated key, recorded as the run's actor under the
	// manual trigger's discipline: the CRD makes the field required, and the
	// operator copies it into the ledger's actor column.
	ActorID string
	// IdempotencyKey, when set, makes a repeated invocation resolve to the
	// same run instead of creating another.
	IdempotencyKey  string
	ResourceGroupID string
	// WaitSeconds is the synchronous variant's budget: when positive, the
	// call polls the JobRun custom resource until a terminal phase or this
	// many seconds elapse, then answers with the phase observed. Zero means
	// async-with-reference.
	WaitSeconds int
}

// FunctionInvokeResult mirrors the manual trigger's contract: a reference to
// the custom resource, not a ledger row. Phase is whatever the resource
// currently reports, empty until the operator has observed it once -- and an
// expired wait reports the last observed phase rather than pretending the run
// finished.
type FunctionInvokeResult struct {
	Namespace     string `json:"namespace"`
	Name          string `json:"name"`
	OccurrenceKey string `json:"occurrence_key"`
	Trigger       string `json:"trigger"`
	Phase         string `json:"phase"`
	Created       bool   `json:"created"`
}

// Invoke validates the request, pins the function's active revision, and
// publishes the JobRun. It never writes the ledger.
func (uc *InvokeFunctionUseCase) Invoke(ctx context.Context, in FunctionInvokeInput) (FunctionInvokeResult, error) {
	normalized, err := normalizeInvokeInput(in)
	if err != nil {
		return FunctionInvokeResult{}, err
	}
	if uc.reader == nil || uc.revisions == nil {
		return FunctionInvokeResult{}, fmt.Errorf("function reader and revision reader are required")
	}
	if uc.publisher == nil {
		return FunctionInvokeResult{}, fmt.Errorf("job run publisher is required")
	}

	def, found, err := uc.reader.GetForTenant(ctx, normalized.TenantID, normalized.FunctionID)
	if err != nil {
		return FunctionInvokeResult{}, fmt.Errorf("read function for invoke: %w", err)
	}
	if !found || !visibleToGroup(def.ResourceGroupID, normalized.ResourceGroupID) {
		return FunctionInvokeResult{}, &resource.NotFoundError{Resource: "function", ID: normalized.FunctionID}
	}
	// Invoking a paused function is a conflict, not an error: the definition
	// exists and the caller reached it, but the platform has been told not to
	// run it.
	if def.Status == domainfunction.StatusPaused {
		return FunctionInvokeResult{}, &resource.ConflictError{
			Resource: "function",
			ID:       def.ID,
			Field:    "status",
			Message:  "cannot invoke a paused function",
		}
	}

	sourceUID := domainfunction.SourceUID(def.ID)
	revision, err := uc.revisions.ActiveRevision(ctx, normalized.TenantID, sourceUID)
	if err != nil {
		return FunctionInvokeResult{}, fmt.Errorf("resolve active revision: %w", err)
	}
	if revision.ID < 1 {
		return FunctionInvokeResult{}, &resource.ConflictError{
			Resource: "function",
			ID:       def.ID,
			Field:    "revision",
			Message:  "the function has no active revision yet; the operator's revision sync has not caught up",
		}
	}

	occurrenceKey, err := invokeOccurrenceKey(normalized, sourceUID)
	if err != nil {
		return FunctionInvokeResult{}, err
	}

	start := time.Now()
	stored, created, err := uc.publisher.Publish(ctx, invokeJobRun(def, revision, occurrenceKey, normalized.ActorID))
	if err != nil {
		return FunctionInvokeResult{}, fmt.Errorf("publish function invocation: %w", err)
	}

	result := FunctionInvokeResult{
		Namespace:     stored.Namespace,
		Name:          stored.Name,
		OccurrenceKey: occurrenceKey,
		Trigger:       string(v1alpha1.Function),
		Phase:         stored.Status.Phase,
		Created:       created,
	}

	if normalized.WaitSeconds > 0 {
		result.Phase = uc.awaitTerminalPhase(ctx, stored.Namespace, stored.Name,
			start.Add(time.Duration(normalized.WaitSeconds)*time.Second))
	}
	return result, nil
}

// awaitTerminalPhase polls the custom resource until its phase is terminal or
// the deadline passes, whichever comes first. The resource is the read model:
// the operator patches status from stored state, so no new read surface is
// needed. An expired wait is not an error -- the caller always gets the
// reference, and the phase observed last.
func (uc *InvokeFunctionUseCase) awaitTerminalPhase(ctx context.Context, namespace, name string, deadline time.Time) string {
	interval := uc.pollInterval
	if interval <= 0 {
		interval = invokePollInterval
	}
	phase := ""
	for {
		if err := ctx.Err(); err != nil {
			return phase
		}
		run, err := uc.publisher.Get(ctx, namespace, name)
		if err == nil {
			phase = run.Status.Phase
			if jobrun.Terminal(jobrun.Phase(phase)) {
				return phase
			}
		}
		if !time.Now().Add(interval).Before(deadline) {
			return phase
		}
		select {
		case <-ctx.Done():
			return phase
		case <-time.After(interval):
		}
	}
}

func normalizeInvokeInput(in FunctionInvokeInput) (FunctionInvokeInput, error) {
	if in.FunctionID < 1 {
		return FunctionInvokeInput{}, validation.New("id", "must be >= 1")
	}
	tenantID, err := normalizeTenantID(in.TenantID)
	if err != nil {
		return FunctionInvokeInput{}, err
	}
	actorID := strings.TrimSpace(in.ActorID)
	if actorID == "" {
		return FunctionInvokeInput{}, validation.New("actor_id", "is required")
	}
	if len(actorID) > 255 {
		return FunctionInvokeInput{}, validation.New("actor_id", "must be <= 255 characters")
	}
	if in.WaitSeconds < 0 || in.WaitSeconds > maxInvokeWaitSeconds {
		return FunctionInvokeInput{}, validation.New("wait_seconds",
			fmt.Sprintf("must be between 0 and %d", maxInvokeWaitSeconds))
	}
	return FunctionInvokeInput{
		FunctionID:      in.FunctionID,
		TenantID:        tenantID,
		ActorID:         actorID,
		IdempotencyKey:  strings.TrimSpace(in.IdempotencyKey),
		ResourceGroupID: in.ResourceGroupID,
		WaitSeconds:     in.WaitSeconds,
	}, nil
}

// invokeJobRun renders the JobRun custom resource for one invocation. The
// scheduledJobRef names the function's stable revision identity -- the same
// string the ledger stores as source_uid -- and the object name derives from
// the occurrence key, so a replayed invocation addresses one object. There are
// deliberately no owner references: the ref does not point at a ScheduledJob
// custom resource, and an owner pointer to an object that does not exist would
// have Kubernetes garbage-collect the run immediately.
func invokeJobRun(def domainfunction.Definition, revision functionRevision, occurrenceKey, actorID string) v1alpha1.JobRun {
	sourceUID := domainfunction.SourceUID(def.ID)
	return v1alpha1.JobRun{
		TypeMeta: metav1.TypeMeta{APIVersion: v1alpha1.GroupVersion.String(), Kind: "JobRun"},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: revision.Namespace,
			Name:      v1alpha1.RunObjectName(sourceUID, occurrenceKey),
		},
		Spec: v1alpha1.JobRunSpec{
			ScheduledJobRef:    v1alpha1.ObjectReference{Name: sourceUID, UID: sourceUID},
			DefinitionRevision: revision.ID,
			Trigger:            v1alpha1.Function,
			Actor:              actorID,
			OccurrenceKey:      occurrenceKey,
			TimeoutSeconds:     int32(def.TimeoutSeconds),
		},
	}
}

// invokeOccurrenceKey derives the 64-hex occurrence key the ledger deduplicates
// on, the manual trigger's derivation with a function-shaped base. With an
// idempotency key the value is stable, so a retried invocation lands on the
// same run; without one, fresh entropy makes each call its own run, since two
// invocations are two runs unless the caller says otherwise.
func invokeOccurrenceKey(in FunctionInvokeInput, sourceUID string) (string, error) {
	base := fmt.Sprintf("function|%s|%s|%d", in.TenantID, sourceUID, in.FunctionID)
	if in.IdempotencyKey != "" {
		return sha256Hex(base + "|idem|" + in.IdempotencyKey), nil
	}

	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", fmt.Errorf("generate invocation nonce: %w", err)
	}
	return sha256Hex(base + "|nonce|" + hex.EncodeToString(nonce[:])), nil
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
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
