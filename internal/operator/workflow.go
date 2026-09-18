package operator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/app/controlplane"
	"orbitjob/internal/core/domain/function"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
	"orbitjob/internal/core/domain/workflow"
)

// WorkflowRunStore is the operator's view of the workflow_run_control_plane
// ledger: the postgres contract (internal/core/store/postgres/contracts.go)
// narrowed to the methods reconciliation and the walker use. The operator is
// the only ledger writer — the admin API holds SELECT — so every state
// transition of a workflow run goes through these methods.
type WorkflowRunStore interface {
	// CreateRunForTenant inserts one workflow run for the pinned revision,
	// deduplicating on (source_uid, occurrence_key). created is false when the
	// occurrence already exists, which is what makes a re-delivered CR
	// converge on one row instead of two.
	CreateRunForTenant(ctx context.Context, tenantID string, run workflow.Run) (stored workflow.Run, created bool, err error)
	// RunByOccurrence loads one run by its dedup identity.
	RunByOccurrence(ctx context.Context, tenantID, sourceUID, occurrenceKey string) (workflow.Run, bool, error)
	// OpenRuns lists the tenant's non-terminal workflow runs — the walker's
	// input. A run at CancelRequested is open: its steps still need observing
	// even though no new step may be created.
	OpenRuns(ctx context.Context, tenantID string) ([]workflow.Run, error)
	// Steps lists the step runs grouped under one workflow run, ordered by id.
	Steps(ctx context.Context, tenantID string, workflowRunID int64) ([]workflow.StepRun, error)
	// CreateStepForTenant inserts one step run and groups it under the
	// workflow run in the same transaction, deduplicating on the step's
	// occurrence key.
	CreateStepForTenant(ctx context.Context, tenantID string, workflowRunID int64, step workflow.StepRun, maxAttempts int, actor string) (stored workflow.StepRun, created bool, err error)
	// UpdatePhase advances a workflow run's phase and merges the decision
	// trail, refusing a write to a run that already reached a terminal phase.
	UpdatePhase(ctx context.Context, tenantID string, runID int64, phase workflow.Phase, decisions map[string]workflow.TaskDecision) (changed bool, err error)
}

// WorkflowRevisionSource loads definition revisions the workflow surface
// needs: the pinned workflow revision a run decodes from, and the tenant's
// active revisions, which resolve a task's JobRef name onto the definition's
// active revision and a step's source uid back onto its definition.
type WorkflowRevisionSource interface {
	RevisionByID(ctx context.Context, tenantID string, id int64) (revision.Revision, error)
	ActiveRevisions(ctx context.Context, tenantID string) ([]revision.Revision, error)
}

// WorkflowHistoryPruner is the store's workflow-retention support: listing a
// workflow definition's terminal runs beyond its retained history, and
// deleting one run steps-then-row in one transaction, so the RESTRICT foreign
// key between steps and the workflow row can never orphan a step.
type WorkflowHistoryPruner interface {
	PrunableRuns(ctx context.Context, tenantID, sourceUID string, keepSuccessful, keepFailed int) ([]workflow.Run, error)
	DeleteRun(ctx context.Context, tenantID string, runID int64) (bool, error)
}

// FunctionRunRecorder maintains the function_runs read model: one row per
// terminal function invocation, upserted by the operator's terminal-phase
// bookkeeping — the hook pattern the check read model and the SLI snapshots
// share.
type FunctionRunRecorder interface {
	RecordCompleted(ctx context.Context, tenantID string, record function.CompletedRecord) error
}

