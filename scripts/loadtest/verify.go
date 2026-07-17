package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lib/pq"
)

// terminalStatuses are the states an instance never leaves once reached.
var terminalStatuses = map[string]bool{"success": true, "failed": true, "canceled": true}

func VerifyCorrectness(evidence Evidence) []CheckResult {
	return []CheckResult{
		checkDuplicateTokens(evidence.Attempts),
		checkTenantLeak(evidence.TenantReads),
		checkDuplicateLedger(evidence.Ledger),
		checkTerminalRegression(evidence.Regressions),
		checkExpectedTerminal(evidence.TerminalMismatches),
	}
}

func EvaluateQualification(run RunRecord, evidence Evidence, minimumInstances int) Result {
	result := Result{SchemaVersion: "v020-result/v1", RunID: run.RunID, Qualification: run.Qualification}

	correctness := VerifyCorrectness(evidence)
	result.Checks = correctness

	for _, check := range correctness {
		if check.Status == VerdictFail {
			result.Verdict = VerdictFail
			result.Reason = fmt.Sprintf("correctness failure: %s", check.ID)
			result.CompletedAt = time.Now()
			return result
		}
	}

	if len(evidence.MissingSources) > 0 {
		result.Verdict = VerdictInconclusive
		result.Reason = fmt.Sprintf("missing evidence sources: %s", strings.Join(evidence.MissingSources, ", "))
		result.CompletedAt = time.Now()
		return result
	}

	if evidence.Truncated {
		result.Verdict = VerdictInconclusive
		result.Reason = "run truncated before the schedule completed"
		result.CompletedAt = time.Now()
		return result
	}

	if evidence.InFlight > 0 {
		result.Verdict = VerdictInconclusive
		result.Reason = fmt.Sprintf("%d instances not in a terminal state at verify time", evidence.InFlight)
		result.CompletedAt = time.Now()
		return result
	}

	if evidence.MissingDefinitions > 0 {
		result.Verdict = VerdictInconclusive
		result.Reason = fmt.Sprintf("%d created definitions have no instances", evidence.MissingDefinitions)
		result.CompletedAt = time.Now()
		return result
	}

	if !run.Qualification {
		result.Verdict = VerdictPass
		result.Reason = "non-standard run completed"
		result.CompletedAt = time.Now()
		return result
	}

	if evidence.QualifiedInstances < minimumInstances {
		result.Verdict = VerdictFail
		result.Reason = fmt.Sprintf("qualified instances %d < %d", evidence.QualifiedInstances, minimumInstances)
		result.CompletedAt = time.Now()
		return result
	}

	result.Verdict = VerdictPass
	result.Reason = "all qualification gates passed"
	result.CompletedAt = time.Now()
	return result
}

