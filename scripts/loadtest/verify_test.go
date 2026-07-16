package main

import "testing"

func TestVerifyDetectsDuplicateValidTokens(t *testing.T) {
	e := Evidence{Attempts: []Attempt{
		{InstanceID: 9, Token: "a", Valid: true},
		{InstanceID: 9, Token: "b", Valid: true},
	}}
	for _, check := range VerifyCorrectness(e) {
		if check.ID == "duplicate-valid-attempt-token" && check.Status != VerdictFail {
			t.Fatalf("status = %s", check.Status)
		}
	}
}

func TestVerifyDetectsTenantLeak(t *testing.T) {
	e := Evidence{TenantReads: []TenantRead{{ActorTenant: "load-alpha", ObjectTenant: "load-beta", HTTPStatus: 200}}}
	for _, check := range VerifyCorrectness(e) {
		if check.ID == "tenant-leak" && check.Status != VerdictFail {
			t.Fatalf("status = %s", check.Status)
		}
	}
}

func TestVerifyDetectsDuplicateLedger(t *testing.T) {
	e := Evidence{Ledger: []LedgerEntry{
		{IdempotencyKey: "key-1", Operation: "upsert"},
		{IdempotencyKey: "key-1", Operation: "upsert"},
	}}
	for _, check := range VerifyCorrectness(e) {
		if check.ID == "duplicate-idempotent-side-effect" && check.Status != VerdictFail {
			t.Fatalf("status = %s", check.Status)
		}
	}
}

func TestCorrectnessFailureWinsOverInterruption(t *testing.T) {
	result := EvaluateQualification(RunRecord{}, Evidence{
		Interruptions: []Interruption{{Kind: "host-suspend"}},
		TenantReads:   []TenantRead{{ActorTenant: "a", ObjectTenant: "b", HTTPStatus: 200}},
	}, 10000)
	if result.Verdict != VerdictFail {
		t.Fatalf("verdict = %s, want FAIL", result.Verdict)
	}
}

func TestMissingEvidenceIsInconclusive(t *testing.T) {
	result := EvaluateQualification(RunRecord{}, Evidence{MissingSources: []string{"postgres"}}, 10000)
	if result.Verdict != VerdictInconclusive {
		t.Fatalf("verdict = %s, want INCONCLUSIVE", result.Verdict)
	}
}

func TestPassRequiresMinimumInstances(t *testing.T) {
	result := EvaluateQualification(RunRecord{Qualification: true}, Evidence{Instances: 5000}, 10000)
	if result.Verdict != VerdictFail {
		t.Fatalf("verdict = %s, want FAIL", result.Verdict)
	}
}

func TestFullPass(t *testing.T) {
	result := EvaluateQualification(RunRecord{Qualification: true}, Evidence{Instances: 11234}, 10000)
	if result.Verdict != VerdictPass {
		t.Fatalf("verdict = %s, want PASS", result.Verdict)
	}
}

func TestPassUsesConfigurableMinimum(t *testing.T) {
	result := EvaluateQualification(RunRecord{Qualification: true}, Evidence{Instances: 8000}, 8000)
	if result.Verdict != VerdictPass {
		t.Fatalf("verdict = %s, want PASS", result.Verdict)
	}
}