// workflowRunSpecFromUnstructured decodes only the spec, mirroring
// jobRunSpecFromUnstructured: a status write by another writer cannot
// influence how this operator interprets the run. The CR shape's own Validate
// answers every structural question (workflowRef, pinned revision, Manual
// trigger, actor, occurrence key), so a spec the schema would have refused is
// refused here too, not silently driven.
func workflowRunSpecFromUnstructured(obj unstructured.Unstructured) (v1alpha1.WorkflowRunSpec, error) {
	var spec v1alpha1.WorkflowRunSpec
	raw, found, err := unstructured.NestedMap(obj.Object, "spec")
	if err != nil || !found {
		return spec, fmt.Errorf("workflow run %s/%s has no spec", obj.GetNamespace(), obj.GetName())
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return spec, err
	}
	if err := json.Unmarshal(encoded, &spec); err != nil {
		return spec, fmt.Errorf("decode workflow run spec: %w", err)
	}
	if err := spec.Validate(); err != nil {
		return spec, fmt.Errorf("workflow run %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
	}
	if strings.TrimSpace(spec.Actor) == "" {
		return spec, fmt.Errorf("workflow run %s/%s has no actor", obj.GetNamespace(), obj.GetName())
	}
	return spec, nil
}

// ReconcileWorkflowRun materializes a WorkflowRun CR into the workflow ledger
// and projects stored state back onto the CR's status.
//
// The CR is the manual-trigger transport and the operator is the ledger's only
// writer, so the first reconcile of a CR creates its workflow_run_control_plane
// row — the CR-first flow materializeManualRun established for JobRuns. A
// re-delivered CR finds the row by (source_uid, occurrence_key) and skips the
// insert, so redelivery and resync are idempotent. After that the CR behaves
// like every read model in this package: the ledger is the state, the status
// subresource is a projection of it, and a drifted status is repaired here.
func (r Runtime) ReconcileWorkflowRun(ctx context.Context, obj unstructured.Unstructured) error {
	if r.Workflows == nil || r.Revisions == nil || r.Tenants == nil {
		return fmt.Errorf("workflow run reconciliation is not configured")
	}
	spec, err := workflowRunSpecFromUnstructured(obj)
	if err != nil {
		return err
	}
	tenant, err := r.Tenants.Resolve(obj)
	if err != nil {
		return err
	}

	stored, found, err := r.Workflows.RunByOccurrence(ctx, tenant, spec.WorkflowRef.UID, spec.OccurrenceKey)
	if err != nil {
		return err
	}
	if !found {
		stored, found, err = r.materializeWorkflowRun(ctx, tenant, spec)
		if err != nil {
			return err
		}
		if !found {
			// materializeWorkflowRun refuses only a non-Manual trigger, and the
			// spec gate above already refused those. Reaching this point means a
			// gate changed without the other following; refusing loudly keeps an
			// unmaterializable CR from spinning silently.
			return fmt.Errorf("no stored workflow run for occurrence %s", spec.OccurrenceKey)
		}
	}

	// A cancel request is intent; the ledger row must carry it before anything
	// else acts on the run. The walker reads the row, not the CR, so recording
	// it here — not just fanning out — is what stops new steps. CancelUnknown is
	// left alone: a stop the platform could not confirm already dominates the
	// run's roll-up, and overwriting it with the request would erase the one
	// honest signal the row holds.
	if spec.CancelRequested &&
		stored.Phase != workflow.PhaseCancelRequested &&
		stored.Phase != workflow.PhaseCancelUnknown &&
		!workflow.Terminal(stored.Phase) {
		if _, err := r.Workflows.UpdatePhase(ctx, tenant, stored.ID, workflow.PhaseCancelRequested, nil); err != nil {
			return err
		}
		stored.Phase = workflow.PhaseCancelRequested
	}

	return r.patchWorkflowStatus(ctx, obj, tenant, stored)
}

// materializeWorkflowRun creates the ledger row for a WorkflowRun that arrived
// as a bare Custom Resource, and reports whether the run is now this
// operator's to walk.
//
// The same gates as a manual JobRun: the pinned revision must exist for the
// tenant (what stops a forged CR from inventing work), the revision's identity
// must match the workflow reference (what stops a CR from tying one
// definition's occurrence key to another definition's spec), and the decoded
// DAG must validate (an invalid spec earns a refusal, not a half-advanced
// run). CreateRunForTenant deduplicates on (source_uid, occurrence_key), so a
// repeated materialization — a retried trigger, a resync redelivery — lands on
// the stored row unchanged.
func (r Runtime) materializeWorkflowRun(ctx context.Context, tenant string, spec v1alpha1.WorkflowRunSpec) (workflow.Run, bool, error) {
	// The WorkflowRun resource is the manual-trigger transport; the CRD's
	// trigger enum admits only Manual and the spec gate enforces it. This is
	// the belt to that suspenders: a scheduled workflow run is row-first with
	// no CR, and nothing else may materialize one.
	if spec.Trigger != v1alpha1.Manual {
		return workflow.Run{}, false, nil
	}
	rev, err := r.Revisions.RevisionByID(ctx, tenant, spec.DefinitionRevision)
	if err != nil {
		return workflow.Run{}, false, fmt.Errorf("load pinned revision %d for workflow run: %w", spec.DefinitionRevision, err)
	}
	if rev.Identity.SourceUID != spec.WorkflowRef.UID {
		return workflow.Run{}, false, fmt.Errorf(
			"workflow run %s pins revision %d of workflow %s, not %s",
			spec.OccurrenceKey, spec.DefinitionRevision, rev.Identity.SourceUID, spec.WorkflowRef.UID)
	}
	var declared v1alpha1.WorkflowJobSpec
	if err := json.Unmarshal([]byte(rev.NormalizedSpec), &declared); err != nil {
		return workflow.Run{}, false, fmt.Errorf("decode pinned revision %d: %w", rev.ID, err)
	}
	if err := workflow.ValidateDAG(workflow.DAGFromSpec(declared)); err != nil {
		return workflow.Run{}, false, fmt.Errorf("workflow %s revision %d is not a valid DAG: %w", rev.Identity.Name, rev.ID, err)
	}
	stored, _, err := r.Workflows.CreateRunForTenant(ctx, tenant, workflow.Run{
		SourceUID:     spec.WorkflowRef.UID,
		RevisionID:    spec.DefinitionRevision,
		OccurrenceKey: spec.OccurrenceKey,
		Trigger:       jobrun.Manual,
		Actor:         spec.Actor,
		Phase:         workflow.PhasePending,
	})
	if err != nil {
		return workflow.Run{}, false, fmt.Errorf("create workflow run for occurrence %s: %w", spec.OccurrenceKey, err)
	}
	return stored, true, nil
}

// workflowStatus builds the read model the operator patches onto a WorkflowRun
// CR: the ledger row's phase plus one entry per task of the pinned DAG. A task
// with a step reports the step's facts; a task without one reports its skip
// decision, which is the only record of why it never ran. StartedAt is the
// row's creation — the honest instant the run came to exist — and CompletedAt
// is the row's last write once terminal; per-attempt timing lives on the step
// rows and is deliberately not approximated here.
func (r Runtime) workflowStatus(ctx context.Context, tenant string, row workflow.Run) (v1alpha1.WorkflowRunStatus, error) {
	status := v1alpha1.WorkflowRunStatus{Phase: string(row.Phase)}
	status.StartedAt = &metav1.Time{Time: row.CreatedAt}
	if workflow.Terminal(row.Phase) {
		status.CompletedAt = &metav1.Time{Time: row.UpdatedAt}
	}

	rev, err := r.Revisions.RevisionByID(ctx, tenant, row.RevisionID)
	if err != nil {
		return status, fmt.Errorf("load pinned revision %d for status: %w", row.RevisionID, err)
	}
	var declared v1alpha1.WorkflowJobSpec
	if err := json.Unmarshal([]byte(rev.NormalizedSpec), &declared); err != nil {
		return status, fmt.Errorf("decode pinned revision %d: %w", rev.ID, err)
	}

	steps, err := r.Workflows.Steps(ctx, tenant, row.ID)
	if err != nil {
		return status, err
	}
	byKey := make(map[string]workflow.StepRun, len(steps))
	for _, step := range steps {
		byKey[step.OccurrenceKey] = step
	}

	status.Steps = make([]v1alpha1.WorkflowRunStatusStep, 0, len(declared.Tasks))
	for _, task := range declared.Tasks {
		key := workflow.StepOccurrenceKey(row.SourceUID, row.OccurrenceKey, task.Name)
		step, hasStep := byKey[key]
		if hasStep {
			status.Steps = append(status.Steps, v1alpha1.WorkflowRunStatusStep{
				Task:          task.Name,
				Phase:         string(step.Phase),
				Attempt:       int32(step.Attempt),
				OccurrenceKey: key,
			})
			continue
		}
		entry := v1alpha1.WorkflowRunStatusStep{Task: task.Name, OccurrenceKey: key}
		if decision, decided := row.TaskDecisions[task.Name]; decided && decision.Skipped {
			entry.Skipped = true
			entry.SkipReason = decision.Reason
		}
		status.Steps = append(status.Steps, entry)
	}
	return status, nil
}

// patchWorkflowStatus projects stored workflow state onto the CR that carries
// it. The reconcile path arrives from a live watch, so a missing object there
// is an error like any other write failure.
func (r Runtime) patchWorkflowStatus(ctx context.Context, obj unstructured.Unstructured, tenant string, row workflow.Run) error {
	fragment, err := workflowStatusFragment(ctx, r, tenant, row)
	if err != nil {
		return err
	}
	return r.patchStatus(ctx, workflowRunGVR, obj, fragment)
}

// patchWorkflowStatusByName is the walker's projection path: it addresses the
// CR by the pinned revision's identity, the same derivation every writer of a
// WorkflowRun uses. NotFound is success — a scheduled run has no CR — while
// any other failure is returned: a read model that silently stops projecting
// is a lie with a delay. Exported because the walker consumes it through the
// composition root's ProjectStatus closure.
func (r Runtime) PatchWorkflowStatusByName(ctx context.Context, tenant string, row workflow.Run) error {
	if r.Dynamic == nil {
		return fmt.Errorf("dynamic client is required")
	}
	rev, err := r.Revisions.RevisionByID(ctx, tenant, row.RevisionID)
	if err != nil {
		return fmt.Errorf("load pinned revision %d for status: %w", row.RevisionID, err)
	}
	fragment, err := workflowStatusFragment(ctx, r, tenant, row)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{"status": fragment})
	if err != nil {
		return err
	}
	name := v1alpha1.WorkflowRunObjectName(rev.Identity.Name, row.OccurrenceKey)
	_, err = r.Dynamic.Resource(workflowRunGVR).Namespace(rev.Identity.Namespace).
		Patch(ctx, name, types.MergePatchType, payload, metav1.PatchOptions{}, "status")
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("patch workflow run %s/%s status: %w", rev.Identity.Namespace, name, err)
	}
	return nil
}

