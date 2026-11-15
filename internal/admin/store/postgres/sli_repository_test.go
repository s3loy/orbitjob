package postgres

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"orbitjob/internal/core/domain/sli"
	"orbitjob/internal/platform/scan"
)

// adminSLIColumns is the projection the admin slis queries scan, in the
// order the scans depend on.
var adminSLIColumns = []string{
	"id", "tenant_id", "name", "description", "sli_type", "source_type",
	"source_config", "aggregation", "good_event_criteria", "version",
	"created_at", "updated_at",
	"configuration_version", "statistics_semantics", "effective_from", "last_event_at", "last_sample_at", "last_observed_at",
}

const adminSLIReadJoins = `FROM slis LEFT JOIN sli_configuration_versions v ON v.tenant_id=slis.tenant_id AND v.sli_id=slis.id AND v.configuration_version=1 LEFT JOIN sli_observation_state o ON o.tenant_id=slis.tenant_id AND o.sli_id=slis.id`

func TestSLIReadRepository_Get(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	now := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	activeFrom := now.Add(-time.Hour)
	lastEventAt := now.Add(-time.Minute)
	lastSampleAt := lastEventAt.Add(-time.Second)
	expectTenantTx(t, mock, adminSLIFixtureTenantID)
	// An unscoped caller passes NULL for the group, which disables the group
	// predicate rather than matching an empty string against every row.
	mock.ExpectQuery(`SELECT (.+) `+regexp.QuoteMeta(adminSLIReadJoins+` WHERE slis.tenant_id = $1 AND id = $2 AND deleted_at IS NULL AND ($3::text IS NULL OR resource_group_id = $3)`)).
		WithArgs(adminSLIFixtureTenantID, int64(3), nil).
		WillReturnRows(sqlmock.NewRows(adminSLIColumns).AddRow(
			3, adminSLIFixtureTenantID, "checkout-availability", nil, "availability",
			"job_run", []byte(`{"source_uid":"check-7"}`), "ratio",
			[]byte(`{"status":"success"}`), 1, activeFrom, activeFrom,
			1, sli.EventTime, activeFrom, lastEventAt, lastSampleAt, now,
		))
	mock.ExpectCommit()

	snap, err := NewSLIReadRepository(db).Get(context.Background(), adminSLIFixtureTenantID, "", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.ID != 3 || snap.Name != "checkout-availability" || snap.SLIType != "availability" {
		t.Fatalf("identity did not survive the scan: %+v", snap)
	}
	if snap.SourceType != "job_run" || snap.SourceConfig["source_uid"] != "check-7" {
		t.Errorf("source = %q %v, want job_run/check-7", snap.SourceType, snap.SourceConfig)
	}
	if snap.GoodEventCriteria["status"] != "success" {
		t.Errorf("good_event_criteria = %v", snap.GoodEventCriteria)
	}
	if snap.ConfigurationVersion != 1 || snap.StatisticsSemantics != sli.EventTime || !snap.ActiveFrom.Equal(activeFrom) ||
		snap.LastEventAt == nil || !snap.LastEventAt.Equal(lastEventAt) || snap.LastSampleAt == nil || !snap.LastSampleAt.Equal(lastSampleAt) ||
		snap.LastObservedAt == nil || !snap.LastObservedAt.Equal(now) {
		t.Fatalf("measurement evidence did not survive the scan: %+v", snap)
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
	expectTenantTx(t, mock, adminSLIFixtureTenantID)
	// A group-scoped key carries its group: the query matches only rows the
	// group owns.
	mock.ExpectQuery(`SELECT (.+) `+regexp.QuoteMeta(adminSLIReadJoins+` WHERE slis.tenant_id = $1 AND id = $2 AND deleted_at IS NULL AND ($3::text IS NULL OR resource_group_id = $3)`)).
		WithArgs(adminSLIFixtureTenantID, int64(3), adminSLIFixtureGroupID).
		WillReturnRows(sqlmock.NewRows(adminSLIColumns).AddRow(
			3, adminSLIFixtureTenantID, "checkout-availability", nil, "availability",
			"job_run", []byte(`{"source_uid":"check-7"}`), "ratio",
			[]byte(`{"status":"success"}`), 1, now, now,
			1, sli.EventTime, now, nil, nil, nil,
		))
	mock.ExpectCommit()

	snap, err := NewSLIReadRepository(db).Get(context.Background(), adminSLIFixtureTenantID, adminSLIFixtureGroupID, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.ConfigurationVersion != 1 || snap.StatisticsSemantics != sli.EventTime || !snap.ActiveFrom.Equal(now) ||
		snap.LastEventAt != nil || snap.LastSampleAt != nil || snap.LastObservedAt != nil {
		t.Fatalf("unsampled SLI must retain missing observation timestamps: %+v", snap)
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

	expectTenantTx(t, mock, adminSLIFixtureTenantID)
	mock.ExpectQuery(`SELECT (.+) FROM slis`).
		WithArgs(adminSLIFixtureTenantID, int64(99), nil).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	_, err = NewSLIReadRepository(db).Get(context.Background(), adminSLIFixtureTenantID, "", 99)
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

	expectTenantTx(t, mock, adminSLIFixtureTenantID)
	mock.ExpectQuery(`SELECT (.+) FROM slis`).
		WithArgs(adminSLIFixtureTenantID, int64(3), nil).
		WillReturnError(errors.New("db down"))
	mock.ExpectRollback()

	if _, err := NewSLIReadRepository(db).Get(context.Background(), adminSLIFixtureTenantID, "", 3); err == nil {
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
	expectTenantTx(t, mock, adminSLIFixtureTenantID)
	// The group predicate sits in the count too, so the total only counts rows
	// the caller can see.
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM slis`).
		WithArgs(adminSLIFixtureTenantID, nil, "").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectQuery(`SELECT (.+) `+regexp.QuoteMeta(adminSLIReadJoins+` WHERE slis.tenant_id = $1 AND deleted_at IS NULL AND ($2::text IS NULL OR resource_group_id = $2)`)+`.* ORDER BY id DESC LIMIT \$3 OFFSET \$4`).
		WithArgs(adminSLIFixtureTenantID, nil, 50, 0, "").
		WillReturnRows(sqlmock.NewRows(adminSLIColumns).
			AddRow(4, adminSLIFixtureTenantID, "signup-availability", nil, "availability",
				"job_run", []byte(`{"source_uid":"check-8"}`), "ratio",
				[]byte(`{"status":"success"}`), 1, now, now, 1, "mixed", now, now, now, now).
			AddRow(3, adminSLIFixtureTenantID, "checkout-availability", nil, "availability",
				"job_run", []byte(`{"source_uid":"check-7"}`), "ratio",
				[]byte(`{"status":"success"}`), 1, now, now, 1, sli.EventTime, now, nil, nil, nil))
	mock.ExpectCommit()

	slis, total, err := NewSLIReadRepository(db).List(context.Background(), adminSLIFixtureTenantID, "", 50, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(slis) != 2 || slis[0].Name != "signup-availability" || slis[1].Name != "checkout-availability" {
		t.Fatalf("slis = %+v, want newest id first", slis)
	}
	if slis[0].SourceConfig["source_uid"] != "check-8" {
		t.Errorf("slis[0].source_config = %v", slis[0].SourceConfig)
	}
	if slis[0].StatisticsSemantics != "mixed" || slis[0].LastEventAt == nil || !slis[0].LastEventAt.Equal(now) ||
		slis[0].LastSampleAt == nil || !slis[0].LastSampleAt.Equal(now) || slis[0].LastObservedAt == nil || !slis[0].LastObservedAt.Equal(now) {
		t.Fatalf("list lost observation metadata: %+v", slis[0])
	}
	if slis[1].LastEventAt != nil || slis[1].LastSampleAt != nil || slis[1].LastObservedAt != nil {
		t.Fatalf("list invented observation timestamps: %+v", slis[1])
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

	expectTenantTx(t, mock, adminSLIFixtureTenantID)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM slis`).
		WithArgs(adminSLIFixtureTenantID, adminSLIFixtureGroupID, "").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT (.+) FROM slis`).
		WithArgs(adminSLIFixtureTenantID, adminSLIFixtureGroupID, 50, 0, "").
		WillReturnRows(sqlmock.NewRows(adminSLIColumns))
	mock.ExpectCommit()

	slis, total, err := NewSLIReadRepository(db).List(context.Background(), adminSLIFixtureTenantID, adminSLIFixtureGroupID, 50, 0)
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

	expectTenantTx(t, mock, adminSLIFixtureTenantID)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM slis`).
		WithArgs(adminSLIFixtureTenantID, nil, "").
		WillReturnError(errors.New("count failed"))
	mock.ExpectRollback()

	if _, _, err := NewSLIReadRepository(db).List(context.Background(), adminSLIFixtureTenantID, "", 50, 0); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

var adminSLIFixtureTenantID = scan.GenerateID()
var adminSLIFixtureGroupID = scan.GenerateID()
