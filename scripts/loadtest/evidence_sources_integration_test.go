//go:build integration

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lib/pq"
	"orbitjob/internal/platform/scan"
)

func TestCollectedEvidenceNeedsRealAttemptAndJournalSources(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("dedicated TEST_DATABASE_DSN required")
	}
	u, err := url.Parse(dsn)
	if err != nil || !strings.Contains(strings.ToLower(u.Path), "test") {
		t.Fatal("refusing a non-test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close() }()
	schema := fmt.Sprintf("quality_load_evidence_%d", time.Now().UnixNano())
	quoted := pq.QuoteIdentifier(schema)
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		if _, err := admin.ExecContext(cleanup, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	_, err = db.ExecContext(ctx, `
		CREATE TABLE job_run_control_plane(id bigint PRIMARY KEY, revision_id bigint, phase text, occurrence_key text, source_uid text, tenant_id text, trigger text);
		CREATE TABLE job_run_attempts_control_plane(run_id bigint, attempt_number int, phase text, tenant_id text);
		CREATE TABLE audit_events(resource_id text, diff jsonb, event_type text, tenant_id text);
	`)
	if err != nil {
		t.Fatal(err)
	}
	tenants := []string{scan.GenerateID(), scan.GenerateID()}
	created := []CreatedDefinition{{CaseID: "this-run-a", RevisionID: 1, Tenant: tenants[0]}, {CaseID: "this-run-b", RevisionID: 2, Tenant: tenants[1]}}
	defs := []Definition{{CaseID: "this-run-a", Expected: Expected{TerminalState: "success"}}, {CaseID: "this-run-b", Expected: Expected{TerminalState: "success"}}}
	var objects struct {
		Items []observedRunCR `json:"items"`
	}
	for i, cd := range created {
		run := ledgerRun{id: int64(i + 1), revisionID: cd.RevisionID, phase: phaseSucceeded, trigger: "Manual", occurrenceKey: fmt.Sprintf("occurrence-%d", i), sourceUID: fmt.Sprintf("source-%d", i)}
		if _, err := db.ExecContext(ctx, `INSERT INTO job_run_control_plane VALUES($1,$2,$3,$4,$5,$6,$7)`, run.id, run.revisionID, run.phase, run.occurrenceKey, run.sourceUID, cd.Tenant, run.trigger); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO job_run_attempts_control_plane VALUES($1,1,'Succeeded',$2)`, run.id, cd.Tenant); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO audit_events VALUES($1,'{"from_phase":"Running","to_phase":"Succeeded"}','job_run.status_changed',$2)`, fmt.Sprint(run.id), cd.Tenant); err != nil {
			t.Fatal(err)
		}
		obj := observedRunCR{}
		obj.Metadata.Name = jobRunCRName(cd.CaseID, run.occurrenceKey)
		obj.Spec.OccurrenceKey = run.occurrenceKey
		obj.Spec.RevisionID = run.revisionID
		obj.Spec.Source.UID = run.sourceUID
		obj.Spec.Trigger = run.trigger
		objects.Items = append(objects.Items, obj)
	}
	// A foreign revision in the same tenant must not poison this run's audit.
	if _, err := db.ExecContext(ctx, `INSERT INTO job_run_control_plane VALUES(999,999,'Running','foreign','foreign',$1);`, tenants[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO audit_events VALUES('999','{"from_phase":"Succeeded","to_phase":"Running"}','job_run.status_changed',$1)`, tenants[0]); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	data, _ := json.Marshal(objects)
	path := filepath.Join(bin, "objects.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte("#!/bin/sh\nexec /bin/cat \"$LOAD_EVIDENCE_CR_FILE\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("LOAD_EVIDENCE_CR_FILE", path)
	var journal atomic.Value
	journal.Store(`[{"case_id":"this-run-a","idempotency_key":"delivery","operation":"delivery"}]`)
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(journal.Load().(string))) }))
	defer fixture.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer api.Close()
	collect := func() Evidence {
		e, err := CollectEvidence(ctx, u.String(), fixture.URL, api.URL, map[string]string{tenants[0]: "local-a", tenants[1]: "local-b"}, map[string]string{tenants[0]: "namespace-a", tenants[1]: "namespace-b"}, created, defs)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	e := collect()
	if len(e.MissingSources) > 0 || e.TransitionObservations != 2 || len(e.Regressions) != 0 {
		t.Fatalf("scoped evidence=%+v", e)
	}
	if result := EvaluateQualification(RunRecord{}, e, 0); result.Verdict != VerdictPass {
		t.Fatalf("complete real sources did not pass: %+v", result)
	}
	// An accepted request lost before ledger projection used to be invisible:
	// both tenants still have healthy observations, so aggregate checks pass.
	if len(e.ManualRuns) != 2 {
		t.Fatalf("manual ledger identities not collected: %+v", e.ManualRuns)
	}
	stats := RunStats{ConfigSHA256: "recorded", Commit: "execution", ScheduledEvents: 3, Triggered: 3, Accepted: 3, AcceptedTriggers: append([]AcceptedTrigger(nil), e.ManualRuns...)}
	stats.AcceptedTriggers = append(stats.AcceptedTriggers, AcceptedTrigger{Tenant: tenants[0], RevisionID: 1, OccurrenceKey: "accepted-but-never-projected"})
	e.ExecutionChecks = executionEvidenceChecks(stats, e)
	if result := EvaluateQualification(RunRecord{}, e, 0); result.Verdict != VerdictInconclusive {
		t.Fatalf("accepted but absent run disappeared from verdict: %+v", result)
	}
	stats.Accepted, stats.Triggered, stats.ScheduledEvents = 2, 2, 2
	stats.AcceptedTriggers = stats.AcceptedTriggers[:2]
	e.ExecutionChecks = executionEvidenceChecks(stats, e)
	if result := EvaluateQualification(RunRecord{}, e, 0); result.Verdict != VerdictPass {
		t.Fatalf("complete trigger/ledger correlation did not pass: %+v", result)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM job_run_attempts_control_plane`); err != nil {
		t.Fatal(err)
	}
	journal.Store(`[]`)
	e = collect()
	missing := strings.Join(e.MissingSources, ",")
	if !strings.Contains(missing, "attempts:") || !strings.Contains(missing, "ledger: empty source") {
		t.Fatalf("empty real sources not recorded: %+v", e.MissingSources)
	}
	if result := EvaluateQualification(RunRecord{}, e, 0); result.Verdict != VerdictInconclusive {
		t.Fatalf("empty real sources verdict=%s", result.Verdict)
	}
}
