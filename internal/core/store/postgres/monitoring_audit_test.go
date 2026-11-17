package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"orbitjob/internal/core/domain/audit"
	"orbitjob/internal/core/domain/check"
	"orbitjob/internal/core/domain/sli"
	"orbitjob/internal/core/domain/slo"
	"orbitjob/internal/platform/scan"
)

type monitoringMutationCase struct {
	name, event, resource string
	diff                  map[string]any
	setup                 func(sqlmock.Sqlmock)
	run                   func(context.Context, *sql.DB) error
}

func monitoringMutationCases() []monitoringMutationCase {
	tenantID := coreFixtureCheckRepositoryTenantID
	now := time.Now()
	checkRow := func(status string, version int) *sqlmock.Rows {
		return sqlmock.NewRows(checkColumns).AddRow(41, "health", nil, tenantID, nil, status,
			"http_health", []byte(`{}`), []byte(`[]`), "interval", nil, 60, "UTC", 30, 0, 0,
			[]byte(`{}`), nil, version, now, now, 1)
	}
	sloRow := func(status string, version int) *sqlmock.Rows {
		return sqlmock.NewRows(sloColumns).AddRow(41, tenantID, "availability", nil, 7, .99,
			"rolling", 3600, 14.4, 6, status, version, now, now)
	}
	checkStatus := func(action, before, after string) monitoringMutationCase {
		return monitoringMutationCase{
			name: "check " + action, event: "check.status_changed", resource: "check",
			diff: map[string]any{"from_status": before, "to_status": after, "from_version": 1, "to_version": 2},
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT status FROM checks").WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow(before))
				mock.ExpectQuery("UPDATE checks").WillReturnRows(checkRow(after, 2))
			},
			run: func(ctx context.Context, db *sql.DB) error {
				_, err := NewCheckRepository(db).ChangeStatus(ctx, tenantID, "", 41, 1, action)
				return err
			},
		}
	}
	sloStatus := func(status string) monitoringMutationCase {
		return monitoringMutationCase{
			name: "slo " + status, event: "slo.status_changed", resource: "slo",
			diff: map[string]any{"to_status": status, "from_version": 1, "to_version": 2},
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("UPDATE slos SET status").WillReturnRows(sloRow(status, 2))
			},
			run: func(ctx context.Context, db *sql.DB) error {
				_, err := NewSLORepository(db).ChangeStatus(ctx, tenantID, "", 41, 1, status)
				return err
			},
		}
	}
	return []monitoringMutationCase{
		{
			name: "check create", event: "check.created", resource: "check",
			diff: map[string]any{"name": "health", "check_type": "http_health", "version": 1},
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("INSERT INTO checks").WillReturnRows(checkRow("active", 1))
			},
			run: func(ctx context.Context, db *sql.DB) error {
				_, err := NewCheckRepository(db).Create(ctx, check.CreateSpec{TenantID: tenantID, Name: "health", CheckType: "http_health"})
				return err
			},
		},
		checkStatus(check.ActionPause, "active", "paused"),
		checkStatus(check.ActionResume, "paused", "active"),
		{
			name: "check delete", event: "check.deleted", resource: "check",
			diff: map[string]any{"from_version": 1, "to_version": 2},
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("UPDATE checks SET deleted_at").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(41))
			},
			run: func(ctx context.Context, db *sql.DB) error {
				return NewCheckRepository(db).Delete(ctx, tenantID, "", 41, 1)
			},
		},
		{
			name: "sli create", event: "sli.created", resource: "sli",
			diff: map[string]any{"name": "availability", "sli_type": "availability", "version": 1},
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("INSERT INTO slis").WillReturnRows(sqlmock.NewRows(sliColumns).AddRow(
					41, tenantID, "availability", nil, "availability", "job_run", []byte(`{}`), "ratio", []byte(`{}`), 1, now, now))
			},
			run: func(ctx context.Context, db *sql.DB) error {
				_, err := NewSLIRepository(db).Create(ctx, tenantID, sli.CreateSpec{Name: "availability"})
				return err
			},
		},
		{
			name: "sli delete", event: "sli.deleted", resource: "sli",
			diff: map[string]any{"from_version": 1, "to_version": 2},
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT COALESCE").WillReturnRows(sqlmock.NewRows([]string{"source_uid"}).AddRow("check-7"))
				mock.ExpectExec("SELECT orbitjob_lock_sli_source").WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec("UPDATE slis SET deleted_at").WillReturnResult(sqlmock.NewResult(0, 1))
			},
			run: func(ctx context.Context, db *sql.DB) error {
				return NewSLIRepository(db).Delete(ctx, tenantID, "", 41, 1)
			},
		},
		{
			name: "slo create", event: "slo.created", resource: "slo",
			diff: map[string]any{"sli_id": 7, "version": 1, "window_duration_seconds": 3600},
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT id FROM slis").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))
				mock.ExpectQuery("INSERT INTO slos").WillReturnRows(sloRow("active", 1))
			},
			run: func(ctx context.Context, db *sql.DB) error {
				_, err := NewSLORepository(db).Create(ctx, tenantID, slo.CreateSpec{WindowDuration: time.Hour})
				return err
			},
		},
		sloStatus("paused"),
		sloStatus("active"),
		{
			name: "slo delete", event: "slo.deleted", resource: "slo",
			diff: map[string]any{"from_version": 1, "to_version": 2},
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("UPDATE slos SET deleted_at").WillReturnResult(sqlmock.NewResult(0, 1))
			},
			run: func(ctx context.Context, db *sql.DB) error {
				return NewSLORepository(db).Delete(ctx, tenantID, "", 41, 1)
			},
		},
	}
}

