package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/lib/pq"
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

// CollectEvidence reads correctness evidence from the OrbitJob database, the
// load fixture ledger, and cross-tenant Admin API probes. It never mutates
// OrbitJob state. The orbitjob DSN must be read-only.
func CollectEvidence(ctx context.Context, orbitjobDSN, loadPGDSN, apiURL string, tenantKeys map[string]string, created []CreatedDefinition) (Evidence, error) {
	var e Evidence

	odb, err := sql.Open("postgres", orbitjobDSN)
	if err != nil {
		return e, fmt.Errorf("open orbitjob db: %w", err)
	}
	defer func() { _ = odb.Close() }()

	if err := odb.QueryRowContext(ctx, "SELECT count(*) FROM job_instances").Scan(&e.Instances); err != nil {
		return e, fmt.Errorf("count instances: %w", err)
	}

	rows, err := odb.QueryContext(ctx, "SELECT instance_id, attempt_no, status FROM job_instance_attempts WHERE status IN ('success','running')")
	if err != nil {
		return e, fmt.Errorf("query attempts: %w", err)
	}
	for rows.Next() {
		var instID int64
		var attemptNo int
		var status string
		if err := rows.Scan(&instID, &attemptNo, &status); err != nil {
			_ = rows.Close()
			return e, err
		}
		e.Attempts = append(e.Attempts, Attempt{InstanceID: instID, Token: fmt.Sprintf("attempt-%d", attemptNo), Valid: status == "success"})
	}
	_ = rows.Close()

	for actor, actorKey := range tenantKeys {
		client := NewAPIClient(apiURL, actorKey)
		for _, def := range created {
			if def.Tenant == actor {
				continue
			}
			_, err := client.GetJob(ctx, def.JobID, def.Tenant)
			status := 403
			if err == nil {
				status = 200
			}
			e.TenantReads = append(e.TenantReads, TenantRead{ActorTenant: actor, ObjectTenant: def.Tenant, HTTPStatus: status})
			break
		}
	}

	if loadPGDSN != "" {
		lbdb, err := sql.Open("postgres", loadPGDSN)
		if err != nil {
			e.MissingSources = append(e.MissingSources, "load-pg")
		} else {
			defer func() { _ = lbdb.Close() }()
			lrows, err := lbdb.QueryContext(ctx, "SELECT idempotency_key, operation FROM ledger")
			if err != nil {
				e.MissingSources = append(e.MissingSources, "ledger")
			} else {
				for lrows.Next() {
					var key, op string
					if err := lrows.Scan(&key, &op); err != nil {
						_ = lrows.Close()
						return e, err
					}
					e.Ledger = append(e.Ledger, LedgerEntry{IdempotencyKey: key, Operation: op})
				}
				_ = lrows.Close()
			}
		}
	}

	return e, nil
}

func runVerify(args []string) error {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	runID := flags.String("run-id", "", "run ID")
	runRoot := flags.String("run-root", "test/load/runs", "run root")
	orbitjobDSN := flags.String("orbitjob-dsn", os.Getenv("ORBITJOB_DSN"), "OrbitJob read-only DSN")
	loadPGDSN := flags.String("load-pg-dsn", os.Getenv("LOAD_PG_DSN"), "load fixture PG DSN")
	apiURL := flags.String("api-url", os.Getenv("ORBITJOB_API_URL"), "Admin API URL")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *runID == "" || *orbitjobDSN == "" || *apiURL == "" {
		return fmt.Errorf("run-id, orbitjob-dsn and api-url are required")
	}
	runDir := filepath.Join(*runRoot, *runID)
	tenantKeys, err := loadTenantKeys(filepath.Join(runDir, "tenant-keys.json"))
	if err != nil {
		return fmt.Errorf("load tenant keys: %w", err)
	}
	created, err := loadCreatedDefinitions(filepath.Join(runDir, "created-definitions.json"))
	if err != nil {
		return fmt.Errorf("load created definitions: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	evidence, err := CollectEvidence(ctx, *orbitjobDSN, *loadPGDSN, *apiURL, tenantKeys, created)
	if err != nil {
		return fmt.Errorf("collect evidence: %w", err)
	}
	run := RunRecord{RunID: *runID, Qualification: true, StartedAt: time.Now()}
	result := EvaluateQualification(run, evidence)
	if err := writeStableJSON(filepath.Join(runDir, "result.json"), result); err != nil {
		return err
	}
	fmt.Printf("verify: verdict=%s reason=%s instances=%d\n", result.Verdict, result.Reason, evidence.Instances)
	return nil
}

func loadTenantKeys(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var keys map[string]string
	if err := json.Unmarshal(data, &keys); err != nil {
		return nil, err
	}
	return keys, nil
}

func loadCreatedDefinitions(path string) ([]CreatedDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var created []CreatedDefinition
	if err := json.Unmarshal(data, &created); err != nil {
		return nil, err
	}
	return created, nil
}
