package execution

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Label keys stamped on every Job the control plane creates. They are the only
// link from an observed Kubernetes object back to platform state, so they must
// be written on create and treated as immutable.
const (
	LabelRunName    = "orbitjob.io/job-run"
	LabelRunUID     = "orbitjob.io/job-run-uid"
	LabelRevisionID = "orbitjob.io/revision-id"
	LabelAttempt    = "orbitjob.io/attempt"
)

// Identity ties a Kubernetes Job to the platform objects it belongs to. RunUID is
// required because a JobRun name can be reused after deletion; the UID is what
// distinguishes a recreated run from the original.
type Identity struct {
	RunName    string
	RunUID     string
	RevisionID string
	Attempt    int
}

// Template is the executable part of a pinned definition revision.
type Template struct {
	Image        string
	Command      []string
	Args         []string
	BackoffLimit int32
	// ActiveDeadlineSeconds bounds the Job in Kubernetes itself, so the deadline
	// still applies when the operator is down. Zero means no deadline.
	ActiveDeadlineSeconds int64
}

// BuildJob renders the Kubernetes Job for one attempt. It copies slices so a
// caller mutating the revision cannot change the desired Job after the fact.
func BuildJob(identity Identity, namespace string, template Template) *batchv1.Job {
	backoff := template.BackoffLimit
	if backoff < 0 {
		backoff = 0
	}
	labels := map[string]string{
		LabelRunName:    identity.RunName,
		LabelRunUID:     identity.RunUID,
		LabelRevisionID: identity.RevisionID,
		LabelAttempt:    itoa(identity.Attempt),
	}
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      JobName(identity),
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoff,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					// The platform owns retry. A Pod that fails is one failed
					// attempt inside this Job, never a silent in-Pod restart.
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{{
						Name:    "task",
						Image:   template.Image,
						Command: append([]string(nil), template.Command...),
						Args:    append([]string(nil), template.Args...),
					}},
				},
			},
		},
	}
	if template.ActiveDeadlineSeconds > 0 {
		deadline := template.ActiveDeadlineSeconds
		job.Spec.ActiveDeadlineSeconds = &deadline
	}
	return job
}

// Phase is the platform's reading of a Kubernetes Job's status. It is driven by
// the terminal conditions rather than the succeeded/failed counters: a Job with
// failed Pods is still running while it retries, and a Job with succeeded Pods
// is not complete until it declares the Complete condition.
func Phase(job batchv1.Job) string {
	for _, condition := range job.Status.Conditions {
		if condition.Status != corev1.ConditionTrue {
			continue
		}
		switch condition.Type {
		case batchv1.JobComplete:
			return PhaseSucceeded
		case batchv1.JobFailed:
			return PhaseFailed
		}
	}
	if job.Status.Active > 0 {
		return PhaseRunning
	}
	return PhasePending
}

// Phase values reported for an observed Kubernetes Job.
const (
	PhasePending   = "Pending"
	PhaseRunning   = "Running"
	PhaseSucceeded = "Succeeded"
	PhaseFailed    = "Failed"
)
