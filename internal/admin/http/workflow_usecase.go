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
	domainworkflow "orbitjob/internal/core/domain/workflow"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
)

// The workflow use cases consume the storage through the narrow interfaces
// below. The WorkflowRunStore contract is landed; the definition read paths go
// over the job_definition_revisions projection (source_mode='workflow'), which
// the operator's WorkflowJob reconcile writes, and the per-definition run list
// has no contract yet -- both are marked where they appear, so the storage
// workstream can see exactly what it owes.

// WorkflowDefinition is one active workflow revision as the projection stores
// it: the revision's identity fields plus the WorkflowJob spec it normalized.
type WorkflowDefinition struct {
	// ID is the revision id, the identity the read model exposes -- the same
	// convention jobs use for their :id path parameters.
	ID         int64
	Name       string
	Namespace  string
	SourceUID  string
	Generation int64
	Spec       v1alpha1.WorkflowJobSpec
	CreatedAt  time.Time
}

// workflowDefinitionReader reads active workflow revisions from the
// job_definition_revisions projection. Implementations must filter to
// source_mode='workflow' and the active revision only: an inactive revision is
// history, not the workflow. A definition that does not exist, is not a
// workflow revision, or belongs to another tenant is NotFoundError.
type workflowDefinitionReader interface {
	ListActive(ctx context.Context, in WorkflowDefinitionListInput) ([]WorkflowDefinition, error)
	GetActive(ctx context.Context, tenantID string, id int64) (WorkflowDefinition, error)
}

type WorkflowDefinitionListInput struct {
	TenantID string
	Limit    int
	Offset   int
}

// workflowRunReader is the slice of the WorkflowRunStore contract the admin
// read and cancel paths need. RunsForDefinition is the one method the contract
// does not declare: the store carries the walker's OpenRuns, which answers a
// different question (non-terminal runs of the whole tenant), so listing one
// workflow's history owes the storage workstream a query beside it.
type workflowRunReader interface {
	RunByID(ctx context.Context, tenantID string, id int64) (run domainworkflow.Run, found bool, err error)
	Steps(ctx context.Context, tenantID string, workflowRunID int64) ([]domainworkflow.StepRun, error)
	RunsForDefinition(ctx context.Context, tenantID, sourceUID string, limit, offset int) ([]domainworkflow.Run, error)
}

// workflowRunPublisher is the slice of kube.WorkflowRunPublisher the workflow
// routes use: a manual trigger creates the custom resource, a cancel patches
// its cancelRequested, and the operator owns every effect of both.
type workflowRunPublisher interface {
	Create(ctx context.Context, run v1alpha1.WorkflowRun) (v1alpha1.WorkflowRun, bool, error)
	RequestCancel(ctx context.Context, namespace, name string) error
}

// ==================== Definition reads ====================

// ListWorkflowsUseCase lists the active revision of every workflow definition
// in the caller's tenant.
type ListWorkflowsUseCase struct {
	definitions workflowDefinitionReader
}

func NewListWorkflowsUseCase(definitions workflowDefinitionReader) *ListWorkflowsUseCase {
	return &ListWorkflowsUseCase{definitions: definitions}
}

type WorkflowListInput struct {
	TenantID        string
	ResourceGroupID string
	Limit           int
	Offset          int
}

type WorkflowListItem struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	Namespace      string    `json:"namespace"`
	SourceUID      string    `json:"source_uid"`
	Generation     int64     `json:"generation"`
	Schedule       string    `json:"schedule"`
	Suspend        bool      `json:"suspend"`
	FailPolicy     string    `json:"fail_policy"`
	TimeoutSeconds int32     `json:"timeout_seconds"`
	TaskCount      int       `json:"task_count"`
	CreatedAt      time.Time `json:"created_at"`
}

