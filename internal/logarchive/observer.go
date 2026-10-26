package logarchive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	api "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/app/execution"
)

var runResource = schema.GroupVersionResource{Group: "workloads.orbitjob.io", Version: "v1alpha1", Resource: "jobruns"}

type Observer struct {
	Store      *Store
	Repository Repository
	Kube       kubernetes.Interface
	Dynamic    dynamic.Interface
	Namespaces map[string]string
	runCursors map[string]string
	podCursors map[string]string
}

func (o *Observer) Run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		pass, cancel := context.WithTimeout(ctx, 30*time.Second)
		if err := o.Tick(pass); err != nil {
			observationErrors.Inc()
			slog.WarnContext(ctx, "execution archive observation", "error", err)
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (o *Observer) register(ctx context.Context, run *api.JobRun) (Identity, error) {
	tenant := o.Namespaces[run.Namespace]
	if tenant == "" {
		return Identity{}, fmt.Errorf("unmapped archive namespace")
	}
	id := Identity{TenantID: tenant, SourceUID: run.Spec.ScheduledJobRef.UID, SourceName: run.Spec.ScheduledJobRef.Name, OccurrenceKey: run.Spec.OccurrenceKey, Namespace: run.Namespace, RevisionID: run.Spec.DefinitionRevision, RunName: run.Name, RunUID: string(run.UID), CreatedAt: run.CreationTimestamp.Time}
	id, phase, finished, err := o.Repository.Discover(ctx, id)
	if err != nil {
		return id, err
	}
	if finished != nil && o.Store.limits.Now().Sub(*finished) > Retention {
		if _, e := o.Store.Snapshot(id); e == ErrMissing {
			return id, ErrMissing
		}
	}
	if err = o.Store.Register(id); err != nil {
		if errors.Is(err, ErrCapacity) {
			m := Manifest{Identity: id, State: "capacity", CompletedAt: finished}
			if finished != nil {
				until := o.Store.limits.Now().Add(Retention)
				m.ProtectedUntil = &until
			}
			if syncErr := o.Repository.Sync(ctx, m); syncErr != nil {
				return id, syncErr
			}
		}
		return id, err
	}
	if err = o.Store.Append(id, runEvents(id, run, phase)); err != nil && err != ErrSealed {
		return id, err
	}
	if finished != nil {
		if err = o.Store.Complete(id, phase, *finished); err != nil {
			return id, err
		}
	}
	m, err := o.Store.Snapshot(id)
	if err != nil {
		return id, err
	}
	if m.CompletedAt != nil && m.SealedAt == nil && time.Since(*m.CompletedAt) >= 30*time.Second && time.Since(m.UpdatedAt) >= 3*time.Second {
		if err = o.Store.Seal(id); err != nil {
			return id, err
		}
		m, _ = o.Store.Snapshot(id)
	}
	if err = o.Repository.Sync(ctx, m); err != nil {
		return id, err
	}
	requested, err := o.Repository.CleanupRequested(ctx, id)
	if err != nil {
		return id, err
	}
	if requested && m.SealedAt != nil && m.State != "deleted" {
		if err = o.Store.Delete(id, true); err != nil {
			return id, err
		}
		m, _ = o.Store.Snapshot(id)
		err = o.Repository.Sync(ctx, m)
	}
	return id, err
}

// ResolvePod verifies the complete live owner chain before persisting a source.
func (o *Observer) ResolvePod(ctx context.Context, namespace, podName, podUID string, restart int32) (Identity, Source, error) {
	if o.Namespaces[namespace] == "" {
		return Identity{}, Source{}, fmt.Errorf("%w: unmapped collector namespace", ErrInvalidSource)
	}
	pod, err := o.Kube.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return Identity{}, Source{}, fmt.Errorf("%w: source Pod no longer retained", ErrInvalidSource)
		}
		return Identity{}, Source{}, err
	}
	if string(pod.UID) != podUID {
		return Identity{}, Source{}, fmt.Errorf("%w: collector Pod UID mismatch", ErrInvalidSource)
	}
	owner := metav1.GetControllerOf(pod)
	if owner == nil || owner.APIVersion != "batch/v1" || owner.Kind != "Job" {
		return Identity{}, Source{}, fmt.Errorf("%w: Pod has no execution Job owner", ErrInvalidSource)
	}
	job, err := o.Kube.BatchV1().Jobs(namespace).Get(ctx, owner.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return Identity{}, Source{}, fmt.Errorf("%w: source Job no longer retained", ErrInvalidSource)
		}
		return Identity{}, Source{}, err
	}
	if job.UID != owner.UID {
		return Identity{}, Source{}, fmt.Errorf("%w: collector Job UID mismatch", ErrInvalidSource)
	}
	identity, err := execution.IdentityFromJob(job)
	if err != nil {
		return Identity{}, Source{}, fmt.Errorf("%w: source is not a valid managed execution", ErrInvalidSource)
	}
	for _, key := range []string{execution.LabelRunName, execution.LabelRunUID, execution.LabelRevisionID, execution.LabelAttempt} {
		if pod.Labels[key] != job.Labels[key] {
			return Identity{}, Source{}, fmt.Errorf("%w: collector Pod execution label mismatch", ErrInvalidSource)
		}
	}
	ref := metav1.GetControllerOf(job)
	if ref == nil || ref.Kind != "JobRun" || ref.APIVersion != api.GroupVersion.String() || ref.Name != identity.RunName || string(ref.UID) != identity.RunUID {
		return Identity{}, Source{}, fmt.Errorf("%w: collector JobRun owner mismatch", ErrInvalidSource)
	}
	raw, err := o.Dynamic.Resource(runResource).Namespace(namespace).Get(ctx, identity.RunName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return Identity{}, Source{}, fmt.Errorf("%w: source JobRun no longer retained", ErrInvalidSource)
		}
		return Identity{}, Source{}, err
	}
	var run api.JobRun
	if err = runtime.DefaultUnstructuredConverter.FromUnstructured(raw.Object, &run); err != nil {
		return Identity{}, Source{}, err
	}
	if string(run.UID) != identity.RunUID {
		return Identity{}, Source{}, fmt.Errorf("%w: collector JobRun UID mismatch", ErrInvalidSource)
	}
	if identity.RevisionID != strconv.FormatInt(run.Spec.DefinitionRevision, 10) {
		return Identity{}, Source{}, fmt.Errorf("%w: Job revision differs from pinned JobRun", ErrInvalidSource)
	}
	if len(pod.Spec.Containers) != 1 || pod.Spec.Containers[0].Name != "task" {
		return Identity{}, Source{}, fmt.Errorf("%w: unexpected execution container", ErrInvalidSource)
	}
	id, err := o.register(ctx, &run)
	if err != nil {
		return id, Source{}, err
	}
	source := Source{Namespace: namespace, PodName: podName, PodUID: podUID, JobUID: string(job.UID), Attempt: identity.Attempt, Container: "task", Restart: restart}
	if err = o.Repository.VerifyAttempt(ctx, id, source); err != nil {
		return id, Source{}, err
	}
	return id, source, o.Store.Bind(id, source)
}

