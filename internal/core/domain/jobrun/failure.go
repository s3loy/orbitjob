package jobrun

import "encoding/json"

// HasOOMTerminationEvidence classifies the task container only after the
// complete identity-bound termination proof passed the existing validator.
// Exit code 137 alone is not evidence of an OOM kill.
func HasOOMTerminationEvidence(data []byte, uid string) bool {
	if !HasPodTerminationEvidence(data, uid) {
		return false
	}
	var proof struct {
		Pods []struct {
			Containers []struct {
				Name  string `json:"name"`
				State struct {
					Terminated *struct {
						Reason string `json:"reason"`
					} `json:"terminated"`
				} `json:"state"`
			} `json:"containers"`
		} `json:"pods"`
	}
	if json.Unmarshal(data, &proof) != nil {
		return false
	}
	for _, pod := range proof.Pods {
		for _, container := range pod.Containers {
			if container.Name == "task" && container.State.Terminated != nil && container.State.Terminated.Reason == "OOMKilled" {
				return true
			}
		}
	}
	return false
}
