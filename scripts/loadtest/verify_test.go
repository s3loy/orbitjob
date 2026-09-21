package main

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func TestVerifyDetectsDuplicateSucceededAttempts(t *testing.T) {
	e := Evidence{Attempts: []RunAttempt{
		{RunID: 9, AttemptNumber: 1, Succeeded: true},
		{RunID: 9, AttemptNumber: 2, Succeeded: true},
	}}
	for _, check := range VerifyCorrectness(e) {
		if check.ID == "duplicate-succeeded-attempt" && check.Status != VerdictFail {
			t.Fatalf("status = %s", check.Status)
		}
	}
}

func TestVerifyAllowsOneSucceededAttemptPerRun(t *testing.T) {
	e := Evidence{Attempts: []RunAttempt{
		{RunID: 9, AttemptNumber: 1, Succeeded: false},
		{RunID: 9, AttemptNumber: 2, Succeeded: true},
	}}
	for _, check := range VerifyCorrectness(e) {
		if check.ID == "duplicate-succeeded-attempt" && check.Status != VerdictPass {
			t.Fatalf("a retry that ended in one success failed the check: %s", check.Message)
		}
	}
}

func TestVerifyDetectsTenantLeak(t *testing.T) {
	e := Evidence{TenantReads: []TenantRead{
		{ActorTenant: "20000000000000000000000001", ObjectTenant: "20000000000000000000000002", HTTPStatus: 200},
	}}
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
	e := Evidence{Regressions: []Regression{{ResourceID: "run-1", From: "Succeeded", To: "Running"}}}
	for _, check := range VerifyCorrectness(e) {
		if check.ID == "terminal-regression" && check.Status != VerdictFail {
			t.Fatalf("status = %s, want FAIL", check.Status)
		}
	}
}

func TestVerifyDetectsTerminalMismatch(t *testing.T) {
	e := Evidence{TerminalMismatches: []TerminalMismatch{{CaseID: "c-1", RevisionID: 7, Expected: "Succeeded", Actual: "Failed", Count: 2}}}
	for _, check := range VerifyCorrectness(e) {
		if check.ID == "expected-terminal-state" && check.Status != VerdictFail {
			t.Fatalf("status = %s, want FAIL", check.Status)
		}
	}
}

// A terminal ledger run whose JobRun object is gone is a finding: generated
// definitions raise retention so nothing prunes mid-run, and the canceled end
// state is defined as the row, the phase, and the CR together.
func TestVerifyDetectsMissingJobRunCRs(t *testing.T) {
	e := Evidence{MissingCRs: 2, MissingCRSamples: []string{"case-1-abcd1234"}}
	for _, check := range VerifyCorrectness(e) {
		if check.ID == "jobrun-cr-present" && check.Status != VerdictFail {
			t.Fatalf("status = %s, want FAIL", check.Status)
		}
	}
	if got := checkJobRunCRsPresent(0, nil); got.Status != VerdictPass {
		t.Fatalf("no missing CRs: status = %s, want PASS", got.Status)
	}
}

func TestMissingEvidenceIsInconclusive(t *testing.T) {
	result := EvaluateQualification(RunRecord{}, Evidence{MissingSources: []string{"postgres"}}, 10000)
	if result.Verdict != VerdictInconclusive {
		t.Fatalf("verdict = %s, want INCONCLUSIVE", result.Verdict)
	}
}

func TestTruncatedRunIsInconclusive(t *testing.T) {
	result := EvaluateQualification(RunRecord{Qualification: true}, Evidence{Truncated: true, QualifiedRuns: 20000}, 10000)
	if result.Verdict != VerdictInconclusive {
		t.Fatalf("verdict = %s, want INCONCLUSIVE", result.Verdict)
	}
}

func TestInFlightRunsAreInconclusive(t *testing.T) {
	result := EvaluateQualification(RunRecord{Qualification: true}, Evidence{InFlight: 3, QualifiedRuns: 20000}, 10000)
	if result.Verdict != VerdictInconclusive {
		t.Fatalf("verdict = %s, want INCONCLUSIVE", result.Verdict)
	}
}

func TestMissingDefinitionsAreInconclusive(t *testing.T) {
	result := EvaluateQualification(RunRecord{Qualification: true}, Evidence{MissingDefinitions: 5, QualifiedRuns: 20000}, 10000)
	if result.Verdict != VerdictInconclusive {
		t.Fatalf("verdict = %s, want INCONCLUSIVE", result.Verdict)
	}
}

func TestPassRequiresMinimumQualifiedRuns(t *testing.T) {
	// Raw run count no longer feeds the gate: 20000 raw rows with only
	// 5000 in the expected terminal phase must fail.
	result := EvaluateQualification(RunRecord{Qualification: true}, Evidence{Runs: 20000, QualifiedRuns: 5000}, 10000)
	if result.Verdict != VerdictFail {
		t.Fatalf("verdict = %s, want FAIL", result.Verdict)
	}
}

