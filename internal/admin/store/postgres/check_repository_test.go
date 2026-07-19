package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	checkquery "orbitjob/internal/admin/app/check/query"
	"orbitjob/internal/domain/resource"
)

func TestCheckRepository_Get(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewCheckRepository(db)
	checkConfigJSON := []byte(`{"url":"https://example.com","method":"GET"}`)
	assertionJSON := []byte(`[{"metric":"status","operator":"==","threshold":200,"severity":"critical"}]`)
	labelsJSON := []byte(`{"env":"test"}`)
	now := time.Now()

	rows := sqlmock.NewRows([]string{
		"id", "name", "description", "tenant_id", "status", "check_type", "check_config", "assertion_rules",
		"schedule_type", "cron_expr", "interval_sec", "timezone", "timeout_sec", "retry_limit",
		"priority", "labels", "next_run_at", "version", "created_at", "updated_at",
	}).AddRow(
		int64(1), "http-check", "test desc", "default", "active", "http_health",
		checkConfigJSON, assertionJSON,
		"interval", nil, nil, "UTC", 10, 3,
		5, labelsJSON, nil, 1, now, now,
	)

	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config`).WithArgs("default").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT (.+) FROM checks`).
		WithArgs("default", int64(1)).
		WillReturnRows(rows)
	mock.ExpectCommit()

	snap, err := repo.Get(context.Background(), "default", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.ID != 1 {
		t.Errorf("expected id 1, got %d", snap.ID)
	}
	if snap.CheckConfig["url"] != "https://example.com" {
		t.Errorf("expected url 'https://example.com', got %v", snap.CheckConfig["url"])
	}
	if len(snap.AssertionRules) != 1 {
		t.Fatalf("expected 1 assertion rule, got %d", len(snap.AssertionRules))
	}
	if snap.AssertionRules[0].Metric != "status" {
		t.Errorf("expected metric 'status', got %q", snap.AssertionRules[0].Metric)
	}
	if snap.Labels["env"] != "test" {
		t.Errorf("expected env 'test', got %v", snap.Labels["env"])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestCheckRepository_Get_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewCheckRepository(db)
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config`).WithArgs("default").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT (.+) FROM checks`).
		WithArgs("default", int64(999)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	_, err = repo.Get(context.Background(), "default", 999)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var notFound *resource.NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *resource.NotFoundError, got %T: %v", err, err)
	}
	if notFound.Resource != "check" {
		t.Errorf("expected resource 'check', got %q", notFound.Resource)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestCheckRepository_Get_DBError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewCheckRepository(db)
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config`).WithArgs("default").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT (.+) FROM checks`).
		WithArgs("default", int64(1)).
		WillReturnError(errors.New("db down"))
	mock.ExpectRollback()

	_, err = repo.Get(context.Background(), "default", 1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestCheckRepository_List(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewCheckRepository(db)
	now := time.Now()

	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config`).WithArgs("default").WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM checks`).
		WithArgs("default", nil).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	rows := sqlmock.NewRows([]string{
		"id", "name", "description", "tenant_id", "status", "check_type",
		"schedule_type", "next_run_at", "version", "created_at",
	}).AddRow(int64(1), "http-check", "desc", "default", "active", "http_health",
		"interval", nil, 1, now)

	mock.ExpectQuery(`SELECT (.+) FROM checks`).
		WithArgs("default", nil, 20, 0).
		WillReturnRows(rows)
	mock.ExpectCommit()

	items, total, err := repo.List(context.Background(), checkquery.ListChecksInput{
		TenantID: "default",
		Limit:    20,
		Offset:   0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 {
		t.Errorf("expected total 1, got %d", total)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Name != "http-check" {
		t.Errorf("expected name 'http-check', got %q", items[0].Name)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}