// workflowStatusFragment renders the status read model as the patch payload
// patchStatus expects. The typed status is round-tripped through JSON so the
// wire keys stay owned by the one struct.
func workflowStatusFragment(ctx context.Context, r Runtime, tenant string, row workflow.Run) (map[string]any, error) {
	status, err := r.workflowStatus(ctx, tenant, row)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		return nil, err
	}
	var fragment map[string]any
	if err := json.Unmarshal(encoded, &fragment); err != nil {
		return nil, err
	}
	return fragment, nil
}

// functionRunID derives the function_runs read model's run id from the ledger
// occurrence key: a UUID-shaped rendering of the key's sha256, so a replayed
// recording addresses the same row. It is the same derivation the check read
// model uses; a run cannot be both a check and a function, so the two never
// collide.
func functionRunID(occurrenceKey string) string {
	digest := occurrenceKey
	if len(digest) != sha256.Size*2 {
		sum := sha256.Sum256([]byte(occurrenceKey))
		digest = hex.EncodeToString(sum[:])
	}
	return digest[0:8] + "-" + digest[8:12] + "-" + digest[12:16] + "-" + digest[16:20] + "-" + digest[20:32]
}

// functionTriggeredAt resolves the read model's triggered_at: the instant the
// invocation was created. The terminal-outcome read carries the newest
// attempt's instants but not the row's created_at, so a started invocation
// reports its start and one canceled before starting reports now — the closest
// honest answer available from this hook's inputs.
func functionTriggeredAt(outcome controlplane.TerminalOutcome, now time.Time) time.Time {
	if !outcome.StartedAt.IsZero() {
		return outcome.StartedAt
	}
	return now
}