func TestFullPass(t *testing.T) {
	result := EvaluateQualification(RunRecord{Qualification: true}, Evidence{Runs: 11234, QualifiedRuns: 11234}, 10000)
	if result.Verdict != VerdictPass {
		t.Fatalf("verdict = %s, want PASS", result.Verdict)
	}
}

func TestPassUsesConfigurableMinimum(t *testing.T) {
	result := EvaluateQualification(RunRecord{Qualification: true}, Evidence{Runs: 8000, QualifiedRuns: 8000}, 8000)
	if result.Verdict != VerdictPass {
		t.Fatalf("verdict = %s, want PASS", result.Verdict)
	}
}

func TestNonQualificationPass(t *testing.T) {
	result := EvaluateQualification(RunRecord{Qualification: false}, Evidence{Runs: 9000, QualifiedRuns: 9000}, 0)
	if result.Verdict != VerdictPass {
		t.Fatalf("verdict = %s, want PASS", result.Verdict)
	}
}

// A run records what it was. Verifying it with a different config has to fail:
// silently taking the config's profile is how a smoke run gets written down as
// a standard qualification run, and nothing downstream can tell.
func TestResolveRunIdentityRejectsAMismatchedConfig(t *testing.T) {
	stats := RunStats{Profile: "smoke", Seed: "smoke-seed", Qualification: false}
	_, _, _, err := resolveRunIdentity(stats, Config{Profile: "standard", Seed: "standard-seed", Qualification: true}, "run-1")
	if err == nil {
		t.Fatal("a mismatched config was accepted")
	}
}

func TestResolveRunIdentityUsesTheRunsOwnRecord(t *testing.T) {
	stats := RunStats{Profile: "smoke", Seed: "smoke-seed", Qualification: false}
	profile, seed, qualification, err := resolveRunIdentity(stats, Config{Profile: "smoke", Seed: "other-seed"}, "run-1")
	if err != nil {
		t.Fatalf("matching profile rejected: %v", err)
	}
	if profile != "smoke" || seed != "smoke-seed" || qualification {
		t.Fatalf("got profile=%q seed=%q qualification=%v", profile, seed, qualification)
	}
}

// Runs recorded before the profile was stored carry none, and falling back to
// the passed config is the only option left.
func TestResolveRunIdentityFallsBackForOlderRuns(t *testing.T) {
	profile, seed, qualification, err := resolveRunIdentity(RunStats{}, Config{
		Profile: "standard", Seed: "standard-seed", Qualification: true,
	}, "run-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile != "standard" || seed != "standard-seed" || !qualification {
		t.Fatalf("got profile=%q seed=%q qualification=%v", profile, seed, qualification)
	}
}

// A smoke profile cannot reach all 1200 definitions in 30 minutes: it emits
// about 500 events. Judging it by whether it touched every definition marked
// every smoke run inconclusive, which reads as a failure and is not one.
func TestMissingDefinitionsDoNotBlockANonQualifyingRun(t *testing.T) {
	result := EvaluateQualification(RunRecord{Qualification: false}, Evidence{MissingDefinitions: 590, QualifiedRuns: 680}, 0)
	if result.Verdict != VerdictPass {
		t.Fatalf("verdict = %s, want PASS", result.Verdict)
	}
	if !strings.Contains(result.Reason, "590") {
		t.Fatalf("coverage is not reported: %s", result.Reason)
	}
}

// scenarioToLedgerPhase is the contract between the scenario vocabulary and
// the ledger phases. An unmapped state must be refused by CollectEvidence,
// never silently counted as a success.
func TestScenarioToLedgerPhaseCoversTheScenarioVocabulary(t *testing.T) {
	for _, scenario := range []string{"success", "failed", "canceled"} {
		if scenarioToLedgerPhase[scenario] == "" {
			t.Fatalf("scenario state %q has no ledger phase", scenario)
		}
	}
	for scenario, phase := range scenarioToLedgerPhase {
		if !terminalPhases[phase] {
			t.Fatalf("scenario %q maps to non-terminal phase %q", scenario, phase)
		}
	}
}

// jobRunCRName mirrors the operator's naming rule: scheduled job name plus the
// first eight characters of the occurrence key.
func TestJobRunCRNameMirrorsTheOperatorRule(t *testing.T) {
	got := jobRunCRName("failure-cancel-while-running-0001", "0123456789abcdef")
	if got != "failure-cancel-while-running-0001-01234567" {
		t.Fatalf("CR name = %q", got)
	}
	if jobRunCRName("case-1", "") != "case-1-unknown" {
		t.Fatalf("empty occurrence key has no fallback applied: %q", jobRunCRName("case-1", ""))
	}
}

// A tenant query that does not name a well-formed ULID is refused before
// anything is sent: the RLS guard has no default value to fall back to.
func TestWithTenantTxRefusesNonUlidTenant(t *testing.T) {
	err := withTenantTx(context.Background(), nil, "load-alpha", func(tx *sql.Tx) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "ULID") {
		t.Fatalf("error = %v, want a ULID-shape refusal", err)
	}
}
