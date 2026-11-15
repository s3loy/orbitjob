package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"orbitjob/internal/platform/scan"
)

func TestTriggerAcceptanceRequiresARealRunReceipt(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		accepted bool
	}{
		{"conflict", 409, `{}`, false},
		{"empty success", 200, `{}`, false},
		{"wrong trigger", 201, `{"namespace":"tasks","name":"run","occurrence_key":"key","trigger":"Cron"}`, false},
		{"malformed", 201, `{`, false},
		{"created", 201, `{"namespace":"tasks","name":"run","occurrence_key":"key","trigger":"Manual","created":true}`, true},
		{"replay", 200, `{"namespace":"tasks","name":"run","occurrence_key":"key","trigger":"Manual","created":false}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); _, _ = fmt.Fprint(w, tc.body) }))
			defer srv.Close()
			schedule := PhaseSchedule{Events: []TriggerEvent{{Tenant: "actor", RevisionID: 7, IdempotencyKey: "request"}}}
			engine := NewRunEngine(map[string]*APIClient{"actor": NewAPIClient(srv.URL, "key")}, schedule, nil)
			if err := engine.Run(context.Background(), func() time.Duration { return 0 }); err != nil {
				t.Fatal(err)
			}
			_, accepted, rejected, _ := engine.Stats()
			if (accepted == 1) != tc.accepted || accepted+rejected != 1 || int64(len(engine.AcceptedTriggers())) != accepted {
				t.Fatalf("accepted=%d rejected=%d receipts=%+v", accepted, rejected, engine.AcceptedTriggers())
			}
		})
	}
}

func TestManualAccountingCannotPassWithLostWorkOrCronSubstitutes(t *testing.T) {
	receipt := AcceptedTrigger{Tenant: scan.GenerateID(), RevisionID: 7, OccurrenceKey: "accepted"}
	for _, tc := range []struct {
		name   string
		change func(*RunStats, *Evidence)
	}{
		{"accepted never projected", func(s *RunStats, e *Evidence) { e.ManualRuns = nil }},
		{"other occurrence", func(s *RunStats, e *Evidence) { e.ManualRuns[0].OccurrenceKey = "cron-substitute" }},
		{"other tenant", func(s *RunStats, e *Evidence) { e.ManualRuns[0].Tenant = scan.GenerateID() }},
		{"other revision", func(s *RunStats, e *Evidence) { e.ManualRuns[0].RevisionID++ }},
		{"duplicate projection", func(s *RunStats, e *Evidence) { e.ManualRuns = append(e.ManualRuns, e.ManualRuns[0]) }},
		{"duplicate receipt", func(s *RunStats, e *Evidence) {
			s.AcceptedTriggers = append(s.AcceptedTriggers, s.AcceptedTriggers[0])
			s.Accepted = 2
			s.Triggered = 2
			s.ScheduledEvents = 2
		}},
		{"missing receipt", func(s *RunStats, e *Evidence) { s.AcceptedTriggers = nil }},
		{"rejected", func(s *RunStats, e *Evidence) { s.Rejected = 1; s.Triggered = 2; s.ScheduledEvents = 2 }},
		{"skipped", func(s *RunStats, e *Evidence) { s.Skipped = 1; s.ScheduledEvents = 2 }},
		{"impossible counters", func(s *RunStats, e *Evidence) { s.Triggered = 2 }},
		{"negative counters", func(s *RunStats, e *Evidence) { s.Rejected = -1 }},
		{"legacy source", func(s *RunStats, e *Evidence) { s.Commit = "" }},
		{"legacy config", func(s *RunStats, e *Evidence) { s.ConfigSHA256 = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stats := RunStats{ConfigSHA256: "observed", Commit: "execution", ScheduledEvents: 1, Triggered: 1, Accepted: 1, AcceptedTriggers: []AcceptedTrigger{receipt}, Completed: true}
			e := completeVerificationEvidence()
			e.ManualRuns = []AcceptedTrigger{receipt}
			tc.change(&stats, &e)
			e.ExecutionChecks = executionEvidenceChecks(stats, e)
			if result := EvaluateQualification(RunRecord{}, e, 0); result.Verdict != VerdictInconclusive {
				t.Fatalf("false pass: %+v", result)
			}
		})
	}
	stats := RunStats{ConfigSHA256: "observed", Commit: "execution", ScheduledEvents: 1, Triggered: 1, Accepted: 1, AcceptedTriggers: []AcceptedTrigger{receipt}}
	e := completeVerificationEvidence()
	e.ManualRuns = []AcceptedTrigger{receipt}
	e.ExecutionChecks = executionEvidenceChecks(stats, e)
	if result := EvaluateQualification(RunRecord{}, e, 0); result.Verdict != VerdictPass {
		t.Fatalf("complete receipt failed: %+v", result)
	}
}

func TestVerificationArtifactsMustCoverTheWholeCorpus(t *testing.T) {
	tenant := scan.GenerateID()
	for _, tc := range []struct {
		name   string
		change func(*[]CreatedDefinition, *[]Definition, map[string]string, map[string]string)
	}{
		{"missing created", func(c *[]CreatedDefinition, d *[]Definition, k, n map[string]string) { *c = nil }},
		{"duplicate case", func(c *[]CreatedDefinition, d *[]Definition, k, n map[string]string) {
			*c = append(*c, (*c)[0])
			*d = append(*d, (*d)[0])
		}},
		{"missing actor key", func(c *[]CreatedDefinition, d *[]Definition, k, n map[string]string) { delete(k, tenant) }},
		{"wrong namespace", func(c *[]CreatedDefinition, d *[]Definition, k, n map[string]string) { n[tenant] = "other" }},
		{"wrong tenant", func(c *[]CreatedDefinition, d *[]Definition, k, n map[string]string) {
			(*c)[0].Tenant = scan.GenerateID()
		}},
		{"wrong expectation", func(c *[]CreatedDefinition, d *[]Definition, k, n map[string]string) {
			(*c)[0].ExpectedTerminalState = "failed"
		}},
		{"zero revision", func(c *[]CreatedDefinition, d *[]Definition, k, n map[string]string) { (*c)[0].RevisionID = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			created := []CreatedDefinition{{CaseID: "case", Tenant: tenant, RevisionID: 7, ExpectedTerminalState: "success"}}
			defs := []Definition{{CaseID: "case", Tenant: tenant, Expected: Expected{TerminalState: "success"}}}
			keys := map[string]string{tenant: "key"}
			namespaces := map[string]string{tenant: TenantNamespace(tenant)}
			if err := validateVerificationArtifacts(created, defs, keys, namespaces); err != nil {
				t.Fatal(err)
			}
			tc.change(&created, &defs, keys, namespaces)
			if err := validateVerificationArtifacts(created, defs, keys, namespaces); err == nil {
				t.Fatal("incomplete/corrupt corpus accepted")
			}
		})
	}
}
