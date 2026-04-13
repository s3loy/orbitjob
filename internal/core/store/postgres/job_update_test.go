//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	domainjob "orbitjob/internal/core/domain/job"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/platform/postgrestest"
)

func TestJobRepository_Update(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewJobRepository(db)

	now := time.Now().UTC().Truncate(time.Second)
	cronExpr := "*/15 * * * *"
	partitionKey := "tenant-update:batch"
	jobID := seedJob(t, db, seedJobInput{
		Name:        "old-name",
		TenantID:    "tenant-update",
		Priority:    1,
		TriggerType: domainjob.TriggerTypeManual,
		Timezone:    "UTC",
		HandlerType: "http",
	})

	spec, err := domainjob.NormalizeUpdate(now, domainjob.UpdateInput{
		ID:                   jobID,
		TenantID:             "tenant-update",
		Version:              1,
		Name:                 "nightly-report",
		Priority:             9,
		PartitionKey:         &partitionKey,
		TriggerType:          domainjob.TriggerTypeCron,
		CronExpr:             &cronExpr,
		Timezone:             "Asia/Shanghai",
		HandlerType:          "http",
		HandlerPayload:       map[string]any{"url": "https://example.com/hook"},
		TimeoutSec:           120,
		RetryLimit:           3,
		RetryBackoffSec:      10,
		RetryBackoffStrategy: domainjob.RetryBackoffExponential,
		ConcurrencyPolicy:    domainjob.ConcurrencyForbid,
		MisfirePolicy:        domainjob.MisfireFireNow,
	})
	if err != nil {
		t.Fatalf("NormalizeUpdate() error = %v", err)
	}

	out, err := repo.Update(context.Background(), spec, "control-plane-user")
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if out.ID != jobID {
		t.Fatalf("expected id=%d, got %d", jobID, out.ID)
	}
	if out.Name != "nightly-report" {
		t.Fatalf("expected name=%q, got %q", "nightly-report", out.Name)
	}
	if out.Version != 2 {
		t.Fatalf("expected version=%d, got %d", 2, out.Version)
	}
	if out.NextRunAt == nil {
		t.Fatalf("expected next_run_at to be set")
	}

	var (
		storedName         string
		storedVersion      int
		storedPriority     int
		storedPartitionKey sql.NullString
		storedCronExpr     sql.NullString
		storedTimezone     string
		storedHandlerType  string
		storedAuditCount   int
	)

	err = db.QueryRowContext(context.Background(), `
		SELECT name, version, priority, partition_key, cron_expr, timezone, handler_type
		FROM jobs
		WHERE tenant_id = $1 AND id = $2
	`, "tenant-update", jobID).Scan(
		&storedName,
		&storedVersion,
		&storedPriority,
		&storedPartitionKey,
		&storedCronExpr,
		&storedTimezone,
		&storedHandlerType,
	)
	if err != nil {
		t.Fatalf("reload job: %v", err)
	}
	if storedName != "nightly-report" {
		t.Fatalf("expected stored name=%q, got %q", "nightly-report", storedName)
	}
	if storedVersion != 2 {
		t.Fatalf("expected stored version=%d, got %d", 2, storedVersion)
	}
	if storedPriority != 9 {
		t.Fatalf("expected stored priority=%d, got %d", 9, storedPriority)
	}
	if !storedPartitionKey.Valid || storedPartitionKey.String != partitionKey {
		t.Fatalf("expected stored partition_key=%q, got %+v", partitionKey, storedPartitionKey)
	}
	if !storedCronExpr.Valid || storedCronExpr.String != cronExpr {
		t.Fatalf("expected stored cron_expr=%q, got %+v", cronExpr, storedCronExpr)
	}
	if storedTimezone != "Asia/Shanghai" {
		t.Fatalf("expected stored timezone=%q, got %q", "Asia/Shanghai", storedTimezone)
	}
	if storedHandlerType != "http" {
		t.Fatalf("expected stored handler_type=%q, got %q", "http", storedHandlerType)
	}

	err = db.QueryRowContext(context.Background(), `
		SELECT count(*)
		FROM job_change_audits
		WHERE tenant_id = $1 AND job_id = $2 AND action = 'update' AND changed_by = $3
	`, "tenant-update", jobID, "control-plane-user").Scan(&storedAuditCount)
	if err != nil {
		t.Fatalf("count audits: %v", err)
	}
	if storedAuditCount != 1 {
		t.Fatalf("expected 1 audit row, got %d", storedAuditCount)
	}
}

