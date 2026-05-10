package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	domainworker "orbitjob/internal/core/domain/worker"
)

func TestNewWorkerRepository(t *testing.T) {
	db := &sql.DB{}
	repo := NewWorkerRepository(db)
	if repo == nil {
		t.Fatal("expected repo != nil")
	}
	if repo.db != db {
		t.Fatal("expected repository to keep db reference")
	}
}

// TestWorkerRepository_UpsertHeartbeatUnit_Success tests the INSERT ON CONFLICT path.
func TestWorkerRepository_UpsertHeartbeatUnit_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewWorkerRepository(db)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	leaseExpires := now.Add(30 * time.Second)

	spec := domainworker.HeartbeatSpec{
		TenantID:        "tenant-a",
		WorkerID:        "worker-1",
		Status:          domainworker.StatusOnline,
		LastHeartbeatAt: now,
		LeaseExpiresAt:  leaseExpires,
		Capacity:        2,
		Labels:          map[string]any{"queue": "video"},
	}

	mock.ExpectQuery("INSERT INTO workers").
		WithArgs("worker-1", "tenant-a", "online", now, leaseExpires, 2, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{
			"tenant_id", "worker_id", "status", "last_heartbeat_at", "lease_expires_at",
			"capacity", "labels", "created_at", "updated_at",
		}).AddRow("tenant-a", "worker-1", "online", now, leaseExpires, 2,
			[]byte(`{"queue":"video"}`), now, now))

	out, err := repo.UpsertHeartbeat(context.Background(), spec)
	if err != nil {
		t.Fatalf("UpsertHeartbeat() error = %v", err)
	}
	if out.WorkerID != "worker-1" {
		t.Fatalf("expected worker_id=worker-1, got %q", out.WorkerID)
	}
	if out.Status != domainworker.StatusOnline {
		t.Fatalf("expected status=online, got %q", out.Status)
	}
	if out.Capacity != 2 {
		t.Fatalf("expected capacity=2, got %d", out.Capacity)
	}
	if out.Labels["queue"] != "video" {
		t.Fatalf("expected labels.queue=video, got %v", out.Labels["queue"])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestWorkerRepository_UpsertHeartbeatUnit_InsertError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewWorkerRepository(db)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	leaseExpires := now.Add(30 * time.Second)

	spec := domainworker.HeartbeatSpec{
		TenantID:        "tenant-a",
		WorkerID:        "worker-1",
		Status:          domainworker.StatusOnline,
		LastHeartbeatAt: now,
		LeaseExpiresAt:  leaseExpires,
		Capacity:        2,
		Labels:          map[string]any{"queue": "video"},
	}

	mock.ExpectQuery("INSERT INTO workers").
		WithArgs("worker-1", "tenant-a", "online", now, leaseExpires, 2, sqlmock.AnyArg()).
		WillReturnError(errors.New("insert boom"))

	_, err = repo.UpsertHeartbeat(context.Background(), spec)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "upsert worker heartbeat") {
		t.Fatalf("expected upsert worker heartbeat error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestWorkerRepository_GetByIDUnit_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewWorkerRepository(db)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	leaseExpires := now.Add(30 * time.Second)

	mock.ExpectQuery("SELECT").
		WithArgs("tenant-a", "worker-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"tenant_id", "worker_id", "status", "last_heartbeat_at", "lease_expires_at",
			"capacity", "labels", "created_at", "updated_at",
		}).AddRow("tenant-a", "worker-1", "online", now, leaseExpires, 3,
			[]byte(`{"queue":"image"}`), now, now))

	out, err := repo.GetByID(context.Background(), "tenant-a", "worker-1")
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if out.WorkerID != "worker-1" {
		t.Fatalf("expected worker_id=worker-1, got %q", out.WorkerID)
	}
	if out.Status != domainworker.StatusOnline {
		t.Fatalf("expected status=online, got %q", out.Status)
	}
	if out.Capacity != 3 {
		t.Fatalf("expected capacity=3, got %d", out.Capacity)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestWorkerRepository_GetByIDUnit_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewWorkerRepository(db)

	mock.ExpectQuery("SELECT").
		WithArgs("tenant-a", "worker-missing").
		WillReturnError(sql.ErrNoRows)

	_, err = repo.GetByID(context.Background(), "tenant-a", "worker-missing")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "get worker by id") {
		t.Fatalf("expected get worker by id error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestScanWorkerSnapshot_UnmarshalError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := time.Now().UTC()
	// Malformed JSON in labels
	rows := sqlmock.NewRows([]string{
		"tenant_id", "worker_id", "status", "last_heartbeat_at", "lease_expires_at",
		"capacity", "labels", "created_at", "updated_at",
	}).AddRow("tenant-a", "worker-1", "online", now, now, 2, []byte("{bad json"), now, now)

	mock.ExpectQuery("SELECT").WillReturnRows(rows)

	r, err := db.Query("SELECT")
	if err != nil {
		t.Fatalf("Query error = %v", err)
	}
	defer func() { _ = r.Close() }()

	if !r.Next() {
		t.Fatal("expected a row")
	}
	_, err = scanWorkerSnapshot(r)
	if err == nil || !strings.Contains(err.Error(), "decode worker labels") {
		t.Fatalf("expected decode worker labels error, got %v", err)
	}
}

func TestScanWorkerSnapshot_NullJSONLabels(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := time.Now().UTC()
	// JSON null → unmarshals to nil Labels map
	rows := sqlmock.NewRows([]string{
		"tenant_id", "worker_id", "status", "last_heartbeat_at", "lease_expires_at",
		"capacity", "labels", "created_at", "updated_at",
	}).AddRow("tenant-a", "worker-1", "online", now, now, 2, []byte("null"), now, now)

	mock.ExpectQuery("SELECT").WillReturnRows(rows)

	r, err := db.Query("SELECT")
	if err != nil {
		t.Fatalf("Query error = %v", err)
	}
	defer func() { _ = r.Close() }()

	if !r.Next() {
		t.Fatal("expected a row")
	}
	snap, err := scanWorkerSnapshot(r)
	if err != nil {
		t.Fatalf("scanWorkerSnapshot() error = %v", err)
	}
	if snap.Labels == nil {
		t.Fatal("expected non-nil Labels map (nil should be turned into empty map)")
	}
	if len(snap.Labels) != 0 {
		t.Fatalf("expected empty Labels, got %v", snap.Labels)
	}
}

func TestScanWorkerSnapshot_NilLabels(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := time.Now().UTC()
	// Nil labels (empty byte slice)
	rows := sqlmock.NewRows([]string{
		"tenant_id", "worker_id", "status", "last_heartbeat_at", "lease_expires_at",
		"capacity", "labels", "created_at", "updated_at",
	}).AddRow("tenant-a", "worker-1", "online", now, now, 2, []byte{}, now, now)

	mock.ExpectQuery("SELECT").WillReturnRows(rows)

	r, err := db.Query("SELECT")
	if err != nil {
		t.Fatalf("Query error = %v", err)
	}
	defer func() { _ = r.Close() }()

	if !r.Next() {
		t.Fatal("expected a row")
	}
	snap, err := scanWorkerSnapshot(r)
	if err != nil {
		t.Fatalf("scanWorkerSnapshot() error = %v", err)
	}
	if snap.Labels == nil {
		t.Fatal("expected non-nil Labels map")
	}
	if len(snap.Labels) != 0 {
		t.Fatalf("expected empty Labels, got %v", snap.Labels)
	}
}

func TestWorkerRepository_UpsertHeartbeatUnit_MarshalLabelsError(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewWorkerRepository(db)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	leaseExpires := now.Add(30 * time.Second)

	// Channel values are not JSON-serializable
	spec := domainworker.HeartbeatSpec{
		TenantID:        "tenant-a",
		WorkerID:        "worker-1",
		Status:          domainworker.StatusOnline,
		LastHeartbeatAt: now,
		LeaseExpiresAt:  leaseExpires,
		Capacity:        2,
		Labels:          map[string]any{"ch": make(chan int)},
	}

	_, err = repo.UpsertHeartbeat(context.Background(), spec)
	if err == nil || !strings.Contains(err.Error(), "marshal worker labels") {
		t.Fatalf("expected marshal worker labels error, got %v", err)
	}
}