// List resolves the caller's scope first: a workflow revision has no resource
// group to be limited by, so a scoped caller is refused rather than served a
// wider view -- the job definition reads' exact contract.
func (uc *ListWorkflowsUseCase) List(ctx context.Context, in WorkflowListInput) ([]WorkflowListItem, error) {
	tenantID, err := normalizeTenantID(in.TenantID)
	if err != nil {
		return nil, err
	}
	if err := resource.RequireUnscoped(in.ResourceGroupID, "workflow definition"); err != nil {
		return nil, err
	}
	if uc.definitions == nil {
		return nil, fmt.Errorf("workflow definition reader is required")
	}

	defs, err := uc.definitions.ListActive(ctx, WorkflowDefinitionListInput{
		TenantID: tenantID,
		Limit:    normalizeListLimit(in.Limit),
		Offset:   in.Offset,
	})
	if err != nil {
		return nil, err
	}

	out := make([]WorkflowListItem, 0, len(defs))
	for _, def := range defs {
		out = append(out, newWorkflowListItem(def))
	}
	return out, nil
}

// GetWorkflowUseCase reads one workflow definition's active revision.
type GetWorkflowUseCase struct {
	definitions workflowDefinitionReader
}

func NewGetWorkflowUseCase(definitions workflowDefinitionReader) *GetWorkflowUseCase {
	return &GetWorkflowUseCase{definitions: definitions}
}

type WorkflowGetInput struct {
	ID              int64
	TenantID        string
	ResourceGroupID string
}

type WorkflowGetItem struct {
	WorkflowListItem
	Tasks []WorkflowTaskItem `json:"tasks"`
}

func (uc *GetWorkflowUseCase) Get(ctx context.Context, in WorkflowGetInput) (WorkflowGetItem, error) {
	def, err := uc.get(ctx, in)
	if err != nil {
		return WorkflowGetItem{}, err
	}
	item := WorkflowGetItem{WorkflowListItem: newWorkflowListItem(def)}
	item.Tasks = newWorkflowTaskItems(def.Spec)
	return item, nil
}

func (uc *GetWorkflowUseCase) get(ctx context.Context, in WorkflowGetInput) (WorkflowDefinition, error) {
	tenantID, err := normalizeTenantID(in.TenantID)
	if err != nil {
		return WorkflowDefinition{}, err
	}
	if err := resource.RequireUnscoped(in.ResourceGroupID, "workflow definition"); err != nil {
		return WorkflowDefinition{}, err
	}
	if in.ID < 1 {
		return WorkflowDefinition{}, validation.New("id", "must be >= 1")
	}
	if uc.definitions == nil {
		return WorkflowDefinition{}, fmt.Errorf("workflow definition reader is required")
	}
	return uc.definitions.GetActive(ctx, tenantID, in.ID)
}

func newWorkflowListItem(def WorkflowDefinition) WorkflowListItem {
	failPolicy := string(domainworkflow.FailPolicyFailFast)
	if def.Spec.FailPolicy == v1alpha1.Continue {
		failPolicy = string(domainworkflow.FailPolicyContinue)
	}
	return WorkflowListItem{
		ID:             def.ID,
		Name:           def.Name,
		Namespace:      def.Namespace,
		SourceUID:      def.SourceUID,
		Generation:     def.Generation,
		Schedule:       def.Spec.Schedule,
		Suspend:        def.Spec.Suspend,
		FailPolicy:     failPolicy,
		TimeoutSeconds: def.Spec.TimeoutSeconds,
		TaskCount:      len(def.Spec.Tasks),
		CreatedAt:      def.CreatedAt,
	}
}

func newWorkflowTaskItems(spec v1alpha1.WorkflowJobSpec) []WorkflowTaskItem {
	out := make([]WorkflowTaskItem, 0, len(spec.Tasks))
	for _, task := range spec.Tasks {
		item := WorkflowTaskItem{
			Name:      task.Name,
			JobRef:    task.JobRef.Name,
			DependsOn: append([]string(nil), task.DependsOn...),
		}
		if task.Condition != nil {
			cond := &WorkflowConditionItem{Type: string(task.Condition.Type)}
			for _, rule := range task.Condition.Rules {
				cond.Rules = append(cond.Rules, WorkflowRuleItem{
					Task:     rule.Task,
					Field:    string(rule.Field),
					Operator: string(rule.Operator),
					Values:   append([]string(nil), rule.Values...),
					Number:   rule.Number,
				})
			}
			item.Condition = cond
		}
		out = append(out, item)
	}
	return out
}

type WorkflowTaskItem struct {
	Name      string                 `json:"name"`
	JobRef    string                 `json:"job_ref"`
	DependsOn []string               `json:"depends_on"`
	Condition *WorkflowConditionItem `json:"condition,omitempty"`
}

