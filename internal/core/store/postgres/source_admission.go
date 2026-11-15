package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/workflow"
	"orbitjob/internal/domain/resource"
)

var admissionOutcomes = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "orbitjob_source_admission_total", Help: "Source admission decisions by bounded outcome (created, existing, blocked, rejected, error).",
}, []string{"outcome"})
var admissionLockWait = promauto.NewHistogram(prometheus.HistogramOpts{
	Name: "orbitjob_source_admission_lock_wait_seconds", Help: "Time waiting for the transaction-scoped source admission lock in seconds.",
})

// Keep this identity identical to revision activation, including its mode. Do
// not include a revision ID: old and new payloads share the source's admission.
func sourceLockKey(tenant, mode, uid string) string { return tenant + "/" + mode + "/" + uid }
func lockSource(ctx context.Context, tx *sql.Tx, tenant, mode, uid string) error {
	started := time.Now()
	_, err := tx.ExecContext(ctx, lockRevisionSQL, sourceLockKey(tenant, mode, uid))
	admissionLockWait.Observe(time.Since(started).Seconds())
	return err
}

// Each protected read is a separate statement after acquiring the lock. A
// Repeatable Read snapshot or a same-statement CTE can miss the prior winner.
func withAdmissionTx(ctx context.Context, db *sql.DB, tenant string, fn func(context.Context, *sql.Tx) error) error {
	if db == nil {
		return fmt.Errorf("nil database")
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = WithTenant(ctx, tx, tenant); err != nil {
		return err
	}
	if err = fn(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func lockAdmissionSource(ctx context.Context, tx *sql.Tx, tenant, uid string, revision int64, requiredMode string) (string, error) {
	var mode, storedUID string
	err := tx.QueryRowContext(ctx, `SELECT source_mode, source_uid FROM job_definition_revisions WHERE id=$1 AND tenant_id=$2`, revision, tenant).Scan(&mode, &storedUID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", &resource.NotFoundError{Resource: "job_definition_revision", ID: revision}
	}
	if err != nil {
		return "", err
	}
	if storedUID != uid || (requiredMode != "" && mode != requiredMode) {
		return "", fmt.Errorf("pinned revision does not belong to the requested source")
	}
	if err = lockSource(ctx, tx, tenant, mode, uid); err != nil {
		return "", err
	}
	return mode, nil
}

func sourcePolicy(ctx context.Context, tx *sql.Tx, tenant, mode, uid string) (string, bool, error) {
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT normalized_spec::text FROM job_definition_revisions WHERE tenant_id=$1 AND source_mode=$2 AND source_uid=$3 AND is_active`, tenant, mode, uid).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, &jobrun.AdmissionBlockedError{Reason: "inactive_source"}
	}
	if err != nil {
		return "", false, err
	}
	var spec struct {
		ConcurrencyPolicy string `json:"concurrencyPolicy"`
		Suspend           bool   `json:"suspend"`
	}
	if err = json.Unmarshal([]byte(raw), &spec); err != nil {
		return "", false, fmt.Errorf("decode active admission policy: %w", err)
	}
	policy := spec.ConcurrencyPolicy
	if mode == "check" || mode == "function" {
		policy = "Allow"
	}
	if policy == "" {
		policy = "Allow"
	}
	if policy != "Allow" && policy != "Forbid" && (policy != "Replace" || mode != sourceModeKubernetes) {
		return "", false, fmt.Errorf("unsupported active concurrency policy %q", policy)
	}
	return policy, spec.Suspend, nil
}

func checkSourceAdmission(ctx context.Context, tx *sql.Tx, tenant, mode, uid string, trigger jobrun.Trigger) error {
	if err := checkTenantExecutionActive(ctx, tx); err != nil {
		return err
	}
	if err := checkSourceControls(ctx, tx, tenant, mode, uid, trigger, true); err != nil {
		return err
	}
	policy, suspend, err := sourcePolicy(ctx, tx, tenant, mode, uid)
	if err != nil {
		return err
	}
	if suspend && trigger == jobrun.Schedule {
		return &jobrun.AdmissionBlockedError{Policy: policy, Reason: "suspended"}
	}
	if policy != "Forbid" && (policy != "Replace" || trigger != jobrun.Schedule) {
		return nil
	}
	var count int
	query := countOpenRunsSQL
	phases := terminalPhases()
	if mode == workflow.SourceModeWorkflow {
		query = `SELECT count(*) FROM workflow_run_control_plane WHERE tenant_id=$1 AND source_uid=$2 AND phase <> ALL($3)`
		phases = terminalWorkflowPhases()
	}
	if err = tx.QueryRowContext(ctx, query, tenant, uid, phases).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	blocked := &jobrun.AdmissionBlockedError{Policy: policy, Reason: "concurrency"}
	if policy == "Replace" {
		rows, err := tx.QueryContext(ctx, openRunsSQL, tenant, uid, terminalPhases())
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var run jobrun.OpenRun
			var at sql.NullTime
			if err = rows.Scan(&run.ID, &run.RevisionID, &run.OccurrenceKey, &run.Phase, &at, &run.Trigger, &run.Actor); err != nil {
				return err
			}
			if at.Valid {
				run.ScheduledFor = at.Time
			}
			blocked.CancelTargets = append(blocked.CancelTargets, run)
		}
		if err = rows.Err(); err != nil {
			return err
		}
	}
	return blocked
}

func recordAdmission(ctx context.Context, tenant, source string, created bool, err error) {
	outcome := "existing"
	if created {
		outcome = "created"
	}
	if err != nil {
		outcome = "error"
		var blocked *jobrun.AdmissionBlockedError
		if errors.As(err, &blocked) {
			outcome = "blocked"
			slog.InfoContext(ctx, "source admission blocked", "tenant", tenant, "source", source, "policy", blocked.Policy, "reason", blocked.Reason)
		}
	}
	admissionOutcomes.WithLabelValues(outcome).Inc()
}
