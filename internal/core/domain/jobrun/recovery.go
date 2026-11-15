package jobrun

import (
	"encoding/json"
	"errors"
	"time"
)

var ErrAdmissionBlocked = errors.New("source admission blocked")

// AdmissionBlockedError distinguishes a waiting request from an existing run.
type AdmissionBlockedError struct {
	Policy        string
	Reason        string
	CancelTargets []OpenRun
}

func (e *AdmissionBlockedError) Error() string        { return "source admission blocked: " + e.Reason }
func (e *AdmissionBlockedError) Is(target error) bool { return target == ErrAdmissionBlocked }

const (
	ReasonJobNotFound          = "JobNotFound"
	ReasonJobUIDMismatch       = "JobUIDMismatch"
	ReasonPodIdentityAmbiguous = "PodIdentityAmbiguous"
)

// Execution joins the current attempt to its pinned source, independent of CRs.
type Execution struct {
	Run                                                                    StoredRun
	DispatchStage                                                          DispatchStage
	SourceUID, OccurrenceKey, Namespace, DefinitionName, SourceMode, Actor string
	Trigger                                                                Trigger
	// PhaseUpdatedAt is the persisted phase clock, never a reconciliation clock.
	PhaseUpdatedAt time.Time
}

// HasPodTerminationEvidence recognizes the shape produced by the trusted Pod
// observer. Job phase facts and historical empty placeholders cannot stop a run.
func HasPodTerminationEvidence(data []byte, uid string) bool {
	var proof struct {
		Source       string `json:"source"`
		Protocol     string `json:"protocol"`
		StopBoundary string `json:"stop_boundary"`
		Revoked      bool   `json:"revoked"`
		JobUID       string `json:"job_uid"`
		Pods         []struct {
			UID        string `json:"uid"`
			Containers []struct {
				Name  string `json:"name"`
				State struct {
					Terminated *struct {
						FinishedAt time.Time `json:"finishedAt"`
						Reason     string    `json:"reason"`
					} `json:"terminated"`
				} `json:"state"`
			} `json:"containers"`
		} `json:"pods"`
	}
	if uid == "" || json.Unmarshal(data, &proof) != nil || proof.Source != "pods" || proof.Revoked || proof.JobUID != uid || len(proof.Pods) == 0 {
		return false
	}
	if proof.Protocol != "" && (proof.Protocol != PodHoldProtocol || !controllerStopAcknowledged(proof.StopBoundary)) {
		return false
	}
	for _, pod := range proof.Pods {
		if pod.UID == "" || len(pod.Containers) == 0 {
			return false
		}
		for _, container := range pod.Containers {
			exit := container.State.Terminated
			if container.Name == "" || exit == nil || exit.FinishedAt.IsZero() || exit.Reason == "ContainerStatusUnknown" {
				return false
			}
		}
	}
	return true
}
