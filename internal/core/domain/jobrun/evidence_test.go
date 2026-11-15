package jobrun

import (
	"encoding/json"
	"testing"
)

const retainedProofFixture = `{"source":"pods","protocol":"pod-hold-v1","hold_release":"pending","namespace":"jobs","job_name":"job","job_uid":"uid","stop_boundary":"failure_target","pods":[{"uid":"pod-uid","name":"pod","resource_version":"20","containers":[{"name":"task","state":{"terminated":{"finishedAt":"2026-10-29T00:00:00Z"}}}]}]}`

func TestRetainedEvidenceRequiresCompleteIdentityAndPreservesRevokedObligation(t *testing.T) {
	for _, test := range []struct {
		name                     string
		change                   func(map[string]any)
		proof, pending, complete bool
	}{
		{"pending", func(map[string]any) {}, true, true, false},
		{"done", func(p map[string]any) { p["hold_release"] = "done" }, true, false, true},
		{"revoked", func(p map[string]any) { p["revoked"] = true }, false, true, false},
		{"revoked-done", func(p map[string]any) { p["revoked"], p["hold_release"] = true, "done" }, false, true, false},
		{"lost-release", func(p map[string]any) { delete(p, "hold_release") }, false, true, false},
		{"legacy", func(p map[string]any) { delete(p, "protocol") }, false, false, false},
		{"missing-namespace", func(p map[string]any) { delete(p, "namespace") }, false, true, false},
		{"unstopped", func(p map[string]any) { p["stop_boundary"] = "running" }, false, true, false},
		{"deleting-pending", func(p map[string]any) { p["stop_boundary"] = "deleting" }, false, true, false},
		{"deleting-done", func(p map[string]any) { p["stop_boundary"], p["hold_release"] = "deleting", "done" }, false, true, false},
		{"duplicate-pod", func(p map[string]any) { p["pods"] = append(p["pods"].([]any), p["pods"].([]any)[0]) }, false, true, false},
		{"missing-pod-name", func(p map[string]any) { delete(p["pods"].([]any)[0].(map[string]any), "name") }, false, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var proof map[string]any
			if err := json.Unmarshal([]byte(retainedProofFixture), &proof); err != nil {
				t.Fatal(err)
			}
			test.change(proof)
			data, err := json.Marshal(proof)
			if err != nil {
				t.Fatal(err)
			}
			if got := HasRetainedTerminationEvidence(data, "uid"); got != test.proof {
				t.Fatalf("proof=%t want=%t", got, test.proof)
			}
			if got := EvidenceReleasePending(data); got != test.pending {
				t.Fatalf("pending=%t want=%t", got, test.pending)
			}
			if got := EvidenceReleaseComplete(data); got != test.complete {
				t.Fatalf("complete=%t want=%t", got, test.complete)
			}
		})
	}
}

func TestRetainedProofCannotFallBackToLegacyPodValidation(t *testing.T) {
	for _, boundary := range []string{"deleting", "", "running", "failure_target", "success_criteria_met", "failed", "complete"} {
		for _, release := range []string{"pending", "done"} {
			t.Run(boundary+"/"+release, func(t *testing.T) {
				var proof map[string]any
				if err := json.Unmarshal([]byte(retainedProofFixture), &proof); err != nil {
					t.Fatal(err)
				}
				proof["stop_boundary"], proof["hold_release"] = boundary, release
				data, err := json.Marshal(proof)
				if err != nil {
					t.Fatal(err)
				}
				if !UsesPodEvidenceRetention(data) {
					t.Fatal("untrusted checkpoint lost its protocol identity")
				}
				trusted := boundary != "deleting" && boundary != "" && boundary != "running"
				if got := HasPodTerminationEvidence(data, "uid"); got != trusted {
					t.Fatalf("generic proof=%t want=%t", got, trusted)
				}
				delete(proof, "protocol")
				legacy, err := json.Marshal(proof)
				if err != nil {
					t.Fatal(err)
				}
				if UsesPodEvidenceRetention(legacy) || !HasPodTerminationEvidence(legacy, "uid") {
					t.Fatal("legacy no-protocol behavior changed")
				}
			})
		}
	}
}
