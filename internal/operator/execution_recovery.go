package operator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/app/execution"
	"orbitjob/internal/core/domain/jobrun"
	corepostgres "orbitjob/internal/core/store/postgres"
)

type executionRecoveryStore interface {
	CurrentExecution(context.Context, string, int64) (jobrun.Execution, bool, error)
	ExecutionRecoveriesAfter(context.Context, string, int64, int) ([]jobrun.Execution, error)
	MarkExecutionUnknown(context.Context, string, jobrun.Execution, string) error
	ClearExecutionUnknown(context.Context, string, jobrun.Execution, string) error
	RecordTerminationEvidence(context.Context, string, jobrun.Execution, []byte) error
	InvalidatePodTerminationEvidence(context.Context, string, jobrun.Execution) error
}

var _ executionRecoveryStore = (*corepostgres.ControlPlaneRepository)(nil)
var executionObservations = promauto.NewCounterVec(prometheus.CounterOpts{Name: "orbitjob_execution_recovery_total", Help: "Execution recovery observations by bounded outcome."}, []string{"outcome"})

type ExecutionRecoveryCursor struct {
	mu            sync.Mutex
	After         map[string]int64
	EvidenceAfter map[string]int64
	nextTenant    int
}

func (r Runtime) RecoverExecutions(ctx context.Context, tenants []string, limit int, cursor *ExecutionRecoveryCursor) (int, error) {
	store, ok := r.Runs.(executionRecoveryStore)
	if !ok {
		return 0, fmt.Errorf("durable execution recovery store required")
	}
	if cursor == nil {
		return 0, fmt.Errorf("execution recovery cursor required")
	}
	cursor.mu.Lock()
	defer cursor.mu.Unlock()
	if cursor.After == nil {
		cursor.After = map[string]int64{}
	}
	if cursor.EvidenceAfter == nil {
		cursor.EvidenceAfter = map[string]int64{}
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	count := 0
	var failures []error
	start := 0
	if len(tenants) > 0 {
		start = cursor.nextTenant % len(tenants)
	}
	for offset := range tenants {
		index := (start + offset) % len(tenants)
		tenant := tenants[index]
		if err := ctx.Err(); err != nil {
			return count, errors.Join(append(failures, err)...)
		}
		cursor.nextTenant = (index + 1) % len(tenants)
		budget := r.ExecutionRecoveryTenantBudget
		if budget <= 0 {
			budget = 5 * time.Second
		}
		tenantCtx, cancelTenant := context.WithTimeout(ctx, budget)
		if evidenceStore, ok := r.Runs.(retainedEvidenceStore); ok {
			n, err := r.recoverPodEvidence(tenantCtx, tenant, limit, cursor, evidenceStore)
			count += n
			if err != nil {
				failures = append(failures, err)
			}
		}
		rows, err := store.ExecutionRecoveriesAfter(tenantCtx, tenant, cursor.After[tenant], limit)
		if err != nil {
			failures = append(failures, err)
			cancelTenant()
			continue
		}
		if len(rows) == 0 {
			cursor.After[tenant] = 0
			cancelTenant()
			continue
		}
		if len(rows) > limit {
			rows = rows[:limit]
		}
		for _, e := range rows {
			if tenantCtx.Err() != nil {
				failures = append(failures, tenantCtx.Err())
				break
			}
			cursor.After[tenant] = e.Run.ID
			if err := r.recoverExecution(tenantCtx, tenant, e); err != nil {
				executionObservations.WithLabelValues("error").Inc()
				slog.ErrorContext(ctx, "execution recovery failed", "tenant", tenant, "run_id", e.Run.ID, "error", err)
				failures = append(failures, err)
			} else {
				count++
			}
		}
		cancelTenant()
	}
	return count, errors.Join(failures...)
}

func (r Runtime) recoverExecution(ctx context.Context, tenant string, e jobrun.Execution) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	store := r.Runs.(executionRecoveryStore)
	if jobrun.EvidenceReleasePending(e.Run.TerminationEvidence) {
		evidenceStore, ok := r.Runs.(retainedEvidenceStore)
		if !ok {
			return fmt.Errorf("retained Pod evidence store required")
		}
		a, found, err := evidenceStore.RecordedExecution(ctx, tenant, e.Namespace, e.Run.AttemptJobName, e.Run.AttemptUID)
		if err != nil || !found {
			return err
		}
		job, _, err := r.Jobs.Get(ctx, e.Namespace, a.JobName)
		if err != nil {
			return err
		}
		settled, err := r.settlePodEvidence(ctx, tenant, a, job)
		if err != nil || !settled {
			return err
		}
	}
	if jobrun.Terminal(e.Run.Phase) {
		return nil
	}
	stopping := e.Run.Phase == jobrun.CancelRequested || e.Run.Phase == jobrun.CancelUnknown
	terminal := jobrun.Terminal(e.Run.AttemptPhase) || e.Run.AttemptPhase == jobrun.RetryWaiting
	if terminal && !stopping {
		phase := e.Run.AttemptPhase
		if phase == jobrun.Failed {
			phase = jobrun.RetryWaiting
		}
		changed, err := r.Runs.UpdateRunPhase(ctx, tenant, e.Run.ID, phase, e.Run.Attempt)
		if errors.Is(err, corepostgres.ErrRunChanged) || errors.Is(err, jobrun.ErrRunTerminal) {
			return nil
		}
		if err != nil {
			return err
		}
		if changed {
			executionObservations.WithLabelValues("terminal_repaired").Inc()
			if err = r.recordTerminal(ctx, tenant, e.Run.ID, phase); err != nil {
				return err
			}
		}
		return r.projectRecoveredExecution(ctx, tenant, e)
	}
	// Cancellation owns stop evidence even when the attempt already has a Job
	// terminal phase. Neither that phase nor a missing Job proves container exit.
	if stopping {
		return r.projectRecoveredExecution(ctx, tenant, e)
	}
	if r.Jobs == nil {
		return fmt.Errorf("job observer required")
	}
	job, found, err := r.Jobs.Get(ctx, e.Namespace, e.Run.AttemptJobName)
	if err != nil {
		return err
	}
	if e.Run.AttemptUID == "" {
		// An accepted Create may have lost its response. The original CR UID
		// persisted with the reservation is the only authority to bind it.
		if found && e.Run.OwnerRunUID != "" {
			id, identityErr := execution.IdentityFromJob(job)
			if identityErr == nil && id.RunUID == e.Run.OwnerRunUID &&
				slices.Contains(v1alpha1.RunNameCandidates(e.DefinitionName, e.OccurrenceKey), id.RunName) &&
				execution.ValidateOwnership(job, execution.Identity{RunName: id.RunName, RunUID: e.Run.OwnerRunUID, RevisionID: fmt.Sprint(e.Run.RevisionID), Attempt: e.Run.Attempt}) == nil {
				if err := r.Runs.BindAttemptObservation(ctx, tenant, e.Run.ID, e.Run.Attempt, job.Name, string(job.UID), job.ResourceVersion); err != nil {
					return err
				}
				current, ok, err := store.CurrentExecution(ctx, tenant, e.Run.ID)
				if err != nil || !ok {
					return err
				}
				return r.recoverExecution(ctx, tenant, current)
			}
		}
		if e.DispatchStage != jobrun.DispatchPrepared {
			reason := jobrun.ReasonJobNotFound
			if found {
				reason = jobrun.ReasonPodIdentityAmbiguous
			}
			if err := store.MarkExecutionUnknown(ctx, tenant, e, reason); err != nil && !errors.Is(err, corepostgres.ErrRunChanged) {
				return err
			}
			executionObservations.WithLabelValues("dispatch_unknown").Inc()
		}
		return r.projectRecoveredExecution(ctx, tenant, e)
	}
	if !found || string(job.UID) != e.Run.AttemptUID {
		reason := jobrun.ReasonJobNotFound
		if found {
			reason = jobrun.ReasonJobUIDMismatch
		}
		if !terminal {
			if err = store.MarkExecutionUnknown(ctx, tenant, e, reason); err != nil && !errors.Is(err, corepostgres.ErrRunChanged) {
				return err
			}
		}
		executionObservations.WithLabelValues("unknown").Inc()
		return r.projectRecoveredExecution(ctx, tenant, e)
	}
	id, err := execution.IdentityFromJob(job)
	if err != nil {
		return r.quarantineExecution(ctx, tenant, e)
	}
	names := v1alpha1.RunNameCandidates(e.DefinitionName, e.OccurrenceKey)
	if !slices.Contains(names, id.RunName) || id.RevisionID != fmt.Sprint(e.Run.RevisionID) || id.Attempt != e.Run.Attempt || id.RunUID == "" {
		return r.quarantineExecution(ctx, tenant, e)
	}
	if e.Run.OwnerRunUID != "" && id.RunUID != e.Run.OwnerRunUID {
		return r.quarantineExecution(ctx, tenant, e)
	}
	owned := false
	for _, owner := range job.OwnerReferences {
		if owner.Controller != nil && *owner.Controller && owner.Kind == "JobRun" && owner.APIVersion == v1alpha1.GroupVersion.String() && string(owner.UID) == id.RunUID {
			owned = true
		}
	}
	if !owned {
		return r.quarantineExecution(ctx, tenant, e)
	}
	if !stopping {
		if err = store.ClearExecutionUnknown(ctx, tenant, e, id.RunUID); err != nil && !errors.Is(err, corepostgres.ErrRunChanged) {
			return err
		}
		raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(job)
		if err != nil {
			return err
		}
		if err = r.ReconcileJob(ctx, unstructured.Unstructured{Object: raw}); err != nil {
			return err
		}
	}
	if !execution.RetainsTerminationEvidence(job) && (execution.Phase(*job) == execution.PhaseSucceeded || execution.Phase(*job) == execution.PhaseFailed) {
		proof, _ := json.Marshal(map[string]any{"source": "job", "job_uid": e.Run.AttemptUID, "resource_version": job.ResourceVersion, "observed_at": r.now()})
		if err = store.RecordTerminationEvidence(ctx, tenant, e, proof); err != nil && !errors.Is(err, corepostgres.ErrRunChanged) {
			return err
		}
	}
	return r.projectRecoveredExecution(ctx, tenant, e)
}

