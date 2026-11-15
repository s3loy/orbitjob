package controlplane

import (
	"encoding/json"
	"time"

	"orbitjob/internal/core/domain/jobrun"
)

// TaskSpanFromEvidence returns the actual task-container wall-clock span across
// all Pods of one attempt. It does not use reservation or reconciliation times.
// Caller-provided bounds are the Run creation and first durable terminal
// observation. A clock-inconsistent or incomplete source has no precise span.
func TaskSpanFromEvidence(data []byte, jobUID string, createdAt, observedAt time.Time) (time.Time, time.Time, bool) {
	if createdAt.IsZero() || observedAt.IsZero() || !jobrun.HasPodTerminationEvidence(data, jobUID) {
		return time.Time{}, time.Time{}, false
	}
	if jobrun.UsesPodEvidenceRetention(data) && !jobrun.HasRetainedTerminationEvidence(data, jobUID) {
		return time.Time{}, time.Time{}, false
	}
	var proof struct {
		Pods []struct {
			UID        string `json:"uid"`
			Containers []struct {
				Name  string `json:"name"`
				State struct {
					Terminated *struct {
						StartedAt  time.Time `json:"startedAt"`
						FinishedAt time.Time `json:"finishedAt"`
					} `json:"terminated"`
				} `json:"state"`
			} `json:"containers"`
		} `json:"pods"`
	}
	if json.Unmarshal(data, &proof) != nil {
		return time.Time{}, time.Time{}, false
	}
	var start, end time.Time
	seen := map[string]bool{}
	for _, pod := range proof.Pods {
		if seen[pod.UID] {
			return time.Time{}, time.Time{}, false
		}
		seen[pod.UID] = true
		found := false
		for _, container := range pod.Containers {
			if container.Name != "task" {
				continue
			}
			if found {
				return time.Time{}, time.Time{}, false
			}
			found = true
			exit := container.State.Terminated
			if exit == nil || exit.StartedAt.IsZero() || exit.FinishedAt.Before(exit.StartedAt) || exit.StartedAt.Before(createdAt) || exit.FinishedAt.After(observedAt) {
				return time.Time{}, time.Time{}, false
			}
			if start.IsZero() || exit.StartedAt.Before(start) {
				start = exit.StartedAt
			}
			if end.IsZero() || exit.FinishedAt.After(end) {
				end = exit.FinishedAt
			}
		}
		if !found {
			return time.Time{}, time.Time{}, false
		}
	}
	return start, end, !start.IsZero() && !end.Before(start)
}
