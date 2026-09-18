package operator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/app/controlplane"
	"orbitjob/internal/core/app/execution"
	"orbitjob/internal/core/app/projection"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
	"orbitjob/internal/platform/metrics"
)

// RevisionStore projects and loads immutable definition revisions.
type RevisionStore interface {
	ApplyRevisionForTenant(ctx context.Context, tenantID string, rev revision.Revision) (int64, error)
	RevisionByID(ctx context.Context, tenantID string, id int64) (revision.Revision, error)
}

// RunStore owns run and attempt progress. Reads go through RunByOccurrence
// rather than the CR status subresource, so a forged status cannot change what
// actually executes.
type RunStore interface {
	CreateOccurrenceForTenant(ctx context.Context, tenantID string, run jobrun.JobRun, maxAttempts int, actor string) (jobrun.StoredRun, bool, error)
	RunByOccurrence(ctx context.Context, tenantID, sourceUID, occurrenceKey string) (jobrun.StoredRun, bool, error)
	// CreateAttemptForTenant records the attempt and advances the run to
	// runPhase in one transaction, writing the audit row there too. The phase is
	// the caller's because only the caller knows whether this is a first attempt
	// or a retry.
	CreateAttemptForTenant(ctx context.Context, tenantID string, runID int64, attempt jobrun.Attempt, runPhase jobrun.Phase) error
	UpdateAttemptPhase(ctx context.Context, tenantID, jobName, phase, jobUID, resourceVersion string) (int64, int, error)
	// UpdateRunPhase advances a run and reports whether the run's phase
	// changed. Terminal-phase bookkeeping fires only on a real transition, so a
	// resync that re-observes a finished run costs nothing downstream.
	UpdateRunPhase(ctx context.Context, tenantID string, runID int64, phase jobrun.Phase, attempt int) (bool, error)
}

// TerminalOutcomeRecorder receives the runs whose phase genuinely changed to a
// terminal value. It is the one hook where the check read model, the SLI
// snapshots and the check metrics are written, so all three describe the same
// transition exactly once. Implementation is optional: a runtime without one
// simply records no outcome bookkeeping.
type TerminalOutcomeRecorder interface {
	RecordTerminalPhase(ctx context.Context, tenantID string, runID int64, phase jobrun.Phase)
}

// JobManager drives Kubernetes Jobs for the control plane. Ensure and Delete must
// be idempotent: a reconcile that repeats after a crash has to converge on the
// same cluster state rather than erroring.
type JobManager interface {
	Ensure(ctx context.Context, desired *batchv1.Job) (*batchv1.Job, error)
	Get(ctx context.Context, namespace, name string) (*batchv1.Job, bool, error)
	Delete(ctx context.Context, namespace, name string) error
}

// Runtime is the composition root that turns watched Kubernetes objects into
// platform state and back. Every handler is idempotent: it reads current stored
// state, computes the desired action, and writes only what is missing.
type Runtime struct {
	Dynamic   dynamic.Interface
	Jobs      JobManager
	Revisions RevisionStore
	Runs      RunStore
	Tenants   TenantResolver
	// Workflows, when set, is the workflow ledger the WorkflowRun reconcile
	// materializes into and the workflow walker advances from. A runtime
	// without one refuses workflow run reconciliation loudly rather than
	// silently ignoring the CR.
	Workflows WorkflowRunStore
	// OutcomeReader loads a run's terminal facts for the bookkeeping hooks
	// that need more than the run id — the function_runs read model records
	// the occurrence key's run id and the attempt timing.
	OutcomeReader controlplane.OutcomeReader
	// Outcomes, when set, receives every run whose phase genuinely changed to
	// Succeeded or Failed, and maintains the check read model, SLI snapshots
	// and check metrics from that one fact.
	Outcomes TerminalOutcomeRecorder
	// Functions, when set, maintains the function_runs read model from the
	// same terminal transitions, routed by the function source-uid prefix.
	Functions FunctionRunRecorder
	Now       func() time.Time
}

