package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"orbitjob/internal/core/domain/jobrun"
)

type retainedProof struct {
	Source          string    `json:"source"`
	Protocol        string    `json:"protocol"`
	HoldRelease     string    `json:"hold_release"`
	Namespace       string    `json:"namespace"`
	JobName         string    `json:"job_name"`
	JobUID          string    `json:"job_uid"`
	ResourceVersion string    `json:"resource_version"`
	StopBoundary    string    `json:"stop_boundary"`
	At              time.Time `json:"observed_at"`
	Pods            []podFact `json:"pods"`
}

// RetainsTerminationEvidence is a creation-time protocol marker. Legacy Jobs
// must never gain this guarantee by retrofitting a finalizer during observation.
func RetainsTerminationEvidence(job *batchv1.Job) bool {
	return job != nil && slices.Contains(job.Spec.Template.Finalizers, TerminationEvidenceFinalizer)
}

// StopBoundary requires a Job-controller acknowledgement. Deletion alone does
// not acknowledge completion of an in-flight Pod create.
func StopBoundary(job *batchv1.Job) string {
	if job == nil {
		return ""
	}
	for _, condition := range job.Status.Conditions {
		if condition.Status != corev1.ConditionTrue {
			continue
		}
		switch condition.Type {
		case batchv1.JobFailureTarget:
			return "failure_target"
		case batchv1.JobSuccessCriteriaMet:
			return "success_criteria_met"
		case batchv1.JobFailed:
			return "failed"
		case batchv1.JobComplete:
			return "complete"
		}
	}
	return ""
}

// InspectRetainedTermination never replaces committed complete evidence with
// the smaller inventory left after a partial finalizer release.
func (a Adapter) InspectRetainedTermination(ctx context.Context, job *batchv1.Job, persisted []byte) ([]byte, bool, error) {
	if !RetainsTerminationEvidence(job) || job.UID == "" {
		return nil, false, fmt.Errorf("original retained Job identity required")
	}
	boundary := StopBoundary(job)
	trusted := jobrun.HasRetainedTerminationEvidence(persisted, string(job.UID))
	if jobrun.UsesPodEvidenceRetention(persisted) && !trusted {
		return nil, false, ErrPodIdentityAmbiguous
	}
	if boundary == "" && !trusted {
		return nil, false, nil
	}
	pods, err := a.ownedPodInventory(ctx, job.Namespace, job.Name, string(job.UID))
	if err != nil {
		return nil, false, err
	}
	if trusted {
		proof, err := decodeRetainedProof(persisted, job.Namespace, job.Name, string(job.UID))
		if err != nil {
			return nil, false, err
		}
		if err = proofCoversInventory(proof, pods); err != nil {
			return nil, false, err
		}
		return persisted, true, nil
	}
	if len(pods) == 0 {
		return nil, false, ErrPodIdentityAmbiguous
	}
	proof := retainedProof{Source: "pods", Protocol: jobrun.PodHoldProtocol, HoldRelease: "pending",
		Namespace: job.Namespace, JobName: job.Name, JobUID: string(job.UID),
		ResourceVersion: job.ResourceVersion, StopBoundary: boundary, At: time.Now().UTC()}
	for _, pod := range pods {
		if !slices.Contains(pod.Finalizers, TerminationEvidenceFinalizer) || pod.ResourceVersion == "" || pod.Name == "" {
			return nil, false, ErrPodIdentityAmbiguous
		}
		fact, err := terminationFact(pod)
		if err != nil {
			return nil, false, err
		}
		proof.Pods = append(proof.Pods, fact)
	}
	data, err := json.Marshal(proof)
	return data, err == nil, err
}

func decodeRetainedProof(data []byte, namespace, name, uid string) (retainedProof, error) {
	var proof retainedProof
	if !jobrun.HasRetainedTerminationEvidence(data, uid) || json.Unmarshal(data, &proof) != nil ||
		proof.Namespace != namespace || proof.JobName != name {
		return proof, ErrPodIdentityAmbiguous
	}
	return proof, nil
}

func proofCoversInventory(proof retainedProof, pods []corev1.Pod) error {
	covered := make(map[string]string, len(proof.Pods))
	for _, pod := range proof.Pods {
		if pod.Name == "" || covered[pod.UID] != "" {
			return ErrPodIdentityAmbiguous
		}
		covered[pod.UID] = pod.Name
	}
	for _, pod := range pods {
		if covered[string(pod.UID)] != pod.Name {
			return ErrPodIdentityAmbiguous
		}
		if _, err := terminationFact(pod); err != nil {
			return err
		}
	}
	return nil
}

// ReleaseTerminationEvidence must receive the proof read back after its
// database commit. A vanished covered Pod is a converged prior release, while
// a new or active owned Pod invalidates this cleanup sample.
func (a Adapter) ReleaseTerminationEvidence(ctx context.Context, namespace, name, uid string, persisted []byte) error {
	proof, err := decodeRetainedProof(persisted, namespace, name, uid)
	if err != nil {
		return err
	}
	pods, err := a.ownedPodInventory(ctx, namespace, name, uid)
	if err != nil {
		return err
	}
	if err = proofCoversInventory(proof, pods); err != nil {
		return err
	}
	client, ok := a.Client.(interface {
		ReleasePodEvidence(context.Context, string, corev1.Pod) error
	})
	if !ok {
		return fmt.Errorf("guarded Pod evidence release client required")
	}
	for _, pod := range pods {
		if slices.Contains(pod.Finalizers, TerminationEvidenceFinalizer) {
			if err = client.ReleasePodEvidence(ctx, namespace, pod); err != nil {
				return err
			}
		}
	}
	return nil
}

// ReleasePodEvidence removes exactly our current finalizer entry. UID and
// resourceVersion tests prevent changing a replacement Pod or stale metadata.
func (c KubernetesJobClient) ReleasePodEvidence(ctx context.Context, namespace string, pod corev1.Pod) error {
	if c.Core == nil || namespace == "" || pod.Name == "" || pod.UID == "" || pod.ResourceVersion == "" {
		return fmt.Errorf("complete Pod identity required for evidence release")
	}
	index := slices.Index(pod.Finalizers, TerminationEvidenceFinalizer)
	if index < 0 {
		return nil
	}
	patch, err := json.Marshal([]map[string]any{
		{"op": "test", "path": "/metadata/uid", "value": string(pod.UID)},
		{"op": "test", "path": "/metadata/resourceVersion", "value": pod.ResourceVersion},
		{"op": "test", "path": fmt.Sprintf("/metadata/finalizers/%d", index), "value": TerminationEvidenceFinalizer},
		{"op": "remove", "path": fmt.Sprintf("/metadata/finalizers/%d", index)},
	})
	if err != nil {
		return err
	}
	_, err = c.Core.Pods(namespace).Patch(ctx, pod.Name, types.JSONPatchType, patch, metav1.PatchOptions{})
	return err
}