type monitoringAuditDiff map[string]any

func (want monitoringAuditDiff) Match(value driver.Value) bool {
	raw, ok := value.([]byte)
	if !ok {
		return false
	}
	var got map[string]any
	if json.Unmarshal(raw, &got) != nil {
		return false
	}
	for key, value := range want {
		if fmt.Sprint(got[key]) != fmt.Sprint(value) {
			return false
		}
	}
	for _, key := range []string{"check_config", "source_config", "good_event_criteria", "description"} {
		if _, present := got[key]; present {
			return false
		}
	}
	return true
}

func TestMonitoringMutationsAuditVerifiedActorAndRollback(t *testing.T) {
	actorID := scan.GenerateID()
	auditFailure := errors.New("audit unavailable")
	for _, mutation := range monitoringMutationCases() {
		for _, identity := range []struct {
			name string
			ctx  context.Context
			kind string
			err  error
		}{
			{"user", audit.WithActor(t.Context(), actorID, "user"), "user", nil},
			{"api key", audit.WithActor(t.Context(), actorID, "api_key"), "api_key", nil},
			{"explicit system", audit.WithActor(t.Context(), actorID, "system"), "system", nil},
			{"missing identity", t.Context(), "", audit.ErrActorRequired},
			{"invalid identity", audit.WithActor(t.Context(), actorID, "unknown"), "", audit.ErrActorRequired},
			{"audit failure", audit.WithActor(t.Context(), actorID, "user"), "user", auditFailure},
		} {
			t.Run(mutation.name+"/"+identity.name, func(t *testing.T) {
				db, mock, err := sqlmock.New()
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = db.Close() }()
				mock.ExpectBegin()
				expectSetConfig(mock, coreFixtureCheckRepositoryTenantID)
				mutation.setup(mock)
				if identity.kind != "" {
					expect := mock.ExpectExec("INSERT INTO audit_events").WithArgs(coreFixtureCheckRepositoryTenantID,
						identity.kind, actorID, mutation.event, mutation.resource, int64(41), monitoringAuditDiff(mutation.diff))
					if identity.err == nil {
						expect.WillReturnResult(sqlmock.NewResult(1, 1))
					} else {
						expect.WillReturnError(identity.err)
					}
				}
				if identity.err == nil {
					mock.ExpectCommit()
				} else {
					mock.ExpectRollback()
				}
				if err := mutation.run(identity.ctx, db); !errors.Is(err, identity.err) {
					t.Fatalf("mutation error = %v, want %v", err, identity.err)
				}
				if err := mock.ExpectationsWereMet(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
