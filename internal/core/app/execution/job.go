package execution

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Label keys stamped on every Job the control plane creates. They are the only
// link from an observed Kubernetes object back to platform state, so they must
// be written on create and treated as immutable.
const (
	AnnotationRunName            = "orbitjob.io/full-job-run"
	LabelRunName                 = "orbitjob.io/job-run"
	LabelRunUID                  = "orbitjob.io/job-run-uid"
	LabelRevisionID              = "orbitjob.io/revision-id"
	LabelAttempt                 = "orbitjob.io/attempt"
	LabelExecutionNetworkProfile = "orbitjob.io/execution-network-profile"
	RestrictedNetworkProfile     = "restricted-v1"
	// TerminationEvidenceFinalizer retains workload exits until their durable
	// attempt records authorize releasing this controller's evidence hold.
	TerminationEvidenceFinalizer = "orbitjob.io/termination-evidence"
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
	ExecutionPolicyVersion int32
	Image                  string
	Command                []string
	Args                   []string
	BackoffLimit           int32
	Resources              corev1.ResourceRequirements
	// ActiveDeadlineSeconds bounds the Job in Kubernetes itself, so the deadline
	// still applies when the operator is down. Zero means no deadline.
	ActiveDeadlineSeconds int64
}

// BuildJob renders the Kubernetes Job for one attempt. It copies slices so a
// caller mutating the revision cannot change the desired Job after the fact.
func BuildJob(identity Identity, namespace string, template Template) *batchv1.Job {
	backoff := template.BackoffLimit
	automountServiceAccountToken := false
	if backoff < 0 {
		backoff = 0
	}
	labels := map[string]string{
		LabelRunName:    RunLabel(identity.RunName),
		LabelRunUID:     identity.RunUID,
		LabelRevisionID: identity.RevisionID,
		LabelAttempt:    itoa(identity.Attempt),
	}
	if template.ExecutionPolicyVersion == 1 {
		labels[LabelExecutionNetworkProfile] = RestrictedNetworkProfile
	}
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        JobName(identity),
			Namespace:   namespace,
			Labels:      labels,
			Annotations: map[string]string{AnnotationRunName: identity.RunName},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoff,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels, Finalizers: []string{TerminationEvidenceFinalizer}},
				Spec: corev1.PodSpec{
					// User workloads do not call the Kubernetes API. Disabling the
					// projected credential here protects every tenant namespace,
					// including namespaces created after the chart was installed.
					AutomountServiceAccountToken: &automountServiceAccountToken,
					// The platform owns retry. A Pod that fails is one failed
					// attempt inside this Job, never a silent in-Pod restart.
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{{
						Name:      "task",
						Image:     template.Image,
						Command:   append([]string(nil), template.Command...),
						Args:      append([]string(nil), template.Args...),
						Resources: *template.Resources.DeepCopy(),
					}},
				},
			},
		},
	}
	if identity.RunUID != "" {
		controller, block := true, true
		job.OwnerReferences = []metav1.OwnerReference{{APIVersion: "workloads.orbitjob.io/v1alpha1", Kind: "JobRun", Name: identity.RunName, UID: types.UID(identity.RunUID), Controller: &controller, BlockOwnerDeletion: &block}}
	}
	if template.ActiveDeadlineSeconds > 0 {
		deadline := template.ActiveDeadlineSeconds
		job.Spec.ActiveDeadlineSeconds = &deadline
	}
	if template.ExecutionPolicyVersion == 1 {
		noEscalation, nonRoot, user := false, true, int64(65532)
		job.Spec.Template.Spec.SecurityContext = &corev1.PodSecurityContext{
			RunAsNonRoot: &nonRoot, RunAsUser: &user,
			SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		}
		job.Spec.Template.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
			AllowPrivilegeEscalation: &noEscalation,
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		}
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
