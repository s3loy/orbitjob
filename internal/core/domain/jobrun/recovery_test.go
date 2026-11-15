package jobrun

import (
	"strings"
	"testing"
)

func TestPodTerminationProofRejectsHistoricalOrIncompleteFacts(t *testing.T) {
	valid := `{"source":"pods","job_uid":"job","pods":[{"uid":"pod","containers":[{"name":"task","state":{"terminated":{"finishedAt":"2026-10-29T00:00:00Z","reason":"Completed"}}}]}]}`
	for _, test := range []struct {
		name, proof, uid string
		want             bool
	}{
		{"trusted-exit", valid, "job", true},
		{"foreign-job", valid, "other", false},
		{"missing-job", valid, "", false},
		{"job-phase-only", `{"source":"job","job_uid":"job"}`, "job", false},
		{"historical-empty-pods", `{"source":"pods","job_uid":"job","pods":[]}`, "job", false},
		{"missing-pod", strings.Replace(valid, `"uid":"pod"`, `"uid":""`, 1), "job", false},
		{"missing-container", strings.Replace(valid, `"name":"task"`, `"name":""`, 1), "job", false},
		{"running-container", strings.Replace(valid, `"terminated"`, `"running"`, 1), "job", false},
		{"no-runtime-finish", strings.Replace(valid, `"finishedAt"`, `"startedAt"`, 1), "job", false},
		{"synthetic-exit", strings.Replace(valid, `"Completed"`, `"ContainerStatusUnknown"`, 1), "job", false},
		{"empty-container-list", `{"source":"pods","job_uid":"job","pods":[{"uid":"pod","containers":[]}]}`, "job", false},
		{"invalid-json", `{`, "job", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := HasPodTerminationEvidence([]byte(test.proof), test.uid); got != test.want {
				t.Fatalf("trusted=%t want=%t", got, test.want)
			}
		})
	}
}
