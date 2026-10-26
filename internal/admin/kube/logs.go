package kube

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"orbitjob/internal/admin/logs"
	"orbitjob/internal/core/app/execution"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
)

type LogClient struct {
	Client           kubernetes.Interface
	Dynamic          dynamic.Interface
	NamespaceTenants map[string]string
}

func (l LogClient) Read(ctx context.Context, loc logs.Locator, in logs.Request) (logs.Result, error) {
	if in.TailLines == 0 {
		in.TailLines = 200
	}
	if in.LimitBytes == 0 {
		in.LimitBytes = 32768
	}
	out := logs.Result{State: "unavailable", TailLines: in.TailLines, LimitBytes: in.LimitBytes, Attempt: loc.Attempt, Container: "task"}
	if in.TailLines < 1 || in.TailLines > 500 || in.LimitBytes < 1 || in.LimitBytes > 65536 || loc.Attempt < 1 {
		return out, validation.New("logs", "invalid snapshot limits or attempt")
	}
	if l.NamespaceTenants[loc.Namespace] != loc.TenantID || loc.TenantID == "" {
		return out, &resource.ScopeError{Resource: "logs", Scope: "namespace tenant"}
	}
	if l.Client == nil || l.Dynamic == nil {
		return out, fmt.Errorf("log client unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	finish := func(state, reason string) (logs.Result, error) {
		out.State = state
		out.Reason = reason
		return out, nil
	}
	if loc.JobName == "" || loc.JobUID == "" {
		return finish("starting", "attempt has no observed Kubernetes Job")
	}
	job, err := l.Client.BatchV1().Jobs(loc.Namespace).Get(ctx, loc.JobName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return finish("absent", "recorded Job is no longer retained")
	}
	if err != nil {
		return logReadFailure(out, err)
	}
	deny := func() (logs.Result, error) {
		return out, &resource.ScopeError{Resource: "logs", Scope: "immutable execution ownership"}
	}
	labels := job.Labels
	identity, identityErr := execution.IdentityFromJob(job)
	if identityErr != nil || string(job.UID) != loc.JobUID || identity.RunUID == "" || identity.RevisionID != strconv.FormatInt(loc.RevisionID, 10) || identity.Attempt != loc.Attempt {
		return deny()
	}
	// Name alone is never sufficient: verify both the JobRun UID and its pinned
	// ledger identity, including function-sourced runs and historical revisions.
	run, err := (JobRunPublisher{Client: l.Dynamic}).Get(ctx, loc.Namespace, identity.RunName)
	if err != nil {
		var missing *resource.NotFoundError
		if errors.As(err, &missing) {
			return finish("absent", "immutable JobRun is unavailable")
		}
		return logReadFailure(out, err)
	}
	if string(run.UID) != identity.RunUID || run.Spec.ScheduledJobRef.UID != loc.SourceUID || run.Spec.ScheduledJobRef.Name != loc.SourceName || run.Spec.DefinitionRevision != loc.RevisionID || run.Spec.OccurrenceKey != loc.OccurrenceKey {
		return deny()
	}

	owner := metav1.GetControllerOf(job)
	if owner == nil {
		return finish("unavailable", "historical Job has no immutable JobRun controller reference")
	}
	if owner.APIVersion != "workloads.orbitjob.io/v1alpha1" || owner.Kind != "JobRun" || owner.Name != run.Name || owner.UID != run.UID {
		return deny()
	}

	pods, err := l.Client.CoreV1().Pods(loc.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "batch.kubernetes.io/controller-uid=" + loc.JobUID, Limit: 20})
	if err != nil {
		return logReadFailure(out, err)
	}
	if pods.Continue != "" {
		return finish("unavailable", "too many Pod candidates")
	}
	owned := make([]corev1.Pod, 0, len(pods.Items))
	for _, pod := range pods.Items {
		owner := metav1.GetControllerOf(&pod)
		if owner == nil || owner.APIVersion != "batch/v1" || owner.Kind != "Job" || string(owner.UID) != loc.JobUID || owner.Name != loc.JobName {
			return deny()
		}
		for _, key := range []string{execution.LabelRunName, execution.LabelRunUID, execution.LabelRevisionID, execution.LabelAttempt} {
			if pod.Labels[key] != labels[key] {
				return deny()
			}
		}
		if len(pod.Spec.Containers) != 1 || pod.Spec.Containers[0].Name != "task" {
			return deny()
		}
		owned = append(owned, pod)
	}
	if len(owned) == 0 {
		return finish("absent", "no retained Pod for this attempt")
	}
	sort.Slice(owned, func(i, j int) bool {
		if owned[i].CreationTimestamp.Equal(&owned[j].CreationTimestamp) {
			return string(owned[i].UID) > string(owned[j].UID)
		}
		return owned[i].CreationTimestamp.After(owned[j].CreationTimestamp.Time)
	})
	pod := owned[0]
	if pod.Status.Phase == corev1.PodPending {
		return finish("starting", "task container has not started")
	}
	// Request one extra byte to make truncation explicit while keeping the
	// returned UTF-8 text within the exact advertised byte limit.
	requestBytes := in.LimitBytes + 1
	stream, err := l.Client.CoreV1().Pods(loc.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{Container: "task", TailLines: &in.TailLines, LimitBytes: &requestBytes, Follow: false}).Stream(ctx)
	if err != nil {
		return logReadFailure(out, err)
	}
	defer func() { _ = stream.Close() }()
	data, err := io.ReadAll(io.LimitReader(stream, requestBytes))
	if err != nil {
		return logReadFailure(out, err)
	}

	// pods/log has no UID precondition. A final UID check prevents a Pod name
	// deleted/reused between discovery and log retrieval from releasing text
	// belonging to the replacement; an old UID cannot be recreated.
	confirmed, checkErr := l.Client.CoreV1().Pods(loc.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
	if checkErr != nil {
		return logReadFailure(out, checkErr)
	}
	if pod.UID == "" || confirmed.UID != pod.UID {
		return deny()
	}
	out.Truncated = int64(len(data)) > in.LimitBytes
	if out.Truncated {
		data = data[:in.LimitBytes]
	}
	out.Text = string(bytes.ToValidUTF8(data, nil))
	out.State = "ready"
	slog.InfoContext(ctx, "execution log snapshot", "tenant_id", loc.TenantID, "attempt", loc.Attempt, "bytes", len(out.Text), "truncated", out.Truncated)
	return out, nil
}
func logReadFailure(out logs.Result, err error) (logs.Result, error) {
	if apierrors.IsForbidden(err) {
		return out, &resource.ScopeError{Resource: "logs", Scope: "Kubernetes access"}
	}
	out.State = "unavailable"
	out.Reason = "Kubernetes log snapshot unavailable"
	if apierrors.IsNotFound(err) {
		out.State = "absent"
		out.Reason = "Pod logs are no longer retained"
	}
	return out, nil
}
