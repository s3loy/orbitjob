//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	domainjob "orbitjob/internal/core/domain/job"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/platform/postgrestest"
)

func TestJobRepository_Pause(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewJobRepository(db)
	jobID := seedJob(t, db, seedJobInput{
		Name:        "pause-job",
		TenantID:    "tenant-pause",
		TriggerType: domainjob.TriggerTypeManual,
		Timezone:    "UTC",
		HandlerType: "http",
	})

	out, err := repo.ChangeStatus(context.Background(), domainjob.ChangeStatusSpec{
		ID:            jobID,
		TenantID:      "tenant-pause",
		Version:       1,
		CurrentStatus: domainjob.StatusActive,
		NextStatus:    domainjob.StatusPaused,
		Action:        domainjob.ActionPause,
	}, "control-plane-user")
	if err != nil {
		t.Fatalf("ChangeStatus() error = %v", err)
	}
	if out.Status != domainjob.StatusPaused {
		t.Fatalf("expected status=%q, got %q", domainjob.StatusPaused, out.Status)
	}
	if out.Version != 2 {
		t.Fatalf("expected version=%d, got %d", 2, out.Version)
	}

	var storedStatus string
	var storedVersion int
	var storedAuditCount int
	err = db.QueryRowContext(context.Background(), `
		SELECT status, version
		FROM jobs
		WHERE tenant_id = $1 AND id = $2
	`, "tenant-pause", jobID).Scan(&storedStatus, &storedVersion)
	if err != nil {
		t.Fatalf("reload job: %v", err)
	}
	if storedStatus != domainjob.StatusPaused {
		t.Fatalf("expected stored status=%q, got %q", domainjob.StatusPaused, storedStatus)
	}
	if storedVersion != 2 {
		t.Fatalf("expected stored version=%d, got %d", 2, storedVersion)
	}

	err = db.QueryRowContext(context.Background(), `
		SELECT count(*)
		FROM job_change_audits
		WHERE tenant_id = $1 AND job_id = $2 AND action = 'pause' AND changed_by = $3
	`, "tenant-pause", jobID, "control-plane-user").Scan(&storedAuditCount)
	if err != nil {
		t.Fatalf("count audits: %v", err)
	}
	if storedAuditCount != 1 {
		t.Fatalf("expected 1 audit row, got %d", storedAuditCount)
	}
}

func TestJobRepository_Resume(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewJobRepository(db)
	jobID := seedJob(t, db, seedJobInput{
		Name:        "resume-job",
		TenantID:    "tenant-resume",
		TriggerType: domainjob.TriggerTypeManual,
		Timezone:    "UTC",
		HandlerType: "http",
	})

	_, err := db.ExecContext(context.Background(), `
		UPDATE jobs
		SET status = 'paused', version = 3
		WHERE tenant_id = $1 AND id = $2
	`, "tenant-resume", jobID)
	if err != nil {
		t.Fatalf("seed paused job: %v", err)
	}

	out, err := repo.ChangeStatus(context.Background(), domainjob.ChangeStatusSpec{
		ID:            jobID,
		TenantID:      "tenant-resume",
		Version:       3,
		CurrentStatus: domainjob.StatusPaused,
		NextStatus:    domainjob.StatusActive,
		Action:        domainjob.ActionResume,
	}, "control-plane-user")
	if err != nil {
		t.Fatalf("ChangeStatus() error = %v", err)
	}
	if out.Status != domainjob.StatusActive {
		t.Fatalf("expected status=%q, got %q", domainjob.StatusActive, out.Status)
	}
	if out.Version != 4 {
		t.Fatalf("expected version=%d, got %d", 4, out.Version)
	}
}

func TestJobRepository_ChangeStatusVersionConflict(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewJobRepository(db)
	jobID := seedJob(t, db, seedJobInput{
		Name:        "conflict-job",
		TenantID:    "tenant-status-conflict",
		TriggerType: domainjob.TriggerTypeManual,
		Timezone:    "UTC",
		HandlerType: "http",
	})

	_, err := repo.ChangeStatus(context.Background(), domainjob.ChangeStatusSpec{
		ID:            jobID,
		TenantID:      "tenant-status-conflict",
		Version:       7,
		CurrentStatus: domainjob.StatusActive,
		NextStatus:    domainjob.StatusPaused,
		Action:        domainjob.ActionPause,
	}, "control-plane-user")
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}

	var conflictErr *resource.ConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("expected ConflictError, got %T (%v)", err, err)
	}
	if conflictErr.Field != "version" {
		t.Fatalf("expected field=%q, got %q", "version", conflictErr.Field)
	}
}

func TestJobRepository_ChangeStatusRollsBackWhenAuditInsertFails(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewJobRepository(db)
	jobID := seedJob(t, db, seedJobInput{
		Name:        "audit-job",
		TenantID:    "tenant-status-audit",
		TriggerType: domainjob.TriggerTypeManual,
		Timezone:    "UTC",
		HandlerType: "http",
	})

	_, err := repo.ChangeStatus(context.Background(), domainjob.ChangeStatusSpec{
		ID:            jobID,
		TenantID:      "tenant-status-audit",
		Version:       1,
		CurrentStatus: domainjob.StatusActive,
		NextStatus:    domainjob.StatusPaused,
		Action:        domainjob.ActionPause,
	}, strings.Repeat("x", 129))
	if err == nil {
		t.Fatal("expected audit insert failure, got nil")
	}

	var status string
	var version int
	err = db.QueryRowContext(context.Background(), `
		SELECT status, version
		FROM jobs
		WHERE tenant_id = $1 AND id = $2
	`, "tenant-status-audit", jobID).Scan(&status, &version)
	if err != nil {
		t.Fatalf("reload job: %v", err)
	}
	if status != domainjob.StatusActive || version != 1 {
		t.Fatalf("expected rollback to preserve active/version=1, got %s/%d", status, version)
	}
}

func TestJobRepository_ChangeStatusPreservesNextRunAt(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewJobRepository(db)
	cronExpr := "*/5 * * * *"
	jobID := seedJob(t, db, seedJobInput{
		Name:        "cron-job",
		TenantID:    "tenant-next-run",
		TriggerType: domainjob.TriggerTypeCron,
		CronExpr:    &cronExpr,
		Timezone:    "UTC",
		HandlerType: "http",
	})

	var nextRunAt sql.NullTime
	err := db.QueryRowContext(context.Background(), `
		SELECT next_run_at
		FROM jobs
		WHERE tenant_id = $1 AND id = $2
	`, "tenant-next-run", jobID).Scan(&nextRunAt)
	if err != nil {
		t.Fatalf("load next_run_at: %v", err)
	}

	out, err := repo.ChangeStatus(context.Background(), domainjob.ChangeStatusSpec{
		ID:            jobID,
		TenantID:      "tenant-next-run",
		Version:       1,
		CurrentStatus: domainjob.StatusActive,
		NextStatus:    domainjob.StatusPaused,
		Action:        domainjob.ActionPause,
	}, "control-plane-user")
	if err != nil {
		t.Fatalf("ChangeStatus() error = %v", err)
	}
	if nextRunAt.Valid && out.NextRunAt == nil {
		t.Fatal("expected next_run_at to be preserved")
	}
}