func (r Runtime) quarantineExecution(ctx context.Context, tenant string, e jobrun.Execution) error {
	if err := r.Runs.(executionRecoveryStore).MarkExecutionUnknown(ctx, tenant, e, jobrun.ReasonPodIdentityAmbiguous); err != nil && !errors.Is(err, corepostgres.ErrRunChanged) {
		return err
	}
	executionObservations.WithLabelValues("identity_conflict").Inc()
	return r.projectRecoveredExecution(ctx, tenant, e)
}

// CR recreation restores observation/cancellation, not execution ownership.
func (r Runtime) projectRecoveredExecution(ctx context.Context, tenant string, e jobrun.Execution) error {
	if r.Dynamic == nil {
		return fmt.Errorf("run CR client required")
	}
	store := r.Runs.(executionRecoveryStore)
	current, found, err := store.CurrentExecution(ctx, tenant, e.Run.ID)
	if err != nil || !found {
		return err
	}
	e = current
	expected := v1alpha1.RunIdentity{SourceUID: e.SourceUID, RevisionID: e.Run.RevisionID, OccurrenceKey: e.OccurrenceKey, Trigger: v1alpha1.Trigger(e.Trigger)}
	var obj *unstructured.Unstructured
	for _, name := range v1alpha1.RunNameCandidates(e.DefinitionName, e.OccurrenceKey) {
		candidate, getErr := r.Dynamic.Resource(jobRunGVR).Namespace(e.Namespace).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(getErr) {
			continue
		}
		if getErr != nil {
			return getErr
		}
		spec, decodeErr := jobRunSpecFromUnstructured(*candidate)
		if decodeErr != nil {
			return decodeErr
		}
		if spec.ScheduledJobRef.UID != expected.SourceUID || spec.DefinitionRevision != expected.RevisionID || spec.OccurrenceKey != expected.OccurrenceKey || spec.Trigger != expected.Trigger {
			return fmt.Errorf("recovery CR identity conflicts")
		}
		obj = candidate
		break
	}
	if obj == nil {
		rev, err := r.Revisions.RevisionByID(ctx, tenant, e.Run.RevisionID)
		if err != nil {
			return err
		}
		var declared v1alpha1.ScheduledJobSpec
		if err = json.Unmarshal([]byte(rev.NormalizedSpec), &declared); err != nil {
			return err
		}
		spec := v1alpha1.JobRunSpec{ScheduledJobRef: v1alpha1.ObjectReference{Name: e.DefinitionName, UID: e.SourceUID}, DefinitionRevision: e.Run.RevisionID, Trigger: expected.Trigger, Actor: e.Actor, OccurrenceKey: e.OccurrenceKey, TimeoutSeconds: declared.TimeoutSeconds, CancelRequested: e.Run.Phase == jobrun.CancelRequested || e.Run.Phase == jobrun.CancelUnknown}
		raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&v1alpha1.JobRun{TypeMeta: metav1.TypeMeta{APIVersion: v1alpha1.GroupVersion.String(), Kind: "JobRun"}, ObjectMeta: metav1.ObjectMeta{Name: v1alpha1.RunObjectName(e.DefinitionName, e.OccurrenceKey), Namespace: e.Namespace}, Spec: spec})
		if err != nil {
			return err
		}
		obj, err = r.Dynamic.Resource(jobRunGVR).Namespace(e.Namespace).Create(ctx, &unstructured.Unstructured{Object: raw}, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		if err != nil {
			return err
		}
	}
	if obj.GetDeletionTimestamp() != nil {
		return nil
	}
	if e.Run.Phase == jobrun.CancelRequested || e.Run.Phase == jobrun.CancelUnknown {
		spec, err := jobRunSpecFromUnstructured(*obj)
		if err != nil {
			return err
		}
		return r.reconcileCancellation(ctx, tenant, *obj, e.Run, spec)
	}
	return r.patchRunStatus(ctx, *obj, e.Run)
}

// transport/request budgets are owned by controller callers; a requeue is a
// hint to observe again, never permission to create a replacement attempt.
func (r Runtime) requeueRun(obj unstructured.Unstructured) {
	if r.EnqueueAfter != nil {
		r.EnqueueAfter("jobruns", obj.GetNamespace(), obj.GetName(), 5*time.Second)
	}
}
