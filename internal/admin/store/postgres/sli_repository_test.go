package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

// adminSLIColumns is the projection the admin slis queries scan, in the
// order the scans depend on.
var adminSLIColumns = []string{
	"id", "tenant_id", "name", "description", "sli_type", "source_type",
	"source_config", "aggregation", "good_event_criteria", "version",
	"created_at", "updated_at",
}

func TestSLIReadRepository_Get(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	now := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	expectTenantTx(t, mock, "tenant-a")
	// An unscoped caller passes NULL for the group, which disables the group
	// predicate rather than matching an empty string against every row.
	mock.ExpectQuery(`SELECT (.+) FROM slis WHERE tenant_id = \$1 AND id = \$2 AND deleted_at IS NULL`).
		WithArgs("tenant-a", int64(3), nil).
		WillReturnRows(sqlmock.NewRows(adminSLIColumns).AddRow(
			3, "tenant-a", "checkout-availability", nil, "availability",
			"check_run", []byte(`{"check_id":7}`), "ratio",
			[]byte(`{"status":"success"}`), 1, now, now,
		))
	mock.ExpectCommit()

	snap, err := NewSLIReadRepository(db).Get(context.Background(), "tenant-a", "", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.ID != 3 || snap.Name != "checkout-availability" || snap.SLIType != "availability" {
		t.Fatalf("identity did not survive the scan: %+v", snap)
	}
	if snap.SourceConfig["check_id"] != float64(7) {
		t.Errorf("source_config = %v, want check_id 7", snap.SourceConfig)
	}
	if snap.GoodEventCriteria["status"] != "success" {
		t.Errorf("good_event_criteria = %v", snap.GoodEventCriteria)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSLIReadRepository_Get_ScopedToAResourceGroup(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	now := time.Now()
	expectTenantTx(t, mock, "tenant-a")
	// A group-scoped key carries its group: the query matches only rows the
	// group owns.
	mock.ExpectQuery(`SELECT (.+) FROM slis WHERE tenant_id = \$1 AND id = \$2 AND deleted_at IS NULL`).
		WithArgs("tenant-a", int64(3), "rg-1").
		WillReturnRows(sqlmock.NewRows(adminSLIColumns).AddRow(
			3, "tenant-a", "checkout-availability", nil, "availability",
			"check_run", []byte(`{"check_id":7}`), "ratio",
			[]byte(`{"status":"success"}`), 1, now, now,
		))
	mock.ExpectCommit()

	if _, err := NewSLIReadRepository(db).Get(context.Background(), "tenant-a", "rg-1", 3); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSLIReadRepository_Get_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT (.+) FROM slis`).
		WithArgs("tenant-a", int64(99), nil).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	_, err = NewSLIReadRepository(db).Get(context.Background(), "tenant-a", "", 99)
	assertNotFound(t, err, "sli")
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSLIReadRepository_Get_DBError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT (.+) FROM slis`).
		WithArgs("tenant-a", int64(3), nil).
		WillReturnError(errors.New("db down"))
	mock.ExpectRollback()

	if _, err := NewSLIReadRepository(db).Get(context.Background(), "tenant-a", "", 3); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSLIReadRepository_List(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	now := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	expectTenantTx(t, mock, "tenant-a")
	// The group predicate sits in the count too, so the total only counts rows
	// the caller can see.
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM slis`).
		WithArgs("tenant-a", nil).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectQuery(`SELECT (.+) FROM slis WHERE tenant_id = \$1 AND deleted_at IS NULL AND \(\$2::text IS NULL OR resource_group_id = \$2\) ORDER BY id DESC LIMIT \$3 OFFSET \$4`).
		WithArgs("tenant-a", nil, 50, 0).
		WillReturnRows(sqlmock.NewRows(adminSLIColumns).
			AddRow(4, "tenant-a", "signup-availability", nil, "availability",
				"check_run", []byte(`{"check_id":8}`), "ratio",
				[]byte(`{"status":"success"}`), 1, now, now).
			AddRow(3, "tenant-a", "checkout-availability", nil, "availability",
				"check_run", []byte(`{"check_id":7}`), "ratio",
				[]byte(`{"status":"success"}`), 1, now, now))
	mock.ExpectCommit()

	slis, total, err := NewSLIReadRepository(db).List(context.Background(), "tenant-a", "", 50, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(slis) != 2 || slis[0].Name != "signup-availability" || slis[1].Name != "checkout-availability" {
		t.Fatalf("slis = %+v, want newest id first", slis)
	}
	if slis[0].SourceConfig["check_id"] != float64(8) {
		t.Errorf("slis[0].source_config = %v", slis[0].SourceConfig)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSLIReadRepository_List_ScopedTotal(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM slis`).
		WithArgs("tenant-a", "rg-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT (.+) FROM slis`).
		WithArgs("tenant-a", "rg-1", 50, 0).
		WillReturnRows(sqlmock.NewRows(adminSLIColumns))
	mock.ExpectCommit()

	slis, total, err := NewSLIReadRepository(db).List(context.Background(), "tenant-a", "rg-1", 50, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 0 || len(slis) != 0 {
		t.Fatalf("total=%d len=%d, want 0 and 0", total, len(slis))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSLIReadRepository_List_CountError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	expectTenantTx(t, mock, "tenant-a")
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM slis`).
		WithArgs("tenant-a", nil).
		WillReturnError(errors.New("count failed"))
	mock.ExpectRollback()

	if _, _, err := NewSLIReadRepository(db).List(context.Background(), "tenant-a", "", 50, 0); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}
