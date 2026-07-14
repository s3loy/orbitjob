package main

import (
	"fmt"
	"strings"
)

func VerifyCorrectness(evidence Evidence) []CheckResult {
	var checks []CheckResult
	checks = append(checks, checkDuplicateTokens(evidence.Attempts))
	checks = append(checks, checkTenantLeak(evidence.TenantReads))
	checks = append(checks, checkDuplicateLedger(evidence.Ledger))
	checks = append(checks, checkTerminalRegression(evidence.Transitions))
	return checks
}

func EvaluateQualification(run RunRecord, evidence Evidence) Result {
	result := Result{SchemaVersion: "v020-result/v1", RunID: run.RunID, Qualification: run.Qualification}

	correctness := VerifyCorrectness(evidence)
	result.Checks = correctness

	for _, check := range correctness {
		if check.Status == VerdictFail {
			result.Verdict = VerdictFail
			result.Reason = fmt.Sprintf("correctness failure: %s", check.ID)
			return result
		}
	}

	if len(evidence.MissingSources) > 0 {
		result.Verdict = VerdictInconclusive
		result.Reason = fmt.Sprintf("missing evidence sources: %s", strings.Join(evidence.MissingSources, ", "))
		return result
	}

	if len(evidence.Interruptions) > 0 {
		result.Verdict = VerdictInconclusive
		result.Reason = "environmental interruption detected"
		return result
	}

	if !run.Qualification {
		result.Verdict = VerdictPass
		result.Reason = "non-standard run completed"
		return result
	}

	if evidence.Instances < 10000 {
		result.Verdict = VerdictFail
		result.Reason = fmt.Sprintf("instances %d < 10000", evidence.Instances)
		return result
	}

	result.Verdict = VerdictPass
	result.Reason = "all qualification gates passed"
	return result
}

type Evidence struct {
	Instances      int
	Attempts       []Attempt
	TenantReads    []TenantRead
	Ledger         []LedgerEntry
	Transitions    []Transition
	MissingSources []string
	Interruptions  []Interruption
}

type Attempt struct {
	InstanceID int64
	Token      string
	Valid      bool
}

type TenantRead struct {
	ActorTenant  string
	ObjectTenant string
	HTTPStatus   int
}

type LedgerEntry struct {
	IdempotencyKey string
	Operation      string
}

type Transition struct {
	InstanceID int64
	From       string
	To         string
}

type Interruption struct {
	Kind string
}

func checkDuplicateTokens(attempts []Attempt) CheckResult {
	seen := map[int64]string{}
	for _, attempt := range attempts {
		if !attempt.Valid {
			continue
		}
		if prev, ok := seen[attempt.InstanceID]; ok && prev != attempt.Token {
			return CheckResult{ID: "duplicate-valid-attempt-token", Status: VerdictFail, Message: fmt.Sprintf("instance %d has two valid tokens", attempt.InstanceID)}
		}
		seen[attempt.InstanceID] = attempt.Token
	}
	return CheckResult{ID: "duplicate-valid-attempt-token", Status: VerdictPass}
}

func checkTenantLeak(reads []TenantRead) CheckResult {
	for _, read := range reads {
		if read.ActorTenant != read.ObjectTenant && read.HTTPStatus == 200 {
			return CheckResult{ID: "tenant-leak", Status: VerdictFail, Message: fmt.Sprintf("%s read %s data", read.ActorTenant, read.ObjectTenant)}
		}
	}
	return CheckResult{ID: "tenant-leak", Status: VerdictPass}
}

func checkDuplicateLedger(entries []LedgerEntry) CheckResult {
	seen := map[string]bool{}
	for _, entry := range entries {
		if seen[entry.IdempotencyKey] {
			return CheckResult{ID: "duplicate-idempotent-side-effect", Status: VerdictFail, Message: fmt.Sprintf("duplicate ledger key %s", entry.IdempotencyKey)}
		}
		seen[entry.IdempotencyKey] = true
	}
	return CheckResult{ID: "duplicate-idempotent-side-effect", Status: VerdictPass}
}

func checkTerminalRegression(transitions []Transition) CheckResult {
	terminals := map[string]bool{"success": true, "failed": true, "canceled": true}
	for _, t := range transitions {
		if terminals[t.From] && t.To != t.From {
			return CheckResult{ID: "terminal-regression", Status: VerdictFail, Message: fmt.Sprintf("instance %d regressed from %s to %s", t.InstanceID, t.From, t.To)}
		}
	}
	return CheckResult{ID: "terminal-regression", Status: VerdictPass}
}
