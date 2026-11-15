package main

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// AcceptedTrigger is a receipt from the API, independent of the later ledger
// projection. Counting only projected rows cannot detect an accepted run lost
// before projection, especially when cron rows keep the aggregate nonempty.
type AcceptedTrigger struct {
	Tenant        string `json:"tenant"`
	RevisionID    int64  `json:"revision_id"`
	OccurrenceKey string `json:"occurrence_key"`
}

func configSHA256(cfg Config) string {
	return fmt.Sprintf("%x", sha256.Sum256(mustJSON(cfg)))
}

func validateVerificationArtifacts(created []CreatedDefinition, definitions []Definition, keys, namespaces map[string]string) error {
	if len(created) == 0 || len(created) != len(definitions) {
		return fmt.Errorf("created definitions must cover the generated corpus exactly")
	}
	byCase := make(map[string]Definition, len(definitions))
	for _, def := range definitions {
		if def.CaseID == "" || byCase[def.CaseID].CaseID != "" {
			return fmt.Errorf("generated corpus has empty or duplicate case identities")
		}
		byCase[def.CaseID] = def
	}
	seen := make(map[int64]bool)
	for _, cd := range created {
		def, ok := byCase[cd.CaseID]
		if !ok || cd.RevisionID <= 0 || seen[cd.RevisionID] || cd.Tenant != def.Tenant || cd.ExpectedTerminalState != def.Expected.TerminalState {
			return fmt.Errorf("created definition does not uniquely match its generated case")
		}
		if err := validateTenantID(cd.Tenant); err != nil {
			return fmt.Errorf("created definition has an invalid tenant")
		}
		if strings.TrimSpace(keys[cd.Tenant]) == "" || namespaces[cd.Tenant] != TenantNamespace(cd.Tenant) {
			return fmt.Errorf("created definition lacks its tenant key or namespace evidence")
		}
		seen[cd.RevisionID] = true
		delete(byCase, cd.CaseID)
	}
	return nil
}

func executionEvidenceChecks(stats RunStats, evidence Evidence) []CheckResult {
	check := CheckResult{ID: "manual-trigger-accounting", Status: VerdictInconclusive}
	finish := func(message string) []CheckResult {
		check.Message = message
		return []CheckResult{check}
	}
	if stats.ConfigSHA256 == "" || stats.Commit == "" {
		return finish("execution-time config/source identity missing; historical artifacts cannot establish a fresh pass")
	}
	if stats.ScheduledEvents < 0 || stats.Triggered < 0 || stats.Accepted < 0 || stats.Rejected < 0 || stats.Skipped < 0 ||
		stats.Triggered != stats.Accepted+stats.Rejected || int64(stats.ScheduledEvents) != stats.Triggered+stats.Skipped {
		return finish("scheduled/triggered/accepted/rejected/skipped counters do not reconcile")
	}
	if stats.Rejected > 0 || stats.Skipped > 0 {
		return finish(fmt.Sprintf("offered workload incomplete: %d triggers rejected, %d skipped", stats.Rejected, stats.Skipped))
	}
	if int64(len(stats.AcceptedTriggers)) != stats.Accepted {
		return finish("accepted count does not match the independent trigger receipts")
	}
	observed := make(map[AcceptedTrigger]int, len(evidence.ManualRuns))
	for _, run := range evidence.ManualRuns {
		observed[run]++
	}
	seen := make(map[AcceptedTrigger]bool, len(stats.AcceptedTriggers))
	for _, receipt := range stats.AcceptedTriggers {
		if receipt.Tenant == "" || receipt.RevisionID <= 0 || receipt.OccurrenceKey == "" || seen[receipt] || observed[receipt] != 1 {
			return finish("accepted manual trigger lacks exactly one matching tenant/revision/occurrence ledger row, or its receipt is invalid/duplicated")
		}
		seen[receipt] = true
	}
	if len(observed) != len(seen) {
		return finish("manual ledger rows do not match this execution's accepted receipts")
	}
	check.Status = VerdictPass
	return finish("all scheduled manual triggers accepted and independently projected")
}