// Handlers renders the runtime as the callbacks the informer dispatcher expects.
func (r Runtime) Handlers() DynamicHandler {
	return DynamicHandler{
		Client:                r.Dynamic,
		ReconcileScheduledJob: r.ReconcileScheduledJob,
		ReconcileJobRun:       r.ReconcileJobRun,
		ReconcileJob:          r.ReconcileJob,
		ReconcileWorkflowRun:  r.ReconcileWorkflowRun,
		ReconcileWorkflowJob:  r.ReconcileWorkflowJob,
	}
}

func (r Runtime) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

// ReconcileScheduledJob projects the declared spec as an immutable revision and
// reports the outcome on the CR status. It never creates runs: scheduling is the
// scheduler's job, and keeping the two apart is what stops an operator restart
// from firing a backlog of occurrences.
func (r Runtime) ReconcileScheduledJob(ctx context.Context, obj unstructured.Unstructured) error {
	if r.Revisions == nil || r.Tenants == nil {
		return fmt.Errorf("scheduled job reconciliation is not configured")
	}
	typed, err := scheduledJobFromUnstructured(obj)
	if err != nil {
		return err
	}
	tenant, err := r.Tenants.Resolve(obj)
	if err != nil {
		return err
	}
	revisionID, err := (projection.Service{Revisions: r.Revisions, Now: r.Now}).
		ApplyScheduledJob(ctx, typed, tenant, "orbitjob-operator")
	if err != nil {
		return fmt.Errorf("project scheduled job %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
	}
	return r.patchStatus(ctx, scheduledJobGVR, obj, map[string]any{
		"observedGeneration": typed.Generation,
		"activeRevision":     revisionID,
	})
}

// ReconcileWorkflowJob projects the declared workflow spec as an immutable
// revision and reports the outcome on the CR status, exactly as a ScheduledJob
// reconcile does one resource kind up. It never creates runs: firing the DAG
// is the workflow walker's job, and keeping the two apart is what stops an
// operator restart from fanning out a backlog of workflow executions.
func (r Runtime) ReconcileWorkflowJob(ctx context.Context, obj unstructured.Unstructured) error {
	if r.Revisions == nil || r.Tenants == nil {
		return fmt.Errorf("workflow job reconciliation is not configured")
	}
	typed, err := workflowJobFromUnstructured(obj)
	if err != nil {
		return err
	}
	tenant, err := r.Tenants.Resolve(obj)
	if err != nil {
		return err
	}
	revisionID, err := (projection.Service{Revisions: r.Revisions, Now: r.Now}).
		ApplyWorkflowJob(ctx, typed, tenant, "orbitjob-operator")
	if err != nil {
		return fmt.Errorf("project workflow job %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
	}
	return r.patchStatus(ctx, workflowJobGVR, obj, map[string]any{
		"observedGeneration": typed.Generation,
		"activeRevision":     revisionID,
	})
}

// ReconcileJobRun drives one logical run towards its next attempt. The run row is
// the source of truth for how many attempts exist and how many are permitted, so
// a re-reconcile after a restart lands on the same attempt number and therefore
// the same deterministic Job name.
func (r Runtime) ReconcileJobRun(ctx context.Context, obj unstructured.Unstructured) error {
	if r.Runs == nil || r.Revisions == nil || r.Tenants == nil {
		return fmt.Errorf("job run reconciliation is not configured")
	}
	spec, err := jobRunSpecFromUnstructured(obj)
	if err != nil {
		return err
	}
	tenant, err := r.Tenants.Resolve(obj)
	if err != nil {
		return err
	}

	stored, found, err := r.Runs.RunByOccurrence(ctx, tenant, spec.ScheduledJobRef.UID, spec.OccurrenceKey)
	if err != nil {
		return err
	}
	if !found {
		stored, found, err = r.materializeManualRun(ctx, tenant, spec)
		if err != nil {
			return err
		}
		if !found {
			// A CR with no stored run and no manual trigger is not one this
			// operator creates state for: the scheduler owns the creation of
			// scheduled runs, and doing its job here would let a JobRun written
			// by anyone with CR access bypass occurrence deduplication and the
			// concurrency policy.
			return fmt.Errorf("no stored run for occurrence %s", spec.OccurrenceKey)
		}
	}

	// Terminal first: a run that already finished must not be dragged back into
	// cancellation by a stale request flag, which would otherwise fail forever
	// and pin the reconcile loop on an outcome that can never change.
	if jobrun.Terminal(stored.Phase) {
		return r.patchRunStatus(ctx, obj, stored)
	}
	// A cancel request is intent, not fact. Drive it to completion before any
	// other branch: a run the user asked to stop must not start another attempt.
	if stored.Phase == jobrun.CancelRequested || spec.CancelRequested {
		return r.reconcileCancellation(ctx, tenant, obj, stored, spec)
	}
	// One attempt at a time. The attempt's Job is live, so starting another would
	// run the same occurrence concurrently and recording an outcome would report a
	// result that has not happened yet. Wait for the Job observation to move the
	// run; the status is still derived from stored state, so a drifted CR status
	// is repaired here.
	if stored.AttemptInFlight() {
		return r.patchRunStatus(ctx, obj, stored)
	}
	if !stored.CanStartAttempt() {
		// No attempt is in flight and none may start: the attempt budget is spent.
		// Record the outcome once and stop.
		failed, err := jobrun.Transition(jobrun.JobRun{Phase: stored.Phase}, jobrun.Failed)
		if err != nil {
			return nil // already in a phase that cannot fail again
		}
		phaseChanged, err := r.Runs.UpdateRunPhase(ctx, tenant, stored.ID, failed.Phase, stored.Attempt)
		if err != nil {
			return err
		}
		metrics.OperatorRunsFailedTotal.Inc()
		r.recordOutcome(ctx, tenant, stored.ID, failed.Phase, phaseChanged)
		r.recordFunctionOutcome(ctx, tenant, stored.ID, failed.Phase, phaseChanged)
		stored.Phase = failed.Phase
		return r.patchRunStatus(ctx, obj, stored)
	}

	attemptNumber := stored.NextAttempt()
	rev, err := r.Revisions.RevisionByID(ctx, tenant, stored.RevisionID)
	if err != nil {
		return err
	}
	var declared v1alpha1.ScheduledJobSpec
	if err := json.Unmarshal([]byte(rev.NormalizedSpec), &declared); err != nil {
		return fmt.Errorf("decode pinned revision %d: %w", rev.ID, err)
	}

	identity := execution.Identity{
		RunName:    obj.GetName(),
		RunUID:     string(obj.GetUID()),
		RevisionID: fmt.Sprint(rev.ID),
		Attempt:    attemptNumber,
	}
	desired := execution.BuildJob(identity, obj.GetNamespace(), execution.Template{
		Image:                 declared.JobTemplate.Image,
		Command:               declared.JobTemplate.Command,
		Args:                  declared.JobTemplate.Args,
		BackoffLimit:          declared.JobTemplate.BackoffLimit,
		ActiveDeadlineSeconds: int64(spec.TimeoutSeconds),
	})

	job, err := r.Jobs.Ensure(ctx, desired)
	if err != nil {
		metrics.OperatorJobCreationFailuresTotal.Inc()
		return fmt.Errorf("ensure kubernetes job for run %s attempt %d: %w", identity.RunName, attemptNumber, err)
	}
	// Ensure is idempotent by namespace and name, so a Job that already carries
	// this name is adopted. Name is not proof of ownership: a run name repeats
	// when a ScheduledJob is deleted and recreated, and a Job from before that
	// can still be there. Recording a foreign Job's UID would make this run
	// drive a workload it does not own, so the labels are checked before the
	// attempt row is written.
	if err := execution.ValidateOwnership(job, identity); err != nil {
		return fmt.Errorf("kubernetes job %s/%s: %w", job.Namespace, job.Name, err)
	}
	if job.UID == "" {
		return fmt.Errorf("kubernetes job %s has no uid; refusing to record unverifiable ownership", job.Name)
	}

	attempt := jobrun.Attempt{
		Number:                  attemptNumber,
		KubernetesJobName:       job.Name,
		KubernetesJobUID:        string(job.UID),
		ObservedResourceVersion: job.ResourceVersion,
		Phase:                   jobrun.CreatingAttempt,
		StartedAt:               r.now(),
	}
	// The attempt row, the run's advance to Running and the audit entry commit
	// together inside the store, so a crash here cannot leave the attempts table
	// ahead of the run counter and start a second Job for the same occurrence.
	if err := r.Runs.CreateAttemptForTenant(ctx, tenant, stored.ID, attempt, jobrun.Running); err != nil {
		if errors.Is(err, jobrun.ErrRunTerminal) {
			// The run finished between the read above and this write; the store
			// rolled the attempt back. The write that finished the run is what
			// patches the CR status, so there is nothing left to do here.
			return nil
		}
		return err
	}
	stored.Phase = jobrun.Running
	stored.Attempt = attemptNumber
	return r.patchRunStatus(ctx, obj, stored)
}

// materializeManualRun creates the ledger row for a JobRun that arrived as a
// bare Custom Resource, and reports whether the run is now this operator's to
// drive.
//
// The admin API cannot write the ledger itself -- it holds SELECT only on the
// control-plane tables -- so a manual trigger travels as a JobRun and the
// operator, which owns the tables, writes the row from the CR's declared spec.
// The row, its actor and its audit entry commit in one transaction inside the
// store, and the caller then drives the first attempt in the same reconcile, so
// one pass takes a manual trigger from declaration to execution. A reconcile
// that repeats finds the row and skips this path, so it is idempotent.
//
// Only the CR-first triggers are materialized here: Manual, and Function --
// an HTTP function invocation is CR-first exactly like a manual trigger, and
// distinct only so ledger readers can count invocations without parsing
// occurrence keys. A scheduled occurrence always has its row committed before
// its Custom Resource is published, so a Schedule-trigger CR with no row was
// not written by the scheduler and is left alone rather than allowed to bypass
// occurrence deduplication and concurrency. The same holds for a Workflow step
// CR: the walker commits the step row before publishing, so a Workflow-trigger
// CR with no row is refused.
//
// The pinned revision must exist for the tenant. That is what stops a forged CR
// from inventing work: the run is bound to a definition the tenant already
// declared, and the revision's retry policy is the run's.
func (r Runtime) materializeManualRun(ctx context.Context, tenant string, spec v1alpha1.JobRunSpec) (jobrun.StoredRun, bool, error) {
	if spec.Trigger != v1alpha1.Manual && spec.Trigger != v1alpha1.Function {
		return jobrun.StoredRun{}, false, nil
	}
	rev, err := r.Revisions.RevisionByID(ctx, tenant, spec.DefinitionRevision)
	if err != nil {
		return jobrun.StoredRun{}, false, fmt.Errorf("load pinned revision %d for manual run: %w", spec.DefinitionRevision, err)
	}
	// The run is deduplicated under the definition identity and pinned to a
	// revision; if the two disagree, the CR is tying one definition's occurrence
	// key to another definition's spec, and the ledger would report a run the
	// definition never declared. Refuse it.
	if rev.Identity.SourceUID != spec.ScheduledJobRef.UID {
		return jobrun.StoredRun{}, false, fmt.Errorf(
			"manual run %s pins revision %d of definition %s, not %s",
			spec.OccurrenceKey, spec.DefinitionRevision, rev.Identity.SourceUID, spec.ScheduledJobRef.UID)
	}
	var declared v1alpha1.ScheduledJobSpec
	if err := json.Unmarshal([]byte(rev.NormalizedSpec), &declared); err != nil {
		return jobrun.StoredRun{}, false, fmt.Errorf("decode pinned revision %d: %w", rev.ID, err)
	}
	stored, _, err := r.Runs.CreateOccurrenceForTenant(ctx, tenant, jobrun.JobRun{
		SourceUID:     spec.ScheduledJobRef.UID,
		RevisionID:    fmt.Sprint(spec.DefinitionRevision),
		OccurrenceKey: spec.OccurrenceKey,
		Trigger:       jobrun.Trigger(spec.Trigger),
	}, declared.RetryPolicy.EffectiveMaxAttempts(), spec.Actor)
	if err != nil {
		return jobrun.StoredRun{}, false, fmt.Errorf("create manual run for occurrence %s: %w", spec.OccurrenceKey, err)
	}
	// Only the CR-first triggers count here. A scheduled occurrence is created
	// by the scheduler, and counting it would make this series say "triggered
	// by a person" when nothing was.
	metrics.OperatorRunsCreatedTotal.Inc()
	return stored, true, nil
}
