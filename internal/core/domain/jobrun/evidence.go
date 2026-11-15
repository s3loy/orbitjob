package jobrun

import (
	"encoding/json"
	"time"
)

const PodHoldProtocol = "pod-hold-v1"

// RecordedAttempt keeps a target attempt separate from the run's current one.
// Late cleanup must not accidentally advance a newer attempt or its outcome.
type RecordedAttempt struct {
	Execution           Execution
	ID                  int64
	Number              int
	Phase               Phase
	JobName             string
	JobUID              string
	OwnerRunUID         string
	UnknownSince        *time.Time
	UnknownReason       string
	TerminationEvidence []byte
}

type retainedEvidence struct {
	Protocol     string `json:"protocol"`
	Release      string `json:"hold_release"`
	Revoked      bool   `json:"revoked"`
	Namespace    string `json:"namespace"`
	JobName      string `json:"job_name"`
	StopBoundary string `json:"stop_boundary"`
	Pods         []struct {
		UID     string `json:"uid"`
		Name    string `json:"name"`
		Version string `json:"resource_version"`
	} `json:"pods"`
}

// HasRetainedTerminationEvidence validates the persisted protocol envelope.
// The trusted adapter separately verifies live ownership and all containers.
func HasRetainedTerminationEvidence(data []byte, uid string) bool {
	var proof retainedEvidence
	if json.Unmarshal(data, &proof) != nil || proof.Protocol != PodHoldProtocol || proof.Revoked ||
		(proof.Release != "pending" && proof.Release != "done") || proof.Namespace == "" || proof.JobName == "" ||
		!HasPodTerminationEvidence(data, uid) {
		return false
	}
	if !controllerStopAcknowledged(proof.StopBoundary) {
		return false
	}
	seen := make(map[string]bool, len(proof.Pods))
	for _, pod := range proof.Pods {
		if pod.Name == "" || pod.Version == "" || seen[pod.UID] {
			return false
		}
		seen[pod.UID] = true
	}
	return true
}

// UsesPodEvidenceRetention recognizes the protocol even when an older or
// revoked checkpoint cannot authorize proof, release or cancellation.
func UsesPodEvidenceRetention(data []byte) bool {
	var proof struct {
		Protocol string `json:"protocol"`
	}
	return json.Unmarshal(data, &proof) == nil && proof.Protocol == PodHoldProtocol
}

func controllerStopAcknowledged(boundary string) bool {
	switch boundary {
	case "failure_target", "success_criteria_met", "failed", "complete":
		return true
	default:
		return false
	}
}

// EvidenceReleasePending includes older deletion-only "done" checkpoints.
// Their unacknowledged inventory cannot discharge the retention obligation.
func EvidenceReleasePending(data []byte) bool {
	var proof retainedEvidence
	return json.Unmarshal(data, &proof) == nil && proof.Protocol == PodHoldProtocol &&
		(proof.Release != "done" || proof.Revoked || !controllerStopAcknowledged(proof.StopBoundary))
}

// EvidenceReleaseComplete validates the release checkpoint envelope. Callers
// consuming stop evidence also require HasRetainedTerminationEvidence.
func EvidenceReleaseComplete(data []byte) bool {
	var proof retainedEvidence
	return json.Unmarshal(data, &proof) == nil && proof.Protocol == PodHoldProtocol &&
		proof.Release == "done" && !proof.Revoked && controllerStopAcknowledged(proof.StopBoundary)
}