type WorkflowConditionItem struct {
	Type  string             `json:"type"`
	Rules []WorkflowRuleItem `json:"rules"`
}

type WorkflowRuleItem struct {
	Task     string   `json:"task"`
	Field    string   `json:"field"`
	Operator string   `json:"operator"`
	Values   []string `json:"values,omitempty"`
	Number   *int64   `json:"number,omitempty"`
}

// ==================== Manual trigger ====================

// TriggerWorkflowUseCase turns a manual workflow trigger into a WorkflowRun
// custom resource. The operator materializes the workflow_run_control_plane
// row from it -- row-first, one resource kind up from the jobrun path -- so
// the use case never writes the ledger and cannot return a row id.
type TriggerWorkflowUseCase struct {
	definitions workflowDefinitionReader
	publisher   workflowRunPublisher
}

func NewTriggerWorkflowUseCase(definitions workflowDefinitionReader, publisher workflowRunPublisher) *TriggerWorkflowUseCase {
	return &TriggerWorkflowUseCase{definitions: definitions, publisher: publisher}
}

type WorkflowTriggerInput struct {
	// WorkflowID is the workflow's active revision id.
	WorkflowID int64
	TenantID   string
	// ActorID is the authenticated principal that asked for the run. The CRD
	// makes the field required, and the operator copies it into the workflow
	// run row's actor column, which the schema keeps NOT NULL and non-empty.
	ActorID string
	// IdempotencyKey, when set, makes a repeated trigger resolve to the same
	// run instead of creating another.
	IdempotencyKey  string
	ResourceGroupID string
}

// WorkflowTriggerResult mirrors the manual job trigger's contract: a reference
// to the custom resource, not a ledger row. Phase is whatever the resource
// currently reports, empty until the operator has observed it once.
type WorkflowTriggerResult struct {
	Namespace     string `json:"namespace"`
	Name          string `json:"name"`
	OccurrenceKey string `json:"occurrence_key"`
	Trigger       string `json:"trigger"`
	Phase         string `json:"phase"`
	Created       bool   `json:"created"`
}

func (uc *TriggerWorkflowUseCase) Trigger(ctx context.Context, in WorkflowTriggerInput) (WorkflowTriggerResult, error) {
	normalized, err := normalizeWorkflowTriggerInput(in)
	if err != nil {
		return WorkflowTriggerResult{}, err
	}
	if uc.publisher == nil {
		return WorkflowTriggerResult{}, fmt.Errorf("workflow run publisher is required")
	}

	def, err := (&GetWorkflowUseCase{definitions: uc.definitions}).get(ctx, WorkflowGetInput{
		ID:              normalized.WorkflowID,
		TenantID:        normalized.TenantID,
		ResourceGroupID: normalized.ResourceGroupID,
	})
	if err != nil {
		return WorkflowTriggerResult{}, err
	}
	if def.Spec.Suspend {
		return WorkflowTriggerResult{}, &resource.ConflictError{
			Resource: "workflow",
			ID:       def.ID,
			Field:    "suspend",
			Message:  "cannot trigger a suspended workflow",
		}
	}

	occurrenceKey, err := workflowOccurrenceKey(normalized, def.SourceUID)
	if err != nil {
		return WorkflowTriggerResult{}, err
	}

	stored, created, err := uc.publisher.Create(ctx, manualWorkflowRun(def, occurrenceKey, normalized.ActorID))
	if err != nil {
		return WorkflowTriggerResult{}, fmt.Errorf("publish workflow run: %w", err)
	}

	return WorkflowTriggerResult{
		Namespace:     stored.Namespace,
		Name:          stored.Name,
		OccurrenceKey: occurrenceKey,
		Trigger:       string(v1alpha1.Manual),
		Phase:         stored.Status.Phase,
		Created:       created,
	}, nil
}

