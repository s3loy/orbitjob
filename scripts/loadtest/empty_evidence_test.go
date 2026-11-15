package main

import "testing"

func completeVerificationEvidence() Evidence {
	return Evidence{
		Runs: 1, QualifiedRuns: 1,
		Attempts:               []RunAttempt{{RunID: 1, AttemptNumber: 1, Succeeded: true}},
		TenantReads:            []TenantRead{{ActorTenant: "actor", ObjectTenant: "other", HTTPStatus: 403}},
		Ledger:                 []LedgerEntry{{IdempotencyKey: "observed", Operation: "delivery"}},
		TransitionObservations: 1,
	}
}

func TestEmptySourceIsNotAPass(t *testing.T) {
	for _, tc := range []struct {
		name   string
		remove func(*Evidence)
	}{
		{"attempts", func(e *Evidence) { e.Attempts = nil }},
		{"tenant reads", func(e *Evidence) { e.TenantReads = nil }},
		{"side effects", func(e *Evidence) { e.Ledger = nil }},
		{"transitions", func(e *Evidence) { e.TransitionObservations = 0 }},
		{"runs", func(e *Evidence) { e.Runs = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := completeVerificationEvidence()
			tc.remove(&e)
			for _, qualifying := range []bool{false, true} {
				result := EvaluateQualification(RunRecord{Qualification: qualifying}, e, 1)
				if result.Verdict != VerdictInconclusive {
					t.Fatalf("empty source verdict=%s qualification=%v", result.Verdict, qualifying)
				}
			}
		})
	}
	for _, check := range VerifyCorrectness(Evidence{}) {
		if check.Status == VerdictPass {
			t.Fatalf("empty source printed PASS for %s", check.ID)
		}
	}
}
