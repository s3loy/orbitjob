package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

// adminSLOColumns is the projection the admin slos queries scan, in the
// order the scans depend on.
var adminSLOColumns = []string{
	"id", "tenant_id", "name", "description", "sli_id", "target",
	"window_type", "window_duration", "alert_fast_burn_rate",
	"alert_slow_burn_rate", "status", "version", "created_at", "updated_at",
}

func TestSLOReadRepository_Get(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	now := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	expectTenantTx(t, mock, "tenant-a")
	// window_duration travels as seconds and must land as a duration.
	mock.ExpectQuery(`SELECT (.+) FROM slos WHERE tenant_id = \$1 AND id = \$2 AND deleted_at IS NULL`).
		WithArgs("tenant-a", int64(3), nil).
		WillReturnRows(sqlmock.NewRows(adminSLOColumns).AddRow(
			3, "tenant-a", "checkout-slo", nil, 7, 0.99,
			"rolling", 86400, 14.4, 6, "active", 2, now, now,
		))
	mock.ExpectCommit()

	snap, err := NewSLOReadRepository(db).Get(context.Background(), "tenant-a", "", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.ID != 3 || snap.Name != "checkout-slo" || snap.SLIID != 7 {
		t.Fatalf("identity did not survive the scan: %+v", snap)
	}
	if snap.Target != 0.99 || snap.AlertFastBurnRate != 14.4 || snap.AlertSlowBurnRate != 6 {
		t.Fatalf("targets did not survive the scan: %+v", snap)
	}
	if snap.WindowDuration != 24*time.Hour {
		t.Errorf("window_duration = %v, want 24h", snap.WindowDuration)
	}
	if snap.Status != "active" || snap.Version != 2 {
		t.Errorf("status = %q version = %d", snap.Status, snap.Version)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSLOReadRepository_Get_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT (.+) FROM slos`).
		WithArgs("tenant-a", int64(99), nil).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	_, err = NewSLOReadRepository(db).Get(context.Background(), "tenant-a", "", 99)
	assertNotFound(t, err, "slo")
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSLOReadRepository_Get_DBError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT (.+) FROM slos`).
		WithArgs("tenant-a", int64(3), nil).
		WillReturnError(errors.New("db down"))
	mock.ExpectRollback()

	if _, err := NewSLOReadRepository(db).Get(context.Background(), "tenant-a", "", 3); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSLOReadRepository_List(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	now := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM slos`).
		WithArgs("tenant-a", nil).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectQuery(`SELECT (.+) FROM slos WHERE tenant_id = \$1 AND deleted_at IS NULL AND \(\$2::text IS NULL OR resource_group_id = \$2\) ORDER BY id DESC LIMIT \$3 OFFSET \$4`).
		WithArgs("tenant-a", nil, 50, 0).
		WillReturnRows(sqlmock.NewRows(adminSLOColumns).
			AddRow(4, "tenant-a", "signup-slo", nil, 8, 0.999,
				"calendar", 604800, 14.4, 6, "active", 1, now, now).
			AddRow(3, "tenant-a", "checkout-slo", nil, 7, 0.99,
				"rolling", 3600, 14.4, 6, "active", 2, now, now))
	mock.ExpectCommit()

	slos, total, err := NewSLOReadRepository(db).List(context.Background(), "tenant-a", "", 50, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(slos) != 2 || slos[0].Name != "signup-slo" || slos[1].Name != "checkout-slo" {
		t.Fatalf("slos = %+v, want newest id first", slos)
	}
	if slos[0].WindowDuration != 7*24*time.Hour || slos[1].WindowDuration != time.Hour {
		t.Fatalf("window durations = %v, %v", slos[0].WindowDuration, slos[1].WindowDuration)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSLOReadRepository_List_Empty(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM slos`).
		WithArgs("tenant-a", "rg-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT (.+) FROM slos`).
		WithArgs("tenant-a", "rg-1", 50, 0).
		WillReturnRows(sqlmock.NewRows(adminSLOColumns))
	mock.ExpectCommit()

	slos, total, err := NewSLOReadRepository(db).List(context.Background(), "tenant-a", "rg-1", 50, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 0 || len(slos) != 0 {
		t.Fatalf("total=%d len=%d, want 0 and 0", total, len(slos))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSLOReadRepository_List_RowsError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	now := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM slos`).
		WithArgs("tenant-a", nil).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`SELECT (.+) FROM slos`).
		WithArgs("tenant-a", nil, 50, 0).
		WillReturnRows(sqlmock.NewRows(adminSLOColumns).
			AddRow(3, "tenant-a", "checkout-slo", nil, 7, 0.99,
				"rolling", 3600, 14.4, 6, "active", 2, now, now).
			RowError(0, errors.New("iteration failure")))
	mock.ExpectRollback()

	if _, _, err := NewSLOReadRepository(db).List(context.Background(), "tenant-a", "", 50, 0); err == nil {
		t.Fatal("expected iteration error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}