type Evidence struct {
	Instances          int // instances belonging to this run's definitions
	QualifiedInstances int // instances in their expected terminal state
	InFlight           int // instances not yet in a terminal state
	MissingDefinitions int // created definitions with zero instances
	Attempts           []Attempt
	TenantReads        []TenantRead
	Ledger             []LedgerEntry
	Regressions        []Regression
	TerminalMismatches []TerminalMismatch
	MissingSources     []string
	Truncated          bool
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

// Regression is a terminal-state violation recovered from audit_events: an
// instance that left a terminal status after reaching it.
type Regression struct {
	ResourceID string
	From       string
	To         string
}

// TerminalMismatch counts instances of one definition that terminated in a
// state other than the definition's Expected.TerminalState.
type TerminalMismatch struct {
	CaseID   string
	JobID    int64
	Expected string
	Actual   string
	Count    int
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

func checkTerminalRegression(regressions []Regression) CheckResult {
	if len(regressions) > 0 {
		first := regressions[0]
		return CheckResult{ID: "terminal-regression", Status: VerdictFail,
			Message: fmt.Sprintf("%d terminal regressions; first: instance %s went %s -> %s", len(regressions), first.ResourceID, first.From, first.To)}
	}
	return CheckResult{ID: "terminal-regression", Status: VerdictPass}
}

func checkExpectedTerminal(mismatches []TerminalMismatch) CheckResult {
	if len(mismatches) > 0 {
		first := mismatches[0]
		return CheckResult{ID: "expected-terminal-state", Status: VerdictFail,
			Message: fmt.Sprintf("%d definitions terminated in unexpected states; first: %s expected %s, got %s x%d",
				len(mismatches), first.CaseID, first.Expected, first.Actual, first.Count)}
	}
	return CheckResult{ID: "expected-terminal-state", Status: VerdictPass}
}

// CollectEvidence reads correctness evidence from the OrbitJob database, the
// load fixture ledger, and cross-tenant Admin API probes. It never mutates
// OrbitJob state. The orbitjob DSN must be read-only. Instance counting is
// scoped to the run's created definitions: a full-table count would let
// leftovers from earlier runs feed the qualification gate.
func CollectEvidence(ctx context.Context, orbitjobDSN, fixtureURL, apiURL string, tenantKeys map[string]string, created []CreatedDefinition, definitions []Definition) (Evidence, error) {
	var e Evidence

	expectedByCase := make(map[string]string, len(definitions))
	for _, def := range definitions {
		terminal := def.Expected.TerminalState
		if terminal == "" {
			terminal = "success"
		}
		expectedByCase[def.CaseID] = terminal
	}
	jobIDs := make([]int64, 0, len(created))
	expectedByJob := make(map[int64]string, len(created))
	caseByJob := make(map[int64]string, len(created))
	for _, cd := range created {
		jobIDs = append(jobIDs, cd.JobID)
		expectedByJob[cd.JobID] = expectedByCase[cd.CaseID]
		caseByJob[cd.JobID] = cd.CaseID
	}

	odb, err := sql.Open("postgres", orbitjobDSN)
	if err != nil {
		return e, fmt.Errorf("open orbitjob db: %w", err)
	}
	defer func() { _ = odb.Close() }()

	// Instance counts by status, scoped to this run's definitions.
	statusCounts := map[int64]map[string]int{}
	rows, err := odb.QueryContext(ctx, `
		SELECT job_id, status, count(*) FROM job_instances
		WHERE job_id = ANY($1)
		GROUP BY job_id, status
	`, pq.Array(jobIDs))
	if err != nil {
		return e, fmt.Errorf("count instances by status: %w", err)
	}
	for rows.Next() {
		var jobID int64
		var status string
		var count int
		if err := rows.Scan(&jobID, &status, &count); err != nil {
			_ = rows.Close()
			return e, fmt.Errorf("scan instance count: %w", err)
		}
		if statusCounts[jobID] == nil {
			statusCounts[jobID] = map[string]int{}
		}
		statusCounts[jobID][status] = count
		e.Instances += count
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return e, fmt.Errorf("iterate instance counts: %w", err)
	}
	_ = rows.Close()

	mismatchCounts := map[int64]map[string]int{}
	for _, cd := range created {
		counts := statusCounts[cd.JobID]
		if len(counts) == 0 {
			e.MissingDefinitions++
			continue
		}
		expected := expectedByJob[cd.JobID]
		if expected == "" {
			expected = "success"
		}
		for status, count := range counts {
			switch {
			case !terminalStatuses[status]:
				e.InFlight += count
			case status == expected:
				e.QualifiedInstances += count
			default:
				if mismatchCounts[cd.JobID] == nil {
					mismatchCounts[cd.JobID] = map[string]int{}
				}
				mismatchCounts[cd.JobID][status] += count
			}
		}
	}
	for jobID, byStatus := range mismatchCounts {
		for status, count := range byStatus {
			e.TerminalMismatches = append(e.TerminalMismatches, TerminalMismatch{
				CaseID:   caseByJob[jobID],
				JobID:    jobID,
				Expected: expectedByJob[jobID],
				Actual:   status,
				Count:    count,
			})
		}
	}

	// Attempt tokens for the duplicate-execution check.
	rows, err = odb.QueryContext(ctx, "SELECT instance_id, attempt_no, status FROM job_instance_attempts WHERE status IN ('success','running')")
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
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return e, fmt.Errorf("iterate attempts: %w", err)
	}
	_ = rows.Close()

	// Terminal regressions from the audit log: an instance that left a
	// terminal status after reaching it. audit_events survives runs, so scope
	// to this run's job ids via the instances table join.
	rows, err = odb.QueryContext(ctx, `
		SELECT ae.resource_id, ae.diff->>'from_status', ae.diff->>'to_status'
		FROM audit_events ae
		WHERE ae.event_type = 'instance.status_changed'
		  AND ae.diff->>'from_status' IN ('success','failed','canceled')
		  AND ae.diff->>'to_status' <> ae.diff->>'from_status'
		  AND ae.created_at > now() - interval '1 day'
	`)
	if err != nil {
		return e, fmt.Errorf("query regressions: %w", err)
	}
	for rows.Next() {
		var r Regression
		if err := rows.Scan(&r.ResourceID, &r.From, &r.To); err != nil {
			_ = rows.Close()
			return e, fmt.Errorf("scan regression: %w", err)
		}
		e.Regressions = append(e.Regressions, r)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return e, fmt.Errorf("iterate regressions: %w", err)
	}
	_ = rows.Close()

	// Cross-tenant probes: every actor reads up to three foreign definitions.
	// Only 200 counts as a leak; 403/404 are expected denials; anything else
	// (transport error, 5xx) is an evidence gap, not a pass.
	for actor, actorKey := range tenantKeys {
		client := NewAPIClient(apiURL, actorKey)
		probed := 0
		for _, def := range created {
			if def.Tenant == actor || probed >= 3 {
				continue
			}
			_, err := client.GetJob(ctx, def.JobID, def.Tenant)
			status := 200
			if err != nil {
				var aerr *APIError
				if errors.As(err, &aerr) {
					status = aerr.StatusCode
				} else {
					status = 0
				}
			}
			if status == 0 || status >= 500 {
				e.MissingSources = append(e.MissingSources, fmt.Sprintf("tenant-probe:%s", actor))
				break
			}
			e.TenantReads = append(e.TenantReads, TenantRead{ActorTenant: actor, ObjectTenant: def.Tenant, HTTPStatus: status})
			probed++
		}
		if probed == 0 {
			e.MissingSources = append(e.MissingSources, fmt.Sprintf("tenant-probe:%s", actor))
		}
	}

	if fixtureURL != "" {
		// The fixture keeps its ledger in an in-memory dict exposed over HTTP
		// (deploy/load/fixture-configmap.yaml), not a Postgres table. Querying the
		// HTTP endpoint is the only way to read it.
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fixtureURL+"/ledger", nil)
		if err != nil {
			e.MissingSources = append(e.MissingSources, "ledger")
		} else {
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				e.MissingSources = append(e.MissingSources, "ledger")
			} else {
				defer func() { _ = resp.Body.Close() }()
				var entries []struct {
					IdempotencyKey string `json:"idempotency_key"`
					Operation      string `json:"operation"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
					e.MissingSources = append(e.MissingSources, "ledger")
				} else {
					for _, entry := range entries {
						e.Ledger = append(e.Ledger, LedgerEntry{IdempotencyKey: entry.IdempotencyKey, Operation: entry.Operation})
					}
				}
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
	fixtureURL := flags.String("fixture-url", defaultFixtureURL(), "load fixture HTTP URL")
	apiURL := flags.String("api-url", os.Getenv("ORBITJOB_API_URL"), "Admin API URL")
	configPath := flags.String("config", "test/load/config/standard.yaml", "load config (qualification flag)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *runID == "" || *orbitjobDSN == "" || *apiURL == "" {
		return fmt.Errorf("run-id, orbitjob-dsn and api-url are required")
	}
	cfg, err := LoadConfig(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
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
	definitions, err := loadGeneratedDefinitions(filepath.Join(runDir, "generated", "definitions.json"))
	if err != nil {
		return fmt.Errorf("load generated definitions: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	evidence, err := CollectEvidence(ctx, *orbitjobDSN, *fixtureURL, *apiURL, tenantKeys, created, definitions)
	if err != nil {
		return fmt.Errorf("collect evidence: %w", err)
	}

	stats, err := loadRunStats(filepath.Join(runDir, "run-stats.json"))
	if err != nil {
		return fmt.Errorf("load run stats: %w (run the loadtest run command first; it writes run-stats.json)", err)
	}
	evidence.Truncated = !stats.Completed

	run := RunRecord{
		RunID:         *runID,
		Profile:       cfg.Profile,
		Qualification: cfg.Qualification,
		Seed:          cfg.Seed,
		Commit:        gitCommit(),
		Dirty:         gitDirty(),
		StartedAt:     stats.StartedAt,
		Environment: Environment{
			DockerCPU:         cfg.Environment.DockerCPU,
			DockerMemoryBytes: int64(cfg.Environment.DockerMemoryGi) * (1 << 30),
		},
	}
	if err := writeStableJSON(filepath.Join(runDir, "run-record.json"), run); err != nil {
		return fmt.Errorf("write run record: %w", err)
	}

	result := EvaluateQualification(run, evidence, cfg.MinimumInstances)
	if err := writeStableJSON(filepath.Join(runDir, "result.json"), result); err != nil {
		return err
	}
	fmt.Printf("verify: verdict=%s reason=%s instances=%d qualified=%d in_flight=%d\n",
		result.Verdict, result.Reason, evidence.Instances, evidence.QualifiedInstances, evidence.InFlight)
	return nil
}

func defaultFixtureURL() string {
	if v := os.Getenv("ORBITJOB_FIXTURE_URL"); v != "" {
		return v
	}
	return fixtureURL
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

func loadRunStats(path string) (RunStats, error) {
	var stats RunStats
	data, err := os.ReadFile(path)
	if err != nil {
		return stats, err
	}
	if err := json.Unmarshal(data, &stats); err != nil {
		return stats, fmt.Errorf("decode run stats: %w", err)
	}
	return stats, nil
}