func normalizeWorkflowTriggerInput(in WorkflowTriggerInput) (WorkflowTriggerInput, error) {
	if in.WorkflowID < 1 {
		return WorkflowTriggerInput{}, validation.New("id", "must be >= 1")
	}
	// Scope is decided first, in the use case that owns the definition read,
	// so an authorization denial never depends on the rest of the input being
	// well-formed.
	tenantID, err := normalizeTenantID(in.TenantID)
	if err != nil {
		return WorkflowTriggerInput{}, err
	}
	actorID := strings.TrimSpace(in.ActorID)
	if actorID == "" {
		return WorkflowTriggerInput{}, validation.New("actor_id", "is required")
	}
	if len(actorID) > 255 {
		return WorkflowTriggerInput{}, validation.New("actor_id", "must be <= 255 characters")
	}
	return WorkflowTriggerInput{
		WorkflowID:      in.WorkflowID,
		TenantID:        tenantID,
		ActorID:         actorID,
		IdempotencyKey:  strings.TrimSpace(in.IdempotencyKey),
		ResourceGroupID: in.ResourceGroupID,
	}, nil
}

// manualWorkflowRun renders the WorkflowRun custom resource for a manual
// trigger. The object name derives from the occurrence key through the shared
// naming rule, so a replayed trigger addresses one object rather than minting
// a second run. The actor travels in spec.actor, the field the CRD makes
// required and validates, so the value the operator copies into the ledger's
// actor column cannot be a header the caller invented.
func manualWorkflowRun(def WorkflowDefinition, occurrenceKey, actorID string) v1alpha1.WorkflowRun {
	return v1alpha1.WorkflowRun{
		TypeMeta: metav1.TypeMeta{APIVersion: v1alpha1.GroupVersion.String(), Kind: "WorkflowRun"},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: def.Namespace,
			Name:      v1alpha1.WorkflowRunObjectName(def.Name, occurrenceKey),
		},
		Spec: v1alpha1.WorkflowRunSpec{
			WorkflowRef:        v1alpha1.ObjectReference{Name: def.Name, UID: def.SourceUID},
			DefinitionRevision: def.ID,
			Trigger:            v1alpha1.Manual,
			Actor:              actorID,
			OccurrenceKey:      occurrenceKey,
		},
	}
}

// workflowOccurrenceKey derives the 64-hex occurrence key the workflow ledger
// deduplicates on, the manual trigger's derivation with a workflow-shaped
// base. With an idempotency key the value is stable; without one, fresh
// entropy makes each trigger its own run.
func workflowOccurrenceKey(in WorkflowTriggerInput, sourceUID string) (string, error) {
	base := fmt.Sprintf("workflow|%s|%s|%d", in.TenantID, sourceUID, in.WorkflowID)
	if in.IdempotencyKey != "" {
		return workflowSHA256Hex(base + "|idem|" + in.IdempotencyKey), nil
	}

	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", fmt.Errorf("generate workflow trigger nonce: %w", err)
	}
	return workflowSHA256Hex(base + "|nonce|" + hex.EncodeToString(nonce[:])), nil
}

func workflowSHA256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// ==================== Workflow runs ====================

// ListWorkflowRunsUseCase lists one workflow's runs from the workflow ledger,
// newest first. The workflow is named by its active revision id, so the use
// case resolves the definition first and answers with its runs only.
type ListWorkflowRunsUseCase struct {
	definitions workflowDefinitionReader
	runs        workflowRunReader
}

func NewListWorkflowRunsUseCase(definitions workflowDefinitionReader, runs workflowRunReader) *ListWorkflowRunsUseCase {
	return &ListWorkflowRunsUseCase{definitions: definitions, runs: runs}
}

type WorkflowRunListInput struct {
	WorkflowID      int64
	TenantID        string
	ResourceGroupID string
	Limit           int
	Offset          int
}