func TestJobRepository_UpdateVersionConflict(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewJobRepository(db)

	now := time.Now().UTC()
	jobID := seedJob(t, db, seedJobInput{
		Name:        "demo-job",
		TenantID:    "tenant-conflict",
		TriggerType: domainjob.TriggerTypeManual,
		Timezone:    "UTC",
		HandlerType: "http",
	})

	spec, err := domainjob.NormalizeUpdate(now, domainjob.UpdateInput{
		ID:          jobID,
		TenantID:    "tenant-conflict",
		Version:     7,
		Name:        "manual-report",
		TriggerType: domainjob.TriggerTypeManual,
		HandlerType: "http",
	})
	if err != nil {
		t.Fatalf("NormalizeUpdate() error = %v", err)
	}

	_, err = repo.Update(context.Background(), spec, "control-plane-user")
	if err == nil {
		t.Fatalf("expected conflict error, got nil")
	}

	var conflictErr *resource.ConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("expected ConflictError, got %T (%v)", err, err)
	}
	if conflictErr.Field != "version" {
		t.Fatalf("expected field=%q, got %q", "version", conflictErr.Field)
	}
}

func TestJobRepository_UpdateRollsBackWhenAuditInsertFails(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewJobRepository(db)

	now := time.Now().UTC()
	jobID := seedJob(t, db, seedJobInput{
		Name:        "demo-job",
		TenantID:    "tenant-audit",
		TriggerType: domainjob.TriggerTypeManual,
		Timezone:    "UTC",
		HandlerType: "http",
	})

	spec, err := domainjob.NormalizeUpdate(now, domainjob.UpdateInput{
		ID:          jobID,
		TenantID:    "tenant-audit",
		Version:     1,
		Name:        "manual-report",
		TriggerType: domainjob.TriggerTypeManual,
		HandlerType: "http",
	})
	if err != nil {
		t.Fatalf("NormalizeUpdate() error = %v", err)
	}

	_, err = repo.Update(context.Background(), spec, strings.Repeat("x", 129))
	if err == nil {
		t.Fatalf("expected audit insert failure, got nil")
	}

	var (
		status  string
		version int
		name    string
	)

	err = db.QueryRowContext(context.Background(), `
		SELECT name, status, version
		FROM jobs
		WHERE tenant_id = $1 AND id = $2
	`, "tenant-audit", jobID).Scan(&name, &status, &version)
	if err != nil {
		t.Fatalf("reload job: %v", err)
	}
	if name != "demo-job" || status != "active" || version != 1 {
		t.Fatalf("expected rollback to preserve demo-job/active/version=1, got %s/%s/%d", name, status, version)
	}
}

type seedJobInput struct {
	Name        string
	TenantID    string
	Priority    int
	TriggerType string
	CronExpr    *string
	Timezone    string
	HandlerType string
}

func seedJob(t *testing.T, db *sql.DB, in seedJobInput) int64 {
	t.Helper()

	var id int64
	err := db.QueryRowContext(context.Background(), `
		INSERT INTO jobs (
			name,
			tenant_id,
			priority,
			trigger_type,
			cron_expr,
			timezone,
			handler_type
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`,
		in.Name,
		in.TenantID,
		in.Priority,
		in.TriggerType,
		in.CronExpr,
		in.Timezone,
		in.HandlerType,
	).Scan(&id)
	if err != nil {
		t.Fatalf("seed job: %v", err)
	}

	return id
}
