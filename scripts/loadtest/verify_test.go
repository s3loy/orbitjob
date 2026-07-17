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

func TestVerifyDetectsTerminalRegression(t *testing.T) {
	e := Evidence{Regressions: []Regression{{ResourceID: "run-1", From: "success", To: "running"}}}
	for _, check := range VerifyCorrectness(e) {
		if check.ID == "terminal-regression" && check.Status != VerdictFail {
			t.Fatalf("status = %s, want FAIL", check.Status)
		}
	}
}

func TestVerifyDetectsTerminalMismatch(t *testing.T) {
	e := Evidence{TerminalMismatches: []TerminalMismatch{{CaseID: "c-1", JobID: 7, Expected: "success", Actual: "failed", Count: 2}}}
	for _, check := range VerifyCorrectness(e) {
		if check.ID == "expected-terminal-state" && check.Status != VerdictFail {
			t.Fatalf("status = %s, want FAIL", check.Status)
		}
	}
}

func TestMissingEvidenceIsInconclusive(t *testing.T) {
	result := EvaluateQualification(RunRecord{}, Evidence{MissingSources: []string{"postgres"}}, 10000)
	if result.Verdict != VerdictInconclusive {
		t.Fatalf("verdict = %s, want INCONCLUSIVE", result.Verdict)
	}
}

func TestTruncatedRunIsInconclusive(t *testing.T) {
	result := EvaluateQualification(RunRecord{Qualification: true}, Evidence{Truncated: true, QualifiedInstances: 20000}, 10000)
	if result.Verdict != VerdictInconclusive {
		t.Fatalf("verdict = %s, want INCONCLUSIVE", result.Verdict)
	}
}

func TestInFlightInstancesAreInconclusive(t *testing.T) {
	result := EvaluateQualification(RunRecord{Qualification: true}, Evidence{InFlight: 3, QualifiedInstances: 20000}, 10000)
	if result.Verdict != VerdictInconclusive {
		t.Fatalf("verdict = %s, want INCONCLUSIVE", result.Verdict)
	}
}

func TestMissingDefinitionsAreInconclusive(t *testing.T) {
	result := EvaluateQualification(RunRecord{Qualification: true}, Evidence{MissingDefinitions: 5, QualifiedInstances: 20000}, 10000)
	if result.Verdict != VerdictInconclusive {
		t.Fatalf("verdict = %s, want INCONCLUSIVE", result.Verdict)
	}
}

func TestPassRequiresMinimumQualifiedInstances(t *testing.T) {
	// Raw instance count no longer feeds the gate: 20000 raw rows with only
	// 5000 in the expected terminal state must fail.
	result := EvaluateQualification(RunRecord{Qualification: true}, Evidence{Instances: 20000, QualifiedInstances: 5000}, 10000)
	if result.Verdict != VerdictFail {
		t.Fatalf("verdict = %s, want FAIL", result.Verdict)
	}
}

func TestFullPass(t *testing.T) {
	result := EvaluateQualification(RunRecord{Qualification: true}, Evidence{Instances: 11234, QualifiedInstances: 11234}, 10000)
	if result.Verdict != VerdictPass {
		t.Fatalf("verdict = %s, want PASS", result.Verdict)
	}
}

func TestPassUsesConfigurableMinimum(t *testing.T) {
	result := EvaluateQualification(RunRecord{Qualification: true}, Evidence{Instances: 8000, QualifiedInstances: 8000}, 8000)
	if result.Verdict != VerdictPass {
		t.Fatalf("verdict = %s, want PASS", result.Verdict)
	}
}

func TestNonQualificationPass(t *testing.T) {
	result := EvaluateQualification(RunRecord{Qualification: false}, Evidence{Instances: 9000, QualifiedInstances: 9000}, 0)
	if result.Verdict != VerdictPass {
		t.Fatalf("verdict = %s, want PASS", result.Verdict)
	}
}
