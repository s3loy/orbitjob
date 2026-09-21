package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"orbitjob/internal/core/domain/audit"
)

func TestInsertAuditEvent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	// The row carries the change itself: actor, verb, subject and the diff
	// that says what changed.
	mock.ExpectExec(`INSERT INTO audit_events`).
		WithArgs(
			"tenant-a", "caller-key", audit.EventCreate, audit.ResourcePolicy,
			"p1", jsonBytes{want: `{"statement":"allow"}`},
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	ev := testAuditEvent()
	ev.Diff = map[string]any{"statement": "allow"}
	if err := insertAuditEvent(context.Background(), tx, ev); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = tx.Commit()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestInsertAuditEvent_RequiresATenant(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	mock.ExpectRollback()

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	// An audit row without a tenant would be invisible to every scoped read,
	// so it is refused before anything is sent.
	err = insertAuditEvent(context.Background(), tx, audit.Event{EventType: audit.EventCreate})
	if err == nil {
		t.Fatal("expected error for a tenantless event")
	}
	// The caller's rollback completes the sequence before it is checked.
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestInsertAuditEvent_ExecError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO audit_events`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnError(errors.New("insert failed"))
	mock.ExpectRollback()

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := insertAuditEvent(context.Background(), tx, testAuditEvent()); err == nil {
		t.Fatal("expected error, got nil")
	}
	_ = tx.Rollback()
}

func TestInsertAuditEvent_NullsAnEmptyActor(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	// An absent actor is stored as NULL rather than as an empty string, so
	// "unknown" and "unrecorded" do not become two spellings of each other.
	mock.ExpectExec(`INSERT INTO audit_events`).
		WithArgs("tenant-a", nil, audit.EventCreate, audit.ResourcePolicy, "p1", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	ev := testAuditEvent()
	ev.ActorID = ""
	if err := insertAuditEvent(context.Background(), tx, ev); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = tx.Commit()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestNewGrantEvent(t *testing.T) {
	ev := newGrantEvent("tenant-a", "caller-key", audit.EventCreate, audit.ResourcePolicy, "p1", nil)
	if ev.TenantID != "tenant-a" || ev.ActorID != "caller-key" {
		t.Fatalf("event = %+v", ev)
	}
	// A nil diff becomes an empty object, so the encoded row is {} and not
	// the JSON null a marshal of nil would produce.
	if ev.Diff == nil {
		t.Fatal("diff = nil, want an empty map")
	}
	b, err := json.Marshal(ev.Diff)
	if err != nil || string(b) != "{}" {
		t.Fatalf("encoded diff = %s err = %v, want {}", b, err)
	}

	withDiff := newGrantEvent("tenant-a", "caller-key", audit.EventCreate, audit.ResourcePolicy, "p1",
		map[string]any{"granted": true})
	if withDiff.Diff["granted"] != true {
		t.Fatalf("diff = %+v, want the granted flag", withDiff.Diff)
	}
}