// recordFunctionOutcome upserts the function_runs read model when a terminal
// transition belongs to a function invocation — the checkobserve pattern
// applied to the serverless surface: one hook, fired only on a real
// transition, keyed by the deterministic run id so a replay lands on the same
// row. A run whose source_uid does not carry the function prefix is someone
// else's outcome and is left alone. Like the check recorder, a failed read
// model write is logged, not returned: the phase write is the fact, and
// failing the reconcile would requeue into a run that can never transition
// again.
func (r Runtime) recordFunctionOutcome(ctx context.Context, tenant string, runID int64, phase jobrun.Phase, phaseChanged bool) {
	if r.Functions == nil || r.OutcomeReader == nil || !phaseChanged {
		return
	}
	status, ok := function.ReadModelStatus(phase)
	if !ok {
		return
	}
	outcome, err := r.OutcomeReader.TerminalOutcome(ctx, tenant, runID)
	if err != nil {
		slog.Error("read terminal outcome for function run failed", "tenant_id", tenant, "run_id", runID, "error", err)
		return
	}
	functionID, ok := function.IDFromSourceUID(outcome.SourceUID)
	if !ok {
		return
	}
	record := function.CompletedRecord{
		RunID:       functionRunID(outcome.OccurrenceKey),
		FunctionID:  functionID,
		Status:      status,
		TriggeredAt: functionTriggeredAt(outcome, r.now()),
		StartedAt:   outcome.StartedAt,
		FinishedAt:  outcome.CompletedAt,
	}
	if !outcome.StartedAt.IsZero() && !outcome.CompletedAt.IsZero() {
		record.DurationMs = int(outcome.CompletedAt.Sub(outcome.StartedAt).Milliseconds())
	}
	if err := r.Functions.RecordCompleted(ctx, tenant, record); err != nil {
		slog.Error("record function run read model failed", "tenant_id", tenant, "run_id", runID, "error", err)
	}
}