func (o *Observer) Tick(ctx context.Context) error {
	if o.runCursors == nil {
		o.runCursors = map[string]string{}
	}
	if o.podCursors == nil {
		o.podCursors = map[string]string{}
	}
	var failures []error
	for namespace := range o.Namespaces {
		if err := o.observeRuns(ctx, namespace); err != nil {
			failures = append(failures, err)
		}
		pods, err := o.Kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: execution.LabelRunUID, Limit: 250, Continue: o.podCursors[namespace]})
		if err != nil {
			o.podCursors[namespace] = ""
			failures = append(failures, err)
			continue
		}
		o.podCursors[namespace] = pods.Continue
		for _, pod := range pods.Items {
			if err = o.observePod(ctx, pod); err != nil && err != ErrSealed {
				slog.DebugContext(ctx, "archive Pod observation pending", "error", err)
			}
		}
	}
	_, err := o.Store.Sweep()
	if err != nil {
		failures = append(failures, err)
	}
	for _, tenant := range o.Namespaces {
		if err = o.maintainDirectory(ctx, tenant); err != nil {
			failures = append(failures, err)
		}
		if err = o.Repository.Reap(ctx, tenant); err != nil {
			failures = append(failures, err)
		}
	}
	if err = o.Store.Reap(); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func (o *Observer) observeRuns(ctx context.Context, namespace string) error {
	continuation := o.runCursors[namespace]
	for page := 0; page < 20; page++ {
		runs, err := o.Dynamic.Resource(runResource).Namespace(namespace).List(ctx, metav1.ListOptions{Limit: 25, Continue: continuation})
		if err != nil {
			o.runCursors[namespace] = ""
			return err
		}
		for _, raw := range runs.Items {
			var run api.JobRun
			if err = runtime.DefaultUnstructuredConverter.FromUnstructured(raw.Object, &run); err != nil {
				return err
			}
			if _, err = o.register(ctx, &run); err != nil {
				slog.DebugContext(ctx, "archive execution registration pending", "error", err)
			}
		}
		continuation = runs.GetContinue()
		o.runCursors[namespace] = continuation
		if continuation == "" {
			return nil
		}
	}
	return nil
}

func eventID(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		_, _ = h.Write([]byte(p))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