type WorkflowRunItem struct {
	ID            int64     `json:"id"`
	SourceUID     string    `json:"workflow_source_uid"`
	RevisionID    int64     `json:"revision_id"`
	OccurrenceKey string    `json:"occurrence_key"`
	Trigger       string    `json:"trigger"`
	Actor         string    `json:"actor"`
	Phase         string    `json:"phase"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func newWorkflowRunItem(run domainworkflow.Run) WorkflowRunItem {
	return WorkflowRunItem{
		ID:            run.ID,
		SourceUID:     run.SourceUID,
		RevisionID:    run.RevisionID,
		OccurrenceKey: run.OccurrenceKey,
		Trigger:       string(run.Trigger),
		Actor:         run.Actor,
		Phase:         string(run.Phase),
		CreatedAt:     run.CreatedAt,
		UpdatedAt:     run.UpdatedAt,
	}
}

func (uc *ListWorkflowRunsUseCase) List(ctx context.Context, in WorkflowRunListInput) ([]WorkflowRunItem, error) {
	tenantID, err := normalizeTenantID(in.TenantID)
	if err != nil {
		return nil, err
	}
	if err := resource.RequireUnscoped(in.ResourceGroupID, "workflow run"); err != nil {
		return nil, err
	}
	if uc.runs == nil {
		return nil, fmt.Errorf("workflow run reader is required")
	}

	def, err := (&GetWorkflowUseCase{definitions: uc.definitions}).get(ctx, WorkflowGetInput{
		ID:              in.WorkflowID,
		TenantID:        tenantID,
		ResourceGroupID: in.ResourceGroupID,
	})
	if err != nil {
		return nil, err
	}

	rows, err := uc.runs.RunsForDefinition(ctx, tenantID, def.SourceUID, normalizeListLimit(in.Limit), in.Offset)
	if err != nil {
		return nil, err
	}
	out := make([]WorkflowRunItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, newWorkflowRunItem(row))
	}
	return out, nil
}

// GetWorkflowRunUseCase reads one workflow run together with its step runs.
// The steps are the ordinary job runs the walker grouped under the workflow
// run; the workflow ledger row is the state, and the column is the pointer.
type GetWorkflowRunUseCase struct {
	definitions workflowDefinitionReader
	runs        workflowRunReader
}

func NewGetWorkflowRunUseCase(definitions workflowDefinitionReader, runs workflowRunReader) *GetWorkflowRunUseCase {
	return &GetWorkflowRunUseCase{definitions: definitions, runs: runs}
}

type WorkflowRunGetInput struct {
	WorkflowID      int64
	RunID           int64
	TenantID        string
	ResourceGroupID string
}

type WorkflowStepItem struct {
	ID            int64     `json:"id"`
	SourceUID     string    `json:"source_uid"`
	OccurrenceKey string    `json:"occurrence_key"`
	Phase         string    `json:"phase"`
	Attempt       int       `json:"attempt"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type WorkflowRunGetItem struct {
	WorkflowRunItem
	Steps []WorkflowStepItem `json:"steps"`
}

func (uc *GetWorkflowRunUseCase) Get(ctx context.Context, in WorkflowRunGetInput) (WorkflowRunGetItem, error) {
	run, err := uc.resolveRun(ctx, in.WorkflowID, in.RunID, in.TenantID, in.ResourceGroupID)
	if err != nil {
		return WorkflowRunGetItem{}, err
	}

	tenantID, err := normalizeTenantID(in.TenantID)
	if err != nil {
		return WorkflowRunGetItem{}, err
	}
	steps, err := uc.runs.Steps(ctx, tenantID, run.ID)
	if err != nil {
		return WorkflowRunGetItem{}, err
	}

	out := WorkflowRunGetItem{WorkflowRunItem: newWorkflowRunItem(run)}
	out.Steps = make([]WorkflowStepItem, 0, len(steps))
	for _, step := range steps {
		out.Steps = append(out.Steps, WorkflowStepItem{
			ID:            step.ID,
			SourceUID:     step.SourceUID,
			OccurrenceKey: step.OccurrenceKey,
			Phase:         string(step.Phase),
			Attempt:       step.Attempt,
			CreatedAt:     step.CreatedAt,
			UpdatedAt:     step.UpdatedAt,
		})
	}
	return out, nil
}

// resolveRun loads one run and verifies it belongs to the workflow the route
// names. The id the route carries is the workflow's active revision id, and a
// run of a different definition must be indistinguishable from a run that does
// not exist -- saying anything else would leak other workflows' history
// through the route's own prefix.
func (uc *GetWorkflowRunUseCase) resolveRun(ctx context.Context, workflowID, runID int64, tenantID, scope string) (domainworkflow.Run, error) {
	if runID < 1 {
		return domainworkflow.Run{}, validation.New("run_id", "must be >= 1")
	}
	normalizedTenant, err := normalizeTenantID(tenantID)
	if err != nil {
		return domainworkflow.Run{}, err
	}
	if err := resource.RequireUnscoped(scope, "workflow run"); err != nil {
		return domainworkflow.Run{}, err
	}
	if uc.runs == nil {
		return domainworkflow.Run{}, fmt.Errorf("workflow run reader is required")
	}

	def, err := (&GetWorkflowUseCase{definitions: uc.definitions}).get(ctx, WorkflowGetInput{
		ID:              workflowID,
		TenantID:        normalizedTenant,
		ResourceGroupID: scope,
	})
	if err != nil {
		return domainworkflow.Run{}, err
	}

	run, found, err := uc.runs.RunByID(ctx, normalizedTenant, runID)
	if err != nil {
		return domainworkflow.Run{}, err
	}
	if !found || run.SourceUID != def.SourceUID {
		return domainworkflow.Run{}, &resource.NotFoundError{Resource: "workflow run", ID: runID}
	}
	return run, nil
}

// ==================== Cancel ====================

// CancelWorkflowRunUseCase requests a stop for one workflow run by patching
// spec.cancelRequested on its WorkflowRun custom resource. The operator fans
// the stop out over the workflow's non-terminal step runs and owns every
// ledger write, so the use case has no database writes at all.
type CancelWorkflowRunUseCase struct {
	definitions workflowDefinitionReader
	runs        workflowRunReader
	publisher   workflowRunPublisher
}

func NewCancelWorkflowRunUseCase(definitions workflowDefinitionReader, runs workflowRunReader, publisher workflowRunPublisher) *CancelWorkflowRunUseCase {
	return &CancelWorkflowRunUseCase{definitions: definitions, runs: runs, publisher: publisher}
}

type WorkflowCancelInput struct {
	WorkflowID      int64
	RunID           int64
	TenantID        string
	ResourceGroupID string
}

// WorkflowCancelResult mirrors the run cancel's contract: a reference to the
// custom resource the stop intent was patched onto, and the workflow phase
// observed at request time. The workflow reaches Canceled only once its steps
// are observed terminal, so a caller wanting the outcome reads the run again.
type WorkflowCancelResult struct {
	Namespace     string `json:"namespace"`
	Name          string `json:"name"`
	OccurrenceKey string `json:"occurrence_key"`
	Phase         string `json:"phase"`
}

func (uc *CancelWorkflowRunUseCase) Cancel(ctx context.Context, in WorkflowCancelInput) (WorkflowCancelResult, error) {
	run, err := (&GetWorkflowRunUseCase{definitions: uc.definitions, runs: uc.runs}).resolveRun(
		ctx, in.WorkflowID, in.RunID, in.TenantID, in.ResourceGroupID)
	if err != nil {
		return WorkflowCancelResult{}, err
	}
	if uc.publisher == nil {
		return WorkflowCancelResult{}, fmt.Errorf("workflow run publisher is required")
	}

	// The object name derives from the workflow definition's name and the
	// run's occurrence key -- the same derivation the trigger used, so a
	// caller that created the run can address it for cancellation.
	def, err := (&GetWorkflowUseCase{definitions: uc.definitions}).get(ctx, WorkflowGetInput{
		ID:              in.WorkflowID,
		TenantID:        in.TenantID,
		ResourceGroupID: in.ResourceGroupID,
	})
	if err != nil {
		return WorkflowCancelResult{}, err
	}
	name := v1alpha1.WorkflowRunObjectName(def.Name, run.OccurrenceKey)

	// A workflow that already reached a terminal phase succeeds without
	// patching: a finished workflow is final, and the current phase is the
	// answer. CancelRequested and CancelUnknown runs are patched again, which
	// is a no-op on the resource, so a retried request converges either way.
	if domainworkflow.Terminal(run.Phase) {
		return WorkflowCancelResult{
			Namespace:     def.Namespace,
			Name:          name,
			OccurrenceKey: run.OccurrenceKey,
			Phase:         string(run.Phase),
		}, nil
	}

	if err := uc.publisher.RequestCancel(ctx, def.Namespace, name); err != nil {
		return WorkflowCancelResult{}, fmt.Errorf("request cancel for workflow run %d: %w", in.RunID, err)
	}

	return WorkflowCancelResult{
		Namespace:     def.Namespace,
		Name:          name,
		OccurrenceKey: run.OccurrenceKey,
		Phase:         string(run.Phase),
	}, nil
}
