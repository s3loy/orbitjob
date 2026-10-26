package logarchive

import (
	"fmt"
	api "orbitjob/api/kubernetes/workloads/v1alpha1"
)

// Record only states actually observed in the CR or authoritative ledger.
// An unobserved intermediate transition is not reconstructed as an event.
func runEvents(id Identity, run *api.JobRun, phase string) []Record {
	rows := []Record{{ID: "publication:" + id.RunUID, Kind: "event", Stage: "accepted", Time: id.CreatedAt, Text: "Execution request published; platform admission and execution are observed separately."}}
	if id.RunID > 0 && phase != "" {
		stage := "preparing"
		if phase == "Running" {
			stage = "running"
		}
		if phase == "Succeeded" || phase == "Failed" || phase == "Canceled" || phase == "Rejected" {
			stage = "completed"
		}
		rows = append(rows, Record{ID: eventID(id.RunUID, "ledger", phase, fmt.Sprint(run.Status.Attempt)), Kind: "event", Stage: stage, Attempt: int(run.Status.Attempt), Text: "Execution admitted in ledger; observed phase: " + phase})
	}
	for _, c := range run.Status.Conditions {
		row := Record{ID: eventID(id.RunUID, c.Type, c.Status, c.Reason, c.Message), Kind: "event", Stage: "preparing", Attempt: int(run.Status.Attempt), Text: fmt.Sprintf("JobRun %s=%s: %s %s", c.Type, c.Status, c.Reason, c.Message)}
		if c.LastTransitionTime != nil {
			row.Time = c.LastTransitionTime.Time
		}
		rows = append(rows, row)
	}
	return rows
}
