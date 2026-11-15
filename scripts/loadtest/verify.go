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
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/lib/pq"
	workloadsv1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
)

// The run lifecycle the ledger records. These mirror the phases the operator
// writes into job_run_control_plane; the load tool does not import the domain
// package, and a drift here is a verify failure, not a silent mismatch.
const (
	phaseSucceeded = "Succeeded"
	phaseFailed    = "Failed"
	phaseCanceled  = "Canceled"
)

// terminalPhases are the states a run never leaves once reached.
var terminalPhases = map[string]bool{phaseSucceeded: true, phaseFailed: true, phaseCanceled: true}

// scenarioToLedgerPhase translates a scenario's terminal_state into the phase
// the ledger records. A scenario expecting "canceled" ends Canceled only
// because the load generator issues the cancel itself.
var scenarioToLedgerPhase = map[string]string{
	"success":  phaseSucceeded,
	"failed":   phaseFailed,
	"canceled": phaseCanceled,
}

func VerifyCorrectness(evidence Evidence) []CheckResult {
	checks := []CheckResult{
		checkDuplicateSucceededAttempts(evidence.Attempts),
		checkTenantLeak(evidence.TenantReads),
		checkDuplicateLedger(evidence.Ledger),
		checkTerminalRegression(evidence.Regressions, evidence.TransitionObservations),
		checkExpectedTerminal(evidence.TerminalMismatches),
		checkJobRunCRsPresent(evidence.MissingCRs, evidence.MissingCRSamples),
	}
	if evidence.Runs == 0 {
		for i := range checks {
			if checks[i].Status == VerdictPass && (checks[i].ID == "expected-terminal-state" || checks[i].ID == "jobrun-cr-present") {
				checks[i].Status, checks[i].Message = VerdictInconclusive, "empty run evidence source"
			}
		}
	}
	checks = append(checks, evidence.ExecutionChecks...)
	return append(checks, evidence.FaultChecks...)
}

