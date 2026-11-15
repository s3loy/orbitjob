package jobrun

import (
	"fmt"
	"testing"

	"orbitjob/internal/platform/scan"
)

func TestOOMClassificationRequiresTaskReasonAndTrustedIdentity(t *testing.T) {
	uid := scan.GenerateID()
	for _, c := range []struct {
		name, reason, observedUID string
		want                      bool
	}{
		{"task", "OOMKilled", uid, true},
		{"task", "Error", uid, false},
		{"debugger", "OOMKilled", uid, false},
		{"task", "OOMKilled", scan.GenerateID(), false},
	} {
		proof := []byte(fmt.Sprintf(`{"source":"pods","job_uid":%q,"pods":[{"uid":%q,"containers":[{"name":%q,"state":{"terminated":{"finishedAt":"2026-11-12T00:00:00Z","reason":%q,"exitCode":137}}}]}]}`, uid, scan.GenerateID(), c.name, c.reason))
		if got := HasOOMTerminationEvidence(proof, c.observedUID); got != c.want {
			t.Fatalf("name=%s reason=%s OOM=%t want=%t", c.name, c.reason, got, c.want)
		}
	}
}