func EvaluateQualification(run RunRecord, evidence Evidence, minimumRuns int) Result {
	result := Result{RunID: run.RunID, Qualification: run.Qualification}

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
	for _, check := range correctness {
		if check.Status == VerdictInconclusive {
			result.Verdict, result.Reason = VerdictInconclusive, fmt.Sprintf("incomplete evidence: %s: %s", check.ID, check.Message)
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
		result.Reason = fmt.Sprintf("%d runs not in a terminal state at verify time", evidence.InFlight)
		result.CompletedAt = time.Now()
		return result
	}

	if !run.Qualification {
		result.Verdict = VerdictPass
		result.Reason = "non-standard run completed"
		if evidence.MissingDefinitions > 0 {
			// Coverage is reported, not judged. A smoke profile emits about 500
			// events across a corpus of 1200 definitions, so it can never touch
			// them all -- the gates below treat that as inconclusive, which is
			// right for a qualification run and wrong here.
			result.Reason = fmt.Sprintf("non-standard run completed; %d created definitions were never triggered", evidence.MissingDefinitions)
		}
		result.CompletedAt = time.Now()
		return result
	}

	// Coverage gates. These say the run did not exercise enough to conclude
	// anything, which is a statement about the run rather than about the
	// system, so they apply only where the profile claims to be exhaustive.
	if evidence.MissingDefinitions > 0 {
		result.Verdict = VerdictInconclusive
		result.Reason = fmt.Sprintf("%d created definitions have no runs", evidence.MissingDefinitions)
		result.CompletedAt = time.Now()
		return result
	}

	if evidence.QualifiedRuns < minimumRuns {
		result.Verdict = VerdictFail
		result.Reason = fmt.Sprintf("qualified runs %d < %d", evidence.QualifiedRuns, minimumRuns)
		result.CompletedAt = time.Now()
		return result
	}

	result.Verdict = VerdictPass
	result.Reason = "all qualification gates passed"
	result.CompletedAt = time.Now()
	return result
}

type Evidence struct {
	ManualRuns             []AcceptedTrigger
	ExecutionChecks        []CheckResult
	Runs                   int // runs belonging to this run's definitions
	QualifiedRuns          int // runs in their expected terminal state with their CR present
	InFlight               int // runs not yet in a terminal state
	MissingDefinitions     int // created definitions with zero runs
	MissingCRs             int // terminal ledger runs whose JobRun custom resource is gone
	MissingCRSamples       []string
	Attempts               []RunAttempt
	TenantReads            []TenantRead
	Ledger                 []LedgerEntry
	Regressions            []Regression
	TransitionObservations int
	FaultChecks            []CheckResult
	TerminalMismatches     []TerminalMismatch
	MissingSources         []string
	Truncated              bool
}

// RunAttempt is one platform attempt of one run: an attempt is one Kubernetes
// Job, and at most one of a run's attempts may have succeeded.
type RunAttempt struct {
	RunID         int64
	AttemptNumber int
	Succeeded     bool
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

// Regression is a terminal-state violation recovered from the audit log: a run
// that left a terminal phase after reaching it.
type Regression struct {
	ResourceID string
	From       string
	To         string
}

// TerminalMismatch counts the runs of one definition that terminated in a
// phase other than the definition's expected terminal state.
type TerminalMismatch struct {
	CaseID     string
	RevisionID int64
	Expected   string
	Actual     string
	Count      int
}

// checkDuplicateSucceededAttempts fails a run that has two successful
// attempts: each attempt is one Kubernetes Job, so two succeeded means the
// same occurrence executed twice.
func checkDuplicateSucceededAttempts(attempts []RunAttempt) CheckResult {
	if len(attempts) == 0 {
		return CheckResult{ID: "duplicate-succeeded-attempt", Status: VerdictInconclusive, Message: "empty attempt evidence source"}
	}
	succeeded := map[int64]bool{}
	for _, attempt := range attempts {
		if !attempt.Succeeded {
			continue
		}
		if succeeded[attempt.RunID] {
			return CheckResult{ID: "duplicate-succeeded-attempt", Status: VerdictFail, Message: fmt.Sprintf("run %d has two succeeded attempts", attempt.RunID)}
		}
		succeeded[attempt.RunID] = true
	}
	return CheckResult{ID: "duplicate-succeeded-attempt", Status: VerdictPass}
}

func checkTenantLeak(reads []TenantRead) CheckResult {
	if len(reads) == 0 {
		return CheckResult{ID: "tenant-leak", Status: VerdictInconclusive, Message: "empty tenant-probe evidence source"}
	}
	for _, read := range reads {
		if read.ActorTenant != read.ObjectTenant && read.HTTPStatus == 200 {
			return CheckResult{ID: "tenant-leak", Status: VerdictFail, Message: fmt.Sprintf("%s read %s data", read.ActorTenant, read.ObjectTenant)}
		}
	}
	return CheckResult{ID: "tenant-leak", Status: VerdictPass}
}

func checkDuplicateLedger(entries []LedgerEntry) CheckResult {
	if len(entries) == 0 {
		return CheckResult{ID: "duplicate-idempotent-side-effect", Status: VerdictInconclusive, Message: "empty side-effect journal"}
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		if seen[entry.IdempotencyKey] {
			return CheckResult{ID: "duplicate-idempotent-side-effect", Status: VerdictFail, Message: fmt.Sprintf("duplicate ledger key %s", entry.IdempotencyKey)}
		}
		seen[entry.IdempotencyKey] = true
	}
	return CheckResult{ID: "duplicate-idempotent-side-effect", Status: VerdictPass}
}

func checkTerminalRegression(regressions []Regression, observations int) CheckResult {
	if len(regressions) > 0 {
		first := regressions[0]
		return CheckResult{ID: "terminal-regression", Status: VerdictFail,
			Message: fmt.Sprintf("%d terminal regressions; first: run %s went %s -> %s", len(regressions), first.ResourceID, first.From, first.To)}
	}
	if observations == 0 {
		return CheckResult{ID: "terminal-regression", Status: VerdictInconclusive, Message: "empty phase-transition evidence source"}
	}
	return CheckResult{ID: "terminal-regression", Status: VerdictPass}
}

func checkExpectedTerminal(mismatches []TerminalMismatch) CheckResult {
	if len(mismatches) > 0 {
		first := mismatches[0]
		return CheckResult{ID: "expected-terminal-state", Status: VerdictFail,
			Message: fmt.Sprintf("%d definitions terminated in unexpected phases; first: %s expected %s, got %s x%d",
				len(mismatches), first.CaseID, first.Expected, first.Actual, first.Count)}
	}
	return CheckResult{ID: "expected-terminal-state", Status: VerdictPass}
}

// checkJobRunCRsPresent fails when a terminal ledger run has no JobRun custom
// resource. Generated definitions raise their history limits high enough that
// retention prunes nothing during the run, so a missing CR means the cluster
// lost the object or deleted it out from under the ledger.
func checkJobRunCRsPresent(missing int, samples []string) CheckResult {
	if missing > 0 {
		return CheckResult{ID: "jobrun-cr-present", Status: VerdictFail,
			Message: fmt.Sprintf("%d terminal runs have no JobRun custom resource; first: %s", missing, strings.Join(samples, ", "))}
	}
	return CheckResult{ID: "jobrun-cr-present", Status: VerdictPass}
}

// CollectEvidence reads correctness evidence from the OrbitJob database, the
// JobRun custom resources, the load fixture ledger, and cross-tenant Admin API
// probes. It never mutates OrbitJob state. Run counting is scoped to the
// run's created definitions: a full-table count would let leftovers from
// earlier runs feed the qualification gate.
//
// The ledger is row-level-security protected. Every per-tenant query runs in a
// transaction that first sets app.tenant_id to that tenant's ULID, and the
// queries also scope by this run's revision ids, so the verdict cannot depend
// on rows another tenant or another run wrote.
func CollectEvidence(ctx context.Context, orbitjobDSN, fixtureURL, apiURL string, tenantKeys, tenantNamespaces map[string]string, created []CreatedDefinition, definitions []Definition) (Evidence, error) {
	var e Evidence

	expectedByCase := make(map[string]string, len(definitions))
	for _, def := range definitions {
		terminal := def.Expected.TerminalState
		if terminal == "" {
			terminal = "success"
		}
		expected, ok := scenarioToLedgerPhase[terminal]
		if !ok {
			return e, fmt.Errorf("scenario terminal state %q has no ledger phase", terminal)
		}
		expectedByCase[def.CaseID] = expected
	}

	// Per-tenant revision scoping: the DB queries and the CR cross-check both
	// address one tenant's definitions at a time.
	tenantRevisions := map[string][]int64{}
	expectedByRevision := make(map[int64]string, len(created))
	caseByRevision := make(map[int64]string, len(created))
	for _, cd := range created {
		tenantRevisions[cd.Tenant] = append(tenantRevisions[cd.Tenant], cd.RevisionID)
		expectedByRevision[cd.RevisionID] = expectedByCase[cd.CaseID]
		caseByRevision[cd.RevisionID] = cd.CaseID
	}

	odb, err := sql.Open("postgres", orbitjobDSN)
	if err != nil {
		return e, fmt.Errorf("open orbitjob db: %w", err)
	}
	defer func() { _ = odb.Close() }()

	// Run counts by phase, per tenant, scoped to this run's revisions and to
	// the tenant the RLS guard names.
	runsByRevision := map[int64][]ledgerRun{}
	for tenant, revisions := range tenantRevisions {
		rows, err := queryTenantRuns(ctx, odb, tenant, revisions)
		if err != nil {
			return e, err
		}
		if len(rows) == 0 {
			e.MissingSources = append(e.MissingSources, "runs:"+tenant)
		}
		for _, row := range rows {
			runsByRevision[row.revisionID] = append(runsByRevision[row.revisionID], row)
		}

		attempts, err := queryTenantAttempts(ctx, odb, tenant, revisions)
		if err != nil {
			return e, err
		}
		e.Attempts = append(e.Attempts, attempts...)
		if len(attempts) == 0 {
			e.MissingSources = append(e.MissingSources, "attempts:"+tenant)
		}

		transitions, err := queryTenantTransitions(ctx, odb, tenant, revisions)
		if err != nil {
			return e, err
		}
		if len(transitions) == 0 {
			e.MissingSources = append(e.MissingSources, "phase-transitions:"+tenant)
		}
		for _, transition := range transitions {
			if transition.From == "" || transition.To == "" {
				e.MissingSources = append(e.MissingSources, "invalid-phase-transition:"+tenant)
				continue
			}
			e.TransitionObservations++
			if terminalPhases[transition.From] && transition.To != transition.From {
				e.Regressions = append(e.Regressions, transition)
			}
		}
	}

	// Classify each created definition's runs against its expectation.
	for _, cd := range created {
		rows := runsByRevision[cd.RevisionID]
		if len(rows) == 0 {
			e.MissingDefinitions++
			continue
		}
		expected := expectedByRevision[cd.RevisionID]
		mismatches := map[string]int{}
		for _, row := range rows {
			if row.trigger == "Manual" {
				e.ManualRuns = append(e.ManualRuns, AcceptedTrigger{Tenant: cd.Tenant, RevisionID: row.revisionID, OccurrenceKey: row.occurrenceKey})
			}
			e.Runs++
			switch {
			case !terminalPhases[row.phase]:
				e.InFlight++
			case row.phase == expected:
				e.QualifiedRuns++
			default:
				mismatches[row.phase]++
			}
		}
		for phase, count := range mismatches {
			e.TerminalMismatches = append(e.TerminalMismatches, TerminalMismatch{
				CaseID:     cd.CaseID,
				RevisionID: cd.RevisionID,
				Expected:   expected,
				Actual:     phase,
				Count:      count,
			})
		}
	}

	// CR cross-check: list the JobRun objects in each tenant's namespace and
	// require one for every terminal run the ledger reports, canceled runs
	// included. Generated definitions raise retention high enough that nothing
	// is pruned mid-run, so a terminal row without its object is a finding.
	// A namespace read failure is an evidence gap instead: the difference
	// between "the cluster lost an object" and "we could not look" matters.
	crPresent, err := collectRunCRs(ctx, tenantNamespaces)
	if err != nil {
		e.MissingSources = append(e.MissingSources, err.Error())
	} else {
		for _, cd := range created {
			for _, row := range runsByRevision[cd.RevisionID] {
				if !terminalPhases[row.phase] {
					continue
				}
				if !runCRPresent(crPresent, tenantNamespaces[cd.Tenant], cd.CaseID, row) {
					e.MissingCRs++
					if len(e.MissingCRSamples) < 5 {
						e.MissingCRSamples = append(e.MissingCRSamples, jobRunCRName(cd.CaseID, row.occurrenceKey))
					}
				}
			}
		}
	}

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
			_, err := client.GetJob(ctx, def.RevisionID)
			status := 200
			if err != nil {
				var aerr *APIError
				if errors.As(err, &aerr) {
					status = aerr.StatusCode
				} else {
					status = 0
				}
			}
			if status != 200 && status != http.StatusForbidden && status != http.StatusNotFound {
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

	caseIDs := make(map[string]bool, len(created))
	for _, def := range created {
		caseIDs[def.CaseID] = true
	}
	entries, err := collectFixtureJournal(ctx, fixtureURL, caseIDs)
	if err != nil {
		e.MissingSources = append(e.MissingSources, "ledger: "+err.Error())
	} else {
		e.Ledger = entries
	}

	return e, nil
}

// ledgerRun is one job_run_control_plane row the verifier needs: its phase,
// the revision that owns it, and the occurrence key that names its JobRun
// object.
type ledgerRun struct {
	id            int64
	revisionID    int64
	phase         string
	occurrenceKey string
	sourceUID     string
	trigger       string
}

// queryTenantRuns reads the run rows of one tenant's revisions inside a
// transaction scoped by app.tenant_id. The RLS guard is defense in depth --
// the query also filters by this run's revision ids -- but a reader that
// touched another tenant's rows would be a finding on its own.
func queryTenantRuns(ctx context.Context, odb *sql.DB, tenant string, revisions []int64) ([]ledgerRun, error) {
	rows := []ledgerRun{}
	err := withTenantTx(ctx, odb, tenant, func(tx *sql.Tx) error {
		result, err := tx.QueryContext(ctx, `
			SELECT id, revision_id, phase, occurrence_key, source_uid, trigger
			FROM job_run_control_plane
			WHERE revision_id = ANY($1)
		`, pq.Array(revisions))
		if err != nil {
			return err
		}
		defer func() { _ = result.Close() }()
		for result.Next() {
			var row ledgerRun
			if err := result.Scan(&row.id, &row.revisionID, &row.phase, &row.occurrenceKey, &row.sourceUID, &row.trigger); err != nil {
				return fmt.Errorf("scan run row: %w", err)
			}
			rows = append(rows, row)
		}
		return result.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("query runs for tenant %s: %w", tenant, err)
	}
	return rows, nil
}

// queryTenantAttempts reads the attempt trail of one tenant's runs. Each
// attempt is one Kubernetes Job.
func queryTenantAttempts(ctx context.Context, odb *sql.DB, tenant string, revisions []int64) ([]RunAttempt, error) {
	attempts := []RunAttempt{}
	err := withTenantTx(ctx, odb, tenant, func(tx *sql.Tx) error {
		result, err := tx.QueryContext(ctx, `
			SELECT a.run_id, a.attempt_number, a.phase
			FROM job_run_attempts_control_plane a
			JOIN job_run_control_plane r ON r.id = a.run_id
			WHERE r.revision_id = ANY($1)
		`, pq.Array(revisions))
		if err != nil {
			return err
		}
		defer func() { _ = result.Close() }()
		for result.Next() {
			var attempt RunAttempt
			var phase string
			if err := result.Scan(&attempt.RunID, &attempt.AttemptNumber, &phase); err != nil {
				return fmt.Errorf("scan attempt row: %w", err)
			}
			attempt.Succeeded = phase == phaseSucceeded
			attempts = append(attempts, attempt)
		}
		return result.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("query attempts for tenant %s: %w", tenant, err)
	}
	return attempts, nil
}

// Read all observed transitions of this run's revisions. An empty violation-only
// query cannot distinguish healthy transitions from an absent audit producer.
func queryTenantTransitions(ctx context.Context, odb *sql.DB, tenant string, revisions []int64) ([]Regression, error) {
	regressions := []Regression{}
	err := withTenantTx(ctx, odb, tenant, func(tx *sql.Tx) error {
		result, err := tx.QueryContext(ctx, `
			SELECT ae.resource_id, COALESCE(ae.diff->>'from_phase',''), COALESCE(ae.diff->>'to_phase','')
			FROM audit_events ae
			JOIN job_run_control_plane r ON r.id::text = ae.resource_id AND r.tenant_id = ae.tenant_id
			WHERE ae.event_type = 'job_run.status_changed' AND r.revision_id = ANY($1)
		`, pq.Array(revisions))
		if err != nil {
			return err
		}
		defer func() { _ = result.Close() }()
		for result.Next() {
			var r Regression
			if err := result.Scan(&r.ResourceID, &r.From, &r.To); err != nil {
				return fmt.Errorf("scan regression: %w", err)
			}
			regressions = append(regressions, r)
		}
		return result.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("query regressions for tenant %s: %w", tenant, err)
	}
	return regressions, nil
}

// withTenantTx runs fn in a transaction that first sets app.tenant_id to the
// tenant's ULID, the same guard the RLS policies bind to. The setting is
// transaction-local, so it cannot leak to the next borrower of the pooled
// connection. A tenant id that is not a well-formed ULID is refused before
// anything is sent: there is no tenant value to fall back to.
func withTenantTx(ctx context.Context, odb *sql.DB, tenant string, fn func(*sql.Tx) error) error {
	if err := validateTenantID(tenant); err != nil {
		return err
	}
	tx, err := odb.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenant); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("set tenant guard: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	// Read-only evidence: rollback rather than commit, so the transaction can
	// never be mistaken for a writer.
	if err := tx.Rollback(); err != nil {
		return err
	}
	return nil
}

// jobRunCRName mirrors the operator's JobRun naming rule: the scheduled job's
// name plus the shared complete occurrence digest.
func jobRunCRName(scheduledJobName, occurrenceKey string) string {
	return workloadsv1alpha1.RunObjectName(scheduledJobName, occurrenceKey)
}

type observedRunCR struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		OccurrenceKey string `json:"occurrenceKey"`
		RevisionID    int64  `json:"definitionRevision"`
		Trigger       string `json:"trigger"`
		Source        struct {
			UID string `json:"uid"`
		} `json:"scheduledJobRef"`
	} `json:"spec"`
}

func runCRPresent(objects map[string]observedRunCR, namespace, definition string, run ledgerRun) bool {
	for _, name := range workloadsv1alpha1.RunNameCandidates(definition, run.occurrenceKey) {
		obj, ok := objects[namespace+"/"+name]
		if ok && obj.Spec.OccurrenceKey == run.occurrenceKey && obj.Spec.RevisionID == run.revisionID && obj.Spec.Source.UID == run.sourceUID && obj.Spec.Trigger == run.trigger {
			return true
		}
	}
	return false
}

// Evidence must identify the object in its tenant namespace and verify its
// immutable occurrence spec; a matching short name alone is not ownership.
func collectRunCRs(ctx context.Context, tenantNamespaces map[string]string) (map[string]observedRunCR, error) {
	present := map[string]observedRunCR{}
	for _, namespace := range tenantNamespaces {
		out, err := exec.CommandContext(ctx, "kubectl", "get", "jobruns", "-n", namespace,
			"-o", "json").CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("jobruns:%s", namespace)
		}
		var list struct {
			Items []observedRunCR `json:"items"`
		}
		if err := json.Unmarshal(out, &list); err != nil {
			return nil, fmt.Errorf("jobruns:%s: %w", namespace, err)
		}
		for _, obj := range list.Items {
			present[namespace+"/"+obj.Metadata.Name] = obj
		}
	}
	return present, nil
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
	cfg, err = resolveRunTenants(cfg, *runRoot, *runID, false)
	if err != nil {
		return err
	}
	runDir := filepath.Join(*runRoot, *runID)
	tenantKeys, err := loadTenantKeys(filepath.Join(runDir, "tenant-keys.json"))
	if err != nil {
		return fmt.Errorf("load tenant keys: %w", err)
	}
	tenantNamespaces, err := loadTenantNamespaces(filepath.Join(runDir, "tenant-namespaces.json"))
	if err != nil {
		return fmt.Errorf("load tenant namespaces: %w", err)
	}
	created, err := loadCreatedDefinitions(filepath.Join(runDir, "created-definitions.json"))
	if err != nil {
		return fmt.Errorf("load created definitions: %w", err)
	}
	definitions, err := loadGeneratedDefinitions(filepath.Join(runDir, "generated", "definitions.json"))
	if err != nil {
		return fmt.Errorf("load generated definitions: %w", err)
	}
	if err := validateVerificationArtifacts(created, definitions, tenantKeys, tenantNamespaces); err != nil {
		return err
	}
	stats, err := loadRunStats(filepath.Join(runDir, "run-stats.json"))
	if err != nil {
		return fmt.Errorf("load run stats: %w (run the loadtest run command first)", err)
	}
	if stats.RunID != *runID || (stats.ConfigSHA256 != "" && stats.ConfigSHA256 != configSHA256(cfg)) {
		return fmt.Errorf("run stats do not match this run ID and execution config; use the original artifacts and config")
	}
	profile, seed, qualification, err := resolveRunIdentity(stats, cfg, *runID)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	evidence, err := CollectEvidence(ctx, *orbitjobDSN, *fixtureURL, *apiURL, tenantKeys, tenantNamespaces, created, definitions)
	if err != nil {
		return fmt.Errorf("collect evidence: %w", err)
	}

	evidence.Truncated = !stats.Completed
	journal, journalErr := readJournalSnapshot(ctx, *fixtureURL, *runID)
	journalCheck := CheckResult{ID: "journal-continuity", Status: VerdictInconclusive}
	if journalErr == nil {
		journalErr = verifyJournalContinuity(stats.JournalStart, journal)
	}
	if journalErr == nil {
		journalCheck.Status = VerdictPass
		journalCheck.Message = "fixture epoch and complete sequence interval preserved"
	} else {
		journalCheck.Message = journalErr.Error()
	}
	evidence.FaultChecks = faultEvidenceChecks(stats, cfg, collectFaultRecoveryChecks(ctx, runDir, cfg, stats, *orbitjobDSN, journal))
	evidence.ExecutionChecks = executionEvidenceChecks(stats, evidence)
	evidence.ExecutionChecks = append(evidence.ExecutionChecks, journalCheck)

	run := RunRecord{
		RunID:         *runID,
		Profile:       profile,
		Qualification: qualification,
		Seed:          seed,
		Commit:        stats.Commit,
		Dirty:         stats.Dirty,
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
	fmt.Printf("verify: verdict=%s reason=%s runs=%d qualified=%d in_flight=%d\n",
		result.Verdict, result.Reason, evidence.Runs, evidence.QualifiedRuns, evidence.InFlight)
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

func loadTenantNamespaces(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var namespaces map[string]string
	if err := json.Unmarshal(data, &namespaces); err != nil {
		return nil, err
	}
	return namespaces, nil
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

// resolveRunIdentity decides what a run record should say about the run.
//
// The run writes down what it was; a config the caller passes here is checked
// against that, never used to overwrite it. Getting this wrong is not cosmetic:
// verifying a smoke run without --config used to relabel it a standard
// qualification run, which is a false release record no later step can detect.
//
// Runs recorded before the profile was stored fall back to the passed config.
func resolveRunIdentity(stats RunStats, cfg Config, runID string) (string, string, bool, error) {
	if stats.Profile == "" {
		return cfg.Profile, cfg.Seed, cfg.Qualification, nil
	}
	if stats.Profile != cfg.Profile {
		return "", "", false, fmt.Errorf(
			"run %s recorded profile %q but --config is %q; pass the config the run used",
			runID, stats.Profile, cfg.Profile)
	}
	seed := stats.Seed
	if seed == "" {
		seed = cfg.Seed
	}
	return stats.Profile, seed, stats.Qualification, nil
}
